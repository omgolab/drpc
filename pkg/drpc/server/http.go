package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/omgolab/drpc/pkg/gateway"
	"github.com/omgolab/drpc/pkg/proc"
	glog "github.com/omgolab/go-commons/pkg/log"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

// HTTPServerManager handles HTTP server functionality
type HTTPServerManager struct {
	server     *http.Server
	listener   net.Listener
	readyCh    chan struct{}
	logger     glog.Logger
	ctx        context.Context
	handlerMux *http.ServeMux
	mu         sync.RWMutex
}

// NewHTTPServerManager creates a new HTTP server manager
func NewHTTPServerManager(ctx context.Context, handlerMux *http.ServeMux, logger glog.Logger) *HTTPServerManager {
	return &HTTPServerManager{
		ctx:        ctx,
		handlerMux: handlerMux,
		logger:     logger,
		readyCh:    make(chan struct{}),
	}
}

// Setup configures and starts the HTTP server
func (h *HTTPServerManager) Setup(cfg *Config, p2pHost host.Host) error {
	httpAddr := fmt.Sprintf("%s:%d", cfg.httpHost, cfg.httpPort)

	// Check if port is in use and force close if needed
	if cfg.forceCloseExistingPort && cfg.httpPort > 0 {
		if err := proc.KillPort(fmt.Sprintf("%d", cfg.httpPort)); err != nil {
			return err
		}
	}

	// Create HTTP server with gateway handler
	httpHandler := gateway.SetupHandler(h.handlerMux, cfg.logger, p2pHost, cfg.corsConfig)
	httpServer, err := createHTTP2Server(httpHandler, httpAddr)
	if err != nil {
		return err
	}

	h.server = httpServer

	// Start server in goroutine
	go h.serve(httpAddr)

	return nil
}

// serve starts the HTTP server and handles errors
func (h *HTTPServerManager) serve(httpAddr string) {
	l, err := net.Listen("tcp", httpAddr)
	if err != nil {
		h.handleError("HTTP server listen error", err)
		return
	}

	h.mu.Lock()
	h.listener = l
	h.mu.Unlock()

	h.logger.Info("HTTP server listening", glog.LogFields{"address": httpAddr})

	if h.readyCh != nil {
		close(h.readyCh) // Signal that listener is ready
	}

	if err := h.server.Serve(l); err != http.ErrServerClosed {
		h.handleError("HTTP server error", err)
	}
}

// handleError logs an HTTP error and updates server state
func (h *HTTPServerManager) handleError(msg string, err error) {
	h.logger.Error(msg, err)

	h.mu.Lock()
	h.listener = nil
	h.mu.Unlock()

	// Only close the channel if it's not nil and not already closed
	select {
	case <-h.readyCh:
		// Channel is already closed, do nothing
	default:
		// Channel is open, close it
		close(h.readyCh)
	}
}

// Server returns the underlying HTTP server
func (h *HTTPServerManager) Server() *http.Server {
	return h.server
}

// HTTPAddr returns the listening HTTP address (host:port) as a string.
// Blocks until the HTTP server is listening or context is canceled.
func (h *HTTPServerManager) HTTPAddr() string {
	// Return early if no HTTP server is configured
	if h.server == nil || h.readyCh == nil {
		return ""
	}

	// Fast path for already initialized servers
	h.mu.RLock()
	listener := h.listener
	h.mu.RUnlock()

	if listener != nil {
		return h.formatListenerAddr()
	}

	// If not ready yet, wait for the HTTP listener
	select {
	case <-h.readyCh:
		h.mu.RLock()
		listener := h.listener
		h.mu.RUnlock()

		if listener != nil {
			return h.formatListenerAddr()
		} else {
			// Listener channel was closed but not ready - initialization failed
			h.logger.Error("HTTP listener initialization failed or server has stopped", nil)
		}
	case <-h.ctx.Done():
		// Context was canceled
		h.logger.Error("Context canceled before HTTP listener was ready", nil)
	}
	return ""
}

// formatListenerAddr returns the formatted listener address with the appropriate scheme
func (h *HTTPServerManager) formatListenerAddr() string {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if h.listener == nil {
		return ""
	}

	addr := h.listener.Addr().String()
	// Check if TLS/SSL is configured
	// if h.server.TLSConfig != nil {
	// 	return "https://" + addr
	// }
	return "http://" + addr
}

// Close gracefully shuts down the HTTP server
func (h *HTTPServerManager) Close() error {
	if h.server == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := h.server.Shutdown(ctx)
	h.mu.Lock()
	h.listener = nil // Clear the listener on shutdown
	h.mu.Unlock()

	if err != nil {
		return fmt.Errorf("http server shutdown error: %w", err)
	}
	return nil
}

// createHTTP2Server creates an HTTP server with HTTP/2 support
func createHTTP2Server(handler http.Handler, addr string) (*http.Server, error) {
	h2s := &http2.Server{}
	server := &http.Server{
		Handler: h2c.NewHandler(handler, h2s),
		Addr:    addr,
	}

	// Configure the server for HTTP/2
	if err := http2.ConfigureServer(server, h2s); err != nil {
		return nil, fmt.Errorf("failed to configure http2 server: %w", err)
	}

	return server, nil
}
