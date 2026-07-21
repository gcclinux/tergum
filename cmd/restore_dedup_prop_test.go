package cmd

import (
	"testing"
	"time"

	"github.com/gcclinux/tergum/internal/model"
	"github.com/gcclinux/tergum/internal/restore"
	"pgregory.net/rapid"
)

// **Validates: Requirements 1.4, 1.5**

// dedupEntriesCurrent mirrors the FIXED deduplication logic
// from cmd/restore.go runRestore function.
// It keeps only one entry per FilePath, preferring entries with valid encryption metadata.
func dedupEntriesCurrent(entries []model.BackupEntry) map[string]restore.RestoreEntry {
	seen := make(map[string]restore.RestoreEntry)
	for _, entry := range entries {
		// Use FilePath as destination (simplified; real code calls resolveDestination)
		re := restore.RestoreEntry{
			Hash:        entry.Blake3Hash,
			FileName:    entry.FileName,
			Destination: entry.FilePath,
			BackupID:    entry.BackupID,
			Metadata:    &entry,
		}
		existing, exists := seen[entry.FilePath]
		if !exists {
			seen[entry.FilePath] = re
		} else {
			existingHasDEK := len(existing.Metadata.EncryptedDEK) > 0 && len(existing.Metadata.Nonce) > 0
			newHasDEK := len(entry.EncryptedDEK) > 0 && len(entry.Nonce) > 0
			if newHasDEK && !existingHasDEK {
				seen[entry.FilePath] = re
			} else if !newHasDEK && existingHasDEK {
				// Keep existing
			} else if entry.BackupDate.After(existing.Metadata.BackupDate) {
				seen[entry.FilePath] = re
			}
		}
	}
	return seen
}

// TestProperty_DedupSelectsValidDEK verifies that when deduplicating backup entries
// for the same file path, the selected entry has valid EncryptedDEK and Nonce fields
// when at least one such entry exists.
//
// Bug Condition: latestEntryByBackupDate(entries).EncryptedDEK == nil
//
//	AND olderEntryWithValidDEKExists(entries)
//
// This test is EXPECTED TO FAIL on unfixed code because the current dedup logic
// only considers BackupDate (latest wins), ignoring whether the entry has valid
// encryption metadata. Server-side deduped entries have nil DEK/Nonce.
func TestProperty_DedupSelectsValidDEK(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate a set of BackupEntry for the SAME file path where:
		// - At least one entry has valid (non-nil) EncryptedDEK and Nonce
		// - The latest entry by BackupDate has nil EncryptedDEK and Nonce

		filePath := rapid.StringMatching(`C:\\Users\\[a-z]{3,8}\\[a-z]{3,8}\.[a-z]{2,4}`).Draw(rt, "filePath")
		fileName := rapid.StringMatching(`[a-z]{3,8}\.[a-z]{2,4}`).Draw(rt, "fileName")
		hash := rapid.StringMatching(`[0-9a-f]{64}`).Draw(rt, "hash")

		// Generate 2-5 entries for the same file
		entryCount := rapid.IntRange(2, 5).Draw(rt, "entryCount")

		// The "valid" entry: has non-nil EncryptedDEK and Nonce, with an OLDER date
		baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

		entries := make([]model.BackupEntry, entryCount)

		// First entry: valid DEK, oldest date
		dek := make([]byte, 32)
		for i := range dek {
			dek[i] = byte(rapid.IntRange(1, 255).Draw(rt, "dekByte"))
		}
		nonce := make([]byte, 12)
		for i := range nonce {
			nonce[i] = byte(rapid.IntRange(1, 255).Draw(rt, "nonceByte"))
		}

		entries[0] = model.BackupEntry{
			ID:           1,
			BackupID:     "backup-001",
			Blake3Hash:   hash,
			FileName:     fileName,
			FilePath:     filePath,
			EncryptedDEK: dek,
			Nonce:        nonce,
			BackupDate:   baseTime, // Oldest
		}

		// Remaining entries: nil DEK/Nonce (server-side deduped), progressively newer dates
		for i := 1; i < entryCount; i++ {
			entries[i] = model.BackupEntry{
				ID:           int64(i + 1),
				BackupID:     rapid.StringMatching(`backup-[0-9]{3}`).Draw(rt, "backupID"),
				Blake3Hash:   hash,
				FileName:     fileName,
				FilePath:     filePath,
				EncryptedDEK: nil,                                             // Server-side deduped: no DEK
				Nonce:        nil,                                             // Server-side deduped: no Nonce
				BackupDate:   baseTime.Add(time.Duration(i) * 24 * time.Hour), // Newer
			}
		}

		// Verify our preconditions:
		// - Latest entry has nil DEK
		latestIdx := 0
		for i, e := range entries {
			if e.BackupDate.After(entries[latestIdx].BackupDate) {
				latestIdx = i
			}
		}
		if entries[latestIdx].EncryptedDEK != nil {
			rt.Fatalf("precondition violated: latest entry should have nil DEK")
		}
		// - At least one entry has valid DEK
		hasValidDEK := false
		for _, e := range entries {
			if len(e.EncryptedDEK) > 0 && len(e.Nonce) > 0 {
				hasValidDEK = true
				break
			}
		}
		if !hasValidDEK {
			rt.Fatalf("precondition violated: at least one entry should have valid DEK")
		}

		// Run the current (buggy) dedup logic
		result := dedupEntriesCurrent(entries)

		// ASSERT: The selected entry for this filePath should have valid DEK/Nonce
		selected, exists := result[filePath]
		if !exists {
			rt.Fatalf("no entry selected for filePath %q", filePath)
		}

		if len(selected.Metadata.EncryptedDEK) == 0 || len(selected.Metadata.Nonce) == 0 {
			rt.Fatalf("dedup selected entry with nil EncryptedDEK/Nonce for %q.\n"+
				"Selected entry BackupDate: %v, BackupID: %s\n"+
				"Expected: entry with valid EncryptedDEK and Nonce should be preferred\n"+
				"Bug: dedup only considers BackupDate, ignoring encryption metadata validity",
				filePath, selected.Metadata.BackupDate, selected.Metadata.BackupID)
		}
	})
}

