package backup

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gcclinux/tergum/internal/crypto"
	"github.com/gcclinux/tergum/internal/model"
)

func TestBuildManifest_WithTempFiles(t *testing.T) {
	dir := t.TempDir()

	// Create temp files with known content
	file1 := filepath.Join(dir, "file1.txt")
	file2 := filepath.Join(dir, "file2.txt")

	if err := os.WriteFile(file1, []byte("hello world"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file2, []byte("goodbye world"), 0644); err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	files := []ScannedFile{
		{Path: file1, Name: "file1.txt", Ext: ".txt", Size: 11, ModifiedAt: &now},
		{Path: file2, Name: "file2.txt", Ext: ".txt", Size: 13, ModifiedAt: &now},
	}

	manifest, err := BuildManifest(files)
	if err != nil {
		t.Fatalf("BuildManifest returned error: %v", err)
	}

	if len(manifest) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(manifest))
	}

	// Verify hashes match what crypto.HashFile produces
	expectedHash1, _ := crypto.HashFile(file1)
	expectedHash2, _ := crypto.HashFile(file2)

	if manifest[0].Blake3Hash != expectedHash1 {
		t.Errorf("manifest[0] hash = %q, want %q", manifest[0].Blake3Hash, expectedHash1)
	}
	if manifest[1].Blake3Hash != expectedHash2 {
		t.Errorf("manifest[1] hash = %q, want %q", manifest[1].Blake3Hash, expectedHash2)
	}

	// Verify other fields
	if manifest[0].FilePath != file1 {
		t.Errorf("manifest[0] path = %q, want %q", manifest[0].FilePath, file1)
	}
	if manifest[0].FileSize != 11 {
		t.Errorf("manifest[0] size = %d, want 11", manifest[0].FileSize)
	}
	if manifest[0].ModifiedAt != now.Unix() {
		t.Errorf("manifest[0] ModifiedAt = %d, want %d", manifest[0].ModifiedAt, now.Unix())
	}
}

func TestBuildManifest_SkipsUnreadableFiles(t *testing.T) {
	dir := t.TempDir()

	// Create one valid file
	validFile := filepath.Join(dir, "valid.txt")
	if err := os.WriteFile(validFile, []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	files := []ScannedFile{
		{Path: filepath.Join(dir, "nonexistent.txt"), Name: "nonexistent.txt", Size: 0, ModifiedAt: &now},
		{Path: validFile, Name: "valid.txt", Size: 7, ModifiedAt: &now},
	}

	manifest, err := BuildManifest(files)
	if err != nil {
		t.Fatalf("BuildManifest returned error: %v", err)
	}

	// Should skip the nonexistent file and include only the valid one
	if len(manifest) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(manifest))
	}
	if manifest[0].FilePath != validFile {
		t.Errorf("manifest[0] path = %q, want %q", manifest[0].FilePath, validFile)
	}
}

func TestComputeDiff_NoOverlap(t *testing.T) {
	clientManifest := []model.ManifestEntry{
		{Blake3Hash: "aaa", FilePath: "/a", FileSize: 10, ModifiedAt: 1000},
		{Blake3Hash: "bbb", FilePath: "/b", FileSize: 20, ModifiedAt: 2000},
		{Blake3Hash: "ccc", FilePath: "/c", FileSize: 30, ModifiedAt: 3000},
	}

	serverHashes := map[string]bool{
		"xxx": true,
		"yyy": true,
	}

	diff := ComputeDiff(clientManifest, serverHashes)

	if diff.TotalFiles != 3 {
		t.Errorf("TotalFiles = %d, want 3", diff.TotalFiles)
	}
	if diff.DedupCount != 0 {
		t.Errorf("DedupCount = %d, want 0", diff.DedupCount)
	}
	if len(diff.NeededHashes) != 3 {
		t.Errorf("len(NeededHashes) = %d, want 3", len(diff.NeededHashes))
	}
}

