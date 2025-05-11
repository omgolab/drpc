package drpc_integration_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"connectrpc.com/connect"
	gv1 "github.com/omgolab/drpc/demo/gen/go/greeter/v1"
	gv1connect "github.com/omgolab/drpc/demo/gen/go/greeter/v1/greeterv1connect"
	glog "github.com/omgolab/go-commons/pkg/log"
)

const (
	utilServerBaseURL            = "http://localhost:8080"
	publicNodeEndpoint           = "/public-node"
	relayNodeEndpoint            = "/relay-node"
	gatewayNodeEndpoint          = "/gateway-node"
	gatewayRelayNodeEndpoint     = "/gateway-relay-node"
	gatewayAutoRelayNodeEndpoint = "/gateway-auto-relay-node"
	DefaultTimeout               = 30 * time.Second
)

// NodeInfo holds the addresses returned by the utility server.
type NodeInfo struct {
	HTTPAddress string `json:"http_address"`
	Libp2pMA    string `json:"libp2p_ma"`
}

// UtilServerHelper manages the utility server process and client interactions.
type UtilServerHelper struct {
	logger       glog.Logger
	cmd          *exec.Cmd
	serverOutput io.ReadCloser
	serverError  io.ReadCloser
	httpClient   *http.Client
	mu           sync.Mutex
	resources    []string // Could store PIDs or other identifiers if needed for more complex cleanup
	serverReady  chan bool
}

// NewUtilServerHelper creates and starts a new utility server helper.
// It will attempt to start the server and wait for it to be ready.
func NewUtilServerHelper(utilServerPath string) *UtilServerHelper {
	l, _ := glog.New()
	helper := &UtilServerHelper{
		logger:      l,
		httpClient:  &http.Client{Timeout: 10 * time.Second},
		serverReady: make(chan bool, 1),
	}

	if utilServerPath == "" {
		utilServerPath = "cmd/util-server/main.go" // Default path relative to project root
	}

	// Start the server in a goroutine so we can wait for it to be ready
	go helper.startServer(utilServerPath)

	// Wait for the server to be ready or timeout
	select {
	case <-helper.serverReady:
		l.Printf("Utility server started successfully.")
	case <-time.After(20 * time.Second): // Increased timeout for server start
		helper.StopServer() // Attempt to clean up if server didn't start
		l.Fatal("Utility server failed to start within the timeout period.", fmt.Errorf("server start timeout"))
	}

	return helper
}

func (h *UtilServerHelper) startServer(utilServerPath string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.cmd != nil && h.cmd.Process != nil {
		h.logger.Printf("Utility server already running or start attempt in progress.")
		return
	}

	h.logger.Printf("Starting utility server from: %s", utilServerPath)
	// Use "go run" for simplicity, can be changed to build and run binary
	h.cmd = exec.Command("go", "run", utilServerPath)

	var err error
	h.serverOutput, err = h.cmd.StdoutPipe()
	if err != nil {
		h.logger.Printf("Error creating stdout pipe for utility server: %v", err)
		h.serverReady <- false // Signal failure
		return
	}
	h.serverError, err = h.cmd.StderrPipe()
	if err != nil {
		h.logger.Printf("Error creating stderr pipe for utility server: %v", err)
		h.serverReady <- false // Signal failure
		return
	}

	if err := h.cmd.Start(); err != nil {
		h.logger.Printf("Error starting utility server: %v", err)
		h.serverReady <- false // Signal failure
		return
	}
	h.logger.Printf("Utility server process started with PID: %d", h.cmd.Process.Pid)
	h.resources = append(h.resources, fmt.Sprintf("PID:%d", h.cmd.Process.Pid))

	// Goroutine to log server output
	go h.logStream(h.serverOutput, "UTIL_SERVER_STDOUT")
	go h.logStream(h.serverError, "UTIL_SERVER_STDERR")

	// Check for server readiness
	go h.checkServerReady()
}

func (h *UtilServerHelper) logStream(reader io.ReadCloser, prefix string) {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		h.logger.Printf("[%s] %s", prefix, scanner.Text())
	}
	if err := scanner.Err(); err != nil && err != io.EOF {
		h.logger.Printf("Error reading from %s: %v", prefix, err)
	}
}

