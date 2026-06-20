package scheduler

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ricardopadilha/tergum/internal/backup"
	"github.com/ricardopadilha/tergum/internal/db"
	"github.com/ricardopadilha/tergum/internal/model"
	"github.com/ricardopadilha/tergum/internal/watcher"
)

// mockWatcher implements watcher.Watcher for testing.
type mockWatcher struct {
	ch chan watcher.StableFile
}

func newMockWatcher() *mockWatcher {
	return &mockWatcher{ch: make(chan watcher.StableFile, 64)}
}

func (m *mockWatcher) Start(ctx context.Context) error           { return nil }
func (m *mockWatcher) Stop() error                               { return nil }
func (m *mockWatcher) AddPath(path string, recursive bool) error { return nil }
func (m *mockWatcher) RemovePath(path string) error              { return nil }
func (m *mockWatcher) StableFiles() <-chan watcher.StableFile    { return m.ch }
func (m *mockWatcher) Status() watcher.WatcherStatus             { return watcher.WatcherStatus{Running: true} }

// mockServerConnection implements backup.ServerConnection for testing.
type mockServerConnection struct {
	uploadedHashes map[string][]byte
	existingHashes map[string]bool
}

func newMockServer() *mockServerConnection {
	return &mockServerConnection{
		uploadedHashes: make(map[string][]byte),
		existingHashes: make(map[string]bool),
	}
}

func (m *mockServerConnection) ExchangeManifest(ctx context.Context, manifest []model.ManifestEntry) (backup.ManifestDiff, error) {
	var needed []string
	dedupCount := 0
	for _, entry := range manifest {
		if m.existingHashes[entry.Blake3Hash] {
			dedupCount++
		} else {
			needed = append(needed, entry.Blake3Hash)
		}
	}
	return backup.ManifestDiff{
		NeededHashes: needed,
		DedupCount:   dedupCount,
	}, nil
}

func (m *mockServerConnection) UploadFile(ctx context.Context, hash string, data []byte, wrappedDEK []byte, nonce []byte, entry model.BackupEntry) error {
	m.uploadedHashes[hash] = data
	m.existingHashes[hash] = true
	return nil
}

func (m *mockServerConnection) SyncDatabase(ctx context.Context, dbPath string) error {
	return nil
}

// mockRepository implements a minimal db.Repository for testing.
type mockRepository struct {
	jobs    []model.BackupJob
	entries []model.BackupEntry
}

func newMockRepo() *mockRepository {
	return &mockRepository{}
}

func (m *mockRepository) InsertBackupEntry(ctx context.Context, entry model.BackupEntry) error {
	m.entries = append(m.entries, entry)
	return nil
}

func (m *mockRepository) CreateJob(ctx context.Context, job model.BackupJob) error {
	m.jobs = append(m.jobs, job)
	return nil
}

func (m *mockRepository) UpdateJob(ctx context.Context, jobID string, update db.JobUpdate) error {
	for i := range m.jobs {
		if m.jobs[i].BackupID == jobID {
			if update.Status != nil {
				m.jobs[i].Status = *update.Status
			}
			if update.FinishedAt != nil {
				m.jobs[i].FinishedAt = update.FinishedAt
			}
			if update.FileCount != nil {
				m.jobs[i].FileCount = *update.FileCount
			}
			if update.BytesNew != nil {
				m.jobs[i].BytesNew = *update.BytesNew
			}
			if update.FilesDeduped != nil {
				m.jobs[i].FilesDeduped = *update.FilesDeduped
			}
			if update.ErrorMessage != nil {
				m.jobs[i].ErrorMessage = *update.ErrorMessage
			}
			break
		}
	}
	return nil
}

