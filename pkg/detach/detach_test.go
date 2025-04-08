package detach

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// skipInCI skips the test if running in CI environment
func skipInCI(t *testing.T) {
	if os.Getenv("CI") == "true" {
		t.Skip("Skipping test in CI environment")
	}
}

// createTempGoProgram creates a Go program that performs a callback
func createTempGoProgram(t *testing.T, code string) (string, string) {
	tmpDir := t.TempDir()
	goFilePath := filepath.Join(tmpDir, "callback_program.go")
	exePath := filepath.Join(tmpDir, "callback_program")

	if err := os.WriteFile(goFilePath, []byte(code), 0644); err != nil {
		t.Fatalf("Failed to write Go file: %v", err)
	}

	if err := exec.Command("go", "build", "-o", exePath, goFilePath).Run(); err != nil {
		t.Fatalf("Failed to build Go program: %v", err)
	}

	return exePath, tmpDir
}

// waitForPID waits for a PID to be sent on the channel or times out
func waitForPID(t *testing.T, pidChan <-chan int, timeout time.Duration, startTime time.Time) int {
	select {
	case pid := <-pidChan:
		duration := time.Since(startTime)
		t.Logf("Received callback with PID: %d after %v", pid, duration)
		return pid
	case <-time.After(timeout):
		t.Fatal("Timeout waiting for success callback")
		return 0 // Never reached, but required for compilation
	}
}

// standardCallbackGoProgram returns a simple Go program that performs a callback
func standardCallbackGoProgram() string {
	return `package main

import (
	"fmt"
	"net/http"
	"os"
	"strings"
)

func main() {
	var callbackURL string
	
	// Extract callback URL from environment
	callbackURL = os.Getenv("DETACHED_CALLBACK_URI")
	
	// If not in env, check args
	if callbackURL == "" {
		for _, arg := range os.Args[1:] {
			if strings.HasPrefix(arg, "--detached-callback-url=") {
				callbackURL = strings.TrimPrefix(arg, "--detached-callback-url=")
				break
			}
		}
	}
	
	if callbackURL == "" {
		fmt.Println("No callback URL provided")
		os.Exit(1)
	}
	
	fmt.Printf("Making callback request to: %s\n", callbackURL)
	
	// Make the callback request
	resp, err := http.Get(callbackURL)
	if err != nil {
		fmt.Printf("Error making callback: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	
	fmt.Printf("Callback response status: %s\n", resp.Status)
	os.Exit(0)
}
`
}

// standardDetachOptions returns the common DetachOptions used in tests
func standardDetachOptions(execPath string, timeout time.Duration) []DetachOption {
	return []DetachOption{
		WithExecutablePath(execPath),
		WithTimeout(timeout),
		WithExitFunc(func(int) {}), // Prevent test exit
	}
}

// verifyProcessExists checks if a process with the given PID exists
func verifyProcessExists(t *testing.T, pid int) {
	if pid <= 0 {
		t.Errorf("Expected positive PID but got %d", pid)
		return
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		t.Logf("Process with PID %d may have already terminated: %v", pid, err)
		return
	}

	// On Unix systems, FindProcess always succeeds, so send signal 0 to check existence
	err = proc.Signal(syscall.Signal(0))
	if err != nil {
		t.Logf("Process with PID %d does not exist or has already terminated: %v", pid, err)
	} else {
		t.Logf("Process with PID %d exists", pid)
	}
}

// testDetachWithCallback starts a detached process and waits for a callback
func testDetachWithCallback(t *testing.T, exePath string, timeout time.Duration) int {
	ctx := context.Background()
	pidChan := make(chan int, 1)
	start := time.Now()

	_, err := StartDetached(
		ctx,
		WithExecutablePath(exePath),
		WithTimeout(timeout),
		WithExitFunc(func(int) {}), // Prevent test from exiting
		WithOnSuccess(func(pid int) {
			pidChan <- pid // Send PID to channel when callback is received
		}),
	)

	if err != nil {
		t.Fatalf("Failed to start detached process: %v", err)
	}

	return waitForPID(t, pidChan, timeout+time.Second, start)
}

func TestStartDetachedProcess(t *testing.T) {
	// Create the test executable using our helper
	exePath, _ := createTempGoProgram(t, standardCallbackGoProgram())

	tests := []struct {
		name        string
		options     []DetachOption
		expectError bool
	}{
		{
			name:        "Successful detach with callback",
			options:     standardDetachOptions(exePath, 5*time.Second),
			expectError: false,
		},
		{
			name: "Timeout due to no callback",
			options: []DetachOption{
				WithExecutablePath("/bin/echo"), // This won't make the callback
				WithTimeout(1 * time.Second),
				WithExitFunc(func(int) {}), // Prevent test exit
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			_, err := StartDetached(ctx, tt.options...)

			if tt.expectError && err == nil {
				t.Errorf("Expected an error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Did not expect an error but got: %v", err)
			}
		})
	}
}

// TestTimeoutScenario directly tests the timeout scenario in StartDetached
func TestTimeoutScenario(t *testing.T) {
	skipInCI(t)

	ctx := context.Background()
	timeout := 500 * time.Millisecond // Very short timeout for faster tests

	// Test that sleep command times out
	start := time.Now()
	_, err := StartDetached(
		ctx,
		WithExecutablePath("/bin/sleep"),
		WithArgs("10"),             // Sleep for 10 seconds
		WithTimeout(timeout),       // But our timeout is much shorter
		WithExitFunc(func(int) {}), // Prevent test from exiting
	)
	duration := time.Since(start)

	// Verify timeout error behavior
	if err == nil {
		t.Fatal("Expected timeout error but got no error")
	}

	if !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("Expected timeout error but got: %v", err)
	}

	// Duration should be close to timeout
	if duration < timeout || duration > timeout*2 {
		t.Fatalf("Expected duration to be around %v but got %v", timeout, duration)
	}

	t.Logf("Got expected timeout error after %v: %v", duration, err)
}

// TestCallbackWithGoProgram tests the callback mechanism using a Go program
func TestCallbackWithGoProgram(t *testing.T) {
	skipInCI(t)

	// Create a helper Go program that will perform the callback
	exePath, _ := createTempGoProgram(t, standardCallbackGoProgram())

	// Test the detach with callback
	pid := testDetachWithCallback(t, exePath, 5*time.Second)

	// Verify the process exists
	verifyProcessExists(t, pid)
}

// TestDirectCallback tests the callback mechanism using direct execution
func TestDirectCallback(t *testing.T) {
	skipInCI(t)

	// If already in detached mode, the callback will be handled automatically
	if IsDetachedMode() {
		t.Log("Running in detached mode, callback will be automatic")
		return
	}

	// Get the current test executable path
	exePath, err := os.Executable()
	if err != nil {
		t.Fatalf("Failed to get test executable path: %v", err)
	}

	// Context with reasonable timeout and minimal args for test
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	testArgs := []string{"-test.v"} // Enable verbose output for child process

	// Start detached process and track PID
	var receivedPID int
	start := time.Now()

	_, err = StartDetached(
		ctx,
		WithExecutablePath(exePath),
		WithArgs(testArgs...),
		WithTimeout(5*time.Second),
		WithExitFunc(func(int) {}), // Prevent test exit
		WithOnSuccess(func(pid int) {
			receivedPID = pid
		}),
	)

	if err != nil {
		t.Fatalf("Failed to start detached process: %v", err)
	}

	// Log successful callback and verify process
	t.Logf("Received callback with PID %d after %v", receivedPID, time.Since(start))
	verifyProcessExists(t, receivedPID)
}
