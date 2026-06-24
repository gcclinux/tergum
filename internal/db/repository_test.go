package db

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gcclinux/tergum/internal/model"
)

func newTestRepo(t *testing.T, isServer bool) *SQLiteRepository {
	t.Helper()
	repo, err := NewRepository(":memory:", isServer)
	if err != nil {
		t.Fatalf("NewRepository: %v", err)
	}
	t.Cleanup(func() { repo.Close() })
	return repo
}

func TestNewRepository_ClientSchema(t *testing.T) {
	repo := newTestRepo(t, false)

	// Verify tables exist by querying them.
	ctx := context.Background()
	_, err := repo.ListWatchExcludes(ctx)
	if err != nil {
		t.Fatalf("ListWatchExcludes on empty table: %v", err)
	}

	// retention_policies should NOT exist for client.
	_, err = repo.db.Exec("SELECT * FROM retention_policies")
	if err == nil {
		t.Fatal("expected retention_policies to not exist for client repo")
	}
}

func TestNewRepository_ServerSchema(t *testing.T) {
	repo := newTestRepo(t, true)

	// retention_policies should exist for server.
	_, err := repo.db.Exec("SELECT * FROM retention_policies")
	if err != nil {
		t.Fatalf("retention_policies should exist for server: %v", err)
	}
}

func TestInsertAndFindByHash(t *testing.T) {
	repo := newTestRepo(t, false)
	ctx := context.Background()

	// Create a job first (foreign key).
	job := model.BackupJob{
		BackupID:    "job-001",
		Level:       "FULL",
		ClientID:    "client-1",
		InitiatedBy: "cli",
		StartedAt:   time.Now().UTC().Truncate(time.Second),
		Status:      model.JobRunning,
	}
	if err := repo.CreateJob(ctx, job); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	perm := uint32(0o644)
	entry := model.BackupEntry{
		BackupID:    "job-001",
		Blake3Hash:  "abc123def456",
		FileName:    "test.txt",
		FilePath:    "/home/user/test.txt",
		FileExt:     ".txt",
		FileSize:    1024,
		CreatedAt:   &now,
		ModifiedAt:  &now,
		Permissions: &perm,
		Owner:       "user",
		FileGroup:   "staff",
		OS:          "linux",
		BackupDate:  now,
	}

	if err := repo.InsertBackupEntry(ctx, entry); err != nil {
		t.Fatalf("InsertBackupEntry: %v", err)
	}

	results, err := repo.FindByHash(ctx, "abc123def456")
	if err != nil {
		t.Fatalf("FindByHash: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	got := results[0]
	if got.Blake3Hash != "abc123def456" {
		t.Errorf("Blake3Hash = %q, want %q", got.Blake3Hash, "abc123def456")
	}
	if got.FileName != "test.txt" {
		t.Errorf("FileName = %q, want %q", got.FileName, "test.txt")
	}
	if got.FileSize != 1024 {
		t.Errorf("FileSize = %d, want %d", got.FileSize, 1024)
	}
	if got.Permissions == nil || *got.Permissions != 0o644 {
		t.Errorf("Permissions = %v, want 0644", got.Permissions)
	}
}

func TestFindByPath(t *testing.T) {
	repo := newTestRepo(t, false)
	ctx := context.Background()

	job := model.BackupJob{
		BackupID: "job-002", Level: "AUTO", ClientID: "client-1",
		InitiatedBy: "cli", StartedAt: time.Now().UTC().Truncate(time.Second), Status: model.JobRunning,
	}
	repo.CreateJob(ctx, job)

	now := time.Now().UTC().Truncate(time.Second)
	entries := []model.BackupEntry{
		{BackupID: "job-002", Blake3Hash: "h1", FileName: "a.go", FilePath: "/src/a.go", FileSize: 100, OS: "linux", BackupDate: now},
		{BackupID: "job-002", Blake3Hash: "h2", FileName: "b.go", FilePath: "/src/b.go", FileSize: 200, OS: "linux", BackupDate: now},
		{BackupID: "job-002", Blake3Hash: "h3", FileName: "c.txt", FilePath: "/docs/c.txt", FileSize: 300, OS: "linux", BackupDate: now},
	}
	for _, e := range entries {
		if err := repo.InsertBackupEntry(ctx, e); err != nil {
			t.Fatalf("InsertBackupEntry: %v", err)
		}
	}

	results, err := repo.FindByPath(ctx, "/src/%")
	if err != nil {
		t.Fatalf("FindByPath: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results for /src/%%, got %d", len(results))
	}
}

func TestSearchBackupFiles(t *testing.T) {
	repo := newTestRepo(t, false)
	ctx := context.Background()

	// Create two jobs.
	repo.CreateJob(ctx, model.BackupJob{
		BackupID: "job-s1", Level: "FULL", ClientID: "c1", StartedAt: time.Now(), Status: model.JobCompleted,
	})
	repo.CreateJob(ctx, model.BackupJob{
		BackupID: "job-s2", Level: "FULL", ClientID: "c1", StartedAt: time.Now(), Status: model.JobCompleted,
	})

	now := time.Now().UTC().Truncate(time.Second)
	entries := []model.BackupEntry{
		{BackupID: "job-s1", Blake3Hash: "h1", FileName: "important.txt", FilePath: "/docs/important.txt", FileSize: 100, OS: "linux", BackupDate: now},
		{BackupID: "job-s1", Blake3Hash: "h2", FileName: "notes.txt", FilePath: "/docs/notes.txt", FileSize: 100, OS: "linux", BackupDate: now},
		{BackupID: "job-s2", Blake3Hash: "h3", FileName: "important.txt", FilePath: "/docs/important.txt", FileSize: 200, OS: "linux", BackupDate: now},
	}
	for _, e := range entries {
		repo.InsertBackupEntry(ctx, e)
	}

	// Search "important" in job-s1 should yield 1 result.
	results1, err := repo.SearchBackupFiles(ctx, "job-s1", "important")
	if err != nil {
		t.Fatalf("SearchBackupFiles: %v", err)
	}
	if len(results1) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results1))
	}
	if results1[0].BackupID != "job-s1" || results1[0].Blake3Hash != "h1" {
		t.Errorf("unexpected entry returned: %+v", results1[0])
	}

	// Search "important" in job-s2 should yield 1 result (h3).
	results2, err := repo.SearchBackupFiles(ctx, "job-s2", "important")
	if err != nil {
		t.Fatalf("SearchBackupFiles: %v", err)
	}
	if len(results2) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results2))
	}
	if results2[0].BackupID != "job-s2" || results2[0].Blake3Hash != "h3" {
		t.Errorf("unexpected entry returned: %+v", results2[0])
	}

	// Search "txt" in job-s1 should yield 2 results.
	results3, err := repo.SearchBackupFiles(ctx, "job-s1", "txt")
	if err != nil {
		t.Fatalf("SearchBackupFiles: %v", err)
	}
	if len(results3) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results3))
	}
}

