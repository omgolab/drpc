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

	"github.com/libp2p/go-libp2p/core/network"
	glog "github.com/omgolab/go-commons/pkg/log"
	"golang.org/x/net/http2"
)

const (
	// defaultMaxEnvelopePathLen defines the default maximum expected path length for the web stream envelope.
	defaultMaxEnvelopePathLen = 4096
	// defaultMaxEnvelopeContentTypeLen defines the default maximum expected content type length for the web stream envelope.
	defaultMaxEnvelopeContentTypeLen = 255 // Max for uint8
)

var (
	// pathBufPool is used for pooling buffers for reading procedure paths.
	pathBufPool = sync.Pool{
		New: func() any {
			b := make([]byte, defaultMaxEnvelopePathLen)
			return &b // Return a pointer to the slice
		},
	}

	// contentTypeBufPool is used for pooling buffers for reading content types.
	contentTypeBufPool = sync.Pool{
		New: func() any {
			b := make([]byte, defaultMaxEnvelopeContentTypeLen)
			return &b // Return a pointer to the slice
		},
	}
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
// It handles buffer pooling internally.
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

	pooledPathBufPtr := pathBufPool.Get().(*[]byte)
	defer pathBufPool.Put(pooledPathBufPtr)
	pathBuf := (*pooledPathBufPtr)[:pathLen]
	if _, err = io.ReadFull(stream, pathBuf); err != nil {
		logger.Error(fmt.Sprintf("parseWebStreamEnvelope: Failed to read procedure path - remotePeer: %s", stream.Conn().RemotePeer().String()), err)
		return "", "", err
	}
	procedurePath = string(pathBuf)

	// Parse content type length
	contentTypeLenBuf := make([]byte, 1) // For uint8
	if _, err = io.ReadFull(stream, contentTypeLenBuf); err != nil {
		logger.Error(fmt.Sprintf("parseWebStreamEnvelope: Failed to read content type length - remotePeer: %s", stream.Conn().RemotePeer().String()), err)
		return "", "", err
	}
	contentTypeLen := uint8(contentTypeLenBuf[0])

	if contentTypeLen == 0 { // contentTypeLen > DefaultMaxEnvelopeContentTypeLen is implicitly checked by pool buffer size
		err = fmt.Errorf("invalid content type length: %d", contentTypeLen)
		logger.Error(fmt.Sprintf("parseWebStreamEnvelope: Invalid content type length - length: %d, remotePeer: %s", contentTypeLen, stream.Conn().RemotePeer().String()), err)
		return "", "", err
	}

	pooledContentTypeBufPtr := contentTypeBufPool.Get().(*[]byte)
	defer contentTypeBufPool.Put(pooledContentTypeBufPtr)
	contentTypeBuf := (*pooledContentTypeBufPtr)[:contentTypeLen]
	if _, err = io.ReadFull(stream, contentTypeBuf); err != nil {
		logger.Error(fmt.Sprintf("parseWebStreamEnvelope: Failed to read content type - remotePeer: %s", stream.Conn().RemotePeer().String()), err)
		return "", "", err
	}
	contentType = string(contentTypeBuf)

	return procedurePath, contentType, nil
}

// performHTTP2Bridging handles the core logic of bridging the stream to an HTTP handler.
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
		_, err := io.Copy(reqWriter, stream)
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

	// Copy the HTTP response back to the libp2p stream
	logger.Debug(fmt.Sprintf("performHTTP2Bridging: Starting to copy HTTP response to libp2p stream - procedure: %s, contentType: %s, remotePeer: %s, statusCode: %d, responseContentType: %s",
		procedurePath,
		contentType,
		stream.Conn().RemotePeer().String(),
		httpResponse.StatusCode,
		httpResponse.Header.Get("Content-Type")))

	// Read small chunks and write them immediately to maintain streaming
	buffer := make([]byte, 32*1024) // 32KB buffer
	totalBytes := 0

	for {
		// Read a chunk from response body
		bytesRead, readErr := httpResponse.Body.Read(buffer)
		if bytesRead > 0 {
			// We have some data, log it and write it to the stream
			logger.Debug(fmt.Sprintf("performHTTP2Bridging: Read chunk - size: %d bytes, chunk #%d",
				bytesRead, totalBytes/bytesRead+1))

			// Print hex dump of data for debugging (only for the first chunk)
			if totalBytes == 0 {
				if bytesRead <= 64 {
					// Full dump for small chunks
					logger.Debug(fmt.Sprintf("performHTTP2Bridging: First chunk data: % x", buffer[:bytesRead]))
				} else {
					// First 64 bytes for larger chunks
					logger.Debug(fmt.Sprintf("performHTTP2Bridging: First chunk data (first 64 bytes): % x", buffer[:64]))
				}
			}

			// Write this chunk to the stream
			n, writeErr := stream.Write(buffer[:bytesRead])
			totalBytes += n

			if writeErr != nil {
				logger.Error(fmt.Sprintf("performHTTP2Bridging: Error writing chunk to libp2p stream - procedure: %s, remotePeer: %s, bytesWritten: %d/%d, totalBytesSent: %d",
					procedurePath,
					stream.Conn().RemotePeer().String(),
					n,
					bytesRead,
					totalBytes),
					writeErr)
				stream.Reset()
				return
			}

			logger.Debug(fmt.Sprintf("performHTTP2Bridging: Successfully wrote chunk - bytesWritten: %d/%d, totalBytesSent: %d",
				n, bytesRead, totalBytes))
		}

		// Check if we've reached the end
		if readErr != nil {
			if readErr == io.EOF {
				// Normal end of data
				logger.Debug(fmt.Sprintf("performHTTP2Bridging: Finished reading response - totalBytesSent: %d", totalBytes))
				break
			}

			// Real error
			logger.Error(fmt.Sprintf("performHTTP2Bridging: Error reading HTTP response chunk - procedure: %s, remotePeer: %s, totalBytesSent: %d",
				procedurePath,
				stream.Conn().RemotePeer().String(),
				totalBytes),
				readErr)
			stream.Reset()
			return
		}
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

// ServeWebStreamBridge handles a libp2p stream by parsing a custom envelope
// (procedure path, content type) and bridging the stream to an HTTP handler
// using an in-memory HTTP/2 connection.
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
