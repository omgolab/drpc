package routes

import "testing"

func TestResolveMultiaddrs(t *testing.T) {
	// This function currently just returns a fixed value, so just check that it doesn't error
	_, err := resolveMultiaddrs("QmPeerID", []string{"/ip4/127.0.0.1/tcp/9000/p2p/QmPeerID"})
	if err != nil {
		t.Errorf("resolveMultiaddrs returned an error: %v", err)
	}
}
