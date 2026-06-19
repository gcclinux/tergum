package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// TestBuild verifies the project compiles cleanly with CGO_ENABLED=0.
func TestBuild(t *testing.T) {
	tmpDir := t.TempDir()
	binaryName := "tergum"
	if runtime.GOOS == "windows" {
		binaryName = "tergum.exe"
	}
	outputPath := filepath.Join(tmpDir, binaryName)

	cmd := exec.Command("go", "build", "-o", outputPath, ".")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	cmd.Dir = "."

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build failed: %v\noutput: %s", err, out)
	}

	// Verify the binary was produced
	info, err := os.Stat(outputPath)
	if err != nil {
		t.Fatalf("binary not found at %s: %v", outputPath, err)
	}
	if info.Size() == 0 {
		t.Fatal("binary has zero size")
	}
}

// TestVersionCommand verifies `tergum version` exits with code 0.
func TestVersionCommand(t *testing.T) {
	tmpDir := t.TempDir()
	binaryName := "tergum"
	if runtime.GOOS == "windows" {
		binaryName = "tergum.exe"
	}
	outputPath := filepath.Join(tmpDir, binaryName)

	// Build the binary first
	build := exec.Command("go", "build", "-o", outputPath, ".")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	build.Dir = "."

	out, err := build.CombinedOutput()
	if err != nil {
		t.Fatalf("build failed: %v\noutput: %s", err, out)
	}

	// Run `tergum version`
	run := exec.Command(outputPath, "version")
	out, err = run.CombinedOutput()
	if err != nil {
		t.Fatalf("tergum version failed: %v\noutput: %s", err, out)
	}

	// Verify output contains version info
	output := string(out)
	if len(output) == 0 {
		t.Fatal("version command produced no output")
	}
}
