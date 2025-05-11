package gateway

import (
	"fmt"
	"strings"

	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
)

// ParseGatewayP2PAddresses parses addresses in the following formats:
// HTTP gateway format (concise): /@{addr1,addr2,addr3...}/@{/service/method} (first and last /@ as delimiters)
// Returns:
// - a map of peer.ID to []ma.Multiaddr (grouped by peer ID)
// - service path if in HTTP gateway format (empty for direct format)
// - error if parsing failed
func ParseGatewayP2PAddresses(addrStr string) (map[peer.ID][]ma.Multiaddr, string, error) {
	parts := strings.Split(addrStr, GatewayPrefix)
	if len(parts) != 3 {
		return nil, "", fmt.Errorf("invalid concise format: expected format /@addresses/@/service/method")
	}

	// Process addresses (parts[1])
	addrPart := parts[1]
	if addrPart == "" {
		return nil, "", fmt.Errorf("no addresses provided in concise format")
	}

	// Parse service path (parts[2])
	var servicePath string
	if !strings.HasPrefix(parts[2], "/") {
		return nil, "", fmt.Errorf("service path must start with / in concise format")
	}
	servicePath = parts[2]

	// Parse addresses
	peerAddrs, err := ParseCommaSeparatedMultiAddresses(addrPart)
	if err != nil {
		return nil, "", err
	}

	return peerAddrs, servicePath, nil
}

func ParseCommaSeparatedMultiAddresses(addrStr string) (map[peer.ID][]ma.Multiaddr, error) {
	peerAddrs := make(map[peer.ID][]ma.Multiaddr)
	// Process comma-separated addresses
	addrSegments := strings.SplitSeq(addrStr, ",")
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
			return nil, err
		}
	}

	if len(peerAddrs) == 0 {
		return nil, fmt.Errorf("no valid addresses found")
	}
	return peerAddrs, nil
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

	// some private addresses may only have the peer ID and no addresses
	if len(pinfo.Addrs) >= 0 {
		peerMap[pinfo.ID] = append(peerMap[pinfo.ID], pinfo.Addrs...)
	}

	return nil
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
