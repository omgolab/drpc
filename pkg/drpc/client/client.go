package client

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"

	"crypto/tls"

	"connectrpc.com/connect"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/omgolab/drpc/pkg/config"
	"github.com/omgolab/drpc/pkg/core"
	"github.com/omgolab/drpc/pkg/core/host"
	"github.com/omgolab/drpc/pkg/core/pool"
	"github.com/omgolab/drpc/pkg/gateway"
	glog "github.com/omgolab/go-commons/pkg/log"
	"golang.org/x/net/http2"
)

// New creates a new ConnectRPC client that uses libp2p for transport.
// Note: future plugin will generate this function by ServiceName
// ## Communication Paths
// Main communication paths for the client:
// 1. **Path 1:** dRPC Client → Listener(if serverAddr is an http address) → Gateway Handler → Host libp2p Peer → dRPC Handler
// 2. **Path 2:** dRPC Client → Listener(if serverAddr is an http address with gateway indication) → Gateway Handler → Relay libp2p Peer → Host libp2p Peer → dRPC Handler
// 3. **Path 3:** dRPC Client → Host libp2p Peer (if serverAddr is a libp2p multiaddress) → dRPC Handler
// 4. **Path 4:** dRPC Client → Relay libp2p Peer(if serverAddr is a libp2p multiaddress) → Host libp2p Peer → dRPC Handler
func New[T any](
	ctx context.Context,
	serverAddr string,
	newServiceClient func(httpClient connect.HTTPClient, baseURL string, opts ...connect.ClientOption) T,
	clientOpts ...Option,
) (T, error) {
	var zeroValue T

	// Initialize client with default settings
	client := &Config{}

	// Apply options
	if err := client.applyOptions(clientOpts...); err != nil {
		return zeroValue, fmt.Errorf("failed to apply client options: %w", err)
	}

	logger := client.logger

	// Handle HTTP paths (Path 1 and 2)
	if strings.HasPrefix(serverAddr, "http://") || strings.HasPrefix(serverAddr, "https://") {
		// For HTTP paths, we can directly use the ConnectRPC client with the http address
		// This handles Path 1 and Path 2 (gateway handler will resolve between direct or relay)

		// Always use HTTP/2 transport for both HTTP and HTTPS
		// This provides better multiplexing and performance
		httpClient := &http.Client{
			Transport: optimizedHTTP2Transport(),
		}

		// Create the ConnectRPC client
		return newServiceClient(
			httpClient,
			serverAddr,            // Use the provided HTTP URL directly
			client.connectOpts..., // Pass collected connect options
		), nil
	}

	// Handle libp2p paths (Path 3 and 4) and gateway format with the unified parser
	peerAddrs, err := gateway.ParseCommaSeparatedMultiAddresses(serverAddr) // We don't need servicePath for direct connections
	if err != nil {
		logger.Error("Failed to parse addresses", err)
		return zeroValue, fmt.Errorf("failed to parse addresses: %w", err)
	}

	// Creating a new libp2p host for the client.
	clientHost, err := host.CreateLibp2pHost(
		ctx,
		host.WithHostLogger(logger),
		host.WithHostLibp2pOptions(client.libp2pOptions...),
		host.WithHostDHTOptions(client.dhtOptions...),
		host.WithHostAsClientMode(),
	)
	if err != nil {
		logger.Error("Failed to create libp2p host", err)
		return zeroValue, fmt.Errorf("failed to create libp2p host: %w", err)
	}

	// Get connection pool from manager
	connPool := pool.GetPool(clientHost, logger) // Pass logger

	// Convert the peer addresses map to the format expected by connection logic
	addrInfoMap := gateway.ConvertToAddrInfoMap(peerAddrs)

	// Try connecting to peers in parallel
	connectedPeerID, err := pool.ConnectToFirstAvailablePeer(
		ctx,
		clientHost,
		addrInfoMap,
		logger,
	)

	if err != nil {
		return zeroValue, fmt.Errorf("failed to connect to any peer: %w", err)
	}

	logger.Info("Successfully connected to peer", glog.LogFields{"peerID": connectedPeerID.String()})

	// Custom transport that uses the libp2p dialer with connection pool
	// Keep track of the current stream for reuse? Maybe not needed if transport handles it. Let's keep for now.
	var currentStream network.Stream

	// Use standard http.Transport for libp2p connections.
	// Both http.Transport and http2.Transport can be used and works/tested, but http2 is preferred for HTTP/2.
	transport := &http2.Transport{
		AllowHTTP: true,
		DialTLSContext: func(ctx context.Context, network, addr string, tlsCfg *tls.Config) (net.Conn, error) {
			// Ignore TLS, use libp2p dialer for h2c
			return dialWithPool(ctx, connPool, config.DRPC_PROTOCOL_ID, connectedPeerID, &currentStream)
		},
	}

	// Create a custom HTTP client with the libp2p transport
	httpClient := &http.Client{
		Transport: transport,
	}

	// Create the ConnectRPC client
	return newServiceClient(
		httpClient,
		"http://localhost",    // Placeholder URL, as we're using a custom dialer
		client.connectOpts..., // Pass collected connect options
	), nil
}

// dialWithPool uses a libp2p host and connection pool as dialer.
func dialWithPool(ctx context.Context, connPool *pool.ConnectionPool, pid protocol.ID, peerID peer.ID, currentStream *network.Stream) (net.Conn, error) {
	// If we already have a stream, check if it's still valid and reuse it
	if *currentStream != nil {
		// Only check direction and basic connection state
		// Direction check is unique to dialWithPool because we specifically need outbound streams
		if (*currentStream).Stat().Direction == network.DirOutbound &&
			(*currentStream).Conn() != nil &&
			!(*currentStream).Conn().IsClosed() {
			// Stream is valid, wrap and return it (reuse as-is)
			return &core.Conn{Stream: *currentStream}, nil
		}

		// If stream can't be reused, close it and get a new one
		// The ManagedStream.Close() will handle proper release to the pool
		(*currentStream).Close()
		*currentStream = nil
	}

	// Get a new stream from the pool using the application protocol ID (pid)
	// Libp2p handles the underlying relay mechanism transparently.
	stream, err := connPool.GetStream(ctx, peerID, pid) // Always use the provided application protocol ID
	if err != nil {
		return nil, fmt.Errorf("failed to get stream from pool for peer %s with protocol %s: %w", peerID, pid, err)
	}
	*currentStream = stream

	return &core.Conn{Stream: stream}, nil
}
