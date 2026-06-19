package backup

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// **Validates: Requirements 7.1, 7.2, 7.3, 7.4**

// TestProperty_ScanExcludedFilesNeverAppear verifies that for any file tree and
// exclude patterns, no file matching an exclude pattern ever appears in the scan results.
func TestProperty_ScanExcludedFilesNeverAppear(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		dir := t.TempDir()

		// Generate a random file tree with 5-20 files.
		fileCount := rapid.IntRange(5, 20).Draw(rt, "fileCount")
		extensions := []string{".txt", ".tmp", ".log", ".go", ".md", ".dat", ".bak"}
		createdFiles := make([]string, 0, fileCount)

		for i := 0; i < fileCount; i++ {
			// Choose a random extension.
			ext := rapid.SampledFrom(extensions).Draw(rt, "ext")
			// Choose a random subdirectory depth (0-2).
			depth := rapid.IntRange(0, 2).Draw(rt, "depth")
			parts := make([]string, 0, depth+1)
			for d := 0; d < depth; d++ {
				dirName := rapid.StringMatching(`[a-z]{3,8}`).Draw(rt, "dirName")
				parts = append(parts, dirName)
			}
			fileName := rapid.StringMatching(`[a-z]{3,10}`).Draw(rt, "fileName") + ext
			parts = append(parts, fileName)

			relPath := filepath.Join(parts...)
			fullPath := filepath.Join(dir, relPath)

			// Create file with random size (1-500 bytes).
			size := rapid.IntRange(1, 500).Draw(rt, "fileSize")
			data := make([]byte, size)
			if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
				rt.Fatalf("MkdirAll failed: %v", err)
			}
			if err := os.WriteFile(fullPath, data, 0o644); err != nil {
				rt.Fatalf("WriteFile failed: %v", err)
			}
			createdFiles = append(createdFiles, relPath)
		}

		// Choose exclude patterns from known set of glob patterns.
		availablePatterns := []string{"*.tmp", "*.log", "*.bak"}
		excludePatterns := make([]string, 0, len(availablePatterns))
		for _, p := range availablePatterns {
			if rapid.Bool().Draw(rt, "include_"+p) {
				excludePatterns = append(excludePatterns, p)
			}
		}
		// Ensure at least one exclude pattern is selected.
		if len(excludePatterns) == 0 {
			excludePatterns = append(excludePatterns, availablePatterns[0])
		}

		// Run the scan.
		results, err := Scan(context.Background(), []string{dir}, excludePatterns, 0)
		if err != nil {
			rt.Fatalf("Scan failed: %v", err)
		}

		// Verify: no result file matches any exclude pattern.
		for _, f := range results {
			for _, pattern := range excludePatterns {
				if matched, _ := filepath.Match(pattern, f.Name); matched {
					rt.Fatalf("excluded file appeared in results: %s matched pattern %s", f.Name, pattern)
				}
			}
		}

		_ = createdFiles // used implicitly via filesystem
	})
}

// TestProperty_ScanOversizedFilesNeverAppear verifies that for any file tree and
// max file size, no file exceeding the max size ever appears in the scan results.
func TestProperty_ScanOversizedFilesNeverAppear(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		dir := t.TempDir()

		// Generate a random file tree with 5-15 files of varying sizes.
		fileCount := rapid.IntRange(5, 15).Draw(rt, "fileCount")

		for i := 0; i < fileCount; i++ {
			fileName := rapid.StringMatching(`[a-z]{3,8}\.txt`).Draw(rt, "fileName")
			fullPath := filepath.Join(dir, fileName)

			// Create file with random size (1-2000 bytes).
			size := rapid.IntRange(1, 2000).Draw(rt, "fileSize")
			data := make([]byte, size)
			if err := os.WriteFile(fullPath, data, 0o644); err != nil {
				rt.Fatalf("WriteFile failed: %v", err)
			}
		}

		// Choose a random max file size between 100 and 1000 bytes.
		maxFileSize := int64(rapid.IntRange(100, 1000).Draw(rt, "maxFileSize"))

		// Run the scan with the max file size constraint.
		results, err := Scan(context.Background(), []string{dir}, nil, maxFileSize)
		if err != nil {
			rt.Fatalf("Scan failed: %v", err)
		}

		// Verify: no result file exceeds the max file size.
		for _, f := range results {
			if f.Size > maxFileSize {
				rt.Fatalf("oversized file appeared in results: %s has size %d, max is %d", f.Name, f.Size, maxFileSize)
			}
		}
	})
}

