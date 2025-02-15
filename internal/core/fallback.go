package core

import "net"

// defaultLocalFallbackAddr returns a fallback local address.
func defaultLocalFallbackAddr() net.Addr {
	return &net.TCPAddr{
		IP:   net.IPv4(127, 0, 0, 1),
		Port: 0,
	}
}
