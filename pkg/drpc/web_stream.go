package drpc

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

// parseWebStreamEnvelope reads the procedure path and content type from the stream.
// It handles buffer pooling internally.
func parseWebStreamEnvelope(stream network.Stream, logger glog.Logger) (procedurePath string, contentType string, err error) {
	// Parse procedure path length
	lenBuf := make([]byte, 4) // For uint32
	if _, err = io.ReadFull(stream, lenBuf); err != nil {
		logger.Error("parseWebStreamEnvelope: Failed to read procedure path length", err, glog.LogFields{"remotePeer": stream.Conn().RemotePeer().String()})
		return "", "", err
	}
	pathLen := binary.BigEndian.Uint32(lenBuf)

	if pathLen == 0 || pathLen > defaultMaxEnvelopePathLen {
		err = fmt.Errorf("invalid procedure path length: %d", pathLen)
		logger.Error("parseWebStreamEnvelope: Invalid procedure path length", err, glog.LogFields{"length": pathLen, "remotePeer": stream.Conn().RemotePeer().String()})
		return "", "", err
	}

	pooledPathBufPtr := pathBufPool.Get().(*[]byte)
	defer pathBufPool.Put(pooledPathBufPtr)
	pathBuf := (*pooledPathBufPtr)[:pathLen]
	if _, err = io.ReadFull(stream, pathBuf); err != nil {
		logger.Error("parseWebStreamEnvelope: Failed to read procedure path", err, glog.LogFields{"remotePeer": stream.Conn().RemotePeer().String()})
		return "", "", err
	}
	procedurePath = string(pathBuf)

	// Parse content type length
	contentTypeLenBuf := make([]byte, 1) // For uint8
	if _, err = io.ReadFull(stream, contentTypeLenBuf); err != nil {
		logger.Error("parseWebStreamEnvelope: Failed to read content type length", err, glog.LogFields{"remotePeer": stream.Conn().RemotePeer().String()})
		return "", "", err
	}
	contentTypeLen := uint8(contentTypeLenBuf[0])

	if contentTypeLen == 0 { // contentTypeLen > DefaultMaxEnvelopeContentTypeLen is implicitly checked by pool buffer size
		err = fmt.Errorf("invalid content type length: %d", contentTypeLen)
		logger.Error("parseWebStreamEnvelope: Invalid content type length", err, glog.LogFields{"length": contentTypeLen, "remotePeer": stream.Conn().RemotePeer().String()})
		return "", "", err
	}

	pooledContentTypeBufPtr := contentTypeBufPool.Get().(*[]byte)
	defer contentTypeBufPool.Put(pooledContentTypeBufPtr)
	contentTypeBuf := (*pooledContentTypeBufPtr)[:contentTypeLen]
	if _, err = io.ReadFull(stream, contentTypeBuf); err != nil {
		logger.Error("parseWebStreamEnvelope: Failed to read content type", err, glog.LogFields{"remotePeer": stream.Conn().RemotePeer().String()})
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
			logger.Error("performHTTP2Bridging: Error copying from libp2p stream to reqWriter", err, glog.LogFields{"procedure": procedurePath, "remotePeer": stream.Conn().RemotePeer().String()})
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
		logger.Error("performHTTP2Bridging: Error creating HTTP/2 request", err, glog.LogFields{"procedure": procedurePath, "remotePeer": stream.Conn().RemotePeer().String()})
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
		logger.Error("performHTTP2Bridging: Error sending HTTP/2 request", err, glog.LogFields{"procedure": procedurePath, "remotePeer": stream.Conn().RemotePeer().String()})
		stream.Reset()
		reqReader.CloseWithError(err) // Close pipe reader on error
		return
	}
	defer httpResponse.Body.Close()

	// Copy the HTTP response back to the libp2p stream
	if _, err = io.Copy(stream, httpResponse.Body); err != nil {
		if !errors.Is(err, io.EOF) { // EOF from httpResponse.Body is normal after full read
			logger.Error("performHTTP2Bridging: Error copying response to libp2p stream", err, glog.LogFields{"procedure": procedurePath, "remotePeer": stream.Conn().RemotePeer().String()})
			stream.Reset()
		}
	}
}

// ServeWebStreamBridge handles a libp2p stream by parsing a custom envelope
// (procedure path, content type) and bridging the stream to an HTTP handler
// using an in-memory HTTP/2 connection.
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
			logger.Error("Panic recovered in ServeWebStreamBridge", fmt.Errorf("panic: %v", r), glog.LogFields{"remotePeer": stream.Conn().RemotePeer().String()})
			stream.Reset() // Ensure stream is reset on panic
		}
		// Ensure stream is closed, log if error occurs during close
		if err := stream.Close(); err != nil {
			// Avoid logging error if context is already done (e.g. server shutting down)
			// as stream might be closed by the remote peer or other reasons.
			select {
			case <-ctx.Done():
				// Context is done, probably a graceful shutdown, don't log stream close error as error.
				logger.Debug("Stream closed during shutdown in ServeWebStreamBridge", glog.LogFields{"remotePeer": stream.Conn().RemotePeer().String(), "error": err.Error()})
			default:
				logger.Error("Error closing web stream in ServeWebStreamBridge", err, glog.LogFields{"remotePeer": stream.Conn().RemotePeer().String()})
			}
		}
	}()

	procedurePath, contentType, err := parseWebStreamEnvelope(stream, logger)
	if err != nil {
		// parseWebStreamEnvelope already logs the specific error
		stream.Reset()
		return
	}

	logger.Info("ServeWebStreamBridge: Handling stream", glog.LogFields{"procedure": procedurePath, "contentType": contentType, "remotePeer": stream.Conn().RemotePeer().String()})

	performHTTP2Bridging(ctx, logger, httpHandler, stream, procedurePath, contentType)
	// performHTTP2Bridging handles its own internal errors, logging, and stream resets.
	// stream.Close() is handled by the main defer.
}
