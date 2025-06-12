package gateway

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"sync"
	"time"

	"crypto/tls"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/omgolab/drpc/pkg/config"
	"github.com/omgolab/drpc/pkg/core"
	"github.com/omgolab/drpc/pkg/core/pool"
	glog "github.com/omgolab/go-commons/pkg/log"
	"golang.org/x/net/http2"
)

const (
	// GatewayPrefix is the URL path prefix used to identify gateway requests.
	GatewayPrefix = "/@"

	// Stream forwarding optimization constants
	initialBufferSize = 8192  // Initial buffer size for adaptive buffering
	maxBufferSize     = 65536 // Maximum buffer size for adaptive buffering
	// TODO: like we discussed in the web_stream.go, we need to use a generic LRU cache utils class
	addressCacheSize = 512 // Maximum number of cached parsed addresses
	addressCacheTTL  = 10 * time.Minute
	// TTL for address cache entries
)

// addressCacheEntry represents a cached address parsing result with TTL
type addressCacheEntry struct {
	peerAddrs   map[peer.ID][]ma.Multiaddr
	servicePath string
	expires     time.Time
}

// addressCache provides LRU-like caching for frequent address parsing operations
var (
	addressCache   = make(map[string]*addressCacheEntry)
	addressCacheMu sync.RWMutex
	addressKeys    []string // Track insertion order for LRU eviction
)

// getCachedAddress retrieves a parsed address from cache if not expired
func getCachedAddress(path string) (map[peer.ID][]ma.Multiaddr, string, bool) {
	addressCacheMu.RLock()
	defer addressCacheMu.RUnlock()

	if entry, exists := addressCache[path]; exists {
		if time.Now().Before(entry.expires) {
			return entry.peerAddrs, entry.servicePath, true
		}
		// Entry expired but we'll clean it up later to avoid lock upgrade
	}
	return nil, "", false
}

// setCachedAddress stores a parsed address in cache with TTL
func setCachedAddress(path string, peerAddrs map[peer.ID][]ma.Multiaddr, servicePath string) {
	addressCacheMu.Lock()
	defer addressCacheMu.Unlock()

	now := time.Now()
	entry := &addressCacheEntry{
		peerAddrs:   peerAddrs,
		servicePath: servicePath,
		expires:     now.Add(addressCacheTTL),
	}

	// If cache is full, remove oldest entry (simple LRU)
	if len(addressCache) >= addressCacheSize {
		if len(addressKeys) > 0 {
			oldestKey := addressKeys[0]
			delete(addressCache, oldestKey)
			addressKeys = addressKeys[1:]
		}
	}

	// Add new entry
	addressCache[path] = entry
	addressKeys = append(addressKeys, path)

	// Clean expired entries opportunistically
	cleanExpiredAddresses(now)
}

// cleanExpiredAddresses removes expired cache entries (must be called with lock held)
func cleanExpiredAddresses(now time.Time) {
	validKeys := make([]string, 0, len(addressKeys))
	for _, key := range addressKeys {
		if entry, exists := addressCache[key]; exists && now.Before(entry.expires) {
			validKeys = append(validKeys, key)
		} else {
			delete(addressCache, key)
		}
	}
	addressKeys = validKeys
}

// adaptiveBuffer provides adaptive buffer sizing for stream operations
type adaptiveBuffer struct {
	buffer      []byte
	currentSize int
	maxSize     int
}

// newAdaptiveBuffer creates a new adaptive buffer
func newAdaptiveBuffer() *adaptiveBuffer {
	bufPtr := pool.MediumBufferPool.Get() // Use medium buffer pool instead of large
	buffer := *bufPtr
	maxSize := len(buffer)
	if maxSize == 0 {
		// Fallback if pool returns empty buffer
		buffer = make([]byte, maxBufferSize)
		maxSize = maxBufferSize
	}
	return &adaptiveBuffer{
		buffer:      buffer,
		currentSize: min(initialBufferSize, maxSize),
		maxSize:     maxSize,
	}
}

