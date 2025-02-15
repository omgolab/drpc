# dRPC: Decentralized ConnectRPC with libp2p

## Introduction

In today's world, decentralized applications are gaining more and more traction. However, building such applications can be complex, requiring developers to handle peer discovery, connection management, and transport protocols.

dRPC is a library that simplifies the development of decentralized applications by enabling ConnectRPC services to be transported over libp2p, a modular peer-to-peer networking stack. This allows you to build decentralized and resilient applications using the familiar ConnectRPC framework.

## What is ConnectRPC?

ConnectRPC is a modern, lightweight RPC framework that builds on HTTP/2 and Protocol Buffers. It provides a simple and efficient way to define and consume APIs.

## What is libp2p?

libp2p is a modular peer-to-peer networking stack that provides a set of protocols for peer discovery, connection management, and transport. It allows you to build decentralized applications that are resilient to network failures and censorship.

## dRPC: ConnectRPC meets libp2p

dRPC combines the best of both worlds by enabling ConnectRPC services to be transported over libp2p. This allows you to build decentralized applications with the ease and efficiency of ConnectRPC.

## Features

*   **ConnectRPC Compatibility:** Seamlessly integrates with ConnectRPC services.
*   **libp2p Transport:** Leverages libp2p for peer discovery, connection management, and transport.
*   **Decentralized Architecture:** Enables building decentralized applications without relying on centralized servers.
*   **Resilience:** Provides resilience against network failures and censorship.
*   **Gateway Support:** Allows clients to connect to services via a gateway using multiaddrs.
*   **Streaming Support:** Supports ConnectRPC streaming methods over libp2p.

## Getting Started

To get started with dRPC, you need to have Go 1.20 or later, libp2p, and ConnectRPC installed.

### Installation

```bash
go get github.com/omgolab/drpc
```

### Usage

#### Server

```go
package main

import (
	"context"
	"fmt"
	"net/http"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"connectrpc.com/connect"
	"github.com/omgolab/drpc/pkg/drpc"
	greeterv1 "github.com/omgolab/drpc/examples/echo/gen/go/greeter/v1"
	greeterv1connect "github.com/omgolab/drpc/examples/echo/gen/go/greeter/v1/greeterv1connect"
	"github.com/omgolab/drpc/examples/echo/greeter"
)

func main() {
	ctx := context.Background()
	h, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/0.0.0.0/tcp/0"))
	if err != nil {
		panic(err)
	}
	fmt.Println("Host ID:", h.ID())
	fmt.Println("Listening on:")
	for _, addr := range h.Addrs() {
		fmt.Println(addr)
	}

	// Create a new Greeter service.
	greeterServer := &greeter.Server{}

	// Create a new Connect service handler.
	mux := http.NewServeMux()
	path, handler := greeterv1connect.NewGreeterServiceHandler(greeterServer)
	mux.Handle(path, handler)

	// Create a new dRPC server.
	server := drpc.NewServer(ctx, h, mux)

	select {}
}
```

#### Client

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"connectrpc.com/connect"
	"github.com/omgolab/drpc/pkg/drpc"
	greeterv1connect "github.com/omgolab/drpc/examples/echo/gen/go/greeter/v1/greeterv1connect"
)

func main() {
	h, err := libp2p.New(libp2p.NoListenAddrs)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Client ID:", h.ID())

	serverHost, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/0.0.0.0/tcp/0"))
	if err != nil {
		panic(err)
	}
	client := drpc.NewClient(serverHost, greeterv1connect.NewGreeterServiceClient)

	// Call the SayHello method.
	res, err := client.SayHello(context.Background(), connect.NewRequest(&greeterv1.SayHelloRequest{Name: "World"}))
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(res.Msg.Message)
}
```

#### Gateway

To connect to a service via a gateway, use the following URL format:

```
http://localhost:8080/gateway/<multiadd_with_server_peerID>/user.services.v1.UserService/ListUser
```

## Conclusion

dRPC simplifies the development of decentralized applications by enabling ConnectRPC services to be transported over libp2p. This allows you to build resilient and scalable applications using familiar tools and frameworks.

## Future Work

*   Automatic generation of client and server code using a `protoc` plugin.
*   Support for other programming languages (e.g., Python, Java).
*   Integration with other transport protocols (e.g., WebSockets).
*   Automatic discovery of dRPC services.