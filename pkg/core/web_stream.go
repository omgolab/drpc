package core

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/omgolab/drpc/pkg/core/pool"
	glog "github.com/omgolab/go-commons/pkg/log"
	"golang.org/x/net/http2"
)

// TODO:
// 1. LRU caching should a seperate generic utility package; check if we have other use cases then create a generic util
// 2. Should use dynamic buffer pooling for content type and path buffers
const (
	// defaultMaxEnvelopePathLen defines the default maximum expected path length for the web stream envelope.
	defaultMaxEnvelopePathLen = 4096

	// Stream processing optimization constants
	maxHeaderParseBufferSize = 8192            // Maximum buffer size for streaming header parsing
	contentTypeCacheSize     = 256             // Maximum number of cached content types
	contentTypeCacheTTL      = 5 * time.Minute // TTL for content type cache entries
)

// contentTypeCacheEntry represents a cached content type with TTL
type contentTypeCacheEntry struct {
	contentType string
	expires     time.Time
}

// contentTypeCache provides LRU-like caching for frequent content types
var (
	contentTypeCache = make(map[string]*contentTypeCacheEntry)
	contentTypeMu    sync.RWMutex
	cacheKeys        []string // Track insertion order for LRU eviction
)

// getCachedContentType retrieves a content type from cache if not expired
func getCachedContentType(key string) (string, bool) {
	contentTypeMu.RLock()
	defer contentTypeMu.RUnlock()

	if entry, exists := contentTypeCache[key]; exists {
		if time.Now().Before(entry.expires) {
			return entry.contentType, true
		}
		// Entry expired but we'll clean it up later to avoid lock upgrade
	}
	return "", false
}

// setCachedContentType stores a content type in cache with TTL
func setCachedContentType(key, contentType string) {
	contentTypeMu.Lock()
	defer contentTypeMu.Unlock()

	now := time.Now()
	entry := &contentTypeCacheEntry{
		contentType: contentType,
		expires:     now.Add(contentTypeCacheTTL),
	}

	// If cache is full, remove oldest entry (simple LRU)
	if len(contentTypeCache) >= contentTypeCacheSize {
		if len(cacheKeys) > 0 {
			oldestKey := cacheKeys[0]
			delete(contentTypeCache, oldestKey)
			cacheKeys = cacheKeys[1:]
		}
	}

	// Add new entry
	contentTypeCache[key] = entry
	cacheKeys = append(cacheKeys, key)

	// Clean expired entries opportunistically
	cleanExpiredEntries(now)
}

// cleanExpiredEntries removes expired cache entries (must be called with lock held)
func cleanExpiredEntries(now time.Time) {
	validKeys := make([]string, 0, len(cacheKeys))
	for _, key := range cacheKeys {
		if entry, exists := contentTypeCache[key]; exists && now.Before(entry.expires) {
			validKeys = append(validKeys, key)
		} else {
			delete(contentTypeCache, key)
		}
	}
	cacheKeys = validKeys
}

var (
// Note: Replaced local pools with centralized pool management for better memory efficiency
// pathBufPool replaced with pool.PathBufferPool
// contentTypeBufPool replaced with pool.ContentTypeBufferPool
)

// ByteCountReader wraps an io.Reader to count bytes read
type ByteCountReader struct {
	Reader    io.Reader
	BytesRead int64
}

// Read implements io.Reader and counts bytes
func (r *ByteCountReader) Read(p []byte) (n int, err error) {
	n, err = r.Reader.Read(p)
	r.BytesRead += int64(n)
	return n, err
}

