package drpc_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	gv1 "github.com/omgolab/drpc/examples/echo/gen/go/greeter/v1"
	gv1connect "github.com/omgolab/drpc/examples/echo/gen/go/greeter/v1/greeterv1connect"
	"github.com/omgolab/drpc/examples/echo/greeter"
	"github.com/omgolab/drpc/pkg/config"
	"github.com/omgolab/drpc/pkg/core"
	"github.com/omgolab/drpc/pkg/drpc"
	"github.com/omgolab/drpc/pkg/gateway"
	glog "github.com/omgolab/go-commons/pkg/log"
)

var testLog, _ = glog.New()

const timeout = 30 * time.Second

// --- Utility helpers for compactness ---

type greeterServerMode int

const (
	modeHTTP greeterServerMode = iota
	modeLibP2P
	modeRelay
)

type greeterServerSetup struct {
	server  *drpc.ServerInstance
	addr    string
	cleanup func()
}

// Generic server setup
func setupGreeterServer(t *testing.T, mode greeterServerMode, relayHost host.Host) greeterServerSetup {
	t.Helper()
	mux := http.NewServeMux()
	path, handler := gv1connect.NewGreeterServiceHandler(&greeter.Server{})
	mux.Handle(path, handler)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var server *drpc.ServerInstance
	var err error
	var addr string

	switch mode {
	case modeHTTP:
		server, err = drpc.NewServer(ctx, mux, drpc.WithHTTPPort(0), drpc.WithLogger(testLog), drpc.WithLibP2POptions(libp2p.ForceReachabilityPrivate()))
		if err == nil {
			addr = server.HTTPAddr()
		}
	case modeLibP2P:
		server, err = drpc.NewServer(ctx, mux, drpc.WithDisableHTTP(), drpc.WithLogger(testLog))
		if err == nil {
			addr = server.P2PAddrs()[0]
		}
	case modeRelay:
		server, err = drpc.NewServer(ctx, mux, drpc.WithDisableHTTP(), drpc.WithLibP2POptions(libp2p.ForceReachabilityPrivate()), drpc.WithLogger(testLog))
		if err == nil {
			addr = "/p2p/" + relayHost.ID().String() + "/p2p-circuit/p2p/" + server.P2PHost().ID().String()
		}
	}
	if err != nil || addr == "" {
		t.Fatalf("Failed to create server: %v", err)
	}
	cleanup := func() { _ = server.Close() }
	return greeterServerSetup{server, addr, cleanup}
}

// Generic relay node setup
func setupRelayNode(t *testing.T) (host.Host, func()) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	relayHost, err := core.CreateLibp2pHost(ctx, core.WithHostLogger(testLog), core.WithHostLibp2pOptions(libp2p.ForceReachabilityPublic()))
	if err != nil {
		t.Fatalf("Failed to create relay host: %v", err)
	}
	return relayHost, func() { cancel(); _ = relayHost.Close() }
}

// Generic HTTP gateway setup
func setupHTTPGateway(t *testing.T, relayHost host.Host, targetServerP2pAddr string) (func(bool) string, func()) {
	t.Helper()
	mux := http.NewServeMux()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	gw, err := drpc.NewServer(ctx, mux, drpc.WithHTTPPort(0), drpc.WithLogger(testLog))
	if err != nil {
		t.Fatalf("Failed to create gateway server: %v", err)
	}

	gwAddr := gw.HTTPAddr()
	if gwAddr == "" {
		t.Fatalf("Failed to get gateway HTTP address")
	}

	httpGatewayAddrFn := func(addRelay bool) string {
		fullRelayAddr := targetServerP2pAddr
		if addRelay {
			fullRelayAddr = "/p2p/" + relayHost.ID().String() + "/p2p-circuit" + targetServerP2pAddr
		}
		return gwAddr + gateway.GatewayPrefix + fullRelayAddr + gateway.GatewayPrefix
	}

	return httpGatewayAddrFn, func() { _ = gw.Close() }
}

// --- Parameterized test helpers ---

func testClientUnaryRequest(t *testing.T, client gv1connect.GreeterServiceClient, name string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req := connect.NewRequest(&gv1.SayHelloRequest{Name: name})
	resp, err := client.SayHello(ctx, req)
	if err != nil {
		t.Fatalf("Failed to call SayHello: %v", err)
	}

	want := "Hello, " + name + "!"
	if resp.Msg.Message != want {
		t.Errorf("Unexpected greeting: got %q, want %q", resp.Msg.Message, want)
	}
}

