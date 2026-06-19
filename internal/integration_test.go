// Package internal_test contains integration tests that exercise multiple packages together.
// These tests verify end-to-end flows using local implementations (no real network).
package internal_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ricardopadilha/tergum/internal/backup"
	"github.com/ricardopadilha/tergum/internal/crypto"
	"github.com/ricardopadilha/tergum/internal/db"
	"github.com/ricardopadilha/tergum/internal/model"
	"github.com/ricardopadilha/tergum/internal/restore"
	"github.com/ricardopadilha/tergum/internal/retention"
	"github.com/ricardopadilha/tergum/internal/storage"
	ttls "github.com/ricardopadilha/tergum/internal/tls"
)

// --- helpers ---

// setupIntegrationEnv creates a full integration environment with in-memory DB,
// local CAS storage, and configured engines.
func setupIntegrationEnv(t *testing.T, encryptionOn bool) *integrationEnv {
	t.Helper()

	storageDir := t.TempDir()
	sourceDir := t.TempDir()
	restoreDir := t.TempDir()

	repo, err := db.NewRepository(":memory:", false)
	if err != nil {
		t.Fatalf("failed to create repository: %v", err)
	}
	t.Cleanup(func() { repo.Close() })

	var masterKey []byte
	var encryptor *crypto.AESEncryptor
	if encryptionOn {
		masterKey = make([]byte, 32)
		for i := range masterKey {
			masterKey[i] = byte(i + 42)
		}
		encryptor = crypto.NewEncryptor()
	}

	serverConn := &backup.LocalServerConnection{
		StorageDir: storageDir,
		Repo:       repo,
	}

	backupCfg := backup.EngineConfig{
		IncludePaths:    []string{sourceDir},
		ExcludePatterns: []string{"*.tmp"},
		MaxFileSize:     10 * 1024 * 1024,
		EncryptionOn:    encryptionOn,
		MasterKey:       masterKey,
	}

	backupEngine := backup.NewBackupEngine(serverConn, repo, encryptor, backupCfg)

	dataSource := &restore.LocalDataSource{StorageDir: storageDir}
	restoreEngine := restore.NewRestoreEngine(dataSource, repo, encryptor, masterKey)

	return &integrationEnv{
		StorageDir:    storageDir,
		SourceDir:     sourceDir,
		RestoreDir:    restoreDir,
		Repo:          repo,
		BackupEngine:  backupEngine,
		RestoreEngine: restoreEngine,
		Encryptor:     encryptor,
		MasterKey:     masterKey,
		ServerConn:    serverConn,
	}
}

type integrationEnv struct {
	StorageDir    string
	SourceDir     string
	RestoreDir    string
	Repo          *db.SQLiteRepository
	BackupEngine  *backup.BackupEngine
	RestoreEngine *restore.RestoreEngine
	Encryptor     *crypto.AESEncryptor
	MasterKey     []byte
	ServerConn    *backup.LocalServerConnection
}

func (env *integrationEnv) createFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(env.SourceDir, name)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	return path
}

// --- integration tests ---

