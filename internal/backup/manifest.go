package backup

import (
	"log/slog"

	"github.com/gcclinux/tergum/internal/crypto"
	"github.com/gcclinux/tergum/internal/model"
)

// ManifestDiff contains the results of comparing a client manifest with server-stored hashes.
type ManifestDiff struct {
	NeededHashes []string // Hashes present in manifest but absent from server
	DedupCount   int      // Number of files that were already on server (deduplicated)
	TotalFiles   int      // Total number of files in manifest
}

// BuildManifest creates a manifest from scanned files.
// It computes the BLAKE3 hash of each file. If hash computation fails for a file,
// it is skipped and the error is logged.
func BuildManifest(files []ScannedFile) ([]model.ManifestEntry, error) {
	var entries []model.ManifestEntry

	for _, f := range files {
		hash, err := crypto.HashFile(f.Path)
		if err != nil {
			slog.Warn("failed to hash file, skipping", "path", f.Path, "error", err)
			continue
		}

		var modifiedAt int64
		if f.ModifiedAt != nil {
			modifiedAt = f.ModifiedAt.Unix()
		}

		entries = append(entries, model.ManifestEntry{
			Blake3Hash: hash,
			FilePath:   f.Path,
			FileSize:   f.Size,
			ModifiedAt: modifiedAt,
		})
	}

	return entries, nil
}

// ComputeDiff compares a client manifest against a set of server-stored hashes.
// Returns the hashes that need to be uploaded (not yet on server) and the dedup count.
// NeededHashes contains only unique hashes (deduplicated within the diff itself).
func ComputeDiff(clientManifest []model.ManifestEntry, serverHashes map[string]bool) ManifestDiff {
	diff := ManifestDiff{
		TotalFiles: len(clientManifest),
	}

	seen := make(map[string]bool)

	for _, entry := range clientManifest {
		hash := entry.Blake3Hash

		if serverHashes[hash] {
			diff.DedupCount++
		} else {
			// Only add to NeededHashes if we haven't seen this hash already
			if !seen[hash] {
				diff.NeededHashes = append(diff.NeededHashes, hash)
				seen[hash] = true
			}
		}
	}

	return diff
}
