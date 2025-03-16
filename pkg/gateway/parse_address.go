package gateway

import (
	"errors"
	"fmt"
	"strings"

	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
)

// ParseAddresses parses addresses in the following formats:
// 1. HTTP gateway format (multiple /@/): /@/addr1/@/addr2/.../@/service.v1.ServiceName/MethodName
// 2. HTTP gateway format (concise): /@addr1,addr2,addr3.../@/service/method (first and last /@ as delimiters)
// 3. Direct libp2p format: addr1,addr2,addr3
//
// Returns:
// - a map of peer.ID to []ma.Multiaddr (grouped by peer ID)
// - service path if in HTTP gateway format (empty for direct format)
// - error if parsing failed
func ParseAddresses(addrStr string) (map[peer.ID][]ma.Multiaddr, string, error) {
	peerAddrs := make(map[peer.ID][]ma.Multiaddr)
	var servicePath string

	// Check if we're dealing with HTTP gateway format (contains "/@")
	if strings.Contains(addrStr, "/@/") {
		// Handle standard format with multiple /@/ delimiters
		segments := strings.Split(addrStr, "/@/")

		// First segment is empty because addrStr starts with /@/
		segments = segments[1:]

		// Last segment might contain the service path
		lastIdx := len(segments) - 1
		lastSegment := segments[lastIdx]

		// Check if the last segment contains a service path
		if strings.Count(lastSegment, "/") > 0 && !strings.HasPrefix(lastSegment, "/") {
			// Try to find where the multiaddr ends and service path begins
			for i, char := range lastSegment {
				if char == '/' && i > 0 && !isMultiAddrProtocol(lastSegment[0:i]) {
					// Found the service path
					servicePath = lastSegment[i:]
					segments[lastIdx] = lastSegment[0:i]
					break
				}
			}
		}

		// Process all address segments
		for _, segment := range segments {
			if segment == "" {
				continue
			}

			// Support comma-separated addresses within each segment too
			addrSegments := strings.Split(segment, ",")
			for _, addr := range addrSegments {
				addr = strings.TrimSpace(addr)
				if addr == "" {
					continue
				}

				err := parseAndAddToMap(addr, peerAddrs)
				if err != nil {
					return nil, "", err
				}
			}
		}
	} else if strings.Contains(addrStr, "/@") {
		// Handle concise format with first and last /@ as delimiters
		// Format: /@addr1,addr2,addr3.../@/service/method

		parts := strings.Split(addrStr, "/@")
		if len(parts) != 3 {
			return nil, "", fmt.Errorf("invalid concise format: expected format /@addresses/@/service/method")
		}

		// Process addresses (parts[1])
		addrPart := parts[1]
		if addrPart == "" {
			return nil, "", fmt.Errorf("no addresses provided in concise format")
		}

		// Parse service path (parts[2])
		if !strings.HasPrefix(parts[2], "/") {
			return nil, "", fmt.Errorf("service path must start with / in concise format")
		}
		servicePath = parts[2]

		// Process comma-separated addresses
		addrSegments := strings.Split(addrPart, ",")
		for _, addr := range addrSegments {
			addr = strings.TrimSpace(addr)
			if addr == "" {
				continue
			}

			// Add '/' prefix if missing
			if !strings.HasPrefix(addr, "/") {
				addr = "/" + addr
			}

			err := parseAndAddToMap(addr, peerAddrs)
			if err != nil {
				return nil, "", err
			}
		}
	} else {
		// Handle direct libp2p format (comma separated)
		addrSegments := strings.Split(addrStr, ",")
		for _, addr := range addrSegments {
			addr = strings.TrimSpace(addr)
			if addr == "" {
				continue
			}

			err := parseAndAddToMap(addr, peerAddrs)
			if err != nil {
				return nil, "", err
			}
		}
	}

	if len(peerAddrs) == 0 {
		return nil, "", fmt.Errorf("no valid addresses found")
	}

	return peerAddrs, servicePath, nil
}

// Helper function to parse a single address and add it to the peer map
func parseAndAddToMap(addrStr string, peerMap map[peer.ID][]ma.Multiaddr) error {
	maddr, err := ma.NewMultiaddr(addrStr)
	if err != nil {
		return fmt.Errorf("invalid multiaddress %s: %v", addrStr, err)
	}

	pinfo, err := peer.AddrInfoFromP2pAddr(maddr)
	if err != nil {
		return fmt.Errorf("cannot extract peer info from %s: %v", addrStr, err)
	}

	if len(pinfo.Addrs) > 0 {
		peerMap[pinfo.ID] = append(peerMap[pinfo.ID], pinfo.Addrs...)
	}

	return nil
}

// isMultiAddrProtocol checks if the string is a valid multiaddr protocol
func isMultiAddrProtocol(proto string) bool {
	switch proto {
	case "ip4", "ip6", "dns", "dns4", "dns6", "tcp", "udp", "p2p", "ipfs",
		"http", "https", "ws", "wss", "quic", "quic-v1", "webtransport", "webrtc":
		return true
	default:
		return false
	}
}

// parseGatewayPath parses a gateway path and extracts peer ID and service path
// This is the consolidated version that handles both formats
func parseGatewayPath(path string, logger ...interface{}) ([][]ma.Multiaddr, string, error) {
	if !strings.HasPrefix(path, "/@/") {
		return nil, "", fmt.Errorf("invalid gateway path: must start with /@/")
	}

	parts := strings.Split(strings.TrimPrefix(path, "/@/"), "/@/")
	if len(parts) < 2 {
		return nil, "", fmt.Errorf("invalid gateway path: must contain at least one multiaddr and service path")
	}

	servicePath := parts[len(parts)-1]
	addrParts := parts[:len(parts)-1]

	var peerAddrs [][]ma.Multiaddr
	for _, addrGroup := range addrParts {
		addrs, err := parseMultiaddrs(addrGroup)
		if err != nil {
			return nil, "", fmt.Errorf("invalid multiaddr: %w", err)
		}
		peerAddrs = append(peerAddrs, addrs)
	}

	return peerAddrs, servicePath, nil
}

// extractPeerID extracts a peer ID from a multiaddress
// This is the consolidated version that handles both types of inputs
func extractPeerID(input interface{}) (peer.ID, error) {
	switch v := input.(type) {
	case string:
		// When input is a string (peer ID directly)
		return peer.Decode(v)
	case ma.Multiaddr:
		// When input is a multiaddress
		value, err := v.ValueForProtocol(ma.P_P2P)
		if err != nil {
			return "", fmt.Errorf("peer id not found in multiaddr: %w", err)
		}
		peerID, err := peer.Decode(value)
		if err != nil {
			return "", fmt.Errorf("invalid peer id: %w", err)
		}
		return peerID, nil
	default:
		return "", errors.New("unsupported type for extracting peer ID")
	}
}

// Helper function to parse multiple multiaddrs from a string
func parseMultiaddrs(addrStr string) ([]ma.Multiaddr, error) {
	var addrs []ma.Multiaddr
	for _, addr := range strings.Split(addrStr, ",") {
		maddr, err := ma.NewMultiaddr(strings.TrimSpace(addr))
		if err != nil {
			return nil, err
		}
		addrs = append(addrs, maddr)
	}
	return addrs, nil
}
