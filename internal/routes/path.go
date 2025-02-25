package routes

import (
	"fmt"
	"strings"

	glog "github.com/omgolab/go-commons/pkg/log"
)

// parseGatewayPath parses URLs in the format:
// /@/ip4/127.0.0.1/tcp/9000/p2p/QmPeerID/@/service/path
func parseGatewayPath(path string, logger glog.Logger) ([]string, string, error) {
	logger.Printf("parseGatewayPath - Input path: %s", path)

	logger.Printf("parseGatewayPath - Input path: %s", path)

	// Find the index of the last occurrence of /p2p/
	p2pIndex := strings.LastIndex(path, "/p2p/")
	if p2pIndex == -1 {
		return nil, "", fmt.Errorf("invalid gateway path: no /p2p/ found")
	}

	// Extract the multiaddr part up to (but not including) /p2p/<peerID>
	multiaddrPart := path[:p2pIndex]
	peerID := extractPeerID(path)
	if peerID == "" {
		return nil, "", fmt.Errorf("invalid gateway path: could not extract peer ID")
	}

	// Find the index of the next slash after the peer ID
	fullMultiaddr := multiaddrPart + "/p2p/" + peerID

	// Extract the service path (everything after the peer ID and the following slash)
	servicePath := strings.TrimPrefix(path[p2pIndex+len("/p2p/")+len(peerID):], "/")

	logger.Printf("parseGatewayPath - Raw ServicePath: %s", servicePath)

	// Split service path into base path and method
	parts := strings.Split(servicePath, "/")
	if len(parts) < 2 {
		return nil, "", fmt.Errorf("invalid service path: %s", servicePath)
	}

	// Reconstruct service path in the format expected by Connect-RPC
	if strings.Contains(parts[0], ".") {
		// Already in the correct format
		servicePath = parts[0] + "/" + parts[len(parts)-1]
	} else {
		// Convert to the correct format
		servicePath = "greeter.v1.GreeterService/" + parts[len(parts)-1]
	}

	logger.Printf("parseGatewayPath - Multiaddr: %s", fullMultiaddr)
	logger.Printf("parseGatewayPath - ServicePath: %s", servicePath)

	addrs := []string{}
	if fullMultiaddr != "" {
		addrs = append(addrs, fullMultiaddr)
	}
	logger.Printf("parseGatewayPath - Addresses: %v", addrs)

	if len(addrs) == 0 {
		return nil, "", fmt.Errorf("invalid gateway path: no multiaddr found")
	}

	return addrs, servicePath, nil
}

// extractPeerID extracts peer ID from a multiaddr string
func extractPeerID(addr string) string {
	// Find the /p2p/ component
	p2pIndex := strings.LastIndex(addr, "/p2p/")
	if p2pIndex == -1 {
		return ""
	}

	// Extract peer ID (everything after /p2p/)
	peerID := addr[p2pIndex+len("/p2p/"):]

	// If there's a slash after the peer ID, trim it
	if slashIndex := strings.Index(peerID, "/"); slashIndex != -1 {
		peerID = peerID[:slashIndex]
	}

	return peerID
}
