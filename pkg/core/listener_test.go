package core

import (
	"context"
	"io"
	"net"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewLibp2pListener(t *testing.T) {
	// Create a test host
	h, err := libp2p.New()
	require.NoError(t, err)
	defer h.Close()

	pid := protocol.ID("/test/listener/1.0.0")
	listener := NewLibp2pListener(h, pid)
	require.NotNil(t, listener)

	// Test that the listener implements net.Listener
	var _ net.Listener = listener

	// Test Addr() method
	addr := listener.Addr()
	assert.NotNil(t, addr)
}

func TestListenerAccept(t *testing.T) {
	// Create two test hosts
	h1, err := libp2p.New()
	require.NoError(t, err)
	defer h1.Close()

	h2, err := libp2p.New()
	require.NoError(t, err)
	defer h2.Close()

	pid := protocol.ID("/test/listener/1.0.0")
	listener := NewLibp2pListener(h1, pid)
	require.NotNil(t, listener)

	// Add h1's address to h2's peerstore
	h2.Peerstore().AddAddrs(h1.ID(), h1.Addrs(), time.Hour)
	
	// Connect the hosts
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = h2.Connect(ctx, h1.Peerstore().PeerInfo(h1.ID()))
	require.NoError(t, err)

	// Wait for connection to be fully established
	time.Sleep(100 * time.Millisecond)

	// Create a stream from h2 to h1 in a goroutine
	streamCreated := make(chan struct{})
	go func() {
		stream, err := h2.NewStream(context.Background(), h1.ID(), pid)
		if err != nil {
			t.Errorf("Failed to create stream: %v", err)
			return
		}
		defer stream.Close()
		close(streamCreated)
		time.Sleep(200 * time.Millisecond) // Keep stream open
	}()

	// Accept the connection
	conn, err := listener.Accept()
	require.NoError(t, err)
	require.NotNil(t, conn)
	
	// Test that it's our Conn type
	_, ok := conn.(*Conn)
	assert.True(t, ok)

	// Wait for stream creation to complete
	select {
	case <-streamCreated:
		// Stream was successfully created
	case <-time.After(time.Second):
		t.Log("Stream creation timed out, but connection was accepted")
	}
}

func TestListenerClose(t *testing.T) {
	h, err := libp2p.New()
	require.NoError(t, err)
	defer h.Close()

	pid := protocol.ID("/test/listener/1.0.0")
	listener := NewLibp2pListener(h, pid)
	require.NotNil(t, listener)

	// Start accepting in a goroutine
	doneCh := make(chan error, 1)
	go func() {
		_, err := listener.Accept()
		doneCh <- err
	}()

	// Close the listener
	err = listener.Close()
	assert.NoError(t, err)

	// Accept should return EOF after close
	select {
	case err := <-doneCh:
		assert.Equal(t, io.EOF, err)
	case <-time.After(time.Second):
		t.Fatal("Accept didn't return after Close")
	}
}

func TestListenerAddr(t *testing.T) {
	h, err := libp2p.New()
	require.NoError(t, err)
	defer h.Close()

	pid := protocol.ID("/test/listener/1.0.0")
	listener := NewLibp2pListener(h, pid)
	require.NotNil(t, listener)

	addr := listener.Addr()
	assert.NotNil(t, addr)

	// The address should be a valid net.Addr
	assert.NotEmpty(t, addr.String())
	assert.NotEmpty(t, addr.Network())
}

func TestListenerMultipleStreams(t *testing.T) {
	// Create two test hosts
	h1, err := libp2p.New()
	require.NoError(t, err)
	defer h1.Close()

	h2, err := libp2p.New()
	require.NoError(t, err)
	defer h2.Close()

	pid := protocol.ID("/test/listener/1.0.0")
	listener := NewLibp2pListener(h1, pid)
	require.NotNil(t, listener)

	// Add h1's address to h2's peerstore and connect
	h2.Peerstore().AddAddrs(h1.ID(), h1.Addrs(), time.Hour)
	err = h2.Connect(context.Background(), h1.Peerstore().PeerInfo(h1.ID()))
	require.NoError(t, err)

	// Wait for connection to be established
	time.Sleep(100 * time.Millisecond)

	// Accept multiple connections
	numStreams := 3
	connections := make([]net.Conn, 0, numStreams)

	// Start accepting connections in a goroutine
	connCh := make(chan net.Conn, numStreams)
	go func() {
		for i := 0; i < numStreams; i++ {
			conn, err := listener.Accept()
			if err != nil {
				t.Errorf("Failed to accept connection %d: %v", i, err)
				return
			}
			connCh <- conn
		}
	}()

	// Create streams from h2 to h1
	for i := 0; i < numStreams; i++ {
		go func(index int) {
			stream, err := h2.NewStream(context.Background(), h1.ID(), pid)
			if err != nil {
				t.Errorf("Failed to create stream %d: %v", index, err)
				return
			}
			defer stream.Close()

			// Keep stream open briefly
			time.Sleep(200 * time.Millisecond)
		}(i)
	}

	// Accept all streams
	for i := 0; i < numStreams; i++ {
		select {
		case conn := <-connCh:
			require.NotNil(t, conn)
			connections = append(connections, conn)
		case <-time.After(3 * time.Second):
			t.Fatalf("Timed out waiting for connection %d", i+1)
		}
	}

	assert.Len(t, connections, numStreams)
}
