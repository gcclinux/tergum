package proto

import (
	"context"

	"google.golang.org/grpc"
)

// DataServiceClient is the client API for DataService.
type DataServiceClient interface {
	Upload(ctx context.Context, opts ...grpc.CallOption) (DataService_UploadClient, error)
	Download(ctx context.Context, in *RestoreRequest, opts ...grpc.CallOption) (DataService_DownloadClient, error)
	SyncDatabase(ctx context.Context, opts ...grpc.CallOption) (DataService_SyncDatabaseClient, error)
	ExchangeManifest(ctx context.Context, in *Manifest, opts ...grpc.CallOption) (*ManifestDiff, error)
}

type dataServiceClient struct {
	cc grpc.ClientConnInterface
}

// NewDataServiceClient creates a new DataService client.
func NewDataServiceClient(cc grpc.ClientConnInterface) DataServiceClient {
	return &dataServiceClient{cc}
}

const (
	DataService_Upload_FullMethodName           = "/tergum.v3.DataService/Upload"
	DataService_Download_FullMethodName         = "/tergum.v3.DataService/Download"
	DataService_SyncDatabase_FullMethodName     = "/tergum.v3.DataService/SyncDatabase"
	DataService_ExchangeManifest_FullMethodName = "/tergum.v3.DataService/ExchangeManifest"
)

// DataService_UploadClient is the client streaming interface for Upload.
type DataService_UploadClient interface {
	Send(*FileChunk) error
	CloseAndRecv() (*UploadSummary, error)
	grpc.ClientStream
}

type dataServiceUploadClient struct {
	grpc.ClientStream
}

func (x *dataServiceUploadClient) Send(m *FileChunk) error {
	return x.ClientStream.SendMsg(m)
}

