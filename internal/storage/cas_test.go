package storage

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// mockRefCounter implements RefCounter for testing.
type mockRefCounter struct {
	counts map[string]int64
}

func (m *mockRefCounter) CountHashReferences(ctx context.Context, hash string) (int64, error) {
	return m.counts[hash], nil
}

// validHash is a 64-character lowercase hex string for testing.
const validHash = "ab1234abcdef5678901234567890123456789012345678901234567890abcdef"

func TestPutAndGetRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store := NewCAS(dir, nil)
	ctx := context.Background()

	content := []byte("hello, content-addressable world!")

	err := store.Put(ctx, validHash, bytes.NewReader(content))
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	rc, err := store.Get(ctx, validHash)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	defer rc.Close()

	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("reading content failed: %v", err)
	}

	if !bytes.Equal(got, content) {
		t.Errorf("content mismatch: got %q, want %q", got, content)
	}
}

func TestExistsReturnsTrueAfterPut(t *testing.T) {
	dir := t.TempDir()
	store := NewCAS(dir, nil)
	ctx := context.Background()

	// Before Put, Exists should return false.
	exists, err := store.Exists(ctx, validHash)
	if err != nil {
		t.Fatalf("Exists failed: %v", err)
	}
	if exists {
		t.Error("Exists returned true before Put")
	}

	content := []byte("test data")
	err = store.Put(ctx, validHash, bytes.NewReader(content))
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// After Put, Exists should return true.
	exists, err = store.Exists(ctx, validHash)
	if err != nil {
		t.Fatalf("Exists failed: %v", err)
	}
	if !exists {
		t.Error("Exists returned false after Put")
	}
}

func TestDeleteRemovesFile(t *testing.T) {
	dir := t.TempDir()
	store := NewCAS(dir, nil)
	ctx := context.Background()

	content := []byte("data to delete")
	err := store.Put(ctx, validHash, bytes.NewReader(content))
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	err = store.Delete(ctx, validHash)
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	exists, err := store.Exists(ctx, validHash)
	if err != nil {
		t.Fatalf("Exists failed: %v", err)
	}
	if exists {
		t.Error("file still exists after Delete")
	}
}

func TestPutCreatesTwoLevelDirectoryStructure(t *testing.T) {
	dir := t.TempDir()
	store := NewCAS(dir, nil)
	ctx := context.Background()

	content := []byte("some content")
	err := store.Put(ctx, validHash, bytes.NewReader(content))
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Check two-level directory structure: baseDir/ab/ab1234...
	prefix := validHash[:2]
	subdir := filepath.Join(dir, prefix)
	info, err := os.Stat(subdir)
	if err != nil {
		t.Fatalf("subdirectory does not exist: %v", err)
	}
	if !info.IsDir() {
		t.Error("expected subdirectory to be a directory")
	}

	filePath := filepath.Join(subdir, validHash)
	_, err = os.Stat(filePath)
	if err != nil {
		t.Fatalf("file does not exist at expected path: %v", err)
	}
}

func TestGetNonExistentHashReturnsError(t *testing.T) {
	dir := t.TempDir()
	store := NewCAS(dir, nil)
	ctx := context.Background()

	nonExistentHash := "cd9012fedcba3456789012345678901234567890123456789012345678901234"
	_, err := store.Get(ctx, nonExistentHash)
	if err == nil {
		t.Fatal("expected error for non-existent hash")
	}
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}

