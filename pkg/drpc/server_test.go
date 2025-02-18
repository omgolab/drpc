package drpc

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/libp2p/go-libp2p"
)

func TestBasicServerCreation(t *testing.T) {
	t.Parallel() // allow parallel execution
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "OK")
	})

	ctx := context.Background()
	server, err := NewServer(ctx, mux)
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
	t.Parallel() // allow parallel execution
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "OK")
	})

	ctx := context.Background()
	server, err := NewServer(
		ctx,
		mux,
		WithLibP2POptions(libp2p.NoListenAddrs),
	)
	if err != nil {
		t.Fatalf("Failed to create server with options: %v", err)
	}

	if server.httpServer != nil {
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

	ctx := context.Background()
	server, err := NewServer(ctx, mux, WithLibP2POptions(libp2p.NoListenAddrs))
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	if server.p2pHost == nil {
		t.Fatal("P2P host is nil")
	}
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestHTTPServerStart(t *testing.T) {
	t.Parallel() // allow parallel execution
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "OK")
	})

	ctx := context.Background()
	server, err := NewServer(ctx, mux, WithLibP2POptions(libp2p.NoListenAddrs))
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	if server.httpServer == nil {
		t.Fatal("HTTP server is nil")
	}

	resp, err := http.Get("http://" + server.httpServer.Addr)
	if err != nil {
		t.Fatalf("Failed to connect to HTTP server: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status OK, got %v", resp.StatusCode)
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

	ctx := context.Background()
	server, err := NewServer(ctx, mux, WithLibP2POptions(libp2p.NoListenAddrs))
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	if err := server.Close(); err != nil {
		t.Fatalf("Failed to close server: %v", err)
	}
}

func TestAddressRetrieval(t *testing.T) {
	t.Parallel() // allow parallel execution
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "OK")
	})

	ctx := context.Background()
	server, err := NewServer(ctx, mux, WithLibP2POptions(libp2p.NoListenAddrs), WithHTTPPort(8081))
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	addrs := server.Addrs()
	if len(addrs) == 0 {
		t.Error("No addresses returned")
	}

	httpFound := false
	for _, addr := range addrs {
		if strings.HasPrefix(addr, "http://") {
			httpFound = true
			break
		}
	}
	if !httpFound {
		t.Error("HTTP address not found in server addresses")
	}

	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestDetachedMode(t *testing.T) {
	t.Parallel() // allow parallel execution

	// Create a mux with a custom p2pinfo endpoint.
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	})

	ctx := context.Background()
	// WithDetachedPredicator is used to block until the server is ready.
	server, err := NewServer(ctx, mux, WithHTTPPort(8082), WithDetachOnServerReadyPredicateFunc())
	if err != nil {
		t.Fatalf("Failed to create detached server: %v", err)
	}

	if err := server.Close(); err != nil {
		t.Fatalf("Failed to close server: %v", err)
	}
}
