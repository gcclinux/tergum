package deletion

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gcclinux/tergum/internal/model"
)

// --- Mock Repository ---

type mockRepo struct {
	mu      sync.Mutex
	entries []model.BackupEntry
	jobs    []string // list of backup_ids in backup_jobs
}

func newMockRepo() *mockRepo {
	return &mockRepo{}
}

func (m *mockRepo) addJob(backupID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.jobs = append(m.jobs, backupID)
}

func (m *mockRepo) addEntry(entry model.BackupEntry) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = append(m.entries, entry)
}

func (m *mockRepo) QueryEntries(_ context.Context, filter Filter) ([]model.BackupEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var result []model.BackupEntry
	for _, e := range m.entries {
		if matchesFilter(e, filter) {
			result = append(result, e)
		}
	}
	return result, nil
}

func (m *mockRepo) DeleteEntries(_ context.Context, filter Filter) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var remaining []model.BackupEntry
	var deleted int64
	for _, e := range m.entries {
		if matchesFilter(e, filter) {
			deleted++
		} else {
			remaining = append(remaining, e)
		}
	}
	m.entries = remaining
	return deleted, nil
}

func (m *mockRepo) CountHashReferences(_ context.Context, hash string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var count int64
	for _, e := range m.entries {
		if e.Blake3Hash == hash {
			count++
		}
	}
	return count, nil
}

func (m *mockRepo) DeleteOrphanJobs(_ context.Context) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Find jobs that have no entries.
	activeJobs := make(map[string]struct{})
	for _, e := range m.entries {
		activeJobs[e.BackupID] = struct{}{}
	}

	var remaining []string
	var removed int64
	for _, jobID := range m.jobs {
		if _, ok := activeJobs[jobID]; ok {
			remaining = append(remaining, jobID)
		} else {
			removed++
		}
	}
	m.jobs = remaining
	return removed, nil
}

func matchesFilter(e model.BackupEntry, f Filter) bool {
	// If AllBackups is set and no other filter fields, match everything.
	if f.AllBackups && f.BackupID == "" && f.FolderPath == "" && f.FilePath == "" {
		return true
	}

	match := true
	hasCondition := false

	if f.BackupID != "" {
		hasCondition = true
		if e.BackupID != f.BackupID {
			match = false
		}
	}
	if f.FolderPath != "" {
		hasCondition = true
		if len(e.FilePath) < len(f.FolderPath) || e.FilePath[:len(f.FolderPath)] != f.FolderPath {
			match = false
		}
	}
	if f.FilePath != "" {
		hasCondition = true
		if e.FilePath != f.FilePath {
			match = false
		}
	}

	if !hasCondition {
		return false
	}
	return match
}

// --- Mock Store ---

type mockStore struct {
	mu    sync.Mutex
	files map[string]bool // hash -> exists
}

func newMockStore() *mockStore {
	return &mockStore{files: make(map[string]bool)}
}

func (s *mockStore) Put(_ context.Context, hash string, _ io.Reader) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.files[hash] = true
	return nil
}

func (s *mockStore) Get(_ context.Context, hash string) (io.ReadCloser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.files[hash] {
		return nil, fmt.Errorf("not found")
	}
	return io.NopCloser(strings.NewReader("")), nil
}

func (s *mockStore) Exists(_ context.Context, hash string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.files[hash], nil
}

func (s *mockStore) Delete(_ context.Context, hash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.files[hash] {
		return fmt.Errorf("hash not found in store")
	}
	delete(s.files, hash)
	return nil
}

func (s *mockStore) RefCount(_ context.Context, _ string) (int64, error) {
	return 0, nil
}

func (s *mockStore) has(hash string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.files[hash]
}

func (s *mockStore) add(hash string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.files[hash] = true
}

// --- Helper ---

func makeEntry(backupID, hash, filePath string, size int64) model.BackupEntry {
	now := time.Now()
	return model.BackupEntry{
		BackupID:   backupID,
		Blake3Hash: hash,
		FilePath:   filePath,
		FileName:   filePath,
		FileSize:   size,
		OS:         "linux",
		BackupDate: now,
	}
}

// --- Tests ---