// TestIntegration_BackupAndRestore exercises the full backup → restore pipeline without encryption.
// Flow: create files → backup (scan → manifest → upload → DB sync) → restore → verify.
func TestIntegration_BackupAndRestore(t *testing.T) {
	env := setupIntegrationEnv(t, false)
	ctx := context.Background()

	// Create source files.
	files := map[string]string{
		"doc.txt":        "Hello, this is a document.",
		"data/notes.md":  "# Notes\nImportant stuff here.",
		"config/app.cfg": "key=value\nfoo=bar",
	}
	for name, content := range files {
		env.createFile(t, name, content)
	}

	// Run backup.
	result, err := env.BackupEngine.RunBackup(ctx, backup.BackupRequest{
		Level:       model.BackupLevelFull,
		ClientID:    "integration-client",
		InitiatedBy: "integration-test",
	})
	if err != nil {
		t.Fatalf("RunBackup failed: %v", err)
	}
	if result.Status != model.JobCompleted {
		t.Fatalf("expected completed, got %s", result.Status)
	}
	if result.FilesProcessed != 3 {
		t.Fatalf("expected 3 files processed, got %d", result.FilesProcessed)
	}

	// Verify job recorded.
	jobs, err := env.Repo.ListJobs(ctx, db.JobFilter{Limit: 10})
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if len(jobs) != 1 || jobs[0].Status != model.JobCompleted {
		t.Fatalf("expected 1 completed job, got %d", len(jobs))
	}

	// Restore each file and verify content matches originals.
	for name, expectedContent := range files {
		// Find the entry by path pattern.
		entries, err := env.Repo.FindByPath(ctx, "%"+filepath.Base(name))
		if err != nil {
			t.Fatalf("FindByPath for %s: %v", name, err)
		}
		if len(entries) == 0 {
			t.Fatalf("no entry found for %s", name)
		}

		destPath := filepath.Join(env.RestoreDir, name)
		err = env.RestoreEngine.RestoreFile(ctx, entries[0].Blake3Hash, destPath)
		if err != nil {
			t.Fatalf("RestoreFile %s: %v", name, err)
		}

		restored, err := os.ReadFile(destPath)
		if err != nil {
			t.Fatalf("read restored %s: %v", name, err)
		}
		if string(restored) != expectedContent {
			t.Errorf("content mismatch for %s: got %q, want %q", name, restored, expectedContent)
		}
	}
}

// TestIntegration_BackupWithEncryptionAndRestore tests the full encrypted pipeline:
// encrypt content → store in CAS → populate DB with encryption metadata → restore → decrypt → verify.
// This exercises the encryption module, CAS storage, DB, and restore engine together.
func TestIntegration_BackupWithEncryptionAndRestore(t *testing.T) {
	storageDir := t.TempDir()
	restoreDir := t.TempDir()

	repo, err := db.NewRepository(":memory:", false)
	if err != nil {
		t.Fatalf("create repo: %v", err)
	}
	t.Cleanup(func() { repo.Close() })

	masterKey := make([]byte, 32)
	for i := range masterKey {
		masterKey[i] = byte(i + 42)
	}
	encryptor := crypto.NewEncryptor()

	ctx := context.Background()

	// Create a backup job.
	backupID := "encrypted-job-001"
	job := model.BackupJob{
		BackupID:    backupID,
		Level:       "FULL",
		ClientID:    "encrypted-client",
		InitiatedBy: "integration-test",
		StartedAt:   time.Now().UTC(),
		Status:      model.JobCompleted,
	}
	if err := repo.CreateJob(ctx, job); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	// Simulate the full encrypted backup pipeline for multiple files.
	files := map[string]string{
		"secret.txt": "Top secret information that must be encrypted.",
		"report.md":  "## Confidential Report\nSensitive data here.",
	}

	for name, plaintext := range files {
		content := []byte(plaintext)
		hash := crypto.HashBytes(content)

		// Encrypt (simulates what the backup engine does during upload).
		ciphertext, wrappedDEK, nonce, err := encryptor.Encrypt(content, masterKey)
		if err != nil {
			t.Fatalf("encrypt %s: %v", name, err)
		}

		// Store encrypted content in CAS under the plaintext's BLAKE3 hash.
		dir := filepath.Join(storageDir, hash[:2])
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, hash), ciphertext, 0o644); err != nil {
			t.Fatalf("write CAS: %v", err)
		}

		// Insert DB entry with encryption metadata.
		entry := model.BackupEntry{
			BackupID:     backupID,
			Blake3Hash:   hash,
			FileName:     name,
			FilePath:     "/encrypted/" + name,
			FileSize:     int64(len(content)),
			OS:           "test",
			EncryptedDEK: wrappedDEK,
			Nonce:        nonce,
			BackupDate:   time.Now().UTC(),
		}
		if err := repo.InsertBackupEntry(ctx, entry); err != nil {
			t.Fatalf("insert entry %s: %v", name, err)
		}

		// Verify CAS content is NOT plaintext.
		raw, _ := os.ReadFile(filepath.Join(dir, hash))
		if string(raw) == plaintext {
			t.Errorf("file %s stored as plaintext", name)
		}
		if len(raw) <= len(content) {
			t.Errorf("encrypted %s should be larger than plaintext", name)
		}
	}

	// Restore with decryption and verify round-trip.
	dataSource := &restore.LocalDataSource{StorageDir: storageDir}
	restoreEngine := restore.NewRestoreEngine(dataSource, repo, encryptor, masterKey)

	for name, expectedContent := range files {
		hash := crypto.HashBytes([]byte(expectedContent))
		destPath := filepath.Join(restoreDir, name)

		if err := restoreEngine.RestoreFile(ctx, hash, destPath); err != nil {
			t.Fatalf("RestoreFile %s: %v", name, err)
		}

		restored, err := os.ReadFile(destPath)
		if err != nil {
			t.Fatalf("read restored %s: %v", name, err)
		}
		if string(restored) != expectedContent {
			t.Errorf("decrypted content mismatch for %s: got %q, want %q", name, restored, expectedContent)
		}
	}
}