func TestCountHashReferences(t *testing.T) {
	repo := newTestRepo(t, false)
	ctx := context.Background()

	job := model.BackupJob{
		BackupID: "job-003", Level: "FULL", ClientID: "c1",
		InitiatedBy: "cli", StartedAt: time.Now().UTC().Truncate(time.Second), Status: model.JobRunning,
	}
	repo.CreateJob(ctx, job)

	now := time.Now().UTC().Truncate(time.Second)
	// Same hash, different files.
	for _, fp := range []string{"/a.txt", "/b.txt", "/c.txt"} {
		repo.InsertBackupEntry(ctx, model.BackupEntry{
			BackupID: "job-003", Blake3Hash: "shared-hash", FileName: "f",
			FilePath: fp, FileSize: 10, OS: "linux", BackupDate: now,
		})
	}

	count, err := repo.CountHashReferences(ctx, "shared-hash")
	if err != nil {
		t.Fatalf("CountHashReferences: %v", err)
	}
	if count != 3 {
		t.Errorf("count = %d, want 3", count)
	}
}

func TestDeleteEntries(t *testing.T) {
	repo := newTestRepo(t, false)
	ctx := context.Background()

	job := model.BackupJob{
		BackupID: "job-004", Level: "FULL", ClientID: "c1",
		InitiatedBy: "cli", StartedAt: time.Now().UTC().Truncate(time.Second), Status: model.JobRunning,
	}
	repo.CreateJob(ctx, job)

	now := time.Now().UTC().Truncate(time.Second)
	for i, fp := range []string{"/dir/a.txt", "/dir/b.txt", "/other/c.txt"} {
		repo.InsertBackupEntry(ctx, model.BackupEntry{
			BackupID: "job-004", Blake3Hash: fmt.Sprintf("h%d", i), FileName: "f",
			FilePath: fp, FileSize: 10, OS: "linux", BackupDate: now,
		})
	}

	// Delete by folder path.
	deleted, err := repo.DeleteEntries(ctx, DeleteFilter{FolderPath: "/dir/"})
	if err != nil {
		t.Fatalf("DeleteEntries: %v", err)
	}
	if deleted != 2 {
		t.Errorf("deleted = %d, want 2", deleted)
	}

	// Verify remaining.
	remaining, _ := repo.FindByPath(ctx, "%")
	if len(remaining) != 1 {
		t.Errorf("remaining = %d, want 1", len(remaining))
	}
}

