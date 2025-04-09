package core

import (
	"net"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/multiformats/go-multiaddr"
	mn "github.com/multiformats/go-multiaddr/net"
)

var _ net.Conn = (*Conn)(nil) // Ensure Conn still implements net.Conn

// Helper to convert multiaddr to net.Addr, or return fallback on error.
func netAddrOrFallback(ma multiaddr.Multiaddr) net.Addr {
	addr, err := mn.ToNetAddr(ma)
	if err != nil {
		return defaultLocalFallbackAddr()
	}
	return addr
}

// Conn is a net.Conn that wraps a libp2p stream.
type Conn struct {
	network.Stream
}

func (c *Conn) LocalAddr() net.Addr {
	return netAddrOrFallback(c.Stream.Conn().LocalMultiaddr())
}

func (c *Conn) RemoteAddr() net.Addr {
	return netAddrOrFallback(c.Stream.Conn().RemoteMultiaddr())
}
