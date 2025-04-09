package core

import (
	"context"
	"io"
	"net"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/protocol"
	mn "github.com/multiformats/go-multiaddr/net"
)

var _ net.Listener = (*listener)(nil)

type listener struct {
	h        host.Host
	streamCh chan network.Stream
	ctx      context.Context
	cancel   context.CancelFunc
}

// NewStreamBridgeListener bridges a libp2p network.Stream to a net.Conn
func NewStreamBridgeListener(
	h host.Host,
	pid protocol.ID,
) net.Listener {
	l := listener{
		h:        h,
		streamCh: make(chan network.Stream, 1), // Use a buffered channel (size 1)
	}
	// Use context.Background() so the listener's lifecycle isn't tied to the setup context.
	// It will only close when l.Close() is called.
	l.ctx, l.cancel = context.WithCancel(context.Background())

	h.SetStreamHandler(pid, func(s network.Stream) {
		l.streamCh <- s
	})

	return &l
}

func (l *listener) Accept() (net.Conn, error) {
	select {
	case <-l.ctx.Done():
		return nil, io.EOF
	case s := <-l.streamCh:
		return &Conn{Stream: s}, nil // Use the Conn struct from conn.go
	}
}

func (l *listener) Addr() net.Addr {
	addrs := l.h.Network().ListenAddresses()
	if len(addrs) > 0 {
		for _, a := range addrs {
			na, err := mn.ToNetAddr(a)
			if err == nil {
				return na
			}
		}
	}

	// Investigate: is this correct thing to do?
	return defaultLocalFallbackAddr()
}

func (l *listener) Close() error {
	l.cancel()
	return nil
}