func (h *UtilServerHelper) checkServerReady() {
	// Ping a known endpoint (e.g., /health or one of the node endpoints)
	// For now, we'll assume one of the node endpoints will respond when ready.
	// A dedicated /health endpoint would be better.
	for range 20 { // Try for up to 10 seconds (20 * 500ms)
		// Using public-node as a health check, assuming it's always available
		resp, err := h.httpClient.Get(utilServerBaseURL + publicNodeEndpoint)
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			h.serverReady <- true
			return
		}
		if resp != nil {
			resp.Body.Close()
		}
		time.Sleep(500 * time.Millisecond)
	}
	h.logger.Printf("Utility server did not become ready at %s", utilServerBaseURL)
	h.serverReady <- false // Signal failure if not ready
}

// StopServer stops the utility server process.
func (h *UtilServerHelper) StopServer() {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.cmd == nil || h.cmd.Process == nil {
		h.logger.Printf("Utility server process not found or already stopped.")
		return
	}

	h.logger.Printf("Stopping utility server PID: %d", h.cmd.Process.Pid)
	if err := h.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		// Fallback to Kill if Signal fails (e.g. on Windows or if process is stuck)
		h.logger.Printf("Error sending SIGTERM to utility server: %v. Attempting to kill.", err)
		if errKill := h.cmd.Process.Kill(); errKill != nil {
			h.logger.Printf("Error stopping utility server (SIGTERM failed, Kill also failed): %v, %v", err, errKill)
		} else {
			h.logger.Printf("Utility server killed successfully after SIGTERM failed.")
		}
	} else {
		h.logger.Printf("Utility server signaled to stop (SIGTERM).")
	}

	// Wait for the process to exit to ensure cleanup
	go func() {
		if h.cmd != nil && h.cmd.ProcessState == nil { // Check if process hasn't exited yet
			_, err := h.cmd.Process.Wait()
			if err != nil {
				if !strings.Contains(err.Error(), "signal: killed") && !strings.Contains(err.Error(), "exit status 1") {
					h.logger.Printf("Error waiting for utility server to stop: %v", err)
				}
			}
		}
	}()

	if h.serverOutput != nil {
		h.serverOutput.Close()
	}
	if h.serverError != nil {
		h.serverError.Close()
	}
	h.cmd = nil // Mark as stopped
	h.logger.Printf("Utility server resources cleaned up.")
}

func (h *UtilServerHelper) getNodeInfo(endpoint string) (NodeInfo, error) {
	var info NodeInfo
	serverURL := fmt.Sprintf("%s%s", utilServerBaseURL, endpoint)
	h.logger.Printf("Requesting node info from: %s", serverURL)

	resp, err := h.httpClient.Get(serverURL)
	if err != nil {
		return info, fmt.Errorf("error making GET request to %s: %w", serverURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return info, fmt.Errorf("received non-OK status code %d from %s: %s", resp.StatusCode, serverURL, string(bodyBytes))
	}

	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return info, fmt.Errorf("error decoding JSON response from %s: %w", serverURL, err)
	}

	h.logger.Printf("Received node info from %s: %+v", serverURL, info)
	return info, nil
}

// GetPublicNodeInfo retrieves information for a public node.
func (h *UtilServerHelper) GetPublicNodeInfo() (NodeInfo, error) {
	info, err := h.getNodeInfo(publicNodeEndpoint)
	if err != nil {
		return info, fmt.Errorf("failed to get public node info from %s: %w", publicNodeEndpoint, err)
	}
	if info.HTTPAddress == "" {
		h.logger.Fatal("error:", fmt.Errorf("public node info from %s is missing HTTPAddress", publicNodeEndpoint))
	}
	if info.Libp2pMA == "" {
		h.logger.Fatal("error:", fmt.Errorf("public node info from %s is missing Libp2pMA", publicNodeEndpoint))
	}
	return info, nil
}

// GetRelayNodeInfo retrieves information for a relay node.
func (h *UtilServerHelper) GetRelayNodeInfo() (NodeInfo, error) {

	info, err := h.getNodeInfo(relayNodeEndpoint)
	if err != nil {
		return info, fmt.Errorf("failed to get relay node info from %s: %w", relayNodeEndpoint, err)
	}
	if info.Libp2pMA == "" {
		h.logger.Fatal("", fmt.Errorf("relay node info from %s is missing Libp2pMA", relayNodeEndpoint))
	}
	return info, nil
}

// GetGatewayNodeInfo retrieves information for a gateway node.
func (h *UtilServerHelper) GetGatewayNodeInfo() (NodeInfo, error) {

	info, err := h.getNodeInfo(gatewayNodeEndpoint)
	if err != nil {
		return info, fmt.Errorf("failed to get gateway node info from %s: %w", gatewayNodeEndpoint, err)
	}
	if info.HTTPAddress == "" {
		h.logger.Fatal("error:", fmt.Errorf("gateway node info from %s is missing HTTPAddress", gatewayNodeEndpoint))
	}
	return info, nil
}

