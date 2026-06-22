package watcher

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"pgregory.net/rapid"
)

// **Validates: Requirements 11.2, 11.4, 11.6, 11.8**

// TestProperty_ExcludeFilteringCorrectness verifies that for any combination of
// file names and exclude patterns, the isExcluded function correctly identifies
// files that match any pattern via filepath.Match.
// This directly validates Requirement 11.8: events for excluded paths are
// discarded immediately (before debouncing).
func TestProperty_ExcludeFilteringCorrectness(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate random exclude patterns from a set of valid glob patterns.
		availablePatterns := []string{"*.tmp", "*.log", "*.bak", "*.swp", "~*", "*.cache", "*.o", "*.pyc"}
		patternCount := rapid.IntRange(1, 5).Draw(rt, "patternCount")
		excludePatterns := make([]string, patternCount)
		for i := range excludePatterns {
			excludePatterns[i] = rapid.SampledFrom(availablePatterns).Draw(rt, "pattern")
		}

		// Create a FileWatcher with these exclude patterns (no I/O needed).
		cfg := Config{
			DebounceMs:       500,
			StabilitySeconds: 60,
			ExcludePatterns:  excludePatterns,
		}
		fw := &FileWatcher{cfg: cfg}

		// Generate random file names with various extensions.
		extensions := []string{".tmp", ".log", ".bak", ".swp", ".cache", ".o", ".pyc", ".txt", ".go", ".md", ".dat", ".rs"}
		prefixes := []string{"file", "data", "temp", "backup", "~doc", "readme", "main", "index", "~lock"}
		fileCount := rapid.IntRange(5, 30).Draw(rt, "fileCount")

		for i := 0; i < fileCount; i++ {
			prefix := rapid.SampledFrom(prefixes).Draw(rt, "prefix")
			ext := rapid.SampledFrom(extensions).Draw(rt, "ext")
			fileName := prefix + ext
			path := filepath.Join("/some/watch/dir", fileName)

			isExcl := fw.isExcluded(path)

			// Compute expected result: check if base name matches any pattern.
			baseName := filepath.Base(path)
			expectedExcluded := false
			for _, pattern := range excludePatterns {
				if matched, _ := filepath.Match(pattern, baseName); matched {
					expectedExcluded = true
					break
				}
			}

			if isExcl != expectedExcluded {
				rt.Fatalf("isExcluded(%q) = %v, expected %v (patterns: %v, baseName: %q)",
					path, isExcl, expectedExcluded, excludePatterns, baseName)
			}
		}
	})
}

// TestProperty_ExcludeFilteringNeverPassesMatchedFiles verifies the contrapositive:
// if a file passes the exclude filter (isExcluded returns false), then it truly does
// not match any of the configured exclude patterns.
func TestProperty_ExcludeFilteringNeverPassesMatchedFiles(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate random glob patterns.
		availablePatterns := []string{"*.tmp", "*.log", "*.bak", "*.swp", "~*", "*.cache", "*.o", "*.pyc"}
		patternCount := rapid.IntRange(1, 5).Draw(rt, "patternCount")
		excludePatterns := make([]string, patternCount)
		for i := range excludePatterns {
			excludePatterns[i] = rapid.SampledFrom(availablePatterns).Draw(rt, "pattern")
		}

		cfg := Config{
			DebounceMs:       500,
			StabilitySeconds: 60,
			ExcludePatterns:  excludePatterns,
		}
		fw := &FileWatcher{cfg: cfg}

		// Generate a random file name using regex matching.
		name := rapid.StringMatching(`[a-z~]{2,8}\.[a-z]{1,5}`).Draw(rt, "fileName")
		path := filepath.Join("/watch/dir", name)

		isExcl := fw.isExcluded(path)

		if !isExcl {
			// If not excluded, verify no pattern matches the base name.
			baseName := filepath.Base(path)
			for _, pattern := range excludePatterns {
				if matched, _ := filepath.Match(pattern, baseName); matched {
					rt.Fatalf("file %q was NOT excluded but matches pattern %q (patterns: %v)",
						baseName, pattern, excludePatterns)
				}
			}
		}
	})
}

