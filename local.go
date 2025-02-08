package drpc

import "net"

// Returns a default local address, using localhost and a placeholder port
func defaultLocalFallbackAddr() net.Addr {
	// Use port 0 as a placeholder
	return &net.TCPAddr{
		IP:   net.IPv4(127, 0, 0, 1),
		Port: 0,
	}
}