// GetGatewayRelayNodeInfo retrieves information for a gateway relay node.
func (h *UtilServerHelper) GetGatewayRelayNodeInfo() (NodeInfo, error) {

	info, err := h.getNodeInfo(gatewayRelayNodeEndpoint)
	if err != nil {
		return info, fmt.Errorf("failed to get gateway relay node info from %s: %w", gatewayRelayNodeEndpoint, err)
	}
	if info.HTTPAddress == "" {
		h.logger.Fatal("error:", fmt.Errorf("gateway relay node info from %s is missing HTTPAddress", gatewayRelayNodeEndpoint))
	}
	return info, nil
}

// GetGatewayAutoRelayNodeInfo retrieves information for a gateway auto relay node.
func (h *UtilServerHelper) GetGatewayAutoRelayNodeInfo() (NodeInfo, error) {

	info, err := h.getNodeInfo(gatewayAutoRelayNodeEndpoint)
	if err != nil {
		return info, fmt.Errorf("failed to get gateway auto relay node info from %s: %w", gatewayAutoRelayNodeEndpoint, err)
	}
	if info.HTTPAddress == "" {
		h.logger.Fatal("error:", fmt.Errorf("gateway auto relay node info from %s is missing HTTPAddress", gatewayAutoRelayNodeEndpoint))
	}
	return info, nil
}

// TestClientUnaryRequest performs a unary RPC call (SayHello) and verifies the response.
// It abstracts the common logic for testing unary requests.
func TestClientUnaryRequest(t *testing.T, client gv1connect.GreeterServiceClient, name string, timeout time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req := connect.NewRequest(&gv1.SayHelloRequest{Name: name})
	resp, err := client.SayHello(ctx, req)
	if err != nil {
		t.Fatalf("Failed to call SayHello (name: %s): %v", name, err)
	}

	want := "Hello, " + name + "!"
	if resp.Msg.Message != want {
		t.Errorf("Unexpected greeting: got %q, want %q (name: %s)", resp.Msg.Message, want, name)
	}
}

// TestClientStreamingRequest performs a bidirectional streaming RPC call (BidiStreamingEcho) and verifies the responses.
// It abstracts the common logic for testing streaming requests.
func TestClientStreamingRequest(t *testing.T, client gv1connect.GreeterServiceClient, names []string, timeout time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	stream := client.BidiStreamingEcho(ctx)

	for _, name := range names {
		if err := stream.Send(&gv1.BidiStreamingEchoRequest{Name: name}); err != nil {
			t.Fatalf("Failed to send to stream (name: %s): %v", name, err)
		}
	}
	if err := stream.CloseRequest(); err != nil {
		t.Fatalf("Failed to close request stream: %v", err)
	}

	received := make(map[string]bool)
	receivedCount := 0
	expectedCount := len(names)

	// Process each response like the server processes requests - with a loop that checks for EOF
	for {
		resp, err := stream.Receive()

		// Check if we've reached the end of the stream
		if errors.Is(err, io.EOF) {
			// We should have all expected responses before EOF
			if receivedCount != expectedCount {
				t.Errorf("Stream ended with EOF after only %d/%d responses", receivedCount, expectedCount)
			} else {
				t.Logf("Received expected EOF after all %d responses", expectedCount)
			}
			break
		}

		// Handle other errors
		if err != nil {
			t.Fatalf("Failed to receive from stream (after %d/%d receives): %v", receivedCount, expectedCount, err)
		}

		// Process the response
		if resp == nil {
			t.Fatalf("Received nil response from stream (after %d/%d receives)", receivedCount, expectedCount)
		}

		// Store the greeting we received
		received[resp.Greeting] = true
		receivedCount++

		// If we've received all expected responses, we can optionally break early
		// This is a safety measure to avoid infinite loops if the server sends more than expected
		if receivedCount >= expectedCount {
			// Try one more receive to check if the stream is properly closed
			_, err := stream.Receive()
			if errors.Is(err, io.EOF) {
				t.Logf("Stream properly closed with EOF after receiving all %d responses", expectedCount)
			} else if err != nil {
				t.Logf("Stream closed with unexpected error after receiving all responses: %v", err)
			} else {
				t.Errorf("Expected EOF after %d responses, but received more responses", expectedCount)
			}
			break
		}
	}

	// Verify we received all expected greetings
	for _, name := range names {
		expected := "Hello, " + name + "!"
		if !received[expected] {
			t.Errorf("Missing greeting for %q. Received: %v", name, received)
		}
	}
}
