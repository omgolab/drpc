package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"connectrpc.com/connect"
	gv1 "github.com/omgolab/drpc/demo/gen/go/greeter/v1"
	gv1connect "github.com/omgolab/drpc/demo/gen/go/greeter/v1/greeterv1connect"
	"github.com/omgolab/drpc/pkg/drpc"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	serverMultiaddr, err := getServerInfo()
	if err != nil {
		log.Fatalf("Failed to get server info: %v", err)
	}

	// Create context
	ctx := context.Background()

	// Direct libp2p connection
	fmt.Println("\n=== Scenario 1: Direct libp2p connection ===")
	libp2pClient, err := newLibp2pClient(serverMultiaddr)
	if err != nil {
		log.Printf("Failed to create libp2p client: %v", err)
		fmt.Println("Testing direct libp2p connection...")
	} else {
		// Test unary call
		fmt.Println("Testing unary call via direct libp2p...")
		resp, err := libp2pClient.SayHello(ctx, connect.NewRequest(&gv1.SayHelloRequest{
			Name: "Direct libp2p",
		}))
		if err != nil {
			log.Printf("Direct libp2p call failed: %v", err)
		} else {
			fmt.Printf("Response: %s\n", resp.Msg.Message)
		}
	}

	fmt.Println("\n=== Scenario 2: HTTP Connect-RPC -> libp2p ===")
	if err := testHTTPConnect(ctx); err != nil {
		log.Printf("HTTP Connect error: %v", err)
	}

	fmt.Println("\n=== Scenario 3: Connect-RPC Gateway -> libp2p ===")
	if err := testGateway(ctx, serverMultiaddr); err != nil {
		log.Printf("Gateway error: %v", err)
	}

	fmt.Println("\n=== Scenario 4: Server Streaming (via all methods) ===")

	if err := testStreaming(ctx, serverMultiaddr); err != nil {
		log.Printf("Streaming error: %v", err)
	}

}

func newLibp2pClient(addrStr string) (gv1connect.GreeterServiceClient, error) {
	// Use the new drpc.NewClient signature which now takes a string address
	// instead of host, peerID and addresses list
	client, err := drpc.NewClient(context.Background(), addrStr, gv1connect.NewGreeterServiceClient)

	if err != nil {
		return nil, fmt.Errorf("failed to create drpc client: %w", err)
	}
	return client, nil
}

func testHTTPConnect(ctx context.Context) error {
	// Create an HTTP client
	httpClient := gv1connect.NewGreeterServiceClient(
		http.DefaultClient,
		"http://localhost:8082", // Changed port to 8082
	)

	// Test unary call
	fmt.Println("Testing unary call via HTTP...")
	resp, err := httpClient.SayHello(ctx, connect.NewRequest(&gv1.SayHelloRequest{
		Name: "HTTP Connect",
	}))
	if err != nil {
		return fmt.Errorf("HTTP call failed: %w", err)
	}

	fmt.Printf("Response: %s\n", resp.Msg.Message)
	return nil
}

func testGateway(ctx context.Context, serverMultiaddr string) error {
	// Test unary call via gateway...
	fmt.Println("Testing unary call via gateway...")
	// Use fixed HTTP gateway address instead of extracting from multiaddr.
	gatewayBaseURL := "http://localhost:8082"            // Changed port to 8082
	fmt.Printf("Gateway Base URL: %s\n", gatewayBaseURL) // Debug print

	// Create custom HTTP client with proper configuration
	httpClient := &http.Client{
		Transport: &http.Transport{
			ForceAttemptHTTP2: true,
		},
	}

	// Create Connect-RPC client with proper configuration
	gatewayClient := gv1connect.NewGreeterServiceClient(
		httpClient,
		gatewayBaseURL,
		connect.WithHTTPGet(),
	)

	// Create request with proper headers
	req := connect.NewRequest(&gv1.SayHelloRequest{
		Name: "Gateway",
	})

	// Add headers to request
	req.Header().Set("Content-Type", "application/connect+proto")
	req.Header().Set("Accept", "application/connect+proto")
	req.Header().Set("Connect-Protocol-Version", "1")
	req.Header().Set("Connect-Raw-Response", "1")
	req.Header().Set("Accept-Encoding", "identity")
	req.Header().Set("Content-Encoding", "identity")
	req.Header().Set("User-Agent", "connect-go/1.0")
	req.Header().Set("Connect-Timeout-Ms", "15000")
	req.Header().Set("Connect-Accept-Encoding", "gzip")

	resp, err := gatewayClient.SayHello(ctx, req)
	if err != nil {
		return fmt.Errorf("gateway call failed: %w", err)
	}

	fmt.Printf("Response: %s\n", resp.Msg.Message)
	return nil
}

