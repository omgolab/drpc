package drpc_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/libp2p/go-libp2p"
	host "github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/peerstore"
	rclient "github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/client"
	"github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/relay"
	ma "github.com/multiformats/go-multiaddr"
	gv1 "github.com/omgolab/drpc/examples/echo/gen/go/greeter/v1"
	gv1connect "github.com/omgolab/drpc/examples/echo/gen/go/greeter/v1/greeterv1connect"
	"github.com/omgolab/drpc/examples/echo/greeter"
	"github.com/omgolab/drpc/pkg/drpc"
	glog "github.com/omgolab/go-commons/pkg/log"
)

const (
	shortTimeout  = 5 * time.Second
	normalTimeout = 15 * time.Second
	longTimeout   = 25 * time.Second
)

// setupGreeterHTTP creates an HTTP server with the greeter service and returns server, http address and cleanup function
func setupGreeterHTTP(t *testing.T) (*drpc.ServerInstance, string, func()) {
	t.Helper()
	testLog, _ := glog.New()

	// Create greeter handler and register with HTTP mux
	mux := http.NewServeMux()
	path, handler := gv1connect.NewGreeterServiceHandler(&greeter.Server{})
	mux.Handle(path, handler)

	// Create context for server setup
	ctx, cancel := context.WithTimeout(context.Background(), normalTimeout)
	defer cancel()

	// Create server with random HTTP port
	server, err := drpc.NewServer(ctx, mux,
		drpc.WithHTTPPort(0),
		drpc.WithLogger(testLog))
	if err != nil {
		t.Fatalf("Failed to create HTTP server: %v", err)
	}

	// Wait for HTTP address to be available
	var httpAddr string
	select {
	case <-ctx.Done():
		t.Fatalf("Context deadline exceeded while waiting for HTTP address")
	default:
		if addr := server.HTTPAddr(); addr != "" {
			httpAddr = "http://" + addr
			t.Logf("Server listening on HTTP address: %s", httpAddr)
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if httpAddr == "" {
		t.Fatalf("Failed to get HTTP address")
	}

	// Return cleanup function
	cleanup := func() {
		if err := server.Close(); err != nil {
			t.Logf("Warning: failed to close HTTP server: %v", err)
		}
	}

	return server, httpAddr, cleanup
}

// setupGreeterLibP2P creates a server with the greeter service accessible via libp2p
func setupGreeterLibP2P(t *testing.T) (*drpc.ServerInstance, string, func()) {
	t.Helper()
	testLog, _ := glog.New()

	// Create greeter handler and register with HTTP mux
	mux := http.NewServeMux()
	path, handler := gv1connect.NewGreeterServiceHandler(&greeter.Server{})
	mux.Handle(path, handler)

	// Create context for server setup
	ctx, cancel := context.WithTimeout(context.Background(), normalTimeout)
	defer cancel()

	// Create server with libp2p enabled, no HTTP port
	server, err := drpc.NewServer(ctx, mux,
		drpc.WithHTTPPort(-1), // No HTTP
		drpc.WithLibP2POptions(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0")),
		drpc.WithLogger(testLog))
	if err != nil {
		t.Fatalf("Failed to create libp2p server: %v", err)
	}

	// Wait for a valid libp2p address
	var p2pAddr string
	for attempt := 0; attempt < 25; attempt++ {
		select {
		case <-ctx.Done():
			t.Fatalf("Context deadline exceeded while waiting for libp2p address")
		default:
			for _, addr := range server.Addrs() {
				if maddr, err := ma.NewMultiaddr(addr); err == nil {
					if _, err := maddr.ValueForProtocol(ma.P_P2P); err == nil {
						p2pAddr = addr
						break
					}
				}
			}
			if p2pAddr != "" {
				t.Logf("Server listening on libp2p address: %s", p2pAddr)
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
	}

	if p2pAddr == "" {
		t.Fatalf("Failed to get libp2p address after multiple attempts")
	}

	// Return cleanup function
	cleanup := func() {
		if err := server.Close(); err != nil {
			t.Logf("Warning: failed to close libp2p server: %v", err)
		}
	}

	return server, p2pAddr, cleanup
}

// setupRelayNode creates a libp2p host configured as a relay
func setupRelayNode(t *testing.T) (host.Host, func()) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), normalTimeout)

	// Create relay host with enhanced options
	relayHost, err := libp2p.New(
		libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"),
		libp2p.EnableRelay(), // Keep general relay enabled
		// Explicitly configure Relay v2 resources
		libp2p.EnableRelayService(relay.WithResources(relay.DefaultResources())),
		libp2p.ForceReachabilityPublic(),
	)
	if err != nil {
		cancel()
		t.Fatalf("Failed to create relay host: %v", err)
	}

	// The relay service should be started by the EnableRelayService option.
	// Remove the explicit relay.New call as it might be redundant or conflicting.
	// _, err = relay.New(relayHost)
	// if err != nil {
	// 	relayHost.Close()
	// 	cancel()
	// 	t.Fatalf("Failed to start relay service: %v", err)
	// }

	t.Logf("Relay node created with ID: %s", relayHost.ID())
	t.Logf("Relay listening on addresses: %v", relayHost.Addrs())

	// Wait for service to stabilize with context-aware logic
	select {
	case <-ctx.Done():
		t.Logf("Warning: context timeout while waiting for relay to stabilize")
	case <-time.After(500 * time.Millisecond):
		// Continue after waiting
	}

	cleanup := func() {
		relayHost.Close()
		cancel()
	}

	return relayHost, cleanup
}

// setupGreeterViaRelay creates a server that only listens via a relay
func setupGreeterViaRelay(t *testing.T, relayHost host.Host) (*drpc.ServerInstance, string, func()) {
	t.Helper()
	testLog, _ := glog.New()

	// Create greeter handler and register with HTTP mux
	mux := http.NewServeMux()
	path, handler := gv1connect.NewGreeterServiceHandler(&greeter.Server{})
	mux.Handle(path, handler)

	// Get relay address info
	relayAddrInfo := peer.AddrInfo{
		ID:    relayHost.ID(),
		Addrs: relayHost.Addrs(),
	}

	// Create context for server setup
	ctx, cancel := context.WithTimeout(context.Background(), normalTimeout)
	defer cancel()

	// Create server that connects through relay
	server, err := drpc.NewServer(ctx, mux,
		drpc.WithHTTPPort(-1), // No HTTP
		drpc.WithLibP2POptions(
			libp2p.NoListenAddrs,
			libp2p.ForceReachabilityPrivate(),
			libp2p.EnableRelay(), // Enable relay capabilities for the target server
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
	relayAddrInfo := peer.AddrInfo{
		ID:    relayHost.ID(),
		Addrs: relayHost.Addrs(),
	}

	// Create context for gateway setup
	ctx, cancel := context.WithTimeout(context.Background(), normalTimeout)
	defer cancel()

	// Create gateway server with HTTP and libp2p
	gateway, err := drpc.NewServer(ctx, mux,
		drpc.WithHTTPPort(0), // Random HTTP port
		drpc.WithLibP2POptions(
			libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"),
			// libp2p.EnableRelay(), // Remove: Gateway doesn't need to BE a relay, just USE one.
			libp2p.ForceReachabilityPublic(),
			// Add client-side relay options to allow dialing via relay
			libp2p.EnableAutoRelayWithStaticRelays([]peer.AddrInfo{relayAddrInfo}),
		),
		drpc.WithLogger(testLog))
	if err != nil {
		t.Fatalf("Failed to create gateway server: %v", err)
	}

	// Connect gateway to relay
	if err := gateway.P2PHost().Connect(ctx, relayAddrInfo); err != nil {
		t.Fatalf("Gateway failed to connect to relay: %v", err)
	}

	// *** Add target server's relay address to gateway's peerstore ***
	// This ensures the gateway knows how to reach the target via the relay
	targetRelayAddr, err := ma.NewMultiaddr("/p2p/" + relayHost.ID().String() + "/p2p-circuit/p2p/" + targetServerID.String())
	if err != nil {
		t.Fatalf("Failed to construct target relay multiaddr for gateway peerstore: %v", err)
	}
	gateway.P2PHost().Peerstore().AddAddr(targetServerID, targetRelayAddr, peerstore.PermanentAddrTTL) // Use peerstore.PermanentAddrTTL
	t.Logf("Added target server relay address (%s) to gateway peerstore", targetRelayAddr.String())

	// Wait for HTTP address
	var httpGatewayAddr string
	for attempt := 0; attempt < 25; attempt++ {
		select {
		case <-ctx.Done():
			t.Fatalf("Context deadline exceeded while waiting for gateway HTTP address")
		default:
			if addr := gateway.HTTPAddr(); addr != "" {
				// Construct special HTTP address that includes target server info
				httpGatewayAddr = "http://" + addr + "/gateway/p2p/" + targetServerID.String()
				t.Logf("Gateway listening at: %s", httpGatewayAddr)
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
	}

	if httpGatewayAddr == "" {
		t.Fatalf("Failed to get gateway HTTP address after multiple attempts")
	}

	// Return cleanup function
	cleanup := func() {
		if err := gateway.Close(); err != nil {
			t.Logf("Warning: failed to close gateway server: %v", err)
		}
	}

	return gateway, httpGatewayAddr, cleanup
}

// Basic test helper to verify the client's ability to communicate with the server
// across different connection types (HTTP, libp2p, relay)
func testClientUnaryRequest(t *testing.T, client gv1connect.GreeterServiceClient) {
	ctx, cancel := context.WithTimeout(context.Background(), shortTimeout)
	defer cancel()

	// Make a simple unary request
	resp, err := client.SayHello(ctx, connect.NewRequest(&gv1.SayHelloRequest{Name: "DRPC Test"}))
	if err != nil {
		t.Fatalf("Failed to call SayHello: %v", err)
	}

	// Validate response
	if resp.Msg.Message != "Hello, DRPC Test!" {
		t.Errorf("Unexpected greeting: got %q, want %q", resp.Msg.Message, "Hello, DRPC Test!")
	}
}

// Test helper to verify the client's ability to handle streaming RPCs
func testClientStreamingRequest(t *testing.T, client gv1connect.GreeterServiceClient) {
	ctx, cancel := context.WithTimeout(context.Background(), shortTimeout)
	defer cancel()

	// Start a bidirectional stream
	stream := client.BidiStreamingEcho(ctx)

	// Send multiple names
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

	for i := 0; i < len(names); i++ {
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

// testClientUnaryRequestNormalTimeout is like testClientUnaryRequest but uses normalTimeout
func testClientUnaryRequestNormalTimeout(t *testing.T, client gv1connect.GreeterServiceClient) {
	ctx, cancel := context.WithTimeout(context.Background(), normalTimeout) // Use normalTimeout
	defer cancel()

	resp, err := client.SayHello(ctx, connect.NewRequest(&gv1.SayHelloRequest{Name: "DRPC Test Path 2"}))
	if err != nil {
		t.Fatalf("Failed to call SayHello (normal timeout): %v", err)
	}
	if resp.Msg.Message != "Hello, DRPC Test Path 2!" {
		t.Errorf("Unexpected greeting: got %q, want %q", resp.Msg.Message, "Hello, DRPC Test Path 2!")
	}
}

// testClientStreamingRequestNormalTimeout is like testClientStreamingRequest but uses normalTimeout
func testClientStreamingRequestNormalTimeout(t *testing.T, client gv1connect.GreeterServiceClient) {
	ctx, cancel := context.WithTimeout(context.Background(), normalTimeout) // Use normalTimeout
	defer cancel()
	stream := client.BidiStreamingEcho(ctx)
	names := []string{"Path2-Alice", "Path2-Bob", "Path2-Charlie"}
	for _, name := range names {
		err := stream.Send(&gv1.BidiStreamingEchoRequest{Name: name})
		if err != nil {
			t.Fatalf("Failed to send to stream (normal timeout): %v", err)
		}
	}
	err := stream.CloseRequest()
	if err != nil {
		t.Fatalf("Failed to close request stream (normal timeout): %v", err)
	}
	receivedGreetings := make(map[string]bool)
	for i := 0; i < len(names); i++ {
		resp, err := stream.Receive()
		if err != nil {
			t.Fatalf("Failed to receive from stream (normal timeout): %v", err)
		}
		receivedGreetings[resp.Greeting] = true
	}
	for _, name := range names {
		expected := "Hello, " + name + "!"
		if !receivedGreetings[expected] {
			t.Errorf("Missing greeting for %q (normal timeout)", name)
		}
	}
	_, err = stream.Receive()
	if err == nil {
		t.Errorf("Expected end of stream, but got another response (normal timeout)")
	}
}

// testClientUnaryRequestLongTimeout is like testClientUnaryRequest but uses longTimeout
func testClientUnaryRequestLongTimeout(t *testing.T, client gv1connect.GreeterServiceClient) {
	ctx, cancel := context.WithTimeout(context.Background(), longTimeout) // Use longTimeout
	defer cancel()

	resp, err := client.SayHello(ctx, connect.NewRequest(&gv1.SayHelloRequest{Name: "DRPC Test Path 2 Long"}))
	if err != nil {
		t.Fatalf("Failed to call SayHello (long timeout): %v", err)
	}
	if resp.Msg.Message != "Hello, DRPC Test Path 2 Long!" {
		t.Errorf("Unexpected greeting: got %q, want %q", resp.Msg.Message, "Hello, DRPC Test Path 2 Long!")
	}
}

// testClientStreamingRequestLongTimeout is like testClientStreamingRequest but uses longTimeout
func testClientStreamingRequestLongTimeout(t *testing.T, client gv1connect.GreeterServiceClient) {
	ctx, cancel := context.WithTimeout(context.Background(), longTimeout) // Use longTimeout
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
	// Setup a relay node
	relayHost, relayCleanup := setupRelayNode(t)
	defer relayCleanup()

	// Setup server that connects through the relay
	targetServer, relayAddr, serverCleanup := setupGreeterViaRelay(t, relayHost)
	defer serverCleanup()

	t.Logf("Target server ID: %s, available at relay address: %s",
		targetServer.P2PHost().ID().String(), relayAddr)

	// Setup HTTP gateway that can route to the server through the relay
	_, gatewayAddr, gatewayCleanup := setupHTTPGatewayRelay(t, relayHost, targetServer.P2PHost().ID())
	defer gatewayCleanup()

	// Create client that connects to the gateway
	ctx, cancel := context.WithTimeout(context.Background(), normalTimeout)
	defer cancel()

	client, err := drpc.NewClient(
		ctx,
		gatewayAddr,
		gv1connect.NewGreeterServiceClient,
	)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	t.Log("Testing unary request over HTTP gateway -> relay path (long timeout)")
	testClientUnaryRequestLongTimeout(t, client) // Use long timeout helper

	t.Log("Testing streaming request over HTTP gateway -> relay path (long timeout)")
	testClientStreamingRequestLongTimeout(t, client) // Use long timeout helper
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
		// drpc.WithConnectOptions(connect.WithHTTPVersion(1)), // Remove undefined option
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
	relayServer, relayAddr, serverCleanup := setupGreeterViaRelay(t, relayHost)
	defer serverCleanup()

	t.Logf("Relayed server ID: %s", relayServer.P2PHost().ID().String())

	// Create client that connects through the relay address
	ctx, cancel := context.WithTimeout(context.Background(), normalTimeout)
	defer cancel()

	testLog, _ := glog.New() // Create logger
	client, err := drpc.NewClient(
		ctx,
		relayAddr,
		gv1connect.NewGreeterServiceClient,
		drpc.WithClientLibp2pOptions(libp2p.EnableRelay()), // Keep existing options
		drpc.WithClientLogger(testLog),                     // Add logger option
	)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	t.Log("Testing unary request over libp2p relay path")
	testClientUnaryRequest(t, client)

	t.Log("Testing streaming request over libp2p relay path")
	testClientStreamingRequest(t, client)
}