func TestDeleteByBackupID(t *testing.T) {
	repo := newMockRepo()
	store := newMockStore()
	engine := New(repo, store)

	hash1 := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	hash2 := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

	repo.addJob("backup-1")
	repo.addJob("backup-2")
	repo.addEntry(makeEntry("backup-1", hash1, "/dir/file1.txt", 100))
	repo.addEntry(makeEntry("backup-1", hash2, "/dir/file2.txt", 200))
	repo.addEntry(makeEntry("backup-2", hash1, "/other/file3.txt", 150))
	store.add(hash1)
	store.add(hash2)

	ctx := context.Background()
	result, err := engine.DeleteByBackupID(ctx, "backup-1", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.EntriesDeleted != 2 {
		t.Errorf("expected 2 entries deleted, got %d", result.EntriesDeleted)
	}
	if result.BytesFreed != 300 {
		t.Errorf("expected 300 bytes freed, got %d", result.BytesFreed)
	}
	// hash1 still referenced by backup-2, only hash2 should be removed.
	if result.FilesRemoved != 1 {
		t.Errorf("expected 1 physical file removed, got %d", result.FilesRemoved)
	}
	if store.has(hash1) != true {
		t.Error("hash1 should still exist (referenced by backup-2)")
	}
	if store.has(hash2) != false {
		t.Error("hash2 should be deleted (no more references)")
	}
	// backup-1 job should be removed (no entries left).
	if result.JobsRemoved != 1 {
		t.Errorf("expected 1 job removed, got %d", result.JobsRemoved)
	}
}

func TestDeleteByFolder(t *testing.T) {
	repo := newMockRepo()
	store := newMockStore()
	engine := New(repo, store)

	hash1 := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	hash2 := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	hash3 := "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"

	repo.addJob("backup-1")
	repo.addEntry(makeEntry("backup-1", hash1, "/docs/readme.md", 50))
	repo.addEntry(makeEntry("backup-1", hash2, "/docs/guide.md", 75))
	repo.addEntry(makeEntry("backup-1", hash3, "/src/main.go", 200))
	store.add(hash1)
	store.add(hash2)
	store.add(hash3)

	ctx := context.Background()
	result, err := engine.DeleteByFolder(ctx, "/docs/", "backup-1", false, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.EntriesDeleted != 2 {
		t.Errorf("expected 2 entries deleted, got %d", result.EntriesDeleted)
	}
	if result.BytesFreed != 125 {
		t.Errorf("expected 125 bytes freed, got %d", result.BytesFreed)
	}
	if result.FilesRemoved != 2 {
		t.Errorf("expected 2 physical files removed, got %d", result.FilesRemoved)
	}
	// /src/main.go should still exist.
	if store.has(hash3) != true {
		t.Error("hash3 should still exist")
	}
}

func TestDeleteByFile(t *testing.T) {
	repo := newMockRepo()
	store := newMockStore()
	engine := New(repo, store)

	hash1 := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	hash2 := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

	repo.addJob("backup-1")
	repo.addEntry(makeEntry("backup-1", hash1, "/dir/file1.txt", 100))
	repo.addEntry(makeEntry("backup-1", hash2, "/dir/file2.txt", 200))
	store.add(hash1)
	store.add(hash2)

	ctx := context.Background()
	result, err := engine.DeleteByFile(ctx, "/dir/file1.txt", "backup-1", false, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.EntriesDeleted != 1 {
		t.Errorf("expected 1 entry deleted, got %d", result.EntriesDeleted)
	}
	if result.BytesFreed != 100 {
		t.Errorf("expected 100 bytes freed, got %d", result.BytesFreed)
	}
	if result.FilesRemoved != 1 {
		t.Errorf("expected 1 physical file removed, got %d", result.FilesRemoved)
	}
	if store.has(hash1) != false {
		t.Error("hash1 should be deleted")
	}
	if store.has(hash2) != true {
		t.Error("hash2 should still exist")
	}
}

func TestPhysicalFileDeletedOnlyWhenRefcountZero(t *testing.T) {
	repo := newMockRepo()
	store := newMockStore()
	engine := New(repo, store)

	// Same hash referenced by two entries in different backups.
	sharedHash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

	repo.addJob("backup-1")
	repo.addJob("backup-2")
	repo.addEntry(makeEntry("backup-1", sharedHash, "/file.txt", 100))
	repo.addEntry(makeEntry("backup-2", sharedHash, "/file.txt", 100))
	store.add(sharedHash)

	ctx := context.Background()

	// Delete from backup-1 only.
	result, err := engine.DeleteByBackupID(ctx, "backup-1", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.FilesRemoved != 0 {
		t.Errorf("expected 0 physical files removed (still referenced), got %d", result.FilesRemoved)
	}
	if store.has(sharedHash) != true {
		t.Error("shared hash should still exist in store")
	}

	// Now delete from backup-2 → refcount goes to 0.
	result, err = engine.DeleteByBackupID(ctx, "backup-2", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.FilesRemoved != 1 {
		t.Errorf("expected 1 physical file removed, got %d", result.FilesRemoved)
	}
	if store.has(sharedHash) != false {
		t.Error("shared hash should be deleted from store")
	}
}

func TestJobRemovedWhenAllEntriesDeleted(t *testing.T) {
	repo := newMockRepo()
	store := newMockStore()
	engine := New(repo, store)

	hash1 := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

	repo.addJob("backup-1")
	repo.addJob("backup-2")
	repo.addEntry(makeEntry("backup-1", hash1, "/file1.txt", 100))
	repo.addEntry(makeEntry("backup-2", hash1, "/file2.txt", 200))
	store.add(hash1)

	ctx := context.Background()

	// Delete all entries for backup-1.
	result, err := engine.DeleteByBackupID(ctx, "backup-1", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.JobsRemoved != 1 {
		t.Errorf("expected 1 job removed, got %d", result.JobsRemoved)
	}

	// Verify backup-2 job still exists.
	if len(repo.jobs) != 1 || repo.jobs[0] != "backup-2" {
		t.Errorf("expected backup-2 to remain, got jobs: %v", repo.jobs)
	}
}

func TestDryRunDoesNotModify(t *testing.T) {
	repo := newMockRepo()
	store := newMockStore()
	engine := New(repo, store)

	hash1 := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	hash2 := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

	repo.addJob("backup-1")
	repo.addEntry(makeEntry("backup-1", hash1, "/dir/file1.txt", 100))
	repo.addEntry(makeEntry("backup-1", hash2, "/dir/file2.txt", 200))
	store.add(hash1)
	store.add(hash2)

	ctx := context.Background()
	result, err := engine.DeleteByBackupID(ctx, "backup-1", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Dry-run should report what would happen.
	if result.EntriesDeleted != 2 {
		t.Errorf("expected 2 entries would be deleted, got %d", result.EntriesDeleted)
	}
	if result.BytesFreed != 300 {
		t.Errorf("expected 300 bytes would be freed, got %d", result.BytesFreed)
	}
	if result.FilesRemoved != 2 {
		t.Errorf("expected 2 physical files would be removed, got %d", result.FilesRemoved)
	}
	if result.JobsRemoved != 1 {
		t.Errorf("expected 1 job would be removed, got %d", result.JobsRemoved)
	}

	// Verify nothing was actually modified.
	if len(repo.entries) != 2 {
		t.Errorf("expected 2 entries still in repo, got %d", len(repo.entries))
	}
	if len(repo.jobs) != 1 {
		t.Errorf("expected 1 job still in repo, got %d", len(repo.jobs))
	}
	if !store.has(hash1) {
		t.Error("hash1 should still exist in store after dry-run")
	}
	if !store.has(hash2) {
		t.Error("hash2 should still exist in store after dry-run")
	}
}

func TestAllBackupsFlag(t *testing.T) {
	repo := newMockRepo()
	store := newMockStore()
	engine := New(repo, store)

	hash1 := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

	repo.addJob("backup-1")
	repo.addJob("backup-2")
	repo.addEntry(makeEntry("backup-1", hash1, "/shared/file.txt", 100))
	repo.addEntry(makeEntry("backup-2", hash1, "/shared/file.txt", 100))
	store.add(hash1)

	ctx := context.Background()

	// Delete the file across all backups.
	result, err := engine.DeleteByFile(ctx, "/shared/file.txt", "", true, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.EntriesDeleted != 2 {
		t.Errorf("expected 2 entries deleted, got %d", result.EntriesDeleted)
	}
	if result.FilesRemoved != 1 {
		t.Errorf("expected 1 physical file removed, got %d", result.FilesRemoved)
	}
	if result.JobsRemoved != 2 {
		t.Errorf("expected 2 jobs removed, got %d", result.JobsRemoved)
	}
	if store.has(hash1) {
		t.Error("hash1 should be deleted from store")
	}
}

func TestDeleteAll(t *testing.T) {
	repo := newMockRepo()
	store := newMockStore()
	engine := New(repo, store)

	hash1 := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	hash2 := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

	repo.addJob("backup-1")
	repo.addJob("backup-2")
	repo.addEntry(makeEntry("backup-1", hash1, "/file1.txt", 100))
	repo.addEntry(makeEntry("backup-2", hash2, "/file2.txt", 200))
	store.add(hash1)
	store.add(hash2)

	ctx := context.Background()
	result, err := engine.DeleteAll(ctx, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.EntriesDeleted != 2 {
		t.Errorf("expected 2 entries deleted, got %d", result.EntriesDeleted)
	}
	if result.BytesFreed != 300 {
		t.Errorf("expected 300 bytes freed, got %d", result.BytesFreed)
	}
	if result.FilesRemoved != 2 {
		t.Errorf("expected 2 physical files removed, got %d", result.FilesRemoved)
	}
	if result.JobsRemoved != 2 {
		t.Errorf("expected 2 jobs removed, got %d", result.JobsRemoved)
	}
	if len(repo.entries) != 0 {
		t.Errorf("expected 0 entries remaining, got %d", len(repo.entries))
	}
}

func TestDeleteByBackupID_EmptyID(t *testing.T) {
	repo := newMockRepo()
	store := newMockStore()
	engine := New(repo, store)

	ctx := context.Background()
	_, err := engine.DeleteByBackupID(ctx, "", false)
	if err == nil {
		t.Fatal("expected error for empty backup_id")
	}
}

func TestDeleteByFolder_EmptyPath(t *testing.T) {
	repo := newMockRepo()
	store := newMockStore()
	engine := New(repo, store)

	ctx := context.Background()
	_, err := engine.DeleteByFolder(ctx, "", "backup-1", false, false)
	if err == nil {
		t.Fatal("expected error for empty folder path")
	}
}

func TestDeleteByFile_RequiresBackupIDWhenNotAllBackups(t *testing.T) {
	repo := newMockRepo()
	store := newMockStore()
	engine := New(repo, store)

	ctx := context.Background()
	_, err := engine.DeleteByFile(ctx, "/file.txt", "", false, false)
	if err == nil {
		t.Fatal("expected error when backup_id empty and all_backups is false")
	}
}

func TestDeleteByFolder_AllBackups(t *testing.T) {
	repo := newMockRepo()
	store := newMockStore()
	engine := New(repo, store)

	hash1 := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	hash2 := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

	repo.addJob("backup-1")
	repo.addJob("backup-2")
	repo.addEntry(makeEntry("backup-1", hash1, "/docs/readme.md", 50))
	repo.addEntry(makeEntry("backup-2", hash2, "/docs/guide.md", 75))
	repo.addEntry(makeEntry("backup-2", hash1, "/src/main.go", 200))
	store.add(hash1)
	store.add(hash2)

	ctx := context.Background()
	result, err := engine.DeleteByFolder(ctx, "/docs/", "", true, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.EntriesDeleted != 2 {
		t.Errorf("expected 2 entries deleted, got %d", result.EntriesDeleted)
	}
	// hash1 still referenced by /src/main.go in backup-2, only hash2 removed.
	if result.FilesRemoved != 1 {
		t.Errorf("expected 1 physical file removed, got %d", result.FilesRemoved)
	}
	if store.has(hash2) {
		t.Error("hash2 should be deleted")
	}
	if !store.has(hash1) {
		t.Error("hash1 should still exist (referenced by /src/main.go)")
	}
}
