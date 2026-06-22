package server

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/gcclinux/tergum/internal/backup"
	"github.com/gcclinux/tergum/internal/config"
)

func TestNew_ReturnsServer(t *testing.T) {
	cfg := &config.Config{}
	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	if srv == nil {
		t.Fatal("New() returned nil server")
	}
	if srv.cfg != cfg {
		t.Error("Server.cfg does not match provided config")
	}
	if srv.logger == nil {
		t.Error("Server.logger should not be nil")
	}
}

func TestNew_DefaultState(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			CommandPort: 7400,
			DataPort:    7401,
		},
		Metrics: config.MetricsConfig{
			Enabled: true,
			Port:    7490,
		},
	}

	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	// Before Start(), subsystems should be nil.
	if srv.grpcCmd != nil {
		t.Error("grpcCmd should be nil before Start()")
	}
	if srv.grpcData != nil {
		t.Error("grpcData should be nil before Start()")
	}
	if srv.metrics != nil {
		t.Error("metrics should be nil before Start()")
	}
	if srv.repo != nil {
		t.Error("repo should be nil before Start()")
	}
}

func TestStop_Idempotent(t *testing.T) {
	cfg := &config.Config{}
	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	// Stop should be safe to call even without Start.
	err = srv.Stop()
	if err != nil {
		t.Fatalf("Stop() error: %v", err)
	}

	// Calling Stop again should also be safe (idempotent via sync.Once).
	err = srv.Stop()
	if err != nil {
		t.Fatalf("second Stop() error: %v", err)
	}
}

func TestStoragePathFromDB(t *testing.T) {
	tests := []struct {
		dbPath string
		want   string
	}{
		{"data/tergum.db", "data/storage"},
		{"tergum.db", "storage"},
	}

	for _, tt := range tests {
		got := storagePathFromDB(tt.dbPath)
		// Use filepath.Join to build expected paths so tests pass cross-platform.
		want := filepath.Join(filepath.Dir(tt.dbPath), "storage")
		if got != want {
			t.Errorf("storagePathFromDB(%q) = %q, want %q", tt.dbPath, got, want)
		}
	}
}

func TestClientsDirFromDB(t *testing.T) {
	tests := []struct {
		dbPath string
		want   string
	}{
		{"data/tergum.db", "data/clients"},
		{"tergum.db", "clients"},
	}

	for _, tt := range tests {
		got := clientsDirFromDB(tt.dbPath)
		want := filepath.Join(filepath.Dir(tt.dbPath), "clients")
		if got != want {
			t.Errorf("clientsDirFromDB(%q) = %q, want %q", tt.dbPath, got, want)
		}
	}
}

func TestNoopBackupEngine(t *testing.T) {
	eng := &noopBackupEngine{}

	ctx := context.Background()

	// RunBackup should return an error (server doesn't initiate backups).
	_, err := eng.RunBackup(ctx, backup.BackupRequest{})
	if err == nil {
		t.Error("noopBackupEngine.RunBackup should return an error")
	}

	// Stop should succeed.
	if err := eng.Stop(ctx); err != nil {
		t.Errorf("noopBackupEngine.Stop() error: %v", err)
	}
}

func TestVersion(t *testing.T) {
	if Version == "" {
		t.Error("Version should not be empty")
	}
}

func TestRunRetentionLoop_StopsOnCancel(t *testing.T) {
	cfg := &config.Config{}
	srv, _ := New(cfg)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		srv.runRetentionLoop(ctx)
		close(done)
	}()

	// Cancel immediately.
	cancel()

	select {
	case <-done:
		// OK, loop exited.
	case <-time.After(2 * time.Second):
		t.Fatal("runRetentionLoop did not stop after context cancellation")
	}
}
