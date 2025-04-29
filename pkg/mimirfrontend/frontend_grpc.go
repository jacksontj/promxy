package mimirfrontend

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// FrontendServer is the server API for Frontend service.
// All implementations must embed UnimplementedFrontendServer
// for forward compatibility.
type FrontendServer interface {
	// Query processes a query result.
	Query(context.Context, *QueryResultRequest) (*QueryResultResponse, error)
}

// RegisterFrontendServer registers the FrontendServer implementation with the gRPC server.
func RegisterFrontendServer(s *grpc.Server, srv FrontendServer) {
	s.RegisterService(&_Frontend_serviceDesc, srv)
}

// UnimplementedFrontendServer must be embedded to have forward compatible implementations.
type UnimplementedFrontendServer struct {
}

// Query implements FrontendServer
func (*UnimplementedFrontendServer) Query(context.Context, *QueryResultRequest) (*QueryResultResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Query not implemented")
}

// _Frontend_serviceDesc is the gRPC service descriptor for Frontend service.
// The gRPC package only imports this name for backward compatibility.
var _Frontend_serviceDesc = grpc.ServiceDesc{
	ServiceName: "mimirfrontend.Frontend",
	HandlerType: (*FrontendServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "Query",
			Handler:    _Frontend_Query_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "frontend.proto",
}

func _Frontend_Query_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(QueryResultRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(FrontendServer).Query(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/mimirfrontend.Frontend/Query",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(FrontendServer).Query(ctx, req.(*QueryResultRequest))
	}
	return interceptor(ctx, in, info, handler)
}