package routes

import "fmt"

// resolveMultiaddrs resolves multiple multiaddrs for the same peer
func resolveMultiaddrs(peerIDStr string, addrs []string) (string, error) {
	// TODO: Properly resolve multiaddrs based on priority (WebTransport, WebRTC, TCP/WS, UDP).
	// For now, return the HTTP address with the default port.
	return fmt.Sprintf("localhost:%d", 9090), nil
}
