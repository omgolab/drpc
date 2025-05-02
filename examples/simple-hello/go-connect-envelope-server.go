package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"
	"github.com/multiformats/go-multiaddr"
	greeterpb "github.com/omgolab/drpc/demo/gen/go/greeter/v1"
	"github.com/omgolab/drpc/pkg/proc"
	"google.golang.org/protobuf/proto"
)

const connectEnvelopeProtocolID = "/connect-envelope/1.0.0"

// Envelope flag definitions
const (
	EnvelopeFlagUnary         byte = 0
	EnvelopeFlagStreaming     byte = 1
	EnvelopeFlagError         byte = 2
	EnvelopeFlagBidirectional byte = 3
	EnvelopeFlagEndStream     byte = 0 // Same as unary
)

// startMDNS starts a mDNS discovery service that will advertise this node
func startMDNS(h host.Host) error {
	// setup local mDNS discovery
	s := mdns.NewMdnsService(h, "simple-hello", &mdnsNotifee{h: h})
	return s.Start()
}

type mdnsNotifee struct {
	h host.Host
}

// HandlePeerFound connects to peers discovered via mDNS
func (n *mdnsNotifee) HandlePeerFound(pi peer.AddrInfo) {
	fmt.Printf("mDNS: Found peer: %s\n", pi.ID.String())
	err := n.h.Connect(context.Background(), pi)
	if err == nil {
		fmt.Printf("Connected to peer: %s\n", pi.ID.String())
	}
}

func main() {
	// Create a new libp2p host
	h, err := libp2p.New(
		libp2p.ListenAddrStrings(
			"/ip4/0.0.0.0/tcp/9000",
			"/ip4/0.0.0.0/tcp/9001/ws",
		),
	)
	if err != nil {
		panic(err)
	}
	defer func() {
		if err := h.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "Error closing host: %v\n", err)
		}
	}()

	// Print the node's PeerInfo in multiaddr format
	hostAddr, _ := multiaddr.NewMultiaddr(fmt.Sprintf("/p2p/%s", h.ID().String()))
	var firstFullAddr atomic.Value // stores string
	for _, addr := range h.Addrs() {
		fullAddr := addr.Encapsulate(hostAddr)
		fmt.Printf("Listening on: %s\n", fullAddr)
		if firstFullAddr.Load() == nil {
			firstFullAddr.Store(fullAddr.String())
		}
	}

	// Kill any existing process on port 8080 before starting the server
	_ = proc.KillPort("8080")
	go func() {
		http.HandleFunc("/multiaddr", func(w http.ResponseWriter, r *http.Request) {
			addr := firstFullAddr.Load()
			if addr == nil {
				http.Error(w, "multiaddr not available", http.StatusServiceUnavailable)
				return
			}
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprintln(w, addr.(string))
		})
		_ = http.ListenAndServe("127.0.0.1:8080", nil)
	}()

	// Set a stream handler for the Connect envelope protocol
	// Register the handler for the protocol
	var connectHandler ConnectEnvelopeHandler = &GreeterServiceHandler{}
	h.SetStreamHandler(protocol.ID(connectEnvelopeProtocolID), connectHandler.ServeStream)

	fmt.Printf("Go server is running with ID: %s\n", h.ID().String())
	fmt.Printf("Connect Envelope Protocol: %s\n", connectEnvelopeProtocolID)

	// Start mDNS discovery
	if err := startMDNS(h); err != nil {
		panic(err)
	}

	// Wait for a SIGINT or SIGTERM signal
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	<-ch
	fmt.Println("Received signal, shutting down...")
}

// Handler abstraction for enveloped requests
type ConnectEnvelopeHandler interface {
	ServeStream(s network.Stream)
}

// GreeterServiceHandler routes enveloped requests to the correct method
type GreeterServiceHandler struct{}

