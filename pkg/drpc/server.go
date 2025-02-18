package drpc

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
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
	if !isDetachedMode() && cfg.detachedPredicateFunc != nil {
		server := &Server{ctx: ctx}
		return server, server.startDetachedServer(&cfg, "")
	}

	// Create server instance
	server := &Server{
		handler: connectRpcMuxHandler,
		ctx:     ctx,
	}

	// Setup P2P server
	if err := server.setupP2PServer(ctx, &cfg); err != nil {
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

// setupP2PServer configures and starts the P2P server
func (s *Server) setupP2PServer(ctx context.Context, cfg *cfg) error {
	// Create libp2p host
	lh, err := core.CreateLpHost(ctx, cfg.logger, cfg.libp2pOptions...)
	if err != nil {
		return fmt.Errorf("failed to create libp2p host: %w", err)
	}

	// Create libp2p listener
	listener := core.NewLpListener(ctx, lh, PROTOCOL_ID)

	s.p2pHost = lh

	// Create p2p server
	p2pServer := &http.Server{
		Handler: s.handler,
		Addr:    listener.Addr().String(),
	}

	// Start p2p server
	go func() {
		if err := p2pServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			cfg.logger.Error("p2p server error", err)
		}
	}()

	s.p2pServer = p2pServer
	return nil
}

// isDetachedMode checks if the current process is running in detached mode
func isDetachedMode() bool {
	for _, arg := range os.Args {
		if arg == "--detached" {
			return true
		}
	}
	return false
}

// setupHTTPServer configures and starts the HTTP server
func (s *Server) setupHTTPServer(cfg *cfg, httpAddr string) error {
	mux := http.NewServeMux()

	// Setup gateway and p2pinfo routes
	gateway.SetupRoutes(mux, s.handler, cfg.logger, s.p2pHost)

	// Create HTTP server
	httpServer := &http.Server{
		Handler: mux,
		Addr:    httpAddr,
	}

	// Check if port is in use and force close if needed
	if cfg.forceCloseExistingPort {
		if err := s.checkAndClosePort(httpAddr); err != nil {
			return err
		}
	}

	// Create listener
	var err error
	var listener net.Listener
	for i := 0; i < 3; i++ {
		listener, err = net.Listen("tcp", httpAddr)
		if err == nil {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if err != nil {
		return fmt.Errorf("failed to listen on HTTP port after 3 attempts: %w", err)
	}

	// Start server in goroutine
	go func() {
		if err := httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			cfg.logger.Error("http server error", err)
		}
	}()

	s.httpServer = httpServer
	return nil
}

// checkAndClosePort attempts to listen on a port and closes any existing process if needed
func (s *Server) checkAndClosePort(addr string) error {
	tempListener, err := net.Listen("tcp", addr)
	if err != nil {
		// Extract port from addr
		_, port, err := net.SplitHostPort(addr)
		if err != nil {
			return fmt.Errorf("invalid address format: %w", err)
		}

		if err := killPort(port); err != nil {
			return fmt.Errorf("failed to kill process on port %s: %w", port, err)
		}
	}

	if tempListener != nil {
		tempListener.Close()
	}
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

// killPort kills any process listening on the specified port.
// It supports both Windows and Unix-like systems.
func killPort(port string) error {
	if runtime.GOOS == "windows" {
		// Find the process ID using netstat
		findCmd := exec.Command("cmd", "/C",
			fmt.Sprintf(`netstat -ano | find "LISTENING" | find ":%s"`, port))
		output, err := findCmd.Output()
		if err != nil {
			return fmt.Errorf("failed to find process on port %s: %w", port, err)
		}

		// Parse the output to get PID
		// Output format: TCP    0.0.0.0:8080    0.0.0.0:0    LISTENING    1234
		lines := strings.Split(string(output), "\n")
		if len(lines) == 0 {
			return fmt.Errorf("no process found listening on port %s", port)
		}

		// Extract PID from the last column
		fields := strings.Fields(lines[0])
		if len(fields) < 5 {
			return fmt.Errorf("unexpected netstat output format")
		}
		pid := fields[len(fields)-1]

		// Kill the process
		killCmd := exec.Command("taskkill", "/F", "/PID", pid)
		if err := killCmd.Run(); err != nil {
			return fmt.Errorf("failed to kill process %s: %w", pid, err)
		}
		return nil
	}

	// Unix-like systems (macOS, Linux)
	command := fmt.Sprintf("lsof -i tcp:%s | grep LISTEN | awk '{print $2}' | xargs kill -9", port)
	cmd := exec.Command("bash", "-c", command)
	if output, err := cmd.CombinedOutput(); err != nil {
		// Check if the error is because no process was found
		if strings.Contains(string(output), "kill: no such process") {
			return fmt.Errorf("no process found listening on port %s", port)
		}
		return fmt.Errorf("failed to kill process on port %s: %w", port, err)
	}
	return nil
}

// startDetachedServer starts the server in a detached process
func (s *Server) startDetachedServer(cfg *cfg, exePath string) error {
	if cfg.detachedPredicateFunc == nil {
		return nil
	}

	// Get the current process executable path
	if exePath == "" {
		var err error
		exePath, err = os.Executable()
		if err != nil {
			return fmt.Errorf("failed to get executable path: %w", err)
		}
	}

	// Create a new detached process with the --detached flag
	args := []string{"--detached"}
	cmd := exec.Command(exePath, args...)

	// Detach the process and create new session
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}

	// Redirect output to files for logging
	logFile, err := os.OpenFile("server.log", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to create log file: %w", err)
	}
	defer logFile.Close()

	errFile, err := os.OpenFile("server.err", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to create error file: %w", err)
	}
	defer errFile.Close()

	cmd.Stdout = logFile
	cmd.Stderr = errFile

	// Start the process
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start detached process: %w", err)
	}

	// If a detached predicator is provided, block until it returns nil.
	err = cfg.detachedPredicateFunc(s)
	if err != nil {
		return fmt.Errorf("detached predicate failed: %w", err)
	}

	// Display recent log entries
	if content, err := os.ReadFile("server.log"); err == nil {
		fmt.Println("\nRecent server output:")
		fmt.Println(string(content))
	}
	if content, err := os.ReadFile("server.err"); err == nil && len(content) > 0 {
		fmt.Println("\nRecent server errors:")
		fmt.Println(string(content))
	}

	// Release the detached process.
	cmd.Process.Release()
	return nil
}