func (m *mockRepository) GetManifest(ctx context.Context, backupID string) ([]model.ManifestEntry, error) {
	return nil, nil
}
func (m *mockRepository) FindByHash(ctx context.Context, hash string) ([]model.BackupEntry, error) {
	return nil, nil
}
func (m *mockRepository) FindByPath(ctx context.Context, pattern string) ([]model.BackupEntry, error) {
	return nil, nil
}
func (m *mockRepository) CountHashReferences(ctx context.Context, hash string) (int64, error) {
	return 0, nil
}
func (m *mockRepository) DeleteEntries(ctx context.Context, filter db.DeleteFilter) (int64, error) {
	return 0, nil
}
func (m *mockRepository) QueryEntries(ctx context.Context, filter db.DeleteFilter) ([]model.BackupEntry, error) {
	return nil, nil
}
func (m *mockRepository) DeleteOrphanJobs(ctx context.Context) (int64, error) { return 0, nil }
func (m *mockRepository) ListJobs(ctx context.Context, filter db.JobFilter) ([]model.BackupJob, error) {
	return m.jobs, nil
}
func (m *mockRepository) GetFileVersions(ctx context.Context, filePath string) ([]model.BackupEntry, error) {
	return nil, nil
}
func (m *mockRepository) GetExpiredEntries(ctx context.Context, now time.Time) ([]model.BackupEntry, error) {
	return nil, nil
}
func (m *mockRepository) GetAllFilePaths(ctx context.Context) ([]string, error) { return nil, nil }
func (m *mockRepository) DeleteEntryByID(ctx context.Context, id int64) error   { return nil }
func (m *mockRepository) InsertRetentionPolicy(ctx context.Context, policy model.RetentionPolicy) error {
	return nil
}
func (m *mockRepository) DeleteRetentionPolicy(ctx context.Context, name string) error { return nil }
func (m *mockRepository) ListRetentionPolicies(ctx context.Context) ([]model.RetentionPolicy, error) {
	return nil, nil
}
func (m *mockRepository) RecordRestore(ctx context.Context, entry db.RestoreRecord) error {
	return nil
}
func (m *mockRepository) SaveWatchPath(ctx context.Context, wp db.WatchPath) error { return nil }
func (m *mockRepository) LoadWatchPaths(ctx context.Context) ([]db.WatchPath, error) {
	return nil, nil
}
func (m *mockRepository) DeleteWatchPath(ctx context.Context, path string) error          { return nil }
func (m *mockRepository) AddIncludePath(ctx context.Context, path string) error           { return nil }
func (m *mockRepository) RemoveIncludePath(ctx context.Context, path string) error        { return nil }
func (m *mockRepository) ListIncludePaths(ctx context.Context) ([]string, error)          { return nil, nil }
func (m *mockRepository) AddExcludePattern(ctx context.Context, pattern string) error     { return nil }
func (m *mockRepository) RemoveExcludePattern(ctx context.Context, pattern string) error  { return nil }
func (m *mockRepository) ListExcludePatterns(ctx context.Context) ([]string, error)       { return nil, nil }
func (m *mockRepository) Close() error { return nil }

func TestNewOngoingBackup_DefaultBatchInterval(t *testing.T) {
	mw := newMockWatcher()
	ms := newMockServer()
	mr := newMockRepo()

	ob := NewOngoingBackup(OngoingConfig{
		Watcher: mw,
		Server:  ms,
		Repo:    mr,
	})

	if ob.batchInterval != 5*time.Minute {
		t.Errorf("expected default batch interval 5m, got %v", ob.batchInterval)
	}
}

func TestNewOngoingBackup_CustomBatchInterval(t *testing.T) {
	mw := newMockWatcher()
	ms := newMockServer()
	mr := newMockRepo()

	ob := NewOngoingBackup(OngoingConfig{
		Watcher:       mw,
		Server:        ms,
		Repo:          mr,
		BatchInterval: 10 * time.Second,
	})

	if ob.batchInterval != 10*time.Second {
		t.Errorf("expected batch interval 10s, got %v", ob.batchInterval)
	}
}

