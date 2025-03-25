package gateway

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"sync"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/omgolab/drpc/pkg/config"
	"github.com/omgolab/drpc/pkg/core"
	"github.com/omgolab/drpc/pkg/core/pool"
	glog "github.com/omgolab/go-commons/pkg/log"
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
	// Parse addresses and service path from the URL
	peerAddrs, servicePath, err := ParseAddresses(r.URL.Path)
	if err != nil {
		logger.Printf("Failed to parse addresses: %v", err)
		http.Error(w, fmt.Sprintf("Failed to parse addresses: %v", err), http.StatusBadRequest)
		return
	}

	// Convert addresses map to peer.AddrInfo format
	addrInfoMap := ConvertToAddrInfoMap(peerAddrs)

	// Get the connection pool from the manager
	connPool := pool.GetPool(p2pHost)

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

	// Get stream from pool
	stream, err := connPool.GetStream(r.Context(), connectedPeerID, config.PROTOCOL_ID)
	if err != nil {
		logger.Printf("Failed to create stream with peer %s: %v", connectedPeerID, err)
		http.Error(w, fmt.Sprintf("Failed to create stream with peer %s: %v", connectedPeerID, err), http.StatusInternalServerError)
		return
	}

	// Ensure stream is released back to the pool
	defer connPool.ReleaseStream(connectedPeerID, stream)

	logger.Printf("ForwardHTTPRequest - Connected to PeerID: %s", connectedPeerID.String())
	logger.Printf("ForwardHTTPRequest - Forwarding to service: %s", servicePath)

	// Create a net.Conn from the libp2p stream
	conn := &core.Conn{Stream: stream}

	// Create a custom transport that uses our single connection
	transport := &http.Transport{
		// Override the dial function to return our existing connection
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return conn, nil
		},
		// Enable keep-alives for better performance
		DisableKeepAlives: false,
	}

	// Clone the request to modify it
	req := r.Clone(r.Context())

	// Modify the request path to include the service path
	req.URL.Path = "/" + servicePath
	req.URL.RawPath = "/" + servicePath
	req.URL.Scheme = "http"
	req.URL.Host = "localhost" // Placeholder, actual routing happens via the transport

	// Remove the host header as it might be incorrect for the destination
	req.Host = ""

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

	// Create an HTTP client with our custom transport
	client := &http.Client{
		Transport: transport,
		// Don't follow redirects - we want to forward as is
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
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