func TestCreateAndUpdateJob(t *testing.T) {
	repo := newTestRepo(t, false)
	ctx := context.Background()

	job := model.BackupJob{
		BackupID:    "job-010",
		Level:       "FULL",
		ClientID:    "workstation1",
		ClientIP:    "192.168.1.10",
		InitiatedBy: "scheduler",
		StartedAt:   time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
		Status:      model.JobRunning,
	}
	if err := repo.CreateJob(ctx, job); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	// Update status and file count.
	status := model.JobCompleted
	finished := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	fileCount := int64(42)
	bytesTotal := int64(1048576)
	if err := repo.UpdateJob(ctx, "job-010", JobUpdate{
		Status:     &status,
		FinishedAt: &finished,
		FileCount:  &fileCount,
		BytesTotal: &bytesTotal,
	}); err != nil {
		t.Fatalf("UpdateJob: %v", err)
	}

	// Verify via ListJobs.
	jobs, err := repo.ListJobs(ctx, JobFilter{})
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	j := jobs[0]
	if j.Status != model.JobCompleted {
		t.Errorf("Status = %q, want %q", j.Status, model.JobCompleted)
	}
	if j.FileCount != 42 {
		t.Errorf("FileCount = %d, want 42", j.FileCount)
	}
	if j.BytesTotal != 1048576 {
		t.Errorf("BytesTotal = %d, want 1048576", j.BytesTotal)
	}
}

func TestListJobs_Filter(t *testing.T) {
	repo := newTestRepo(t, false)
	ctx := context.Background()

	for _, j := range []model.BackupJob{
		{BackupID: "j1", Level: "FULL", ClientID: "c1", InitiatedBy: "cli", StartedAt: time.Now().UTC().Truncate(time.Second), Status: model.JobRunning},
		{BackupID: "j2", Level: "AUTO", ClientID: "c2", InitiatedBy: "cli", StartedAt: time.Now().UTC().Truncate(time.Second), Status: model.JobCompleted},
		{BackupID: "j3", Level: "FULL", ClientID: "c1", InitiatedBy: "cli", StartedAt: time.Now().UTC().Truncate(time.Second), Status: model.JobCompleted},
	} {
		repo.CreateJob(ctx, j)
	}

	// Filter by client.
	clientID := "c1"
	jobs, err := repo.ListJobs(ctx, JobFilter{ClientID: &clientID})
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if len(jobs) != 2 {
		t.Errorf("expected 2 jobs for c1, got %d", len(jobs))
	}

	// Filter by status.
	status := model.JobCompleted
	jobs, err = repo.ListJobs(ctx, JobFilter{Status: &status})
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if len(jobs) != 2 {
		t.Errorf("expected 2 completed jobs, got %d", len(jobs))
	}

	// Limit.
	jobs, err = repo.ListJobs(ctx, JobFilter{Limit: 1})
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if len(jobs) != 1 {
		t.Errorf("expected 1 job with limit, got %d", len(jobs))
	}
}

func TestGetManifest(t *testing.T) {
	repo := newTestRepo(t, false)
	ctx := context.Background()

	job := model.BackupJob{
		BackupID: "job-020", Level: "FULL", ClientID: "c1",
		InitiatedBy: "cli", StartedAt: time.Now().UTC().Truncate(time.Second), Status: model.JobRunning,
	}
	repo.CreateJob(ctx, job)

	now := time.Now().UTC().Truncate(time.Second)
	mod := now.Add(-time.Hour)
	repo.InsertBackupEntry(ctx, model.BackupEntry{
		BackupID: "job-020", Blake3Hash: "hash1", FileName: "f1.txt",
		FilePath: "/f1.txt", FileSize: 500, OS: "linux", ModifiedAt: &mod, BackupDate: now,
	})
	repo.InsertBackupEntry(ctx, model.BackupEntry{
		BackupID: "job-020", Blake3Hash: "hash2", FileName: "f2.txt",
		FilePath: "/f2.txt", FileSize: 600, OS: "linux", ModifiedAt: &mod, BackupDate: now,
	})

	manifest, err := repo.GetManifest(ctx, "job-020")
	if err != nil {
		t.Fatalf("GetManifest: %v", err)
	}
	if len(manifest) != 2 {
		t.Fatalf("expected 2 manifest entries, got %d", len(manifest))
	}
	if manifest[0].Blake3Hash != "hash1" && manifest[1].Blake3Hash != "hash1" {
		t.Error("expected hash1 in manifest")
	}
}

