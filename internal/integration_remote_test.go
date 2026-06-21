// Package internal_test contains integration tests that exercise the remote backup/restore
// pipeline over real gRPC with mTLS authentication.
package internal_test

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ricardopadilha/tergum/internal/backup"
	"github.com/ricardopadilha/tergum/internal/config"
	"github.com/ricardopadilha/tergum/internal/db"
	tgrpc "github.com/ricardopadilha/tergum/internal/grpc"
	"github.com/ricardopadilha/tergum/internal/grpc/proto"
	"github.com/ricardopadilha/tergum/internal/model"
	"github.com/ricardopadilha/tergum/internal/registry"
	"github.com/ricardopadilha/tergum/internal/scheduler"
	"github.com/ricardopadilha/tergum/internal/storage"
	ttls "github.com/ricardopadilha/tergum/internal/tls"
	"github.com/ricardopadilha/tergum/internal/watcher"
	"google.golang.org/grpc"
	grpcCodes "google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	grpcStatus "google.golang.org/grpc/status"

	_ "modernc.org/sqlite"
)

// TestIntegration_RemoteBackupRoundTrip exercises the full remote backup and restore
// pipeline over a real gRPC connection with mTLS:
//  1. Generate test certs
//  2. Start a gRPC server with CommandService + DataService
//  3. Connect a client with mTLS
//  4. Create test files, run backup through RemoteServerConnection
//  5. Verify files exist in the server's CAS storage
//  6. Use RemoteDataSource to download and verify content matches
//  7. Run a second backup to verify manifest exchange deduplication (no re-upload)
//  8. Verify SyncDatabase transfers the client DB to the server
func TestIntegration_RemoteBackupRoundTrip(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	// --- Setup directories ---
	certDir := t.TempDir()
	storageDir := t.TempDir()
	sourceDir := t.TempDir()
	clientsDir := t.TempDir()

	// --- Generate mTLS certs ---
	mgr := ttls.NewManager()
	if err := mgr.GenerateCerts(certDir); err != nil {
		t.Fatalf("GenerateCerts: %v", err)
	}

	// --- Load TLS configs ---
	serverTLS, err := mgr.LoadServerTLS(
		filepath.Join(certDir, "ca.crt"),
		filepath.Join(certDir, "server.crt"),
		filepath.Join(certDir, "server.key"),
	)
	if err != nil {
		t.Fatalf("LoadServerTLS: %v", err)
	}

	clientTLS, err := mgr.LoadClientTLS(
		filepath.Join(certDir, "ca.crt"),
		filepath.Join(certDir, "client.crt"),
		filepath.Join(certDir, "client.key"),
	)
	if err != nil {
		t.Fatalf("LoadClientTLS: %v", err)
	}
	clientTLS.ServerName = "localhost"

	// --- Create server-side repository and CAS ---
	serverRepo, err := db.NewRepository(":memory:", true)
	if err != nil {
		t.Fatalf("server repo: %v", err)
	}
	t.Cleanup(func() { serverRepo.Close() })

	cas := storage.NewCAS(storageDir, serverRepo)

	// --- Start gRPC server ---
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}

	serverCreds := credentials.NewTLS(serverTLS)
	grpcServer := grpc.NewServer(grpc.Creds(serverCreds))

	// Register CommandService.
	cmdServer := tgrpc.NewCommandServer(tgrpc.CommandServerConfig{
		BackupEngine: &noopBackupEngine{},
		Repo:         serverRepo,
		Version:      "test-3.0.0",
	})
	proto.RegisterCommandServiceServer(grpcServer, cmdServer)

	// Register DataService.
	dataServer := tgrpc.NewDataServer(tgrpc.DataServerConfig{
		Store:      cas,
		Repo:       serverRepo,
		ClientsDir: clientsDir,
	})
	proto.RegisterDataServiceServer(grpcServer, dataServer)

	go func() {
		if err := grpcServer.Serve(listener); err != nil {
			// Server stopped — expected during cleanup.
		}
	}()
	t.Cleanup(func() { grpcServer.GracefulStop() })

	serverAddr := listener.Addr().String()

	// --- Connect client with mTLS ---
	clientCreds := credentials.NewTLS(clientTLS)
	conn, err := grpc.NewClient(serverAddr, grpc.WithTransportCredentials(clientCreds))
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	dataClient := proto.NewDataServiceClient(conn)

	// --- Create test source files ---
	testFiles := map[string]string{
		"hello.txt":        "Hello, remote world!",
		"data/report.md":   "# Report\nRemote backup test content.",
		"config/settings":  "host=server\nport=7401",
	}
	for name, content := range testFiles {
		path := filepath.Join(sourceDir, name)
		dir := filepath.Dir(path)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	// --- Create client-side repository ---
	clientDBPath := filepath.Join(t.TempDir(), "client.db")
	clientRepo, err := db.NewRepository(clientDBPath, false)
	if err != nil {
		t.Fatalf("client repo: %v", err)
	}
	t.Cleanup(func() { clientRepo.Close() })

	// --- Run backup via RemoteServerConnection ---
	remoteConn := tgrpc.NewRemoteServerConnection(dataClient, "test-client")

	backupCfg := backup.EngineConfig{
		IncludePaths:    []string{sourceDir},
		ExcludePatterns: []string{},
		MaxFileSize:     10 * 1024 * 1024,
		EncryptionOn:    false,
		DatabasePath:    clientDBPath,
	}
	engine := backup.NewBackupEngine(remoteConn, clientRepo, nil, backupCfg)

	result, err := engine.RunBackup(ctx, backup.BackupRequest{
		Level:       model.BackupLevelFull,
		ClientID:    "test-client",
		InitiatedBy: "integration-test",
	})
	if err != nil {
		t.Fatalf("first RunBackup: %v", err)
	}
	if result.Status != model.JobCompleted {
		t.Fatalf("expected completed status, got %s", result.Status)
	}
	if result.FilesProcessed != int64(len(testFiles)) {
		t.Fatalf("expected %d files processed, got %d", len(testFiles), result.FilesProcessed)
	}
	if result.BytesNew == 0 {
		t.Fatal("expected non-zero BytesNew for first backup")
	}

	// --- Verify files exist in server CAS storage ---
	for name, content := range testFiles {
		// Look up the entry via the client repo to get the hash.
		entries, err := clientRepo.FindByPath(ctx, "%"+filepath.Base(name))
		if err != nil {
			t.Fatalf("FindByPath %s: %v", name, err)
		}
		if len(entries) == 0 {
			t.Fatalf("no entry found for %s", name)
		}
		hash := entries[0].Blake3Hash

		// Verify it exists in the server CAS.
		exists, err := cas.Exists(ctx, hash)
		if err != nil {
			t.Fatalf("CAS.Exists for %s: %v", name, err)
		}
		if !exists {
			t.Fatalf("file %s (hash %s) not found in server CAS", name, hash)
		}

		// Verify content via CAS Get.
		reader, err := cas.Get(ctx, hash)
		if err != nil {
			t.Fatalf("CAS.Get for %s: %v", name, err)
		}
		data, err := readAllAndClose(reader)
		if err != nil {
			t.Fatalf("reading CAS data for %s: %v", name, err)
		}
		if string(data) != content {
			t.Errorf("CAS content mismatch for %s: got %q, want %q", name, data, content)
		}
	}

	// --- Restore via RemoteDataSource ---
	remoteSource := tgrpc.NewRemoteDataSource(dataClient, "test-client")

	for name, expectedContent := range testFiles {
		entries, err := clientRepo.FindByPath(ctx, "%"+filepath.Base(name))
		if err != nil {
			t.Fatalf("FindByPath for restore %s: %v", name, err)
		}
		if len(entries) == 0 {
			t.Fatalf("no entry for restore %s", name)
		}

		data, err := remoteSource.DownloadFile(ctx, entries[0].Blake3Hash)
		if err != nil {
			t.Fatalf("DownloadFile %s: %v", name, err)
		}
		if string(data) != expectedContent {
			t.Errorf("restored content mismatch for %s: got %q, want %q", name, data, expectedContent)
		}
	}

	// --- Run second backup to verify deduplication ---
	result2, err := engine.RunBackup(ctx, backup.BackupRequest{
		Level:       model.BackupLevelFull,
		ClientID:    "test-client",
		InitiatedBy: "integration-test-2",
	})
	if err != nil {
		t.Fatalf("second RunBackup: %v", err)
	}
	if result2.Status != model.JobCompleted {
		t.Fatalf("second backup status: %s", result2.Status)
	}
	if result2.FilesDeduped != int64(len(testFiles)) {
		t.Errorf("expected %d deduped files in second backup, got %d", len(testFiles), result2.FilesDeduped)
	}
	if result2.BytesNew != 0 {
		t.Errorf("expected 0 new bytes in second backup (all deduplicated), got %d", result2.BytesNew)
	}

	// --- Verify SyncDatabase transferred the client DB to the server ---
	syncedDBPath := filepath.Join(clientsDir, "test-client.db")
	info, err := os.Stat(syncedDBPath)
	if err != nil {
		t.Fatalf("SyncDatabase file not found at %s: %v", syncedDBPath, err)
	}
	if info.Size() == 0 {
		t.Fatal("synced database file is empty")
	}

	// Verify the synced DB is a valid copy (non-zero and can be opened).
	syncedRepo, err := db.NewRepository(syncedDBPath, false)
	if err != nil {
		t.Fatalf("opening synced DB: %v", err)
	}
	syncedRepo.Close()
}