// parseWebStreamEnvelope reads the procedure path and content type from the stream.
// It handles buffer pooling internally and uses optimized parsing with caching.
func parseWebStreamEnvelope(stream network.Stream, logger glog.Logger) (procedurePath string, contentType string, err error) {
	// Parse procedure path length
	lenBuf := make([]byte, 4) // For uint32
	if _, err = io.ReadFull(stream, lenBuf); err != nil {
		logger.Error(fmt.Sprintf("parseWebStreamEnvelope: Failed to read procedure path length - remotePeer: %s", stream.Conn().RemotePeer().String()), err)
		return "", "", err
	}
	pathLen := binary.BigEndian.Uint32(lenBuf)

	if pathLen == 0 || pathLen > defaultMaxEnvelopePathLen {
		err = fmt.Errorf("invalid procedure path length: %d", pathLen)
		logger.Error(fmt.Sprintf("parseWebStreamEnvelope: Invalid procedure path length - length: %d, remotePeer: %s", pathLen, stream.Conn().RemotePeer().String()), err)
		return "", "", err
	}

	pooledPathBufPtr := pool.PathBufferPool.Get()
	defer pool.PathBufferPool.Put(pooledPathBufPtr)
	pathBuf := (*pooledPathBufPtr)[:pathLen]
	if _, err = io.ReadFull(stream, pathBuf); err != nil {
		logger.Error(fmt.Sprintf("parseWebStreamEnvelope: Failed to read procedure path - remotePeer: %s", stream.Conn().RemotePeer().String()), err)
		return "", "", err
	}
	procedurePath = string(pathBuf)

	// Check cache for this path's content type
	if cachedContentType, found := getCachedContentType(procedurePath); found {
		// Fast path: use cached content type, but we still need to read it from stream for consistency
		logger.Debug(fmt.Sprintf("parseWebStreamEnvelope: Using cached content type for path %s: %s", procedurePath, cachedContentType))
	}

	// Parse content type length
	contentTypeLenBuf := make([]byte, 1) // For uint8
	if _, err = io.ReadFull(stream, contentTypeLenBuf); err != nil {
		logger.Error(fmt.Sprintf("parseWebStreamEnvelope: Failed to read content type length - remotePeer: %s", stream.Conn().RemotePeer().String()), err)
		return "", "", err
	}
	contentTypeLen := uint8(contentTypeLenBuf[0])

	if contentTypeLen == 0 {
		err = fmt.Errorf("invalid content type length: %d", contentTypeLen)
		logger.Error(fmt.Sprintf("parseWebStreamEnvelope: Invalid content type length - length: %d, remotePeer: %s", contentTypeLen, stream.Conn().RemotePeer().String()), err)
		return "", "", err
	}

	pooledContentTypeBufPtr := pool.ContentTypeBufferPool.Get()
	defer pool.ContentTypeBufferPool.Put(pooledContentTypeBufPtr)
	contentTypeBuf := (*pooledContentTypeBufPtr)[:contentTypeLen]
	if _, err = io.ReadFull(stream, contentTypeBuf); err != nil {
		logger.Error(fmt.Sprintf("parseWebStreamEnvelope: Failed to read content type - remotePeer: %s", stream.Conn().RemotePeer().String()), err)
		return "", "", err
	}
	contentType = string(contentTypeBuf)

	// Cache the content type for this path for future use
	setCachedContentType(procedurePath, contentType)

	return procedurePath, contentType, nil
}

