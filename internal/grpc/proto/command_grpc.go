package proto

import (
	"context"

	"google.golang.org/grpc"
)

// CommandServiceClient is the client API for CommandService.
type CommandServiceClient interface {
	TriggerBackup(ctx context.Context, in *BackupRequest, opts ...grpc.CallOption) (*BackupResponse, error)
	StopBackup(ctx context.Context, in *StopRequest, opts ...grpc.CallOption) (*StopResponse, error)
	GetStatus(ctx context.Context, in *StatusRequest, opts ...grpc.CallOption) (*StatusResponse, error)
	Ping(ctx context.Context, in *PingRequest, opts ...grpc.CallOption) (*PingResponse, error)
	ListBackups(ctx context.Context, in *ListBackupsRequest, opts ...grpc.CallOption) (*ListBackupsResponse, error)
	DeleteFromBackup(ctx context.Context, in *DeleteRequest, opts ...grpc.CallOption) (*DeleteResponse, error)
	GetRetention(ctx context.Context, in *RetentionRequest, opts ...grpc.CallOption) (*RetentionResponse, error)
	StartWatcher(ctx context.Context, in *WatcherRequest, opts ...grpc.CallOption) (*WatcherResponse, error)
	StopWatcher(ctx context.Context, in *WatcherRequest, opts ...grpc.CallOption) (*WatcherResponse, error)
	RegisterClient(ctx context.Context, in *RegisterRequest, opts ...grpc.CallOption) (*RegisterResponse, error)
	PushRestore(ctx context.Context, opts ...grpc.CallOption) (CommandService_PushRestoreClient, error)
	CommandTunnel(ctx context.Context, opts ...grpc.CallOption) (CommandService_CommandTunnelClient, error)
}

type commandServiceClient struct {
	cc grpc.ClientConnInterface
}

// NewCommandServiceClient creates a new CommandService client.
func NewCommandServiceClient(cc grpc.ClientConnInterface) CommandServiceClient {
	return &commandServiceClient{cc}
}

const (
	CommandService_TriggerBackup_FullMethodName    = "/tergum.v3.CommandService/TriggerBackup"
	CommandService_StopBackup_FullMethodName       = "/tergum.v3.CommandService/StopBackup"
	CommandService_GetStatus_FullMethodName        = "/tergum.v3.CommandService/GetStatus"
	CommandService_Ping_FullMethodName             = "/tergum.v3.CommandService/Ping"
	CommandService_ListBackups_FullMethodName      = "/tergum.v3.CommandService/ListBackups"
	CommandService_DeleteFromBackup_FullMethodName = "/tergum.v3.CommandService/DeleteFromBackup"
	CommandService_GetRetention_FullMethodName     = "/tergum.v3.CommandService/GetRetention"
	CommandService_StartWatcher_FullMethodName     = "/tergum.v3.CommandService/StartWatcher"
	CommandService_StopWatcher_FullMethodName      = "/tergum.v3.CommandService/StopWatcher"
	CommandService_RegisterClient_FullMethodName   = "/tergum.v3.CommandService/RegisterClient"
	CommandService_PushRestore_FullMethodName      = "/tergum.v3.CommandService/PushRestore"
	CommandService_CommandTunnel_FullMethodName    = "/tergum.v3.CommandService/CommandTunnel"
)