func testClientStreamingRequest(t *testing.T, client gv1connect.GreeterServiceClient, names []string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	stream := client.BidiStreamingEcho(ctx)
	for _, name := range names {
		if err := stream.Send(&gv1.BidiStreamingEchoRequest{Name: name}); err != nil {
			t.Fatalf("Failed to send to stream: %v", err)
		}
	}
	if err := stream.CloseRequest(); err != nil {
		t.Fatalf("Failed to close request stream: %v", err)
	}

	received := make(map[string]bool)
	for range names {
		resp, err := stream.Receive()
		if err != nil {
			t.Fatalf("Failed to receive from stream: %v", err)
		}
		received[resp.Greeting] = true
	}

	for _, name := range names {
		expected := "Hello, " + name + "!"
		if !received[expected] {
			t.Errorf("Missing greeting for %q", name)
		}
	}
	if _, err := stream.Receive(); err == nil {
		t.Errorf("Expected end of stream, but got another response")
	}
}

// --- Main tests (with commentary and relaxed formatting) ---

// TestPath1_HTTPDirect tests the first communication path:
// dRPC Client → Listener (if serverAddr is an http address) → Gateway Handler → Host libp2p Peer → dRPC Handler
func TestPath1_HTTPDirect(t *testing.T) {
	setup := setupGreeterServer(t, modeHTTP, nil)
	defer setup.cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	client, err := drpc.NewClient(ctx, setup.addr, gv1connect.NewGreeterServiceClient)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	t.Run("unary", func(t *testing.T) {
		testClientUnaryRequest(t, client, "DRPC Test")
	})

	t.Run("streaming", func(t *testing.T) {
		names := []string{"Alice", "Bob", "Charlie", "Dave"}
		testClientStreamingRequest(t, client, names)
	})
}

// TestPath2_HTTPGatewayRelay tests the second communication path:
// dRPC Client → Listener (HTTP with gateway indication) → Gateway Handler → Relay → Host libp2p Peer → dRPC Handler
func TestPath2_HTTPGatewayRelay(t *testing.T) {
	config.DEBUG = true
	setup := setupGreeterServer(t, modeHTTP, nil)
	defer setup.cleanup()

	relayHost, relayCleanup := setupRelayNode(t)
	defer relayCleanup()

	gatewayAddrFn, gatewayCleanup := setupHTTPGateway(t, relayHost, setup.server.P2PAddrs()[0])
	defer gatewayCleanup()

	tests := []struct {
		name   string
		addrFn func() string
	}{
		{"HTTP/2 unary/streaming with force relay address", func() string { return gatewayAddrFn(true) }},
		{"HTTP/2 unary/streaming with no/auto relay address", func() string { return gatewayAddrFn(false) }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client, err := drpc.NewClient(context.Background(), tc.addrFn(), gv1connect.NewGreeterServiceClient)
			if err != nil {
				t.Fatalf("Failed to create client: %v", err)
			}
			testClientUnaryRequest(t, client, "DRPC Test")

			names := []string{"Alice", "Bob", "Charlie", "Dave"}
			testClientStreamingRequest(t, client, names)
		})
	}
}

// TestPath3_LibP2PDirect tests the third communication path:
// dRPC Client → Host libp2p Peer → dRPC Handler
func TestPath3_LibP2PDirect(t *testing.T) {
	setup := setupGreeterServer(t, modeLibP2P, nil)
	defer setup.cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	client, err := drpc.NewClient(ctx, setup.addr, gv1connect.NewGreeterServiceClient, drpc.WithClientLogger(testLog))
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	t.Run("unary", func(t *testing.T) {
		testClientUnaryRequest(t, client, "DRPC Test")
	})

	t.Run("streaming", func(t *testing.T) {
		names := []string{"Alice", "Bob", "Charlie", "Dave"}
		testClientStreamingRequest(t, client, names)
	})
}

// TestPath4_LibP2PRelay tests the fourth communication path:
// dRPC Client → Relay libp2p Peer → Host libp2p Peer → dRPC Handler
func TestPath4_LibP2PRelay(t *testing.T) {
	relayHost, relayCleanup := setupRelayNode(t)
	defer relayCleanup()

	setup := setupGreeterServer(t, modeRelay, relayHost)
	defer setup.cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	client, err := drpc.NewClient(ctx, setup.addr, gv1connect.NewGreeterServiceClient, drpc.WithClientLogger(testLog))
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	t.Run("unary", func(t *testing.T) {
		testClientUnaryRequest(t, client, "DRPC Test Path 2 Long")
	})

	t.Run("streaming", func(t *testing.T) {
		names := []string{"Path2Long-Alice", "Path2Long-Bob", "Path2Long-Charlie"}
		testClientStreamingRequest(t, client, names)
	})
}