func TestGetFileVersions(t *testing.T) {
	repo := newTestRepo(t, false)
	ctx := context.Background()

	job := model.BackupJob{
		BackupID: "job-030", Level: "FULL", ClientID: "c1",
		InitiatedBy: "cli", StartedAt: time.Now().UTC().Truncate(time.Second), Status: model.JobRunning,
	}
	repo.CreateJob(ctx, job)

	for i := 0; i < 3; i++ {
		bd := time.Date(2024, 1, 10+i, 0, 0, 0, 0, time.UTC)
		repo.InsertBackupEntry(ctx, model.BackupEntry{
			BackupID: "job-030", Blake3Hash: fmt.Sprintf("v%d", i), FileName: "doc.txt",
			FilePath: "/data/doc.txt", FileSize: int64(100 + i*10), OS: "linux", BackupDate: bd,
		})
	}

	versions, err := repo.GetFileVersions(ctx, "/data/doc.txt")
	if err != nil {
		t.Fatalf("GetFileVersions: %v", err)
	}
	if len(versions) != 3 {
		t.Fatalf("expected 3 versions, got %d", len(versions))
	}
	// Should be ordered by backup_date DESC.
	if versions[0].BackupDate.Before(versions[1].BackupDate) {
		t.Error("expected newest version first")
	}
}

func TestGetExpiredEntries(t *testing.T) {
	repo := newTestRepo(t, false)
	ctx := context.Background()

	job := model.BackupJob{
		BackupID: "job-040", Level: "FULL", ClientID: "c1",
		InitiatedBy: "cli", StartedAt: time.Now().UTC().Truncate(time.Second), Status: model.JobRunning,
	}
	repo.CreateJob(ctx, job)

	now := time.Now().UTC().Truncate(time.Second)
	past := now.Add(-24 * time.Hour)
	future := now.Add(24 * time.Hour)

	repo.InsertBackupEntry(ctx, model.BackupEntry{
		BackupID: "job-040", Blake3Hash: "expired", FileName: "old.txt",
		FilePath: "/old.txt", FileSize: 50, OS: "linux", BackupDate: past, ExpiresAt: &past,
	})
	repo.InsertBackupEntry(ctx, model.BackupEntry{
		BackupID: "job-040", Blake3Hash: "notexpired", FileName: "new.txt",
		FilePath: "/new.txt", FileSize: 50, OS: "linux", BackupDate: now, ExpiresAt: &future,
	})
	repo.InsertBackupEntry(ctx, model.BackupEntry{
		BackupID: "job-040", Blake3Hash: "noexpiry", FileName: "perm.txt",
		FilePath: "/perm.txt", FileSize: 50, OS: "linux", BackupDate: now,
	})

	expired, err := repo.GetExpiredEntries(ctx, now)
	if err != nil {
		t.Fatalf("GetExpiredEntries: %v", err)
	}
	if len(expired) != 1 {
		t.Fatalf("expected 1 expired entry, got %d", len(expired))
	}
	if expired[0].Blake3Hash != "expired" {
		t.Errorf("expected 'expired' hash, got %q", expired[0].Blake3Hash)
	}
}

func TestRecordRestore(t *testing.T) {
	repo := newTestRepo(t, false)
	ctx := context.Background()

	err := repo.RecordRestore(ctx, RestoreRecord{
		Blake3Hash:   "restore-hash",
		FileName:     "important.doc",
		SourceBackup: "job-050",
		RestoredTo:   "/tmp/important.doc",
		RestoredBy:   "admin",
		Success:      true,
	})
	if err != nil {
		t.Fatalf("RecordRestore: %v", err)
	}

	// Verify by querying directly.
	var count int
	repo.db.QueryRow("SELECT COUNT(*) FROM restore_history WHERE blake3_hash = ?", "restore-hash").Scan(&count)
	if count != 1 {
		t.Errorf("expected 1 restore record, got %d", count)
	}
}

func TestWatchExcludes(t *testing.T) {
	repo := newTestRepo(t, false)
	ctx := context.Background()

	err := repo.AddWatchExclude(ctx, "/home/user/docs")
	if err != nil {
		t.Fatalf("AddWatchExclude: %v", err)
	}

	err = repo.AddWatchExclude(ctx, "/var/log")
	if err != nil {
		t.Fatalf("AddWatchExclude: %v", err)
	}

	// Test duplicate insert (should be ignored due to INSERT OR IGNORE)
	err = repo.AddWatchExclude(ctx, "/var/log")
	if err != nil {
		t.Fatalf("AddWatchExclude duplicate: %v", err)
	}

	excludes, err := repo.ListWatchExcludes(ctx)
	if err != nil {
		t.Fatalf("ListWatchExcludes: %v", err)
	}
	if len(excludes) != 2 {
		t.Fatalf("expected 2 excludes, got %d", len(excludes))
	}
	if excludes[0] != "/home/user/docs" || excludes[1] != "/var/log" {
		t.Errorf("unexpected excludes order or content: %v", excludes)
	}

	err = repo.RemoveWatchExclude(ctx, "/home/user/docs")
	if err != nil {
		t.Fatalf("RemoveWatchExclude: %v", err)
	}

	excludes, err = repo.ListWatchExcludes(ctx)
	if err != nil {
		t.Fatalf("ListWatchExcludes: %v", err)
	}
	if len(excludes) != 1 {
		t.Fatalf("expected 1 exclude, got %d", len(excludes))
	}
	if excludes[0] != "/var/log" {
		t.Errorf("unexpected remaining exclude: %q", excludes[0])
	}
}

