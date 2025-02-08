package drpc

import (
	"context"
	"fmt"
	"net/http"

	"github.com/libp2p/go-libp2p/core/host"
)

// NewServer creates a new ConnectRPC server that uses libp2p for transport.
func NewServer(
	ctx context.Context,
	h host.Host,
	muxHandler http.Handler, // ConnectRPC handler
) *http.Server { // Return a standard *http.Server

	// Create a libp2p listener.
	listener := NewListener(ctx, h, PROTOCOL_ID)

	// Create a standard *http.Server.
	server := &http.Server{
		Handler: muxHandler,
		Addr:    listener.Addr().String(), // Add the address to the server
	}

	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			fmt.Println("Server error:", err) // Consider using a logger instead of fmt
		}
	}()

	return server
}
