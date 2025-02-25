package routes

import (
	"strings"
	"testing"

	glog "github.com/omgolab/go-commons/pkg/log"
)

func TestParseGatewayPath(t *testing.T) {
	log, _ := glog.New(glog.WithFileLogger("test.log"))

	// Test case 1: Valid path
	path := "/@/ip4/127.0.0.1/tcp/9000/p2p/QmPeerID/@/service/path"
	addrs, servicePath, err := parseGatewayPath(path, log)
	if err != nil {
		t.Fatalf("parseGatewayPath failed: %v", err)
	}
	if len(addrs) != 1 || !strings.Contains(addrs[0], "QmPeerID") {
		t.Errorf("Unexpected addresses: %v", addrs)
	}
	if servicePath != "greeter.v1.GreeterService/path" {
		t.Errorf("Unexpected service path: %v", servicePath)
	}

	// Test case 2: No /p2p/
	path = "/@/ip4/127.0.0.1/tcp/9000/@/service/path"
	_, _, err = parseGatewayPath(path, log)
	if err == nil {
		t.Errorf("Expected error, got nil")
	}

	// Test case 3: No peer ID
	path = "/@/ip4/127.0.0.1/tcp/9000/p2p//service/path"
	_, _, err = parseGatewayPath(path, log)
	if err == nil {
		t.Errorf("Expected error, got nil")
	}
}

func TestExtractPeerID(t *testing.T) {
	// Test case 1: Valid address
	addr := "/ip4/127.0.0.1/tcp/9000/p2p/QmPeerID"
	peerID := extractPeerID(addr)
	if peerID != "QmPeerID" {
		t.Errorf("Unexpected peer ID: %v", peerID)
	}

	// Test case 2: No /p2p/
	addr = "/ip4/127.0.0.1/tcp/9000"
	peerID = extractPeerID(addr)
	if peerID != "" {
		t.Errorf("Unexpected peer ID: %v", peerID)
	}
}
