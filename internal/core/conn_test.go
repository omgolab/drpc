package core

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/protocol"
	mocknet "github.com/libp2p/go-libp2p/p2p/net/mock"
	"github.com/multiformats/go-multiaddr"
	mn "github.com/multiformats/go-multiaddr/net"
)

var toNetAddr = func(addr multiaddr.Multiaddr) (net.Addr, error) {
	return mn.ToNetAddr(addr)
}

// mockToNetAddr mocks toNetAddr to return an error.
func mockToNetAddr(addr multiaddr.Multiaddr) (net.Addr, error) {
	return nil, errors.New("invalid multiaddr")
}

// TestConnAddresses verifies that valid multiaddrs are properly converted.
func TestConnAddresses(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Create a mock network
	mnet := mocknet.New()
	defer mnet.Close()

	// Create two mock peers
	peerA, err := mnet.GenPeer()
	if err != nil {
		t.Fatalf("Failed to generate peerA: %v", err)
	}
	peerB, err := mnet.GenPeer()
	if err != nil {
		t.Fatalf("Failed to generate peerB: %v", err)
	}

	// Connect the peers
	_, err = mnet.ConnectPeers(peerA.ID(), peerB.ID())
	if err != nil {
		t.Fatalf("Failed to connect peers: %v", err)
	}

	// Create a mock stream between the peers
	stream, err := peerA.NewStream(ctx, peerB.ID(), protocol.ID("/drpc/1.0.0"))
	if err != nil {
		t.Fatalf("Failed to create stream: %v", err)
	}
	defer stream.Close()

	// Wrap the stream in a Conn
	conn := &Conn{Stream: stream}

	// Get the local and remote addresses
	localAddr := conn.LocalAddr()
	remoteAddr := conn.RemoteAddr()

	// Get the expected local and remote addresses from the stream
	expectedLocal, err := toNetAddr(stream.Conn().LocalMultiaddr())
	if err != nil {
		t.Fatalf("Failed to convert local multiaddr: %v", err)
	}
	expectedRemote, err := toNetAddr(stream.Conn().RemoteMultiaddr())
	if err != nil {
		t.Fatalf("Failed to convert remote multiaddr: %v", err)
	}

	// Compare the addresses
	if localAddr.String() != expectedLocal.String() {
		t.Errorf("LocalAddr mismatch: got %s, want %s", localAddr, expectedLocal)
	}
	if remoteAddr.String() != expectedRemote.String() {
		t.Errorf("RemoteAddr mismatch: got %s, want %s", remoteAddr, expectedRemote)
	}
}

// TestFallbackOnInvalidMultiaddr verifies that invalid multiaddrs yield fallback addresses.
func TestFallbackOnInvalidMultiaddr(t *testing.T) {
	// Create a mock network
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	mnet := mocknet.New()
	defer mnet.Close()

	// Create two mock peers
	peerA, err := mnet.GenPeer()
	if err != nil {
		t.Fatalf("Failed to generate peerA: %v", err)
	}
	peerB, err := mnet.GenPeer()
	if err != nil {
		t.Fatalf("Failed to generate peerB: %v", err)
	}

	// Connect the peers
	_, err = mnet.ConnectPeers(peerA.ID(), peerB.ID())
	if err != nil {
		t.Fatalf("Failed to connect peers: %v", err)
	}

	// Create a mock stream between the peers
	stream, err := peerA.NewStream(ctx, peerB.ID(), protocol.ID("/drpc/1.0.0"))
	if err != nil {
		t.Fatalf("Failed to create stream: %v", err)
	}
	defer stream.Close()

	// Temporarily replace toNetAddr with the mock function
	toNetAddr = mockToNetAddr
	defer func() {
		toNetAddr = func(addr multiaddr.Multiaddr) (net.Addr, error) { // Restore original function
			return mn.ToNetAddr(addr)
		}
	}()

	// Wrap the stream in a Conn
	wrappedConn := &Conn{Stream: stream}

	// Get the local and remote addresses
	localAddr := wrappedConn.LocalAddr()
	remoteAddr := wrappedConn.RemoteAddr()

	// Check that the addresses fallback to the default
	fallback := defaultLocalFallbackAddr().String()
	if localAddr.String() != fallback {
		t.Errorf("LocalAddr fallback mismatch: got %s, want %s", localAddr, fallback)
	}
	if remoteAddr.String() != fallback {
		t.Errorf("RemoteAddr fallback mismatch: got %s, want %s", remoteAddr, fallback)
	}
}
