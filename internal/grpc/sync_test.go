package grpc

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/gcclinux/tergum/internal/grpc/proto"
	"google.golang.org/grpc"
)

// --- SyncDatabase Server Handler Tests ---

func TestDataServer_SyncDatabase_StoresWithClientID(t *testing.T) {
	clientsDir := t.TempDir()

	srv := NewDataServer(DataServerConfig{
		Store:      &mockStore{existing: map[string]bool{}},
		Repo:       &mockRepo{},
		ClientsDir: clientsDir,
	})

	// Simulate a stream with client_id in the first chunk.
	dbContent := []byte("SQLite format 3\x00test database content here")
	stream := &mockSyncDatabaseServer{
		chunks: []*proto.DatabaseChunk{
			{Data: dbContent[:20], ClientId: "workstation1"},
			{Data: dbContent[20:]},
		},
	}

	err := srv.SyncDatabase(stream)
	if err != nil {
		t.Fatalf("SyncDatabase() error: %v", err)
	}

	// Verify file was written to correct location.
	destPath := filepath.Join(clientsDir, "workstation1.db")
	data, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("reading synced file: %v", err)
	}
	if string(data) != string(dbContent) {
		t.Errorf("synced content mismatch: got %q, want %q", data, dbContent)
	}

	// Verify response.
	if stream.response == nil {
		t.Fatal("expected response to be set")
	}
	if !stream.response.Success {
		t.Errorf("expected Success=true, got false: %s", stream.response.Message)
	}
}

func TestDataServer_SyncDatabase_MissingClientID(t *testing.T) {
	clientsDir := t.TempDir()

	srv := NewDataServer(DataServerConfig{
		Store:      &mockStore{existing: map[string]bool{}},
		Repo:       &mockRepo{},
		ClientsDir: clientsDir,
	})

	// Stream with empty client_id.
	stream := &mockSyncDatabaseServer{
		chunks: []*proto.DatabaseChunk{
			{Data: []byte("data"), ClientId: ""},
		},
	}

	err := srv.SyncDatabase(stream)
	if err == nil {
		t.Fatal("expected error when client_id is missing")
	}
}

func TestDataServer_SyncDatabase_EmptyStream(t *testing.T) {
	clientsDir := t.TempDir()

	srv := NewDataServer(DataServerConfig{
		Store:      &mockStore{existing: map[string]bool{}},
		Repo:       &mockRepo{},
		ClientsDir: clientsDir,
	})

	stream := &mockSyncDatabaseServer{
		chunks: []*proto.DatabaseChunk{},
	}

	err := srv.SyncDatabase(stream)
	if err == nil {
		t.Fatal("expected error for empty stream")
	}
}

func TestDataServer_SyncDatabase_NoClientsDir(t *testing.T) {
	srv := NewDataServer(DataServerConfig{
		Store:      &mockStore{existing: map[string]bool{}},
		Repo:       &mockRepo{},
		ClientsDir: "", // not configured
	})

	stream := &mockSyncDatabaseServer{
		chunks: []*proto.DatabaseChunk{
			{Data: []byte("data"), ClientId: "test-client"},
		},
	}

	err := srv.SyncDatabase(stream)
	if err == nil {
		t.Fatal("expected error when clients directory not configured")
	}
}

func TestDataServer_SyncDatabase_MultipleClients(t *testing.T) {
	clientsDir := t.TempDir()

	srv := NewDataServer(DataServerConfig{
		Store:      &mockStore{existing: map[string]bool{}},
		Repo:       &mockRepo{},
		ClientsDir: clientsDir,
	})

	// Sync first client.
	stream1 := &mockSyncDatabaseServer{
		chunks: []*proto.DatabaseChunk{
			{Data: []byte("client1-data"), ClientId: "laptop1"},
		},
	}
	if err := srv.SyncDatabase(stream1); err != nil {
		t.Fatalf("SyncDatabase(laptop1) error: %v", err)
	}

	// Sync second client.
	stream2 := &mockSyncDatabaseServer{
		chunks: []*proto.DatabaseChunk{
			{Data: []byte("client2-data"), ClientId: "desktop1"},
		},
	}
	if err := srv.SyncDatabase(stream2); err != nil {
		t.Fatalf("SyncDatabase(desktop1) error: %v", err)
	}

	// Verify both files exist independently.
	data1, err := os.ReadFile(filepath.Join(clientsDir, "laptop1.db"))
	if err != nil {
		t.Fatalf("reading laptop1.db: %v", err)
	}
	if string(data1) != "client1-data" {
		t.Errorf("laptop1.db content = %q, want %q", data1, "client1-data")
	}

	data2, err := os.ReadFile(filepath.Join(clientsDir, "desktop1.db"))
	if err != nil {
		t.Fatalf("reading desktop1.db: %v", err)
	}
	if string(data2) != "client2-data" {
		t.Errorf("desktop1.db content = %q, want %q", data2, "client2-data")
	}
}

// --- SyncDatabaseToServer Client Function Tests ---

