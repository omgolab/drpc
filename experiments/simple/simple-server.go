package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"
	"github.com/multiformats/go-multiaddr"
)

const protocolID = "/hello/1.0.0"

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
	if err != nil {
		fmt.Printf("Error connecting to peer %s: %s\n", pi.ID.String(), err)
	} else {
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
	for _, addr := range h.Addrs() {
		fullAddr := addr.Encapsulate(hostAddr)
		fmt.Printf("Listening on: %s\n", fullAddr)
	}

	// Set a stream handler for the hello protocol
	h.SetStreamHandler(protocol.ID(protocolID), func(s network.Stream) {
		remoteID := s.Conn().RemotePeer().String()
		fmt.Printf("\n===== 📥 Received connection from %s =====\n", remoteID)

		// Read the message
		buf := make([]byte, 1024)
		n, err := s.Read(buf)
		if err != nil {
			fmt.Printf("❌ Error reading from stream: %s\n", err)
			if resetErr := s.Reset(); resetErr != nil {
				fmt.Printf("❌ Error resetting stream: %s\n", resetErr)
			}
			return
		}

		message := string(buf[:n])
		fmt.Printf("📥 RECEIVED: \"%s\"\n", message)

		// Write response message to the stream
		time.Sleep(100 * time.Millisecond) // Small delay for better visibility in logs

		response := fmt.Sprintf("Go server echoing: \"%s\"", message)
		fmt.Printf("📤 SENDING RESPONSE: \"%s\"\n", response)
		_, err = s.Write([]byte(response))
		if err != nil {
			fmt.Printf("❌ Error writing to stream: %s\n", err)
		}

		// Close the stream when done
		fmt.Println("✅ Stream communication complete")
		fmt.Println("======================================================")
		if err := s.Close(); err != nil {
			fmt.Printf("❌ Error closing stream: %s\n", err)
		}
	})

	fmt.Printf("Go server is running with ID: %s\n", h.ID().String())
	fmt.Printf("Protocol: %s\n", protocolID)

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
