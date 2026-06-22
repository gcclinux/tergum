package backup

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/gcclinux/tergum/internal/crypto"
	"github.com/gcclinux/tergum/internal/db"
	"github.com/gcclinux/tergum/internal/model"
)

// setupTestEngine creates a BackupEngine with a local server connection and in-memory DB.
func setupTestEngine(t *testing.T, encryptionOn bool) (*BackupEngine, *db.SQLiteRepository, string, string) {
	t.Helper()

	// Create temp storage directory.
	storageDir := t.TempDir()

	// Create in-memory repository.
	repo, err := db.NewRepository(":memory:", false)
	if err != nil {
		t.Fatalf("failed to create repository: %v", err)
	}
	t.Cleanup(func() { repo.Close() })

	// Create local server connection.
	server := &LocalServerConnection{
		StorageDir: storageDir,
		Repo:       repo,
	}

	// Create temp directory with test files.
	sourceDir := t.TempDir()

	var masterKey []byte
	var encryptor *crypto.AESEncryptor
	if encryptionOn {
		masterKey = make([]byte, 32)
		for i := range masterKey {
			masterKey[i] = byte(i)
		}
		encryptor = crypto.NewEncryptor()
	}

	cfg := EngineConfig{
		IncludePaths:    []string{sourceDir},
		ExcludePatterns: []string{"*.tmp"},
		MaxFileSize:     10 * 1024 * 1024, // 10MB
		EncryptionOn:    encryptionOn,
		MasterKey:       masterKey,
	}

	engine := NewBackupEngine(server, repo, encryptor, cfg)
	return engine, repo, storageDir, sourceDir
}

// createTestFile creates a file with the given content in the source directory.
func createTestFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}
	return path
}

