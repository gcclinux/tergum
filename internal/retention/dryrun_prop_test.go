package retention

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gcclinux/tergum/internal/db"
	"github.com/gcclinux/tergum/internal/deletion"
	"github.com/gcclinux/tergum/internal/model"
	"github.com/gcclinux/tergum/internal/storage"
	"pgregory.net/rapid"
)

// **Validates: Requirements 9.7, 10.6, 13.4**

// dryRunSetup creates an in-memory repo, a temp CAS store, and both engines.
func dryRunSetup(t *rapid.T) (*RetentionEngine, *db.SQLiteRepository, *storage.CAS, string, func()) {
	t.Helper()
	repo, err := db.NewRepository(":memory:", true)
	if err != nil {
		t.Fatalf("NewRepository: %v", err)
	}

	storageDir, err := os.MkdirTemp("", "dryrun-prop-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}

	cas := storage.NewCAS(storageDir, repo)
	engine := New(repo, cas)

	cleanup := func() {
		repo.Close()
		os.RemoveAll(storageDir)
	}
	return engine, repo, cas, storageDir, cleanup
}

// dbSnapshot captures the full state of the backup entries table.
type dbSnapshot struct {
	entries []model.BackupEntry
}

func captureDBSnapshot(t *rapid.T, repo *db.SQLiteRepository) dbSnapshot {
	t.Helper()
	ctx := context.Background()
	entries, err := repo.QueryEntries(ctx, db.DeleteFilter{AllBackups: true})
	if err != nil {
		// If AllBackups without other filters returns nothing, query all backup IDs.
		t.Fatalf("QueryEntries snapshot: %v", err)
	}
	return dbSnapshot{entries: entries}
}

func (s dbSnapshot) count() int {
	return len(s.entries)
}

func (s dbSnapshot) equalTo(other dbSnapshot) bool {
	if len(s.entries) != len(other.entries) {
		return false
	}
	// Compare by ID, hash, file_path, file_size since those are the key attributes.
	sMap := make(map[int64]model.BackupEntry)
	for _, e := range s.entries {
		sMap[e.ID] = e
	}
	for _, e := range other.entries {
		existing, ok := sMap[e.ID]
		if !ok {
			return false
		}
		if existing.Blake3Hash != e.Blake3Hash ||
			existing.FilePath != e.FilePath ||
			existing.FileSize != e.FileSize ||
			existing.BackupID != e.BackupID {
			return false
		}
	}
	return true
}

// casSnapshot captures which hashes exist and their content.
type casSnapshot struct {
	files map[string][]byte
}

func captureCASSnapshot(t *rapid.T, cas *storage.CAS, hashes []string) casSnapshot {
	t.Helper()
	ctx := context.Background()
	snap := casSnapshot{files: make(map[string][]byte)}
	for _, h := range hashes {
		exists, err := cas.Exists(ctx, h)
		if err != nil {
			t.Fatalf("CAS Exists(%s): %v", h, err)
		}
		if exists {
			rc, err := cas.Get(ctx, h)
			if err != nil {
				t.Fatalf("CAS Get(%s): %v", h, err)
			}
			data, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				t.Fatalf("ReadAll(%s): %v", h, err)
			}
			snap.files[h] = data
		}
	}
	return snap
}

func (s casSnapshot) equalTo(other casSnapshot) bool {
	if len(s.files) != len(other.files) {
		return false
	}
	for h, data := range s.files {
		otherData, ok := other.files[h]
		if !ok {
			return false
		}
		if !bytes.Equal(data, otherData) {
			return false
		}
	}
	return true
}

// genSmallHash generates a deterministic 64-char hex hash from an index.
func genSmallHash(idx int) string {
	base := fmt.Sprintf("%064x", idx+1)
	return base[:64]
}

