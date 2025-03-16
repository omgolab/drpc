package core

import (
	"net"
)

// defaultLocalFallbackAddr returns a fallback address when a proper address cannot be determined
func defaultLocalFallbackAddr() net.Addr {
	return &net.TCPAddr{
		IP:   net.IPv4(127, 0, 0, 1),
		Port: 0,
	}
}
