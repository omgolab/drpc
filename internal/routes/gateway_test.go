package routes

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	pb "github.com/omgolab/drpc/examples/echo/gen/go/greeter/v1"
	glog "github.com/omgolab/go-commons/pkg/log"
	"google.golang.org/protobuf/proto"
)

func TestGetGatewayHandler(t *testing.T) {
	// Create a mock logger
	log, _ := glog.New(glog.WithFileLogger("test.log"))

	// Create a gateway handler
	gatewayHandler := getGatewayHandler(log)

	// Test case 1: Not a gateway path (no /@)
	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	gatewayHandler(w, req)
	if w.Code != http.StatusBadRequest { // Expect BadRequest
		t.Errorf("Expected status %v, got %v", http.StatusBadRequest, w.Code)
	}
	if !strings.Contains(w.Body.String(), "invalid gateway path: no /p2p/ found") {
		t.Errorf("Expected body to contain 'invalid gateway path: no /p2p/ found', got %v", w.Body.String())
	}

	// Test case 2: Not a gateway path (no /p2p/)
	req = httptest.NewRequest("GET", "/@/test", nil)
	w = httptest.NewRecorder()
	gatewayHandler(w, req)
	if w.Code != http.StatusBadRequest { // Expect BadRequest
		t.Errorf("Expected status %v, got %v", http.StatusBadRequest, w.Code)
	}
    if !strings.Contains(w.Body.String(), "invalid gateway path: no /p2p/ found") {
		t.Errorf("Expected body to contain 'invalid gateway path: no /p2p/ found', got %v", w.Body.String())
	}

	// Test case 3: Invalid gateway path (no peer ID)
	req = httptest.NewRequest("GET", "/@/ip4/127.0.0.1/tcp/9000/p2p/", nil)
	w = httptest.NewRecorder()
	gatewayHandler(w, req)
	if w.Code != http.StatusBadRequest { // Expect BadRequest
		t.Errorf("Expected status BadRequest, got %v", w.Code)
	}

	// Test case 4: Valid gateway path (This test is skipped for now)
	emptyReq := &pb.SayHelloRequest{}
	reqBytes, _ := proto.Marshal(emptyReq)
	req, _ = http.NewRequest("POST", "/@/ip4/127.0.0.1/tcp/9000/p2p/QmPeerID/@/greeter.v1.GreeterService/SayHello", strings.NewReader(string(reqBytes)))
	req.Header.Set("Content-Type", "application/connect+proto")
	req.Header.Set("Accept", "application/connect+proto")
	req.ContentLength = int64(len(reqBytes))
	w = httptest.NewRecorder()
	gatewayHandler(w, req)
	// if w.Code != http.StatusOK {
	//  t.Errorf("Expected status OK, got %v", w.Code)
	// }
	t.Skip("TestGetGatewayHandler not implemented")
}
