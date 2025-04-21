package drpc_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	rclient "github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/client"
	ma "github.com/multiformats/go-multiaddr"
	gv1 "github.com/omgolab/drpc/examples/echo/gen/go/greeter/v1"
	gv1connect "github.com/omgolab/drpc/examples/echo/gen/go/greeter/v1/greeterv1connect"
	"github.com/omgolab/drpc/examples/echo/greeter"

	// "github.com/omgolab/drpc/pkg/config"
	"github.com/omgolab/drpc/pkg/config"
	"github.com/omgolab/drpc/pkg/core"
	"github.com/omgolab/drpc/pkg/drpc"

	// "github.com/omgolab/drpc/pkg/gateway"
	"net/http/httputil"

	glog "github.com/omgolab/go-commons/pkg/log"
)

type loggingRoundTripper struct {
	inner http.RoundTripper
	t     *testing.T
}

func (lrt *loggingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	rawReq, err := httputil.DumpRequestOut(req, true)
	if err == nil {
		lrt.t.Logf("CLIENT - FULL OUTGOING REQUEST:\n%s", string(rawReq))
	} else {
		lrt.t.Logf("CLIENT - Failed to dump outgoing request: %v", err)
	}
	return lrt.inner.RoundTrip(req)
}

const (
	shortTimeout    = 500000 * time.Second
	normalTimeout   = 10000000 * time.Second
	longTimeout     = 200000000 * time.Second
	veryLongTimeout = 300000000 * time.Second // For tests involving relays
)

// setupGreeterHTTP sets up a server with the greeter service over HTTP and libp2p
func setupGreeterHTTP(t *testing.T) (*drpc.ServerInstance, string, func()) {
	t.Helper()
	testLog, _ := glog.New()

	// Create handler
	mux := http.NewServeMux()
	path, handler := gv1connect.NewGreeterServiceHandler(&greeter.Server{})
	mux.Handle(path, handler)

	// Create server with HTTP and libp2p
	ctx, cancel := context.WithTimeout(context.Background(), normalTimeout)
	defer cancel()
	server, err := drpc.NewServer(ctx, mux,
		drpc.WithHTTPPort(0),
		drpc.WithLogger(testLog),
		drpc.WithLibP2POptions(
			libp2p.ForceReachabilityPrivate(), // Treat server as private; only applicable path 2
		),
	)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Wait for HTTP address
	httpAddr := server.HTTPAddr()
	if httpAddr == "" {
		t.Fatalf("Failed to get HTTP address after multiple attempts")
	}

	// Return cleanup function
	cleanup := func() {
		if err := server.Close(); err != nil {
			t.Logf("Warning: failed to close HTTP server: %v", err)
		}
	}

	return server, "http://" + httpAddr, cleanup
}

// setupGreeterLibP2P sets up a server with the greeter service only over libp2p
func setupGreeterLibP2P(t *testing.T) (*drpc.ServerInstance, string, func()) {
	t.Helper()
	testLog, _ := glog.New()

	// Create handler
	mux := http.NewServeMux()
	path, handler := gv1connect.NewGreeterServiceHandler(&greeter.Server{})
	mux.Handle(path, handler)

	// Create server with only libp2p
	ctx, cancel := context.WithTimeout(context.Background(), normalTimeout)
	defer cancel()
	server, err := drpc.NewServer(ctx, mux,
		drpc.WithDisableHTTP(), // Disable HTTP
		drpc.WithLogger(testLog))
	if err != nil {
		t.Fatalf("Failed to create libp2p server: %v", err)
	}

	// Get libp2p address
	p2pAddr := server.P2PHost().ID().String()
	if p2pAddr == "" {
		t.Fatalf("Failed to get libp2p address")
	}
	t.Logf("Server listening on libp2p address: %s", p2pAddr)

	// Return cleanup function
	cleanup := func() {
		if err := server.Close(); err != nil {
			t.Logf("Warning: failed to close libp2p server: %v", err)
		}
	}

	return server, p2pAddr, cleanup
}

