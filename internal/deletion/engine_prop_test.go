package deletion

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gcclinux/tergum/internal/model"
	"pgregory.net/rapid"
)

// **Validates: Requirements 10.1, 10.2, 10.3, 10.5, 10.7**

// genHash generates a deterministic 64-char hex hash from an index.
func genHash(idx int) string {
	base := fmt.Sprintf("%064x", idx+1)
	return base[:64]
}

// genBackupID generates a backup ID from an index.
func genBackupID(idx int) string {
	return fmt.Sprintf("backup-%d", idx)
}

// genFolderPath picks from a small set of folder prefixes.
var folderPrefixes = []string{"/docs/", "/src/", "/media/", "/config/", "/logs/"}

// genEntries generates a slice of random backup entries across multiple backup IDs
// and folder prefixes, using the provided rapid.T for randomness.
func genEntries(rt *rapid.T) ([]model.BackupEntry, []string, map[string]bool) {
	numEntries := rapid.IntRange(5, 20).Draw(rt, "numEntries")
	numBackups := rapid.IntRange(1, 4).Draw(rt, "numBackups")
	numHashes := rapid.IntRange(2, numEntries).Draw(rt, "numHashes")

	// Generate hashes.
	hashes := make([]string, numHashes)
	for i := range hashes {
		hashes[i] = genHash(i)
	}

	// Generate backup IDs.
	backupIDs := make([]string, numBackups)
	for i := range backupIDs {
		backupIDs[i] = genBackupID(i)
	}

	entries := make([]model.BackupEntry, numEntries)
	storeHashes := make(map[string]bool)

	for i := range entries {
		backupIdx := rapid.IntRange(0, numBackups-1).Draw(rt, fmt.Sprintf("backupIdx_%d", i))
		hashIdx := rapid.IntRange(0, numHashes-1).Draw(rt, fmt.Sprintf("hashIdx_%d", i))
		folderIdx := rapid.IntRange(0, len(folderPrefixes)-1).Draw(rt, fmt.Sprintf("folderIdx_%d", i))
		fileName := rapid.StringMatching(`[a-z]{3,8}\.(txt|go|md|log)`).Draw(rt, fmt.Sprintf("fileName_%d", i))
		fileSize := rapid.Int64Range(10, 10000).Draw(rt, fmt.Sprintf("fileSize_%d", i))

		hash := hashes[hashIdx]
		entries[i] = model.BackupEntry{
			ID:         int64(i + 1),
			BackupID:   backupIDs[backupIdx],
			Blake3Hash: hash,
			FilePath:   folderPrefixes[folderIdx] + fileName,
			FileName:   fileName,
			FileSize:   fileSize,
			OS:         "linux",
		}
		storeHashes[hash] = true
	}

	return entries, backupIDs, storeHashes
}

// setupMocks creates and populates mock repo and store from generated entries.
func setupMocks(entries []model.BackupEntry, backupIDs []string, storeHashes map[string]bool) (*mockRepo, *mockStore) {
	repo := newMockRepo()
	store := newMockStore()

	for _, id := range backupIDs {
		repo.addJob(id)
	}
	for _, e := range entries {
		repo.addEntry(e)
	}
	for h := range storeHashes {
		store.add(h)
	}
	return repo, store
}

// TestProperty_DeleteByBackupID verifies that deleting by backup_id removes
// exactly the entries matching that backup_id and leaves all others untouched.
func TestProperty_DeleteByBackupID(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		entries, backupIDs, storeHashes := genEntries(rt)
		repo, store := setupMocks(entries, backupIDs, storeHashes)
		engine := New(repo, store)
		ctx := context.Background()

		// Pick a random backup ID to delete.
		targetIdx := rapid.IntRange(0, len(backupIDs)-1).Draw(rt, "targetBackupIdx")
		targetID := backupIDs[targetIdx]

		// Compute expected: entries that match vs entries that don't.
		var expectedDeleted []model.BackupEntry
		var expectedRemaining []model.BackupEntry
		for _, e := range entries {
			if e.BackupID == targetID {
				expectedDeleted = append(expectedDeleted, e)
			} else {
				expectedRemaining = append(expectedRemaining, e)
			}
		}

		result, err := engine.DeleteByBackupID(ctx, targetID, false)
		if err != nil {
			rt.Fatalf("DeleteByBackupID failed: %v", err)
		}

		// Verify exactly matching entries were removed.
		if result.EntriesDeleted != int64(len(expectedDeleted)) {
			rt.Fatalf("expected %d entries deleted, got %d", len(expectedDeleted), result.EntriesDeleted)
		}

		// Verify remaining entries are exactly the non-matching ones.
		repo.mu.Lock()
		remainingCount := len(repo.entries)
		repo.mu.Unlock()
		if remainingCount != len(expectedRemaining) {
			rt.Fatalf("expected %d remaining entries, got %d", len(expectedRemaining), remainingCount)
		}

		// Verify physical files: a hash should be removed from store only if
		// zero remaining entries reference it.
		for h := range storeHashes {
			refCount := int64(0)
			for _, e := range expectedRemaining {
				if e.Blake3Hash == h {
					refCount++
				}
			}
			if refCount == 0 {
				if store.has(h) {
					rt.Fatalf("hash %s should be removed from store (no refs remain)", h)
				}
			} else {
				if !store.has(h) {
					rt.Fatalf("hash %s should still exist in store (%d refs remain)", h, refCount)
				}
			}
		}
	})
}

