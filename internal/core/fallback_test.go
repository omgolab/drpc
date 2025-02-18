package core

import (
	"net"
	"testing"
)

func TestFallback(t *testing.T) {
	// Verify that defaultLocalFallbackAddr returns 127.0.0.1:0.
	fallback := defaultLocalFallbackAddr()
	tcp, ok := fallback.(*net.TCPAddr)
	if !ok {
		t.Fatalf("Fallback address is not a *net.TCPAddr")
	}
	if !tcp.IP.Equal(net.IPv4(127, 0, 0, 1)) || tcp.Port != 0 {
		t.Errorf("Fallback address mismatch: got %v, want 127.0.0.1:0", tcp)
	}
}