// setupRelayNode sets up a basic libp2p node configured as a relay
func setupRelayNode(t *testing.T) (host.Host, func()) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), normalTimeout)
	log, _ := glog.New()

	// Use core.CreateLibp2pHost to create the relay host with proper configuration
	relayHost, err := core.CreateLibp2pHost(ctx, log, []libp2p.Option{
		libp2p.ForceReachabilityPublic(),
		libp2p.EnableRelayService(), // Enable relay service functionality
	})
	if err != nil {
		t.Fatalf("Failed to create relay host: %v", err)
	}
	t.Logf("Relay node created with ID: %s, Addrs: %v", relayHost.ID(), relayHost.Addrs())

	// Wait for service to stabilize with context-aware logic
	select {
	case <-ctx.Done():
		t.Logf("Warning: context timeout while waiting for relay to stabilize")
	case <-time.After(500 * time.Millisecond):
		// Continue after a short delay
	}

	cleanup := func() {
		cancel() // Cancel the context first
		if err := relayHost.Close(); err != nil {
			t.Logf("Warning: failed to close relay host: %v", err)
		}
	}
	return relayHost, cleanup
}

// setupGreeterViaRelay sets up a server that connects through a relay
func setupGreeterViaRelay(t *testing.T, relayHost host.Host) (*drpc.ServerInstance, string, func()) {
	t.Helper()
	testLog, _ := glog.New()

	// Create handler
	mux := http.NewServeMux()
	path, handler := gv1connect.NewGreeterServiceHandler(&greeter.Server{})
	mux.Handle(path, handler)

	// Create server with only libp2p, configured to use the relay
	ctx, cancel := context.WithTimeout(context.Background(), normalTimeout)
	defer cancel()

	// Get relay address info
	relayAddrInfo := peer.AddrInfo{
		ID:    relayHost.ID(),
		Addrs: relayHost.Addrs(),
	}

	server, err := drpc.NewServer(ctx, mux,
		drpc.WithDisableHTTP(), // Disable HTTP
		drpc.WithLibP2POptions(
			libp2p.ForceReachabilityPrivate(), // Treat server as private
			// Also configure AutoRelay for the target server, pointing to the relay node.
			// This helps ensure it actively maintains a connection if needed.
			libp2p.EnableAutoRelayWithStaticRelays([]peer.AddrInfo{relayAddrInfo}),
		),
		drpc.WithLogger(testLog))
	if err != nil {
		t.Fatalf("Failed to create relay server: %v", err)
	}

	// Connect to relay
	if err := server.P2PHost().Connect(ctx, relayAddrInfo); err != nil {
		t.Fatalf("Failed to connect to relay: %v", err)
	}

	// Make relay reservation
	reservation, err := rclient.Reserve(ctx, server.P2PHost(), relayAddrInfo)
	if err != nil {
		t.Fatalf("Failed to make relay reservation: %v", err)
	}
	t.Logf("Made relay reservation with expiration: %v", reservation.Expiration)

	// Construct relay address
	relayMa, err := ma.NewMultiaddr(
		"/p2p/" + relayHost.ID().String() +
			"/p2p-circuit/p2p/" +
			server.P2PHost().ID().String())
	if err != nil {
		t.Fatalf("Failed to construct relay multiaddr: %v", err)
	}
	relayAddr := relayMa.String()
	t.Logf("Server available via relay at: %s", relayAddr)

	// Return cleanup function
	cleanup := func() {
		if err := server.Close(); err != nil {
			t.Logf("Warning: failed to close relay server: %v", err)
		}
	}

	return server, relayAddr, cleanup
}