// performHTTP2Bridging handles the core logic of bridging the stream to an HTTP handler.
// Enhanced with zero-copy optimizations and improved memory management.
func performHTTP2Bridging(
	ctx context.Context,
	logger glog.Logger,
	httpHandler http.Handler,
	stream network.Stream,
	procedurePath string,
	contentType string,
) {
	reqReader, reqWriter := io.Pipe()
	clientConn, serverConn := net.Pipe()

	// Goroutine to copy data from the libp2p stream to the request pipe writer
	go func() {
		defer reqWriter.Close()

		// Use optimized copying with pooled buffers
		bufPtr := pool.LargeBufferPool.Get()
		defer pool.LargeBufferPool.Put(bufPtr)

		_, err := io.CopyBuffer(reqWriter, stream, *bufPtr)
		if err != nil && !errors.Is(err, io.EOF) {
			logger.Error(fmt.Sprintf("performHTTP2Bridging: Error copying from libp2p stream to reqWriter - procedure: %s, remotePeer: %s", procedurePath, stream.Conn().RemotePeer().String()), err)
			reqWriter.CloseWithError(err) // Signal error to the reader
		}
	}()

	// Goroutine to serve the HTTP handler on the server side of the in-memory pipe
	go func() {
		defer serverConn.Close()
		defer clientConn.Close() // Ensure clientConn is closed if serverConn handler exits

		h2s := &http2.Server{}
		serveOpts := &http2.ServeConnOpts{
			Handler: httpHandler,
			Context: ctx,
		}
		h2s.ServeConn(serverConn, serveOpts)
	}()

	// Setup HTTP/2 client to talk to the in-memory server
	tr := &http2.Transport{
		AllowHTTP: true,
		DialTLSContext: func(dialCtx context.Context, network, addr string, cfg *tls.Config) (net.Conn, error) {
			return clientConn, nil
		},
	}
	httpClient := &http.Client{Transport: tr}

	// Create and send the HTTP/2 request
	httpRequest, err := http.NewRequestWithContext(ctx, "POST", "http://drpc-webstream"+procedurePath, reqReader)
	if err != nil {
		logger.Error(fmt.Sprintf("performHTTP2Bridging: Error creating HTTP/2 request - procedure: %s, remotePeer: %s", procedurePath, stream.Conn().RemotePeer().String()), err)
		stream.Reset()
		reqReader.CloseWithError(err) // Close pipe reader on error
		// clientConn and serverConn will be closed by their goroutine's defer
		return
	}
	httpRequest.Header.Set("Content-Type", contentType)
	httpRequest.Header.Set("Accept", contentType)
	httpRequest.Header.Set("Connect-Protocol-Version", "1")

	httpResponse, err := httpClient.Do(httpRequest)
	if err != nil {
		logger.Error(fmt.Sprintf("performHTTP2Bridging: Error sending HTTP/2 request - procedure: %s, remotePeer: %s", procedurePath, stream.Conn().RemotePeer().String()), err)
		stream.Reset()
		reqReader.CloseWithError(err) // Close pipe reader on error
		return
	}
	defer httpResponse.Body.Close()

	// Copy the HTTP response back to the libp2p stream with zero-copy optimization
	logger.Debug(fmt.Sprintf("performHTTP2Bridging: Starting to copy HTTP response to libp2p stream - procedure: %s, contentType: %s, remotePeer: %s, statusCode: %d, responseContentType: %s",
		procedurePath,
		contentType,
		stream.Conn().RemotePeer().String(),
		httpResponse.StatusCode,
		httpResponse.Header.Get("Content-Type")))

	// Use optimized streaming copy with adaptive buffer sizing
	totalBytes, err := optimizedStreamCopy(stream, httpResponse.Body, logger, procedurePath)
	if err != nil {
		logger.Error(fmt.Sprintf("performHTTP2Bridging: Error during optimized stream copy - procedure: %s, remotePeer: %s, totalBytesSent: %d",
			procedurePath,
			stream.Conn().RemotePeer().String(),
			totalBytes), err)
		stream.Reset()
		return
	}

	// Finished processing all chunks
	logger.Debug(fmt.Sprintf("performHTTP2Bridging: Successfully processed all response chunks - procedure: %s, totalBytesSent: %d",
		procedurePath,
		totalBytes))

	// Make sure we flush and sync before returning
	if f, ok := stream.(interface{ Flush() error }); ok {
		if err := f.Flush(); err != nil {
			logger.Error(fmt.Sprintf("performHTTP2Bridging: Error flushing stream - procedure: %s, remotePeer: %s, totalBytesSent: %d",
				procedurePath,
				stream.Conn().RemotePeer().String(),
				totalBytes), err)
		} else {
			logger.Debug("performHTTP2Bridging: Successfully flushed stream")
		}
	}
}