// TestIntegration_RetentionProtectsLatestVersion creates multiple backup versions,
// applies an aggressive retention policy, and verifies the latest version survives.
func TestIntegration_RetentionProtectsLatestVersion(t *testing.T) {
	// Use a server-mode repo for retention policies.
	storageDir := t.TempDir()
	repo, err := db.NewRepository(":memory:", true)
	if err != nil {
		t.Fatalf("create repo: %v", err)
	}
	t.Cleanup(func() { repo.Close() })

	cas := storage.NewCAS(storageDir, repo)
	retEngine := retention.New(repo, cas)
	ctx := context.Background()

	// Create 4 backup versions of the same file at different dates.
	now := time.Now().UTC()
	versions := []struct {
		jobID      string
		hash       string
		backupDate time.Time
		content    string
	}{
		{"job-v1", "", now.Add(-90 * 24 * time.Hour), "version 1 content"},
		{"job-v2", "", now.Add(-60 * 24 * time.Hour), "version 2 content updated"},
		{"job-v3", "", now.Add(-30 * 24 * time.Hour), "version 3 more changes"},
		{"job-v4", "", now.Add(-1 * time.Hour), "version 4 latest content"},
	}

	// Compute hashes and store.
	for i := range versions {
		versions[i].hash = crypto.HashBytes([]byte(versions[i].content))

		// Create job.
		job := model.BackupJob{
			BackupID:    versions[i].jobID,
			Level:       "FULL",
			ClientID:    "retention-test-client",
			InitiatedBy: "test",
			StartedAt:   versions[i].backupDate,
			Status:      model.JobCompleted,
		}
		if err := repo.CreateJob(ctx, job); err != nil {
			t.Fatalf("create job %s: %v", versions[i].jobID, err)
		}

		// Insert backup entry.
		entry := model.BackupEntry{
			BackupID:   versions[i].jobID,
			Blake3Hash: versions[i].hash,
			FileName:   "document.txt",
			FilePath:   "/docs/document.txt",
			FileSize:   int64(len(versions[i].content)),
			OS:         "linux",
			BackupDate: versions[i].backupDate,
		}
		if err := repo.InsertBackupEntry(ctx, entry); err != nil {
			t.Fatalf("insert entry %s: %v", versions[i].jobID, err)
		}

		// Store in CAS.
		if err := cas.Put(ctx, versions[i].hash, strings.NewReader(versions[i].content)); err != nil {
			t.Fatalf("store in CAS %s: %v", versions[i].jobID, err)
		}
	}

	// Add aggressive retention policy: keep 7 days, keep 1 version.
	keepDays := 7
	err = retEngine.AddPolicy(ctx, model.RetentionPolicy{
		Name:         "aggressive-cleanup",
		KeepDays:     &keepDays,
		KeepVersions: 1,
		Pattern:      "*",
		Priority:     10,
		Enabled:      true,
	})
	if err != nil {
		t.Fatalf("AddPolicy: %v", err)
	}

	retEngine.SetClock(func() time.Time { return now })

	// Run retention.
	result, err := retEngine.Evaluate(ctx, false)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	// Versions 1, 2, 3 are older than 7 days and exceed keep_versions(1) → expired.
	if result.EntriesExpired != 3 {
		t.Errorf("expected 3 expired, got %d", result.EntriesExpired)
	}
	if result.Protected != 1 {
		t.Errorf("expected 1 protected (latest), got %d", result.Protected)
	}

	// Verify latest version still exists in DB.
	remaining, err := repo.GetFileVersions(ctx, "/docs/document.txt")
	if err != nil {
		t.Fatalf("GetFileVersions: %v", err)
	}
	if len(remaining) != 1 {
		t.Fatalf("expected 1 remaining version, got %d", len(remaining))
	}
	if remaining[0].Blake3Hash != versions[3].hash {
		t.Errorf("remaining version should be the latest, got hash %s", remaining[0].Blake3Hash)
	}

	// Verify latest version's content is still in CAS.
	exists, err := cas.Exists(ctx, versions[3].hash)
	if err != nil {
		t.Fatalf("CAS Exists: %v", err)
	}
	if !exists {
		t.Error("latest version's CAS file should still exist")
	}
}

