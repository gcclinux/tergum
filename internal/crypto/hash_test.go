package crypto

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHashBytes_Empty(t *testing.T) {
	hash := HashBytes([]byte{})
	// BLAKE3 hash of empty input is a known constant
	if len(hash) != 64 {
		t.Errorf("expected 64 hex characters, got %d", len(hash))
	}
	if hash != strings.ToLower(hash) {
		t.Error("expected lowercase hex string")
	}
}

func TestHashBytes_Deterministic(t *testing.T) {
	data := []byte("hello, tergum backup system")
	h1 := HashBytes(data)
	h2 := HashBytes(data)
	if h1 != h2 {
		t.Errorf("same input produced different hashes: %s vs %s", h1, h2)
	}
}

func TestHashBytes_DifferentInputs(t *testing.T) {
	h1 := HashBytes([]byte("foo"))
	h2 := HashBytes([]byte("bar"))
	if h1 == h2 {
		t.Error("different inputs produced the same hash")
	}
}

func TestHashFile_Success(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "testfile.txt")
	content := []byte("file content for hashing")

	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatal(err)
	}

	hash, err := HashFile(path)
	if err != nil {
		t.Fatalf("HashFile returned error: %v", err)
	}

	// Should match HashBytes on same content
	expected := HashBytes(content)
	if hash != expected {
		t.Errorf("HashFile(%q) = %s, want %s", path, hash, expected)
	}
}

func TestHashFile_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.txt")

	if err := os.WriteFile(path, []byte{}, 0644); err != nil {
		t.Fatal(err)
	}

	hash, err := HashFile(path)
	if err != nil {
		t.Fatalf("HashFile returned error: %v", err)
	}

	expected := HashBytes([]byte{})
	if hash != expected {
		t.Errorf("HashFile empty = %s, want %s", hash, expected)
	}
}

func TestHashFile_NonExistent(t *testing.T) {
	_, err := HashFile("/nonexistent/path/file.txt")
	if err == nil {
		t.Error("expected error for non-existent file, got nil")
	}
}

func TestHashFile_LargeFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "large.bin")

	// Create a 1MB file to test streaming behavior
	data := make([]byte, 1<<20)
	for i := range data {
		data[i] = byte(i % 256)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}

	hash, err := HashFile(path)
	if err != nil {
		t.Fatalf("HashFile returned error: %v", err)
	}

	expected := HashBytes(data)
	if hash != expected {
		t.Errorf("HashFile large = %s, want %s", hash, expected)
	}

	if len(hash) != 64 {
		t.Errorf("expected 64 hex characters, got %d", len(hash))
	}
}