func TestOngoingBackup_ProcessBatch_NewFile(t *testing.T) {
	// Create a temp file to be "backed up".
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.txt")
	content := []byte("hello ongoing backup")
	if err := os.WriteFile(tmpFile, content, 0o644); err != nil {
		t.Fatal(err)
	}

	mw := newMockWatcher()
	ms := newMockServer()
	mr := newMockRepo()

	ob := NewOngoingBackup(OngoingConfig{
		Watcher:       mw,
		Server:        ms,
		Repo:          mr,
		BatchInterval: 100 * time.Millisecond,
	})

	// Directly test processBatch.
	batch := []watcher.StableFile{
		{
			Path:       tmpFile,
			Hash:       "abc123def456",
			ModifiedAt: time.Now(),
			Size:       int64(len(content)),
		},
	}

	ctx := context.Background()
	ob.processBatch(ctx, batch)

	// Verify job was created with "watcher" initiated_by.
	if len(mr.jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(mr.jobs))
	}
	if mr.jobs[0].InitiatedBy != "watcher" {
		t.Errorf("expected initiated_by=watcher, got %q", mr.jobs[0].InitiatedBy)
	}
	if mr.jobs[0].Level != "ONGOING" {
		t.Errorf("expected level=ONGOING, got %q", mr.jobs[0].Level)
	}
	if mr.jobs[0].Status != model.JobCompleted {
		t.Errorf("expected job status=completed, got %q", mr.jobs[0].Status)
	}

	// Verify file was uploaded.
	if _, ok := ms.uploadedHashes["abc123def456"]; !ok {
		t.Error("expected file to be uploaded to server")
	}

	// Verify backup entry was created.
	if len(mr.entries) != 1 {
		t.Fatalf("expected 1 backup entry, got %d", len(mr.entries))
	}
	if mr.entries[0].Blake3Hash != "abc123def456" {
		t.Errorf("expected hash abc123def456, got %q", mr.entries[0].Blake3Hash)
	}
	if mr.entries[0].FileName != "test.txt" {
		t.Errorf("expected filename test.txt, got %q", mr.entries[0].FileName)
	}
}

func TestOngoingBackup_ProcessBatch_DedupFile(t *testing.T) {
	// Create a temp file.
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "existing.txt")
	content := []byte("already backed up")
	if err := os.WriteFile(tmpFile, content, 0o644); err != nil {
		t.Fatal(err)
	}

	mw := newMockWatcher()
	ms := newMockServer()
	mr := newMockRepo()

	// Pre-populate server with this hash.
	ms.existingHashes["existinghash123"] = true

	ob := NewOngoingBackup(OngoingConfig{
		Watcher:       mw,
		Server:        ms,
		Repo:          mr,
		BatchInterval: 100 * time.Millisecond,
	})

	batch := []watcher.StableFile{
		{
			Path:       tmpFile,
			Hash:       "existinghash123",
			ModifiedAt: time.Now(),
			Size:       int64(len(content)),
		},
	}

	ctx := context.Background()
	ob.processBatch(ctx, batch)

	// Verify file was NOT uploaded (dedup).
	if _, ok := ms.uploadedHashes["existinghash123"]; ok {
		t.Error("expected deduped file to not be uploaded")
	}

	// Verify backup entry was still recorded.
	if len(mr.entries) != 1 {
		t.Fatalf("expected 1 backup entry, got %d", len(mr.entries))
	}

	// Verify job shows dedup count.
	if len(mr.jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(mr.jobs))
	}
	if mr.jobs[0].FilesDeduped != 1 {
		t.Errorf("expected 1 deduped file, got %d", mr.jobs[0].FilesDeduped)
	}
}

