package core

import (
	"net"

	"github.com/libp2p/go-libp2p/core/network"
	mn "github.com/multiformats/go-multiaddr/net"
)

var _ net.Conn = (*Conn)(nil)

// Conn is a net.Conn that wraps a libp2p stream.
type Conn struct {
	network.Stream
}

func (c *Conn) LocalAddr() net.Addr {
	addr, err := mn.ToNetAddr(c.Stream.Conn().LocalMultiaddr())
	if err != nil {
		return defaultLocalFallbackAddr()
	}
	return addr
}

func (c *Conn) RemoteAddr() net.Addr {
	addr, err := mn.ToNetAddr(c.Stream.Conn().RemoteMultiaddr())
	if err != nil {
		return defaultLocalFallbackAddr()
	}
	return addr
}
