package drpc_integration_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	gv1connect "github.com/omgolab/drpc/demo/gen/go/greeter/v1/greeterv1connect"
	"github.com/omgolab/drpc/pkg/drpc/client"
	glog "github.com/omgolab/go-commons/pkg/log"
)

var (
	testLog, _ = glog.New() // Assuming glog.New() is suitable for tests
	tutil      *UtilServerHelper
)

// TestMain is used for global setup/teardown of the utility server.
// It initializes the utility server once for all tests.
func TestMain(m *testing.M) {
	fmt.Println("Setting up global utility server for integration tests...")

	// Initialize the utility server helper with our setup logger
	tutil = NewUtilServerHelper("../../../cmd/util-server/main.go")

	// Run the tests
	code := m.Run()

	// Clean up
	fmt.Println("Tearing down global utility server...")
	tutil.StopServer()

	os.Exit(code)
}

// Test_Path1_DirectHTTP_Communication verifies dRPC communication directly over HTTP.
// Path: dRPC Client → HTTP Listener (Server) → dRPC Handler
// This test ensures that a dRPC client can connect to a dRPC server
// that is listening for HTTP connections and successfully execute unary and streaming calls.
func Test_Path1_DirectHTTP_Communication(t *testing.T) {
	publicNodeInfo, err := tutil.GetPublicNodeInfo()
	if err != nil {
		t.Fatalf("Failed to get public node details: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), DefaultTimeout)
	defer cancel()

	client, err := client.New(ctx, publicNodeInfo.HTTPAddress, gv1connect.NewGreeterServiceClient, client.WithLogger(testLog))
	if err != nil {
		t.Fatalf("Failed to create client for direct HTTP communication to %s: %v", publicNodeInfo.HTTPAddress, err)
	}

	t.Run("unary_direct_http", func(t *testing.T) {
		TestClientUnaryRequest(t, client, "DirectHTTP", DefaultTimeout)
	})

	t.Run("streaming_direct_http", func(t *testing.T) {
		names := []string{"HTTP-Alice", "HTTP-Bob", "HTTP-Charlie"}
		TestClientStreamingRequest(t, client, names, DefaultTimeout)
	})
}

// Test_Path2_HTTP_Gateway_Via_Relay_Communication verifies dRPC communication through an HTTP Gateway,
// which then connects to the target server via a LibP2P relay.
// Path: dRPC Client → HTTP Listener (Gateway) → Gateway Handler → LibP2P Relay → Target LibP2P Host (Server) → dRPC Handler
// This test covers scenarios where the client speaks HTTP to a gateway, and the gateway
// handles the P2P communication, including routing through a relay if necessary.
func Test_Path2_HTTP_Gateway_Via_Relay_Communication(t *testing.T) {
	// Get direct gateway to public node address
	gn, err := tutil.GetGatewayNodeInfo()
	if err != nil {
		t.Fatalf("Failed to get gateway node info: %v", err)
	}

	// Get gateway to private node via relay node address
	grn, err := tutil.GetGatewayRelayNodeInfo()
	if err != nil {
		t.Fatalf("Failed to get gateway relay node info: %v", err)
	}

	// Get gateway to private node via discovered relay node autometically
	// This is a new test case to check if the gateway can discover and use a relay node automatically
	// without needing to specify it explicitly.
	garn, err := tutil.GetGatewayAutoRelayNodeInfo()
	if err != nil {
		t.Fatalf("Failed to get gateway auto relay node info: %v", err)
	}

	tests := []struct {
		name        string
		addr        string
		description string
	}{
		{
			name:        "http_gateway_with_direct_p2p_from_gateway",
			addr:        gn.HTTPAddress,
			description: "Client talks HTTP to Gateway, Gateway attempts direct P2P to Target Server.",
		},
		{
			name:        "http_gateway_with_fixed_relay",
			addr:        grn.HTTPAddress,
			description: "Client talks HTTP to Gateway, Gateway uses Relay to connect to P2P Target Server.",
		},
		{
			name:        "http_gateway_with_auto_relay",
			addr:        garn.HTTPAddress,
			description: "Client talks HTTP to Gateway, Gateway uses Auto Relay to connect to P2P Target Server.",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("Test Case: %s - %s", tc.name, tc.description)
			t.Logf("Client will connect to Gateway URL: %s", tc.addr)

			clientCtx, clientCancel := context.WithTimeout(context.Background(), 20*time.Second) // Increased timeout for gateway path
			defer clientCancel()

			client, clientErr := client.New(clientCtx, tc.addr, gv1connect.NewGreeterServiceClient, client.WithLogger(testLog))
			if clientErr != nil {
				t.Fatalf("Failed to create client for HTTP Gateway (%s): %v. Addr: %s", tc.name, clientErr, tc.addr)
			}

			t.Run("unary", func(t *testing.T) {
				TestClientUnaryRequest(t, client, "Gateway-"+tc.name, DefaultTimeout)
			})

			t.Run("streaming", func(t *testing.T) {
				names := []string{"Gw-Alice-" + tc.name, "Gw-Bob-" + tc.name}
				TestClientStreamingRequest(t, client, names, DefaultTimeout)
			})
		})
	}
}

// Test_Path3_DirectLibP2P_Communication verifies dRPC communication directly over LibP2P.
// Path: dRPC Client → LibP2P Host (Server) → dRPC Handler
// This test ensures that a dRPC client can connect to a dRPC server
// using its LibP2P address and perform unary and streaming calls.
// This assumes the client and server can directly discover and connect to each other on the LibP2P network.
func Test_Path3_DirectLibP2P_Communication(t *testing.T) {
	publicNodeInfo, err := tutil.GetPublicNodeInfo()
	if err != nil {
		t.Fatalf("Failed to get public node details: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), DefaultTimeout)
	defer cancel()

	client, err := client.New(ctx, publicNodeInfo.Libp2pMA, gv1connect.NewGreeterServiceClient, client.WithLogger(testLog))
	if err != nil {
		t.Fatalf("Failed to create client for direct LibP2P communication to %s: %v", publicNodeInfo.Libp2pMA, err)
	}

	t.Run("unary_direct_libp2p", func(t *testing.T) {
		TestClientUnaryRequest(t, client, "Direct-LibP2P", DefaultTimeout)
	})

	t.Run("streaming_direct_libp2p", func(t *testing.T) {
		names := []string{"LibP2P-Alice", "LibP2P-Bob", "LibP2P-Charlie"}
		TestClientStreamingRequest(t, client, names, DefaultTimeout)
	})
}

// Test_Path4_LibP2P_Via_Relay_Communication verifies dRPC communication over LibP2P routed through a relay node.
func Test_Path4_LibP2P_Via_Relay_Communication(t *testing.T) {
	relayNodeInfo, err := tutil.GetRelayNodeInfo()
	if err != nil {
		t.Fatalf("Failed to get relay node details: %v", err)
	}

	// extract private "/p2p/nodeID" node address from the relay node info
	// The goal is to connect to the private node via discovered auto relay node
	relayAddrParts := strings.Split(relayNodeInfo.Libp2pMA, "/p2p-circuit")
	if len(relayAddrParts) != 2 {
		// We can't fatalf here if we want to test the direct relay case,
		// but the auto_relay case will depend on this.
		// For now, log an error and let the auto_relay sub-test fail if it uses this.
		t.Errorf("Failed to parse relay address for auto_relay scenario: %s. Auto-relay tests might fail.", relayNodeInfo.Libp2pMA)
	}

	tests := []struct {
		name        string
		addr        string
		description string
	}{
		{
			name:        "libp2p_fixed_relay",
			addr:        relayNodeInfo.Libp2pMA,
			description: "Client connects to Target Server via a specified LibP2P Relay Node.",
		},
		{
			name:        "libp2p_auto_relay",
			addr:        relayAddrParts[1],
			description: "Client connects to Target Server via an automatically discovered LibP2P Relay Node.",
		},
	}

	for _, tc := range tests {
		tc := tc // capture range variable
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("Test Case: %s - %s", tc.name, tc.description)
			t.Logf("Client will connect to LibP2P Address: %s", tc.addr)

			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second) // Increased timeout for relay path
			defer cancel()

			client, clientErr := client.New(ctx, tc.addr, gv1connect.NewGreeterServiceClient, client.WithLogger(testLog))
			if clientErr != nil {
				t.Fatalf("Failed to create client for LibP2P communication (%s): %v. Addr: %s", tc.name, clientErr, tc.addr)
			}

			t.Run("unary", func(t *testing.T) {
				TestClientUnaryRequest(t, client, "unary-"+tc.name, DefaultTimeout)
			})

			t.Run("streaming", func(t *testing.T) {
				names := []string{"stream-" + tc.name + "-Alice", "stream-" + tc.name + "-Bob"}
				TestClientStreamingRequest(t, client, names, DefaultTimeout)
			})
		})
	}
}
