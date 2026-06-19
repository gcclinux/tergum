// Package backup implements the backup engine: scanning, manifest exchange, and orchestration.
package backup

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// ScannedFile represents a file discovered during scanning.
type ScannedFile struct {
	Path          string
	Name          string
	Ext           string
	Size          int64
	CreatedAt     *time.Time
	ModifiedAt    *time.Time
	AccessedAt    *time.Time
	Permissions   *uint32
	Owner         string
	FileGroup     string
	Hidden        bool
	Symlink       bool
	SymlinkTarget string
	OS            string // "linux", "darwin", "windows"
}

// Scan walks all include paths recursively, filters by exclude patterns and
// max file size, and returns the list of files to back up.
// If maxFileSize <= 0, no size limit is applied.
func Scan(ctx context.Context, includePaths []string, excludePatterns []string, maxFileSize int64) ([]ScannedFile, error) {
	if len(includePaths) == 0 {
		return nil, nil
	}

	var results []ScannedFile

	for _, root := range includePaths {
		// Resolve the root path to an absolute path.
		absRoot, err := filepath.Abs(root)
		if err != nil {
			continue
		}

		err = filepath.WalkDir(absRoot, func(path string, d fs.DirEntry, walkErr error) error {
			// Check context cancellation.
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			// If there was an error accessing this path, skip it.
			if walkErr != nil {
				return nil
			}

			// Get the relative path from root for pattern matching.
			relPath, err := filepath.Rel(absRoot, path)
			if err != nil {
				relPath = path
			}

			// Check exclude patterns against both the name and relative path.
			name := d.Name()
			if matchesExclude(name, relPath, excludePatterns) {
				if d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}

			// Skip directories themselves (we only collect files).
			if d.IsDir() {
				return nil
			}

			// Use Lstat to detect symlinks.
			info, err := os.Lstat(path)
			if err != nil {
				// Can't stat, skip gracefully.
				return nil
			}

			// Skip files exceeding max file size (only if maxFileSize > 0).
			if maxFileSize > 0 && info.Size() > maxFileSize {
				return nil
			}

			sf := ScannedFile{
				Path: path,
				Name: name,
				Ext:  filepath.Ext(name),
				Size: info.Size(),
				OS:   runtime.GOOS,
			}

			// Detect symlink.
			if info.Mode()&os.ModeSymlink != 0 {
				sf.Symlink = true
				target, err := os.Readlink(path)
				if err == nil {
					sf.SymlinkTarget = target
				}
			}

			// Detect hidden files (files starting with ".").
			sf.Hidden = isHidden(name)

			// Capture permissions (Unix mode bits).
			perm := uint32(info.Mode().Perm())
			sf.Permissions = &perm

			// Capture timestamps.
			modTime := info.ModTime()
			sf.ModifiedAt = &modTime

			// Platform-specific timestamps and owner/group.
			fillPlatformMetadata(&sf, path, info)

			results = append(results, sf)
			return nil
		})
		if err != nil {
			// If context was cancelled, return immediately.
			if ctx.Err() != nil {
				return results, ctx.Err()
			}
			// For other errors (e.g., root doesn't exist), continue to next include path.
			continue
		}
	}

	return results, nil
}

// matchesExclude checks if a file/directory name or relative path matches any exclude pattern.
func matchesExclude(name, relPath string, excludePatterns []string) bool {
	for _, pattern := range excludePatterns {
		// Normalize: remove trailing slash from pattern (used to indicate directories).
		cleanPattern := strings.TrimSuffix(pattern, "/")
		cleanPattern = strings.TrimSuffix(cleanPattern, string(filepath.Separator))

		// Match against the file/directory name.
		if matched, _ := filepath.Match(cleanPattern, name); matched {
			return true
		}

		// Match against the relative path.
		if matched, _ := filepath.Match(cleanPattern, relPath); matched {
			return true
		}

		// Also try matching with filepath separator normalization.
		normalizedRel := filepath.ToSlash(relPath)
		normalizedPattern := filepath.ToSlash(cleanPattern)
		if matched, _ := filepath.Match(normalizedPattern, normalizedRel); matched {
			return true
		}
	}
	return false
}

// isHidden determines if a file is hidden. On all platforms, files starting with "."
// are considered hidden.
func isHidden(name string) bool {
	return strings.HasPrefix(name, ".")
}
