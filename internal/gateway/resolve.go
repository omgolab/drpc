package gateway

// resolveMultiaddrs resolves multiple multiaddrs for the same peer
func resolveMultiaddrs(peerIDStr string, addrs []string) (string, error) {
	// For now, just return localhost:8080 for testing
	return "localhost:8080", nil
}