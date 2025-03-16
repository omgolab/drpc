package drpc

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"

	"connectrpc.com/connect"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/omgolab/drpc/pkg/core"
	"github.com/omgolab/drpc/pkg/gateway"
)

// NewClient creates a new ConnectRPC client that uses libp2p for transport.
// Note: future plugin will generate this function by ServiceName
// ## Communication Paths
// Main communication paths for the client:
// 1. **Path 1:** dRPC Client → Listener(if serverAddr is an http address) → Gateway Handler → Host libp2p Peer → dRPC Handler
// 2. **Path 2:** dRPC Client → Listener(if serverAddr is an http address with gateway indication) → Gateway Handler → Relay libp2p Peer → Host libp2p Peer → dRPC Handler
// 3. **Path 3:** dRPC Client → Host libp2p Peer (if serverAddr is a libp2p multiaddress) → dRPC Handler
// 4. **Path 4:** dRPC Client → Relay libp2p Peer(if serverAddr is a libp2p multiaddress) → Host libp2p Peer → dRPC Handler
func NewClient[T any](serverAddr string, newServiceClient func(httpClient connect.HTTPClient, baseURL string, opts ...connect.ClientOption) T) (T, error) {
	ctx := context.Background()
	var zeroValue T

	// Handle HTTP paths (Path 1 and 2)
	if strings.HasPrefix(serverAddr, "http://") || strings.HasPrefix(serverAddr, "https://") {
		// For HTTP paths, we can directly use the ConnectRPC client with the http address
		// This handles Path 1 and Path 2 (gateway handler will resolve between direct or relay)
		httpClient := &http.Client{
			Transport: &http.Transport{
				ForceAttemptHTTP2: true,
			},
		}

		// Create the ConnectRPC client
		client := newServiceClient(
			httpClient,
			serverAddr, // Use the provided HTTP URL directly
		)
		return client, nil
	}

	// Handle libp2p paths (Path 3 and 4) and gateway format with the unified parser
	peerAddrs, _, err := gateway.ParseAddresses(serverAddr) // We don't need servicePath for direct connections
	if err != nil {
		log.Printf("Failed to parse addresses: %v", err)
		return zeroValue, fmt.Errorf("failed to parse addresses: %v", err)
	}

	if len(peerAddrs) == 0 {
		return zeroValue, fmt.Errorf("no valid peer addresses found")
	}

	// Select the first peer ID from the map
	var firstPeerID peer.ID
	var selectedAddrs []ma.Multiaddr
	for id, addrs := range peerAddrs {
		firstPeerID = id
		selectedAddrs = addrs
		break
	}

	if len(peerAddrs) > 1 {
		log.Printf("Warning: Multiple peer IDs found in addresses, using first one: %s", firstPeerID)
	}

	// Creating a new libp2p host for the client
	clientHost, err := libp2p.New(libp2p.NoListenAddrs)
	if err != nil {
		log.Printf("Failed to create libp2p host: %v", err)
		return zeroValue, fmt.Errorf("failed to create libp2p host: %v", err)
	}

	// Create AddrInfo for the selected peer
	peerInfo := peer.AddrInfo{
		ID:    firstPeerID,
		Addrs: selectedAddrs,
	}

	// Connect to the server using any of the available transports
	if err := clientHost.Connect(ctx, peerInfo); err != nil {
		log.Printf("Failed to connect to peer %s: %v", firstPeerID, err)
		return zeroValue, fmt.Errorf("failed to connect to peer: %v", err)
	}

	log.Printf("Successfully connected to peer %s using one of %d transports",
		firstPeerID, len(selectedAddrs))

	// Custom transport that uses the libp2p dialer
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dial(ctx, clientHost, PROTOCOL_ID, firstPeerID)
			// TODO: dial all addresses and return the first successful connection
		},
	}

	// Create a custom HTTP client with the libp2p transport
	httpClient := &http.Client{
		Transport: transport,
	}

	// Create the ConnectRPC client
	client := newServiceClient(
		httpClient,
		"http://localhost", // Placeholder URL, as we're using a custom dialer
	)
	return client, nil
}

// dial uses a libp2p host as dialer.
func dial(ctx context.Context, h host.Host, pid protocol.ID, peerID peer.ID) (net.Conn, error) {
	if h.Network().Connectedness(peerID) != network.Connected {
		return nil, errors.New("not connected to peer")
	}

	relayProtocolID := protocol.ID("/libp2p/circuit/relay/0.2.0/hop")
	streamProtocolID := pid
	if isRelayAddr(h.Peerstore().PeerInfo(peerID).Addrs[0].String()) {
		streamProtocolID = relayProtocolID
	}

	// stream
	stream, err := h.NewStream(ctx, peerID, streamProtocolID)
	if err != nil {
		return nil, err
	}

	return &core.Conn{Stream: stream}, nil
}

func isRelayAddr(addr string) bool {
	isRelay := (addr != "" && (len(addr) > 11 && contains(addr, "/p2p-circuit")))
	log.Printf("Checking if address is relay address: %s, result: %v", addr, isRelay)
	return isRelay
}

func contains(s string, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