func TestInvalidHashFormatReturnsError(t *testing.T) {
	dir := t.TempDir()
	store := NewCAS(dir, nil)
	ctx := context.Background()

	invalidHashes := []string{
		"",                    // empty
		"abc",                 // too short
		"ABCDEF1234567890ABCDEF1234567890ABCDEF1234567890ABCDEF1234567890", // uppercase
		"zz1234abcdef5678901234567890123456789012345678901234567890abcdef", // invalid hex
		"ab1234abcdef5678901234567890123456789012345678901234567890abcde",  // 63 chars
		"ab1234abcdef5678901234567890123456789012345678901234567890abcdeff", // 65 chars
	}

	for _, hash := range invalidHashes {
		t.Run("Put_"+hash, func(t *testing.T) {
			err := store.Put(ctx, hash, bytes.NewReader([]byte("data")))
			if err != ErrInvalidHash {
				t.Errorf("Put(%q): expected ErrInvalidHash, got: %v", hash, err)
			}
		})

		t.Run("Get_"+hash, func(t *testing.T) {
			_, err := store.Get(ctx, hash)
			if err != ErrInvalidHash {
				t.Errorf("Get(%q): expected ErrInvalidHash, got: %v", hash, err)
			}
		})

		t.Run("Exists_"+hash, func(t *testing.T) {
			_, err := store.Exists(ctx, hash)
			if err != ErrInvalidHash {
				t.Errorf("Exists(%q): expected ErrInvalidHash, got: %v", hash, err)
			}
		})

		t.Run("Delete_"+hash, func(t *testing.T) {
			err := store.Delete(ctx, hash)
			if err != ErrInvalidHash {
				t.Errorf("Delete(%q): expected ErrInvalidHash, got: %v", hash, err)
			}
		})

		t.Run("RefCount_"+hash, func(t *testing.T) {
			_, err := store.RefCount(ctx, hash)
			if err != ErrInvalidHash {
				t.Errorf("RefCount(%q): expected ErrInvalidHash, got: %v", hash, err)
			}
		})
	}
}

func TestRefCountWithNilRefCounter(t *testing.T) {
	dir := t.TempDir()
	store := NewCAS(dir, nil)
	ctx := context.Background()

	count, err := store.RefCount(ctx, validHash)
	if err != nil {
		t.Fatalf("RefCount failed: %v", err)
	}
	if count != 0 {
		t.Errorf("expected RefCount 0 with nil RefCounter, got %d", count)
	}
}

func TestRefCountWithMockRefCounter(t *testing.T) {
	dir := t.TempDir()
	rc := &mockRefCounter{counts: map[string]int64{validHash: 5}}
	store := NewCAS(dir, rc)
	ctx := context.Background()

	count, err := store.RefCount(ctx, validHash)
	if err != nil {
		t.Fatalf("RefCount failed: %v", err)
	}
	if count != 5 {
		t.Errorf("expected RefCount 5, got %d", count)
	}
}

func TestDeleteNonExistentHashReturnsError(t *testing.T) {
	dir := t.TempDir()
	store := NewCAS(dir, nil)
	ctx := context.Background()

	nonExistentHash := "cd9012fedcba3456789012345678901234567890123456789012345678901234"
	err := store.Delete(ctx, nonExistentHash)
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}

func TestDeleteCleansUpEmptyParentDir(t *testing.T) {
	dir := t.TempDir()
	store := NewCAS(dir, nil)
	ctx := context.Background()

	content := []byte("cleanup test")
	err := store.Put(ctx, validHash, bytes.NewReader(content))
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	err = store.Delete(ctx, validHash)
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// The parent directory (hash[:2]) should be removed since it's now empty.
	parentDir := filepath.Join(dir, validHash[:2])
	_, err = os.Stat(parentDir)
	if !os.IsNotExist(err) {
		t.Error("expected parent directory to be removed after deleting the only file")
	}
}

func TestPutOverwritesExistingContent(t *testing.T) {
	dir := t.TempDir()
	store := NewCAS(dir, nil)
	ctx := context.Background()

	// First put
	err := store.Put(ctx, validHash, bytes.NewReader([]byte("original")))
	if err != nil {
		t.Fatalf("first Put failed: %v", err)
	}

	// Second put with different content (same hash - simulates re-store)
	newContent := []byte("updated content")
	err = store.Put(ctx, validHash, bytes.NewReader(newContent))
	if err != nil {
		t.Fatalf("second Put failed: %v", err)
	}

	rc, err := store.Get(ctx, validHash)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	defer rc.Close()

	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("reading content failed: %v", err)
	}
	if !bytes.Equal(got, newContent) {
		t.Errorf("expected updated content, got %q", got)
	}
}
