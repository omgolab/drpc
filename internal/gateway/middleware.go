package gateway

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
)

// GetGatewayHandler wraps a Connect RPC handler to support both direct and p2p gateway requests
func GetGatewayHandler(muxHandler http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		isGateway, addrs, servicePath := parseGatewayPath(r.URL.Path)
		if !isGateway {
			muxHandler.ServeHTTP(w, r)
			return
		}

		if len(addrs) == 0 {
			http.Error(w, "no valid addresses provided", http.StatusBadRequest)
			return
		}

		// First address must contain peer ID
		peerIDStr := extractPeerID(addrs[0])
		if peerIDStr == "" {
			http.Error(w, "first address must contain peer ID", http.StatusBadRequest)
			return
		}

		targetAddr, err := resolveMultiaddrs(peerIDStr, addrs)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if err := forwardRequest(w, r, targetAddr, servicePath); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
}

// parseGatewayPath parses URLs in the format:
// /@/ip4/127.0.0.1/tcp/9000/p2p/QmPeerID/@/ip4/[::1]/tcp/9000/@/service/path
func parseGatewayPath(path string) (isGateway bool, addrs []string, servicePath string) {
	if !strings.HasPrefix(path, "/@") {
		return false, nil, ""
	}

	// Split by /@ to get [first_addr_with_peerid, addr2, addr3, servicePath]
	parts := strings.Split(path[2:], "/@")
	if len(parts) < 2 {
		return true, nil, ""
	}

	// First address must contain peer ID
	if !strings.Contains(parts[0], "/p2p/") {
		return true, nil, ""
	}

	addrs = parts[:len(parts)-1]
	servicePath = "/" + parts[len(parts)-1]

	return true, addrs, servicePath
}

// extractPeerID extracts peer ID from a multiaddr string
func extractPeerID(addr string) string {
	parts := strings.Split(addr, "/p2p/")
	if len(parts) != 2 {
		return ""
	}
	return parts[1]
}

// resolveMultiaddrs resolves multiple multiaddrs for the same peer
func resolveMultiaddrs(peerIDStr string, addrs []string) (string, error) {
	// Validate peer ID format
	_, err := peer.Decode(peerIDStr)
	if err != nil {
		return "", fmt.Errorf("invalid peer ID: %w", err)
	}

	// Convert addresses to multiaddrs
	var maddrs []multiaddr.Multiaddr
	for i, addr := range addrs {
		var fullAddr string
		if i == 0 {
			// First address already has peer ID
			fullAddr = addr
		} else {
			// Append peer ID to other addresses
			fullAddr = addr + "/p2p/" + peerIDStr
		}

		ma, err := multiaddr.NewMultiaddr(fullAddr)
		if err != nil {
			continue // Skip invalid addresses
		}
		maddrs = append(maddrs, ma)
	}

	if len(maddrs) == 0 {
		return "", fmt.Errorf("no valid addresses")
	}

	// For now, use the first valid address
	// TODO: Implement more sophisticated address selection
	return maddrs[0].String(), nil
}

// forwardRequest forwards the HTTP request to the target service
func forwardRequest(w http.ResponseWriter, r *http.Request, targetAddr, servicePath string) error {
	newReq, err := http.NewRequest(r.Method, "http://"+targetAddr+servicePath, r.Body)
	if err != nil {
		return fmt.Errorf("failed to create new request: %w", err)
	}
	newReq.Header = r.Header

	httpClient := &http.Client{}
	resp, err := httpClient.Do(newReq)
	if err != nil {
		return fmt.Errorf("failed to forward request: %w", err)
	}
	defer resp.Body.Close()

	// Copy response headers
	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}

	w.WriteHeader(resp.StatusCode)
	if _, err := io.Copy(w, resp.Body); err != nil {
		return fmt.Errorf("error copying response body: %w", err)
	}

	return nil
}
