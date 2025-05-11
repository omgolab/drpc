package drpc

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network" // For network.Stream
	"github.com/omgolab/drpc/pkg/config"
	"github.com/omgolab/drpc/pkg/core"
	"github.com/omgolab/drpc/pkg/detach"
	"github.com/omgolab/drpc/pkg/gateway"
	"github.com/omgolab/drpc/pkg/proc"
	glog "github.com/omgolab/go-commons/pkg/log" // Ensure glog is imported
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

// ServerInstance represents a dual-protocol server (libp2p and HTTP)
type ServerInstance struct {
	// Core components
	p2pHost    host.Host
	handlerMux *http.ServeMux // Handler for both HTTP and p2p
	logger     glog.Logger
	ctx        context.Context // Context for server lifecycle management

	// Servers and listener
	p2pServer  *http.Server
	httpServer *http.Server

	// State management
	mu           sync.RWMutex  // Protect state access
	httpListener net.Listener  // Store the actual HTTP listener
	httpReadyCh  chan struct{} // Channel to signal when HTTP listener is ready
}

// NewServer creates a new ConnectRPC server that uses both libp2p and HTTP for transport.
func NewServer(
	ctx context.Context,
	connectRpcMuxHandler *http.ServeMux,
	opts ...ServerOption,
) (*ServerInstance, error) {
	// Apply options
	cfg := getDefaultConfig()
	for _, opt := range opts {
		if err := opt(&cfg); err != nil {
			return nil, fmt.Errorf("failed to apply option: %w", err)
		}
	}

	// Start detached process if enabled
	if cfg.isDetachServer {
		server := &ServerInstance{ctx: ctx}
		detachOpts := []detach.DetachOption{detach.WithLogger(cfg.logger)}
		if len(cfg.detachOptions) > 0 {
			detachOpts = append(detachOpts, cfg.detachOptions...)
		}
		_, err := detach.StartDetached(ctx, detachOpts...)
		return server, err
	}

	// Create server instance
	server := &ServerInstance{
		handlerMux: connectRpcMuxHandler,
		ctx:        ctx,
		logger:     cfg.logger, // Store the logger
	}

	// Setup P2P server
	if err := server.setupRpcLpBridgeServer(ctx, &cfg); err != nil {
		return nil, err
	}

	// Start HTTP server if enabled
	if cfg.httpPort >= 0 {
		httpAddr := fmt.Sprintf("%s:%d", cfg.httpHost, cfg.httpPort)
		// Use a channel that can signal success or failure
		server.httpReadyCh = make(chan struct{})

		// Check if port is in use and force close if needed
		if cfg.forceCloseExistingPort && cfg.httpPort > 0 {
			if err := proc.KillPort(fmt.Sprintf("%d", cfg.httpPort)); err != nil {
				return nil, err
			}
		}

		if err := server.setupHTTPServer(&cfg, httpAddr); err != nil {
			return nil, err
		}
	}

	return server, nil
}

// setupRpcLpBridgeServer configures and starts the P2P<->RPC server
func (s *ServerInstance) setupRpcLpBridgeServer(ctx context.Context, cfg *cfg) error {
	// Create libp2p host
	var err error
	s.p2pHost, err = core.CreateLibp2pHost(
		ctx,
		core.WithHostLogger(cfg.logger),
		core.WithHostLibp2pOptions(cfg.libp2pOptions...),
		core.WithHostDHTOptions(cfg.dhtOptions...),
	)
	if err != nil {
		return fmt.Errorf("failed to create libp2p host: %w", err)
	}

	// Create libp2p to HTTP p2pBridgeListener
	p2pBridgeListener := core.NewLibp2pListener(s.p2pHost, config.DRPC_PROTOCOL_ID)

	// Create rpc server for the p2p listener
	rpcServer, err := s.createHTTP2Server(s.handlerMux, p2pBridgeListener.Addr().String())
	if err != nil {
		return fmt.Errorf("failed to create p2p HTTP server: %w", err)
	}

	// Start rpc server
	go func() {
		if err := rpcServer.Serve(p2pBridgeListener); err != nil && err != http.ErrServerClosed {
			cfg.logger.Error("p2p server error", err)
		}
	}()

	s.p2pServer = rpcServer

	// Now, set up the handler for the web stream envelope protocol
	s.p2pHost.SetStreamHandler(config.DRPC_WEB_STREAM_PROTOCOL_ID, func(stream network.Stream) {
		// It captures `s` (ServerInstance)
		// The panic recovery from the original anonymous handler can be kept here,
		// or it can be assumed that ServeWebStreamBridge handles its own panics (which it does).
		// For simplicity and to ensure ServeWebStreamBridge is fully self-contained,
		// we rely on its internal panic handling.
		core.ServeWebStreamBridge(s.ctx, s.logger, s.handlerMux, stream)
	})
	s.logger.Info("Set libp2p stream handler for web stream envelope protocol", glog.LogFields{"protocolID": config.DRPC_WEB_STREAM_PROTOCOL_ID})

	return nil
}