func (c *commandServiceClient) TriggerBackup(ctx context.Context, in *BackupRequest, opts ...grpc.CallOption) (*BackupResponse, error) {
	out := new(BackupResponse)
	err := c.cc.Invoke(ctx, CommandService_TriggerBackup_FullMethodName, in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *commandServiceClient) StopBackup(ctx context.Context, in *StopRequest, opts ...grpc.CallOption) (*StopResponse, error) {
	out := new(StopResponse)
	err := c.cc.Invoke(ctx, CommandService_StopBackup_FullMethodName, in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *commandServiceClient) GetStatus(ctx context.Context, in *StatusRequest, opts ...grpc.CallOption) (*StatusResponse, error) {
	out := new(StatusResponse)
	err := c.cc.Invoke(ctx, CommandService_GetStatus_FullMethodName, in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *commandServiceClient) Ping(ctx context.Context, in *PingRequest, opts ...grpc.CallOption) (*PingResponse, error) {
	out := new(PingResponse)
	err := c.cc.Invoke(ctx, CommandService_Ping_FullMethodName, in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *commandServiceClient) ListBackups(ctx context.Context, in *ListBackupsRequest, opts ...grpc.CallOption) (*ListBackupsResponse, error) {
	out := new(ListBackupsResponse)
	err := c.cc.Invoke(ctx, CommandService_ListBackups_FullMethodName, in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *commandServiceClient) DeleteFromBackup(ctx context.Context, in *DeleteRequest, opts ...grpc.CallOption) (*DeleteResponse, error) {
	out := new(DeleteResponse)
	err := c.cc.Invoke(ctx, CommandService_DeleteFromBackup_FullMethodName, in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *commandServiceClient) GetRetention(ctx context.Context, in *RetentionRequest, opts ...grpc.CallOption) (*RetentionResponse, error) {
	out := new(RetentionResponse)
	err := c.cc.Invoke(ctx, CommandService_GetRetention_FullMethodName, in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *commandServiceClient) StartWatcher(ctx context.Context, in *WatcherRequest, opts ...grpc.CallOption) (*WatcherResponse, error) {
	out := new(WatcherResponse)
	err := c.cc.Invoke(ctx, CommandService_StartWatcher_FullMethodName, in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *commandServiceClient) StopWatcher(ctx context.Context, in *WatcherRequest, opts ...grpc.CallOption) (*WatcherResponse, error) {
	out := new(WatcherResponse)
	err := c.cc.Invoke(ctx, CommandService_StopWatcher_FullMethodName, in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *commandServiceClient) RegisterClient(ctx context.Context, in *RegisterRequest, opts ...grpc.CallOption) (*RegisterResponse, error) {
	out := new(RegisterResponse)
	err := c.cc.Invoke(ctx, CommandService_RegisterClient_FullMethodName, in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// CommandService_PushRestoreClient is the client streaming interface for PushRestore.
type CommandService_PushRestoreClient interface {
	Send(*FileChunk) error
	CloseAndRecv() (*PushRestoreResponse, error)
	grpc.ClientStream
}

type commandServicePushRestoreClient struct {
	grpc.ClientStream
}

func (x *commandServicePushRestoreClient) Send(m *FileChunk) error {
	return x.ClientStream.SendMsg(m)
}

func (x *commandServicePushRestoreClient) CloseAndRecv() (*PushRestoreResponse, error) {
	if err := x.ClientStream.CloseSend(); err != nil {
		return nil, err
	}
	m := new(PushRestoreResponse)
	if err := x.ClientStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

func (c *commandServiceClient) PushRestore(ctx context.Context, opts ...grpc.CallOption) (CommandService_PushRestoreClient, error) {
	stream, err := c.cc.NewStream(ctx, &CommandService_ServiceDesc.Streams[0], CommandService_PushRestore_FullMethodName, opts...)
	if err != nil {
		return nil, err
	}
	return &commandServicePushRestoreClient{stream}, nil
}

// CommandService_CommandTunnelClient is the bidirectional streaming interface for CommandTunnel.
type CommandService_CommandTunnelClient interface {
	Send(*TunnelResponse) error
	Recv() (*TunnelCommand, error)
	grpc.ClientStream
}

type commandServiceCommandTunnelClient struct {
	grpc.ClientStream
}

func (x *commandServiceCommandTunnelClient) Send(m *TunnelResponse) error {
	return x.ClientStream.SendMsg(m)
}

func (x *commandServiceCommandTunnelClient) Recv() (*TunnelCommand, error) {
	m := new(TunnelCommand)
	if err := x.ClientStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

func (c *commandServiceClient) CommandTunnel(ctx context.Context, opts ...grpc.CallOption) (CommandService_CommandTunnelClient, error) {
	stream, err := c.cc.NewStream(ctx, &CommandService_ServiceDesc.Streams[1], CommandService_CommandTunnel_FullMethodName, opts...)
	if err != nil {
		return nil, err
	}
	return &commandServiceCommandTunnelClient{stream}, nil
}

// CommandServiceServer is the server API for CommandService.
type CommandServiceServer interface {
	TriggerBackup(context.Context, *BackupRequest) (*BackupResponse, error)
	StopBackup(context.Context, *StopRequest) (*StopResponse, error)
	GetStatus(context.Context, *StatusRequest) (*StatusResponse, error)
	Ping(context.Context, *PingRequest) (*PingResponse, error)
	ListBackups(context.Context, *ListBackupsRequest) (*ListBackupsResponse, error)
	DeleteFromBackup(context.Context, *DeleteRequest) (*DeleteResponse, error)
	GetRetention(context.Context, *RetentionRequest) (*RetentionResponse, error)
	StartWatcher(context.Context, *WatcherRequest) (*WatcherResponse, error)
	StopWatcher(context.Context, *WatcherRequest) (*WatcherResponse, error)
	RegisterClient(context.Context, *RegisterRequest) (*RegisterResponse, error)
	PushRestore(CommandService_PushRestoreServer) error
	CommandTunnel(CommandService_CommandTunnelServer) error
	mustEmbedUnimplementedCommandServiceServer()
}

// UnimplementedCommandServiceServer should be embedded to have forward compatible implementations.
type UnimplementedCommandServiceServer struct{}

func (UnimplementedCommandServiceServer) TriggerBackup(context.Context, *BackupRequest) (*BackupResponse, error) {
	return nil, grpc.Errorf(12, "method TriggerBackup not implemented") //nolint:staticcheck
}
func (UnimplementedCommandServiceServer) StopBackup(context.Context, *StopRequest) (*StopResponse, error) {
	return nil, grpc.Errorf(12, "method StopBackup not implemented") //nolint:staticcheck
}
func (UnimplementedCommandServiceServer) GetStatus(context.Context, *StatusRequest) (*StatusResponse, error) {
	return nil, grpc.Errorf(12, "method GetStatus not implemented") //nolint:staticcheck
}
func (UnimplementedCommandServiceServer) Ping(context.Context, *PingRequest) (*PingResponse, error) {
	return nil, grpc.Errorf(12, "method Ping not implemented") //nolint:staticcheck
}
func (UnimplementedCommandServiceServer) ListBackups(context.Context, *ListBackupsRequest) (*ListBackupsResponse, error) {
	return nil, grpc.Errorf(12, "method ListBackups not implemented") //nolint:staticcheck
}
func (UnimplementedCommandServiceServer) DeleteFromBackup(context.Context, *DeleteRequest) (*DeleteResponse, error) {
	return nil, grpc.Errorf(12, "method DeleteFromBackup not implemented") //nolint:staticcheck
}
func (UnimplementedCommandServiceServer) GetRetention(context.Context, *RetentionRequest) (*RetentionResponse, error) {
	return nil, grpc.Errorf(12, "method GetRetention not implemented") //nolint:staticcheck
}
func (UnimplementedCommandServiceServer) StartWatcher(context.Context, *WatcherRequest) (*WatcherResponse, error) {
	return nil, grpc.Errorf(12, "method StartWatcher not implemented") //nolint:staticcheck
}
func (UnimplementedCommandServiceServer) StopWatcher(context.Context, *WatcherRequest) (*WatcherResponse, error) {
	return nil, grpc.Errorf(12, "method StopWatcher not implemented") //nolint:staticcheck
}
func (UnimplementedCommandServiceServer) RegisterClient(context.Context, *RegisterRequest) (*RegisterResponse, error) {
	return nil, grpc.Errorf(12, "method RegisterClient not implemented") //nolint:staticcheck
}
func (UnimplementedCommandServiceServer) PushRestore(CommandService_PushRestoreServer) error {
	return grpc.Errorf(12, "method PushRestore not implemented") //nolint:staticcheck
}
func (UnimplementedCommandServiceServer) CommandTunnel(CommandService_CommandTunnelServer) error {
	return grpc.Errorf(12, "method CommandTunnel not implemented") //nolint:staticcheck
}
func (UnimplementedCommandServiceServer) mustEmbedUnimplementedCommandServiceServer() {}

// UnsafeCommandServiceServer may be embedded to opt out of forward compatibility for this service.
type UnsafeCommandServiceServer interface {
	mustEmbedUnimplementedCommandServiceServer()
}

// RegisterCommandServiceServer registers the CommandService server implementation.
func RegisterCommandServiceServer(s grpc.ServiceRegistrar, srv CommandServiceServer) {
	s.RegisterService(&CommandService_ServiceDesc, srv)
}

// CommandService_PushRestoreServer is the server-side streaming interface for PushRestore.
type CommandService_PushRestoreServer interface {
	SendAndClose(*PushRestoreResponse) error
	Recv() (*FileChunk, error)
	grpc.ServerStream
}

type commandServicePushRestoreServer struct {
	grpc.ServerStream
}

func (x *commandServicePushRestoreServer) SendAndClose(m *PushRestoreResponse) error {
	return x.ServerStream.SendMsg(m)
}

func (x *commandServicePushRestoreServer) Recv() (*FileChunk, error) {
	m := new(FileChunk)
	if err := x.ServerStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

// CommandService_CommandTunnelServer is the bidirectional streaming interface for CommandTunnel (server side).
type CommandService_CommandTunnelServer interface {
	Send(*TunnelCommand) error
	Recv() (*TunnelResponse, error)
	grpc.ServerStream
}

type commandServiceCommandTunnelServer struct {
	grpc.ServerStream
}

func (x *commandServiceCommandTunnelServer) Send(m *TunnelCommand) error {
	return x.ServerStream.SendMsg(m)
}

func (x *commandServiceCommandTunnelServer) Recv() (*TunnelResponse, error) {
	m := new(TunnelResponse)
	if err := x.ServerStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

// CommandService_ServiceDesc is the grpc.ServiceDesc for CommandService.
var CommandService_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "tergum.v3.CommandService",
	HandlerType: (*CommandServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "TriggerBackup",
			Handler:    _CommandService_TriggerBackup_Handler,
		},
		{
			MethodName: "StopBackup",
			Handler:    _CommandService_StopBackup_Handler,
		},
		{
			MethodName: "GetStatus",
			Handler:    _CommandService_GetStatus_Handler,
		},
		{
			MethodName: "Ping",
			Handler:    _CommandService_Ping_Handler,
		},
		{
			MethodName: "ListBackups",
			Handler:    _CommandService_ListBackups_Handler,
		},
		{
			MethodName: "DeleteFromBackup",
			Handler:    _CommandService_DeleteFromBackup_Handler,
		},
		{
			MethodName: "GetRetention",
			Handler:    _CommandService_GetRetention_Handler,
		},
		{
			MethodName: "StartWatcher",
			Handler:    _CommandService_StartWatcher_Handler,
		},
		{
			MethodName: "StopWatcher",
			Handler:    _CommandService_StopWatcher_Handler,
		},
		{
			MethodName: "RegisterClient",
			Handler:    _CommandService_RegisterClient_Handler,
		},
	},
	Streams: []grpc.StreamDesc{
		{
			StreamName:    "PushRestore",
			Handler:       _CommandService_PushRestore_Handler,
			ClientStreams: true,
		},
		{
			StreamName:    "CommandTunnel",
			Handler:       _CommandService_CommandTunnel_Handler,
			ServerStreams: true,
			ClientStreams: true,
		},
	},
	Metadata: "proto/v3/command.proto",
}

func _CommandService_TriggerBackup_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(BackupRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(CommandServiceServer).TriggerBackup(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: CommandService_TriggerBackup_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(CommandServiceServer).TriggerBackup(ctx, req.(*BackupRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _CommandService_StopBackup_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(StopRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(CommandServiceServer).StopBackup(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: CommandService_StopBackup_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(CommandServiceServer).StopBackup(ctx, req.(*StopRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _CommandService_GetStatus_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(StatusRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(CommandServiceServer).GetStatus(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: CommandService_GetStatus_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(CommandServiceServer).GetStatus(ctx, req.(*StatusRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _CommandService_Ping_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(PingRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(CommandServiceServer).Ping(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: CommandService_Ping_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(CommandServiceServer).Ping(ctx, req.(*PingRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _CommandService_ListBackups_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(ListBackupsRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(CommandServiceServer).ListBackups(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: CommandService_ListBackups_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(CommandServiceServer).ListBackups(ctx, req.(*ListBackupsRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _CommandService_DeleteFromBackup_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(DeleteRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(CommandServiceServer).DeleteFromBackup(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: CommandService_DeleteFromBackup_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(CommandServiceServer).DeleteFromBackup(ctx, req.(*DeleteRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _CommandService_GetRetention_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(RetentionRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(CommandServiceServer).GetRetention(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: CommandService_GetRetention_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(CommandServiceServer).GetRetention(ctx, req.(*RetentionRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _CommandService_StartWatcher_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(WatcherRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(CommandServiceServer).StartWatcher(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: CommandService_StartWatcher_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(CommandServiceServer).StartWatcher(ctx, req.(*WatcherRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _CommandService_StopWatcher_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(WatcherRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(CommandServiceServer).StopWatcher(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: CommandService_StopWatcher_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(CommandServiceServer).StopWatcher(ctx, req.(*WatcherRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _CommandService_RegisterClient_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(RegisterRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(CommandServiceServer).RegisterClient(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: CommandService_RegisterClient_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(CommandServiceServer).RegisterClient(ctx, req.(*RegisterRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _CommandService_PushRestore_Handler(srv interface{}, stream grpc.ServerStream) error {
	return srv.(CommandServiceServer).PushRestore(&commandServicePushRestoreServer{stream})
}

func _CommandService_CommandTunnel_Handler(srv interface{}, stream grpc.ServerStream) error {
	return srv.(CommandServiceServer).CommandTunnel(&commandServiceCommandTunnelServer{stream})
}
