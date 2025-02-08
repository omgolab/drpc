package drpc

import (
	"net"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	mn "github.com/multiformats/go-multiaddr/net"
)

var _ net.Conn = (*Conn)(nil)

// Conn is a net.Conn that wraps a libp2p stream.
type Conn struct {
	network.Stream
}

func (c *Conn) Read(b []byte) (n int, err error) {
	return c.Stream.Read(b)
}

func (c *Conn) Write(b []byte) (n int, err error) {
	return c.Stream.Write(b)
}

func (c *Conn) Close() error {
	return c.Stream.Close()
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

func (c *Conn) SetDeadline(t time.Time) error {
	return c.Stream.SetDeadline(t)
}

func (c *Conn) SetReadDeadline(t time.Time) error {
	return c.Stream.SetReadDeadline(t)
}

func (c *Conn) SetWriteDeadline(t time.Time) error {
	return c.Stream.SetWriteDeadline(t)
}
