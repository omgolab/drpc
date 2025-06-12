package main

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/libp2p/go-libp2p"
	gv1connect "github.com/omgolab/drpc/demo/gen/go/greeter/v1/greeterv1connect"
	"github.com/omgolab/drpc/demo/greeter"
	"github.com/omgolab/drpc/pkg/drpc/server"
	glog "github.com/omgolab/go-commons/pkg/log"
)

func main() {
	fmt.Println("Server main function started") // Added for debugging
	log, _ := glog.New(glog.WithFileLogger("server.log"))

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create ConnectRPC mux & register greeter
	mux := http.NewServeMux()
	path, handler := gv1connect.NewGreeterServiceHandler(&greeter.Server{})
	mux.Handle(path, handler)

	server, err := server.New(ctx, mux,
		server.WithLibP2POptions(
			libp2p.ListenAddrStrings("/ip4/0.0.0.0/tcp/9090"),
			// libp2p.DisableRelay(), // Removed to allow AutoRelay to function with HOP protocol
		),
		server.WithHTTPPort(8080), // Use port 8080 for HTTP
		server.WithLogger(log),
		server.WithForceCloseExistingPort(true),
		server.WithHTTPHost("localhost"),
	)
	if err != nil {
		log.Fatal("failed to create server", err)
	}
	defer server.Close()

	// Log the server's listening addresses
	addrs := server.P2PAddrs()
	fmt.Println("P2P Server listening on:", strings.Join(addrs, "\n "))
	// Log the HTTP server's address
	fmt.Println("HTTP Server listening on:", server.HTTPAddr())

	<-ctx.Done()
	log.Info("Shutting down...")
	if err := server.Close(); err != nil {
		log.Error("Error closing dRPC server", err)
	}
}