// TestIntegration_MutualTLSHandshake proves the full mTLS flow:
// generate certs → load configs → server/client handshake → data exchange.
// Also verifies that an invalid client (no cert) is rejected.
func TestIntegration_MutualTLSHandshake(t *testing.T) {
	certDir := t.TempDir()
	mgr := ttls.NewManager()

	// Generate CA, server, and client certs.
	if err := mgr.GenerateCerts(certDir); err != nil {
		t.Fatalf("GenerateCerts: %v", err)
	}

	// Load server TLS config.
	serverCfg, err := mgr.LoadServerTLS(
		filepath.Join(certDir, "ca.crt"),
		filepath.Join(certDir, "server.crt"),
		filepath.Join(certDir, "server.key"),
	)
	if err != nil {
		t.Fatalf("LoadServerTLS: %v", err)
	}

	// Load client TLS config.
	clientCfg, err := mgr.LoadClientTLS(
		filepath.Join(certDir, "ca.crt"),
		filepath.Join(certDir, "client.crt"),
		filepath.Join(certDir, "client.key"),
	)
	if err != nil {
		t.Fatalf("LoadClientTLS: %v", err)
	}

	// Start TLS server.
	listener, err := tls.Listen("tcp", "127.0.0.1:0", serverCfg)
	if err != nil {
		t.Fatalf("tls.Listen: %v", err)
	}
	defer listener.Close()
	addr := listener.Addr().String()

	serverDone := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			serverDone <- err
			return
		}
		defer conn.Close()

		tlsConn := conn.(*tls.Conn)
		if err := tlsConn.Handshake(); err != nil {
			serverDone <- fmt.Errorf("server handshake: %w", err)
			return
		}

		// Verify peer certificate is present.
		state := tlsConn.ConnectionState()
		if len(state.PeerCertificates) == 0 {
			serverDone <- fmt.Errorf("no peer certificate presented")
			return
		}

		// Echo data to prove bidirectional communication.
		buf := make([]byte, 256)
		n, err := conn.Read(buf)
		if err != nil {
			serverDone <- fmt.Errorf("server read: %w", err)
			return
		}
		if _, err := conn.Write(buf[:n]); err != nil {
			serverDone <- fmt.Errorf("server write: %w", err)
			return
		}
		serverDone <- nil
	}()

	// Connect valid client.
	clientCfg.ServerName = "localhost"
	conn, err := tls.Dial("tcp", addr, clientCfg)
	if err != nil {
		t.Fatalf("client dial: %v", err)
	}
	defer conn.Close()

	// Send message and verify echo.
	msg := []byte("integration test mTLS payload")
	if _, err := conn.Write(msg); err != nil {
		t.Fatalf("client write: %v", err)
	}

	buf := make([]byte, 256)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("client read: %v", err)
	}
	if string(buf[:n]) != string(msg) {
		t.Errorf("echo mismatch: got %q, want %q", buf[:n], msg)
	}

	if err := <-serverDone; err != nil {
		t.Fatalf("server: %v", err)
	}

	// --- Test invalid client (no certificate) is rejected ---
	listener2, err := tls.Listen("tcp", "127.0.0.1:0", serverCfg)
	if err != nil {
		t.Fatalf("tls.Listen (2): %v", err)
	}
	defer listener2.Close()
	addr2 := listener2.Addr().String()

	go func() {
		conn, err := listener2.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		tlsConn := conn.(*tls.Conn)
		_ = tlsConn.Handshake() // Expected to fail
	}()

	// Client without certificate.
	caData, err := os.ReadFile(filepath.Join(certDir, "ca.crt"))
	if err != nil {
		t.Fatalf("read CA: %v", err)
	}
	caPool := x509.NewCertPool()
	caPool.AppendCertsFromPEM(caData)

	invalidCfg := &tls.Config{
		RootCAs:    caPool,
		ServerName: "localhost",
		MinVersion: tls.VersionTLS13,
		// No Certificates — should be rejected by mTLS server.
	}

	invalidConn, err := tls.Dial("tcp", addr2, invalidCfg)
	if err != nil {
		// Connection rejected at dial — expected for mTLS.
		return
	}
	defer invalidConn.Close()

	// If dial succeeded, the handshake or I/O should fail.
	if _, err := invalidConn.Write([]byte("test")); err != nil {
		return // Expected failure
	}
	readBuf := make([]byte, 64)
	if _, err := invalidConn.Read(readBuf); err != nil {
		return // Expected failure
	}
	t.Error("expected connection without client cert to be rejected")
}