func TestSyncDatabaseToServer_Success(t *testing.T) {
	// Create a temp DB file to sync.
	dbDir := t.TempDir()
	dbPath := filepath.Join(dbDir, "test.db")
	dbContent := []byte("test database content for sync")
	if err := os.WriteFile(dbPath, dbContent, 0o644); err != nil {
		t.Fatalf("writing test db: %v", err)
	}

	mockClient := &mockDataServiceClient{
		syncStream: &mockSyncDatabaseClientStream{},
	}

	err := SyncDatabaseToServer(context.Background(), mockClient, dbPath, "my-client")
	if err != nil {
		t.Fatalf("SyncDatabaseToServer() error: %v", err)
	}

	// Verify chunks were sent.
	chunks := mockClient.syncStream.sentChunks
	if len(chunks) == 0 {
		t.Fatal("expected at least one chunk to be sent")
	}

	// First chunk should have client_id.
	if chunks[0].ClientId != "my-client" {
		t.Errorf("first chunk ClientId = %q, want %q", chunks[0].ClientId, "my-client")
	}

	// Subsequent chunks should not have client_id.
	for i := 1; i < len(chunks); i++ {
		if chunks[i].ClientId != "" {
			t.Errorf("chunk %d should not have ClientId, got %q", i, chunks[i].ClientId)
		}
	}

	// Verify total data matches.
	var totalData []byte
	for _, c := range chunks {
		totalData = append(totalData, c.Data...)
	}
	if string(totalData) != string(dbContent) {
		t.Errorf("sent data mismatch: got %q, want %q", totalData, dbContent)
	}
}

func TestSyncDatabaseToServer_EmptyClientID(t *testing.T) {
	dbDir := t.TempDir()
	dbPath := filepath.Join(dbDir, "test.db")
	os.WriteFile(dbPath, []byte("data"), 0o644)

	mockClient := &mockDataServiceClient{}

	err := SyncDatabaseToServer(context.Background(), mockClient, dbPath, "")
	if err == nil {
		t.Fatal("expected error when clientID is empty")
	}
}

func TestSyncDatabaseToServer_FileNotFound(t *testing.T) {
	mockClient := &mockDataServiceClient{}

	err := SyncDatabaseToServer(context.Background(), mockClient, "/nonexistent/path.db", "client1")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestSyncDatabaseToServer_LargeFile(t *testing.T) {
	// Create a file larger than syncChunkSize to test chunking.
	dbDir := t.TempDir()
	dbPath := filepath.Join(dbDir, "large.db")

	// 150KB of data â€” should result in multiple chunks.
	largeData := make([]byte, 150*1024)
	for i := range largeData {
		largeData[i] = byte(i % 256)
	}
	if err := os.WriteFile(dbPath, largeData, 0o644); err != nil {
		t.Fatalf("writing large test db: %v", err)
	}

	mockClient := &mockDataServiceClient{
		syncStream: &mockSyncDatabaseClientStream{},
	}

	err := SyncDatabaseToServer(context.Background(), mockClient, dbPath, "big-client")
	if err != nil {
		t.Fatalf("SyncDatabaseToServer() error: %v", err)
	}

	chunks := mockClient.syncStream.sentChunks
	if len(chunks) < 2 {
		t.Errorf("expected multiple chunks for 150KB file, got %d", len(chunks))
	}

	// Verify first chunk has client_id.
	if chunks[0].ClientId != "big-client" {
		t.Errorf("first chunk ClientId = %q, want %q", chunks[0].ClientId, "big-client")
	}

	// Reconstruct and verify data.
	var totalData []byte
	for _, c := range chunks {
		totalData = append(totalData, c.Data...)
	}
	if len(totalData) != len(largeData) {
		t.Errorf("total data length = %d, want %d", len(totalData), len(largeData))
	}
	for i := range largeData {
		if totalData[i] != largeData[i] {
			t.Fatalf("data mismatch at byte %d: got %d, want %d", i, totalData[i], largeData[i])
		}
	}
}

// --- Mock Types for SyncDatabase Testing ---

// mockSyncDatabaseServer simulates the server-side stream for SyncDatabase.
type mockSyncDatabaseServer struct {
	grpc.ServerStream
	chunks   []*proto.DatabaseChunk
	idx      int
	response *proto.SyncResponse
}

func (m *mockSyncDatabaseServer) Recv() (*proto.DatabaseChunk, error) {
	if m.idx >= len(m.chunks) {
		return nil, io.EOF
	}
	chunk := m.chunks[m.idx]
	m.idx++
	return chunk, nil
}

func (m *mockSyncDatabaseServer) SendAndClose(resp *proto.SyncResponse) error {
	m.response = resp
	return nil
}

func (m *mockSyncDatabaseServer) Context() context.Context {
	return context.Background()
}

// mockDataServiceClient implements proto.DataServiceClient for testing SyncDatabaseToServer.
type mockDataServiceClient struct {
	syncStream *mockSyncDatabaseClientStream
}

func (m *mockDataServiceClient) Upload(ctx context.Context, opts ...grpc.CallOption) (proto.DataService_UploadClient, error) {
	return nil, nil
}

func (m *mockDataServiceClient) Download(ctx context.Context, in *proto.RestoreRequest, opts ...grpc.CallOption) (proto.DataService_DownloadClient, error) {
	return nil, nil
}

func (m *mockDataServiceClient) SyncDatabase(ctx context.Context, opts ...grpc.CallOption) (proto.DataService_SyncDatabaseClient, error) {
	if m.syncStream == nil {
		m.syncStream = &mockSyncDatabaseClientStream{}
	}
	return m.syncStream, nil
}

func (m *mockDataServiceClient) ExchangeManifest(ctx context.Context, in *proto.Manifest, opts ...grpc.CallOption) (*proto.ManifestDiff, error) {
	return nil, nil
}

// mockSyncDatabaseClientStream simulates the client-side stream for SyncDatabase.
type mockSyncDatabaseClientStream struct {
	grpc.ClientStream
	sentChunks []*proto.DatabaseChunk
}

func (m *mockSyncDatabaseClientStream) Send(chunk *proto.DatabaseChunk) error {
	m.sentChunks = append(m.sentChunks, chunk)
	return nil
}

func (m *mockSyncDatabaseClientStream) CloseAndRecv() (*proto.SyncResponse, error) {
	return &proto.SyncResponse{Success: true, Message: "ok"}, nil
}
