package drpc

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"

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
		httpClient := &http.Client{
			Transport: &http.Transport{
				ForceAttemptHTTP2: true,
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

	// Creating a new libp2p host for the client using the robust implementation from core.CreateLibp2pHost
	// but still maintaining client behavior by passing NoListenAddrs option
	clientHost, err := core.CreateLibp2pHost(
		ctx,
		logger,
		append(client.libp2pOptions, libp2p.NoListenAddrs, libp2p.EnableRelay()),
		client.dhtOptions...,
	)
	if err != nil {
		logger.Error("Failed to create libp2p host", err)
		return zeroValue, fmt.Errorf("failed to create libp2p host: %w", err)
	}

	// Get connection pool from manager
	connPool := pool.GetPool(clientHost)

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
	var currentStream network.Stream
	transport := &http.Transport{
		DialContext: func(ctx context.Context, net, addr string) (net.Conn, error) {
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

	//FIXME: after integration tests, we can remove this part till 
	// Check if we're connecting through a relay
	connectedness := h.Network().Connectedness(peerID)
	if connectedness != network.Connected {
		return nil, fmt.Errorf("not connected to peer (state: %v)", connectedness)
	}

	// Keep the protocol ID check for relay connections
	// This is needed for relay connections to work properly
	protocolID := pid

	// Check if any of the peer's addresses include a circuit relay
	isRelayConn := false
	for _, addr := range h.Peerstore().Addrs(peerID) {
		addrStr := addr.String()
		if IsRelayAddr(addrStr) {
			isRelayConn = true
			break
		}
	}

	// If this is a relay connection, use the relay protocol ID
	if isRelayConn {
		protocolID = protocol.ID("/libp2p/circuit/relay/0.2.0/hop")
	}

	// Get a new stream from the pool using the appropriate protocol ID
	stream, err := connPool.GetStream(ctx, peerID, protocolID)
	if err != nil {
		return nil, err
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
