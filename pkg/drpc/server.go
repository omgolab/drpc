package drpc

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/omgolab/drpc/pkg/config"
	"github.com/omgolab/drpc/pkg/core"
	"github.com/omgolab/drpc/pkg/detach"
	"github.com/omgolab/drpc/pkg/gateway"
	"github.com/omgolab/drpc/pkg/proc"
)

// ServerInstance represents a dual-protocol server (libp2p and HTTP)
type ServerInstance struct {
	p2pHost      host.Host
	p2pServer    *http.Server
	httpServer   *http.Server
	httpListener net.Listener    // Store the actual HTTP listener
	ctx          context.Context // Context for server lifecycle management
	handlerMux   *http.ServeMux  // Handler for both HTTP and p2p
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
	}

	// Setup P2P server
	if err := server.setupRpcLpBridgeServer(ctx, &cfg); err != nil {
		return nil, err
	}

	// Start HTTP server if enabled
	if cfg.httpPort >= 0 {
		httpAddr := fmt.Sprintf("%s:%d", cfg.httpHost, cfg.httpPort)

		if err := server.setupHTTPServer(&cfg, httpAddr); err != nil {
			return nil, err
		}
	}

	return server, nil
}

// setupRpcLpBridgeServer configures and starts the P2P<->RPC server
func (s *ServerInstance) setupRpcLpBridgeServer(ctx context.Context, cfg *cfg) error {
	// Create libp2p host
	lh, err := core.CreateLibp2pHost(ctx, cfg.logger, cfg.libp2pOptions, cfg.dhtOptions...)
	if err != nil {
		return fmt.Errorf("failed to create libp2p host: %w", err)
	}

	// Create libp2p to HTTP bridgeListener
	bridgeListener := core.NewStreamBridgeListener(lh, config.PROTOCOL_ID)

	s.p2pHost = lh

	// Create rpc server
	rpcServer := &http.Server{
		Handler: s.handlerMux,
		Addr:    bridgeListener.Addr().String(),
	}

	// Start rpc server
	go func() {
		if err := rpcServer.Serve(bridgeListener); err != nil && err != http.ErrServerClosed {
			cfg.logger.Error("p2p server error", err)
		}
	}()

	s.p2pServer = rpcServer
	return nil
}

// setupHTTPServer configures and starts the HTTP server
func (s *ServerInstance) setupHTTPServer(cfg *cfg, httpAddr string) error {
	// Create HTTP server with gateway routes
	httpServer := &http.Server{
		Handler: gateway.SetupHandler(s.handlerMux, cfg.logger, s.p2pHost),
		Addr:    httpAddr,
	}

	// Check if port is in use and force close if needed
	if cfg.forceCloseExistingPort {
		if err := proc.KillPort(httpAddr); err != nil {
			return err
		}
	}

	// Start server in goroutine
	go func() {
		l, err := net.Listen("tcp", httpAddr)
		if err != nil {
			cfg.logger.Error("http server listen error", err)
			return
		}
		s.httpListener = l // Store the listener
		if err := httpServer.Serve(l); err != nil && err != http.ErrServerClosed {
			cfg.logger.Error("http server error", err)
		}
	}()

	s.httpServer = httpServer
	return nil
}

// Close gracefully shuts down both servers and the libp2p host.
func (s *ServerInstance) Close() error {
	var errs []error

	// Use the context for graceful shutdowns
	ctx := context.Background()

	if s.httpServer != nil {
		if err := s.httpServer.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("http server close error: %w", err))
		}
	}

	if s.p2pServer != nil {
		if err := s.p2pServer.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("p2p server close error: %w", err))
		}
	}

	if s.p2pHost != nil {
		if err := s.p2pHost.Close(); err != nil {
			errs = append(errs, fmt.Errorf("p2p host close error: %w", err))
		}
	}

	if len(errs) > 0 {
		// Format errors as a single string with newlines
		var errMsg strings.Builder
		errMsg.WriteString("close errors:\n")
		for i, err := range errs {
			errMsg.WriteString(fmt.Sprintf("  %d: %v\n", i+1, err))
		}
		return errors.New(errMsg.String())
	}
	return nil
}

// P2PHost returns the underlying libp2p host
func (s *ServerInstance) P2PHost() host.Host {
	return s.p2pHost
}

// HTTPAddr returns the listening HTTP address (host:port) as a string.
// Returns an empty string if the HTTP server is not listening.
func (s *ServerInstance) HTTPAddr() string {
	if s.httpListener != nil {
		return s.httpListener.Addr().String()
	}
	return ""
}

// Addrs returns all listening addresses (both p2p and http)
func (s *ServerInstance) Addrs() []string {
	var addrs []string

	// Add p2p addresses
	if s.p2pHost != nil { // Check if p2pHost is initialized
		for _, addr := range s.p2pHost.Addrs() {
			addrs = append(addrs, addr.String()+"/p2p/"+s.p2pHost.ID().String())
		}
	}

	// Add http address if available
	if s.httpServer != nil {
		httpAddr := s.HTTPAddr() // Use the new method to get the actual address
		if httpAddr != "" {
			addrs = append(addrs, "http://"+httpAddr)
		}
	}

	return addrs
}
