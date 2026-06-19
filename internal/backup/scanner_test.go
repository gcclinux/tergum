package backup

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// helper to create a temp file with specific content and return its path.
func createFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestScan_BasicIncludePath(t *testing.T) {
	dir := t.TempDir()
	createFile(t, dir, "file1.txt", "hello")
	createFile(t, dir, "file2.go", "package main")
	createFile(t, dir, "subdir/file3.md", "# readme")

	files, err := Scan(context.Background(), []string{dir}, nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	if len(files) != 3 {
		t.Fatalf("expected 3 files, got %d", len(files))
	}

	// Verify all files have metadata set.
	for _, f := range files {
		if f.Path == "" {
			t.Error("expected non-empty path")
		}
		if f.Name == "" {
			t.Error("expected non-empty name")
		}
		if f.OS == "" {
			t.Error("expected OS field to be set")
		}
		if f.ModifiedAt == nil {
			t.Error("expected ModifiedAt to be set")
		}
		if f.Permissions == nil {
			t.Error("expected Permissions to be set")
		}
	}
}

func TestScan_ExcludePatternFiltersFiles(t *testing.T) {
	dir := t.TempDir()
	createFile(t, dir, "file1.txt", "hello")
	createFile(t, dir, "file2.tmp", "temp data")
	createFile(t, dir, "file3.log", "log entry")
	createFile(t, dir, "file4.go", "package main")

	files, err := Scan(context.Background(), []string{dir}, []string{"*.tmp", "*.log"}, 0)
	if err != nil {
		t.Fatal(err)
	}

	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(files))
	}

	for _, f := range files {
		if f.Ext == ".tmp" || f.Ext == ".log" {
			t.Errorf("excluded file should not appear: %s", f.Name)
		}
	}
}

func TestScan_ExcludePatternSkipsDirectories(t *testing.T) {
	dir := t.TempDir()
	createFile(t, dir, "keep.txt", "keep me")
	createFile(t, dir, "node_modules/package.json", `{"name":"test"}`)
	createFile(t, dir, "node_modules/index.js", "module.exports = {}")
	createFile(t, dir, ".git/config", "[core]")

	files, err := Scan(context.Background(), []string{dir}, []string{"node_modules", ".git"}, 0)
	if err != nil {
		t.Fatal(err)
	}

	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	if files[0].Name != "keep.txt" {
		t.Errorf("expected keep.txt, got %s", files[0].Name)
	}
}

func TestScan_MaxFileSizeFiltering(t *testing.T) {
	dir := t.TempDir()
	createFile(t, dir, "small.txt", "hi")                                             // 2 bytes
	createFile(t, dir, "medium.txt", "hello world, this is a medium-sized file")       // ~40 bytes
	createFile(t, dir, "large.txt", string(make([]byte, 1024)))                        // 1024 bytes

	// Set max to 100 bytes — should exclude the large file.
	files, err := Scan(context.Background(), []string{dir}, nil, 100)
	if err != nil {
		t.Fatal(err)
	}

	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(files))
	}

	for _, f := range files {
		if f.Size > 100 {
			t.Errorf("file %s exceeds max size: %d", f.Name, f.Size)
		}
	}
}

func TestScan_MaxFileSizeZeroMeansNoLimit(t *testing.T) {
	dir := t.TempDir()
	createFile(t, dir, "small.txt", "hi")
	createFile(t, dir, "large.txt", string(make([]byte, 10*1024)))

	files, err := Scan(context.Background(), []string{dir}, nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	if len(files) != 2 {
		t.Fatalf("expected 2 files (no size limit), got %d", len(files))
	}
}

func TestScan_SymlinkDetection(t *testing.T) {
	dir := t.TempDir()
	target := createFile(t, dir, "target.txt", "target content")
	linkPath := filepath.Join(dir, "link.txt")

	if err := os.Symlink(target, linkPath); err != nil {
		t.Skip("symlinks not supported on this platform:", err)
	}

	files, err := Scan(context.Background(), []string{dir}, nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	var foundLink bool
	for _, f := range files {
		if f.Name == "link.txt" {
			foundLink = true
			if !f.Symlink {
				t.Error("expected link.txt to be detected as symlink")
			}
			if f.SymlinkTarget != target {
				t.Errorf("expected symlink target %q, got %q", target, f.SymlinkTarget)
			}
		}
	}
	if !foundLink {
		t.Error("symlink file not found in scan results")
	}
}

func TestScan_HiddenFileDetection(t *testing.T) {
	dir := t.TempDir()
	createFile(t, dir, ".hidden", "secret")
	createFile(t, dir, "visible.txt", "public")

	files, err := Scan(context.Background(), []string{dir}, nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(files))
	}

	for _, f := range files {
		if f.Name == ".hidden" && !f.Hidden {
			t.Error("expected .hidden to have Hidden=true")
		}
		if f.Name == "visible.txt" && f.Hidden {
			t.Error("expected visible.txt to have Hidden=false")
		}
	}
}

func TestScan_EmptyIncludePathsReturnsEmpty(t *testing.T) {
	files, err := Scan(context.Background(), nil, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("expected 0 files for empty include paths, got %d", len(files))
	}

	files, err = Scan(context.Background(), []string{}, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("expected 0 files for empty include paths, got %d", len(files))
	}
}

func TestScan_ContextCancellationStopsScan(t *testing.T) {
	dir := t.TempDir()
	// Create many files to increase chance of cancellation during walk.
	for i := 0; i < 100; i++ {
		createFile(t, dir, filepath.Join("subdir", "file"+string(rune('a'+i%26))+".txt"), "data")
	}

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel immediately.
	cancel()

	// Give a tiny moment for context to register.
	time.Sleep(1 * time.Millisecond)

	_, err := Scan(ctx, []string{dir}, nil, 0)
	if err == nil {
		t.Fatal("expected context cancellation error, got nil")
	}
	if err != context.Canceled {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestScan_FileMetadataCapture(t *testing.T) {
	dir := t.TempDir()
	createFile(t, dir, "test.go", "package test")

	files, err := Scan(context.Background(), []string{dir}, nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}

	f := files[0]
	if f.Name != "test.go" {
		t.Errorf("expected name 'test.go', got %q", f.Name)
	}
	if f.Ext != ".go" {
		t.Errorf("expected ext '.go', got %q", f.Ext)
	}
	if f.Size != int64(len("package test")) {
		t.Errorf("expected size %d, got %d", len("package test"), f.Size)
	}
	if f.OS == "" {
		t.Error("expected OS to be set")
	}
}

func TestScan_NonExistentIncludePathSkipped(t *testing.T) {
	dir := t.TempDir()
	createFile(t, dir, "real.txt", "data")

	files, err := Scan(context.Background(), []string{"/non/existent/path", dir}, nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	if files[0].Name != "real.txt" {
		t.Errorf("expected 'real.txt', got %q", files[0].Name)
	}
}
