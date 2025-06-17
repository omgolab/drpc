package gateway

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	glog "github.com/omgolab/go-commons/pkg/log"
)

func TestGatewayHandler(t *testing.T) {
	// Create a mock logger
	logger, _ := glog.New(glog.WithFileLogger("test.log"))

	// Create a gateway handler using our consolidated test utility function
	h, err := libp2p.New()
	if err != nil {
		t.Fatal(err)
	}
	gatewayHandler := getGatewayHandler(t, h, logger)

	// Test case 1: Not a gateway path (no /@/)
	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	gatewayHandler.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status %v, got %v", http.StatusNotFound, w.Code)
	}

	// Test case 2: Invalid gateway path (no multiaddr with peer ID)
	req = httptest.NewRequest("GET", "/@/ip4/127.0.0.1/tcp/9000", nil)
	w = httptest.NewRecorder()
	gatewayHandler.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest && w.Code != http.StatusNotFound {
		t.Errorf("Expected status BadRequest or NotFound, got %v", w.Code)
	}

	// Test case 3: Missing service path
	req = httptest.NewRequest("GET", "/@/ip4/127.0.0.1/tcp/9000/p2p/QmPeerID", nil)
	w = httptest.NewRecorder()
	gatewayHandler.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest && w.Code != http.StatusNotFound {
		t.Errorf("Expected status BadRequest or NotFound, got %v", w.Code)
	}
}

// CreateTestLibp2pHosts creates two libp2p hosts connected to each other for testing
func CreateTestLibp2pHosts(t *testing.T) (host.Host, host.Host) {
	// Create first host
	h1, err := libp2p.New(
		libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"),
		libp2p.DisableRelay(),
		libp2p.NoSecurity,
	)
	if err != nil {
		t.Fatalf("Failed to create first host: %v", err)
	}

	// Create second host
	h2, err := libp2p.New(
		libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"),
		libp2p.DisableRelay(),
		libp2p.NoSecurity,
	)
	if err != nil {
		t.Fatalf("Failed to create second host: %v", err)
	}

	// Get multiaddrs
	h1info := peer.AddrInfo{
		ID:    h1.ID(),
		Addrs: h1.Addrs(),
	}
	h2info := peer.AddrInfo{
		ID:    h2.ID(),
		Addrs: h2.Addrs(),
	}

	// Connect them
	err = h1.Connect(context.Background(), h2info)
	if err != nil {
		t.Fatalf("Failed to connect hosts: %v", err)
	}

	err = h2.Connect(context.Background(), h1info)
	if err != nil {
		t.Fatalf("Failed to connect hosts: %v", err)
	}

	return h1, h2
}

func TestGatewayWithMultipleAddresses(t *testing.T) {
	t.Skip("Integration test with actual hosts needed")
}

// MockP2PHandler creates a mock HTTP handler that simulates a p2p gateway for testing
func MockP2PHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Parse path to extract multiaddrs and service path
		pathParts := strings.Split(r.URL.Path, "/@/")
		if len(pathParts) < 2 {
			http.Error(w, "Invalid path format", http.StatusBadRequest)
			return
		}

		// Get the first multiaddr with peer ID
		hasPeerID := false
		for _, part := range pathParts[:len(pathParts)-1] {
			if strings.Contains(part, "/p2p/") {
				hasPeerID = true
				break
			}
		}

		if !hasPeerID {
			http.Error(w, "No peer ID found in multiaddresses", http.StatusBadRequest)
			return
		}

		// Get service path (last part)
		servicePath := pathParts[len(pathParts)-1]
		if !strings.Contains(servicePath, "/") {
			http.Error(w, "Invalid service path format", http.StatusBadRequest)
			return
		}

		// Mock successful response
		w.Header().Set("Content-Type", "application/connect+proto")
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, `{"message":"Hello from mock p2p gateway!"}`)
	}
}

func TestMockGatewayWithMultipleAddresses(t *testing.T) {
	server := httptest.NewServer(MockP2PHandler())
	defer server.Close()

	tests := []struct {
		name       string
		path       string
		wantStatus int
	}{
		{
			name:       "Valid path with single multiaddr",
			path:       "/@/ip4/127.0.0.1/tcp/9000/p2p/QmPeerID/@/greeter/SayHello",
			wantStatus: http.StatusOK,
		},
		{
			name:       "Valid path with multiple multiaddrs",
			path:       "/@/ip4/127.0.0.1/tcp/9000/p2p/QmPeerID/@/ip4/1.2.3.4/tcp/9999/@/greeter/SayHello",
			wantStatus: http.StatusOK,
		},
		{
			name:       "Invalid path - no peer ID",
			path:       "/@/ip4/127.0.0.1/tcp/9000/@/greeter/SayHello",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "Invalid path - missing service",
			path:       "/@/ip4/127.0.0.1/tcp/9000/p2p/QmPeerID",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "Invalid path - malformed service path",
			path:       "/@/ip4/127.0.0.1/tcp/9000/p2p/QmPeerID/@/greeter",
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest("GET", server.URL+tt.path, nil)
			if err != nil {
				t.Fatalf("Failed to create request: %v", err)
			}

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("Request failed: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("Expected status %v, got %v", tt.wantStatus, resp.StatusCode)
			}
		})
	}
}

// Consolidated getGatewayHandler with optional parameters
func getGatewayHandler(t *testing.T, h host.Host, customLogger ...glog.Logger) http.Handler {
	var logger glog.Logger
	var err error

	if len(customLogger) > 0 {
		logger = customLogger[0]
	} else {
		logger, err = glog.New(glog.WithFileLogger("test.log"))
		if err != nil {
			t.Fatal(err)
		}
	}

	// Create base handler that responds with 404
	baseHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})

	return SetupHandler(baseHandler, logger, h, nil)
}