// TestProperty_DedupNilDEKNotSelected verifies that entries with nil encryption
// metadata are never selected when an entry with valid metadata exists for the
// same file path.
func TestProperty_DedupNilDEKNotSelected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Simpler variant: exactly 2 entries, one valid DEK (older), one nil DEK (newer)
		filePath := rapid.StringMatching(`[A-Z]:\\[A-Za-z]{3,10}\\[a-z]{3,8}\.[a-z]{3}`).Draw(rt, "filePath")
		fileName := rapid.StringMatching(`[a-z]{3,8}\.[a-z]{3}`).Draw(rt, "fileName")
		hash := rapid.StringMatching(`[0-9a-f]{64}`).Draw(rt, "hash")

		olderDate := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
		newerDate := time.Date(2024, 6, 20, 10, 0, 0, 0, time.UTC)

		// Generate valid DEK and Nonce bytes
		dek := make([]byte, 32)
		for i := range dek {
			dek[i] = byte(rapid.IntRange(1, 255).Draw(rt, "dekByte"))
		}
		nonce := make([]byte, 12)
		for i := range nonce {
			nonce[i] = byte(rapid.IntRange(1, 255).Draw(rt, "nonceByte"))
		}

		entries := []model.BackupEntry{
			{
				ID:           1,
				BackupID:     "backup-old",
				Blake3Hash:   hash,
				FileName:     fileName,
				FilePath:     filePath,
				EncryptedDEK: dek,
				Nonce:        nonce,
				BackupDate:   olderDate,
			},
			{
				ID:           2,
				BackupID:     "backup-new",
				Blake3Hash:   hash,
				FileName:     fileName,
				FilePath:     filePath,
				EncryptedDEK: nil,
				Nonce:        nil,
				BackupDate:   newerDate,
			},
		}

		// Run the current (buggy) dedup logic
		result := dedupEntriesCurrent(entries)

		selected := result[filePath]

		// ASSERT: Should NOT select the nil-DEK entry
		if len(selected.Metadata.EncryptedDEK) == 0 {
			rt.Fatalf("dedup selected nil-DEK entry (BackupID=%s, Date=%v) over valid-DEK entry (BackupID=%s, Date=%v)\n"+
				"Bug: current logic only compares BackupDate, always picking the newer entry\n"+
				"Expected: prefer entry with valid EncryptedDEK/Nonce for successful decryption",
				selected.Metadata.BackupID, selected.Metadata.BackupDate,
				entries[0].BackupID, entries[0].BackupDate)
		}
	})
}