// setupHTTPServer configures and starts the HTTP server
func (s *ServerInstance) setupHTTPServer(cfg *cfg, httpAddr string) error {
	// Create HTTP server with gateway handler
	httpHandler := gateway.SetupHandler(s.handlerMux, cfg.logger, s.p2pHost)
	httpServer, err := s.createHTTP2Server(httpHandler, httpAddr)
	if err != nil {
		return err
	}

	// Start server in goroutine
	go s.serveHTTP(httpServer, httpAddr, cfg.logger)

	s.httpServer = httpServer
	return nil
}

// handleHTTPError logs an HTTP error and updates server state
func (s *ServerInstance) handleHTTPError(msg string, err error, logger glog.Logger) {
	logger.Error(msg, err)

	s.mu.Lock()
	// Mark HTTP server as failed by setting listener to nil
	s.httpListener = nil
	s.mu.Unlock()

	// Only close the channel if it's not nil and not already closed
	// This is a best-effort check since there's no direct way to check if a channel is closed
	select {
	case <-s.httpReadyCh:
		// Channel is already closed, do nothing
	default:
		// Channel is open, close it
		close(s.httpReadyCh)
	}
}

// Close gracefully shuts down both servers and the libp2p host.
func (s *ServerInstance) Close() error {
	// Use the same timeout for all shutdown operations
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var errs []error

	// Components to shut down (in order)
	components := []struct {
		name     string
		shutdown func() error
	}{
		{"http server", func() error {
			if s.httpServer == nil {
				return nil
			}
			err := s.httpServer.Shutdown(ctx)
			s.mu.Lock()
			s.httpListener = nil // Clear the listener on shutdown
			s.mu.Unlock()
			return err
		}},
		{"p2p server", func() error {
			if s.p2pServer == nil {
				return nil
			}
			return s.p2pServer.Shutdown(ctx)
		}},
		{"p2p host", func() error {
			if s.p2pHost == nil {
				return nil
			}
			return s.p2pHost.Close()
		}},
	}

	// Shut down all components
	for _, c := range components {
		if err := c.shutdown(); err != nil {
			errs = append(errs, fmt.Errorf("%s close error: %w", c.name, err))
		}
	}

	if len(errs) == 0 {
		return nil
	}

	// Format errors as a single string with newlines
	var errMsg strings.Builder
	errMsg.WriteString("close errors:\n")
	for i, err := range errs {
		errMsg.WriteString(fmt.Sprintf("  %d: %v\n", i+1, err))
	}
	return errors.New(errMsg.String())
}

// P2PHost returns the underlying libp2p host
func (s *ServerInstance) P2PHost() host.Host {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.p2pHost
}

// HTTPAddr returns the listening HTTP address (host:port) as a string.
// Blocks until the HTTP server is listening or context is canceled.
func (s *ServerInstance) HTTPAddr() string {
	// Return early if no HTTP server is configured
	if s.httpServer == nil || s.httpReadyCh == nil {
		return ""
	}

	// Fast path for already initialized servers
	s.mu.RLock()
	listener := s.httpListener
	s.mu.RUnlock()

	if listener != nil {
		return s.formatListenerAddr()
	}

	// If not ready yet, wait for the HTTP listener
	select {
	case <-s.httpReadyCh:
		s.mu.RLock()
		listener := s.httpListener
		s.mu.RUnlock()

		if listener != nil {
			return s.formatListenerAddr()
		} else {
			// Listener channel was closed but not ready - initialization failed
			s.logger.Error("HTTP listener initialization failed or server has stopped", nil)
		}
	case <-s.ctx.Done():
		// Context was canceled
		s.logger.Error("Context canceled before HTTP listener was ready", nil)
	}
	return ""
}

// formatListenerAddr returns the formatted listener address with the appropriate scheme
func (s *ServerInstance) formatListenerAddr() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.httpListener == nil {
		return ""
	}

	addr := s.httpListener.Addr().String()
	// Check if TLS/SSL is configured
	// if s.httpServer.TLSConfig != nil {
	// 	return "https://" + addr
	// }
	return "http://" + addr
}

// P2PAddrs returns all p2p listening addresses
func (s *ServerInstance) P2PAddrs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.p2pHost == nil {
		return nil
	}

	hostAddrs := s.p2pHost.Addrs()
	addrs := make([]string, 0, len(hostAddrs))
	hostID := s.p2pHost.ID().String()

	for _, addr := range hostAddrs {
		addrs = append(addrs, fmt.Sprintf("%s/p2p/%s", addr.String(), hostID))
	}

	return addrs
}

// createHTTP2Server creates an HTTP server with HTTP/2 support
func (s *ServerInstance) createHTTP2Server(handler http.Handler, addr string) (*http.Server, error) {
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

// serveHTTP starts an HTTP server, handling setup and error management
func (s *ServerInstance) serveHTTP(httpServer *http.Server, httpAddr string, logger glog.Logger) {
	l, err := net.Listen("tcp", httpAddr)
	if err != nil {
		s.handleHTTPError("HTTP server listen error", err, logger)
		return
	}

	s.mu.Lock()
	s.httpListener = l // Store the listener
	s.mu.Unlock()

	logger.Info("HTTP server listening", glog.LogFields{"address": httpAddr})

	if s.httpReadyCh != nil {
		close(s.httpReadyCh) // Signal that listener is ready
	}

	if err := httpServer.Serve(l); err != http.ErrServerClosed {
		s.handleHTTPError("HTTP server error", err, logger)
	}
}
