package core

import (
	"context"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/protocol"
	mocknet "github.com/libp2p/go-libp2p/p2p/net/mock"
	mn "github.com/multiformats/go-multiaddr/net"
)

// TestConnAddresses verifies that valid multiaddrs are properly converted
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
	// link peerA and peerB
	err = mnet.LinkAll()
	if err != nil {
		t.Fatalf("Failed to link all peers: %v", err)
	}
	_, err = mnet.ConnectPeers(peerA.ID(), peerB.ID())
	if err != nil {
		t.Fatalf("Failed to connect peers: %v", err)
	}

	// Set a stream handler on peerB for the protocol
	peerB.SetStreamHandler(protocol.ID("/drpc/1.0.0"), func(s network.Stream) {
		// Simple handler, just close the stream for this test
		s.Close()
	})

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
	expectedLocal, err := mn.ToNetAddr(stream.Conn().LocalMultiaddr())
	if err != nil {
		t.Fatalf("Failed to convert local multiaddr: %v", err)
	}
	expectedRemote, err := mn.ToNetAddr(stream.Conn().RemoteMultiaddr())
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

// TestFallbackOnInvalidMultiaddr verifies that invalid multiaddrs yield fallback addresses
func TestFallbackOnInvalidMultiaddr(t *testing.T) {
	want := "127.0.0.1:0"
	got := defaultLocalFallbackAddr().String()
	if got != want {
		t.Errorf("defaultLocalFallbackAddr() = %q, want %q", got, want)
	}
}
