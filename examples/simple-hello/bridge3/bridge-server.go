package main

import (
	"context"
	"errors" // Used by GreeterServer and for logger.Fatal
	"fmt"
	"io"  // Used by GreeterServer for BidiStreamingEcho
	"log" // Standard log, used for initial fatal errors before glog is set up
	"net/http"
	"strings"

	"connectrpc.com/connect"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"

	// Import drpc config for protocol ID
	"github.com/omgolab/drpc/pkg/config"
	"github.com/omgolab/drpc/pkg/drpc"           // Import our drpc package
	glog "github.com/omgolab/go-commons/pkg/log" // Import glog

	greeterpb "github.com/omgolab/drpc/demo/gen/go/greeter/v1"
	"github.com/omgolab/drpc/demo/gen/go/greeter/v1/greeterv1connect"
)

// GreeterServer implements the Greeter service
type GreeterServer struct {
	greeterv1connect.UnimplementedGreeterServiceHandler
}

// SayHello implements the unary SayHello method
func (g *GreeterServer) SayHello(ctx context.Context, req *connect.Request[greeterpb.SayHelloRequest]) (*connect.Response[greeterpb.SayHelloResponse], error) {
	if req.Msg == nil || req.Msg.Name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("name cannot be empty"))
	}
	log.Printf("SayHello called with name: %s", req.Msg.Name)
	return connect.NewResponse(&greeterpb.SayHelloResponse{
		Message: fmt.Sprintf("Hello, %s (from Go server)", req.Msg.Name),
	}), nil
}

// BidiStreamingEcho implements bidirectional streaming echo
func (g *GreeterServer) BidiStreamingEcho(ctx context.Context, stream *connect.BidiStream[greeterpb.BidiStreamingEchoRequest, greeterpb.BidiStreamingEchoResponse]) error {
	log.Println("BidiStreamingEcho called")

	for {
		// Check for context cancellation
		if err := ctx.Err(); err != nil {
			log.Printf("Context error: %v", err)
			return err
		}

		// Receive a message
		reqMessage, err := stream.Receive()
		if err != nil {
			if errors.Is(err, io.EOF) {
				log.Println("Client closed the stream (EOF)")
				return nil // Clean end of stream
			}
			log.Printf("Error receiving from stream: %v", err)
			return connect.NewError(connect.CodeUnknown, fmt.Errorf("receive error: %w", err))
		}

		log.Printf("Received request with name: '%s'", reqMessage.Name)

		// Echo back with greeting
		if err := stream.Send(&greeterpb.BidiStreamingEchoResponse{
			Greeting: fmt.Sprintf("Hello, %s (from Go server)", reqMessage.Name),
		}); err != nil {
			log.Printf("Error sending response: %v", err)
			return fmt.Errorf("send error: %w", err)
		}
	}
}

// Get a usable multiaddress from the host
func chooseMultiaddr(h host.Host) string {
	// Prefer non-loopback IPv4 TCP addresses
	for _, addr := range h.Addrs() {
		addrStr := addr.String()
		if !strings.Contains(addrStr, "/ws") &&
			!strings.Contains(addrStr, "/p2p-circuit") &&
			strings.Contains(addrStr, "/ip4/") &&
			strings.Contains(addrStr, "/tcp/") &&
			!strings.Contains(addrStr, "/ip4/127.0.0.1/") {
			return fmt.Sprintf("%s/p2p/%s", addr, h.ID())
		}
	}

	// Fall back to loopback or any other address
	for _, addr := range h.Addrs() {
		addrStr := addr.String()
		if strings.Contains(addrStr, "/ip4/") && strings.Contains(addrStr, "/tcp/") {
			return fmt.Sprintf("%s/p2p/%s", addr, h.ID())
		}
	}

	// Last resort
	if len(h.Addrs()) > 0 {
		return fmt.Sprintf("%s/p2p/%s", h.Addrs()[0], h.ID())
	}
	return ""
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger, glogErr := glog.New()
	if glogErr != nil {
		log.Fatalf("Failed to create glog logger: %v", glogErr)
	}

	h, err := libp2p.New(
		libp2p.ListenAddrStrings(
			"/ip4/0.0.0.0/tcp/0",    // Listen on any available port for TCP
			"/ip4/0.0.0.0/tcp/0/ws", // Also listen on websockets
		),
		libp2p.EnableRelay(),
	)
	if err != nil {
		logger.Fatal("Failed to create libp2p host", err)
	}

	chosenAddr := chooseMultiaddr(h)
	if chosenAddr != "" {
		fmt.Printf("BRIDGE_SERVER_MULTIADDR_P2P:%s\\n", chosenAddr)
	} else {
		logger.Fatal("Could not determine a usable multiaddress", errors.New("failed to find usable multiaddress"))
	}

	// Print all addresses for debugging
	log.Printf("Full list of listening addresses for host %s:", h.ID())
	for _, addr := range h.Addrs() {
		log.Printf("  %s/p2p/%s", addr, h.ID())
	}

	// Set up Connect RPC service
	mux := http.NewServeMux()
	greeterPath, greeterHandler := greeterv1connect.NewGreeterServiceHandler(new(GreeterServer))
	mux.Handle(greeterPath, greeterHandler)
	logger.Info("Registered GreeterService handler", glog.LogFields{"path": greeterPath})

	h.SetStreamHandler(config.DRPC_WEB_STREAM_PROTOCOL_ID, func(s network.Stream) {
		logger.Info("Incoming stream from", glog.LogFields{"remotePeer": s.Conn().RemotePeer().String()})
		drpc.ServeWebStreamBridge(ctx, logger, mux, s)
	})

	logger.Info("Set libp2p stream handler for protocol", glog.LogFields{"protocolID": config.DRPC_WEB_STREAM_PROTOCOL_ID})
	fmt.Println("Go server is running and listening for libp2p connections...")

	<-ctx.Done()
	logger.Info("Shutting down...")
	if err := h.Close(); err != nil {
		logger.Error("Error closing libp2p host", err)
	}
}
