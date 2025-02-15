package drpc

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/omgolab/drpc/internal/core"
	"github.com/omgolab/drpc/internal/gateway"
)

// Server represents a dual-protocol server (libp2p and HTTP)
type Server struct {
	p2pHost    host.Host
	p2pServer  *http.Server
	httpServer *http.Server
	ctx        context.Context // Context for server lifecycle management
}

// NewServer creates a new ConnectRPC server that uses both libp2p and HTTP for transport.
func NewServer(
	ctx context.Context,
	connectRpcMuxHandler http.Handler,
	opts ...Option,
) (*Server, error) {
	// Apply options
	cfg := getDefaultConfig()
	for _, opt := range opts {
		if err := opt(&cfg); err != nil {
			return nil, fmt.Errorf("failed to apply option: %w", err)
		}
	}

	// Create libp2p host
	lh, err := core.CreateLpHost(ctx, cfg.logger, cfg.Libp2pOptions...)
	if err != nil {
		return nil, fmt.Errorf("failed to create libp2p host: %w", err)
	}

	// Create libp2p listener
	listener := core.NewListener(ctx, lh, PROTOCOL_ID)

	// Create p2p server (using raw Connect RPC handler)
	p2pServer := &http.Server{
		Handler: connectRpcMuxHandler,
		Addr:    listener.Addr().String(),
	}

	// Start p2p server
	go func() {
		if err := p2pServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			cfg.logger.Error("p2p server error", err)
		}
	}()

	server := &Server{
		p2pHost:   lh,
		p2pServer: p2pServer,
	}

	// Start HTTP server if enabled with gateway handler
	if cfg.enableHTTP {
		httpPortInt, err := strconv.Atoi(cfg.httpPort)
		if err != nil {
			return nil, fmt.Errorf("invalid HTTP port: %w", err)
		}
		httpAddr := fmt.Sprintf("%s:%d", cfg.httpHost, httpPortInt)
		httpServer := &http.Server{
			Handler: gateway.GetGatewayHandler(connectRpcMuxHandler),
			Addr:    httpAddr,
		}

		go func() {
			if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				cfg.logger.Error("http server error", err)
			}
		}()

		server.httpServer = httpServer
	}

	return server, nil
}

// Close gracefully shuts down both servers and the libp2p host.
func (s *Server) Close() error {
	var errs []error

	// Use the context for graceful shutdowns
	ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second) // 5-second timeout
	defer cancel()

	if s.httpServer != nil {
		if err := s.httpServer.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("http server close error: %w", err))
		}
	}

	if err := s.p2pServer.Shutdown(ctx); err != nil {
		errs = append(errs, fmt.Errorf("p2p server close error: %w", err))
	}

	if err := s.p2pHost.Close(); err != nil {
		errs = append(errs, fmt.Errorf("p2p host close error: %w", err))
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
func (s *Server) P2PHost() host.Host {
	return s.p2pHost
}

// Addrs returns all listening addresses (both p2p and http)
func (s *Server) Addrs() []string {
	var addrs []string

	// Add p2p addresses
	for _, addr := range s.p2pHost.Addrs() {
		addrs = append(addrs, addr.String()+"/p2p/"+s.p2pHost.ID().String())
	}

	// Add http address if available
	if s.httpServer != nil {
		addrs = append(addrs, "http://"+s.httpServer.Addr)
	}

	return addrs
}