func testStreaming(ctx context.Context, serverMultiaddr string) error {
	// Test streaming via direct libp2p
	fmt.Println("Testing streaming via direct libp2p...")
	libp2pClient, err := newLibp2pClient(serverMultiaddr)
	if err != nil {
		fmt.Printf("Failed to create libp2p client: %v\n", err)
		fmt.Println("Skipping direct libp2p streaming test...")
	} else {
		stream, err := libp2pClient.StreamingEcho(ctx, connect.NewRequest(&gv1.StreamingEchoRequest{
			Message: "Direct libp2p stream",
		}))
		if err != nil {
			fmt.Printf("Failed to start libp2p stream: %v\n", err)
		} else {
			for stream.Receive() {
				fmt.Printf("Received from direct libp2p: %s\n", stream.Msg().Message)
			}
			if err := stream.Err(); err != nil {
				fmt.Printf("Stream error: %v\n", err)
			}
		}
	}

	// Test streaming via HTTP
	fmt.Println("\nTesting streaming via HTTP...")
	httpClient := gv1connect.NewGreeterServiceClient(
		http.DefaultClient,
		"http://localhost:8082", // Changed port to 8082
	)

	stream, err := httpClient.StreamingEcho(ctx, connect.NewRequest(&gv1.StreamingEchoRequest{
		Message: "HTTP stream",
	}))
	if err != nil {
		return fmt.Errorf("failed to start HTTP stream: %w", err)
	}

	for stream.Receive() {
		fmt.Printf("Received from HTTP: %s\n", stream.Msg().Message)
	}
	if err := stream.Err(); err != nil {
		return fmt.Errorf("stream error: %w", err)
	}

	// Test streaming via gateway
	fmt.Println("\nTesting streaming via gateway...")
	gatewayAddrStr := "http://localhost:8082" // Changed port to 8082

	gatewayClient := gv1connect.NewGreeterServiceClient(
		http.DefaultClient,
		gatewayAddrStr,
	)

	fmt.Printf("Gateway URL: %s\n", gatewayAddrStr) // Debug print

	stream, err = gatewayClient.StreamingEcho(ctx, connect.NewRequest(&gv1.StreamingEchoRequest{
		Message: "Gateway stream",
	}))
	if err != nil {
		return fmt.Errorf("failed to start gateway stream: %w", err)
	}

	for stream.Receive() {
		fmt.Printf("Received from gateway: %s\n", stream.Msg().Message)
	}
	if err := stream.Err(); err != nil {
		return fmt.Errorf("stream error: %w", err)
	}

	return nil
}

type PeerInfo struct {
	ID    string   `json:"ID"`
	Addrs []string `json:"Addrs"`
	Port  string   `json:"Port"`
}

func getServerInfo() (string, error) {
	var info PeerInfo
	var err error

	for i := 0; i < 100; i++ {
		resp, err := http.Get("http://localhost:8080/p2pinfo")
		if err != nil {
			time.Sleep(100 * time.Millisecond)
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			time.Sleep(100 * time.Millisecond)
			continue
		}

		if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
			return "", err
		}

		if len(info.Addrs) == 0 {
			time.Sleep(100 * time.Millisecond)
			continue
		}
		return info.Addrs[0], nil
	}
	return "", fmt.Errorf("failed to get server info after multiple retries: %w", err)

}