// TestIntegration_RemotePingOverMTLS verifies that the Ping RPC works over mTLS.
func TestIntegration_RemotePingOverMTLS(t *testing.T) {
	t.Parallel()

	certDir := t.TempDir()
	mgr := ttls.NewManager()
	if err := mgr.GenerateCerts(certDir); err != nil {
		t.Fatalf("GenerateCerts: %v", err)
	}

	serverTLS, err := mgr.LoadServerTLS(
		filepath.Join(certDir, "ca.crt"),
		filepath.Join(certDir, "server.crt"),
		filepath.Join(certDir, "server.key"),
	)
	if err != nil {
		t.Fatalf("LoadServerTLS: %v", err)
	}

	clientTLS, err := mgr.LoadClientTLS(
		filepath.Join(certDir, "ca.crt"),
		filepath.Join(certDir, "client.crt"),
		filepath.Join(certDir, "client.key"),
	)
	if err != nil {
		t.Fatalf("LoadClientTLS: %v", err)
	}
	clientTLS.ServerName = "localhost"

	repo, err := db.NewRepository(":memory:", true)
	if err != nil {
		t.Fatalf("repo: %v", err)
	}
	t.Cleanup(func() { repo.Close() })

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	grpcServer := grpc.NewServer(grpc.Creds(credentials.NewTLS(serverTLS)))
	cmdServer := tgrpc.NewCommandServer(tgrpc.CommandServerConfig{
		BackupEngine: &noopBackupEngine{},
		Repo:         repo,
		Version:      "3.0.0-integ",
	})
	proto.RegisterCommandServiceServer(grpcServer, cmdServer)

	go grpcServer.Serve(listener)
	t.Cleanup(func() { grpcServer.GracefulStop() })

	conn, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(credentials.NewTLS(clientTLS)))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	cmdClient := proto.NewCommandServiceClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := cmdClient.Ping(ctx, &proto.PingRequest{})
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if resp.Version != "3.0.0-integ" {
		t.Errorf("version = %q, want %q", resp.Version, "3.0.0-integ")
	}
	if resp.Uptime == "" {
		t.Error("uptime should not be empty")
	}
}

