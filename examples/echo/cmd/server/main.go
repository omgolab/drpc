package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/libp2p/go-libp2p"
	gv1connect "github.com/omgolab/drpc/examples/echo/gen/go/greeter/v1/greeterv1connect"
	"github.com/omgolab/drpc/examples/echo/greeter"
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
		drpc.WithHTTPPort(8082), // Use port 8082 for HTTP
		drpc.WithLogger(log),
		drpc.WithForceCloseExistingPort(true),
		// Predicate function (always returns nil, effectively disabling the check)
		drpc.WithDetachOnServerReadyPredicateFunc(),
		drpc.WithHTTPHost("localhost"),
		drpc.WithNoBootstrap(true),
	)
	if err != nil {
		log.Fatal("failed to create server", err)
	}
	defer server.Close()

	// Wait for shutdown signal
	<-sigChan
}