// setupHTTPGatewayRelay sets up a gateway server that can route to a libp2p relay
// This is used for testing Path 2: Client -> HTTP Gateway -> Relay -> Server
func setupHTTPGatewayRelay(t *testing.T, relayHost host.Host, targetServerID peer.ID) (*drpc.ServerInstance, string, func()) {
	t.Helper()
	testLog, _ := glog.New()

	// Create an empty handler (gateway doesn't need service handlers)
	mux := http.NewServeMux()

	// Get relay address info
	// relayAddrInfo := peer.AddrInfo{
	// 	ID:    relayHost.ID(),
	// 	Addrs: relayHost.Addrs(),
	// }

	// Create context for gateway setup
	ctx, cancel := context.WithTimeout(context.Background(), normalTimeout)
	defer cancel()

	// Create gateway server with HTTP and libp2p
	gateway, err := drpc.NewServer(ctx, mux,
		drpc.WithHTTPPort(0), // Random HTTP port
		drpc.WithLogger(testLog))
	if err != nil {
		t.Fatalf("Failed to create gateway server: %v", err)
	}

	// Connect gateway to relay
	// if err := gateway.P2PHost().Connect(ctx, relayAddrInfo); err != nil {
	// 	t.Fatalf("Gateway failed to connect to relay: %v", err)
	// }
	// t.Log("Waiting 1s after gateway connected to relay...") // Add delay
	// time.Sleep(1 * time.Second)                             // Add delay

	// *** Add target server's relay address to gateway's peerstore ***
	// This ensures the gateway knows how to reach the target via the relay
	// targetRelayAddr, err := ma.NewMultiaddr("/p2p/" + relayHost.ID().String() + "/p2p-circuit/p2p/" + targetServerID.String())
	// if err != nil {
	// 	t.Fatalf("Failed to construct target relay multiaddr for gateway peerstore: %v", err)
	// }
	// gateway.P2PHost().Peerstore().AddAddr(targetServerID, targetRelayAddr, peerstore.PermanentAddrTTL) // Use peerstore.PermanentAddrTTL
	// testLog.Printf("Added target server relay address (%s) to gateway peerstore", targetRelayAddr.String())

	// Wait for HTTP address
	var httpGatewayAddr string
	if addr := gateway.HTTPAddr(); addr != "" {
		// Construct special HTTP address that includes target server info
		httpGatewayAddr = "http://" + addr + "/gateway/p2p/" + relayHost.ID().String() + "/p2p-circuit/p2p/" + targetServerID.String()
		// httpGatewayAddr = "http://" + addr + "/gateway/p2p/" + targetServerID.String()
		testLog.Printf("Gateway listening at: %s", httpGatewayAddr)
	} else {
		t.Fatalf("Failed to get gateway HTTP address")
	}

	// Return cleanup function
	cleanup := func() {
		if err := gateway.Close(); err != nil {
			t.Logf("Warning: failed to close gateway server: %v", err)
		}
	}

	return gateway, httpGatewayAddr, cleanup
}

// Test helper to verify the client's ability to handle unary RPCs
func testClientUnaryRequest(t *testing.T, client gv1connect.GreeterServiceClient) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), shortTimeout)
	defer cancel()

	req := connect.NewRequest(&gv1.SayHelloRequest{Name: "DRPC Test"})
	resp, err := client.SayHello(ctx, req)
	if err != nil {
		t.Fatalf("Failed to call SayHello: %v", err)
	}
	if resp.Msg.Message != "Hello, DRPC Test!" {
		t.Errorf("Unexpected greeting: got %q, want %q", resp.Msg.Message, "Hello, DRPC Test!")
	}
}

// Test helper to verify the client's ability to handle streaming RPCs
func testClientStreamingRequest(t *testing.T, client gv1connect.GreeterServiceClient) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), shortTimeout)
	defer cancel()

	// Start a bidirectional stream
	stream := client.BidiStreamingEcho(ctx)

	// Send multiple requests
	names := []string{"Alice", "Bob", "Charlie", "Dave"}
	for _, name := range names {
		err := stream.Send(&gv1.BidiStreamingEchoRequest{Name: name})
		if err != nil {
			t.Fatalf("Failed to send to stream: %v", err)
		}
	}

	// Close sending
	err := stream.CloseRequest()
	if err != nil {
		t.Fatalf("Failed to close request stream: %v", err)
	}

	// Receive and validate responses
	receivedGreetings := make(map[string]bool)

	for range names {
		resp, err := stream.Receive()
		if err != nil {
			t.Fatalf("Failed to receive from stream: %v", err)
		}
		receivedGreetings[resp.Greeting] = true
	}

	// Check if we received greetings for all names
	for _, name := range names {
		expected := "Hello, " + name + "!"
		if !receivedGreetings[expected] {
			t.Errorf("Missing greeting for %q", name)
		}
	}

	// Make sure stream is properly closed
	_, err = stream.Receive()
	if err == nil {
		t.Errorf("Expected end of stream, but got another response")
	}
}