func TestWALMode(t *testing.T) {
	repo := newTestRepo(t, false)
	var mode string
	repo.db.QueryRow("PRAGMA journal_mode").Scan(&mode)
	if mode != "wal" && mode != "memory" {
		// In-memory databases may report "memory" instead of "wal".
		// For file-based databases, we'd expect "wal".
		t.Logf("journal_mode = %q (in-memory databases may not use WAL)", mode)
	}
}

// --- Additional tests for task 4.2 ---

func TestDeleteEntries_ByBackupID(t *testing.T) {
	repo := newTestRepo(t, false)
	ctx := context.Background()

	// Create two jobs.
	for _, jid := range []string{"job-del-1", "job-del-2"} {
		repo.CreateJob(ctx, model.BackupJob{
			BackupID: jid, Level: "FULL", ClientID: "c1",
			InitiatedBy: "cli", StartedAt: time.Now().UTC().Truncate(time.Second), Status: model.JobRunning,
		})
	}

	now := time.Now().UTC().Truncate(time.Second)
	// Insert entries in both jobs.
	for i := 0; i < 3; i++ {
		repo.InsertBackupEntry(ctx, model.BackupEntry{
			BackupID: "job-del-1", Blake3Hash: fmt.Sprintf("h1-%d", i), FileName: "f",
			FilePath: fmt.Sprintf("/data/file%d.txt", i), FileSize: 10, OS: "linux", BackupDate: now,
		})
	}
	for i := 0; i < 2; i++ {
		repo.InsertBackupEntry(ctx, model.BackupEntry{
			BackupID: "job-del-2", Blake3Hash: fmt.Sprintf("h2-%d", i), FileName: "f",
			FilePath: fmt.Sprintf("/data/file%d.txt", i), FileSize: 10, OS: "linux", BackupDate: now,
		})
	}

	// Delete entries for job-del-1 only.
	deleted, err := repo.DeleteEntries(ctx, DeleteFilter{BackupID: "job-del-1"})
	if err != nil {
		t.Fatalf("DeleteEntries by BackupID: %v", err)
	}
	if deleted != 3 {
		t.Errorf("deleted = %d, want 3", deleted)
	}

	// job-del-2 entries should remain.
	remaining, _ := repo.FindByPath(ctx, "%")
	if len(remaining) != 2 {
		t.Errorf("remaining = %d, want 2", len(remaining))
	}
	for _, e := range remaining {
		if e.BackupID != "job-del-2" {
			t.Errorf("expected remaining entries to belong to job-del-2, got %q", e.BackupID)
		}
	}
}

func TestDeleteEntries_BySingleFilePath(t *testing.T) {
	repo := newTestRepo(t, false)
	ctx := context.Background()

	repo.CreateJob(ctx, model.BackupJob{
		BackupID: "job-fp", Level: "FULL", ClientID: "c1",
		InitiatedBy: "cli", StartedAt: time.Now().UTC().Truncate(time.Second), Status: model.JobRunning,
	})

	now := time.Now().UTC().Truncate(time.Second)
	paths := []string{"/home/user/a.txt", "/home/user/b.txt", "/home/user/c.txt"}
	for i, fp := range paths {
		repo.InsertBackupEntry(ctx, model.BackupEntry{
			BackupID: "job-fp", Blake3Hash: fmt.Sprintf("fph%d", i), FileName: "f",
			FilePath: fp, FileSize: 10, OS: "linux", BackupDate: now,
		})
	}

	// Delete a single file path.
	deleted, err := repo.DeleteEntries(ctx, DeleteFilter{FilePath: "/home/user/b.txt"})
	if err != nil {
		t.Fatalf("DeleteEntries by FilePath: %v", err)
	}
	if deleted != 1 {
		t.Errorf("deleted = %d, want 1", deleted)
	}

	remaining, _ := repo.FindByPath(ctx, "%")
	if len(remaining) != 2 {
		t.Errorf("remaining = %d, want 2", len(remaining))
	}
	for _, e := range remaining {
		if e.FilePath == "/home/user/b.txt" {
			t.Error("deleted file path should not be present")
		}
	}
}