// optimizedStreamCopy performs optimized streaming copy with adaptive buffer sizing
func optimizedStreamCopy(dst io.Writer, src io.Reader, logger glog.Logger, procedurePath string) (int64, error) {
	// Get buffer from pool for optimized copying
	bufPtr := pool.LargeBufferPool.Get()
	defer pool.LargeBufferPool.Put(bufPtr)
	buffer := *bufPtr

	totalBytes := int64(0)
	chunkCount := 0

	// Adaptive buffer sizing: start with smaller chunks, grow for larger streams
	currentBufSize := len(buffer) / 4 // Start with quarter buffer

	for {
		// Use adaptive buffer size
		if currentBufSize > len(buffer) {
			currentBufSize = len(buffer)
		}

		// Read a chunk from response body
		bytesRead, readErr := src.Read(buffer[:currentBufSize])
		if bytesRead > 0 {
			chunkCount++

			// Log first chunk for debugging
			if chunkCount == 1 {
				if bytesRead <= 64 {
					logger.Debug(fmt.Sprintf("optimizedStreamCopy: First chunk data: % x", buffer[:bytesRead]))
				} else {
					logger.Debug(fmt.Sprintf("optimizedStreamCopy: First chunk data (first 64 bytes): % x", buffer[:64]))
				}
			}

			// Write this chunk to the stream
			n, writeErr := dst.Write(buffer[:bytesRead])
			totalBytes += int64(n)

			if writeErr != nil {
				return totalBytes, fmt.Errorf("error writing chunk to stream - bytesWritten: %d/%d, totalBytesSent: %d: %w",
					n, bytesRead, totalBytes, writeErr)
			}

			// Adaptive buffer sizing: increase buffer size for large streams
			if chunkCount > 2 && bytesRead == currentBufSize && currentBufSize < len(buffer) {
				currentBufSize = min(currentBufSize*2, len(buffer))
			}

			logger.Debug(fmt.Sprintf("optimizedStreamCopy: Successfully wrote chunk - bytesWritten: %d/%d, totalBytesSent: %d, bufferSize: %d",
				n, bytesRead, totalBytes, currentBufSize))
		}

		// Check if we've reached the end
		if readErr != nil {
			if readErr == io.EOF {
				// Normal end of data
				logger.Debug(fmt.Sprintf("optimizedStreamCopy: Finished reading response - totalBytesSent: %d, chunks: %d", totalBytes, chunkCount))
				break
			}

			// Real error
			return totalBytes, fmt.Errorf("error reading response chunk - totalBytesSent: %d, chunks: %d: %w",
				totalBytes, chunkCount, readErr)
		}
	}

	return totalBytes, nil
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ServeWebStreamBridge handles a libp2p stream by parsing a custom envelope
// (procedure path, content type) and bridging the stream to an HTTP handler
// using an in-memory HTTP/2 connection.
// Enhanced with streaming optimizations and zero-copy buffers.
func ServeWebStreamBridge(
	ctx context.Context, // Parent context for operations
	baseLogger glog.Logger, // Logger instance; if nil, a no-op logger will be used
	httpHandler http.Handler, // The HTTP handler to serve (e.g., ConnectRPC mux)
	stream network.Stream, // The incoming libp2p stream
) {
	logger := baseLogger
	if logger == nil {
		logger, _ = glog.New() // Fallback to a no-op logger
	}

	defer func() {
		if r := recover(); r != nil {
			logger.Error(fmt.Sprintf("Panic recovered in ServeWebStreamBridge - remotePeer: %s", stream.Conn().RemotePeer().String()), fmt.Errorf("panic: %v", r))
			stream.Reset() // Ensure stream is reset on panic
		}
		// Ensure stream is closed, log if error occurs during close
		if err := stream.Close(); err != nil {
			// Avoid logging error if context is already done (e.g. server shutting down)
			// as stream might be closed by the remote peer or other reasons.
			select {
			case <-ctx.Done():
				// Context is done, probably a graceful shutdown, don't log stream close error as error.
				logger.Debug(fmt.Sprintf("Stream closed during shutdown in ServeWebStreamBridge - remotePeer: %s, error: %s", stream.Conn().RemotePeer().String(), err.Error()))
			default:
				logger.Error(fmt.Sprintf("Error closing web stream in ServeWebStreamBridge - remotePeer: %s", stream.Conn().RemotePeer().String()), err)
			}
		}
	}()

	procedurePath, contentType, err := parseWebStreamEnvelope(stream, logger)
	if err != nil {
		// parseWebStreamEnvelope already logs the specific error
		stream.Reset()
		return
	}

	logger.Info(fmt.Sprintf("ServeWebStreamBridge: Handling stream - procedure: %s, contentType: %s, remotePeer: %s", procedurePath, contentType, stream.Conn().RemotePeer().String()))

	performHTTP2Bridging(ctx, logger, httpHandler, stream, procedurePath, contentType)
	// performHTTP2Bridging handles its own internal errors, logging, and stream resets.
	// stream.Close() is handled by the main defer.
}
