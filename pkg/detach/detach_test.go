package detach

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestStartDetachedProcess(t *testing.T) {
	// Create a simple test executable that makes an HTTP request
	testScript := `package main
import (
    "fmt"
    "net/http"
    "os"
    "strings"
)
func main() {
    var callbackURL string
    for _, arg := range os.Args[1:] {
        if strings.HasPrefix(arg, "--detached-callback-url=") {
            callbackURL = strings.TrimPrefix(arg, "--detached-callback-url=")
            break
        }
    }
    if callbackURL != "" {
        resp, err := http.Get(callbackURL)
        if err != nil {
            fmt.Printf("Error making callback: %v\n", err)
            os.Exit(1)
        }
        resp.Body.Close()
    }
}`

	// Create a temporary file for the test executable
	tmpDir := t.TempDir()
	testFile := tmpDir + "/test.go"
	if err := os.WriteFile(testFile, []byte(testScript), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	// Build the test executable
	testExe := tmpDir + "/test"
	if err := exec.Command("go", "build", "-o", testExe, testFile).Run(); err != nil {
		t.Fatalf("Failed to build test executable: %v", err)
	}

	tests := []struct {
		name        string
		options     []Option
		expectError bool
	}{
		{
			name: "Successful detach with callback",
			options: []Option{
				WithExecutablePath(testExe),
				WithTimeout(5 * time.Second),
			},
			expectError: false,
		},
		{
			name: "Timeout due to no callback",
			options: []Option{
				WithExecutablePath("/bin/echo"), // This won't make the callback
				WithTimeout(1 * time.Second),
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			err := StartDetachedProcess(ctx, tt.options...)

			if tt.expectError && err == nil {
				t.Errorf("Expected an error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Did not expect an error but got: %v", err)
			}
		})
	}
}
