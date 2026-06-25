package proto

import (
	"context"

	"google.golang.org/grpc"
)

// BootstrapServiceClient is the client API for BootstrapService.
// Used during client setup to fetch TLS certificates from the server.
type BootstrapServiceClient interface {
	FetchClientCerts(ctx context.Context, in *BootstrapRequest, opts ...grpc.CallOption) (*BootstrapResponse, error)
}

type bootstrapServiceClient struct {
	cc grpc.ClientConnInterface
}

// NewBootstrapServiceClient creates a new BootstrapService client.
func NewBootstrapServiceClient(cc grpc.ClientConnInterface) BootstrapServiceClient {
	return &bootstrapServiceClient{cc}
}

const (
	BootstrapService_FetchClientCerts_FullMethodName = "/tergum.v3.BootstrapService/FetchClientCerts"
)

func (c *bootstrapServiceClient) FetchClientCerts(ctx context.Context, in *BootstrapRequest, opts ...grpc.CallOption) (*BootstrapResponse, error) {
	out := new(BootstrapResponse)
	err := c.cc.Invoke(ctx, BootstrapService_FetchClientCerts_FullMethodName, in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// BootstrapServiceServer is the server API for BootstrapService.
type BootstrapServiceServer interface {
	FetchClientCerts(context.Context, *BootstrapRequest) (*BootstrapResponse, error)
	mustEmbedUnimplementedBootstrapServiceServer()
}

// UnimplementedBootstrapServiceServer should be embedded to have forward compatible implementations.
type UnimplementedBootstrapServiceServer struct{}

func (UnimplementedBootstrapServiceServer) FetchClientCerts(context.Context, *BootstrapRequest) (*BootstrapResponse, error) {
	return nil, grpc.Errorf(12, "method FetchClientCerts not implemented") //nolint:staticcheck
}
func (UnimplementedBootstrapServiceServer) mustEmbedUnimplementedBootstrapServiceServer() {}

// UnsafeBootstrapServiceServer may be embedded to opt out of forward compatibility for this service.
type UnsafeBootstrapServiceServer interface {
	mustEmbedUnimplementedBootstrapServiceServer()
}

// RegisterBootstrapServiceServer registers the BootstrapService server implementation.
func RegisterBootstrapServiceServer(s grpc.ServiceRegistrar, srv BootstrapServiceServer) {
	s.RegisterService(&BootstrapService_ServiceDesc, srv)
}

// BootstrapService_ServiceDesc is the grpc.ServiceDesc for BootstrapService.
var BootstrapService_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "tergum.v3.BootstrapService",
	HandlerType: (*BootstrapServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "FetchClientCerts",
			Handler:    _BootstrapService_FetchClientCerts_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "proto/v3/bootstrap.proto",
}

func _BootstrapService_FetchClientCerts_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(BootstrapRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(BootstrapServiceServer).FetchClientCerts(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: BootstrapService_FetchClientCerts_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(BootstrapServiceServer).FetchClientCerts(ctx, req.(*BootstrapRequest))
	}
	return interceptor(ctx, in, info, handler)
}
