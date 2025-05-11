package drpc

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"

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
	p2pHost        host.Host
	p2pServer      *http.Server
	httpServer     *http.Server
	httpListener   net.Listener    // Store the actual HTTP listener
	ctx            context.Context // Context for server lifecycle management
	handlerMux     *http.ServeMux  // Handler for both HTTP and p2p
	logger         glog.Logger     // Add logger field
	httpListenerCh chan struct{}   // Channel to signal when HTTP listener is ready
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
		server.httpListenerCh = make(chan struct{})

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

	// Create rpc server for the p2p listener, enabling h2c for HTTP/2 over libp2p
	h2s_p2p := &http2.Server{} // Create separate http2 server instance for p2p
	rpcServer := &http.Server{
		Handler: h2c.NewHandler(s.handlerMux, h2s_p2p), // Wrap handler with h2c
		Addr:    p2pBridgeListener.Addr().String(),
	}

	// Start rpc server
	// Configure the server for HTTP/2
	if err := http2.ConfigureServer(rpcServer, h2s_p2p); err != nil {
		return fmt.Errorf("failed to configure http2 for p2p server: %w", err)
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
		ServeWebStreamBridge(s.ctx, s.logger, s.handlerMux, stream)
	})
	s.logger.Info("Set libp2p stream handler for web stream envelope protocol", glog.LogFields{"protocolID": config.DRPC_WEB_STREAM_PROTOCOL_ID})

	return nil
}

// setupHTTPServer configures and starts the HTTP server
func (s *ServerInstance) setupHTTPServer(cfg *cfg, httpAddr string) error {
	// Create HTTP server with h2c support using the gateway handler
	h2s := &http2.Server{}
	httpServer := &http.Server{
		Handler: h2c.NewHandler(gateway.SetupHandler(s.handlerMux, cfg.logger, s.p2pHost), h2s),
		Addr:    httpAddr,
		// TLSConfig: nil, // TODO: later Take this value from server options
	}

	// Explicitly configure the server for HTTP/2 support via h2c
	if err := http2.ConfigureServer(httpServer, h2s); err != nil {
		return fmt.Errorf("failed to configure http2 server: %w", err)
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
			if s.httpListenerCh != nil {
				close(s.httpListenerCh) // Signal that listener failed
			}
			return
		}
		s.httpListener = l // Store the listener
		if s.httpListenerCh != nil {
			close(s.httpListenerCh) // Signal that listener is ready
		}
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
// Blocks until the HTTP server is listening or context is canceled.
func (s *ServerInstance) HTTPAddr() string {
	if s.httpServer == nil || s.httpListenerCh == nil {
		return "" // Return immediately if no HTTP server is configured
	}

	// Wait for the HTTP listener to be ready
	select {
	case <-s.httpListenerCh:
		if s.httpListener != nil {
			addr := s.httpListener.Addr().String()
			// Check if TLS/SSL is configured
			if s.httpServer.TLSConfig != nil {
				return "https://" + addr
			}
			return "http://" + addr
		}
	case <-s.ctx.Done():
		// Context was canceled
	}
	return ""
}

// P2PAddrs returns all p2p listening addresses
func (s *ServerInstance) P2PAddrs() []string {
	var addrs []string

	// Add p2p addresses
	if s.p2pHost != nil { // Check if p2pHost is initialized
		for _, addr := range s.p2pHost.Addrs() {
			addrs = append(addrs, addr.String()+"/p2p/"+s.p2pHost.ID().String())
		}
	}

	return addrs
}
