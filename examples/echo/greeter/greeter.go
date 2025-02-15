package greeter

import (
	"context"
	"fmt"
	"time"

	"connectrpc.com/connect"
	gv1 "github.com/omgolab/drpc/examples/echo/gen/go/greeter/v1"
	gv1connect "github.com/omgolab/drpc/examples/echo/gen/go/greeter/v1/greeterv1connect"
)

// Server implements the GreeterService.
type Server struct {
	gv1connect.UnimplementedGreeterServiceHandler
}

// SayHello implements the SayHello method.
func (s *Server) SayHello(
	ctx context.Context,
	req *connect.Request[gv1.SayHelloRequest],
) (*connect.Response[gv1.SayHelloResponse], error) {
	msg := fmt.Sprintf("Hello, %s!", req.Msg.Name)
	res := connect.NewResponse(&gv1.SayHelloResponse{
		Message: msg,
	})
	return res, nil
}

// StreamingEcho implements the StreamingEcho method.
func (s *Server) StreamingEcho(
	ctx context.Context,
	req *connect.Request[gv1.StreamingEchoRequest],
	stream *connect.ServerStream[gv1.StreamingEchoResponse],
) error {
	msg := fmt.Sprintf("Echo: %s", req.Msg.Message)
	err := stream.Send(&gv1.StreamingEchoResponse{
		Message: msg,
	})
	if err != nil {
		return fmt.Errorf("send error: %w", err)
	}
	time.Sleep(time.Millisecond * 100)
	return nil
}
