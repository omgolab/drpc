package main

import (
	"context"
	"fmt"
	"net/http"

	"github.com/libp2p/go-libp2p"
	gv1connect "github.com/omgolab/drpc/examples/echo/gen/go/greeter/v1/greeterv1connect"
	"github.com/omgolab/drpc/examples/echo/greeter"
	"github.com/omgolab/drpc/pkg/drpc"
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

	server, err := drpc.NewServer(ctx, mux,
		drpc.WithLibP2POptions(
			libp2p.ListenAddrStrings("/ip4/0.0.0.0/tcp/9090"),
			libp2p.DisableRelay(),
			libp2p.NoSecurity, // Disable TLS
		),
		drpc.WithHTTPPort(8080), // Use port 8080 for HTTP
		drpc.WithLogger(log),
		drpc.WithForceCloseExistingPort(true),
		// Predicate function (always returns nil, effectively disabling the check)
		drpc.WithDetachedServer(),
		drpc.WithHTTPHost("localhost"),
	)
	if err != nil {
		log.Fatal("failed to create server", err)
	}
	defer server.Close()

	// Log the server's listening addresses
	addrs := server.Addrs()
	fmt.Println("Server listening on:", addrs)
}
