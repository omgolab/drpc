package server

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/omgolab/drpc/pkg/config"
	"github.com/omgolab/drpc/pkg/core"
	h "github.com/omgolab/drpc/pkg/core/host"
	glog "github.com/omgolab/go-commons/pkg/log"
)

// P2PServerManager handles P2P server functionality
type P2PServerManager struct {
	host       host.Host
	server     *http.Server
	logger     glog.Logger
	ctx        context.Context
	handlerMux *http.ServeMux
}

// NewP2PServerManager creates a new P2P server manager
func NewP2PServerManager(ctx context.Context, handlerMux *http.ServeMux, logger glog.Logger) *P2PServerManager {
	return &P2PServerManager{
		ctx:        ctx,
		handlerMux: handlerMux,
		logger:     logger,
	}
}

// Setup configures and starts the P2P server
func (p *P2PServerManager) Setup(cfg *Config) error {
	// Create libp2p host
	var err error

	p.host, err = h.CreateLibp2pHost(
		p.ctx,
		h.WithHostLogger(cfg.logger),
		h.WithHostLibp2pOptions(cfg.libp2pOptions...),
		h.WithHostDHTOptions(cfg.dhtOptions...),
	)
	if err != nil {
		return fmt.Errorf("failed to create libp2p host: %w", err)
	}

	// Create libp2p to HTTP bridge listener
	p2pBridgeListener := core.NewLibp2pListener(p.host, config.DRPC_PROTOCOL_ID)

	// Create HTTP/2 server for the P2P listener
	rpcServer, err := createHTTP2Server(p.handlerMux, p2pBridgeListener.Addr().String())
	if err != nil {
		return fmt.Errorf("failed to create p2p HTTP server: %w", err)
	}

	// Start RPC server
	go func() {
		if err := rpcServer.Serve(p2pBridgeListener); err != nil && err != http.ErrServerClosed {
			cfg.logger.Error("p2p server error", err)
		}
	}()

	p.server = rpcServer

	// Set up the web stream envelope protocol handler
	p.host.SetStreamHandler(config.DRPC_WEB_STREAM_PROTOCOL_ID, func(stream network.Stream) {
		// Use ServeWebStreamBridge for handling web stream protocol
		core.ServeWebStreamBridge(p.ctx, p.logger, p.handlerMux, stream)
	})

	p.logger.Info("Set libp2p stream handler for web stream envelope protocol",
		glog.LogFields{"protocolID": config.DRPC_WEB_STREAM_PROTOCOL_ID})

	return nil
}

// GetHost returns the libp2p host
func (p *P2PServerManager) GetHost() host.Host {
	return p.host
}

// Host returns the underlying libp2p host
func (p *P2PServerManager) Host() host.Host {
	return p.host
}

// Server returns the underlying HTTP server
func (p *P2PServerManager) Server() *http.Server {
	return p.server
}

// P2PAddrs returns all P2P listening addresses
func (p *P2PServerManager) P2PAddrs() []string {
	if p.host == nil {
		return nil
	}

	hostAddrs := p.host.Addrs()
	addrs := make([]string, 0, len(hostAddrs))
	hostID := p.host.ID().String()

	for _, addr := range hostAddrs {
		addrs = append(addrs, fmt.Sprintf("%s/p2p/%s", addr.String(), hostID))
	}

	return addrs
}

// Close gracefully shuts down the P2P server
func (p *P2PServerManager) Close() error {
	var errs []error

	// Close P2P server
	if p.server != nil {
		// Set shorter timeouts for server shutdown
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		p.server.ReadTimeout = 5 * time.Second
		p.server.WriteTimeout = 5 * time.Second
		p.server.IdleTimeout = 5 * time.Second

		if err := p.server.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("p2p server shutdown error: %w", err))
		}
	}

	// Close P2P host
	if p.host != nil {
		// Force close all connections before closing host to prevent connection leaks
		network := p.host.Network()
		for _, conn := range network.Conns() {
			if conn != nil {
				_ = conn.Close() // Force close without waiting
			}
		}

		if err := p.host.Close(); err != nil {
			errs = append(errs, fmt.Errorf("p2p host close error: %w", err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("p2p close errors: %v", errs)
	}
	return nil
}
