package drpc

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"

	"crypto/tls"

	"connectrpc.com/connect"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/omgolab/drpc/pkg/config"
	"github.com/omgolab/drpc/pkg/core"
	"github.com/omgolab/drpc/pkg/core/pool"
	"github.com/omgolab/drpc/pkg/gateway"
	glog "github.com/omgolab/go-commons/pkg/log"
	"golang.org/x/net/http2"
)

// NewClient creates a new ConnectRPC client that uses libp2p for transport.
// Note: future plugin will generate this function by ServiceName
// ## Communication Paths
// Main communication paths for the client:
// 1. **Path 1:** dRPC Client → Listener(if serverAddr is an http address) → Gateway Handler → Host libp2p Peer → dRPC Handler
// 2. **Path 2:** dRPC Client → Listener(if serverAddr is an http address with gateway indication) → Gateway Handler → Relay libp2p Peer → Host libp2p Peer → dRPC Handler
// 3. **Path 3:** dRPC Client → Host libp2p Peer (if serverAddr is a libp2p multiaddress) → dRPC Handler
// 4. **Path 4:** dRPC Client → Relay libp2p Peer(if serverAddr is a libp2p multiaddress) → Host libp2p Peer → dRPC Handler
func NewClient[T any](
	ctx context.Context,
	serverAddr string,
	newServiceClient func(httpClient connect.HTTPClient, baseURL string, opts ...connect.ClientOption) T,
	clientOpts ...ClientOption,
) (T, error) {
	var zeroValue T

	// Initialize client with default settings
	client := &clientCfg{}

	// Apply options
	if err := client.applyOptions(clientOpts...); err != nil {
		return zeroValue, fmt.Errorf("failed to apply client options: %w", err)
	}

	logger := client.logger

	// Handle HTTP paths (Path 1 and 2)
	if strings.HasPrefix(serverAddr, "http://") || strings.HasPrefix(serverAddr, "https://") {
		// For HTTP paths, we can directly use the ConnectRPC client with the http address
		// This handles Path 1 and Path 2 (gateway handler will resolve between direct or relay)
		// Use a dedicated http2 transport for h2c (HTTP/2 over cleartext)
		httpClient := &http.Client{
			Transport: &http2.Transport{
				// Allow non-TLS HTTP/2
				AllowHTTP: true,
				// Need a custom dialer that skips TLS for http:// addresses
				DialTLSContext: func(ctx context.Context, network, addr string, cfg *tls.Config) (net.Conn, error) {
					var d net.Dialer
					return d.DialContext(ctx, network, addr)
				},
			},
		}

		// Create the ConnectRPC client
		return newServiceClient(
			httpClient,
			serverAddr,            // Use the provided HTTP URL directly
			client.connectOpts..., // Pass collected connect options
		), nil
	}

	// Handle libp2p paths (Path 3 and 4) and gateway format with the unified parser
	peerAddrs, _, err := gateway.ParseAddresses(serverAddr) // We don't need servicePath for direct connections
	if err != nil {
		logger.Error("Failed to parse addresses", err)
		return zeroValue, fmt.Errorf("failed to parse addresses: %w", err)
	}

	if len(peerAddrs) == 0 {
		return zeroValue, fmt.Errorf("no valid peer addresses found")
	}

	// Creating a new libp2p host for the client.
	// If connecting via relay, configure AutoRelay.
	libp2pOpts := append(client.libp2pOptions, libp2p.NoListenAddrs) // Start with base options

	// Check if the target address is a relay address
	isCircuitAddr := gateway.IsRelayAddr(serverAddr) // Use helper from gateway package
	if isCircuitAddr {
		relayInfo, err := gateway.ExtractRelayAddrInfo(serverAddr) // Extract relay info
		if err != nil {
			return zeroValue, fmt.Errorf("failed to extract relay info from address %s: %w", serverAddr, err)
		}
		if relayInfo == nil {
			return zeroValue, fmt.Errorf("extracted nil relay info from address %s", serverAddr)
		}
		// Add relay client options
		libp2pOpts = append(libp2pOpts,
			libp2p.EnableRelay(), // Needed for client-side? Yes.
			libp2p.EnableAutoRelayWithStaticRelays([]peer.AddrInfo{*relayInfo}),
		)
		logger.Info("Configuring client host to use relay", glog.LogFields{"relayID": relayInfo.ID.String()})
	}

	clientHost, err := core.CreateLibp2pHost(
		ctx,
		logger,
		libp2pOpts,
		client.dhtOptions...,
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
			return dialWithPool(ctx, clientHost, connPool, config.PROTOCOL_ID, connectedPeerID, &currentStream)
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
func dialWithPool(ctx context.Context, h host.Host, connPool *pool.ConnectionPool, pid protocol.ID, peerID peer.ID, currentStream *network.Stream) (net.Conn, error) {
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

// IsRelayAddr checks if the given address string contains the p2p-circuit indicator.
// Renamed from isRelayAddr to export it.
func IsRelayAddr(addr string) bool {
	isRelay := (addr != "" && (len(addr) > 11 && contains(addr, "/p2p-circuit")))
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