// TestProperty_DebounceCollapsesMultipleEvents verifies that when multiple
// filesystem events occur for the same file within the debounce window,
// only one stable file event is emitted per unique path.
// Validates Requirement 11.2: batch file events within debounce window.
//
// This test uses real filesystem I/O and timers. Each iteration takes ~1.1s
// due to the 1-second stability gate. Run with -rapid.checks=5 for faster CI,
// or with default 100 checks for thorough local testing (needs -timeout 180s).
func TestProperty_DebounceCollapsesMultipleEvents(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow property test in short mode")
	}

	rapid.Check(t, func(rt *rapid.T) {
		tmpDir := t.TempDir()

		cfg := Config{
			DebounceMs:       10,
			StabilitySeconds: 1,
			ExcludePatterns:  nil,
			IncludePaths:     []string{tmpDir},
		}

		fw, err := NewFileWatcher(cfg)
		if err != nil {
			rt.Fatalf("NewFileWatcher: %v", err)
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		if err := fw.Start(ctx); err != nil {
			rt.Fatalf("Start: %v", err)
		}
		defer fw.Stop()

		time.Sleep(50 * time.Millisecond)

		// Generate a random number of rapid writes to the same file.
		writeCount := rapid.IntRange(3, 15).Draw(rt, "writeCount")
		testFile := filepath.Join(tmpDir, "debounce_test.txt")

		for w := 0; w < writeCount; w++ {
			content := []byte("content-" + string(rune('A'+w%26)))
			if err := os.WriteFile(testFile, content, 0644); err != nil {
				rt.Fatalf("WriteFile: %v", err)
			}
			time.Sleep(2 * time.Millisecond)
		}

		// Wait for debounce + stability + buffer.
		waitTime := time.Duration(10+1000+500) * time.Millisecond
		timer := time.NewTimer(waitTime)
		defer timer.Stop()

		count := 0
		for {
			select {
			case sf := <-fw.StableFiles():
				if filepath.Base(sf.Path) == "debounce_test.txt" {
					count++
				}
			case <-timer.C:
				goto done
			}
		}
	done:
		// Property: regardless of how many writes, only 1 stable event per file.
		if count != 1 {
			rt.Fatalf("expected exactly 1 stable file event for debounce_test.txt after %d writes, got %d",
				writeCount, count)
		}
	})
}

// TestProperty_StabilityGateDiscardsDeletedFiles verifies that when a file
// is created and then deleted before the stability timer expires, no stable
// file event is emitted.
// Validates Requirements 11.4 and 11.6: stability gate verifies file exists
// at expiry; timer resets on modification (deletion causes discard).
//
// This test uses real filesystem I/O and timers. Each iteration takes ~1.5s.
// Run with -rapid.checks=5 for faster CI, or default for thorough testing.
func TestProperty_StabilityGateDiscardsDeletedFiles(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow property test in short mode")
	}

	rapid.Check(t, func(rt *rapid.T) {
		tmpDir := t.TempDir()

		cfg := Config{
			DebounceMs:       10,
			StabilitySeconds: 1,
			ExcludePatterns:  nil,
			IncludePaths:     []string{tmpDir},
		}

		fw, err := NewFileWatcher(cfg)
		if err != nil {
			rt.Fatalf("NewFileWatcher: %v", err)
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		if err := fw.Start(ctx); err != nil {
			rt.Fatalf("Start: %v", err)
		}
		defer fw.Stop()

		time.Sleep(50 * time.Millisecond)

		// Generate a random file name and content size.
		fileName := rapid.StringMatching(`[a-z]{4,8}\.txt`).Draw(rt, "fileName")
		testFile := filepath.Join(tmpDir, fileName)

		contentSize := rapid.IntRange(5, 200).Draw(rt, "contentSize")
		content := make([]byte, contentSize)
		for c := range content {
			content[c] = byte('a' + c%26)
		}

		// Create the file.
		if err := os.WriteFile(testFile, content, 0644); err != nil {
			rt.Fatalf("WriteFile: %v", err)
		}

		// Wait for the event to register, then delete before stability expires.
		time.Sleep(20 * time.Millisecond)
		if err := os.Remove(testFile); err != nil {
			rt.Fatalf("Remove: %v", err)
		}

		// Wait for full pipeline to complete.
		waitTime := time.Duration(10+1000+500) * time.Millisecond
		timer := time.NewTimer(waitTime)
		defer timer.Stop()

		for {
			select {
			case sf := <-fw.StableFiles():
				if filepath.Base(sf.Path) == fileName {
					rt.Fatalf("deleted file %q should not have passed stability gate", fileName)
				}
			case <-timer.C:
				// Expected: no event for the deleted file.
				return
			}
		}
	})
}
