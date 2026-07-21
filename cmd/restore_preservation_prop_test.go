package cmd

import (
	"strings"
	"testing"
	"time"

	"github.com/gcclinux/tergum/internal/model"
	"github.com/gcclinux/tergum/internal/restore"
	"pgregory.net/rapid"
)

// **Validates: Requirements 3.1, 3.2, 3.3, 3.4, 3.5, 3.6, 3.7**

// Property 2a: For all query strings containing `/` but not `\` and not glob chars
// → classified as path with LIKE pattern ("%" + query + "%").
// After the fix, ALL paths (Unix and Windows) use contains-matching.
func TestProperty_PreservationUnixPathClassification(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate a query containing at least one `/` but no `\` and no glob chars.
		segmentCount := rapid.IntRange(2, 5).Draw(rt, "segmentCount")
		segments := make([]string, segmentCount)
		for i := 0; i < segmentCount; i++ {
			segments[i] = rapid.StringMatching(`[a-zA-Z0-9_\-]{1,15}`).Draw(rt, "segment")
		}
		query := "/" + strings.Join(segments, "/")

		// Sanity checks on generated input
		if !strings.Contains(query, "/") {
			rt.Fatalf("generated query must contain /: %q", query)
		}
		if strings.Contains(query, "\\") {
			rt.Fatalf("generated query must not contain \\: %q", query)
		}
		if strings.Contains(query, "*") || strings.Contains(query, "?") {
			rt.Fatalf("generated query must not contain glob chars: %q", query)
		}

		// Classify using the current logic
		result := classifyQueryCurrent(query)

		// ASSERT: Must be classified as path with prefix LIKE pattern
		expectedPath := "%" + query + "%"
		if result.Path != expectedPath {
			rt.Fatalf("Unix path query %q not classified correctly.\n"+
				"Expected: Path=%q\n"+
				"Got: Path=%q, Name=%q, Pattern=%q",
				query, expectedPath, result.Path, result.Name, result.Pattern)
		}
		if result.Name != "" {
			rt.Fatalf("Unix path query %q should not have Name set, got Name=%q", query, result.Name)
		}
		if result.Pattern != "" {
			rt.Fatalf("Unix path query %q should not have Pattern set, got Pattern=%q", query, result.Pattern)
		}
	})
}

// Property 2b: For all query strings containing `*` or `?` → classified as glob pattern.
// Note: queries with `/` take priority over glob in the current logic, so we test
// glob queries that do NOT contain `/`.
func TestProperty_PreservationGlobClassification(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate a glob-style query with `*` or `?` but no `/` or `\`.
		prefix := rapid.StringMatching(`[a-zA-Z0-9_]{1,10}`).Draw(rt, "prefix")
		globChar := rapid.SampledFrom([]string{"*", "?"}).Draw(rt, "globChar")
		suffix := rapid.StringMatching(`[a-zA-Z0-9_.]{0,10}`).Draw(rt, "suffix")

		query := prefix + globChar + suffix

		// Ensure no path separators (which would take priority)
		if strings.Contains(query, "/") || strings.Contains(query, "\\") {
			rt.Skipf("skipping query with path separator: %q", query)
		}

		// Classify using the current logic
		result := classifyQueryCurrent(query)

		// ASSERT: Must be classified as glob pattern
		if result.Pattern != query {
			rt.Fatalf("Glob query %q not classified correctly.\n"+
				"Expected: Pattern=%q\n"+
				"Got: Path=%q, Name=%q, Pattern=%q",
				query, query, result.Path, result.Name, result.Pattern)
		}
		if result.Path != "" {
			rt.Fatalf("Glob query %q should not have Path set, got Path=%q", query, result.Path)
		}
		if result.Name != "" {
			rt.Fatalf("Glob query %q should not have Name set, got Name=%q", query, result.Name)
		}
	})
}

// Property 2c: For all query strings with no `/`, `\`, `*`, `?` → classified as exact name.
func TestProperty_PreservationExactNameClassification(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate a plain file name with no path separators or glob chars.
		name := rapid.StringMatching(`[a-zA-Z0-9_\-]{1,20}`).Draw(rt, "name")
		ext := rapid.SampledFrom([]string{".txt", ".pdf", ".go", ".png", ".doc", ".jpg", ""}).Draw(rt, "ext")
		query := name + ext

		// Sanity: no special chars
		if strings.Contains(query, "/") || strings.Contains(query, "\\") ||
			strings.Contains(query, "*") || strings.Contains(query, "?") {
			rt.Skipf("skipping query with special chars: %q", query)
		}

		// Classify using the current logic
		result := classifyQueryCurrent(query)

		// ASSERT: Must be classified as exact name
		if result.Name != query {
			rt.Fatalf("Exact name query %q not classified correctly.\n"+
				"Expected: Name=%q\n"+
				"Got: Path=%q, Name=%q, Pattern=%q",
				query, query, result.Path, result.Name, result.Pattern)
		}
		if result.Path != "" {
			rt.Fatalf("Exact name query %q should not have Path set, got Path=%q", query, result.Path)
		}
		if result.Pattern != "" {
			rt.Fatalf("Exact name query %q should not have Pattern set, got Pattern=%q", query, result.Pattern)
		}
	})
}

// Property 2d: For all entry sets where latest entry has valid EncryptedDEK/Nonce
// → dedup selects that latest entry.
func TestProperty_PreservationValidDEKLatestSelected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		filePath := rapid.StringMatching(`/home/[a-z]{3,8}/[a-z]{3,8}\.[a-z]{2,4}`).Draw(rt, "filePath")
		fileName := rapid.StringMatching(`[a-z]{3,8}\.[a-z]{2,4}`).Draw(rt, "fileName")
		hash := rapid.StringMatching(`[0-9a-f]{64}`).Draw(rt, "hash")

		// Generate 2-5 entries for the same file path
		entryCount := rapid.IntRange(2, 5).Draw(rt, "entryCount")
		baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

		entries := make([]model.BackupEntry, entryCount)
		for i := 0; i < entryCount; i++ {
			// Generate valid DEK and Nonce for ALL entries (including the latest)
			dek := make([]byte, 32)
			for j := range dek {
				dek[j] = byte(rapid.IntRange(1, 255).Draw(rt, "dekByte"))
			}
			nonce := make([]byte, 12)
			for j := range nonce {
				nonce[j] = byte(rapid.IntRange(1, 255).Draw(rt, "nonceByte"))
			}

			entries[i] = model.BackupEntry{
				ID:           int64(i + 1),
				BackupID:     rapid.StringMatching(`backup-[0-9]{3}`).Draw(rt, "backupID"),
				Blake3Hash:   hash,
				FileName:     fileName,
				FilePath:     filePath,
				EncryptedDEK: dek,
				Nonce:        nonce,
				BackupDate:   baseTime.Add(time.Duration(i) * 24 * time.Hour),
			}
		}

		// The latest entry is the last one (highest BackupDate)
		latestIdx := entryCount - 1

		// Run the current dedup logic
		result := dedupEntriesCurrent(entries)

		selected, exists := result[filePath]
		if !exists {
			rt.Fatalf("no entry selected for filePath %q", filePath)
		}

		// ASSERT: The selected entry is the latest one (since it has valid DEK)
		if !selected.Metadata.BackupDate.Equal(entries[latestIdx].BackupDate) {
			rt.Fatalf("dedup did not select latest entry when all have valid DEK.\n"+
				"Expected BackupDate: %v\n"+
				"Got BackupDate: %v",
				entries[latestIdx].BackupDate, selected.Metadata.BackupDate)
		}

		// Also verify it has valid DEK
		if len(selected.Metadata.EncryptedDEK) == 0 || len(selected.Metadata.Nonce) == 0 {
			rt.Fatalf("selected entry should have valid DEK/Nonce")
		}
	})
}

// Property 2e: For all entry sets where ALL entries have nil DEK → dedup selects latest by BackupDate.
func TestProperty_PreservationAllNilDEKSelectsLatest(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		filePath := rapid.StringMatching(`/var/[a-z]{3,8}/[a-z]{3,8}\.[a-z]{2,4}`).Draw(rt, "filePath")
		fileName := rapid.StringMatching(`[a-z]{3,8}\.[a-z]{2,4}`).Draw(rt, "fileName")
		hash := rapid.StringMatching(`[0-9a-f]{64}`).Draw(rt, "hash")

		// Generate 2-5 entries with ALL nil DEK/Nonce
		entryCount := rapid.IntRange(2, 5).Draw(rt, "entryCount")
		baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

		entries := make([]model.BackupEntry, entryCount)
		for i := 0; i < entryCount; i++ {
			entries[i] = model.BackupEntry{
				ID:           int64(i + 1),
				BackupID:     rapid.StringMatching(`backup-[0-9]{3}`).Draw(rt, "backupID"),
				Blake3Hash:   hash,
				FileName:     fileName,
				FilePath:     filePath,
				EncryptedDEK: nil,
				Nonce:        nil,
				BackupDate:   baseTime.Add(time.Duration(i) * 24 * time.Hour),
			}
		}

		// The latest entry is the last one
		latestIdx := entryCount - 1

		// Verify precondition: all entries have nil DEK
		for _, e := range entries {
			if len(e.EncryptedDEK) > 0 {
				rt.Fatalf("precondition violated: all entries should have nil DEK")
			}
		}

		// Run the current dedup logic
		result := dedupEntriesCurrent(entries)

		selected, exists := result[filePath]
		if !exists {
			rt.Fatalf("no entry selected for filePath %q", filePath)
		}

		// ASSERT: dedup selects the latest entry by BackupDate
		if !selected.Metadata.BackupDate.Equal(entries[latestIdx].BackupDate) {
			rt.Fatalf("dedup did not select latest entry when all have nil DEK.\n"+
				"Expected BackupDate: %v (entry %d)\n"+
				"Got BackupDate: %v",
				entries[latestIdx].BackupDate, latestIdx, selected.Metadata.BackupDate)
		}
	})
}

// helperVerifySearchQuery is a validation helper used in preservation tests.
// It checks the SearchQuery fields match expected classification.
func helperVerifySearchQuery(rt *rapid.T, query string, result restore.SearchQuery, expectPath, expectName, expectPattern string) {
	if result.Path != expectPath {
		rt.Fatalf("query %q: expected Path=%q, got Path=%q", query, expectPath, result.Path)
	}
	if result.Name != expectName {
		rt.Fatalf("query %q: expected Name=%q, got Name=%q", query, expectName, result.Name)
	}
	if result.Pattern != expectPattern {
		rt.Fatalf("query %q: expected Pattern=%q, got Pattern=%q", query, expectPattern, result.Pattern)
	}
}
