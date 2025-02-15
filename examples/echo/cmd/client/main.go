package main

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"connectrpc.com/connect"
	gv1 "github.com/omgolab/drpc/examples/echo/gen/go/greeter/v1"
	gv1connect "github.com/omgolab/drpc/examples/echo/gen/go/greeter/v1/greeterv1connect"
	"github.com/omgolab/drpc/pkg/drpc"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	ctx := context.Background()

	// Example 1: Direct HTTP client
	fmt.Println("Testing direct HTTP connection...")
	httpClient := connect.NewClient[gv1connect.GreeterServiceClient](
		http.DefaultClient,
		"http://localhost:8080",
	)
	directResponse, err := httpClient.SayHello(ctx, connect.NewRequest(&gv1.SayHelloRequest{
		Name: "Direct HTTP",
	}))
	if err != nil {
		log.Printf("Direct HTTP error: %v", err)
	} else {
		log.Printf("Direct HTTP response: %s", directResponse.Msg.Message)
	}

	// Example 2: P2P client (requires server's peer ID)
	fmt.Println("\nTesting p2p connection...")
	// Note: Get the peer ID from the server output
	p2pClient := drpc.NewClient(server.P2PHost(), gv1connect.NewGreeterServiceClient)
	p2pResponse, err := p2pClient.SayHello(ctx, connect.NewRequest(&gv1.SayHelloRequest{
		Name: "P2P Direct",
	}))
	if err != nil {
		log.Printf("P2P error: %v", err)
	} else {
		log.Printf("P2P response: %s", p2pResponse.Msg.Message)
	}

	// Example 3: Gateway client
	fmt.Println("\nTesting gateway connection...")
	gatewayClient := connect.NewClient[gv1connect.GreeterServiceClient](
		http.DefaultClient,
		"http://localhost:8080/@/ip4/127.0.0.1/tcp/9090/p2p/QmPeerID/@", // Replace QmPeerID with actual peer ID
	)
	gatewayResponse, err := gatewayClient.SayHello(ctx, connect.NewRequest(&gv1.SayHelloRequest{
		Name: "P2P Gateway",
	}))
	if err != nil {
		log.Printf("Gateway error: %v", err)
	} else {
		log.Printf("Gateway response: %s", gatewayResponse.Msg.Message)
	}
}