func TestOngoingBackup_StartStop(t *testing.T) {
	// Create a temp file.
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "watched.txt")
	content := []byte("watched file content")
	if err := os.WriteFile(tmpFile, content, 0o644); err != nil {
		t.Fatal(err)
	}

	mw := newMockWatcher()
	ms := newMockServer()
	mr := newMockRepo()

	ob := NewOngoingBackup(OngoingConfig{
		Watcher:       mw,
		Server:        ms,
		Repo:          mr,
		BatchInterval: 50 * time.Millisecond, // Short interval for testing.
	})

	ctx := context.Background()
	if err := ob.Start(ctx); err != nil {
		t.Fatal(err)
	}

	// Send a stable file through the watcher channel.
	mw.ch <- watcher.StableFile{
		Path:       tmpFile,
		Hash:       "watchedhash999",
		ModifiedAt: time.Now(),
		Size:       int64(len(content)),
	}

	// Wait for batch to be processed.
	time.Sleep(150 * time.Millisecond)

	if err := ob.Stop(); err != nil {
		t.Fatal(err)
	}

	// Verify file was processed.
	if _, ok := ms.uploadedHashes["watchedhash999"]; !ok {
		t.Error("expected watched file to be uploaded")
	}
	if len(mr.jobs) == 0 {
		t.Error("expected at least one job created")
	}
	if len(mr.entries) == 0 {
		t.Error("expected at least one backup entry")
	}
}

func TestOngoingBackup_EmptyBatchNotProcessed(t *testing.T) {
	mw := newMockWatcher()
	ms := newMockServer()
	mr := newMockRepo()

	ob := NewOngoingBackup(OngoingConfig{
		Watcher:       mw,
		Server:        ms,
		Repo:          mr,
		BatchInterval: 50 * time.Millisecond,
	})

	ctx := context.Background()
	if err := ob.Start(ctx); err != nil {
		t.Fatal(err)
	}

	// Wait for a few ticks without sending any files.
	time.Sleep(150 * time.Millisecond)

	if err := ob.Stop(); err != nil {
		t.Fatal(err)
	}

	// No jobs should have been created.
	if len(mr.jobs) != 0 {
		t.Errorf("expected 0 jobs for empty batches, got %d", len(mr.jobs))
	}
}

func TestOngoingBackup_MultipleBatches(t *testing.T) {
	tmpDir := t.TempDir()

	// Create multiple temp files.
	files := make([]string, 3)
	for i := range files {
		files[i] = filepath.Join(tmpDir, filepath.Base(t.Name())+string(rune('a'+i))+".txt")
		if err := os.WriteFile(files[i], []byte("content"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	mw := newMockWatcher()
	ms := newMockServer()
	mr := newMockRepo()

	ob := NewOngoingBackup(OngoingConfig{
		Watcher:       mw,
		Server:        ms,
		Repo:          mr,
		BatchInterval: 80 * time.Millisecond,
	})

	ctx := context.Background()
	if err := ob.Start(ctx); err != nil {
		t.Fatal(err)
	}

	// Send first file.
	mw.ch <- watcher.StableFile{
		Path: files[0], Hash: "hash_a", ModifiedAt: time.Now(), Size: 7,
	}

	// Wait for first batch.
	time.Sleep(120 * time.Millisecond)

	// Send second and third file (should go into next batch).
	mw.ch <- watcher.StableFile{
		Path: files[1], Hash: "hash_b", ModifiedAt: time.Now(), Size: 7,
	}
	mw.ch <- watcher.StableFile{
		Path: files[2], Hash: "hash_c", ModifiedAt: time.Now(), Size: 7,
	}

	// Wait for second batch.
	time.Sleep(120 * time.Millisecond)

	if err := ob.Stop(); err != nil {
		t.Fatal(err)
	}

	// Should have 2 jobs (one per batch).
	if len(mr.jobs) < 2 {
		t.Errorf("expected at least 2 jobs (one per batch), got %d", len(mr.jobs))
	}

	// All 3 files should be uploaded.
	if len(ms.uploadedHashes) != 3 {
		t.Errorf("expected 3 uploaded files, got %d", len(ms.uploadedHashes))
	}
}