// TestProperty_DeleteByFolder verifies that deleting by folder removes exactly
// entries whose file_path starts with the given prefix, and leaves others untouched.
func TestProperty_DeleteByFolder(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		entries, backupIDs, storeHashes := genEntries(rt)
		repo, store := setupMocks(entries, backupIDs, storeHashes)
		engine := New(repo, store)
		ctx := context.Background()

		// Pick a random folder prefix.
		folderIdx := rapid.IntRange(0, len(folderPrefixes)-1).Draw(rt, "folderIdx")
		targetFolder := folderPrefixes[folderIdx]

		// Pick a random backup ID to scope the delete.
		targetBackupIdx := rapid.IntRange(0, len(backupIDs)-1).Draw(rt, "targetBackupIdx")
		targetBackupID := backupIDs[targetBackupIdx]

		// Compute expected: entries matching both folder prefix AND backup ID.
		var expectedDeleted []model.BackupEntry
		var expectedRemaining []model.BackupEntry
		for _, e := range entries {
			if e.BackupID == targetBackupID && strings.HasPrefix(e.FilePath, targetFolder) {
				expectedDeleted = append(expectedDeleted, e)
			} else {
				expectedRemaining = append(expectedRemaining, e)
			}
		}

		result, err := engine.DeleteByFolder(ctx, targetFolder, targetBackupID, false, false)
		if err != nil {
			rt.Fatalf("DeleteByFolder failed: %v", err)
		}

		// Verify exactly matching entries were removed.
		if result.EntriesDeleted != int64(len(expectedDeleted)) {
			rt.Fatalf("expected %d entries deleted, got %d", len(expectedDeleted), result.EntriesDeleted)
		}

		// Verify remaining entries count.
		repo.mu.Lock()
		remainingCount := len(repo.entries)
		repo.mu.Unlock()
		if remainingCount != len(expectedRemaining) {
			rt.Fatalf("expected %d remaining entries, got %d", len(expectedRemaining), remainingCount)
		}

		// Verify physical files removed only when no references remain.
		for h := range storeHashes {
			refCount := int64(0)
			for _, e := range expectedRemaining {
				if e.Blake3Hash == h {
					refCount++
				}
			}
			if refCount == 0 {
				if store.has(h) {
					rt.Fatalf("hash %s should be removed (no refs remain after folder delete)", h)
				}
			} else {
				if !store.has(h) {
					rt.Fatalf("hash %s should still exist (%d refs remain)", h, refCount)
				}
			}
		}
	})
}

// TestProperty_DeleteByFile verifies that deleting a single file removes exactly
// the entry matching that specific file path within the scoped backup.
func TestProperty_DeleteByFile(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		entries, backupIDs, storeHashes := genEntries(rt)
		repo, store := setupMocks(entries, backupIDs, storeHashes)
		engine := New(repo, store)
		ctx := context.Background()

		// Pick a random entry as the target file.
		targetIdx := rapid.IntRange(0, len(entries)-1).Draw(rt, "targetEntryIdx")
		targetEntry := entries[targetIdx]

		// Compute expected: entries matching both file path AND backup ID.
		var expectedDeleted []model.BackupEntry
		var expectedRemaining []model.BackupEntry
		for _, e := range entries {
			if e.BackupID == targetEntry.BackupID && e.FilePath == targetEntry.FilePath {
				expectedDeleted = append(expectedDeleted, e)
			} else {
				expectedRemaining = append(expectedRemaining, e)
			}
		}

		result, err := engine.DeleteByFile(ctx, targetEntry.FilePath, targetEntry.BackupID, false, false)
		if err != nil {
			rt.Fatalf("DeleteByFile failed: %v", err)
		}

		// Verify exactly matching entries removed.
		if result.EntriesDeleted != int64(len(expectedDeleted)) {
			rt.Fatalf("expected %d entries deleted, got %d", len(expectedDeleted), result.EntriesDeleted)
		}

		// Verify remaining entries.
		repo.mu.Lock()
		remainingCount := len(repo.entries)
		repo.mu.Unlock()
		if remainingCount != len(expectedRemaining) {
			rt.Fatalf("expected %d remaining entries, got %d", len(expectedRemaining), remainingCount)
		}

		// Verify physical file cleanup.
		for h := range storeHashes {
			refCount := int64(0)
			for _, e := range expectedRemaining {
				if e.Blake3Hash == h {
					refCount++
				}
			}
			if refCount == 0 {
				if store.has(h) {
					rt.Fatalf("hash %s should be removed (no refs remain after file delete)", h)
				}
			} else {
				if !store.has(h) {
					rt.Fatalf("hash %s should still exist (%d refs remain)", h, refCount)
				}
			}
		}
	})
}

