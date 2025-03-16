package drpc

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/omgolab/drpc/pkg/core"
	"github.com/omgolab/drpc/pkg/detach"
	"github.com/omgolab/drpc/pkg/gateway"
	"github.com/omgolab/drpc/pkg/proc"
	glog "github.com/omgolab/go-commons/pkg/log"
)

// Server represents a dual-protocol server (libp2p and HTTP)
type Server struct {
	p2pHost    host.Host
	p2pServer  *http.Server
	httpServer *http.Server
	ctx        context.Context // Context for server lifecycle management
	handler    http.Handler    // Handler for both HTTP and p2p
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

	// Start detached process if requested and not already detached
	if !isDetachedMode() && cfg.isDetachedServer {
		server := &Server{ctx: ctx}
		return server, server.startDetachedServer()
	}

	// Check if the server is already running in detached mode
	if isDetachedMode() {
		// print current cfg to log
		cfg.logger.Info("server started in detached mode", glog.LogFields{"cfg": cfg})

		// Check if a callback URL is provided
		callbackURL := ""
		for _, arg := range os.Args {
			if strings.HasPrefix(arg, detach.CALLBACK_FLAG) {
				callbackURL = strings.TrimPrefix(arg, detach.CALLBACK_FLAG)
				if err := detach.SendCallbackRequest(callbackURL); err != nil {
					// just warn, don't return error
					cfg.logger.Warn("failed to send callback request", glog.LogFields{"url": callbackURL, "error": err})
				}
				break
			}
		}
	}

	// Create server instance
	server := &Server{
		handler: connectRpcMuxHandler,
		ctx:     ctx,
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
func (s *Server) setupRpcLpBridgeServer(ctx context.Context, cfg *cfg) error {
	// Create libp2p host
	lh, err := core.CreateLibp2pHost(ctx, cfg.logger, cfg.libp2pOptions...)
	if err != nil {
		return fmt.Errorf("failed to create libp2p host: %w", err)
	}

	// Create libp2p to HTTP bridgeListener
	bridgeListener := core.NewStreamBridgeListener(ctx, lh, PROTOCOL_ID)

	s.p2pHost = lh

	// Create rpc server
	rpcServer := &http.Server{
		Handler: s.handler,
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
func (s *Server) setupHTTPServer(cfg *cfg, httpAddr string) error {
	// Create HTTP server with gateway routes
	httpServer := &http.Server{
		Handler: gateway.SetupHandler(s.handler, cfg.logger, s.p2pHost),
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
		if err := httpServer.Serve(l); err != nil && err != http.ErrServerClosed {
			cfg.logger.Error("http server error", err)
		}
	}()

	s.httpServer = httpServer
	return nil
}

// Close gracefully shuts down both servers and the libp2p host.
func (s *Server) Close() error {
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

// startDetachedServer starts the server in a detached process
func (s *Server) startDetachedServer() error {
	// Get the current process executable path
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	// Start the detached process using the detach package
	opts := []detach.Option{
		detach.WithExecutablePath(exePath),
		detach.WithLogFiles("server.log", "server.err"),
	}

	err = detach.StartDetachedProcess(context.Background(), opts...)
	if err != nil {
		return fmt.Errorf("failed to start detached process: %w", err)
	}

	fmt.Println("Server started in detached mode")
	return nil
}

// isDetachedMode checks if the current process is running in detached mode
// TODO: see if we can move this to the detach package
func isDetachedMode() bool {
	flag := detach.CALLBACK_FLAG[:len(detach.CALLBACK_FLAG)-1] // Remove trailing "="
	// Check if the process is running in detached mode
	for _, arg := range os.Args {
		if arg == flag {
			return true
		}
	}
	return false
}