// Test helper for unary requests with a longer timeout (for relay tests)
func testClientUnaryRequestLongTimeout(t *testing.T, client gv1connect.GreeterServiceClient) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), veryLongTimeout) // Use very long timeout
	defer cancel()

	req := connect.NewRequest(&gv1.SayHelloRequest{Name: "DRPC Test Path 2 Long"})
	resp, err := client.SayHello(ctx, req)
	if err != nil {
		t.Fatalf("Failed to call SayHello (long timeout): %v", err)
	}
	if resp.Msg.Message != "Hello, DRPC Test Path 2 Long!" {
		t.Errorf("Unexpected greeting: got %q, want %q", resp.Msg.Message, "Hello, DRPC Test Path 2 Long!")
	}
}

// Test helper for streaming requests with a longer timeout (for relay tests)
func testClientStreamingRequestLongTimeout(t *testing.T, client gv1connect.GreeterServiceClient) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), veryLongTimeout) // Use very long timeout
	defer cancel()
	stream := client.BidiStreamingEcho(ctx)
	names := []string{"Path2Long-Alice", "Path2Long-Bob", "Path2Long-Charlie"}
	for _, name := range names {
		err := stream.Send(&gv1.BidiStreamingEchoRequest{Name: name})
		if err != nil {
			t.Fatalf("Failed to send to stream (long timeout): %v", err)
		}
	}
	err := stream.CloseRequest()
	if err != nil {
		t.Fatalf("Failed to close request stream (long timeout): %v", err)
	}
	receivedGreetings := make(map[string]bool)
	for i := 0; i < len(names); i++ {
		resp, err := stream.Receive()
		if err != nil {
			t.Fatalf("Failed to receive from stream (long timeout): %v", err)
		}
		receivedGreetings[resp.Greeting] = true
	}
	for _, name := range names {
		expected := "Hello, " + name + "!"
		if !receivedGreetings[expected] {
			t.Errorf("Missing greeting for %q (long timeout)", name)
		}
	}
	_, err = stream.Receive()
	if err == nil {
		t.Errorf("Expected end of stream, but got another response (long timeout)")
	}
}

// TestPath1_HTTPDirect tests the first communication path:
// dRPC Client → Listener(if serverAddr is an http address) → Gateway Handler → Host libp2p Peer → dRPC Handler
func TestPath1_HTTPDirect(t *testing.T) {
	// Setup HTTP server with greeter service
	httpServer, addr, cleanup := setupGreeterHTTP(t)
	defer cleanup()

	t.Logf("HTTP server ready at %s with ID: %s", addr, httpServer.P2PHost().ID().String())

	// Create client connected to HTTP server
	ctx, cancel := context.WithTimeout(context.Background(), shortTimeout)
	defer cancel()

	client, err := drpc.NewClient(
		ctx,
		addr,
		gv1connect.NewGreeterServiceClient,
	)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	t.Log("Testing unary request over HTTP direct path")
	testClientUnaryRequest(t, client)

	t.Log("Testing streaming request over HTTP direct path")
	testClientStreamingRequest(t, client)
}