// TestProperty_ScanResultsWithinIncludePaths verifies that all files returned by
// Scan exist within the specified include paths.
func TestProperty_ScanResultsWithinIncludePaths(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		dir := t.TempDir()

		// Create two subdirectories as separate include paths.
		includeDir1 := filepath.Join(dir, "include1")
		includeDir2 := filepath.Join(dir, "include2")
		excludeDir := filepath.Join(dir, "outside")

		for _, d := range []string{includeDir1, includeDir2, excludeDir} {
			if err := os.MkdirAll(d, 0o755); err != nil {
				rt.Fatalf("MkdirAll failed: %v", err)
			}
		}

		// Generate files in each directory.
		fileCount := rapid.IntRange(2, 8).Draw(rt, "fileCount")
		for i := 0; i < fileCount; i++ {
			fileName := rapid.StringMatching(`[a-z]{3,8}\.txt`).Draw(rt, "fileName")
			targetDir := rapid.SampledFrom([]string{includeDir1, includeDir2, excludeDir}).Draw(rt, "targetDir")
			data := make([]byte, rapid.IntRange(1, 100).Draw(rt, "size"))
			if err := os.WriteFile(filepath.Join(targetDir, fileName), data, 0o644); err != nil {
				rt.Fatalf("WriteFile failed: %v", err)
			}
		}

		// Scan only the include paths (not the excludeDir).
		includePaths := []string{includeDir1, includeDir2}
		results, err := Scan(context.Background(), includePaths, nil, 0)
		if err != nil {
			rt.Fatalf("Scan failed: %v", err)
		}

		// Verify: all results are within one of the include paths.
		for _, f := range results {
			absInclude1, _ := filepath.Abs(includeDir1)
			absInclude2, _ := filepath.Abs(includeDir2)

			if !strings.HasPrefix(f.Path, absInclude1) && !strings.HasPrefix(f.Path, absInclude2) {
				rt.Fatalf("file %s is not within any include path", f.Path)
			}
		}
	})
}

// TestProperty_ScanFullIncludesAllMatchingFiles verifies that FULL-level scan
// includes ALL files matching include paths minus excludes minus oversized.
// This is the completeness property: nothing is accidentally omitted.
func TestProperty_ScanFullIncludesAllMatchingFiles(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		dir := t.TempDir()

		// Generate a random file tree with 5-15 files.
		fileCount := rapid.IntRange(5, 15).Draw(rt, "fileCount")
		extensions := []string{".txt", ".tmp", ".log", ".go", ".md"}
		type fileInfo struct {
			relPath string
			size    int
		}
		createdFiles := make([]fileInfo, 0, fileCount)

		for i := 0; i < fileCount; i++ {
			ext := rapid.SampledFrom(extensions).Draw(rt, "ext")
			fileName := rapid.StringMatching(`[a-z]{3,8}`).Draw(rt, "fileName") + ext
			fullPath := filepath.Join(dir, fileName)

			size := rapid.IntRange(1, 2000).Draw(rt, "fileSize")
			data := make([]byte, size)
			if err := os.WriteFile(fullPath, data, 0o644); err != nil {
				rt.Fatalf("WriteFile failed: %v", err)
			}
			createdFiles = append(createdFiles, fileInfo{relPath: fileName, size: size})
		}

		// Choose exclude patterns.
		excludePatterns := []string{"*.tmp", "*.log"}

		// Choose a random max file size (500-1500 bytes); 0 means no limit.
		useMaxSize := rapid.Bool().Draw(rt, "useMaxSize")
		var maxFileSize int64
		if useMaxSize {
			maxFileSize = int64(rapid.IntRange(500, 1500).Draw(rt, "maxFileSize"))
		}

		// Run the scan.
		results, err := Scan(context.Background(), []string{dir}, excludePatterns, maxFileSize)
		if err != nil {
			rt.Fatalf("Scan failed: %v", err)
		}

		// Build set of result paths for quick lookup.
		resultPaths := make(map[string]bool)
		for _, f := range results {
			resultPaths[f.Path] = true
		}

		// Verify completeness: every file that SHOULD be included IS included.
		for _, cf := range createdFiles {
			// Check if file should be excluded by pattern.
			excluded := false
			for _, pattern := range excludePatterns {
				if matched, _ := filepath.Match(pattern, cf.relPath); matched {
					excluded = true
					break
				}
			}
			if excluded {
				continue
			}

			// Check if file exceeds max size.
			if maxFileSize > 0 && int64(cf.size) > maxFileSize {
				continue
			}

			// This file should be in results.
			absPath := filepath.Join(dir, cf.relPath)
			// Resolve to absolute path as Scan does.
			absPath, _ = filepath.Abs(absPath)
			if !resultPaths[absPath] {
				rt.Fatalf("expected file %s to be in scan results but it was missing (size=%d, maxFileSize=%d)",
					cf.relPath, cf.size, maxFileSize)
			}
		}

		// Also verify exclusion correctness from the other direction.
		for _, f := range results {
			for _, pattern := range excludePatterns {
				if matched, _ := filepath.Match(pattern, f.Name); matched {
					rt.Fatalf("excluded file appeared in results: %s matched pattern %s", f.Name, pattern)
				}
			}
			if maxFileSize > 0 && f.Size > maxFileSize {
				rt.Fatalf("oversized file in results: %s has size %d, max is %d", f.Name, f.Size, maxFileSize)
			}
		}
	})
}