// getBuffer returns the current buffer slice
func (ab *adaptiveBuffer) getBuffer() []byte {
	return ab.buffer[:ab.currentSize]
}

// adjustSize adjusts buffer size based on usage patterns
func (ab *adaptiveBuffer) adjustSize(bytesUsed int) {
	// If we used the full buffer, increase size
	if bytesUsed == ab.currentSize && ab.currentSize < ab.maxSize {
		ab.currentSize = min(ab.currentSize*2, ab.maxSize)
	}
	// If we used less than quarter, decrease size
	if bytesUsed < ab.currentSize/4 && ab.currentSize > initialBufferSize {
		ab.currentSize = max(ab.currentSize/2, initialBufferSize)
	}
}

// close returns the buffer to the pool
func (ab *adaptiveBuffer) close() {
	if ab.buffer != nil && len(ab.buffer) > 0 {
		bufPtr := &ab.buffer
		pool.MediumBufferPool.Put(bufPtr) // Use medium buffer pool instead of large
		ab.buffer = nil
	}
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// max returns the maximum of two integers
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ForwardHTTPRequest handles the entire request forwarding process using standard Go HTTP client
// Enhanced with address caching, adaptive buffering, and improved error recovery.
func ForwardHTTPRequest(w http.ResponseWriter, r *http.Request, p2pHost host.Host, logger glog.Logger) {
	// DEBUG: Log incoming request method, proto, headers
	if config.DEBUG {
		logger.Printf("[DEBUG] Incoming request: Method=%s Proto=%s ProtoMajor=%d ProtoMinor=%d URI=%s", r.Method, r.Proto, r.ProtoMajor, r.ProtoMinor, r.RequestURI)
		for k, v := range r.Header {
			logger.Printf("[DEBUG] Header: %s: %q", k, v)
		}
	}
	// Check cache for parsed addresses first
	var peerAddrs map[peer.ID][]ma.Multiaddr
	var servicePath string
	var err error

	if cachedPeerAddrs, cachedServicePath, found := getCachedAddress(r.URL.Path); found {
		// Fast path: use cached parsed addresses
		peerAddrs = cachedPeerAddrs
		servicePath = cachedServicePath
		if config.DEBUG {
			logger.Printf("[DEBUG] Using cached addresses for path: %s", r.URL.Path)
		}
	} else {
		// Parse addresses and service path from the URL (without the gateway prefix)
		peerAddrs, servicePath, err = ParseGatewayP2PAddresses(r.URL.Path)
		if err != nil {
			logger.Printf("Failed to parse addresses from path '%s': %v", r.URL.Path, err)
			http.Error(w, fmt.Sprintf("Failed to parse addresses: %v", err), http.StatusBadRequest)
			return
		}

		// Cache the parsed addresses
		setCachedAddress(r.URL.Path, peerAddrs, servicePath)
	}

	// Convert addresses map to peer.AddrInfo format
	addrInfoMap := ConvertToAddrInfoMap(peerAddrs)

	// Get the connection pool from the manager
	connPool := pool.GetPool(p2pHost, logger)

	// Try connecting to peers in parallel with improved error recovery
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

	if config.DEBUG {
		logger.Printf("ForwardHTTPRequest - Connected to PeerID: %s", connectedPeerID.String())
		logger.Printf("ForwardHTTPRequest - Forwarding to service: %s", servicePath)
	}

	// We must use http2.Transport for backend requests to support streaming
	transport := &http2.Transport{
		AllowHTTP: true,
		DialTLS: func(network, addr string, _ *tls.Config) (net.Conn, error) {
			protocolID := config.DRPC_PROTOCOL_ID
			stream, err := connPool.GetStream(r.Context(), connectedPeerID, protocolID)
			if err != nil {
				logger.Printf("DialTLS: Dialing %s with app protocol %s", connectedPeerID, protocolID)
				logger.Printf("Failed to get stream for dial to %s using protocol %s: %v", connectedPeerID, protocolID, err)
				return nil, err
			}
			// Return the stream wrapped in net.Conn
			// The http.Client will manage closing this conn/stream
			return &core.Conn{Stream: stream}, nil
		},
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
	// Dump and log the full outgoing HTTP request, including headers and body
	if config.DEBUG {
		rawReq, err := httputil.DumpRequestOut(req, true)
		if err == nil {
			logger.Printf("ForwardHTTPRequest - FULL OUTGOING REQUEST:\n%s", string(rawReq))
		} else {
			logger.Printf("ForwardHTTPRequest - Failed to dump outgoing request: %v", err)
		}
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

	// Execute the request via the client which uses our libp2p connection
	resp, err := client.Do(req)
	if err != nil {
		logger.Printf("Failed to execute request: %v", err)
		http.Error(w, fmt.Sprintf("Failed to execute request: %v", err), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	// Dump and log the full incoming HTTP response, including headers and body
	if config.DEBUG {
		rawResp, err := httputil.DumpResponse(resp, true)
		if err == nil {
			logger.Printf("ForwardHTTPRequest - FULL INCOMING RESPONSE:\n%s", string(rawResp))
		} else {
			logger.Printf("ForwardHTTPRequest - Failed to dump incoming response: %v", err)
		}
	}

	// Copy response headers
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	// Set response status
	w.WriteHeader(resp.StatusCode)

	// Copy response body using adaptive buffering with pipelining
	if _, err = adaptiveStreamCopy(w, resp.Body, logger); err != nil {
		logger.Printf("Failed to copy response body: %v", err)
		// Too late to change the status code here, client already has headers
		return
	}

	logger.Printf("ForwardHTTPRequest - Successfully forwarded request to %s", servicePath)
}

// adaptiveStreamCopy uses adaptive buffering and pipelining for optimized copying
func adaptiveStreamCopy(dst io.Writer, src io.Reader, logger glog.Logger) (int64, error) {
	adaptiveBuf := newAdaptiveBuffer()
	defer adaptiveBuf.close()

	totalBytes := int64(0)
	chunkCount := 0

	for {
		buffer := adaptiveBuf.getBuffer()

		// Read a chunk from source
		bytesRead, readErr := src.Read(buffer)
		if bytesRead > 0 {
			chunkCount++

			// Write this chunk to destination
			n, writeErr := dst.Write(buffer[:bytesRead])
			totalBytes += int64(n)

			if writeErr != nil {
				return totalBytes, fmt.Errorf("error writing chunk: bytesWritten=%d/%d, totalBytes=%d: %w",
					n, bytesRead, totalBytes, writeErr)
			}

			// Adjust buffer size based on usage
			adaptiveBuf.adjustSize(bytesRead)

			if config.DEBUG && chunkCount <= 3 {
				logger.Printf("adaptiveStreamCopy: chunk %d - read: %d, wrote: %d, bufferSize: %d, totalBytes: %d",
					chunkCount, bytesRead, n, len(buffer), totalBytes)
			}
		}

		// Check if we've reached the end
		if readErr != nil {
			if readErr == io.EOF {
				// Normal end of data
				logger.Printf("adaptiveStreamCopy: completed - totalBytes: %d, chunks: %d", totalBytes, chunkCount)
				break
			}

			// Real error
			return totalBytes, fmt.Errorf("error reading chunk: totalBytes=%d, chunks=%d: %w",
				totalBytes, chunkCount, readErr)
		}
	}

	return totalBytes, nil
}

// optimizedCopy uses the centralized buffer pool for copying (legacy method, kept for compatibility)
func optimizedCopy(dst io.Writer, src io.Reader) (int64, error) {
	bufPtr := pool.MediumBufferPool.Get()
	defer pool.MediumBufferPool.Put(bufPtr)

	// Use the buffer for copying
	return io.CopyBuffer(dst, src, *bufPtr)
}