// TestIntegration_DedupAcrossBackups verifies that running two backups of the
// same files results in only one physical copy in CAS storage.
func TestIntegration_DedupAcrossBackups(t *testing.T) {
	env := setupIntegrationEnv(t, false)
	ctx := context.Background()

	// Create source files.
	env.createFile(t, "stable.txt", "this content stays the same across backups")
	env.createFile(t, "other.dat", "another file with different content")

	// First backup.
	result1, err := env.BackupEngine.RunBackup(ctx, backup.BackupRequest{
		Level:       model.BackupLevelFull,
		ClientID:    "dedup-client",
		InitiatedBy: "test",
	})
	if err != nil {
		t.Fatalf("first backup: %v", err)
	}
	if result1.Status != model.JobCompleted {
		t.Fatalf("first backup status: %s", result1.Status)
	}
	if result1.FilesProcessed != 2 {
		t.Fatalf("expected 2 files in first backup, got %d", result1.FilesProcessed)
	}
	if result1.BytesNew == 0 {
		t.Fatal("first backup should have new bytes")
	}

	// Count physical files after first backup.
	physFiles1 := countPhysicalFiles(t, env.StorageDir)

	// Second backup — same files, same content.
	result2, err := env.BackupEngine.RunBackup(ctx, backup.BackupRequest{
		Level:       model.BackupLevelFull,
		ClientID:    "dedup-client",
		InitiatedBy: "test",
	})
	if err != nil {
		t.Fatalf("second backup: %v", err)
	}
	if result2.Status != model.JobCompleted {
		t.Fatalf("second backup status: %s", result2.Status)
	}

	// All files should be deduplicated in the second backup.
	if result2.FilesDeduped != 2 {
		t.Errorf("expected 2 deduped in second backup, got %d", result2.FilesDeduped)
	}
	if result2.BytesNew != 0 {
		t.Errorf("expected 0 new bytes in second backup, got %d", result2.BytesNew)
	}

	// Physical file count should not have changed.
	physFiles2 := countPhysicalFiles(t, env.StorageDir)
	if physFiles2 != physFiles1 {
		t.Errorf("physical file count changed: %d → %d (expected dedup)", physFiles1, physFiles2)
	}
	if physFiles1 != 2 {
		t.Errorf("expected 2 physical files (one per unique hash), got %d", physFiles1)
	}
}