// TestProperty_JobRemovedWhenAllEntriesDeleted verifies that when all entries
// for a backup_id are removed, the job record is also cleaned up.
func TestProperty_JobRemovedWhenAllEntriesDeleted(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		entries, backupIDs, storeHashes := genEntries(rt)
		repo, store := setupMocks(entries, backupIDs, storeHashes)
		engine := New(repo, store)
		ctx := context.Background()

		// Pick a random backup ID to delete.
		targetIdx := rapid.IntRange(0, len(backupIDs)-1).Draw(rt, "targetBackupIdx")
		targetID := backupIDs[targetIdx]

		// Determine which jobs should survive (still have entries after delete).
		jobsWithRemainingEntries := make(map[string]bool)
		for _, e := range entries {
			if e.BackupID != targetID {
				jobsWithRemainingEntries[e.BackupID] = true
			}
		}

		result, err := engine.DeleteByBackupID(ctx, targetID, false)
		if err != nil {
			rt.Fatalf("DeleteByBackupID failed: %v", err)
		}

		// The target job should always be removed (we deleted all its entries).
		if result.JobsRemoved < 1 {
			rt.Fatalf("expected at least 1 job removed (target), got %d", result.JobsRemoved)
		}

		// Verify remaining jobs are exactly those with remaining entries.
		repo.mu.Lock()
		remainingJobs := make(map[string]bool)
		for _, j := range repo.jobs {
			remainingJobs[j] = true
		}
		repo.mu.Unlock()

		if remainingJobs[targetID] {
			rt.Fatalf("target job %s should have been removed", targetID)
		}
		for jobID := range jobsWithRemainingEntries {
			if !remainingJobs[jobID] {
				rt.Fatalf("job %s should still exist (has remaining entries)", jobID)
			}
		}
	})
}

// TestProperty_AllBackupsDeletesAcrossAllSets verifies that the --all-backups
// flag removes the target file/folder from every backup set, not just one.
func TestProperty_AllBackupsDeletesAcrossAllSets(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		entries, backupIDs, storeHashes := genEntries(rt)
		repo, store := setupMocks(entries, backupIDs, storeHashes)
		engine := New(repo, store)
		ctx := context.Background()

		// Pick a random folder prefix to delete across all backups.
		folderIdx := rapid.IntRange(0, len(folderPrefixes)-1).Draw(rt, "folderIdx")
		targetFolder := folderPrefixes[folderIdx]

		// Compute expected: entries matching the folder prefix across ALL backups.
		var expectedDeleted []model.BackupEntry
		var expectedRemaining []model.BackupEntry
		for _, e := range entries {
			if strings.HasPrefix(e.FilePath, targetFolder) {
				expectedDeleted = append(expectedDeleted, e)
			} else {
				expectedRemaining = append(expectedRemaining, e)
			}
		}

		result, err := engine.DeleteByFolder(ctx, targetFolder, "", true, false)
		if err != nil {
			rt.Fatalf("DeleteByFolder (all backups) failed: %v", err)
		}

		// Verify exactly matching entries from all backup sets removed.
		if result.EntriesDeleted != int64(len(expectedDeleted)) {
			rt.Fatalf("expected %d entries deleted across all backups, got %d",
				len(expectedDeleted), result.EntriesDeleted)
		}

		// Verify remaining entries.
		repo.mu.Lock()
		remainingCount := len(repo.entries)
		repo.mu.Unlock()
		if remainingCount != len(expectedRemaining) {
			rt.Fatalf("expected %d remaining entries, got %d", len(expectedRemaining), remainingCount)
		}

		// Verify no remaining entry matches the target folder.
		repo.mu.Lock()
		for _, e := range repo.entries {
			if strings.HasPrefix(e.FilePath, targetFolder) {
				repo.mu.Unlock()
				rt.Fatalf("entry with path %s should have been deleted (matches %s)", e.FilePath, targetFolder)
			}
		}
		repo.mu.Unlock()

		// Verify physical files removed only when no references remain.
		for h := range storeHashes {
			refCount := int64(0)
			for _, e := range expectedRemaining {
				if e.Blake3Hash == h {
					refCount++
				}
			}
			if refCount == 0 {
				if store.has(h) {
					rt.Fatalf("hash %s should be removed (no refs remain after all-backups delete)", h)
				}
			} else {
				if !store.has(h) {
					rt.Fatalf("hash %s should still exist (%d refs remain)", h, refCount)
				}
			}
		}
	})
}
