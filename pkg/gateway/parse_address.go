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
			addrSegments := strings.SplitSeq(segment, ",")
			for addr := range addrSegments {
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
		addrSegments := strings.SplitSeq(addrPart, ",")
		for addr := range addrSegments {
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
		// Handle direct libp2p format (potentially with service path suffix)
		// Example: /p2p/Qm.../greeter.v1/SayHello
		// Iterate through components to find the end of the valid multiaddr

		var validAddr ma.Multiaddr
		var remainingPath string
		var err error

		currentAddrStr := ""
		components := strings.Split(strings.TrimPrefix(addrStr, "/"), "/") // Split into parts

		for i := 0; i < len(components); i += 2 { // Process protocol/value pairs
			if i+1 >= len(components) {
				// Odd number of components, likely the start of the service path
				remainingPath = "/" + strings.Join(components[i:], "/")
				break
			}
			protoName := components[i]
			protoValue := components[i+1]

			// Check if the protocol name is standard
			if !isMultiAddrProtocol(protoName) {
				// Found the start of the service path
				remainingPath = "/" + strings.Join(components[i:], "/")
				break // Stop processing components
			}

			// Append the current valid component pair
			currentAddrStr += "/" + protoName + "/" + protoValue

			// Special handling for /p2p/ protocol - it marks the end of the address part
			// if protoName == "p2p" {
			// 	// Assume anything after /p2p/<peerid> is the service path
			// 	if i+2 < len(components) {
			// 		remainingPath = "/" + strings.Join(components[i+2:], "/")
			// 	}
			// 	break // Stop processing components after /p2p/
			// }
			// Let's remove the special handling for now and rely on isMultiAddrProtocol check
		}

		// Attempt to parse the built address string
		if currentAddrStr == "" {
			return nil, "", fmt.Errorf("no valid multiaddress components found in %s", addrStr)
		}
		validAddr, err = ma.NewMultiaddr(currentAddrStr)
		if err != nil {
			// If parsing failed here, it means the loop included non-addr components.
			// This shouldn't happen if isMultiAddrProtocol is correct.
			// However, let's try parsing without the last component pair as a fallback.
			parts := strings.Split(currentAddrStr, "/")
			if len(parts) > 3 { // Need at least /proto/value/
				shorterAddrStr := strings.Join(parts[:len(parts)-2], "/")
				validAddr, err = ma.NewMultiaddr(shorterAddrStr)
				if err == nil {
					// Successfully parsed shorter address, assume last pair was service path start
					remainingPath = "/" + strings.Join(parts[len(parts)-2:], "/") + remainingPath
				} else {
					// Still failed, return original error
					return nil, "", fmt.Errorf("failed to parse extracted multiaddress '%s': %w", currentAddrStr, err)
				}
			} else {
				// Cannot shorten further, return original error
				return nil, "", fmt.Errorf("failed to parse extracted multiaddress '%s': %w", currentAddrStr, err)
			}
		}

		// Assign the identified service path
		servicePath = remainingPath

		// Extract Peer ID directly from the valid multiaddress part
		peerIDValue, err := validAddr.ValueForProtocol(ma.P_P2P)
		if err != nil {
			// This should ideally not happen if validAddr parsing succeeded and contained /p2p/
			return nil, "", fmt.Errorf("could not extract peer ID from address '%s': %w", validAddr.String(), err)
		}
		peerID, err := peer.Decode(peerIDValue)
		if err != nil {
			return nil, "", fmt.Errorf("could not decode peer ID '%s': %w", peerIDValue, err)
		}

		// Add the extracted valid multiaddress to the map, associated with the peer ID.
		// The connection logic needs the full address (including relay info) to connect correctly.
		peerAddrs[peerID] = append(peerAddrs[peerID], validAddr)
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
		"http", "https", "ws", "wss", "quic", "quic-v1", "webtransport", "webrtc", "p2p-circuit": // Added p2p-circuit
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

// ExtractRelayAddrInfo attempts to extract the relay peer's AddrInfo from a circuit address string.
// It expects an address like /p2p/relay-id/p2p-circuit/...
func ExtractRelayAddrInfo(addrStr string) (*peer.AddrInfo, error) {
	maddr, err := ma.NewMultiaddr(addrStr)
	if err != nil {
		return nil, fmt.Errorf("invalid multiaddress %s: %w", addrStr, err)
	}

	// Check if it's a circuit address
	_, err = maddr.ValueForProtocol(ma.P_CIRCUIT) // P_CIRCUIT is the code for /p2p-circuit
	if err != nil {
		// Not a circuit address or malformed
		return nil, fmt.Errorf("not a circuit address or missing circuit component: %w", err)
	}

	// Extract the relay part (everything before /p2p-circuit)
	relayMaStr, _ := ma.SplitFunc(maddr, func(c ma.Component) bool {
		return c.Protocol().Code == ma.P_CIRCUIT
	})

	if relayMaStr == nil {
		return nil, fmt.Errorf("could not extract relay part from circuit address %s", addrStr)
	}

	// Convert the relay part to AddrInfo
	relayInfo, err := peer.AddrInfoFromP2pAddr(relayMaStr)
	if err != nil {
		return nil, fmt.Errorf("could not get AddrInfo from relay part %s: %w", relayMaStr.String(), err)
	}
	if relayInfo == nil || relayInfo.ID == "" {
		return nil, fmt.Errorf("extracted relay AddrInfo is invalid for %s", relayMaStr.String())
	}

	return relayInfo, nil
}

// IsRelayAddr checks if the given address string contains the p2p-circuit indicator.
func IsRelayAddr(addr string) bool {
	// Basic check for the presence of "/p2p-circuit"
	// Note: A more robust check might involve full multiaddr parsing,
	// but this is often sufficient and faster for quick checks.
	return strings.Contains(addr, "/p2p-circuit")
}