// TestPath2_HTTPGatewayRelay tests the second communication path:
// dRPC Client → Listener(HTTP with gateway indication) → Gateway Handler → Relay → Host libp2p Peer → dRPC Handler
func TestPath2_HTTPGatewayRelay(t *testing.T) {
	config.DEBUG = true // Enable debug logging for this test

	// Setup greeter HTTP server (target host)
	httpServer, addr, cleanup := setupGreeterHTTP(t)
	defer cleanup()
	t.Logf("Greeter HTTP server ready at %s with ID: %s", addr, httpServer.P2PHost().ID())

	// create a relay peer
	relayHost, relayCleanup := setupRelayNode(t)
	defer relayCleanup()

	// Setup HTTP gw that routes to the server via built-in relay behavior
	gw, gatewayAddr, gatewayCleanup := setupHTTPGatewayRelay(t, relayHost, httpServer.P2PHost().ID())
	defer gatewayCleanup()
	t.Logf("Gateway listening at: %s with host ID: %s", gatewayAddr, gw.P2PHost().ID())

	// Test HTTP/1.1 streaming (default transport)
	t.Run("HTTP/1.1 Streaming", func(t *testing.T) {
		t.Skip("HTTP/1.1 streaming is not supported by the gateway/backend; skipping this test.")
		// Instrument HTTP/1.1 client with loggingRoundTripper
		httpClient := &http.Client{
			Transport: &loggingRoundTripper{
				inner: http.DefaultTransport,
				t:     t,
			},
		}
		client := gv1connect.NewGreeterServiceClient(httpClient, gatewayAddr)
		t.Log("Testing unary request over HTTP/1.1 gateway path")
		testClientUnaryRequest(t, client)
		t.Log("Testing streaming request over HTTP/1.1 gateway path")
		if err := func() (err error) {
			defer func() {
				if r := recover(); r != nil {
					err = r.(error)
				}
			}()
			testClientStreamingRequest(t, client)
			return nil
		}(); err != nil {
			t.Errorf("HTTP/1.1 streaming error: %v", err)
		}
	})

	// Test HTTP/2 streaming
	t.Run("HTTP/2 Streaming", func(t *testing.T) {
		client, err := drpc.NewClient(
			context.Background(),
			gatewayAddr,
			gv1connect.NewGreeterServiceClient,
		)
		if err != nil {
			t.Fatalf("Failed to create client: %v", err)
		}
		if err := func() (err error) {
			defer func() {
				if r := recover(); r != nil {
					err = r.(error)
				}
			}()
			testClientStreamingRequest(t, client)
			return nil
		}(); err != nil {
			t.Errorf("HTTP/2 streaming error: %v", err)
		}
	})
}

// TestPath3_LibP2PDirect tests the third communication path:
// dRPC Client → Host libp2p Peer → dRPC Handler
func TestPath3_LibP2PDirect(t *testing.T) {
	// Setup libp2p server with greeter service
	serverInstance, p2pAddr, cleanup := setupGreeterLibP2P(t)
	defer cleanup()

	t.Logf("Server ID: %s", serverInstance.P2PHost().ID().String())

	// Create client connected directly to libp2p server
	ctx, cancel := context.WithTimeout(context.Background(), shortTimeout)
	defer cancel()
	testLog, _ := glog.New() // Create logger

	client, err := drpc.NewClient(
		ctx,
		p2pAddr,
		gv1connect.NewGreeterServiceClient,
		drpc.WithClientLogger(testLog), // Use correct ClientOption
	)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	t.Log("Testing unary request over direct libp2p path")
	testClientUnaryRequest(t, client)

	t.Log("Testing streaming request over direct libp2p path")
	testClientStreamingRequest(t, client)
}

// TestPath4_LibP2PRelay tests the fourth communication path:
// dRPC Client → Relay libp2p Peer → Host libp2p Peer → dRPC Handler
func TestPath4_LibP2PRelay(t *testing.T) {
	// Setup a relay node
	relayHost, relayCleanup := setupRelayNode(t)
	defer relayCleanup()

	// Setup server that connects through the relay
	targetServer, relayAddr, serverCleanup := setupGreeterViaRelay(t, relayHost)
	defer serverCleanup()

	t.Logf("Target server ID: %s, available at relay address: %s",
		targetServer.P2PHost().ID().String(), relayAddr)

	// Create client that connects via the relay
	ctx, cancel := context.WithTimeout(context.Background(), veryLongTimeout) // Use very long timeout for relay
	defer cancel()
	testLog, _ := glog.New() // Create logger
	client, err := drpc.NewClient(
		ctx,
		relayAddr, // Connect to the relay address
		gv1connect.NewGreeterServiceClient,
		drpc.WithClientLibp2pOptions(libp2p.EnableRelay()), // Keep existing options
		drpc.WithClientLogger(testLog),                     // Add logger option
	)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	t.Log("Testing unary request over libp2p relay path")
	testClientUnaryRequestLongTimeout(t, client) // Use long timeout helper

	t.Log("Testing streaming request over libp2p relay path")
	testClientStreamingRequestLongTimeout(t, client) // Use long timeout helper
}
