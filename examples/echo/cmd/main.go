package main

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/omgolab/drpc"
	greeterv1 "github.com/omgolab/drpc/examples/echo/gen/go/greeter/v1"
	"github.com/omgolab/drpc/examples/echo/gen/go/greeter/v1/greeterv1connect"
	"github.com/omgolab/drpc/examples/echo/greeter"

	"connectrpc.com/connect"
)

func setupHosts(ctx context.Context) (host.Host, host.Host) {
	// hosts
	ha, _ := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/9000"))
	hb, _ := libp2p.New(libp2p.NoListenAddrs)

	// connect
	err := hb.Connect(ctx, peer.AddrInfo{
		ID:    ha.ID(),
		Addrs: ha.Addrs(),
	})
	if err != nil {
		log.Fatal(err)
	}

	return ha, hb
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	// ctx
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// hosts
	hs, hc := setupHosts(ctx)
	defer hs.Close()
	defer hc.Close()

	// server
	{
		// ConnectRPC server & register greeter
		mux := http.NewServeMux()
		path, handler := greeterv1connect.NewGreeterServiceHandler(&greeter.Server{})
		mux.Handle(path, handler)

		// serve ConnectRPC server over libp2p host
		server := drpc.NewServer(ctx, hs, mux)
		fmt.Printf("Server listening on %v\n", server.Addr)
	}

	// client
	{
		// client conn
		client := drpc.NewClient(hs, greeterv1connect.NewGreeterServiceClient)

		// SayHello
		res, err := client.SayHello(ctx, connect.NewRequest(&greeterv1.SayHelloRequest{
			Name: "Alice",
		}))
		if err != nil {
			log.Fatal(err)
		}

		// print result
		log.Println(res.Msg.Message)
	}
}
