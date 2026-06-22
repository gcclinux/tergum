package storage

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/gcclinux/tergum/internal/crypto"
	"pgregory.net/rapid"
)

// **Validates: Requirements 5.1, 5.2, 5.5**

// TestProperty_CASRoundTrip verifies that for any byte sequence, computing its
// BLAKE3 hash, storing it under that hash, and retrieving it by the same hash
// returns content identical to the original.
func TestProperty_CASRoundTrip(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Create a fresh CAS store for each iteration to ensure isolation.
		dir := t.TempDir()
		store := NewCAS(dir, nil)
		ctx := context.Background()

		// Generate a random byte sequence between 0 and 1MB.
		size := rapid.IntRange(0, 1*1024*1024).Draw(rt, "size")
		data := rapid.SliceOfN(rapid.Byte(), size, size).Draw(rt, "data")

		// Compute the BLAKE3 hash of the data.
		hash := crypto.HashBytes(data)

		// Store the data under its hash.
		err := store.Put(ctx, hash, bytes.NewReader(data))
		if err != nil {
			rt.Fatalf("Put failed: %v", err)
		}

		// Retrieve the data by the same hash.
		rc, err := store.Get(ctx, hash)
		if err != nil {
			rt.Fatalf("Get failed: %v", err)
		}
		defer rc.Close()

		retrieved, err := io.ReadAll(rc)
		if err != nil {
			rt.Fatalf("ReadAll failed: %v", err)
		}

		// Verify content is identical.
		if !bytes.Equal(retrieved, data) {
			rt.Fatalf("round-trip content mismatch: stored %d bytes, retrieved %d bytes", len(data), len(retrieved))
		}
	})
}

// TestProperty_BLAKE3Determinism verifies that computing the BLAKE3 hash of the
// same content always produces the same 256-bit hash (determinism).
func TestProperty_BLAKE3Determinism(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate a random byte sequence between 0 and 1MB.
		size := rapid.IntRange(0, 1*1024*1024).Draw(rt, "size")
		data := rapid.SliceOfN(rapid.Byte(), size, size).Draw(rt, "data")

		// Compute hash twice on the same data.
		hash1 := crypto.HashBytes(data)
		hash2 := crypto.HashBytes(data)

		// Verify both hashes are identical.
		if hash1 != hash2 {
			rt.Fatalf("BLAKE3 non-deterministic: hash1=%s, hash2=%s for %d bytes", hash1, hash2, len(data))
		}

		// Verify hash is exactly 64 lowercase hex characters (256-bit).
		if len(hash1) != 64 {
			rt.Fatalf("expected 64-char hex hash, got %d chars: %s", len(hash1), hash1)
		}
	})
}

// TestProperty_CASExistsAfterPut verifies that after storing content by its
// BLAKE3 hash, Exists returns true for that hash.
func TestProperty_CASExistsAfterPut(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Create a fresh CAS store for each iteration.
		dir := t.TempDir()
		store := NewCAS(dir, nil)
		ctx := context.Background()

		// Generate a random byte sequence between 0 and 1MB.
		size := rapid.IntRange(0, 1*1024*1024).Draw(rt, "size")
		data := rapid.SliceOfN(rapid.Byte(), size, size).Draw(rt, "data")

		// Compute the BLAKE3 hash.
		hash := crypto.HashBytes(data)

		// Before Put, Exists should return false.
		existsBefore, err := store.Exists(ctx, hash)
		if err != nil {
			rt.Fatalf("Exists (before Put) failed: %v", err)
		}
		if existsBefore {
			rt.Fatalf("Exists returned true before Put for hash %s", hash)
		}

		// Store the data.
		err = store.Put(ctx, hash, bytes.NewReader(data))
		if err != nil {
			rt.Fatalf("Put failed: %v", err)
		}

		// After Put, Exists should return true.
		existsAfter, err := store.Exists(ctx, hash)
		if err != nil {
			rt.Fatalf("Exists (after Put) failed: %v", err)
		}
		if !existsAfter {
			rt.Fatalf("Exists returned false after Put for hash %s", hash)
		}
	})
}
