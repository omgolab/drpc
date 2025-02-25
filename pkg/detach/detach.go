package detach

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// TODO: Detachment should 2 types of callback; push (current)  or pull(we call the callback url for a time till we give up).

const CALLBACK_FLAG = "--detached-callback-url="

// Option defines a functional option for configuring the detached process.
type Option func(*config)

// config holds the configuration for the detached process.
type config struct {
	executablePath string
	timeout        time.Duration
	args           []string
	stdoutPath     string
	stderrPath     string
}

// WithExecutablePath sets the executable path explicitly.
func WithExecutablePath(path string) Option {
	return func(c *config) {
		c.executablePath = path
	}
}

// WithTimeout sets the timeout for the callback.
func WithTimeout(duration time.Duration) Option {
	return func(c *config) {
		c.timeout = duration
	}
}

// WithArgs adds additional arguments to the detached process command.
func WithArgs(args ...string) Option {
	return func(c *config) {
		c.args = append(c.args, args...)
	}
}

// WithLogFiles sets the paths for stdout and stderr log files.
func WithLogFiles(stdoutPath, stderrPath string) Option {
	return func(c *config) {
		c.stdoutPath = stdoutPath
		c.stderrPath = stderrPath
	}
}

// StartDetachedProcess starts the current application in a detached process.
func StartDetachedProcess(ctx context.Context, opts ...Option) error {
	cfg := &config{
		timeout: 10 * time.Second, // Default timeout
	}

	for _, opt := range opts {
		opt(cfg)
	}

	if cfg.executablePath == "" {
		var err error
		cfg.executablePath, err = os.Executable()
		if err != nil {
			return fmt.Errorf("failed to get executable path: %w", err)
		}
	}

	// Create HTTP server to handle the callback
	mux := http.NewServeMux()
	done := make(chan struct{})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		close(done)
	})

	// Start server on a random available port
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return fmt.Errorf("failed to create listener: %w", err)
	}

	srv := &http.Server{
		Handler: mux,
	}

	// Start server in a goroutine
	go func() {
		if err := srv.Serve(listener); err != nil && err != http.ErrServerClosed {
			fmt.Printf("Server error: %v\n", err)
		}
	}()

	// Ensure server is closed when we're done
	defer func() {
		if err := srv.Shutdown(context.Background()); err != nil {
			fmt.Printf("Error shutting down server: %v\n", err)
		}
	}()

	// Get the actual callback URL
	callbackURL := fmt.Sprintf("http://%s", listener.Addr().String())

	// Add callback URL to args
	args := append(cfg.args, CALLBACK_FLAG+callbackURL)
	cmd := exec.CommandContext(ctx, cfg.executablePath, args...)

	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}

	if cfg.stdoutPath != "" {
		stdoutFile, err := os.OpenFile(cfg.stdoutPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
		if err != nil {
			return fmt.Errorf("failed to create stdout log file: %w", err)
		}
		defer stdoutFile.Close()
		cmd.Stdout = stdoutFile
	}

	if cfg.stderrPath != "" {
		stderrFile, err := os.OpenFile(cfg.stderrPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
		if err != nil {
			return fmt.Errorf("failed to create stderr log file: %w", err)
		}
		defer stderrFile.Close()
		cmd.Stderr = stderrFile
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start detached process: %w", err)
	}

	// Create timeout context for waiting
	timeoutCtx, cancel := context.WithTimeout(ctx, cfg.timeout)
	defer cancel()

	// Wait for either callback or timeout
	select {
	case <-timeoutCtx.Done():
		return fmt.Errorf("timeout waiting for callback from detached process")
	case <-done:
		return cmd.Process.Release()
	}
}

func SendCallbackRequest(callbackURL string) error {
	req, err := http.NewRequest(http.MethodGet, callbackURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	return nil
}