// TestIntegration_BackupStopGraceful starts a backup with multiple files,
// stops it mid-way, and verifies the job status is "stopped" with partial entries.
func TestIntegration_BackupStopGraceful(t *testing.T) {
	storageDir := t.TempDir()
	sourceDir := t.TempDir()

	repo, err := db.NewRepository(":memory:", false)
	if err != nil {
		t.Fatalf("create repo: %v", err)
	}
	t.Cleanup(func() { repo.Close() })

	// Create many files so the backup has work to do.
	for i := 0; i < 20; i++ {
		content := fmt.Sprintf("file %d content with unique data %d", i, i*7+13)
		path := filepath.Join(sourceDir, fmt.Sprintf("file_%03d.txt", i))
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("create file %d: %v", i, err)
		}
	}

	// Use a blocking server connection that triggers Stop after a few uploads.
	blockingServer := &stoppingServerConnection{
		storageDir: storageDir,
		stopAfter:  3, // Stop after 3 uploads
	}

	cfg := backup.EngineConfig{
		IncludePaths: []string{sourceDir},
		MaxFileSize:  10 * 1024 * 1024,
	}

	engine := backup.NewBackupEngine(blockingServer, repo, nil, cfg)
	blockingServer.engine = engine

	ctx := context.Background()
	result, err := engine.RunBackup(ctx, backup.BackupRequest{
		Level:       model.BackupLevelFull,
		ClientID:    "stop-test-client",
		InitiatedBy: "test",
	})
	if err != nil {
		t.Fatalf("RunBackup: %v", err)
	}

	// Verify job is stopped.
	if result.Status != model.JobStopped {
		t.Errorf("expected status 'stopped', got %q", result.Status)
	}

	// Verify job status in DB.
	jobs, err := repo.ListJobs(ctx, db.JobFilter{Limit: 10})
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	if jobs[0].Status != model.JobStopped {
		t.Errorf("DB job status: expected 'stopped', got %q", jobs[0].Status)
	}

	// Verify partial uploads exist (some files were uploaded before stop).
	physFiles := countPhysicalFiles(t, storageDir)
	if physFiles == 0 {
		t.Error("expected some files to be uploaded before stop")
	}
	if physFiles >= 20 {
		t.Errorf("expected partial upload (< 20 files), got %d", physFiles)
	}
}

// --- helper types and functions ---

// stoppingServerConnection triggers Stop after a certain number of uploads.
type stoppingServerConnection struct {
	storageDir  string
	engine      *backup.BackupEngine
	stopAfter   int
	uploadCount int
}

func (s *stoppingServerConnection) ExchangeManifest(ctx context.Context, manifest []model.ManifestEntry) (backup.ManifestDiff, error) {
	local := &backup.LocalServerConnection{StorageDir: s.storageDir}
	return local.ExchangeManifest(ctx, manifest)
}

func (s *stoppingServerConnection) UploadFile(ctx context.Context, hash string, data []byte, wrappedDEK []byte, nonce []byte, entry model.BackupEntry) error {
	local := &backup.LocalServerConnection{StorageDir: s.storageDir}
	if err := local.UploadFile(ctx, hash, data, wrappedDEK, nonce, entry); err != nil {
		return err
	}
	s.uploadCount++
	if s.uploadCount >= s.stopAfter {
		_ = s.engine.Stop(ctx)
	}
	return nil
}

func (s *stoppingServerConnection) SyncDatabase(ctx context.Context, dbPath string) error {
	return nil
}

// countPhysicalFiles counts all regular files in a directory tree.
func countPhysicalFiles(t *testing.T, dir string) int {
	t.Helper()
	count := 0
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			count++
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	return count
}