func TestProperty_RetentionDryRunIdempotence(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		engine, repo, cas, _, cleanup := dryRunSetup(rt)
		defer cleanup()
		ctx := context.Background()

		now := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
		engine.SetClock(func() time.Time { return now })

		// Generate 5-15 backup entries across a few file paths to create versions.
		numFiles := rapid.IntRange(2, 5).Draw(rt, "numFiles")
		versionsPerFile := rapid.IntRange(2, 3).Draw(rt, "versionsPerFile")

		var allHashes []string
		jobCounter := 0

		for f := 0; f < numFiles; f++ {
			filePath := fmt.Sprintf("/data/dir%d/file%d.txt", f%3, f)

			for v := 0; v < versionsPerFile; v++ {
				jobID := fmt.Sprintf("job-%d", jobCounter)
				jobCounter++

				err := repo.CreateJob(ctx, model.BackupJob{
					BackupID:    jobID,
					Level:       "FULL",
					ClientID:    "test-client",
					InitiatedBy: "cli",
					StartedAt:   now.Add(-time.Duration(100-jobCounter) * 24 * time.Hour),
					Status:      model.JobCompleted,
				})
				if err != nil {
					rt.Fatalf("CreateJob: %v", err)
				}

				hash := genSmallHash(jobCounter)
				allHashes = append(allHashes, hash)

				// Compute backup date: spread versions across time.
				daysAgo := rapid.IntRange(1, 180).Draw(rt, fmt.Sprintf("daysAgo_%d_%d", f, v))
				backupDate := now.Add(-time.Duration(daysAgo) * 24 * time.Hour)

				entry := model.BackupEntry{
					BackupID:   jobID,
					Blake3Hash: hash,
					FileName:   fmt.Sprintf("file%d.txt", f),
					FilePath:   filePath,
					FileSize:   int64(rapid.IntRange(100, 5000).Draw(rt, fmt.Sprintf("size_%d_%d", f, v))),
					OS:         "linux",
					BackupDate: backupDate,
				}
				if err := repo.InsertBackupEntry(ctx, entry); err != nil {
					rt.Fatalf("InsertBackupEntry: %v", err)
				}

				// Store data in CAS for each hash.
				content := fmt.Sprintf("content-for-%s", hash)
				if err := cas.Put(ctx, hash, strings.NewReader(content)); err != nil {
					rt.Fatalf("CAS Put: %v", err)
				}
			}
		}

		// Add a retention policy that will match and expire some entries.
		keepDays := rapid.IntRange(7, 60).Draw(rt, "keepDays")
		keepVersions := rapid.IntRange(1, 2).Draw(rt, "keepVersions")
		err := engine.AddPolicy(ctx, model.RetentionPolicy{
			Name:         "test-policy",
			KeepDays:     &keepDays,
			KeepVersions: keepVersions,
			Pattern:      "", // matches everything
			Priority:     10,
			Enabled:      true,
		})
		if err != nil {
			rt.Fatalf("AddPolicy: %v", err)
		}

		// --- Capture state before dry-run ---
		dbBefore := captureDBSnapshot(rt, repo)
		casBefore := captureCASSnapshot(rt, cas, allHashes)

		// --- Execute dry-run ---
		dryResult, err := engine.Evaluate(ctx, true)
		if err != nil {
			rt.Fatalf("Evaluate(dryRun=true): %v", err)
		}

		// --- Verify state unchanged after dry-run ---
		dbAfter := captureDBSnapshot(rt, repo)
		casAfter := captureCASSnapshot(rt, cas, allHashes)

		if !dbBefore.equalTo(dbAfter) {
			rt.Fatalf("property violation: DB state changed after dry-run (before=%d entries, after=%d entries)",
				dbBefore.count(), dbAfter.count())
		}
		if !casBefore.equalTo(casAfter) {
			rt.Fatalf("property violation: CAS state changed after dry-run")
		}

		// --- Execute real run ---
		realResult, err := engine.Evaluate(ctx, false)
		if err != nil {
			rt.Fatalf("Evaluate(dryRun=false): %v", err)
		}

		// --- Verify dry-run result accurately predicted what happened ---
		if dryResult.EntriesExpired != realResult.EntriesExpired {
			rt.Fatalf("property violation: dry-run reported %d entries expired, actual was %d",
				dryResult.EntriesExpired, realResult.EntriesExpired)
		}
	})
}

// deletionRepoAdapter adapts db.SQLiteRepository to the deletion.Repository interface.
type deletionRepoAdapter struct {
	repo *db.SQLiteRepository
}

func (a *deletionRepoAdapter) QueryEntries(ctx context.Context, filter deletion.Filter) ([]model.BackupEntry, error) {
	return a.repo.QueryEntries(ctx, db.DeleteFilter{
		BackupID:   filter.BackupID,
		FolderPath: filter.FolderPath,
		FilePath:   filter.FilePath,
		AllBackups: filter.AllBackups,
	})
}

func (a *deletionRepoAdapter) DeleteEntries(ctx context.Context, filter deletion.Filter) (int64, error) {
	return a.repo.DeleteEntries(ctx, db.DeleteFilter{
		BackupID:   filter.BackupID,
		FolderPath: filter.FolderPath,
		FilePath:   filter.FilePath,
		AllBackups: filter.AllBackups,
	})
}

func (a *deletionRepoAdapter) CountHashReferences(ctx context.Context, hash string) (int64, error) {
	return a.repo.CountHashReferences(ctx, hash)
}

