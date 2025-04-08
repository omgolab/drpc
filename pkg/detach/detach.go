package detach

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	glog "github.com/omgolab/go-commons/pkg/log"
)

const (
	DETACHED_CALLBACK_URI = "DETACHED_CALLBACK_URI"
)

// DetachOption defines a functional option for configuring the detached process.
type DetachOption func(*config)

// config holds the configuration for the detached process.
type config struct {
	executablePath          string
	timeout                 time.Duration
	args                    []string
	stdoutPath              string
	stderrPath              string
	log                     glog.Logger
	onSuccessCallback       func(pid int) // Callback function when detached process starts successfully
	shouldContinueOnSuccess bool          // Whether to exit the parent process after successful detachment
	exitFunc                func(int)     // Function to call for exiting, defaults to os.Exit
}

// init automatically handles the callback if running in detached mode
func init() {
	if IsDetachedMode() {
		handleCallbackIfDetached()
	}
}

// WithExecutablePath sets the executable path explicitly.
func WithExecutablePath(path string) DetachOption {
	return func(c *config) {
		c.executablePath = path
	}
}

// WithTimeout sets the timeout for the callback.
func WithTimeout(duration time.Duration) DetachOption {
	return func(c *config) {
		c.timeout = duration
	}
}

// WithArgs adds additional arguments to the detached process command.
func WithArgs(args ...string) DetachOption {
	return func(c *config) {
		c.args = append(c.args, args...)
	}
}

// WithLogFiles sets the paths for stdout and stderr log files.
func WithLogFiles(stdoutPath, stderrPath string) DetachOption {
	return func(c *config) {
		c.stdoutPath = stdoutPath
		c.stderrPath = stderrPath
	}
}

// WithLogger sets the logger for logging messages.
func WithLogger(logger glog.Logger) DetachOption {
	return func(c *config) {
		c.log = logger
	}
}

// WithOnSuccess sets a callback function that is called when the detached process
// starts successfully. The pid of the detached process is passed to the callback.
func WithOnSuccess(callback func(pid int)) DetachOption {
	return func(c *config) {
		c.onSuccessCallback = callback
	}
}

// WithExitOnSuccess configures whether the parent process should exit after
// successfully starting the detached process.
func WithExitOnSuccess(exit bool) DetachOption {
	return func(c *config) {
		c.shouldContinueOnSuccess = !exit
	}
}

// WithExitFunc sets a custom exit function, primarily for testing.
func WithExitFunc(exitFunc func(int)) DetachOption {
	return func(cfg *config) {
		cfg.exitFunc = exitFunc
	}
}

// callbackHandled is used to track if callback has been handled (to avoid multiple callbacks)
var (
	callbackHandled bool
	callbackMutex   sync.Mutex
)

// newConfig creates a new configuration with default values and applies the provided options.
func newConfig(opts ...DetachOption) *config {
	// Default configuration
	logger, _ := glog.New()
	cfg := &config{
		timeout:    30 * time.Second, // Default timeout
		log:        logger,           // Default logger
		stdoutPath: "server.log",
		stderrPath: "server.err",
		exitFunc:   os.Exit, // Default exit function
	}

	for _, opt := range opts {
		opt(cfg)
	}

	return cfg
}

// IsDetachedMode checks if the current process is running in detached mode.
// Returns true if the process has the callback environment variable.
func IsDetachedMode() bool {
	return os.Getenv(DETACHED_CALLBACK_URI) != ""
}

// getCallbackURL extracts the callback URL from the environment variable.
// Returns empty string if no callback URL is found.
func getCallbackURL() string {
	return os.Getenv(DETACHED_CALLBACK_URI)
}

// handleCallbackIfDetached processes the callback URL if running in detached mode.
// This is called automatically by the init function when in detached mode.
func handleCallbackIfDetached(opts ...DetachOption) {
	callbackURL := getCallbackURL()
	if callbackURL == "" {
		return
	}

	cfg := newConfig(opts...)
	callbackMutex.Lock()
	defer callbackMutex.Unlock()

	if callbackHandled {
		return
	}

	cfg.log.Info("handling callback in detached mode", map[string]any{"url": callbackURL})

	if err := sendCallbackRequest(callbackURL); err != nil {
		cfg.log.Warn("failed to send callback request", map[string]interface{}{"url": callbackURL, "error": err})
	}

	callbackHandled = true
}