func TestComputeDiff_FullOverlap(t *testing.T) {
	clientManifest := []model.ManifestEntry{
		{Blake3Hash: "aaa", FilePath: "/a", FileSize: 10, ModifiedAt: 1000},
		{Blake3Hash: "bbb", FilePath: "/b", FileSize: 20, ModifiedAt: 2000},
	}

	serverHashes := map[string]bool{
		"aaa": true,
		"bbb": true,
		"ccc": true,
	}

	diff := ComputeDiff(clientManifest, serverHashes)

	if diff.TotalFiles != 2 {
		t.Errorf("TotalFiles = %d, want 2", diff.TotalFiles)
	}
	if diff.DedupCount != 2 {
		t.Errorf("DedupCount = %d, want 2", diff.DedupCount)
	}
	if len(diff.NeededHashes) != 0 {
		t.Errorf("len(NeededHashes) = %d, want 0", len(diff.NeededHashes))
	}
}

func TestComputeDiff_PartialOverlap(t *testing.T) {
	clientManifest := []model.ManifestEntry{
		{Blake3Hash: "aaa", FilePath: "/a", FileSize: 10, ModifiedAt: 1000},
		{Blake3Hash: "bbb", FilePath: "/b", FileSize: 20, ModifiedAt: 2000},
		{Blake3Hash: "ccc", FilePath: "/c", FileSize: 30, ModifiedAt: 3000},
	}

	serverHashes := map[string]bool{
		"bbb": true,
	}

	diff := ComputeDiff(clientManifest, serverHashes)

	if diff.TotalFiles != 3 {
		t.Errorf("TotalFiles = %d, want 3", diff.TotalFiles)
	}
	if diff.DedupCount != 1 {
		t.Errorf("DedupCount = %d, want 1", diff.DedupCount)
	}
	if len(diff.NeededHashes) != 2 {
		t.Errorf("len(NeededHashes) = %d, want 2", len(diff.NeededHashes))
	}

	// Verify the needed hashes contain the right values
	needed := make(map[string]bool)
	for _, h := range diff.NeededHashes {
		needed[h] = true
	}
	if !needed["aaa"] {
		t.Error("NeededHashes should contain 'aaa'")
	}
	if !needed["ccc"] {
		t.Error("NeededHashes should contain 'ccc'")
	}
}

func TestComputeDiff_DuplicateHashesInManifest(t *testing.T) {
	// Multiple files with the same hash (identical content)
	clientManifest := []model.ManifestEntry{
		{Blake3Hash: "aaa", FilePath: "/a1", FileSize: 10, ModifiedAt: 1000},
		{Blake3Hash: "aaa", FilePath: "/a2", FileSize: 10, ModifiedAt: 2000},
		{Blake3Hash: "bbb", FilePath: "/b", FileSize: 20, ModifiedAt: 3000},
		{Blake3Hash: "bbb", FilePath: "/b2", FileSize: 20, ModifiedAt: 4000},
	}

	serverHashes := map[string]bool{
		"bbb": true,
	}

	diff := ComputeDiff(clientManifest, serverHashes)

	if diff.TotalFiles != 4 {
		t.Errorf("TotalFiles = %d, want 4", diff.TotalFiles)
	}
	// bbb appears twice in manifest, both match server
	if diff.DedupCount != 2 {
		t.Errorf("DedupCount = %d, want 2", diff.DedupCount)
	}
	// aaa should only appear once in NeededHashes despite being in manifest twice
	if len(diff.NeededHashes) != 1 {
		t.Errorf("len(NeededHashes) = %d, want 1", len(diff.NeededHashes))
	}
	if len(diff.NeededHashes) > 0 && diff.NeededHashes[0] != "aaa" {
		t.Errorf("NeededHashes[0] = %q, want 'aaa'", diff.NeededHashes[0])
	}
}

func TestComputeDiff_EmptyManifest(t *testing.T) {
	clientManifest := []model.ManifestEntry{}
	serverHashes := map[string]bool{"aaa": true, "bbb": true}

	diff := ComputeDiff(clientManifest, serverHashes)

	if diff.TotalFiles != 0 {
		t.Errorf("TotalFiles = %d, want 0", diff.TotalFiles)
	}
	if diff.DedupCount != 0 {
		t.Errorf("DedupCount = %d, want 0", diff.DedupCount)
	}
	if len(diff.NeededHashes) != 0 {
		t.Errorf("len(NeededHashes) = %d, want 0", len(diff.NeededHashes))
	}
}