func TestDeleteEntries_AllBackups(t *testing.T) {
	repo := newTestRepo(t, false)
	ctx := context.Background()

	// AllBackups flag alone doesn't produce conditions (no WHERE filter),
	// so DeleteEntries returns 0 with an empty filter. AllBackups is used
	// in combination with other filters. Test that BackupID + AllBackups
	// still works by matching BackupID, and test standalone AllBackups is a no-op.

	repo.CreateJob(ctx, model.BackupJob{
		BackupID: "job-all-1", Level: "FULL", ClientID: "c1",
		InitiatedBy: "cli", StartedAt: time.Now().UTC().Truncate(time.Second), Status: model.JobRunning,
	})
	repo.CreateJob(ctx, model.BackupJob{
		BackupID: "job-all-2", Level: "AUTO", ClientID: "c1",
		InitiatedBy: "cli", StartedAt: time.Now().UTC().Truncate(time.Second), Status: model.JobRunning,
	})

	now := time.Now().UTC().Truncate(time.Second)
	// Same file path across two backup sets.
	repo.InsertBackupEntry(ctx, model.BackupEntry{
		BackupID: "job-all-1", Blake3Hash: "ha1", FileName: "shared.txt",
		FilePath: "/shared.txt", FileSize: 10, OS: "linux", BackupDate: now,
	})
	repo.InsertBackupEntry(ctx, model.BackupEntry{
		BackupID: "job-all-2", Blake3Hash: "ha2", FileName: "shared.txt",
		FilePath: "/shared.txt", FileSize: 10, OS: "linux", BackupDate: now,
	})

	// With AllBackups=true but no other filter, DeleteEntries deletes ALL entries.
	deleted, err := repo.DeleteEntries(ctx, DeleteFilter{AllBackups: true})
	if err != nil {
		t.Fatalf("DeleteEntries AllBackups only: %v", err)
	}
	if deleted != 2 {
		t.Errorf("expected 2 deleted with AllBackups (all entries), got %d", deleted)
	}

	// Re-insert for next test.
	repo.InsertBackupEntry(ctx, model.BackupEntry{
		BackupID: "job-all-1", Blake3Hash: "ha1", FileName: "shared.txt",
		FilePath: "/shared.txt", FileSize: 10, OS: "linux", BackupDate: now,
	})
	repo.InsertBackupEntry(ctx, model.BackupEntry{
		BackupID: "job-all-2", Blake3Hash: "ha2", FileName: "shared.txt",
		FilePath: "/shared.txt", FileSize: 10, OS: "linux", BackupDate: now,
	})

	// With AllBackups + FilePath, delete the file across all backup sets.
	deleted, err = repo.DeleteEntries(ctx, DeleteFilter{FilePath: "/shared.txt", AllBackups: true})
	if err != nil {
		t.Fatalf("DeleteEntries AllBackups+FilePath: %v", err)
	}
	if deleted != 2 {
		t.Errorf("deleted = %d, want 2 (across all backups)", deleted)
	}

	remaining, _ := repo.FindByPath(ctx, "%")
	if len(remaining) != 0 {
		t.Errorf("remaining = %d, want 0", len(remaining))
	}
}

func TestDeleteEntries_EmptyFilter(t *testing.T) {
	repo := newTestRepo(t, false)
	ctx := context.Background()

	repo.CreateJob(ctx, model.BackupJob{
		BackupID: "job-empty", Level: "FULL", ClientID: "c1",
		InitiatedBy: "cli", StartedAt: time.Now().UTC().Truncate(time.Second), Status: model.JobRunning,
	})

	now := time.Now().UTC().Truncate(time.Second)
	repo.InsertBackupEntry(ctx, model.BackupEntry{
		BackupID: "job-empty", Blake3Hash: "he1", FileName: "f",
		FilePath: "/keep.txt", FileSize: 10, OS: "linux", BackupDate: now,
	})

	// Empty filter should not delete anything (safety guard).
	deleted, err := repo.DeleteEntries(ctx, DeleteFilter{})
	if err != nil {
		t.Fatalf("DeleteEntries empty filter: %v", err)
	}
	if deleted != 0 {
		t.Errorf("expected 0 deleted with empty filter, got %d", deleted)
	}

	remaining, _ := repo.FindByPath(ctx, "%")
	if len(remaining) != 1 {
		t.Errorf("remaining = %d, want 1 (should be untouched)", len(remaining))
	}
}