// ServeStream handles all the incoming requests within a libp2p stream
func (h *GreeterServiceHandler) ServeStream(s network.Stream) {
	remoteID := s.Conn().RemotePeer().String()
	fmt.Printf("\n===== 📥 [Connect Envelope] Received connection from %s =====\n", remoteID)
	fmt.Printf("Protocol: %s, RemotePeer: %s\n", s.Protocol(), remoteID)

	// Track if we're in a streaming mode or bidirectional mode
	isStreaming := false
	isBidirectional := false
	var messageCount int
	startTime := time.Now()

	fmt.Printf("Starting to process stream at %s (go version)\n", startTime.Format(time.RFC3339))
	defer func() {
		fmt.Printf("Finished processing stream after %s, total messages: %d\n", time.Since(startTime), messageCount)
	}()

	// Buffer to hold incomplete reads
	readBuffer := make([]byte, 0)

	// Read loop
	readBuf := make([]byte, 4096)
	for {
		fmt.Printf("Waiting for data from client...\n")
		n, err := s.Read(readBuf)
		if err != nil {
			if err.Error() != "EOF" {
				fmt.Printf("❌ Error reading from stream: %s\n", err)
			} else {
				fmt.Printf("Client closed connection (EOF)\n")
			}
			break
		}

		fmt.Printf("Read %d bytes from client, data: %v\n", n, readBuf[:n])

		// Append to existing buffer
		readBuffer = append(readBuffer, readBuf[:n]...)
		fmt.Printf("Current buffer size: %d bytes, first 10 bytes (if available): %v\n",
			len(readBuffer),
			func() []byte {
				if len(readBuffer) > 10 {
					return readBuffer[:10]
				}
				return readBuffer
			}())

		// Process complete envelopes from buffer
		processedCount := 0
		for len(readBuffer) >= 5 {
			// Read envelope header
			flags := readBuffer[0]
			msgLen := binary.BigEndian.Uint32(readBuffer[1:5])

			// Found envelope - print the envelope flags in a more descriptive way
			flagDesc := ""
			switch flags {
			case EnvelopeFlagUnary: // Value 0
				if msgLen == 0 {
					flagDesc = "END_STREAM"
				} else {
					flagDesc = "UNARY"
				}
			case EnvelopeFlagStreaming:
				flagDesc = "STREAMING"
			case EnvelopeFlagError:
				flagDesc = "ERROR"
			case EnvelopeFlagBidirectional:
				flagDesc = "BIDIRECTIONAL"
			default:
				flagDesc = "UNKNOWN"
			}

			fmt.Printf("Found envelope - flags: 0x%02x (%s), length: %d, buffer size: %d\n",
				flags, flagDesc, msgLen, len(readBuffer))

			// Check if we have a complete envelope
			if len(readBuffer) < int(5+msgLen) {
				fmt.Printf("Incomplete envelope, waiting for more data (need %d more bytes)\n", 5+msgLen-uint32(len(readBuffer)))
				break
			}

			// Extract payload
			payload := readBuffer[5 : 5+msgLen]

			// Process message based on flags
			h.handleEnvelope(s, flags, payload, &isStreaming, &isBidirectional, &messageCount)
			processedCount++

			// Remove processed envelope from buffer
			readBuffer = readBuffer[5+msgLen:]
			fmt.Printf("Processed envelope %d in this batch, remaining buffer size: %d bytes\n", processedCount, len(readBuffer))
		}

		fmt.Printf("Batch processing complete, processed %d envelopes, remaining buffer: %d bytes\n", processedCount, len(readBuffer))

		// If we've processed at least one message and buffer is empty, check if there's more data
		if processedCount > 0 && len(readBuffer) == 0 {
			// Optional: Set a short read timeout to check for more data without blocking indefinitely
			if err := s.SetReadDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
				fmt.Printf("Warning: Failed to set read deadline: %s\n", err)
			}

			// Try a non-blocking read to see if more data is available
			tmpBuf := make([]byte, 1)
			n, err := s.Read(tmpBuf)

			// Reset deadline to infinite
			if err := s.SetReadDeadline(time.Time{}); err != nil {
				fmt.Printf("Warning: Failed to reset read deadline: %s\n", err)
			}

			if err != nil && err.Error() == "EOF" {
				fmt.Printf("No more data available after processing %d messages, client closed connection\n", messageCount)
				break
			} else if n > 0 {
				// If we got data, add it back to the buffer for next iteration
				readBuffer = append(readBuffer, tmpBuf[:n]...)
				fmt.Printf("More data is available, continuing read loop\n")
			}
		}
	}

	// Explicitly close the write side to flush all responses before closing the stream
	if cw, ok := s.(interface{ CloseWrite() error }); ok {
		_ = cw.CloseWrite()
	}

	fmt.Println("✅ [Connect Envelope] Stream communication complete")
	fmt.Println("======================================================")
	if err := s.Close(); err != nil {
		fmt.Printf("❌ Error closing stream: %s\n", err)
	}
} // Handle a single envelope message
func (h *GreeterServiceHandler) handleEnvelope(s network.Stream, flags byte, payload []byte, isStreaming *bool, isBidirectional *bool, messageCount *int) {
	// Increment message counter first thing
	*messageCount++

	// Log the raw payload (first 20 bytes max) in hex format to help debugging
	payloadPreview := payload
	if len(payload) > 20 {
		payloadPreview = payload[:20]
	}

	// Convert payload preview to hex string
	hexPreview := ""
	for _, b := range payloadPreview {
		hexPreview += fmt.Sprintf("%02x ", b)
	}

	fmt.Printf("Handling message #%d with flags=0x%02x, payload length=%d, preview=%s\n",
		*messageCount, flags, len(payload), hexPreview)
	// Streaming flag (1) indicates it's a streaming request
	if flags == EnvelopeFlagStreaming {
		*isStreaming = true

		// Handle StreamingEcho request
		var echoReq greeterpb.StreamingEchoRequest
		if err := proto.Unmarshal(payload, &echoReq); err != nil {
			fmt.Printf("❌ Error unmarshaling StreamingEcho request: %s\n", err)
			h.writeEnvelopeError(s, flags, "Failed to unmarshal request: "+err.Error())
			return
		}

		fmt.Printf("📥 [Connect Envelope] RECEIVED: StreamingEcho Message=%q (count: %d)\n", echoReq.Message, *messageCount)

		// Create and send response
		resp := &greeterpb.StreamingEchoResponse{
			Message: "Echo: " + echoReq.Message,
		}

		fmt.Printf("📤 [Connect Envelope] SENDING StreamingEcho RESPONSE: %q with flag=%d\n", resp.Message, flags)
		err := h.writeEnvelope(s, flags, resp) // Use same flags for response
		if err != nil {
			fmt.Printf("❌ Error sending streaming response: %s\n", err)
		} else {
			fmt.Printf("✅ Streaming response sent successfully\n")
		}
		fmt.Printf("Streaming message %d processed\n", *messageCount)
	} else if flags == EnvelopeFlagBidirectional {
		// Bidirectional stream handling
		*isBidirectional = true

		// Log envelope details for debugging
		fmt.Printf("Processing bidirectional envelope - message #%d, buffer=%d bytes\n",
			*messageCount, len(payload))

		// For simplicity, we'll parse the incoming chat message as a StreamingEchoRequest
		// Then construct a response in the same format as the client expects
		var echoReq greeterpb.StreamingEchoRequest
		if err := proto.Unmarshal(payload, &echoReq); err != nil {
			fmt.Printf("❌ Error unmarshaling bidirectional request: %s\n", err)
			h.writeEnvelopeError(s, flags, "Failed to unmarshal request: "+err.Error())
			return
		}

		// Extract text from the message
		text := echoReq.Message
		fmt.Printf("📥 [Connect Envelope] RECEIVED BIDIRECTIONAL: Message=%q (count: %d)\n", text, *messageCount)

		// Create a response message - using StreamingEchoResponse for simplicity
		responseText := fmt.Sprintf("Hello, %s (from Go server)", text)
		resp := &greeterpb.StreamingEchoResponse{
			Message: responseText,
		}

		// Format a proper bidirectional response
		fmt.Printf("📤 [Connect Envelope] SENDING BIDIRECTIONAL RESPONSE: %q with flag=%d\n", responseText, flags)
		// We need to use the bidirectional flag (3) for the response to ensure proper handling
		err := h.writeEnvelope(s, EnvelopeFlagBidirectional, resp)
		if err != nil {
			fmt.Printf("❌ Error sending bidirectional response: %s\n", err)
		} else {
			fmt.Printf("✅ Bidirectional response sent successfully\n")
		}
	} else if flags == EnvelopeFlagUnary {
		// Try SayHelloRequest (unary)
		var sayHelloReq greeterpb.SayHelloRequest
		if err := proto.Unmarshal(payload, &sayHelloReq); err != nil {
			fmt.Printf("❌ Error unmarshaling SayHello request: %s\n", err)
			return
		}

		if sayHelloReq.Name == "" {
			fmt.Printf("❌ Received empty SayHello request name\n")
			return
		}

		fmt.Printf("📥 [Connect Envelope] RECEIVED: SayHello Name=%q\n", sayHelloReq.Name)

		resp := &greeterpb.SayHelloResponse{
			Message: "Hello, " + sayHelloReq.Name + " (from Go server)",
		}

		err := h.writeEnvelope(s, flags, resp)
		if err != nil {
			fmt.Printf("❌ Error sending SayHello response: %s\n", err)
		}

		// For unary requests without streaming, we can stop here if needed
		if !*isStreaming && !*isBidirectional {
			fmt.Printf("Unary request completed successfully\n")
		}
	} else {
		fmt.Printf("⚠️ Unrecognized flag: 0x%02x\n", flags)
	}
} // Write a protobuf message as an envelope with specified flags
func (h *GreeterServiceHandler) writeEnvelope(s network.Stream, flags byte, msg proto.Message) error {
	respBytes, err := proto.Marshal(msg)
	if err != nil {
		fmt.Printf("❌ Error marshaling response: %s\n", err)
		s.Reset()
		return err
	}

	// Create a single buffer for the entire envelope to minimize writes
	respBuf := make([]byte, 5+len(respBytes))
	respBuf[0] = flags
	binary.BigEndian.PutUint32(respBuf[1:5], uint32(len(respBytes)))
	copy(respBuf[5:], respBytes)

	// Print the entire envelope buffer in hex for debugging
	hexBuf := ""
	for i, b := range respBuf {
		if i < 20 { // Print at most first 20 bytes
			hexBuf += fmt.Sprintf("%02x ", b)
		} else if i == 20 {
			hexBuf += "..."
			break
		}
	}
	fmt.Printf("Writing envelope: flags=0x%02x, length=%d, preview=%s\n",
		flags, len(respBytes), hexBuf)

	// Send in a single write call
	n, err := s.Write(respBuf)
	if err != nil {
		fmt.Printf("❌ Error writing response envelope: %s\n", err)
		s.Reset()
		return err
	}
	fmt.Printf("Wrote envelope with flags=0x%02x, length=%d, bytes written=%d\n", flags, len(respBytes), n)

	// Flush immediately to ensure client gets the response
	if flusher, ok := s.(interface{ Flush() error }); ok {
		if err := flusher.Flush(); err != nil {
			fmt.Printf("❌ Error flushing stream: %s\n", err)
			return err
		}
	}

	return nil
}

// writeEnvelopeWithError writes an error message as a protocol envelope response
func (h *GreeterServiceHandler) writeEnvelopeError(s network.Stream, flags byte, errorMessage string) error {
	// Create a custom error response
	fmt.Printf("Sending error response: %s\n", errorMessage)

	// For simplicity, we'll use the StreamingEchoResponse to send the error
	resp := &greeterpb.StreamingEchoResponse{
		Message: "Error: " + errorMessage,
	}

	return h.writeEnvelope(s, flags, resp)
}
