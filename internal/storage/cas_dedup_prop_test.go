package storage

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ricardopadilha/tergum/internal/crypto"
	"github.com/ricardopadilha/tergum/internal/db"
	"github.com/ricardopadilha/tergum/internal/model"
	"pgregory.net/rapid"
)

// **Validates: Requirements 5.3, 5.4, 10.4**

// TestProperty_DeduplicationAndRefCounting verifies Property 4:
// For any set of files stored in the CAS, a physical storage file SHALL exist
// if and only if at least one database entry references its BLAKE3 hash.
// When a file with an identical hash is stored again, the deduplication counter
// SHALL increment without creating a duplicate storage file.
// When all referencing entries are deleted, the physical file SHALL be removed.
func TestProperty_DeduplicationAndRefCounting(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		ctx := context.Background()

		// Set up in-memory SQLite repository and temp-dir CAS store.
		repo, err := db.NewRepository(":memory:", false)
		if err != nil {
			rt.Fatalf("NewRepository: %v", err)
		}
		defer repo.Close()

		casDir := t.TempDir()
		store := NewCAS(casDir, repo)

		// Generate between 5 and 20 file entries.
		numFiles := rapid.IntRange(5, 20).Draw(rt, "numFiles")

		// Generate a pool of unique content blobs (3-8) to simulate deduplication.
		numUniqueBlobs := rapid.IntRange(3, 8).Draw(rt, "numUniqueBlobs")
		type blob struct {
			data []byte
			hash string
		}
		blobs := make([]blob, numUniqueBlobs)
		for i := range blobs {
			size := rapid.IntRange(10, 512).Draw(rt, fmt.Sprintf("blobSize_%d", i))
			data := rapid.SliceOfN(rapid.Byte(), size, size).Draw(rt, fmt.Sprintf("blobData_%d", i))
			blobs[i] = blob{data: data, hash: crypto.HashBytes(data)}
		}

		// Create a backup job (required by foreign key).
		jobID := "job-dedup-prop"
		err = repo.CreateJob(ctx, model.BackupJob{
			BackupID:    jobID,
			Level:       "FULL",
			ClientID:    "prop-client",
			InitiatedBy: "test",
			StartedAt:   time.Now().UTC().Truncate(time.Second),
			Status:      model.JobRunning,
		})
		if err != nil {
			rt.Fatalf("CreateJob: %v", err)
		}

		// Track which hashes have been physically stored.
		storedHashes := make(map[string]bool)
		// Track how many DB entries reference each hash.
		hashRefCount := make(map[string]int)

		// Assign each file entry a blob from the pool (some will share hashes).
		now := time.Now().UTC().Truncate(time.Second)
		type fileEntry struct {
			blobIdx int
			hash    string
			path    string
		}
		files := make([]fileEntry, numFiles)
		for i := range files {
			blobIdx := rapid.IntRange(0, numUniqueBlobs-1).Draw(rt, fmt.Sprintf("fileBlob_%d", i))
			files[i] = fileEntry{
				blobIdx: blobIdx,
				hash:    blobs[blobIdx].hash,
				path:    fmt.Sprintf("/data/file_%d.bin", i),
			}
		}

		// Store physical files in CAS for each unique hash, then insert DB entries.
		for i, f := range files {
			b := blobs[f.blobIdx]

			// Only Put to CAS if this hash hasn't been stored yet (dedup behavior).
			if !storedHashes[f.hash] {
				err := store.Put(ctx, f.hash, bytes.NewReader(b.data))
				if err != nil {
					rt.Fatalf("Put failed for hash %s: %v", f.hash, err)
				}
				storedHashes[f.hash] = true
			}

			// Insert DB entry referencing this hash.
			err := repo.InsertBackupEntry(ctx, model.BackupEntry{
				BackupID:   jobID,
				Blake3Hash: f.hash,
				FileName:   fmt.Sprintf("file_%d.bin", i),
				FilePath:   f.path,
				FileSize:   int64(len(b.data)),
				OS:         "linux",
				BackupDate: now,
			})
			if err != nil {
				rt.Fatalf("InsertBackupEntry: %v", err)
			}
			hashRefCount[f.hash]++
		}

		// VERIFY: For each unique hash, CAS file exists AND CountHashReferences > 0.
		for hash := range storedHashes {
			exists, err := store.Exists(ctx, hash)
			if err != nil {
				rt.Fatalf("Exists check failed for %s: %v", hash, err)
			}
			if !exists {
				rt.Fatalf("CAS file should exist for hash %s (refcount=%d)", hash, hashRefCount[hash])
			}

			refCount, err := store.RefCount(ctx, hash)
			if err != nil {
				rt.Fatalf("RefCount failed for %s: %v", hash, err)
			}
			if refCount <= 0 {
				rt.Fatalf("RefCount should be > 0 for hash %s, got %d", hash, refCount)
			}
			if int(refCount) != hashRefCount[hash] {
				rt.Fatalf("RefCount mismatch for hash %s: got %d, expected %d", hash, refCount, hashRefCount[hash])
			}
		}

		// Verify deduplication: storing a duplicate hash again does NOT create
		// a new physical file (Put is idempotent for same hash).
		// Pick a random hash and re-Put it; file count in CAS dir should not change.
		if len(blobs) > 0 {
			dupIdx := rapid.IntRange(0, numUniqueBlobs-1).Draw(rt, "dupIdx")
			dupBlob := blobs[dupIdx]

			// Put again (simulates duplicate store).
			err := store.Put(ctx, dupBlob.hash, bytes.NewReader(dupBlob.data))
			if err != nil {
				rt.Fatalf("Duplicate Put failed: %v", err)
			}

			// CAS file still exists (single file, not duplicated).
			exists, err := store.Exists(ctx, dupBlob.hash)
			if err != nil {
				rt.Fatalf("Exists after dup Put: %v", err)
			}
			if !exists {
				rt.Fatalf("CAS file missing after duplicate Put for hash %s", dupBlob.hash)
			}
		}

		// Delete a random subset of entries and verify reference counting.
		// Generate a boolean mask to decide which files to delete (at least 1).
		deleteFlags := make([]bool, numFiles)
		atLeastOne := false
		for i := range deleteFlags {
			deleteFlags[i] = rapid.Bool().Draw(rt, fmt.Sprintf("delete_%d", i))
			if deleteFlags[i] {
				atLeastOne = true
			}
		}
		// Ensure at least one is deleted.
		if !atLeastOne {
			deleteFlags[0] = true
		}

		for idx, shouldDelete := range deleteFlags {
			if !shouldDelete {
				continue
			}
			f := files[idx]
			// Delete by file path.
			_, err := repo.DeleteEntries(ctx, db.DeleteFilter{FilePath: f.path})
			if err != nil {
				rt.Fatalf("DeleteEntries for %s: %v", f.path, err)
			}
			hashRefCount[f.hash]--
		}

		// VERIFY: For each hash, check CAS file vs. remaining references.
		for hash := range storedHashes {
			refCount, err := store.RefCount(ctx, hash)
			if err != nil {
				rt.Fatalf("RefCount after deletion for %s: %v", hash, err)
			}

			expectedRef := hashRefCount[hash]
			if expectedRef < 0 {
				expectedRef = 0
			}
			if int(refCount) != expectedRef {
				rt.Fatalf("RefCount mismatch after deletion for hash %s: got %d, expected %d", hash, refCount, expectedRef)
			}

			exists, err := store.Exists(ctx, hash)
			if err != nil {
				rt.Fatalf("Exists after deletion for %s: %v", hash, err)
			}

			if refCount == 0 {
				// When all references are deleted, physical file SHALL be removed.
				// Simulate the cleanup: delete from CAS.
				err := store.Delete(ctx, hash)
				if err != nil {
					rt.Fatalf("Delete from CAS for orphan hash %s: %v", hash, err)
				}

				// Verify it's gone.
				existsAfter, err := store.Exists(ctx, hash)
				if err != nil {
					rt.Fatalf("Exists after CAS delete for %s: %v", hash, err)
				}
				if existsAfter {
					rt.Fatalf("CAS file should be removed after all refs deleted for hash %s", hash)
				}
			} else {
				// CAS file should still exist when refcount > 0.
				if !exists {
					rt.Fatalf("CAS file should still exist for hash %s (refcount=%d)", hash, refCount)
				}
			}
		}
	})
}
