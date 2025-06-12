package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/omgolab/drpc/pkg/detach"
	glog "github.com/omgolab/go-commons/pkg/log"
)

// DRPCServer represents a dual-protocol server (libp2p and HTTP)
type DRPCServer struct {
	// Server managers
	p2pManager  *P2PServerManager
	httpManager *HTTPServerManager

	// Core components
	handlerMux *http.ServeMux
	logger     glog.Logger
	ctx        context.Context

	// State management
	mu sync.RWMutex
}

// New creates a new dRPC server that uses both libp2p and HTTP/2 for transport with connectRPC based handlers.
func New(
	ctx context.Context,
	connectRpcMuxHandler *http.ServeMux,
	opts ...ServerOption,
) (*DRPCServer, error) {
	// Apply options
	cfg := GetDefaultConfig()
	for _, opt := range opts {
		if err := opt(&cfg); err != nil {
			return nil, fmt.Errorf("failed to apply option: %w", err)
		}
	}

	// Start detached process if enabled
	if cfg.isDetachServer {
		server := &DRPCServer{ctx: ctx}
		detachOpts := []detach.DetachOption{detach.WithLogger(cfg.logger)}
		if len(cfg.detachOptions) > 0 {
			detachOpts = append(detachOpts, cfg.detachOptions...)
		}
		_, err := detach.StartDetached(ctx, detachOpts...)
		return server, err
	}

	// Create server instance
	server := &DRPCServer{
		handlerMux: connectRpcMuxHandler,
		ctx:        ctx,
		logger:     cfg.logger,
	}

	// Setup P2P server
	server.p2pManager = NewP2PServerManager(ctx, connectRpcMuxHandler, cfg.logger)
	if err := server.p2pManager.Setup(&cfg); err != nil {
		return nil, err
	}

	// Start HTTP server if enabled
	if cfg.httpPort >= 0 {
		server.httpManager = NewHTTPServerManager(ctx, connectRpcMuxHandler, cfg.logger)
		if err := server.httpManager.Setup(&cfg, server.p2pManager.Host()); err != nil {
			return nil, err
		}
	}

	return server, nil
}

// P2PHost returns the underlying libp2p host
func (s *DRPCServer) P2PHost() host.Host {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.p2pManager == nil {
		return nil
	}
	return s.p2pManager.Host()
}

// HTTPAddr returns the listening HTTP address (host:port) as a string.
// Blocks until the HTTP server is listening or context is canceled.
func (s *DRPCServer) HTTPAddr() string {
	if s.httpManager == nil {
		return ""
	}
	return s.httpManager.HTTPAddr()
}

// P2PAddrs returns all p2p listening addresses
func (s *DRPCServer) P2PAddrs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.p2pManager == nil {
		return nil
	}
	return s.p2pManager.P2PAddrs()
}

// IsHTTPRunning returns true if the HTTP server is running
func (s *DRPCServer) IsHTTPRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.httpManager != nil
}

// IsP2PRunning returns true if the P2P server is running
func (s *DRPCServer) IsP2PRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.p2pManager != nil
}

// GetP2PHost returns the libp2p host if available
func (s *DRPCServer) GetP2PHost() host.Host {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.p2pManager == nil {
		return nil
	}
	return s.p2pManager.GetHost()
}

// Close gracefully shuts down both servers and the libp2p host.
func (s *DRPCServer) Close() error {
	var errs []error

	// Components to shut down (in order)
	components := []struct {
		name     string
		shutdown func() error
	}{
		{"http server", func() error {
			if s.httpManager == nil {
				return nil
			}
			return s.httpManager.Close()
		}},
		{"p2p server", func() error {
			if s.p2pManager == nil {
				return nil
			}
			return s.p2pManager.Close()
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