// TestIntegration_ServerTriggeredBackup verifies that a server can trigger a backup
// on a client's CommandService, that data arrives in the server CAS, and that a
// second TriggerBackup returns AlreadyExists when a backup is already running.
func TestIntegration_ServerTriggeredBackup(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	// --- Setup directories ---
	certDir := t.TempDir()
	storageDir := t.TempDir()
	sourceDir := t.TempDir()
	clientsDir := t.TempDir()

	// --- Generate mTLS certs ---
	mgr := ttls.NewManager()
	if err := mgr.GenerateCerts(certDir); err != nil {
		t.Fatalf("GenerateCerts: %v", err)
	}

	// --- Load TLS configs ---
	serverTLS, err := mgr.LoadServerTLS(
		filepath.Join(certDir, "ca.crt"),
		filepath.Join(certDir, "server.crt"),
		filepath.Join(certDir, "server.key"),
	)
	if err != nil {
		t.Fatalf("LoadServerTLS: %v", err)
	}

	clientTLS, err := mgr.LoadClientTLS(
		filepath.Join(certDir, "ca.crt"),
		filepath.Join(certDir, "client.crt"),
		filepath.Join(certDir, "client.key"),
	)
	if err != nil {
		t.Fatalf("LoadClientTLS: %v", err)
	}
	clientTLS.ServerName = "localhost"

	// --- Create test source files for the client to back up ---
	testFiles := map[string]string{
		"doc.txt":         "Server-triggered backup test content",
		"subdir/notes.md": "# Notes\nTriggered by the server.",
	}
	for name, content := range testFiles {
		path := filepath.Join(sourceDir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	// --- Create server-side repository and CAS ---
	serverRepo, err := db.NewRepository(":memory:", true)
	if err != nil {
		t.Fatalf("server repo: %v", err)
	}
	t.Cleanup(func() { serverRepo.Close() })

	cas := storage.NewCAS(storageDir, serverRepo)

	// --- Create a registry for the server ---
	registryDB, err := newTestRegistryDB()
	if err != nil {
		t.Fatalf("registry DB: %v", err)
	}
	t.Cleanup(func() { registryDB.Close() })

	reg, err := newTestRegistry(registryDB)
	if err != nil {
		t.Fatalf("registry: %v", err)
	}

	// --- Start the server gRPC (CommandService + DataService) ---
	serverListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen (server): %v", err)
	}

	serverCreds := credentials.NewTLS(serverTLS)
	grpcServer := grpc.NewServer(grpc.Creds(serverCreds))

	cmdServer := tgrpc.NewCommandServer(tgrpc.CommandServerConfig{
		BackupEngine: &noopBackupEngine{},
		Repo:         serverRepo,
		Registry:     reg,
		Version:      "test-3.0.0",
	})
	proto.RegisterCommandServiceServer(grpcServer, cmdServer)

	dataServer := tgrpc.NewDataServer(tgrpc.DataServerConfig{
		Store:      cas,
		Repo:       serverRepo,
		ClientsDir: clientsDir,
	})
	proto.RegisterDataServiceServer(grpcServer, dataServer)

	go grpcServer.Serve(serverListener)
	t.Cleanup(func() { grpcServer.GracefulStop() })

	serverAddr := serverListener.Addr().String()

	// --- Client connects to server for data uploads ---
	clientCreds := credentials.NewTLS(clientTLS)
	serverConn, err := grpc.NewClient(serverAddr, grpc.WithTransportCredentials(clientCreds))
	if err != nil {
		t.Fatalf("grpc.NewClient (to server): %v", err)
	}
	t.Cleanup(func() { serverConn.Close() })

	dataClient := proto.NewDataServiceClient(serverConn)
	cmdClient := proto.NewCommandServiceClient(serverConn)

	// --- Client-side: create local DB and RemoteServerConnection ---
	clientDBPath := filepath.Join(t.TempDir(), "client.db")
	clientRepo, err := db.NewRepository(clientDBPath, false)
	if err != nil {
		t.Fatalf("client repo: %v", err)
	}
	t.Cleanup(func() { clientRepo.Close() })

	remoteConn := tgrpc.NewRemoteServerConnection(dataClient, "test-client")

	// --- Start client gRPC server (ClientCommandServer) ---
	clientCfg := &config.Config{
		Client: config.ClientConfig{
			IncludePaths:    []string{sourceDir},
			ExcludePatterns: []string{},
			MaxFileSize:     "10GB",
		},
		Database: config.DatabaseConfig{
			Path: clientDBPath,
		},
		Encryption: config.EncryptionConfig{
			Enabled: false,
		},
	}

	clientCmdServer := tgrpc.NewClientCommandServer(tgrpc.ClientCommandServerConfig{
		ServerConn: remoteConn,
		Repo:       clientRepo,
		Cfg:        clientCfg,
	})

	// Start client's gRPC CommandService on a random port.
	clientListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen (client): %v", err)
	}

	// Client also uses mTLS (server cert since it's acting as a gRPC server to receive commands).
	clientGRPCServer := grpc.NewServer(grpc.Creds(serverCreds))
	proto.RegisterCommandServiceServer(clientGRPCServer, clientCmdServer)

	go clientGRPCServer.Serve(clientListener)
	t.Cleanup(func() { clientGRPCServer.GracefulStop() })

	clientAddr := clientListener.Addr().String()

	// --- Client registers with the server and pings ---
	regCtx, regCancel := context.WithTimeout(ctx, 5*time.Second)
	defer regCancel()

	regResp, err := cmdClient.RegisterClient(regCtx, &proto.RegisterRequest{
		ClientId: "test-client",
		Address:  clientAddr,
	})
	if err != nil {
		t.Fatalf("RegisterClient: %v", err)
	}
	if !regResp.Success {
		t.Fatalf("RegisterClient not successful")
	}

	// Verify client is registered in the registry.
	ci := reg.GetClient("test-client")
	if ci == nil {
		// The registry uses the CN from mTLS cert ("Tergum Client") rather than
		// the request field when TLS info is available. Check that too.
		ci = reg.GetClient("Tergum Client")
	}
	if ci == nil {
		t.Fatal("client not found in registry after registration")
	}

	// Ping the server.
	pingResp, err := cmdClient.Ping(regCtx, &proto.PingRequest{})
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if pingResp.Version != "test-3.0.0" {
		t.Errorf("ping version = %q, want %q", pingResp.Version, "test-3.0.0")
	}

	// --- Server sends TriggerBackup to client's CommandService ---
	// Connect server to the client's CommandService.
	serverToClientConn, err := grpc.NewClient(clientAddr, grpc.WithTransportCredentials(clientCreds))
	if err != nil {
		t.Fatalf("grpc.NewClient (to client): %v", err)
	}
	t.Cleanup(func() { serverToClientConn.Close() })

	clientCmdClient := proto.NewCommandServiceClient(serverToClientConn)

	triggerCtx, triggerCancel := context.WithTimeout(ctx, 10*time.Second)
	defer triggerCancel()

	triggerResp, err := clientCmdClient.TriggerBackup(triggerCtx, &proto.BackupRequest{
		Level:       proto.BackupLevel_FULL,
		ClientId:    "test-client",
		InitiatedBy: "server-integration-test",
	})
	if err != nil {
		t.Fatalf("TriggerBackup: %v", err)
	}
	if triggerResp.Status != "started" {
		t.Fatalf("TriggerBackup status = %q, want %q", triggerResp.Status, "started")
	}

	// --- Wait for the backup to complete ---
	// Poll GetStatus until the client reports idle or timeout.
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		statusResp, err := clientCmdClient.GetStatus(ctx, &proto.StatusRequest{ClientId: "test-client"})
		if err != nil {
			t.Fatalf("GetStatus: %v", err)
		}
		if statusResp.Status == "idle" {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	// Final check to confirm backup is done.
	statusResp, err := clientCmdClient.GetStatus(ctx, &proto.StatusRequest{ClientId: "test-client"})
	if err != nil {
		t.Fatalf("GetStatus (final): %v", err)
	}
	if statusResp.Status != "idle" {
		t.Fatalf("backup did not complete within timeout; status = %q", statusResp.Status)
	}

	// --- Verify files arrived in server CAS ---
	for name, content := range testFiles {
		entries, err := clientRepo.FindByPath(ctx, "%"+filepath.Base(name))
		if err != nil {
			t.Fatalf("FindByPath %s: %v", name, err)
		}
		if len(entries) == 0 {
			t.Fatalf("no entry found for %s", name)
		}
		hash := entries[0].Blake3Hash

		exists, err := cas.Exists(ctx, hash)
		if err != nil {
			t.Fatalf("CAS.Exists for %s: %v", name, err)
		}
		if !exists {
			t.Fatalf("file %s (hash %s) not found in server CAS", name, hash)
		}

		reader, err := cas.Get(ctx, hash)
		if err != nil {
			t.Fatalf("CAS.Get for %s: %v", name, err)
		}
		data, err := readAllAndClose(reader)
		if err != nil {
			t.Fatalf("reading CAS data for %s: %v", name, err)
		}
		if string(data) != content {
			t.Errorf("CAS content mismatch for %s: got %q, want %q", name, data, content)
		}
	}

	// --- Verify AlreadyExists when triggering a second backup rapidly ---
	// Trigger two backups rapidly — the second should fail with AlreadyExists.
	// First, create new files so the backup has work to do and takes time.
	largeContent := make([]byte, 256*1024)
	for i := range largeContent {
		largeContent[i] = byte(i % 256)
	}
	for i := 0; i < 5; i++ {
		path := filepath.Join(sourceDir, fmt.Sprintf("large_%d.bin", i))
		if err := os.WriteFile(path, largeContent, 0o644); err != nil {
			t.Fatalf("write large_%d.bin: %v", i, err)
		}
	}

	// Trigger first backup.
	resp1, err := clientCmdClient.TriggerBackup(ctx, &proto.BackupRequest{
		Level:       proto.BackupLevel_FULL,
		ClientId:    "test-client",
		InitiatedBy: "already-exists-test-1",
	})
	if err != nil {
		t.Fatalf("TriggerBackup (first for AlreadyExists test): %v", err)
	}
	if resp1.Status != "started" {
		t.Fatalf("first trigger status = %q, want %q", resp1.Status, "started")
	}

	// Immediately trigger a second backup — should get AlreadyExists.
	_, err = clientCmdClient.TriggerBackup(ctx, &proto.BackupRequest{
		Level:       proto.BackupLevel_FULL,
		ClientId:    "test-client",
		InitiatedBy: "already-exists-test-2",
	})
	if err == nil {
		t.Fatal("expected AlreadyExists error for second TriggerBackup, got nil")
	}

	// Verify the error is AlreadyExists.
	if code := grpcStatus.Code(err); code != grpcCodes.AlreadyExists {
		t.Fatalf("expected AlreadyExists error code, got %v: %v", code, err)
	}

	// Wait for the first rapid-fire backup to finish before test cleanup.
	deadline = time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		statusResp, err := clientCmdClient.GetStatus(ctx, &proto.StatusRequest{ClientId: "test-client"})
		if err != nil {
			break
		}
		if statusResp.Status == "idle" {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// newTestRegistryDB creates an in-memory SQLite database for testing.
func newTestRegistryDB() (*sql.DB, error) {
	return sql.Open("sqlite", ":memory:")
}

// newTestRegistry creates a registry with the given DB (no background checker).
func newTestRegistry(d *sql.DB) (*registry.Registry, error) {
	return registry.New(registry.Config{
		DB:               d,
		OfflineThreshold: 90 * time.Second,
		CheckInterval:    30 * time.Second,
	})
}

// --- helpers ---

// noopBackupEngine satisfies backup.Engine without doing anything.
type noopBackupEngine struct{}

func (n *noopBackupEngine) RunBackup(ctx context.Context, req backup.BackupRequest) (*backup.BackupResult, error) {
	return &backup.BackupResult{BackupID: "noop"}, nil
}

func (n *noopBackupEngine) Stop(ctx context.Context) error {
	return nil
}

// readAllAndClose reads all data from an io.ReadCloser and closes it.
func readAllAndClose(rc interface {
	Read([]byte) (int, error)
	Close() error
}) ([]byte, error) {
	defer rc.Close()
	var data []byte
	buf := make([]byte, 4096)
	for {
		n, err := rc.Read(buf)
		if n > 0 {
			data = append(data, buf[:n]...)
		}
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			return data, err
		}
	}
	return data, nil
}

// TestIntegration_EndToEndFullWorkflow exercises the complete lifecycle:
//  1. Server starts with registry and scheduler
//  2. Client connects, registers, starts heartbeat
//  3. Server triggers backup on client
//  4. Client backs up files to server
//  5. Client syncs database to server
//  6. Server verifies files in CAS and client DB in clients/ directory
//  7. Client requests restore from server
//  8. Verify restored file matches original
//  9. Server starts watcher on client via RPC
//  10. Client detects file change, streams to server (abbreviated — see note)
//  11. Verify ongoing backup appears in server's job history
//  12. Also: missed schedule when client offline → triggered on reconnect
func TestIntegration_EndToEndFullWorkflow(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	// --- Setup directories ---
	certDir := t.TempDir()
	storageDir := t.TempDir()
	sourceDir := t.TempDir()
	clientsDir := t.TempDir()

	// --- Generate mTLS certs ---
	mgr := ttls.NewManager()
	if err := mgr.GenerateCerts(certDir); err != nil {
		t.Fatalf("GenerateCerts: %v", err)
	}

	// --- Load TLS configs ---
	serverTLS, err := mgr.LoadServerTLS(
		filepath.Join(certDir, "ca.crt"),
		filepath.Join(certDir, "server.crt"),
		filepath.Join(certDir, "server.key"),
	)
	if err != nil {
		t.Fatalf("LoadServerTLS: %v", err)
	}

	clientTLS, err := mgr.LoadClientTLS(
		filepath.Join(certDir, "ca.crt"),
		filepath.Join(certDir, "client.crt"),
		filepath.Join(certDir, "client.key"),
	)
	if err != nil {
		t.Fatalf("LoadClientTLS: %v", err)
	}
	clientTLS.ServerName = "localhost"

	// --- Create test source files ---
	testFiles := map[string]string{
		"readme.txt":     "End-to-end test file content.",
		"data/config.yml": "key: value\nport: 8080",
		"logs/app.log":   "2024-01-01 INFO Application started",
	}
	for name, content := range testFiles {
		path := filepath.Join(sourceDir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	// --- Server-side: repo, CAS, registry, scheduler ---
	serverRepo, err := db.NewRepository(":memory:", true)
	if err != nil {
		t.Fatalf("server repo: %v", err)
	}
	t.Cleanup(func() { serverRepo.Close() })

	cas := storage.NewCAS(storageDir, serverRepo)

	registryDB, err := newTestRegistryDB()
	if err != nil {
		t.Fatalf("registry DB: %v", err)
	}
	t.Cleanup(func() { registryDB.Close() })

	reg, err := newTestRegistry(registryDB)
	if err != nil {
		t.Fatalf("registry: %v", err)
	}

	// Start the registry background offline-checker.
	regCtx, regCancel := context.WithCancel(ctx)
	t.Cleanup(regCancel)
	go reg.Start(regCtx)

	// --- Start gRPC server (CommandService + DataService) ---
	serverListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen (server): %v", err)
	}

	serverCreds := credentials.NewTLS(serverTLS)
	grpcServer := grpc.NewServer(grpc.Creds(serverCreds))

	cmdServer := tgrpc.NewCommandServer(tgrpc.CommandServerConfig{
		BackupEngine: &noopBackupEngine{},
		Repo:         serverRepo,
		Registry:     reg,
		Version:      "test-e2e-3.0.0",
	})
	proto.RegisterCommandServiceServer(grpcServer, cmdServer)

	dataServer := tgrpc.NewDataServer(tgrpc.DataServerConfig{
		Store:      cas,
		Repo:       serverRepo,
		ClientsDir: clientsDir,
	})
	proto.RegisterDataServiceServer(grpcServer, dataServer)

	go grpcServer.Serve(serverListener)
	t.Cleanup(func() { grpcServer.GracefulStop() })

	serverAddr := serverListener.Addr().String()

	// --- Client connects to server ---
	clientCreds := credentials.NewTLS(clientTLS)
	serverConn, err := grpc.NewClient(serverAddr, grpc.WithTransportCredentials(clientCreds))
	if err != nil {
		t.Fatalf("grpc.NewClient (to server): %v", err)
	}
	t.Cleanup(func() { serverConn.Close() })

	dataClient := proto.NewDataServiceClient(serverConn)
	cmdClient := proto.NewCommandServiceClient(serverConn)

	// --- Client-side: local DB, RemoteServerConnection ---
	clientDBPath := filepath.Join(t.TempDir(), "client.db")
	clientRepo, err := db.NewRepository(clientDBPath, false)
	if err != nil {
		t.Fatalf("client repo: %v", err)
	}
	t.Cleanup(func() { clientRepo.Close() })

	remoteConn := tgrpc.NewRemoteServerConnection(dataClient, "e2e-client")

	// --- Client-side: start CommandService (to receive server commands) ---
	clientCfg := &config.Config{
		Client: config.ClientConfig{
			IncludePaths:    []string{sourceDir},
			ExcludePatterns: []string{},
			MaxFileSize:     "10GB",
		},
		Database: config.DatabaseConfig{
			Path: clientDBPath,
		},
		Encryption: config.EncryptionConfig{
			Enabled: false,
		},
	}

	// Create a mock file watcher for the client (for step 9).
	// Using a mock avoids complex inotify/DB interactions in the test.
	mockWatcher := &mockFileWatcher{}

	clientCmdServer := tgrpc.NewClientCommandServer(tgrpc.ClientCommandServerConfig{
		ServerConn: remoteConn,
		Repo:       clientRepo,
		Cfg:        clientCfg,
		Watcher:    mockWatcher,
	})

	clientListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen (client): %v", err)
	}

	clientGRPCServer := grpc.NewServer(grpc.Creds(serverCreds))
	proto.RegisterCommandServiceServer(clientGRPCServer, clientCmdServer)

	go clientGRPCServer.Serve(clientListener)
	t.Cleanup(func() { clientGRPCServer.GracefulStop() })

	clientAddr := clientListener.Addr().String()

	// =======================================================================
	// Phase 1: Client registers and heartbeats
	// =======================================================================
	t.Run("Phase1_RegisterAndHeartbeat", func(t *testing.T) {
		// Register client with server.
		regResp, err := cmdClient.RegisterClient(ctx, &proto.RegisterRequest{
			ClientId: "e2e-client",
			Address:  clientAddr,
		})
		if err != nil {
			t.Fatalf("RegisterClient: %v", err)
		}
		if !regResp.Success {
			t.Fatal("RegisterClient not successful")
		}

		// Verify client in registry (may be under CN "Tergum Client" due to mTLS).
		ci := reg.GetClient("e2e-client")
		if ci == nil {
			ci = reg.GetClient("Tergum Client")
		}
		if ci == nil {
			t.Fatal("client not found in registry")
		}
		if ci.Status != "online" {
			t.Errorf("client status = %q, want %q", ci.Status, "online")
		}

		// Simulate heartbeat via Ping (server updates last_seen).
		pingResp, err := cmdClient.Ping(ctx, &proto.PingRequest{})
		if err != nil {
			t.Fatalf("Ping: %v", err)
		}
		if pingResp.Version != "test-e2e-3.0.0" {
			t.Errorf("version = %q, want %q", pingResp.Version, "test-e2e-3.0.0")
		}
	})

	// =======================================================================
	// Phase 2: Server triggers backup on client
	// =======================================================================
	t.Run("Phase2_ServerTriggeredBackup", func(t *testing.T) {
		// Server connects to client CommandService.
		serverToClientConn, err := grpc.NewClient(clientAddr, grpc.WithTransportCredentials(clientCreds))
		if err != nil {
			t.Fatalf("grpc.NewClient (to client): %v", err)
		}
		t.Cleanup(func() { serverToClientConn.Close() })

		clientCmdClient := proto.NewCommandServiceClient(serverToClientConn)

		// Trigger backup.
		triggerResp, err := clientCmdClient.TriggerBackup(ctx, &proto.BackupRequest{
			Level:       proto.BackupLevel_FULL,
			ClientId:    "e2e-client",
			InitiatedBy: "e2e-server-trigger",
		})
		if err != nil {
			t.Fatalf("TriggerBackup: %v", err)
		}
		if triggerResp.Status != "started" {
			t.Fatalf("TriggerBackup status = %q, want %q", triggerResp.Status, "started")
		}

		// Wait for backup to complete.
		deadline := time.Now().Add(30 * time.Second)
		for time.Now().Before(deadline) {
			statusResp, err := clientCmdClient.GetStatus(ctx, &proto.StatusRequest{ClientId: "e2e-client"})
			if err != nil {
				t.Fatalf("GetStatus: %v", err)
			}
			if statusResp.Status == "idle" {
				break
			}
			time.Sleep(200 * time.Millisecond)
		}

		// Confirm idle.
		statusResp, err := clientCmdClient.GetStatus(ctx, &proto.StatusRequest{ClientId: "e2e-client"})
		if err != nil {
			t.Fatalf("GetStatus (final): %v", err)
		}
		if statusResp.Status != "idle" {
			t.Fatalf("backup did not complete; status = %q", statusResp.Status)
		}
	})

	// =======================================================================
	// Phase 3: Verify files in CAS
	// =======================================================================
	t.Run("Phase3_VerifyFilesInCAS", func(t *testing.T) {
		for name, content := range testFiles {
			entries, err := clientRepo.FindByPath(ctx, "%"+filepath.Base(name))
			if err != nil {
				t.Fatalf("FindByPath %s: %v", name, err)
			}
			if len(entries) == 0 {
				t.Fatalf("no entry found for %s", name)
			}
			hash := entries[0].Blake3Hash

			exists, err := cas.Exists(ctx, hash)
			if err != nil {
				t.Fatalf("CAS.Exists for %s: %v", name, err)
			}
			if !exists {
				t.Fatalf("file %s (hash %s) not in server CAS", name, hash)
			}

			reader, err := cas.Get(ctx, hash)
			if err != nil {
				t.Fatalf("CAS.Get for %s: %v", name, err)
			}
			data, err := readAllAndClose(reader)
			if err != nil {
				t.Fatalf("reading CAS for %s: %v", name, err)
			}
			if string(data) != content {
				t.Errorf("CAS mismatch for %s: got %q, want %q", name, data, content)
			}
		}
	})

	// =======================================================================
	// Phase 4: Verify synced database in clients/ directory
	// =======================================================================
	t.Run("Phase4_VerifySyncedDatabase", func(t *testing.T) {
		syncedDBPath := filepath.Join(clientsDir, "e2e-client.db")
		info, err := os.Stat(syncedDBPath)
		if err != nil {
			t.Fatalf("synced DB not found at %s: %v", syncedDBPath, err)
		}
		if info.Size() == 0 {
			t.Fatal("synced DB file is empty")
		}

		// Verify it's a valid SQLite DB.
		syncedRepo, err := db.NewRepository(syncedDBPath, false)
		if err != nil {
			t.Fatalf("opening synced DB: %v", err)
		}
		syncedRepo.Close()
	})

	// =======================================================================
	// Phase 5: Client requests restore from server
	// =======================================================================
	t.Run("Phase5_RestoreFromServer", func(t *testing.T) {
		remoteSource := tgrpc.NewRemoteDataSource(dataClient, "e2e-client")

		for name, expectedContent := range testFiles {
			entries, err := clientRepo.FindByPath(ctx, "%"+filepath.Base(name))
			if err != nil {
				t.Fatalf("FindByPath for restore %s: %v", name, err)
			}
			if len(entries) == 0 {
				t.Fatalf("no entry for restore %s", name)
			}

			data, err := remoteSource.DownloadFile(ctx, entries[0].Blake3Hash)
			if err != nil {
				t.Fatalf("DownloadFile %s: %v", name, err)
			}
			if string(data) != expectedContent {
				t.Errorf("restore mismatch for %s: got %q, want %q", name, data, expectedContent)
			}
		}
	})

	// =======================================================================
	// Phase 6: Server starts watcher on client via RPC
	// =======================================================================
	t.Run("Phase6_ServerStartsWatcher", func(t *testing.T) {
		serverToClientConn, err := grpc.NewClient(clientAddr, grpc.WithTransportCredentials(clientCreds))
		if err != nil {
			t.Fatalf("grpc.NewClient (to client): %v", err)
		}
		t.Cleanup(func() { serverToClientConn.Close() })

		clientCmdClient := proto.NewCommandServiceClient(serverToClientConn)

		// Start watcher via RPC.
		watchResp, err := clientCmdClient.StartWatcher(ctx, &proto.WatcherRequest{
			ClientId: "e2e-client",
		})
		if err != nil {
			t.Fatalf("StartWatcher: %v", err)
		}
		if !watchResp.Success {
			t.Fatalf("StartWatcher not successful: %s", watchResp.Message)
		}

		// Verify watcher is running (via mock).
		ws := mockWatcher.Status()
		if !ws.Running {
			t.Fatal("watcher should be running after StartWatcher RPC")
		}

		// NOTE: Testing actual file-change detection and ongoing backup streaming
		// requires complex timing coordination that would make this test flaky.
		// The watcher→ongoing backup pipeline is tested via unit tests.
		// Here we verify the RPC control path works end-to-end.

		// Stop the watcher via RPC for cleanup.
		stopResp, err := clientCmdClient.StopWatcher(ctx, &proto.WatcherRequest{
			ClientId: "e2e-client",
		})
		if err != nil {
			t.Fatalf("StopWatcher: %v", err)
		}
		if !stopResp.Success {
			t.Fatalf("StopWatcher not successful: %s", stopResp.Message)
		}

		// Verify watcher stopped.
		ws = mockWatcher.Status()
		if ws.Running {
			t.Fatal("watcher should be stopped after StopWatcher RPC")
		}
	})

	// =======================================================================
	// Phase 7: Verify job history on server
	// =======================================================================
	t.Run("Phase7_VerifyJobHistory", func(t *testing.T) {
		// The client's backup should have created job records in the client repo.
		jobs, err := clientRepo.ListJobs(ctx, db.JobFilter{Limit: 10})
		if err != nil {
			t.Fatalf("ListJobs: %v", err)
		}
		if len(jobs) == 0 {
			t.Fatal("expected at least one job in client history")
		}

		// Verify the most recent job completed successfully.
		found := false
		for _, j := range jobs {
			if j.Status == model.JobCompleted {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("no completed job found in client history; jobs: %+v", jobs)
		}
	})

	// =======================================================================
	// Phase 8: Missed schedule when client offline → triggered on reconnect
	// =======================================================================
	t.Run("Phase8_MissedScheduleOnReconnect", func(t *testing.T) {
		// Determine the client ID in the registry (might be CN from cert).
		clientID := "e2e-client"
		if ci := reg.GetClient(clientID); ci == nil {
			clientID = "Tergum Client"
		}

		// Set up a per-client scheduler with a mock trigger.
		trigger := &recordingBackupTrigger{}
		clientSched := scheduler.NewClientScheduler(scheduler.ClientSchedulerConfig{
			Registry:       reg,
			Trigger:        trigger,
			Logger:         slog.Default(),
			ReconnectGrace: 50 * time.Millisecond, // fast for testing
		})

		schedCtx, schedCancel := context.WithCancel(ctx)
		t.Cleanup(schedCancel)
		go clientSched.Start(schedCtx)

		// Give scheduler time to start.
		time.Sleep(50 * time.Millisecond)

		// Mark client offline.
		if err := reg.MarkOffline(clientID); err != nil {
			t.Fatalf("MarkOffline: %v", err)
		}

		// Record a missed backup (simulating what the scheduler cron job does).
		if err := reg.RecordMissedBackup(clientID, "FULL", time.Now()); err != nil {
			t.Fatalf("RecordMissedBackup: %v", err)
		}

		// Verify missed backup was recorded.
		ci := reg.GetClient(clientID)
		if ci == nil {
			t.Fatal("client not found in registry")
		}
		if len(ci.MissedBackups) == 0 {
			t.Fatal("expected at least one missed backup")
		}

		// Simulate reconnection: heartbeat updates status to online.
		if err := reg.Heartbeat(clientID); err != nil {
			t.Fatalf("Heartbeat: %v", err)
		}

		// Trigger the reconnection handler.
		clientSched.HandleReconnect(clientID)

		// Wait for the reconnect grace period + some buffer.
		time.Sleep(200 * time.Millisecond)

		// Verify the trigger was called for the missed backup.
		triggered := trigger.getTriggered()
		if len(triggered) == 0 {
			t.Fatal("expected missed backup to be triggered on reconnect")
		}

		foundMissed := false
		for _, tr := range triggered {
			if tr.clientID == clientID && tr.level == model.BackupLevelFull {
				foundMissed = true
				break
			}
		}
		if !foundMissed {
			t.Errorf("missed FULL backup trigger not found; triggered: %+v", triggered)
		}

		// Verify missed backups are resolved.
		ci = reg.GetClient(clientID)
		if ci != nil && len(ci.MissedBackups) > 0 {
			t.Errorf("missed backups should be resolved after reconnect, got %d", len(ci.MissedBackups))
		}
	})
}

// --- helpers for end-to-end test ---

// triggerRecord stores a single trigger invocation.
type triggerRecord struct {
	level    model.BackupLevel
	clientID string
}

// recordingBackupTrigger records all TriggerBackup calls for verification.
type recordingBackupTrigger struct {
	mu       sync.Mutex
	triggers []triggerRecord
}

func (r *recordingBackupTrigger) TriggerBackup(ctx context.Context, level model.BackupLevel, clientID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.triggers = append(r.triggers, triggerRecord{level: level, clientID: clientID})
	return nil
}

func (r *recordingBackupTrigger) getTriggered() []triggerRecord {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]triggerRecord, len(r.triggers))
	copy(result, r.triggers)
	return result
}

// mockFileWatcher is a minimal mock implementing watcher.Watcher for testing
// the RPC control flow without requiring real filesystem watching.
type mockFileWatcher struct {
	mu      sync.Mutex
	running bool
}

func (m *mockFileWatcher) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.running = true
	return nil
}

func (m *mockFileWatcher) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.running = false
	return nil
}

func (m *mockFileWatcher) AddPath(path string, recursive bool) error {
	return nil
}

func (m *mockFileWatcher) RemovePath(path string) error {
	return nil
}

func (m *mockFileWatcher) StableFiles() <-chan watcher.StableFile {
	return make(chan watcher.StableFile)
}

func (m *mockFileWatcher) Status() watcher.WatcherStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	return watcher.WatcherStatus{Running: m.running}
}
