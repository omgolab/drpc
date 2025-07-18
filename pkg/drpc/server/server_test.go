package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time" // Import time package

	"github.com/libp2p/go-libp2p"
	"github.com/omgolab/drpc/pkg/detach"
)

const testTimeout = 10 * time.Second // Define a reasonable timeout for tests

func TestBasicServerCreation(t *testing.T) {
	t.Parallel() // allow parallel execution
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "OK")
	})

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout) // Add timeout
	defer cancel()                                                        // Ensure cancellation
	server, err := New(ctx, mux)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	if server == nil {
		t.Fatal("Server is nil")
	}
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestWithOptions(t *testing.T) {
	//Removing t.parallel for now to avoid port conflicts
	//t.Parallel() // allow parallel execution
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "OK")
	})

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout) // Add timeout
	defer cancel()                                                        // Ensure cancellation
	server, err := New(
		ctx,
		mux,
		WithLibP2POptions(libp2p.NoListenAddrs),
		WithDisableHTTP(), // Disable HTTP server
	)
	if err != nil {
		t.Fatalf("Failed to create server with options: %v", err)
	}

	if server.IsHTTPRunning() {
		t.Error("HTTP server should be disabled")
	}

	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestP2PServerStart(t *testing.T) {
	t.Parallel() // allow parallel execution
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "OK")
	})

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout) // Add timeout
	defer cancel()                                                        // Ensure cancellation
	server, err := New(ctx, mux, WithLibP2POptions(libp2p.NoListenAddrs))
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	if server.GetP2PHost() == nil {
		t.Fatal("P2P host is nil")
	}
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestHTTPServerStart(t *testing.T) {
	//Removing t.parallel for now
	//t.Parallel() // allow parallel execution
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "OK")
	})

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout) // Add timeout
	defer cancel()                                                        // Ensure cancellation
	// Use WithHTTPPort(0) for dynamic port allocation
	server, err := New(ctx, mux, WithLibP2POptions(libp2p.NoListenAddrs), WithHTTPPort(0))
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	if !server.IsHTTPRunning() {
		t.Fatal("HTTP server should be running")
	}

	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestServerClose(t *testing.T) {
	t.Parallel() // allow parallel execution
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "OK")
	})

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout) // Add timeout
	defer cancel()                                                        // Ensure cancellation
	server, err := New(ctx, mux, WithLibP2POptions(libp2p.NoListenAddrs))
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	if err := server.Close(); err != nil {
		t.Fatalf("Failed to close server: %v", err)
	}
}

func TestAddressRetrieval(t *testing.T) {
	//Removing t.parallel
	//t.Parallel() // allow parallel execution
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "OK")
	})

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout) // Add timeout
	defer cancel()                                                        // Ensure cancellation
	// Use WithHTTPPort(0) for dynamic port allocation
	server, err := New(ctx, mux, WithLibP2POptions(libp2p.NoListenAddrs), WithHTTPPort(0))
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Add a small delay to allow the HTTP listener goroutine to start
	// This is a common pattern when testing asynchronous server startup.
	time.Sleep(100 * time.Millisecond)

	addrs := server.P2PAddrs()
	if len(addrs) != 0 {
		t.Error("Expected zero p2p addresses when NoListenAddrs is set")
	}

	if !strings.HasPrefix(server.HTTPAddr(), "http") {
		t.Error("HTTP address not found in server addresses")
	}

	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestDetachedMode(t *testing.T) {
	t.Parallel() // allow parallel execution
	t.Log("Starting TestDetachedMode")

	// Create a mux with a custom p2pinfo endpoint.
	mux := http.NewServeMux()
	t.Log("Created ServeMux")
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	})

	ctx := context.Background() // Use background context directly
	// Use WithHTTPPort(0) for dynamic port allocation
	server, err := New(ctx, mux,
		WithHTTPPort(0),
		WithDetachServer(detach.WithExitFunc(func(i int) {})), // Use the new option
	)
	if err != nil {
		t.Fatalf("Failed to create detached server: %v, server: %v", err, server)
	}
	t.Log("Created detached server")

	if err := server.Close(); err != nil {
		t.Fatalf("Failed to close server: %v", err)
	}
	t.Log("Closed server")
}
