package gateway

import (
	"fmt"
	"strconv"

	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
)

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