// StartDetached starts the application in a detached process.
// It first checks if already running in detached mode and returns immediately if so.
// Returns an error if the detached process couldn't be started.
func StartDetached(ctx context.Context, opts ...DetachOption) (bool, error) {
	// Check if already in detached mode
	if IsDetachedMode() {
		cfg := newConfig(opts...)
		cfg.log.Info("Already running in detached mode", nil)
		return true, nil
	}

	// If no executable path provided, add current executable as default
	if len(opts) == 0 || !hasExecutablePath(opts) {
		exePath, err := os.Executable()
		if err != nil {
			return false, fmt.Errorf("failed to get executable path: %w", err)
		}
		opts = append(opts, WithExecutablePath(exePath))
	}

	// Start the server in detached mode
	cfg := newConfig(opts...)
	pid, err := startDetachedProcess(ctx, cfg)
	if err != nil {
		return false, fmt.Errorf("failed to start server in detached mode: %w", err)
	}

	cfg.log.Info("Server started in detached mode", map[string]any{"pid": pid})

	// Call the success callback if provided
	if cfg.onSuccessCallback != nil {
		cfg.onSuccessCallback(pid)
	}

	// Exit if configured to do so
	if !cfg.shouldContinueOnSuccess {
		cfg.exitFunc(0) // Use the configured exit function
	}

	return true, nil
}

// hasExecutablePath checks if the executable path option is provided
func hasExecutablePath(opts []DetachOption) bool {
	// Create a test config to check if any option sets the executable path
	testCfg := &config{}
	for _, opt := range opts {
		opt(testCfg)
	}
	return testCfg.executablePath != ""
}

// startDetachedProcess starts the current application in a detached process.
// Returns the PID of the started process and any error that occurred.
func startDetachedProcess(ctx context.Context, cfg *config) (int, error) {
	// Verify we have an executable path
	if cfg.executablePath == "" {
		var err error
		cfg.executablePath, err = os.Executable()
		if err != nil {
			return 0, fmt.Errorf("failed to get executable path: %w", err)
		}
	}

	// Set up callback HTTP server
	done := make(chan struct{})
	server, listener, err := setupCallbackServer(done)
	if err != nil {
		return 0, err
	}
	defer gracefulShutdown(server, cfg.log)

	callbackURL := fmt.Sprintf("http://%s", listener.Addr().String())
	cfg.log.Info("Created callback URL", map[string]any{"url": callbackURL})

	// Configure and start the process
	pid, err := startProcess(ctx, callbackURL, cfg)
	if err != nil {
		return 0, err
	}

	// Wait for callback or timeout
	if err := waitForCallback(ctx, cfg.timeout, done); err != nil {
		return 0, err
	}

	// Release the process
	if err := releaseProcess(pid); err != nil {
		return pid, err
	}

	return pid, nil
}

// setupCallbackServer creates an HTTP server for receiving the callback
func setupCallbackServer(done chan struct{}) (*http.Server, net.Listener, error) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		close(done)
	})

	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create listener: %w", err)
	}

	srv := &http.Server{Handler: mux}
	go func() {
		if err := srv.Serve(listener); err != nil && err != http.ErrServerClosed {
			// We can't log here since we don't have access to the logger
		}
	}()

	return srv, listener, nil
}

// gracefulShutdown attempts to shut down the HTTP server gracefully
func gracefulShutdown(server *http.Server, log glog.Logger) {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Warn("Error shutting down server", map[string]any{"error": err})
	}
}

// startProcess creates and starts the detached process
func startProcess(ctx context.Context, callbackURL string, cfg *config) (int, error) {
	cmd := exec.CommandContext(ctx, cfg.executablePath, cfg.args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Env = append(os.Environ(), fmt.Sprintf("%s=%s", DETACHED_CALLBACK_URI, callbackURL))

	// Set up stdout and stderr files
	if err := setupOutputFiles(cmd, cfg); err != nil {
		return 0, err
	}

	// Start the command
	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("failed to start detached process: %w", err)
	}

	pid := cmd.Process.Pid
	cfg.log.Info("Started detached process", map[string]any{"pid": pid})
	return pid, nil
}

// setupOutputFiles configures stdout and stderr output for the detached process
func setupOutputFiles(cmd *exec.Cmd, cfg *config) error {
	if cfg.stdoutPath != "" {
		stdoutFile, err := os.OpenFile(cfg.stdoutPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
		if err != nil {
			return fmt.Errorf("failed to create stdout log file: %w", err)
		}
		cmd.Stdout = stdoutFile
		// Note: we don't defer close as the file will be used by the process
	}

	if cfg.stderrPath != "" {
		stderrFile, err := os.OpenFile(cfg.stderrPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
		if err != nil {
			return fmt.Errorf("failed to create stderr log file: %w", err)
		}
		cmd.Stderr = stderrFile
		// Note: we don't defer close as the file will be used by the process
	}

	return nil
}

// waitForCallback waits for the callback from the detached process or times out
func waitForCallback(ctx context.Context, timeout time.Duration, done chan struct{}) error {
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	select {
	case <-timeoutCtx.Done():
		return fmt.Errorf("timeout waiting for callback from detached process")
	case <-done:
		return nil
	}
}

// releaseProcess releases the given process
func releaseProcess(pid int) error {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("failed to find process: %w", err)
	}

	if err := proc.Release(); err != nil {
		return fmt.Errorf("failed to release process: %w", err)
	}

	return nil
}

// sendCallbackRequest sends a GET request to the callback URL and returns an error if it fails
func sendCallbackRequest(callbackURL string) error {
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