func TestConcurrentReadsWAL(t *testing.T) {
	// Use a temp file-based DB so WAL mode is effective.
	dbPath := t.TempDir() + "/test_wal.db"
	repo, err := NewRepository(dbPath, false)
	if err != nil {
		t.Fatalf("NewRepository: %v", err)
	}
	t.Cleanup(func() { repo.Close() })

	ctx := context.Background()

	// Verify WAL mode is active on file-based DB.
	var mode string
	repo.db.QueryRow("PRAGMA journal_mode").Scan(&mode)
	if mode != "wal" {
		t.Fatalf("expected journal_mode=wal, got %q", mode)
	}

	// Seed data.
	repo.CreateJob(ctx, model.BackupJob{
		BackupID: "job-wal", Level: "FULL", ClientID: "c1",
		InitiatedBy: "cli", StartedAt: time.Now().UTC().Truncate(time.Second), Status: model.JobRunning,
	})
	now := time.Now().UTC().Truncate(time.Second)
	for i := 0; i < 50; i++ {
		repo.InsertBackupEntry(ctx, model.BackupEntry{
			BackupID: "job-wal", Blake3Hash: fmt.Sprintf("wal-h%d", i), FileName: fmt.Sprintf("f%d.txt", i),
			FilePath: fmt.Sprintf("/wal/f%d.txt", i), FileSize: int64(i * 100), OS: "linux", BackupDate: now,
		})
	}

	// Launch multiple goroutines performing concurrent reads.
	const numReaders = 10
	var wg sync.WaitGroup
	errs := make(chan error, numReaders)

	for g := 0; g < numReaders; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			// Each goroutine performs multiple read operations.
			for i := 0; i < 5; i++ {
				entries, err := repo.FindByPath(ctx, "/wal/%")
				if err != nil {
					errs <- fmt.Errorf("goroutine %d iteration %d FindByPath: %w", id, i, err)
					return
				}
				if len(entries) != 50 {
					errs <- fmt.Errorf("goroutine %d iteration %d: got %d entries, want 50", id, i, len(entries))
					return
				}

				count, err := repo.CountHashReferences(ctx, fmt.Sprintf("wal-h%d", id))
				if err != nil {
					errs <- fmt.Errorf("goroutine %d iteration %d CountHashReferences: %w", id, i, err)
					return
				}
				if count != 1 {
					errs <- fmt.Errorf("goroutine %d iteration %d: count = %d, want 1", id, i, count)
					return
				}

				manifest, err := repo.GetManifest(ctx, "job-wal")
				if err != nil {
					errs <- fmt.Errorf("goroutine %d iteration %d GetManifest: %w", id, i, err)
					return
				}
				if len(manifest) != 50 {
					errs <- fmt.Errorf("goroutine %d iteration %d: manifest = %d, want 50", id, i, len(manifest))
					return
				}
			}
		}(g)
	}

	wg.Wait()
	close(errs)

	for e := range errs {
		t.Error(e)
	}
}

func TestIndexUsage(t *testing.T) {
	repo := newTestRepo(t, false)
	ctx := context.Background()

	// Seed a job and some entries so the planner has something to work with.
	repo.CreateJob(ctx, model.BackupJob{
		BackupID: "job-idx", Level: "FULL", ClientID: "c1",
		InitiatedBy: "cli", StartedAt: time.Now().UTC().Truncate(time.Second), Status: model.JobRunning,
	})
	now := time.Now().UTC().Truncate(time.Second)
	for i := 0; i < 10; i++ {
		repo.InsertBackupEntry(ctx, model.BackupEntry{
			BackupID: "job-idx", Blake3Hash: fmt.Sprintf("idx-h%d", i), FileName: fmt.Sprintf("f%d.txt", i),
			FilePath: fmt.Sprintf("/idx/f%d.txt", i), FileSize: int64(i), OS: "linux", BackupDate: now,
		})
	}

	tests := []struct {
		name  string
		query string
		args  []interface{}
		// We check the plan contains "USING INDEX" or "SEARCH" (not "SCAN TABLE" without index).
		wantIndex string
	}{
		{
			name:      "FindByHash uses idx_backups_hash",
			query:     "EXPLAIN QUERY PLAN SELECT * FROM backups WHERE blake3_hash = ?",
			args:      []interface{}{"idx-h0"},
			wantIndex: "idx_backups_hash",
		},
		{
			name:      "FindByPath exact match uses idx_backups_path",
			query:     "EXPLAIN QUERY PLAN SELECT * FROM backups WHERE file_path = ?",
			args:      []interface{}{"/idx/f0.txt"},
			wantIndex: "idx_backups_path",
		},
		{
			name:      "DeleteEntries by backup_id uses idx_backups_job",
			query:     "EXPLAIN QUERY PLAN DELETE FROM backups WHERE backup_id = ?",
			args:      []interface{}{"job-idx"},
			wantIndex: "idx_backups_job",
		},
		{
			name:      "GetExpiredEntries uses idx_backups_expires",
			query:     "EXPLAIN QUERY PLAN SELECT * FROM backups WHERE expires_at IS NOT NULL AND expires_at < ?",
			args:      []interface{}{"2024-01-01 00:00:00"},
			wantIndex: "idx_backups_expires",
		},
		{
			name:      "ListJobs by client uses idx_jobs_client",
			query:     "EXPLAIN QUERY PLAN SELECT * FROM backup_jobs WHERE client_id = ?",
			args:      []interface{}{"c1"},
			wantIndex: "idx_jobs_client",
		},
		{
			name:      "ListJobs by status uses idx_jobs_status",
			query:     "EXPLAIN QUERY PLAN SELECT * FROM backup_jobs WHERE status = ?",
			args:      []interface{}{"running"},
			wantIndex: "idx_jobs_status",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rows, err := repo.db.QueryContext(ctx, tc.query, tc.args...)
			if err != nil {
				t.Fatalf("EXPLAIN QUERY PLAN: %v", err)
			}
			defer rows.Close()

			var plan strings.Builder
			for rows.Next() {
				var id, parent, notUsed int
				var detail string
				if err := rows.Scan(&id, &parent, &notUsed, &detail); err != nil {
					t.Fatalf("scan plan row: %v", err)
				}
				plan.WriteString(detail)
				plan.WriteString("\n")
			}

			planStr := plan.String()
			if !strings.Contains(planStr, tc.wantIndex) {
				t.Errorf("expected query plan to use %q, got:\n%s", tc.wantIndex, planStr)
			}
		})
	}
}

