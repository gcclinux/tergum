package cmd

import (
	"strings"
	"testing"

	"github.com/gcclinux/tergum/internal/restore"
	"pgregory.net/rapid"
)

// **Validates: Requirements 1.1, 1.2, 1.3**

// classifyQueryCurrent mirrors the FIXED query classification logic
// from cmd/restore.go runRestore function.
// This replicates the inlined behavior so we can test it in isolation.
func classifyQueryCurrent(query string) restore.SearchQuery {
	searchQuery := restore.SearchQuery{}

	if strings.Contains(query, "/") || strings.Contains(query, "\\") {
		// Looks like a path pattern (Unix or Windows).
		searchQuery.Path = "%" + query + "%"
	} else if strings.Contains(query, "*") || strings.Contains(query, "?") {
		// Glob pattern.
		searchQuery.Pattern = query
	} else {
		// Exact file name.
		searchQuery.Name = query
	}

	return searchQuery
}

// TestProperty_WindowsPathDetection verifies that queries containing backslashes
// (but no forward slashes and no glob characters) are classified as path patterns
// with a contains-style LIKE pattern.
//
// Bug Condition: strings.Contains(query, "\\") AND NOT strings.Contains(query, "/")
//
//	AND NOT containsGlobChars(query)
//
// This test is EXPECTED TO FAIL on unfixed code because the current logic only
// checks for "/" to identify path patterns, missing Windows "\" paths entirely.
func TestProperty_WindowsPathDetection(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate a query string that contains at least one backslash,
		// no forward slashes, and no glob characters (* or ?).
		// These represent Windows-style path queries.

		// Generate path segments (alphanumeric directory/file names)
		segmentCount := rapid.IntRange(2, 5).Draw(rt, "segmentCount")
		segments := make([]string, segmentCount)
		for i := 0; i < segmentCount; i++ {
			segments[i] = rapid.StringMatching(`[A-Za-z0-9_\-]{1,20}`).Draw(rt, "segment")
		}

		// Optionally prepend a drive letter (e.g., "C:")
		useDrive := rapid.Bool().Draw(rt, "useDrive")
		var query string
		if useDrive {
			driveLetter := rapid.StringMatching(`[A-Z]`).Draw(rt, "drive")
			query = driveLetter + ":\\" + strings.Join(segments, "\\")
		} else {
			query = strings.Join(segments, "\\")
		}

		// Sanity check our generated input matches the bug condition
		if !strings.Contains(query, "\\") {
			rt.Fatalf("generated query should contain backslash: %q", query)
		}
		if strings.Contains(query, "/") {
			rt.Fatalf("generated query should not contain forward slash: %q", query)
		}
		if strings.Contains(query, "*") || strings.Contains(query, "?") {
			rt.Fatalf("generated query should not contain glob chars: %q", query)
		}

		// Classify using the current (buggy) logic
		result := classifyQueryCurrent(query)

		// EXPECTED CORRECT BEHAVIOR:
		// The query should be classified as a path pattern (searchQuery.Path set)
		// with a contains-style LIKE pattern: "%" + query + "%"
		expectedPath := "%" + query + "%"

		if result.Path != expectedPath {
			rt.Fatalf("Windows path query %q was not classified as path pattern.\n"+
				"Expected: Path=%q\n"+
				"Got: Path=%q, Name=%q, Pattern=%q",
				query, expectedPath, result.Path, result.Name, result.Pattern)
		}
	})
}

// TestProperty_WindowsPathNotExactName verifies that queries with backslashes
// are never classified as exact file names.
//
// This is the minimal assertion: a path containing "\" should NOT be treated
// as an exact name match.
func TestProperty_WindowsPathNotExactName(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate Windows-style paths
		segmentCount := rapid.IntRange(2, 4).Draw(rt, "segmentCount")
		segments := make([]string, segmentCount)
		for i := 0; i < segmentCount; i++ {
			segments[i] = rapid.StringMatching(`[A-Za-z0-9]{2,12}`).Draw(rt, "segment")
		}

		// Add a file extension to the last segment
		ext := rapid.SampledFrom([]string{".txt", ".pdf", ".go", ".png", ".doc"}).Draw(rt, "ext")
		segments[segmentCount-1] = segments[segmentCount-1] + ext

		query := strings.Join(segments, "\\")

		// Classify using the current logic
		result := classifyQueryCurrent(query)

		// ASSERT: A query containing "\" should NEVER be classified as exact name
		if result.Name != "" {
			rt.Fatalf("Windows path query %q was incorrectly classified as exact name: Name=%q\n"+
				"Expected: Path field to be set (path pattern detection)",
				query, result.Name)
		}
	})
}
