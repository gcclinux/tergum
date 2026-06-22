package retention

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gcclinux/tergum/internal/db"
	"github.com/gcclinux/tergum/internal/model"
	"github.com/gcclinux/tergum/internal/storage"
)

// testSetup creates an in-memory repo (server mode), a CAS store, and a retention engine.
func testSetup(t *testing.T) (*RetentionEngine, *db.SQLiteRepository, *storage.CAS) {
	t.Helper()
	repo, err := db.NewRepository(":memory:", true)
	if err != nil {
		t.Fatalf("NewRepository: %v", err)
	}
	t.Cleanup(func() { repo.Close() })

	storageDir := t.TempDir()
	cas := storage.NewCAS(storageDir, repo)

	engine := New(repo, cas)
	return engine, repo, cas
}

// insertTestJob creates a backup job record required for foreign key.
func insertTestJob(t *testing.T, repo *db.SQLiteRepository, backupID string) {
	t.Helper()
	ctx := context.Background()
	err := repo.CreateJob(ctx, model.BackupJob{
		BackupID:    backupID,
		Level:       "FULL",
		ClientID:    "test-client",
		InitiatedBy: "cli",
		StartedAt:   time.Now(),
		Status:      model.JobCompleted,
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
}

// insertTestEntry inserts a backup entry with the given parameters.
func insertTestEntry(t *testing.T, repo *db.SQLiteRepository, backupID, hash, filePath string, fileSize int64, backupDate time.Time) {
	t.Helper()
	ctx := context.Background()
	entry := model.BackupEntry{
		BackupID:   backupID,
		Blake3Hash: hash,
		FileName:   filePath[strings.LastIndex(filePath, "/")+1:],
		FilePath:   filePath,
		FileSize:   fileSize,
		OS:         "linux",
		BackupDate: backupDate,
	}
	if err := repo.InsertBackupEntry(ctx, entry); err != nil {
		t.Fatalf("InsertBackupEntry: %v", err)
	}
}

func TestEvaluate_NoPolicies(t *testing.T) {
	engine, repo, _ := testSetup(t)
	ctx := context.Background()

	// Insert multiple versions of a file.
	insertTestJob(t, repo, "job1")
	insertTestJob(t, repo, "job2")
	now := time.Now()
	insertTestEntry(t, repo, "job1", "aaaa"+strings.Repeat("a", 60), "/data/file.txt", 100, now.Add(-48*time.Hour))
	insertTestEntry(t, repo, "job2", "bbbb"+strings.Repeat("b", 60), "/data/file.txt", 200, now)

	// No policies — should not expire anything.
	result, err := engine.Evaluate(ctx, false)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	if result.EntriesExpired != 0 {
		t.Errorf("expected 0 expired, got %d", result.EntriesExpired)
	}
	if result.EntriesEvaluated != 2 {
		t.Errorf("expected 2 evaluated, got %d", result.EntriesEvaluated)
	}
	if result.Protected != 1 {
		t.Errorf("expected 1 protected, got %d", result.Protected)
	}
}

func TestEvaluate_ProtectsLatestVersion(t *testing.T) {
	engine, repo, _ := testSetup(t)
	ctx := context.Background()

	insertTestJob(t, repo, "job1")
	insertTestJob(t, repo, "job2")
	insertTestJob(t, repo, "job3")
	now := time.Now()

	// Three versions: latest should always be protected.
	insertTestEntry(t, repo, "job1", "aaaa"+strings.Repeat("a", 60), "/data/file.txt", 100, now.Add(-100*24*time.Hour))
	insertTestEntry(t, repo, "job2", "bbbb"+strings.Repeat("b", 60), "/data/file.txt", 200, now.Add(-50*24*time.Hour))
	insertTestEntry(t, repo, "job3", "cccc"+strings.Repeat("c", 60), "/data/file.txt", 300, now)

	// Policy that would expire everything older than 1 day, keep 1 version.
	keepDays := 1
	err := engine.AddPolicy(ctx, model.RetentionPolicy{
		Name:         "aggressive",
		KeepDays:     &keepDays,
		KeepVersions: 1,
		Pattern:      "*",
		Priority:     10,
		Enabled:      true,
	})
	if err != nil {
		t.Fatalf("AddPolicy: %v", err)
	}

	engine.SetClock(func() time.Time { return now })

	result, err := engine.Evaluate(ctx, false)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	// Oldest two should be expired, latest protected.
	if result.EntriesExpired != 2 {
		t.Errorf("expected 2 expired, got %d", result.EntriesExpired)
	}
	if result.Protected != 1 {
		t.Errorf("expected 1 protected, got %d", result.Protected)
	}

	// Verify latest version still exists.
	versions, err := repo.GetFileVersions(ctx, "/data/file.txt")
	if err != nil {
		t.Fatalf("GetFileVersions: %v", err)
	}
	if len(versions) != 1 {
		t.Fatalf("expected 1 remaining version, got %d", len(versions))
	}
	if versions[0].Blake3Hash != "cccc"+strings.Repeat("c", 60) {
		t.Error("expected latest version to be the remaining one")
	}
}

func TestEvaluate_SkipsSingleVersionFiles(t *testing.T) {
	engine, repo, _ := testSetup(t)
	ctx := context.Background()

	insertTestJob(t, repo, "job1")
	now := time.Now()

	// Single version file.
	insertTestEntry(t, repo, "job1", "aaaa"+strings.Repeat("a", 60), "/data/only.txt", 100, now.Add(-100*24*time.Hour))

	// Aggressive policy.
	keepDays := 1
	err := engine.AddPolicy(ctx, model.RetentionPolicy{
		Name:         "aggressive",
		KeepDays:     &keepDays,
		KeepVersions: 1,
		Pattern:      "*",
		Priority:     10,
		Enabled:      true,
	})
	if err != nil {
		t.Fatalf("AddPolicy: %v", err)
	}

	engine.SetClock(func() time.Time { return now })

	result, err := engine.Evaluate(ctx, false)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	// Single-version file should never be touched.
	if result.EntriesExpired != 0 {
		t.Errorf("expected 0 expired for single-version, got %d", result.EntriesExpired)
	}
	if result.Protected != 1 {
		t.Errorf("expected 1 protected, got %d", result.Protected)
	}

	// Verify file still exists.
	versions, err := repo.GetFileVersions(ctx, "/data/only.txt")
	if err != nil {
		t.Fatalf("GetFileVersions: %v", err)
	}
	if len(versions) != 1 {
		t.Errorf("expected 1 version still exists, got %d", len(versions))
	}
}

func TestEvaluate_PolicyMatchingKeepDaysAndKeepVersions(t *testing.T) {
	engine, repo, _ := testSetup(t)
	ctx := context.Background()

	insertTestJob(t, repo, "job1")
	insertTestJob(t, repo, "job2")
	insertTestJob(t, repo, "job3")
	insertTestJob(t, repo, "job4")
	now := time.Now()

	// 4 versions: latest + 3 older.
	insertTestEntry(t, repo, "job1", "aaaa"+strings.Repeat("a", 60), "/docs/report.txt", 100, now.Add(-90*24*time.Hour))
	insertTestEntry(t, repo, "job2", "bbbb"+strings.Repeat("b", 60), "/docs/report.txt", 200, now.Add(-60*24*time.Hour))
	insertTestEntry(t, repo, "job3", "cccc"+strings.Repeat("c", 60), "/docs/report.txt", 300, now.Add(-20*24*time.Hour))
	insertTestEntry(t, repo, "job4", "dddd"+strings.Repeat("d", 60), "/docs/report.txt", 400, now)

	// Policy: keep 30 days, keep 2 versions.
	keepDays := 30
	err := engine.AddPolicy(ctx, model.RetentionPolicy{
		Name:         "docs-policy",
		KeepDays:     &keepDays,
		KeepVersions: 2,
		Pattern:      "/docs/*",
		Priority:     5,
		Enabled:      true,
	})
	if err != nil {
		t.Fatalf("AddPolicy: %v", err)
	}

	engine.SetClock(func() time.Time { return now })

	result, err := engine.Evaluate(ctx, false)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	// Versions at -90d and -60d are older than 30 days AND totalVersions(4) > keep_versions(2).
	// Version at -20d is NOT older than 30 days.
	if result.EntriesExpired != 2 {
		t.Errorf("expected 2 expired, got %d", result.EntriesExpired)
	}

	// Verify remaining versions.
	versions, err := repo.GetFileVersions(ctx, "/docs/report.txt")
	if err != nil {
		t.Fatalf("GetFileVersions: %v", err)
	}
	if len(versions) != 2 {
		t.Fatalf("expected 2 remaining versions, got %d", len(versions))
	}
}

func TestEvaluate_FirstMatchWinsPriority(t *testing.T) {
	engine, repo, _ := testSetup(t)
	ctx := context.Background()

	insertTestJob(t, repo, "job1")
	insertTestJob(t, repo, "job2")
	now := time.Now()

	// Two versions.
	insertTestEntry(t, repo, "job1", "aaaa"+strings.Repeat("a", 60), "/docs/important.txt", 100, now.Add(-60*24*time.Hour))
	insertTestEntry(t, repo, "job2", "bbbb"+strings.Repeat("b", 60), "/docs/important.txt", 200, now)

	// High priority policy: keep 90 days (won't expire).
	keepDays90 := 90
	err := engine.AddPolicy(ctx, model.RetentionPolicy{
		Name:         "high-priority-keep",
		KeepDays:     &keepDays90,
		KeepVersions: 1,
		Pattern:      "/docs/*",
		Priority:     100,
		Enabled:      true,
	})
	if err != nil {
		t.Fatalf("AddPolicy high: %v", err)
	}

	// Low priority policy: keep 1 day (would expire if matched).
	keepDays1 := 1
	err = engine.AddPolicy(ctx, model.RetentionPolicy{
		Name:         "low-priority-expire",
		KeepDays:     &keepDays1,
		KeepVersions: 1,
		Pattern:      "/docs/*",
		Priority:     1,
		Enabled:      true,
	})
	if err != nil {
		t.Fatalf("AddPolicy low: %v", err)
	}

	engine.SetClock(func() time.Time { return now })

	result, err := engine.Evaluate(ctx, false)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	// High priority matches first (keep 90 days). Entry at -60d < 90 days, so not expired.
	if result.EntriesExpired != 0 {
		t.Errorf("expected 0 expired (high priority protects), got %d", result.EntriesExpired)
	}

	// Verify both versions still exist.
	versions, err := repo.GetFileVersions(ctx, "/docs/important.txt")
	if err != nil {
		t.Fatalf("GetFileVersions: %v", err)
	}
	if len(versions) != 2 {
		t.Errorf("expected 2 versions, got %d", len(versions))
	}
}

func TestEvaluate_DryRunDoesNotModifyDB(t *testing.T) {
	engine, repo, _ := testSetup(t)
	ctx := context.Background()

	insertTestJob(t, repo, "job1")
	insertTestJob(t, repo, "job2")
	now := time.Now()

	insertTestEntry(t, repo, "job1", "aaaa"+strings.Repeat("a", 60), "/data/file.txt", 100, now.Add(-100*24*time.Hour))
	insertTestEntry(t, repo, "job2", "bbbb"+strings.Repeat("b", 60), "/data/file.txt", 200, now)

	// Aggressive policy.
	keepDays := 1
	err := engine.AddPolicy(ctx, model.RetentionPolicy{
		Name:         "aggressive",
		KeepDays:     &keepDays,
		KeepVersions: 1,
		Pattern:      "*",
		Priority:     10,
		Enabled:      true,
	})
	if err != nil {
		t.Fatalf("AddPolicy: %v", err)
	}

	engine.SetClock(func() time.Time { return now })

	// Run in dry-run mode.
	result, err := engine.Evaluate(ctx, true)
	if err != nil {
		t.Fatalf("Evaluate dry-run: %v", err)
	}

	// Should report expiration.
	if result.EntriesExpired != 1 {
		t.Errorf("expected 1 expired in dry-run, got %d", result.EntriesExpired)
	}

	// DB should not be modified.
	versions, err := repo.GetFileVersions(ctx, "/data/file.txt")
	if err != nil {
		t.Fatalf("GetFileVersions: %v", err)
	}
	if len(versions) != 2 {
		t.Errorf("expected 2 versions still in DB after dry-run, got %d", len(versions))
	}
}

func TestEvaluate_PhysicalFileDeletedOnlyWhenRefcountZero(t *testing.T) {
	engine, repo, cas := testSetup(t)
	ctx := context.Background()

	insertTestJob(t, repo, "job1")
	insertTestJob(t, repo, "job2")
	insertTestJob(t, repo, "job3")
	now := time.Now()

	sharedHash := "aaaa" + strings.Repeat("a", 60)
	uniqueHash := "bbbb" + strings.Repeat("b", 60)

	// Two files share the same hash (dedup).
	insertTestEntry(t, repo, "job1", sharedHash, "/data/file1.txt", 100, now.Add(-100*24*time.Hour))
	insertTestEntry(t, repo, "job2", sharedHash, "/data/file1.txt", 100, now)
	// Another file references the same hash but different path.
	insertTestEntry(t, repo, "job3", sharedHash, "/other/file2.txt", 100, now)

	// Also create a file with a unique hash that will be expired.
	insertTestJob(t, repo, "job4")
	insertTestJob(t, repo, "job5")
	insertTestEntry(t, repo, "job4", uniqueHash, "/data/unique.txt", 200, now.Add(-100*24*time.Hour))
	insertTestEntry(t, repo, "job5", "cccc"+strings.Repeat("c", 60), "/data/unique.txt", 300, now)

	// Store both physical files.
	if err := cas.Put(ctx, sharedHash, strings.NewReader("shared content")); err != nil {
		t.Fatalf("Put shared: %v", err)
	}
	if err := cas.Put(ctx, uniqueHash, strings.NewReader("unique content")); err != nil {
		t.Fatalf("Put unique: %v", err)
	}

	// Aggressive policy.
	keepDays := 1
	err := engine.AddPolicy(ctx, model.RetentionPolicy{
		Name:         "aggressive",
		KeepDays:     &keepDays,
		KeepVersions: 1,
		Pattern:      "*",
		Priority:     10,
		Enabled:      true,
	})
	if err != nil {
		t.Fatalf("AddPolicy: %v", err)
	}

	engine.SetClock(func() time.Time { return now })

	result, err := engine.Evaluate(ctx, false)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	// file1.txt old version expired, unique.txt old version expired.
	if result.EntriesExpired != 2 {
		t.Errorf("expected 2 expired, got %d", result.EntriesExpired)
	}

	// sharedHash still referenced by /other/file2.txt — should NOT be deleted.
	exists, err := cas.Exists(ctx, sharedHash)
	if err != nil {
		t.Fatalf("Exists shared: %v", err)
	}
	if !exists {
		t.Error("shared hash file should still exist (refcount > 0)")
	}

	// uniqueHash: after deleting the old entry, the only remaining reference
	// for unique.txt is the new version with hash "cccc..." which is different.
	// So uniqueHash refcount should be 0, physical file deleted.
	exists, err = cas.Exists(ctx, uniqueHash)
	if err != nil {
		t.Fatalf("Exists unique: %v", err)
	}
	if exists {
		t.Error("unique hash file should be deleted (refcount == 0)")
	}

	if result.FilesDeleted != 1 {
		t.Errorf("expected 1 physical file deleted, got %d", result.FilesDeleted)
	}
}

func TestAddRemoveListPolicies(t *testing.T) {
	engine, _, _ := testSetup(t)
	ctx := context.Background()

	// Initially empty.
	policies, err := engine.ListPolicies(ctx)
	if err != nil {
		t.Fatalf("ListPolicies: %v", err)
	}
	if len(policies) != 0 {
		t.Errorf("expected 0 policies, got %d", len(policies))
	}

	// Add policies.
	keepDays30 := 30
	keepDays7 := 7
	err = engine.AddPolicy(ctx, model.RetentionPolicy{
		Name:         "policy-a",
		KeepDays:     &keepDays30,
		KeepVersions: 3,
		Pattern:      "/docs/*",
		Priority:     10,
		Enabled:      true,
	})
	if err != nil {
		t.Fatalf("AddPolicy a: %v", err)
	}

	err = engine.AddPolicy(ctx, model.RetentionPolicy{
		Name:         "policy-b",
		KeepDays:     &keepDays7,
		KeepVersions: 1,
		Pattern:      "/tmp/*",
		Priority:     5,
		Enabled:      true,
	})
	if err != nil {
		t.Fatalf("AddPolicy b: %v", err)
	}

	// List policies (should be sorted by priority DESC).
	policies, err = engine.ListPolicies(ctx)
	if err != nil {
		t.Fatalf("ListPolicies: %v", err)
	}
	if len(policies) != 2 {
		t.Fatalf("expected 2 policies, got %d", len(policies))
	}
	if policies[0].Name != "policy-a" {
		t.Errorf("expected first policy to be policy-a (higher priority), got %s", policies[0].Name)
	}
	if policies[1].Name != "policy-b" {
		t.Errorf("expected second policy to be policy-b (lower priority), got %s", policies[1].Name)
	}
	if policies[0].KeepVersions != 3 {
		t.Errorf("expected policy-a keep_versions=3, got %d", policies[0].KeepVersions)
	}

	// Remove a policy.
	err = engine.RemovePolicy(ctx, "policy-a")
	if err != nil {
		t.Fatalf("RemovePolicy: %v", err)
	}

	policies, err = engine.ListPolicies(ctx)
	if err != nil {
		t.Fatalf("ListPolicies after remove: %v", err)
	}
	if len(policies) != 1 {
		t.Errorf("expected 1 policy after removal, got %d", len(policies))
	}
	if policies[0].Name != "policy-b" {
		t.Errorf("expected remaining policy to be policy-b, got %s", policies[0].Name)
	}

	// Remove nonexistent policy should error.
	err = engine.RemovePolicy(ctx, "nonexistent")
	if err == nil {
		t.Error("expected error removing nonexistent policy")
	}
}

func TestEvaluate_KeepVersionsWithNoKeepDays(t *testing.T) {
	engine, repo, _ := testSetup(t)
	ctx := context.Background()

	now := time.Now()

	// Create 5 versions.
	for i := 0; i < 5; i++ {
		jobID := fmt.Sprintf("job%d", i)
		insertTestJob(t, repo, jobID)
		hash := fmt.Sprintf("%04x", i) + strings.Repeat("a", 60)
		insertTestEntry(t, repo, jobID, hash, "/data/multi.txt", 100, now.Add(-time.Duration(5-i)*24*time.Hour))
	}

	// Policy: nil keep_days (keep forever by time), but keep_versions = 3.
	// Per design: expire if backup_date + keep_days < now AND exceeds keep_versions.
	// With nil keep_days, time condition is never satisfied, so nothing is expired.
	err := engine.AddPolicy(ctx, model.RetentionPolicy{
		Name:         "versions-only",
		KeepDays:     nil,
		KeepVersions: 3,
		Pattern:      "*",
		Priority:     10,
		Enabled:      true,
	})
	if err != nil {
		t.Fatalf("AddPolicy: %v", err)
	}

	engine.SetClock(func() time.Time { return now })

	result, err := engine.Evaluate(ctx, false)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	// nil KeepDays means "keep forever" — time condition never met, nothing expired.
	if result.EntriesExpired != 0 {
		t.Errorf("expected 0 expired (nil keep_days = forever), got %d", result.EntriesExpired)
	}

	versions, err := repo.GetFileVersions(ctx, "/data/multi.txt")
	if err != nil {
		t.Fatalf("GetFileVersions: %v", err)
	}
	if len(versions) != 5 {
		t.Errorf("expected 5 remaining versions (nil keep_days keeps all), got %d", len(versions))
	}
}

func TestEvaluate_DisabledPolicyIgnored(t *testing.T) {
	engine, repo, _ := testSetup(t)
	ctx := context.Background()

	insertTestJob(t, repo, "job1")
	insertTestJob(t, repo, "job2")
	now := time.Now()

	insertTestEntry(t, repo, "job1", "aaaa"+strings.Repeat("a", 60), "/data/file.txt", 100, now.Add(-100*24*time.Hour))
	insertTestEntry(t, repo, "job2", "bbbb"+strings.Repeat("b", 60), "/data/file.txt", 200, now)

	// Disabled policy.
	keepDays := 1
	err := engine.AddPolicy(ctx, model.RetentionPolicy{
		Name:         "disabled-policy",
		KeepDays:     &keepDays,
		KeepVersions: 1,
		Pattern:      "*",
		Priority:     10,
		Enabled:      false,
	})
	if err != nil {
		t.Fatalf("AddPolicy: %v", err)
	}

	engine.SetClock(func() time.Time { return now })

	result, err := engine.Evaluate(ctx, false)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	// Disabled policy should be ignored.
	if result.EntriesExpired != 0 {
		t.Errorf("expected 0 expired (policy disabled), got %d", result.EntriesExpired)
	}
}

func TestEvaluate_PurgeModeKeepVersionsZero(t *testing.T) {
	engine, repo, cas := testSetup(t)
	ctx := context.Background()

	insertTestJob(t, repo, "job1")
	insertTestJob(t, repo, "job2")
	insertTestJob(t, repo, "job3")
	now := time.Now()

	hash1 := "aaaa" + strings.Repeat("a", 60)
	hash2 := "bbbb" + strings.Repeat("b", 60)
	hash3 := "cccc" + strings.Repeat("c", 60)

	// Three versions: all older than 7 days.
	insertTestEntry(t, repo, "job1", hash1, "/tmp/cache/data.bin", 100, now.Add(-30*24*time.Hour))
	insertTestEntry(t, repo, "job2", hash2, "/tmp/cache/data.bin", 200, now.Add(-14*24*time.Hour))
	insertTestEntry(t, repo, "job3", hash3, "/tmp/cache/data.bin", 300, now.Add(-10*24*time.Hour))

	// Store physical files.
	if err := cas.Put(ctx, hash1, strings.NewReader("content1")); err != nil {
		t.Fatalf("Put hash1: %v", err)
	}
	if err := cas.Put(ctx, hash2, strings.NewReader("content2")); err != nil {
		t.Fatalf("Put hash2: %v", err)
	}
	if err := cas.Put(ctx, hash3, strings.NewReader("content3")); err != nil {
		t.Fatalf("Put hash3: %v", err)
	}

	// Purge policy: keep_versions=0, keep_days=7. Pattern matches the path.
	keepDays := 7
	err := engine.AddPolicy(ctx, model.RetentionPolicy{
		Name:         "purge-tmp",
		KeepDays:     &keepDays,
		KeepVersions: 0,
		Pattern:      "/tmp/cache/*",
		Priority:     10,
		Enabled:      true,
	})
	if err != nil {
		t.Fatalf("AddPolicy: %v", err)
	}

	engine.SetClock(func() time.Time { return now })

	result, err := engine.Evaluate(ctx, false)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	// ALL three versions are older than 7 days and keep_versions=0,
	// so all should be expired (including the latest).
	if result.EntriesExpired != 3 {
		t.Errorf("expected 3 expired (purge mode), got %d", result.EntriesExpired)
	}

	// No versions should remain.
	versions, err := repo.GetFileVersions(ctx, "/tmp/cache/data.bin")
	if err != nil {
		t.Fatalf("GetFileVersions: %v", err)
	}
	if len(versions) != 0 {
		t.Errorf("expected 0 remaining versions in purge mode, got %d", len(versions))
	}

	// All physical files should be deleted (each hash is unique, refcount = 0).
	for _, hash := range []string{hash1, hash2, hash3} {
		exists, err := cas.Exists(ctx, hash)
		if err != nil {
			t.Fatalf("Exists %s: %v", hash[:8], err)
		}
		if exists {
			t.Errorf("hash %s should be deleted from storage in purge mode", hash[:8])
		}
	}

	if result.FilesDeleted != 3 {
		t.Errorf("expected 3 physical files deleted, got %d", result.FilesDeleted)
	}

	// Protected count should be 0 (purge mode doesn't protect anything).
	if result.Protected != 0 {
		t.Errorf("expected 0 protected in purge mode, got %d", result.Protected)
	}
}

func TestEvaluate_PurgeModeRecentVersionsKept(t *testing.T) {
	engine, repo, _ := testSetup(t)
	ctx := context.Background()

	insertTestJob(t, repo, "job1")
	insertTestJob(t, repo, "job2")
	insertTestJob(t, repo, "job3")
	now := time.Now()

	// Three versions: some are newer than 7 days.
	insertTestEntry(t, repo, "job1", "aaaa"+strings.Repeat("a", 60), "/tmp/cache/recent.bin", 100, now.Add(-30*24*time.Hour))
	insertTestEntry(t, repo, "job2", "bbbb"+strings.Repeat("b", 60), "/tmp/cache/recent.bin", 200, now.Add(-3*24*time.Hour)) // within 7 days
	insertTestEntry(t, repo, "job3", "cccc"+strings.Repeat("c", 60), "/tmp/cache/recent.bin", 300, now.Add(-1*24*time.Hour)) // within 7 days

	// Purge policy: keep_versions=0, keep_days=7.
	keepDays := 7
	err := engine.AddPolicy(ctx, model.RetentionPolicy{
		Name:         "purge-tmp",
		KeepDays:     &keepDays,
		KeepVersions: 0,
		Pattern:      "/tmp/cache/*",
		Priority:     10,
		Enabled:      true,
	})
	if err != nil {
		t.Fatalf("AddPolicy: %v", err)
	}

	engine.SetClock(func() time.Time { return now })

	result, err := engine.Evaluate(ctx, true) // dry run
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	// Only the oldest version (30 days old) should expire.
	// The other two are within the 7-day window.
	if result.EntriesExpired != 1 {
		t.Errorf("expected 1 expired in purge mode (only old one), got %d", result.EntriesExpired)
	}
}

func TestEvaluate_PurgeModeSingleVersionFile(t *testing.T) {
	engine, repo, cas := testSetup(t)
	ctx := context.Background()

	insertTestJob(t, repo, "job1")
	now := time.Now()

	hash := "aaaa" + strings.Repeat("a", 60)

	// Single version, older than keep_days.
	insertTestEntry(t, repo, "job1", hash, "/tmp/cache/single.bin", 100, now.Add(-30*24*time.Hour))

	// Store physical file.
	if err := cas.Put(ctx, hash, strings.NewReader("content")); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// Purge policy: keep_versions=0.
	keepDays := 7
	err := engine.AddPolicy(ctx, model.RetentionPolicy{
		Name:         "purge-tmp",
		KeepDays:     &keepDays,
		KeepVersions: 0,
		Pattern:      "/tmp/cache/*",
		Priority:     10,
		Enabled:      true,
	})
	if err != nil {
		t.Fatalf("AddPolicy: %v", err)
	}

	engine.SetClock(func() time.Time { return now })

	result, err := engine.Evaluate(ctx, false)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	// In purge mode, single-version files ARE deleted (unlike standard mode).
	if result.EntriesExpired != 1 {
		t.Errorf("expected 1 expired (single file, purge mode), got %d", result.EntriesExpired)
	}

	// Physical file should be gone.
	exists, err := cas.Exists(ctx, hash)
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if exists {
		t.Error("physical file should be deleted in purge mode for single-version file")
	}

	if result.FilesDeleted != 1 {
		t.Errorf("expected 1 file deleted, got %d", result.FilesDeleted)
	}

	// Protected should be 0.
	if result.Protected != 0 {
		t.Errorf("expected 0 protected in purge mode, got %d", result.Protected)
	}
}
