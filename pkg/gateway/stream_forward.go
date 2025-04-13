package gateway

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"strings"
	"sync"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/omgolab/drpc/pkg/config"
	"github.com/omgolab/drpc/pkg/core"
	"github.com/omgolab/drpc/pkg/core/pool"
	glog "github.com/omgolab/go-commons/pkg/log"
)

const (
	// GatewayPrefix is the URL path prefix used to identify gateway requests.
	GatewayPrefix = "/gateway/"
)

// Global buffer pool for optimized copying
var bufferPool = &sync.Pool{
	New: func() any {
		// 32KB is a good balance for most HTTP payloads
		return make([]byte, 32*1024)
	},
}

// ForwardHTTPRequest handles the entire request forwarding process using standard Go HTTP client
func ForwardHTTPRequest(w http.ResponseWriter, r *http.Request, p2pHost host.Host, logger glog.Logger) {
	// Strip the gateway prefix and ensure the remaining path starts with '/'
	pathWithoutPrefix := strings.TrimPrefix(r.URL.Path, GatewayPrefix)
	if !strings.HasPrefix(pathWithoutPrefix, "/") {
		pathWithoutPrefix = "/" + pathWithoutPrefix // Ensure it starts with / for ParseAddresses
	}

	// Parse addresses and service path from the URL (without the gateway prefix)
	peerAddrs, servicePath, err := ParseAddresses(pathWithoutPrefix)
	if err != nil {
		logger.Printf("Failed to parse addresses from path '%s': %v", pathWithoutPrefix, err)
		http.Error(w, fmt.Sprintf("Failed to parse addresses: %v", err), http.StatusBadRequest)
		return
	}

	// Convert addresses map to peer.AddrInfo format
	addrInfoMap := ConvertToAddrInfoMap(peerAddrs)

	// Get the connection pool from the manager
	connPool := pool.GetPool(p2pHost, logger)

	// Try connecting to peers in parallel
	connectedPeerID, err := pool.ConnectToFirstAvailablePeer(
		r.Context(),
		p2pHost,
		addrInfoMap,
		logger,
	)
	if err != nil {
		logger.Printf("Failed to connect to any peer: %v", err)
		http.Error(w, fmt.Sprintf("Failed to connect to any peer: %v", err), http.StatusInternalServerError)
		return
	}

	logger.Printf("ForwardHTTPRequest - Connected to PeerID: %s", connectedPeerID.String())
	logger.Printf("ForwardHTTPRequest - Forwarding to service: %s", servicePath)

	// Note: We don't get a single stream here anymore.
	// The transport's DialContext will get streams as needed.

	// Create a standard http transport that dials a new stream for each connection attempt
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			// Determine the correct protocol ID based on whether it's a relay connection
			protocolID := config.PROTOCOL_ID // Default to application protocol
			// Always use the application protocol. Libp2p handles relay negotiation internally
			// when dialing a /p2p-circuit address with WithAllowLimitedConn.
			logger.Printf("DialContext: Dialing %s with app protocol %s", connectedPeerID, protocolID)

			// Get a new stream from the pool for this specific dial using the determined protocol ID
			stream, err := connPool.GetStream(ctx, connectedPeerID, protocolID)
			if err != nil {
				logger.Printf("Failed to get stream for dial to %s using protocol %s: %v", connectedPeerID, protocolID, err)
				return nil, err
			}
			// Return the stream wrapped in net.Conn
			// The http.Client will manage closing this conn/stream
			return &core.Conn{Stream: stream}, nil
		},
		// Explicitly disable HTTP/2 for this internal transport to force HTTP/1.1
		// This might avoid framing issues over the libp2p stream.
		// ForceAttemptHTTP2: false, // Revert: Let http client negotiate (likely HTTP/1.1 first)
		// DisableKeepAlives: true,  // Disable keep-alives to simplify connection handling
	}

	// Clone the request to modify it
	req := r.Clone(r.Context())

	// Modify the request path to be the service path expected by the ConnectRPC handler
	// servicePath already includes the leading '/'
	req.URL.Path = servicePath
	req.URL.RawPath = servicePath // Use RawPath if available, otherwise Path
	req.URL.Scheme = "http"
	req.URL.Host = "localhost" // Placeholder, actual routing happens via the transport

	// Remove the host header as it might be incorrect for the destination
	req.Host = ""
	// Clear RequestURI, as it should not be set in client requests
	req.RequestURI = ""

	// Set Connect-RPC headers if needed
	if r.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/connect+proto")
	}
	if r.Header.Get("Accept") == "" {
		req.Header.Set("Accept", "application/connect+proto")
	}

	// Log the request for debugging
	rawReq, err := httputil.DumpRequestOut(req, false) // Don't include body in logs
	if err == nil {
		logger.Printf("ForwardHTTPRequest - Request: %s", string(rawReq))
	}

	// Create an HTTP client with our custom http2 transport
	// Ensure client doesn't force HTTP/1.1 which might conflict
	client := &http.Client{
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse // Don't follow redirects
		},
		// Do NOT set Timeout here, rely on context cancellation
		// Do NOT set Jar (cookie handling) unless specifically needed
	}

	// Execute the request via the client which uses our lip2p connection
	resp, err := client.Do(req)
	if err != nil {
		logger.Printf("Failed to execute request: %v", err)
		http.Error(w, fmt.Sprintf("Failed to execute request: %v", err), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	// Copy response headers
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	// Set response status
	w.WriteHeader(resp.StatusCode)

	// Copy response body using optimized buffer
	if _, err = optimizedCopy(w, resp.Body); err != nil {
		logger.Printf("Failed to copy response body: %v", err)
		// Too late to change the status code here, client already has headers
		return
	}

	logger.Printf("ForwardHTTPRequest - Successfully forwarded request to %s", servicePath)
}

// optimizedCopy uses the global buffer pool for copying
func optimizedCopy(dst io.Writer, src io.Reader) (int64, error) {
	buf := bufferPool.Get().([]byte)
	defer bufferPool.Put(buf)

	return io.CopyBuffer(dst, src, buf)
}