func TestRunBackup_EmptyDirectory(t *testing.T) {
	engine, repo, _, _ := setupTestEngine(t, false)

	result, err := engine.RunBackup(context.Background(), BackupRequest{
		Level:       model.BackupLevelFull,
		ClientID:    "test-client",
		InitiatedBy: "test",
	})
	if err != nil {
		t.Fatalf("RunBackup failed: %v", err)
	}

	if result.BackupID == "" {
		t.Error("expected non-empty BackupID")
	}
	if result.Status != model.JobCompleted {
		t.Errorf("expected status %q, got %q", model.JobCompleted, result.Status)
	}
	if result.FilesProcessed != 0 {
		t.Errorf("expected 0 files processed, got %d", result.FilesProcessed)
	}

	// Verify job was created in DB.
	jobs, err := repo.ListJobs(context.Background(), db.JobFilter{Limit: 10})
	if err != nil {
		t.Fatalf("failed to list jobs: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	if jobs[0].Status != model.JobCompleted {
		t.Errorf("expected job status %q, got %q", model.JobCompleted, jobs[0].Status)
	}
}

func TestRunBackup_WithFiles_NoEncryption(t *testing.T) {
	engine, repo, storageDir, sourceDir := setupTestEngine(t, false)

	// Create test files.
	createTestFile(t, sourceDir, "file1.txt", "hello world")
	createTestFile(t, sourceDir, "file2.txt", "another file")
	createTestFile(t, sourceDir, "skip.tmp", "should be excluded")

	result, err := engine.RunBackup(context.Background(), BackupRequest{
		Level:       model.BackupLevelFull,
		ClientID:    "test-client",
		InitiatedBy: "test",
	})
	if err != nil {
		t.Fatalf("RunBackup failed: %v", err)
	}

	if result.Status != model.JobCompleted {
		t.Errorf("expected status %q, got %q", model.JobCompleted, result.Status)
	}

	// Should have processed 2 files (skip.tmp excluded by pattern).
	if result.FilesProcessed != 2 {
		t.Errorf("expected 2 files processed, got %d", result.FilesProcessed)
	}

	// All files are new, so BytesNew should be > 0.
	if result.BytesNew <= 0 {
		t.Errorf("expected BytesNew > 0, got %d", result.BytesNew)
	}

	// Verify files were stored in CAS.
	entries, err := findStoredFiles(storageDir)
	if err != nil {
		t.Fatalf("failed to scan storage: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("expected 2 stored files, got %d", len(entries))
	}

	// Verify backup entries in DB.
	jobs, _ := repo.ListJobs(context.Background(), db.JobFilter{Limit: 10})
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
}

func TestRunBackup_WithEncryption(t *testing.T) {
	engine, _, storageDir, sourceDir := setupTestEngine(t, true)

	content := "secret data to encrypt"
	createTestFile(t, sourceDir, "secret.txt", content)

	result, err := engine.RunBackup(context.Background(), BackupRequest{
		Level:       model.BackupLevelFull,
		ClientID:    "test-client",
		InitiatedBy: "test",
	})
	if err != nil {
		t.Fatalf("RunBackup failed: %v", err)
	}

	if result.Status != model.JobCompleted {
		t.Errorf("expected completed, got %q", result.Status)
	}
	if result.FilesProcessed != 1 {
		t.Errorf("expected 1 file processed, got %d", result.FilesProcessed)
	}

	// Verify stored file is NOT the same as original content (it's encrypted).
	entries, err := findStoredFiles(storageDir)
	if err != nil {
		t.Fatalf("failed to scan storage: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 stored file, got %d", len(entries))
	}

	stored, err := os.ReadFile(entries[0])
	if err != nil {
		t.Fatalf("failed to read stored file: %v", err)
	}

	// Encrypted data should be different from plaintext.
	if string(stored) == content {
		t.Error("stored file should be encrypted, but matches plaintext")
	}

	// Encrypted data should be longer than plaintext (nonce + tag overhead).
	if len(stored) <= len(content) {
		t.Errorf("encrypted data should be longer than plaintext: got %d, original %d", len(stored), len(content))
	}
}

func TestRunBackup_Deduplication(t *testing.T) {
	engine, _, storageDir, sourceDir := setupTestEngine(t, false)

	// Create two files with the same content — one hash, two manifest entries.
	sameContent := "duplicate content for dedup test"
	createTestFile(t, sourceDir, "dup1.txt", sameContent)
	createTestFile(t, sourceDir, "dup2.txt", sameContent)

	result, err := engine.RunBackup(context.Background(), BackupRequest{
		Level:       model.BackupLevelFull,
		ClientID:    "test-client",
		InitiatedBy: "test",
	})
	if err != nil {
		t.Fatalf("RunBackup failed: %v", err)
	}

	if result.Status != model.JobCompleted {
		t.Errorf("expected completed, got %q", result.Status)
	}

	// Both files processed, but only 1 unique hash uploaded.
	if result.FilesProcessed != 2 {
		t.Errorf("expected 2 files processed, got %d", result.FilesProcessed)
	}

	// The second file with the same hash goes through the dedup path.
	// NeededHashes will have 1 unique hash, so 1 upload + 1 dedup.
	if result.FilesDeduped != 1 {
		t.Errorf("expected 1 file deduped, got %d", result.FilesDeduped)
	}

	// Only 1 physical file in storage (since both have same hash).
	entries, err := findStoredFiles(storageDir)
	if err != nil {
		t.Fatalf("failed to scan storage: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 physical file in storage, got %d", len(entries))
	}
}

func TestRunBackup_SecondBackupDedup(t *testing.T) {
	engine, _, storageDir, sourceDir := setupTestEngine(t, false)

	createTestFile(t, sourceDir, "file.txt", "persistent data")

	// First backup: file is new.
	result1, err := engine.RunBackup(context.Background(), BackupRequest{
		Level:       model.BackupLevelFull,
		ClientID:    "test-client",
		InitiatedBy: "test",
	})
	if err != nil {
		t.Fatalf("first RunBackup failed: %v", err)
	}
	if result1.BytesNew == 0 {
		t.Error("first backup should have new bytes")
	}

	// Second backup: same file, should be deduplicated.
	result2, err := engine.RunBackup(context.Background(), BackupRequest{
		Level:       model.BackupLevelFull,
		ClientID:    "test-client",
		InitiatedBy: "test",
	})
	if err != nil {
		t.Fatalf("second RunBackup failed: %v", err)
	}

	// File already exists on server, so it should be deduped.
	if result2.FilesDeduped != 1 {
		t.Errorf("expected 1 file deduped in second backup, got %d", result2.FilesDeduped)
	}
	if result2.BytesNew != 0 {
		t.Errorf("expected 0 new bytes in second backup, got %d", result2.BytesNew)
	}

	// Still only 1 physical file.
	entries, err := findStoredFiles(storageDir)
	if err != nil {
		t.Fatalf("failed to scan storage: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 physical file, got %d", len(entries))
	}
}

func TestStop_DuringBackup(t *testing.T) {
	// Create in-memory repository.
	repo, err := db.NewRepository(":memory:", false)
	if err != nil {
		t.Fatalf("failed to create repository: %v", err)
	}
	t.Cleanup(func() { repo.Close() })

	sourceDir := t.TempDir()
	storageDir := t.TempDir()

	// Create many files to give time for stop.
	for i := 0; i < 10; i++ {
		createTestFile(t, sourceDir, fmt.Sprintf("file%d.txt", i), fmt.Sprintf("content %d unique padding to ensure different hashes", i))
	}

	// Use a blocking server connection that calls Stop after the first upload.
	blockingServer := &blockingServerConnection{
		storageDir: storageDir,
	}

	cfg := EngineConfig{
		IncludePaths:    []string{sourceDir},
		ExcludePatterns: nil,
		MaxFileSize:     10 * 1024 * 1024,
	}

	engine := NewBackupEngine(blockingServer, repo, nil, cfg)
	blockingServer.engine = engine
	blockingServer.stopAfter = 2 // Stop after 2 uploads.

	result, err := engine.RunBackup(context.Background(), BackupRequest{
		Level:       model.BackupLevelFull,
		ClientID:    "test-client",
		InitiatedBy: "test",
	})
	if err != nil {
		t.Fatalf("RunBackup failed: %v", err)
	}

	if result.Status != model.JobStopped {
		t.Errorf("expected status %q, got %q", model.JobStopped, result.Status)
	}

	// Verify job status in DB.
	jobs, _ := repo.ListJobs(context.Background(), db.JobFilter{Limit: 10})
	if len(jobs) < 1 {
		t.Fatal("expected at least 1 job")
	}
	if jobs[0].Status != model.JobStopped {
		t.Errorf("expected job status %q, got %q", model.JobStopped, jobs[0].Status)
	}
}

// blockingServerConnection triggers Stop after a certain number of uploads.
type blockingServerConnection struct {
	storageDir  string
	engine      *BackupEngine
	stopAfter   int
	uploadCount int
}

func (b *blockingServerConnection) ExchangeManifest(ctx context.Context, manifest []model.ManifestEntry) (ManifestDiff, error) {
	local := &LocalServerConnection{StorageDir: b.storageDir}
	return local.ExchangeManifest(ctx, manifest)
}

func (b *blockingServerConnection) UploadFile(ctx context.Context, hash string, data []byte, wrappedDEK []byte, nonce []byte, entry model.BackupEntry) error {
	local := &LocalServerConnection{StorageDir: b.storageDir}
	err := local.UploadFile(ctx, hash, data, wrappedDEK, nonce, entry)
	if err != nil {
		return err
	}
	b.uploadCount++
	if b.uploadCount >= b.stopAfter {
		b.engine.Stop(ctx)
	}
	return nil
}

func (b *blockingServerConnection) SyncDatabase(ctx context.Context, dbPath string) error {
	return nil
}

func TestStop_Method(t *testing.T) {
	engine, _, _, sourceDir := setupTestEngine(t, false)

	// Create a file.
	createTestFile(t, sourceDir, "file.txt", "data")

	// Call Stop.
	if err := engine.Stop(context.Background()); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	// Verify the stopped flag is set.
	if !engine.stopped.Load() {
		t.Error("expected stopped flag to be true after Stop()")
	}
}

func TestRunBackup_ContextCancellation(t *testing.T) {
	engine, _, _, sourceDir := setupTestEngine(t, false)

	createTestFile(t, sourceDir, "file.txt", "data")

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	result, err := engine.RunBackup(ctx, BackupRequest{
		Level:       model.BackupLevelFull,
		ClientID:    "test-client",
		InitiatedBy: "test",
	})

	// With a cancelled context, the backup may fail to create the job record.
	// This is expected behavior — either an error or a failed/stopped result.
	if err != nil {
		// Expected: DB operation fails with cancelled context.
		return
	}

	// If no error, the job should be in a non-running terminal state.
	if result.Status == model.JobRunning {
		t.Error("expected terminal job status, got running")
	}
}

func TestRunBackup_BackupLevels(t *testing.T) {
	levels := []model.BackupLevel{
		model.BackupLevelAuto,
		model.BackupLevelFull,
		model.BackupLevelOngoing,
	}

	for _, level := range levels {
		t.Run(level.String(), func(t *testing.T) {
			engine, repo, _, sourceDir := setupTestEngine(t, false)
			createTestFile(t, sourceDir, "test.txt", "level test")

			result, err := engine.RunBackup(context.Background(), BackupRequest{
				Level:       level,
				ClientID:    "test-client",
				InitiatedBy: "test",
			})
			if err != nil {
				t.Fatalf("RunBackup with level %s failed: %v", level, err)
			}
			if result.Status != model.JobCompleted {
				t.Errorf("expected completed for level %s, got %q", level, result.Status)
			}

			// Verify job level is stored correctly.
			jobs, _ := repo.ListJobs(context.Background(), db.JobFilter{Limit: 1})
			if len(jobs) != 1 {
				t.Fatalf("expected 1 job, got %d", len(jobs))
			}
			if jobs[0].Level != level.String() {
				t.Errorf("expected job level %q, got %q", level.String(), jobs[0].Level)
			}
		})
	}
}

func TestLocalServerConnection_ExchangeManifest(t *testing.T) {
	storageDir := t.TempDir()
	conn := &LocalServerConnection{StorageDir: storageDir}

	// Pre-store a hash in the CAS directory.
	existingHash := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	dir := filepath.Join(storageDir, existingHash[:2])
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, existingHash), []byte("data"), 0o644)

	manifest := []model.ManifestEntry{
		{Blake3Hash: existingHash, FilePath: "/existing.txt", FileSize: 4},
		{Blake3Hash: "1111111111111111111111111111111111111111111111111111111111111111", FilePath: "/new.txt", FileSize: 10},
	}

	diff, err := conn.ExchangeManifest(context.Background(), manifest)
	if err != nil {
		t.Fatalf("ExchangeManifest failed: %v", err)
	}

	if diff.DedupCount != 1 {
		t.Errorf("expected 1 dedup, got %d", diff.DedupCount)
	}
	if len(diff.NeededHashes) != 1 {
		t.Errorf("expected 1 needed hash, got %d", len(diff.NeededHashes))
	}
	if diff.NeededHashes[0] != "1111111111111111111111111111111111111111111111111111111111111111" {
		t.Errorf("unexpected needed hash: %s", diff.NeededHashes[0])
	}
}

func TestLocalServerConnection_UploadFile(t *testing.T) {
	storageDir := t.TempDir()
	conn := &LocalServerConnection{StorageDir: storageDir}

	hash := "aabbccdd11223344556677889900aabbccddee1122334455667788990011aa"
	data := []byte("test upload data")

	err := conn.UploadFile(context.Background(), hash, data, nil, nil, model.BackupEntry{})
	if err != nil {
		t.Fatalf("UploadFile failed: %v", err)
	}

	// Verify file exists.
	path := filepath.Join(storageDir, hash[:2], hash)
	stored, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read stored file: %v", err)
	}
	if string(stored) != string(data) {
		t.Errorf("stored data mismatch: got %q, want %q", stored, data)
	}
}

func TestLocalServerConnection_SyncDatabase(t *testing.T) {
	conn := &LocalServerConnection{StorageDir: t.TempDir()}

	// SyncDatabase is a no-op for local connections.
	err := conn.SyncDatabase(context.Background(), "/some/path.db")
	if err != nil {
		t.Fatalf("SyncDatabase should be no-op, got error: %v", err)
	}
}

// findStoredFiles recursively finds all regular files in a directory.
func findStoredFiles(dir string) ([]string, error) {
	var files []string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}
