package greeter

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	greeterv1 "github.com/omgolab/drpc/examples/echo/gen/go/greeter/v1"
	greeterv1connect "github.com/omgolab/drpc/examples/echo/gen/go/greeter/v1/greeterv1connect"
)

// Server implements the GreeterService.
type Server struct {
	greeterv1connect.UnimplementedGreeterServiceHandler
}

// SayHello implements the SayHello method.
func (s *Server) SayHello(
	ctx context.Context,
	req *connect.Request[greeterv1.SayHelloRequest],
) (*connect.Response[greeterv1.SayHelloResponse], error) {
	msg := fmt.Sprintf("Hello, %s!", req.Msg.Name)
	res := connect.NewResponse(&greeterv1.SayHelloResponse{
		Message: msg,
	})
	return res, nil
}