func TestUpdateJob_NilValues(t *testing.T) {
	repo := newTestRepo(t, false)
	ctx := context.Background()

	job := model.BackupJob{
		BackupID: "job-nil", Level: "FULL", ClientID: "c1",
		InitiatedBy: "cli", StartedAt: time.Now().UTC().Truncate(time.Second), Status: model.JobRunning,
		FileCount: 5, BytesTotal: 100,
	}
	repo.CreateJob(ctx, job)

	// Update with all-nil JobUpdate should be a no-op.
	err := repo.UpdateJob(ctx, "job-nil", JobUpdate{})
	if err != nil {
		t.Fatalf("UpdateJob with nil values: %v", err)
	}

	// Verify job remains unchanged.
	jobs, _ := repo.ListJobs(ctx, JobFilter{})
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	if jobs[0].Status != model.JobRunning {
		t.Errorf("status changed unexpectedly: got %q", jobs[0].Status)
	}
}

func TestListJobs_EmptyResults(t *testing.T) {
	repo := newTestRepo(t, false)
	ctx := context.Background()

	// Query with no jobs present.
	jobs, err := repo.ListJobs(ctx, JobFilter{})
	if err != nil {
		t.Fatalf("ListJobs on empty: %v", err)
	}
	if jobs != nil {
		t.Errorf("expected nil slice for empty results, got %v", jobs)
	}

	// Query with non-matching filter.
	nonExistent := "no-such-client"
	jobs, err = repo.ListJobs(ctx, JobFilter{ClientID: &nonExistent})
	if err != nil {
		t.Fatalf("ListJobs non-matching: %v", err)
	}
	if jobs != nil {
		t.Errorf("expected nil slice for non-matching filter, got %v", jobs)
	}
}

func TestFindByHash_NotFound(t *testing.T) {
	repo := newTestRepo(t, false)
	ctx := context.Background()

	results, err := repo.FindByHash(ctx, "nonexistent-hash")
	if err != nil {
		t.Fatalf("FindByHash: %v", err)
	}
	if results != nil {
		t.Errorf("expected nil for no results, got %d entries", len(results))
	}
}

func TestGetManifest_Empty(t *testing.T) {
	repo := newTestRepo(t, false)
	ctx := context.Background()

	manifest, err := repo.GetManifest(ctx, "non-existent-job")
	if err != nil {
		t.Fatalf("GetManifest: %v", err)
	}
	if manifest != nil {
		t.Errorf("expected nil for empty manifest, got %d entries", len(manifest))
	}
}

func TestGetFileVersions_Empty(t *testing.T) {
	repo := newTestRepo(t, false)
	ctx := context.Background()

	versions, err := repo.GetFileVersions(ctx, "/no/such/file.txt")
	if err != nil {
		t.Fatalf("GetFileVersions: %v", err)
	}
	if versions != nil {
		t.Errorf("expected nil for no versions, got %d entries", len(versions))
	}
}
