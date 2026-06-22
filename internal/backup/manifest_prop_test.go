package backup

import (
	"fmt"
	"testing"

	"github.com/gcclinux/tergum/internal/model"
	"pgregory.net/rapid"
)

// **Validates: Requirements 6.3, 6.4, 6.5**

// TestProperty_ManifestDiffCorrectness verifies that for any client manifest (set of
// BLAKE3 hashes) and any server-stored hash set, the ManifestDiff contains exactly the
// hashes present in the manifest but absent from the server store. The number of
// deduplicated files equals the manifest size minus the ManifestDiff size (unique needed hashes).
func TestProperty_ManifestDiffCorrectness(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate a random number of manifest entries (0-100)
		numEntries := rapid.IntRange(0, 100).Draw(rt, "numEntries")

		// Generate a pool of unique hashes to draw from
		poolSize := rapid.IntRange(1, 50).Draw(rt, "hashPoolSize")
		hashPool := make([]string, poolSize)
		for i := range hashPool {
			hashPool[i] = fmt.Sprintf("%064x", rapid.IntRange(0, 1<<30).Draw(rt, fmt.Sprintf("hashSeed_%d", i)))
		}

		// Build client manifest by picking hashes from the pool (allows duplicates)
		clientManifest := make([]model.ManifestEntry, numEntries)
		for i := range clientManifest {
			idx := rapid.IntRange(0, poolSize-1).Draw(rt, fmt.Sprintf("manifestHashIdx_%d", i))
			clientManifest[i] = model.ManifestEntry{
				Blake3Hash: hashPool[idx],
				FilePath:   fmt.Sprintf("/path/to/file_%d", i),
				FileSize:   int64(rapid.IntRange(1, 1<<20).Draw(rt, fmt.Sprintf("fileSize_%d", i))),
				ModifiedAt: int64(rapid.IntRange(1000000, 2000000).Draw(rt, fmt.Sprintf("modTime_%d", i))),
			}
		}

		// Generate server hash set from the pool (subset of pool plus some extras)
		numServerHashes := rapid.IntRange(0, poolSize+10).Draw(rt, "numServerHashes")
		serverHashes := make(map[string]bool)
		for i := 0; i < numServerHashes; i++ {
			// Pick from pool or generate new hash
			if rapid.Bool().Draw(rt, fmt.Sprintf("usePool_%d", i)) && poolSize > 0 {
				idx := rapid.IntRange(0, poolSize-1).Draw(rt, fmt.Sprintf("serverHashIdx_%d", i))
				serverHashes[hashPool[idx]] = true
			} else {
				serverHashes[fmt.Sprintf("server_only_%064x", i)] = true
			}
		}

		// Call ComputeDiff
		diff := ComputeDiff(clientManifest, serverHashes)

		// Property 1: TotalFiles equals manifest size
		if diff.TotalFiles != len(clientManifest) {
			rt.Fatalf("TotalFiles = %d, want %d (manifest size)", diff.TotalFiles, len(clientManifest))
		}

		// Property 2: Every hash in NeededHashes is NOT in serverHashes
		for _, h := range diff.NeededHashes {
			if serverHashes[h] {
				rt.Fatalf("NeededHashes contains %q which IS in serverHashes", h)
			}
		}

		// Property 3: Every unique hash in manifest that is NOT in serverHashes appears in NeededHashes
		neededSet := make(map[string]bool)
		for _, h := range diff.NeededHashes {
			neededSet[h] = true
		}
		for _, entry := range clientManifest {
			if !serverHashes[entry.Blake3Hash] {
				if !neededSet[entry.Blake3Hash] {
					rt.Fatalf("hash %q is in manifest and NOT on server, but missing from NeededHashes", entry.Blake3Hash)
				}
			}
		}

		// Property 4: NeededHashes contains only unique values (no duplicates)
		if len(neededSet) != len(diff.NeededHashes) {
			rt.Fatalf("NeededHashes has duplicates: len(set)=%d, len(slice)=%d", len(neededSet), len(diff.NeededHashes))
		}

		// Property 5: DedupCount equals number of manifest entries whose hash IS in serverHashes
		expectedDedup := 0
		for _, entry := range clientManifest {
			if serverHashes[entry.Blake3Hash] {
				expectedDedup++
			}
		}
		if diff.DedupCount != expectedDedup {
			rt.Fatalf("DedupCount = %d, want %d", diff.DedupCount, expectedDedup)
		}

		// Property 6: DedupCount + number of entries NOT on server == TotalFiles
		entriesNotOnServer := 0
		for _, entry := range clientManifest {
			if !serverHashes[entry.Blake3Hash] {
				entriesNotOnServer++
			}
		}
		if diff.DedupCount+entriesNotOnServer != diff.TotalFiles {
			rt.Fatalf("DedupCount(%d) + entriesNotOnServer(%d) != TotalFiles(%d)",
				diff.DedupCount, entriesNotOnServer, diff.TotalFiles)
		}
	})
}
