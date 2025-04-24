# dRPC: ConnectRPC over libp2p

dRPC is a Go library that allows you to use ConnectRPC services over the libp2p network. It combines the ease of use of ConnectRPC with the decentralized and resilient nature of libp2p.

## Features

- **ConnectRPC Compatibility:** Works seamlessly with existing ConnectRPC services.
- **libp2p Transport:** Uses libp2p for peer discovery and connection management.
- **Decentralized Architecture:** Enables building decentralized applications.
- **Resilience:** Provides resilience against network failures.
- **HTTP Gateway:** Offers an HTTP gateway for non-libp2p clients.

## Architecture

```
[ConnectRPC Client] <-> [libp2p] <-> [dRPC Server] <-> [ConnectRPC Service]
```

The dRPC server can also expose an HTTP gateway:

```
[HTTP Client] <-> [HTTP Gateway] <-> [dRPC Server]
```

## Getting Started

### Prerequisites

- Go 1.20 or later
- [libp2p](https://libp2p.io/) (installed automatically as a Go dependency)
- [ConnectRPC](https://connectrpc.com/) (installed automatically as a Go dependency)
- [buf](https://buf.build/) (for generating code from `.proto` files)

### Installation

```bash
go get github.com/omgolab/drpc
```

### Usage

First define your service using protocol buffer, for example save the following into `proto/greeter/v1/greeter.proto`

```protobuf
syntax = "proto3";

package greeter.v1;

option go_package = "github.com/omgolab/drpc/demo/gen/go/greeter/v1;greeterv1";

service GreeterService {
  rpc SayHello (SayHelloRequest) returns (SayHelloResponse) {}
}

message SayHelloRequest {
  string name = 1;
}

message SayHelloResponse {
  string message = 1;
}
```

Then run following command to generate go code from proto definition

```
buf generate
```

#### Server

```go
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/libp2p/go-libp2p"
	gv1connect "github.com/omgolab/drpc/demo/gen/go/greeter/v1/greeterv1connect"
	"github.com/omgolab/drpc/demo/greeter"
	"github.com/omgolab/drpc/internal/gateway"
	"github.com/omgolab/drpc/pkg/drpc"
	glog "github.com/omgolab/go-commons/pkg/log"
)

func main() {
	log, _ := glog.New(glog.WithFileLogger("server.log"))

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Setup signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Create ConnectRPC mux & register greeter
	mux := http.NewServeMux()
	path, handler := gv1connect.NewGreeterServiceHandler(&greeter.Server{})
	mux.Handle(path, handler)

	server, err := drpc.NewServer(ctx, mux,
		drpc.WithLibP2POptions(
			libp2p.ListenAddrStrings("/ip4/0.0.0.0/tcp/9090"),
			libp2p.DisableRelay(),
			libp2p.NoSecurity, // Disable TLS
		),
		drpc.WithHTTPPort(8080), // Use port 8080
		drpc.WithLogger(log),
		drpc.WithForceCloseExistingPort(true),
		drpc.WithHTTPHost("localhost"),
		drpc.WithNoBootstrap(true),
	)
	if err != nil {
		log.Fatal("failed to create server", err)
	}
	defer server.Close()

	// Add p2pinfo handler
	mux.HandleFunc("/p2pinfo", gateway.P2PInfoHandler(server.P2PHost()))

	// Print listening addresses
	log.Println("Server listening on:")
	for _, addr := range server.Addrs() {
		log.Printf("  %s\n", addr)
	}

	// Wait for shutdown signal
	<-sigChan
}

```

#### Client

```go
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"connectrpc.com/connect"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/peer"
	gv1 "github.com/omgolab/drpc/demo/gen/go/greeter/v1"
	gv1connect "github.com/omgolab/drpc/demo/gen/go/greeter/v1/greeterv1connect"
	"github.com/omgolab/drpc/pkg/drpc"
	"github.com/omgolab/drpc/demo/cmd/client"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	serverMultiaddr, err := client.GetServerInfo()
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
	// Create a libp2p host for the client
	host, err := libp2p.New(
		libp2p.NoListenAddrs,
		libp2p.NoSecurity, // Disable TLS
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create libp2p host: %w", err)
	}

	// Parse the server's multiaddr
	addr, err := peer.AddrInfoFromString(addrStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse address %s: %v", addrStr, err)
	}

	// Connect to the server
	if err := host.Connect(context.Background(), *addr); err != nil {
		return nil, fmt.Errorf("failed to connect to %s: %v", addrStr, err)
	}

	// Successfully connected
	return drpc.NewClient(host, addr.ID, []string{addrStr}, gv1connect.NewGreeterServiceClient), nil
}

func testHTTPConnect(ctx context.Context) error {
	// Create an HTTP client
	httpClient := gv1connect.NewGreeterServiceClient(
		http.DefaultClient,
		"http://localhost:8080",
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
	gatewayBaseURL := "http://localhost:8080"
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
	fmt.Println("Skipping HTTP Connect tests...")
	// httpClient := gv1connect.NewGreeterServiceClient(
	// 	http.DefaultClient,
	// 	"http://localhost:8080",
	// )

	// stream, err := httpClient.StreamingEcho(ctx, connect.NewRequest(&gv1.StreamingEchoRequest{
	// 	Message: "HTTP stream",
	// }))
	// if err != nil {
	// 	return fmt.Errorf("failed to start HTTP stream: %w", err)
	// }

	// for stream.Receive() {
	// 	fmt.Printf("Received from HTTP: %s\n", stream.Msg().Message)
	// }
	// if err := stream.Err(); err != nil {
	// 	return fmt.Errorf("stream error: %w", err)
	// }

	// Test streaming via gateway
	fmt.Println("\nTesting streaming via gateway...")
	gatewayAddrStr := "http://localhost:8080"

	gatewayClient := gv1connect.NewGreeterServiceClient(
		http.DefaultClient,
		gatewayAddrStr,
	)

	fmt.Printf("Gateway URL: %s\n", gatewayAddrStr) // Debug print

	stream, err := gatewayClient.StreamingEcho(ctx, connect.NewRequest(&gv1.StreamingEchoRequest{
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
```

These examples use the `demo` service. You'll need to adapt them to your specific service definition.

## Running the Examples

1.  **Generate the code:** From the `demo` directory, run `buf generate`.
2.  **Build:** From the root of the repository, run:
    ```bash
    go build ./demo/cmd/server
    go build ./demo/cmd/client
    ```
3.  **Run the server:**
    ```bash
    ./server
    ```
    This will start the server in detached mode, logging output to `server.log` and errors to `server.err`.
4.  **Run the client:** In a separate terminal, run:
    ```bash
    ./client
    ```
    The client will attempt to connect using direct libp2p, HTTP, and the gateway (if the server is running with the gateway enabled).

## License

MIT License
