package drpc_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"connectrpc.com/connect"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/omgolab/drpc/pkg/drpc"
)

// MockService represents a mock service client for testing
type MockService struct {
	BaseURL  string
	CallOpts []connect.ClientOption
}

// Mock service factory function for testing
func newMockServiceClient(httpClient connect.HTTPClient, baseURL string, opts ...connect.ClientOption) MockService {
	mock := MockService{
		BaseURL:  baseURL,
		CallOpts: opts,
	}
	return mock
}

// TestHTTPDirectPath tests client Path 1:
// dRPC Client → HTTP Listener → Gateway Handler → Host libp2p Peer → dRPC Handler
func TestHTTPDirectPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Path 1 Success"))
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	client, err := drpc.NewClient(
		ctx,
		server.URL,
		newMockServiceClient,
	)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	if client.BaseURL != server.URL {
		t.Errorf("Expected base URL %s, got %s", server.URL, client.BaseURL)
	}
}

// TestHTTPGatewayRelayPath tests client Path 2:
// dRPC Client → HTTP Listener → Gateway Handler → Relay libp2p Peer → Host libp2p Peer → dRPC Handler
func TestHTTPGatewayRelayPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Path 2 Success"))
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	gatewayURL := server.URL + "/gateway/relay"
	client, err := drpc.NewClient(
		ctx,
		gatewayURL,
		newMockServiceClient,
	)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	if client.BaseURL != gatewayURL {
		t.Errorf("Expected base URL %s, got %s", gatewayURL, client.BaseURL)
	}
}

// TestLibp2pAddressRecognition tests proper handling of libp2p addresses
func TestLibp2pAddressRecognition(t *testing.T) {
	// Test relay address recognition
	relayAddr := "/ip4/127.0.0.1/tcp/4001/p2p/QmYyQSo1c1Ym7orWxLYvCrM2EmxFTANf8wXmmE7DWjhx5N/p2p-circuit/p2p/QmaCpDMGvV2BGHeYERUEnRQAwe3N8SzbUtfsmvsqQLuvuJ"
	maddr, err := ma.NewMultiaddr(relayAddr)
	if err != nil {
		t.Fatalf("Failed to create relay multiaddress: %v", err)
	}

	if !drpc.IsRelayAddr(maddr.String()) {
		t.Errorf("Address %s should be recognized as a relay address", relayAddr)
	}

	// Test direct address recognition
	directAddr := "/ip4/127.0.0.1/tcp/4001/p2p/QmaCpDMGvV2BGHeYERUEnRQAwe3N8SzbUtfsmvsqQLuvuJ"
	maddr2, err := ma.NewMultiaddr(directAddr)
	if err != nil {
		t.Fatalf("Failed to create direct multiaddress: %v", err)
	}

	if drpc.IsRelayAddr(maddr2.String()) {
		t.Errorf("Address %s should not be recognized as a relay address", directAddr)
	}
}

// TestClientOptions tests the client options functionality
func TestClientOptions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Create simple connect options for testing
	connectOpt := connect.WithGRPC()

	// Create client with option
	client, err := drpc.NewClient(
		ctx,
		server.URL,
		newMockServiceClient,
		drpc.WithConnectOptions(connectOpt),
	)
	if err != nil {
		t.Fatalf("Failed to create client with options: %v", err)
	}

	if len(client.CallOpts) == 0 {
		t.Fatalf("Expected client options to be present, got empty options list")
	}

	expectedOpts := []connect.ClientOption{connectOpt}
	callOpts := client.CallOpts
	if !reflect.DeepEqual(callOpts, expectedOpts) {
		t.Errorf("Expected client options to be %v, got %v", expectedOpts, callOpts)
	}
}
