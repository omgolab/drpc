package gateway

import (
	"context"
	"fmt"
	"strconv"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
)

// ResolveMultiaddrs resolves multiple multiaddrs for the same peer
// Implements transport prioritization as described in the flow diagram:
// 1. WebTransport/Quick
// 2. WebRTC
// 3. TCP/WS
func ResolveMultiaddrs(addrs []string) (string, error) {
	multiaddrs := make([]ma.Multiaddr, 0, len(addrs))
	for _, addr := range addrs {
		maddr, err := ma.NewMultiaddr(addr)
		if err != nil {
			continue // Skip invalid addresses
		}
		multiaddrs = append(multiaddrs, maddr)
	}

	if len(multiaddrs) == 0 {
		return "", fmt.Errorf("no valid multiaddresses found")
	}

	// Try to find a WebTransport address first
	for _, maddr := range multiaddrs {
		if containsProtocol(maddr, "webtransport") {
			hostPort, err := extractHostPort(maddr, "udp")
			if err == nil {
				return hostPort, nil
			}
		}
	}

	// Try WebRTC next
	for _, maddr := range multiaddrs {
		if containsProtocol(maddr, "webrtc") {
			hostPort, err := extractHostPort(maddr, "udp")
			if err == nil {
				return hostPort, nil
			}
		}
	}

	// Try WebSocket/TCP last
	for _, maddr := range multiaddrs {
		if containsProtocol(maddr, "ws") || containsProtocol(maddr, "tcp") {
			hostPort, err := extractHostPort(maddr, "tcp")
			if err == nil {
				return hostPort, nil
			}
		}
	}

	// Default fallback: Try to extract host/port from the first multiaddr
	for _, maddr := range multiaddrs {
		for _, proto := range []string{"tcp", "udp"} {
			hostPort, err := extractHostPort(maddr, proto)
			if err == nil {
				return hostPort, nil
			}
		}
	}

	return "", fmt.Errorf("could not resolve any multiaddrs to a valid host:port")
}

// containsProtocol checks if a multiaddress contains a specific protocol
func containsProtocol(maddr ma.Multiaddr, proto string) bool {
	protocols := maddr.Protocols()
	for _, p := range protocols {
		if p.Name == proto {
			return true
		}
	}
	return false
}

// extractHostPort extracts the host and port from a multiaddress based on the protocol
func extractHostPort(maddr ma.Multiaddr, proto string) (string, error) {
	// Get IP address
	ip, err := maddr.ValueForProtocol(ma.P_IP4)
	if err != nil {
		// Try IPv6 if IPv4 not found
		ip, err = maddr.ValueForProtocol(ma.P_IP6)
		if err != nil {
			return "", fmt.Errorf("no IP address found in multiaddr: %v", maddr)
		}
	}

	// Get port based on protocol
	port, err := extractPort(maddr, proto)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%s:%s", ip, port), nil
}

// extractPort extracts the port for a specific protocol from a multiaddress
func extractPort(maddr ma.Multiaddr, proto string) (string, error) {
	// Check if the protocol exists in the multiaddr
	if !containsProtocol(maddr, proto) {
		return "", fmt.Errorf("protocol %s not found in multiaddr", proto)
	}

	// Get the protocol code
	protoCode := ma.ProtocolWithName(proto)
	if protoCode.Code == 0 {
		return "", fmt.Errorf("unknown protocol: %s", proto)
	}

	// Extract the port value for the protocol
	portStr, err := maddr.ValueForProtocol(protoCode.Code)
	if err != nil {
		return "", err
	}

	// Validate port
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return "", err
	}
	if port <= 0 || port > 65535 {
		return "", fmt.Errorf("invalid port number: %d", port)
	}

	return portStr, nil
}

// ResolveMultiaddrsWithContext resolves multiaddresses using a libp2p host for full connectivity
func ResolveMultiaddrsWithContext(ctx context.Context, addrs []ma.Multiaddr, h ...host.Host) (interface{}, error) {
	if len(h) == 0 {
		// For testing purposes or when no host is provided, just return the input addrs
		return addrs, nil
	}

	// When host is provided, use it to connect and return peer addr info
	var results []peer.AddrInfo
	for _, addr := range addrs {
		pinfo, err := peer.AddrInfoFromP2pAddr(addr)
		if err != nil {
			return nil, fmt.Errorf("invalid peer address %s: %w", addr, err)
		}

		if err := h[0].Connect(ctx, *pinfo); err != nil {
			return nil, fmt.Errorf("failed to connect to peer %s: %w", pinfo.ID, err)
		}

		results = append(results, *pinfo)
	}

	return results, nil
}

// ConvertToAddrInfoMap converts the map of peer.ID to multiaddresses to a map of peer.ID to peer.AddrInfo
func ConvertToAddrInfoMap(peerAddrs map[peer.ID][]ma.Multiaddr) map[peer.ID]peer.AddrInfo {
	result := make(map[peer.ID]peer.AddrInfo)

	for peerID, addrs := range peerAddrs {
		addrInfo := peer.AddrInfo{
			ID:    peerID,
			Addrs: addrs,
		}
		result[peerID] = addrInfo
	}

	return result
}
