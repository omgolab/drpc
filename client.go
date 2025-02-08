package drpc

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"

	"connectrpc.com/connect"
	lp "github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
)

// NewClient creates a new ConnectRPC client that uses libp2p for transport.
func NewClient[T any](serverHost host.Host, newServiceClient func(httpClient connect.HTTPClient, baseURL string, opts ...connect.ClientOption) T) T {
	h, _ := lp.New(lp.NoListenAddrs)
	serverPeerID := serverHost.ID()
	ctx := context.Background()

	// connect to ensure the server is reachable
	err := h.Connect(ctx, peer.AddrInfo{
		ID:    serverPeerID,
		Addrs: serverHost.Addrs(),
	})
	if err != nil {
		log.Fatal(err)
	}

	// Custom transport that uses the libp2p dialer.
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dial(ctx, h, PROTOCOL_ID, serverPeerID)
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

	return &Conn{Stream: stream}, nil
}
