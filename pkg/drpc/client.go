package drpc

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"

	"connectrpc.com/connect"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/omgolab/drpc/internal/core"
)

// NewClient creates a new ConnectRPC client that uses libp2p for transport.
func NewClient[T any](clientHost host.Host, serverPeerID peer.ID, serverAddrs []string, newServiceClient func(httpClient connect.HTTPClient, baseURL string, opts ...connect.ClientOption) T) T {
	ctx := context.Background()

	// Parse server multiaddrs
	serverAddrInfo, err := peer.AddrInfoFromString(serverAddrs[0]) // Assuming at least one address
	if err != nil {
		log.Printf("Failed to parse server address: %v", err)
		// Handle the error appropriately, maybe return a nil client and the error
		panic(err) // For now, panic to make it obvious during development
	}

	// Connect to the server
	if err := clientHost.Connect(ctx, *serverAddrInfo); err != nil {
		log.Printf("Failed to connect to server: %v", err)
		// Handle the error appropriately
		panic(err)
	}

	// Custom transport that uses the libp2p dialer.
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dial(ctx, clientHost, PROTOCOL_ID, serverPeerID)
		},
	}

	// Create a custom HTTP client with the custom transport.
	httpClient := &http.Client{
		Transport: transport,
	}

	// Create the ConnectRPC client.
	client := newServiceClient(
		httpClient,
		"http://localhost", // Placeholder URL, as we're using a custom dialer.
	)
	return client
}

// dial uses a libp2p host as dialer.
func dial(ctx context.Context, h host.Host, pid protocol.ID, peerID peer.ID) (net.Conn, error) {
	if h.Network().Connectedness(peerID) != network.Connected {
		return nil, errors.New("not connected to peer")
	}

	// stream
	stream, err := h.NewStream(ctx, peerID, pid)
	if err != nil {
		return nil, err
	}

	return &core.Conn{Stream: stream}, nil
}
