package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"

	"connectrpc.com/connect"
	"github.com/omgolab/drpc/pkg/drpc/server"
	glog "github.com/omgolab/go-commons/pkg/log"

	greeterpb "github.com/omgolab/drpc/demo/gen/go/greeter/v1"
	"github.com/omgolab/drpc/demo/gen/go/greeter/v1/greeterv1connect"
)

// GreeterServer implements the Greeter service
type GreeterServer struct {
	greeterv1connect.UnimplementedGreeterServiceHandler
}

// SayHello implements the unary SayHello method
func (g *GreeterServer) SayHello(ctx context.Context, req *connect.Request[greeterpb.SayHelloRequest]) (*connect.Response[greeterpb.SayHelloResponse], error) {
	if req.Msg == nil || req.Msg.Name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("name cannot be empty"))
	}
	log.Printf("SayHello called with name: %s", req.Msg.Name)
	return connect.NewResponse(&greeterpb.SayHelloResponse{
		Message: fmt.Sprintf("Hello, %s (from Go server)", req.Msg.Name),
	}), nil
}

// BidiStreamingEcho implements bidirectional streaming echo
func (g *GreeterServer) BidiStreamingEcho(ctx context.Context, stream *connect.BidiStream[greeterpb.BidiStreamingEchoRequest, greeterpb.BidiStreamingEchoResponse]) error {
	log.Println("BidiStreamingEcho called")

	for {
		// Check for context cancellation
		if err := ctx.Err(); err != nil {
			log.Printf("Context error: %v", err)
			return err
		}

		// Receive a message
		reqMessage, err := stream.Receive()
		if err != nil {
			if errors.Is(err, io.EOF) {
				log.Println("Client closed the stream (EOF)")
				return nil // Clean end of stream
			}
			log.Printf("Error receiving from stream: %v", err)
			return connect.NewError(connect.CodeUnknown, fmt.Errorf("receive error: %w", err))
		}

		log.Printf("Received request with name: '%s'", reqMessage.Name)

		// Echo back with greeting
		if err := stream.Send(&greeterpb.BidiStreamingEchoResponse{
			Greeting: fmt.Sprintf("Hello, %s (from Go server)", reqMessage.Name),
		}); err != nil {
			log.Printf("Error sending response: %v", err)
			return fmt.Errorf("send error: %w", err)
		}
	}
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger, glogErr := glog.New()
	if glogErr != nil {
		log.Fatalf("Failed to create glog logger: %v", glogErr)
	}

	// Set up Connect RPC service
	mux := http.NewServeMux()
	greeterPath, greeterHandler := greeterv1connect.NewGreeterServiceHandler(new(GreeterServer))
	mux.Handle(greeterPath, greeterHandler)
	logger.Info("Registered GreeterService handler", glog.LogFields{"path": greeterPath})

	// Create a new dRPC server with p2p, HTTP, web-streaming capabilities
	server, err := server.New(
		ctx,
		mux,
		server.WithLogger(logger),
	)
	if err != nil {
		logger.Fatal("Failed to create dRPC server", err)
	}

	// Get and display the p2p addresses
	p2pAddrs := server.P2PAddrs()
	if len(p2pAddrs) > 0 {
		fmt.Printf("BRIDGE_SERVER_MULTIADDR_P2P:%s\n", p2pAddrs[0])
	} else {
		logger.Fatal("Could not determine any usable multiaddress", errors.New("no multiaddresses available"))
	}

	fmt.Println("Go server is running and listening for libp2p connections...")

	<-ctx.Done()
	logger.Info("Shutting down...")
	if err := server.Close(); err != nil {
		logger.Error("Error closing dRPC server", err)
	}
}