func (x *dataServiceUploadClient) CloseAndRecv() (*UploadSummary, error) {
	if err := x.ClientStream.CloseSend(); err != nil {
		return nil, err
	}
	m := new(UploadSummary)
	if err := x.ClientStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

func (c *dataServiceClient) Upload(ctx context.Context, opts ...grpc.CallOption) (DataService_UploadClient, error) {
	stream, err := c.cc.NewStream(ctx, &DataService_ServiceDesc.Streams[0], DataService_Upload_FullMethodName, opts...)
	if err != nil {
		return nil, err
	}
	x := &dataServiceUploadClient{stream}
	return x, nil
}

// DataService_DownloadClient is the server streaming interface for Download.
type DataService_DownloadClient interface {
	Recv() (*FileChunk, error)
	grpc.ClientStream
}

type dataServiceDownloadClient struct {
	grpc.ClientStream
}

func (x *dataServiceDownloadClient) Recv() (*FileChunk, error) {
	m := new(FileChunk)
	if err := x.ClientStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

func (c *dataServiceClient) Download(ctx context.Context, in *RestoreRequest, opts ...grpc.CallOption) (DataService_DownloadClient, error) {
	stream, err := c.cc.NewStream(ctx, &DataService_ServiceDesc.Streams[1], DataService_Download_FullMethodName, opts...)
	if err != nil {
		return nil, err
	}
	x := &dataServiceDownloadClient{stream}
	if err := x.ClientStream.SendMsg(in); err != nil {
		return nil, err
	}
	if err := x.ClientStream.CloseSend(); err != nil {
		return nil, err
	}
	return x, nil
}

// DataService_SyncDatabaseClient is the client streaming interface for SyncDatabase.
type DataService_SyncDatabaseClient interface {
	Send(*DatabaseChunk) error
	CloseAndRecv() (*SyncResponse, error)
	grpc.ClientStream
}

type dataServiceSyncDatabaseClient struct {
	grpc.ClientStream
}

func (x *dataServiceSyncDatabaseClient) Send(m *DatabaseChunk) error {
	return x.ClientStream.SendMsg(m)
}

func (x *dataServiceSyncDatabaseClient) CloseAndRecv() (*SyncResponse, error) {
	if err := x.ClientStream.CloseSend(); err != nil {
		return nil, err
	}
	m := new(SyncResponse)
	if err := x.ClientStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

func (c *dataServiceClient) SyncDatabase(ctx context.Context, opts ...grpc.CallOption) (DataService_SyncDatabaseClient, error) {
	stream, err := c.cc.NewStream(ctx, &DataService_ServiceDesc.Streams[2], DataService_SyncDatabase_FullMethodName, opts...)
	if err != nil {
		return nil, err
	}
	x := &dataServiceSyncDatabaseClient{stream}
	return x, nil
}

func (c *dataServiceClient) ExchangeManifest(ctx context.Context, in *Manifest, opts ...grpc.CallOption) (*ManifestDiff, error) {
	out := new(ManifestDiff)
	err := c.cc.Invoke(ctx, DataService_ExchangeManifest_FullMethodName, in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// DataServiceServer is the server API for DataService.
type DataServiceServer interface {
	Upload(DataService_UploadServer) error
	Download(*RestoreRequest, DataService_DownloadServer) error
	SyncDatabase(DataService_SyncDatabaseServer) error
	ExchangeManifest(context.Context, *Manifest) (*ManifestDiff, error)
	mustEmbedUnimplementedDataServiceServer()
}

// UnimplementedDataServiceServer should be embedded to have forward compatible implementations.
type UnimplementedDataServiceServer struct{}

func (UnimplementedDataServiceServer) Upload(DataService_UploadServer) error {
	return grpc.Errorf(12, "method Upload not implemented") //nolint:staticcheck
}
func (UnimplementedDataServiceServer) Download(*RestoreRequest, DataService_DownloadServer) error {
	return grpc.Errorf(12, "method Download not implemented") //nolint:staticcheck
}
func (UnimplementedDataServiceServer) SyncDatabase(DataService_SyncDatabaseServer) error {
	return grpc.Errorf(12, "method SyncDatabase not implemented") //nolint:staticcheck
}
func (UnimplementedDataServiceServer) ExchangeManifest(context.Context, *Manifest) (*ManifestDiff, error) {
	return nil, grpc.Errorf(12, "method ExchangeManifest not implemented") //nolint:staticcheck
}
func (UnimplementedDataServiceServer) mustEmbedUnimplementedDataServiceServer() {}

// UnsafeDataServiceServer may be embedded to opt out of forward compatibility for this service.
type UnsafeDataServiceServer interface {
	mustEmbedUnimplementedDataServiceServer()
}

// DataService_UploadServer is the server streaming interface for Upload.
type DataService_UploadServer interface {
	SendAndClose(*UploadSummary) error
	Recv() (*FileChunk, error)
	grpc.ServerStream
}

type dataServiceUploadServer struct {
	grpc.ServerStream
}

func (x *dataServiceUploadServer) SendAndClose(m *UploadSummary) error {
	return x.ServerStream.SendMsg(m)
}

func (x *dataServiceUploadServer) Recv() (*FileChunk, error) {
	m := new(FileChunk)
	if err := x.ServerStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

// DataService_DownloadServer is the server streaming interface for Download.
type DataService_DownloadServer interface {
	Send(*FileChunk) error
	grpc.ServerStream
}

type dataServiceDownloadServer struct {
	grpc.ServerStream
}

func (x *dataServiceDownloadServer) Send(m *FileChunk) error {
	return x.ServerStream.SendMsg(m)
}

// DataService_SyncDatabaseServer is the server streaming interface for SyncDatabase.
type DataService_SyncDatabaseServer interface {
	SendAndClose(*SyncResponse) error
	Recv() (*DatabaseChunk, error)
	grpc.ServerStream
}

type dataServiceSyncDatabaseServer struct {
	grpc.ServerStream
}

func (x *dataServiceSyncDatabaseServer) SendAndClose(m *SyncResponse) error {
	return x.ServerStream.SendMsg(m)
}

func (x *dataServiceSyncDatabaseServer) Recv() (*DatabaseChunk, error) {
	m := new(DatabaseChunk)
	if err := x.ServerStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

// RegisterDataServiceServer registers the DataService server implementation.
func RegisterDataServiceServer(s grpc.ServiceRegistrar, srv DataServiceServer) {
	s.RegisterService(&DataService_ServiceDesc, srv)
}

// DataService_ServiceDesc is the grpc.ServiceDesc for DataService.
var DataService_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "tergum.v3.DataService",
	HandlerType: (*DataServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "ExchangeManifest",
			Handler:    _DataService_ExchangeManifest_Handler,
		},
	},
	Streams: []grpc.StreamDesc{
		{
			StreamName:    "Upload",
			Handler:       _DataService_Upload_Handler,
			ClientStreams: true,
		},
		{
			StreamName:    "Download",
			Handler:       _DataService_Download_Handler,
			ServerStreams: true,
		},
		{
			StreamName:    "SyncDatabase",
			Handler:       _DataService_SyncDatabase_Handler,
			ClientStreams: true,
		},
	},
	Metadata: "proto/v3/data.proto",
}

func _DataService_Upload_Handler(srv interface{}, stream grpc.ServerStream) error {
	return srv.(DataServiceServer).Upload(&dataServiceUploadServer{stream})
}

func _DataService_Download_Handler(srv interface{}, stream grpc.ServerStream) error {
	m := new(RestoreRequest)
	if err := stream.RecvMsg(m); err != nil {
		return err
	}
	return srv.(DataServiceServer).Download(m, &dataServiceDownloadServer{stream})
}

func _DataService_SyncDatabase_Handler(srv interface{}, stream grpc.ServerStream) error {
	return srv.(DataServiceServer).SyncDatabase(&dataServiceSyncDatabaseServer{stream})
}

func _DataService_ExchangeManifest_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(Manifest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(DataServiceServer).ExchangeManifest(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: DataService_ExchangeManifest_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(DataServiceServer).ExchangeManifest(ctx, req.(*Manifest))
	}
	return interceptor(ctx, in, info, handler)
}
