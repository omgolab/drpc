package greeter

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	pb "github.com/omgolab/drpc/examples/echo/gen/go/greeter/v1"
)

func TestSayHello(t *testing.T) {
	server := &Server{}
	req := connect.NewRequest(&pb.SayHelloRequest{Name: "Test"})
	resp, err := server.SayHello(context.Background(), req)
	if err != nil {
		t.Fatalf("SayHello failed: %v", err)
	}
	if resp.Msg.Message != "Hello, Test!" {
		t.Errorf("Unexpected message: %s", resp.Msg.Message)
	}
}

func TestStreamingEcho(t *testing.T) {
	// TODO: Implement test cases for StreamingEcho
}