func (a *deletionRepoAdapter) DeleteOrphanJobs(ctx context.Context) (int64, error) {
	return a.repo.DeleteOrphanJobs(ctx)
}

func TestProperty_DeletionDryRunIdempotence(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		_, repo, cas, _, cleanup := dryRunSetup(rt)
		defer cleanup()
		ctx := context.Background()

		// Create the deletion engine using the same repo and store.
		adapter := &deletionRepoAdapter{repo: repo}
		delEngine := deletion.New(adapter, cas)

		// Generate 5-10 entries across 2-3 backup IDs.
		numBackups := rapid.IntRange(2, 3).Draw(rt, "numBackups")
		entriesPerBackup := rapid.IntRange(2, 5).Draw(rt, "entriesPerBackup")

		var allHashes []string
		backupIDs := make([]string, numBackups)

		for b := 0; b < numBackups; b++ {
			backupID := fmt.Sprintf("backup-%d", b)
			backupIDs[b] = backupID

			err := repo.CreateJob(ctx, model.BackupJob{
				BackupID:    backupID,
				Level:       "FULL",
				ClientID:    "test-client",
				InitiatedBy: "cli",
				StartedAt:   time.Now(),
				Status:      model.JobCompleted,
			})
			if err != nil {
				rt.Fatalf("CreateJob: %v", err)
			}

			for e := 0; e < entriesPerBackup; e++ {
				hash := genSmallHash(b*entriesPerBackup + e + 100)
				allHashes = append(allHashes, hash)

				entry := model.BackupEntry{
					BackupID:   backupID,
					Blake3Hash: hash,
					FileName:   fmt.Sprintf("file%d.txt", e),
					FilePath:   fmt.Sprintf("/data/dir%d/file%d.txt", b, e),
					FileSize:   int64(rapid.IntRange(100, 5000).Draw(rt, fmt.Sprintf("delSize_%d_%d", b, e))),
					OS:         "linux",
					BackupDate: time.Now(),
				}
				if err := repo.InsertBackupEntry(ctx, entry); err != nil {
					rt.Fatalf("InsertBackupEntry: %v", err)
				}

				// Store content in CAS.
				content := fmt.Sprintf("deletion-content-%s", hash)
				if err := cas.Put(ctx, hash, strings.NewReader(content)); err != nil {
					rt.Fatalf("CAS Put: %v", err)
				}
			}
		}

		// Pick a random backup ID to target for deletion.
		targetIdx := rapid.IntRange(0, numBackups-1).Draw(rt, "targetBackupIdx")
		targetID := backupIDs[targetIdx]

		// --- Capture state before dry-run ---
		dbBefore := captureDBSnapshot(rt, repo)
		casBefore := captureCASSnapshot(rt, cas, allHashes)

		// --- Execute dry-run deletion ---
		dryResult, err := delEngine.DeleteByBackupID(ctx, targetID, true)
		if err != nil {
			rt.Fatalf("DeleteByBackupID(dryRun=true): %v", err)
		}

		// --- Verify state unchanged after dry-run ---
		dbAfter := captureDBSnapshot(rt, repo)
		casAfter := captureCASSnapshot(rt, cas, allHashes)

		if !dbBefore.equalTo(dbAfter) {
			rt.Fatalf("property violation: DB state changed after deletion dry-run (before=%d, after=%d)",
				dbBefore.count(), dbAfter.count())
		}
		if !casBefore.equalTo(casAfter) {
			rt.Fatalf("property violation: CAS state changed after deletion dry-run")
		}

		// --- Execute real deletion ---
		realResult, err := delEngine.DeleteByBackupID(ctx, targetID, false)
		if err != nil {
			rt.Fatalf("DeleteByBackupID(dryRun=false): %v", err)
		}

		// --- Verify dry-run result accurately predicted actual deletions ---
		if dryResult.EntriesDeleted != realResult.EntriesDeleted {
			rt.Fatalf("property violation: dry-run reported %d entries deleted, actual was %d",
				dryResult.EntriesDeleted, realResult.EntriesDeleted)
		}

		if dryResult.BytesFreed != realResult.BytesFreed {
			rt.Fatalf("property violation: dry-run reported %d bytes freed, actual was %d",
				dryResult.BytesFreed, realResult.BytesFreed)
		}

		if dryResult.FilesRemoved != realResult.FilesRemoved {
			rt.Fatalf("property violation: dry-run reported %d files removed, actual was %d",
				dryResult.FilesRemoved, realResult.FilesRemoved)
		}
	})
}
