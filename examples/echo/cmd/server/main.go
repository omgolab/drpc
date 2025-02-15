package main

import (
    "context"
    "fmt"
    "log"
    "net/http"
    "os"
    "os/signal"
    "syscall"

    gv1 "github.com/omgolab/drpc/examples/echo/gen/go/greeter/v1"
    gv1connect "github.com/omgolab/drpc/examples/echo/gen/go/greeter/v1/greeterv1connect"
    "github.com/omgolab/drpc/examples/echo/greeter"
    "github.com/omgolab/drpc/pkg/drpc"

    "connectrpc.com/connect"
)

func main() {
    log.SetFlags(log.LstdFlags | log.Lshortfile)

    // Create context that can be cancelled
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    // Set up signal handling
    sigChan := make(chan os.Signal, 1)
    signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

    // Create ConnectRPC mux & register greeter
    mux := http.NewServeMux()
    path, handler := gv1connect.NewGreeterServiceHandler(&greeter.Server{})
    mux.Handle(path, handler)

    // Create server with options
    server, err := drpc.NewServer(ctx, mux,
        drpc.WithP2PPort(9090),
        drpc.WithHTTPPort(8080),
        drpc.WithHTTPHost("localhost"),
        drpc.WithHTTPEnabled(true),
    )
    if err != nil {
        log.Fatalf("Failed to create server: %v", err)
    }
    defer server.Close()

    // Print listening addresses
    fmt.Println("Server listening on:")
    for _, addr := range server.Addrs() {
        fmt.Printf("  %s\n", addr)
    }

    // Create a test client using the p2p host
    client := drpc.NewClient(server.P2PHost(), gv1connect.NewGreeterServiceClient)

    // Test the connection
    res, err := client.SayHello(ctx, connect.NewRequest(&gv1.SayHelloRequest{
        Name: "Alice",
    }))
    if err != nil {
        log.Printf("Test call failed: %v", err)
    } else {
        log.Printf("Test call succeeded: %s", res.Msg.Message)
    }

    // Wait for shutdown signal
    <-sigChan
    fmt.Println("\nShutting down gracefully...")
}
