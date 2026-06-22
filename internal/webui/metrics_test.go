package webui

import (
	"runtime"
	"testing"
)

func TestStorageColorScheme_Default(t *testing.T) {
	tests := []struct {
		percent float64
		want    string
	}{
		{0, "blue"},
		{50, "blue"},
		{79.9, "blue"},
		{80, "amber"},
		{85, "amber"},
		{94.9, "amber"},
		{95, "red"},
		{99, "red"},
		{100, "red"},
	}

	for _, tt := range tests {
		got := StorageColorScheme(tt.percent)
		if got != tt.want {
			t.Errorf("StorageColorScheme(%v) = %q, want %q", tt.percent, got, tt.want)
		}
	}
}

func TestStorageColorScheme_Boundaries(t *testing.T) {
	// Exactly at boundary 80%
	if got := StorageColorScheme(80.0); got != "amber" {
		t.Errorf("StorageColorScheme(80.0) = %q, want %q", got, "amber")
	}

	// Just below 80%
	if got := StorageColorScheme(79.99); got != "blue" {
		t.Errorf("StorageColorScheme(79.99) = %q, want %q", got, "blue")
	}

	// Exactly at boundary 95%
	if got := StorageColorScheme(95.0); got != "red" {
		t.Errorf("StorageColorScheme(95.0) = %q, want %q", got, "red")
	}

	// Just below 95%
	if got := StorageColorScheme(94.99); got != "amber" {
		t.Errorf("StorageColorScheme(94.99) = %q, want %q", got, "amber")
	}
}

func TestDiskUsagePercent_EmptyPath(t *testing.T) {
	// Empty path should return 0.
	got := diskUsagePercent("")
	if got != 0 {
		t.Errorf("diskUsagePercent(\"\") = %v, want 0", got)
	}
}

func TestDiskUsagePercent_NonexistentPath(t *testing.T) {
	// Nonexistent path should return 0 (syscall.Statfs fails).
	got := diskUsagePercent("/nonexistent/path/that/does/not/exist")
	if got != 0 {
		t.Errorf("diskUsagePercent(\"/nonexistent/...\") = %v, want 0", got)
	}
}

func TestDiskUsagePercent_ValidPath(t *testing.T) {
	// Root "/" on Linux/Unix, or "C:\" on Windows should return some percentage > 0.
	path := "/"
	if runtime.GOOS == "windows" {
		path = `C:\`
	}
	got := diskUsagePercent(path)
	if got <= 0 || got > 100 {
		t.Errorf("diskUsagePercent(%q) = %v, want value in (0, 100]", path, got)
	}
}
