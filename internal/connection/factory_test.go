package connection

import (
	"testing"

	"github.com/gcclinux/tergum/internal/backup"
	"github.com/gcclinux/tergum/internal/config"
	"github.com/gcclinux/tergum/internal/restore"
)

func TestNewServerConnection_RoleBoth(t *testing.T) {
	cfg := &config.Config{}
	cfg.Node.Role = "both"
	cfg.Backup.StoragePath = "/tmp/test-storage"

	conn, err := NewServerConnection(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	local, ok := conn.(*backup.LocalServerConnection)
	if !ok {
		t.Fatalf("expected *backup.LocalServerConnection, got %T", conn)
	}
	if local.StorageDir != "/tmp/test-storage" {
		t.Errorf("expected StorageDir %q, got %q", "/tmp/test-storage", local.StorageDir)
	}
}

func TestNewServerConnection_RoleServer(t *testing.T) {
	cfg := &config.Config{}
	cfg.Node.Role = "server"

	_, err := NewServerConnection(cfg)
	if err == nil {
		t.Fatal("expected error for server role")
	}
	if err.Error() != "server nodes do not initiate backups" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestNewServerConnection_RoleClientMissingAddress(t *testing.T) {
	cfg := &config.Config{}
	cfg.Node.Role = "client"
	cfg.Server.Address = ""

	_, err := NewServerConnection(cfg)
	if err == nil {
		t.Fatal("expected error for missing server address")
	}
	expected := "server.address is required when node.role is \"client\""
	if err.Error() != expected {
		t.Errorf("expected error %q, got %q", expected, err.Error())
	}
}

func TestNewServerConnection_RoleClientMissingTLS(t *testing.T) {
	cfg := &config.Config{}
	cfg.Node.Role = "client"
	cfg.Server.Address = "192.168.1.5"
	cfg.Server.CommandPort = 7400
	cfg.Server.DataPort = 7401
	// TLS fields left empty — should fail at TLS loading

	_, err := NewServerConnection(cfg)
	if err == nil {
		t.Fatal("expected error for missing TLS config")
	}
	// Should mention TLS credentials
	if !containsSubstring(err.Error(), "TLS") && !containsSubstring(err.Error(), "tls") {
		t.Errorf("expected error to mention TLS, got: %v", err)
	}
}

func TestNewServerConnection_UnknownRole(t *testing.T) {
	cfg := &config.Config{}
	cfg.Node.Role = "unknown"

	_, err := NewServerConnection(cfg)
	if err == nil {
		t.Fatal("expected error for unknown role")
	}
	if !containsSubstring(err.Error(), "unknown node role") {
		t.Errorf("expected error to mention unknown role, got: %v", err)
	}
}

func TestNewDataSource_RoleBoth(t *testing.T) {
	cfg := &config.Config{}
	cfg.Node.Role = "both"
	cfg.Backup.StoragePath = "/tmp/test-storage"

	ds, err := NewDataSource(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	local, ok := ds.(*restore.LocalDataSource)
	if !ok {
		t.Fatalf("expected *restore.LocalDataSource, got %T", ds)
	}
	if local.StorageDir != "/tmp/test-storage" {
		t.Errorf("expected StorageDir %q, got %q", "/tmp/test-storage", local.StorageDir)
	}
}

func TestNewDataSource_RoleServer(t *testing.T) {
	cfg := &config.Config{}
	cfg.Node.Role = "server"

	_, err := NewDataSource(cfg)
	if err == nil {
		t.Fatal("expected error for server role")
	}
	if err.Error() != "server nodes do not initiate restores" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestNewDataSource_RoleClientMissingAddress(t *testing.T) {
	cfg := &config.Config{}
	cfg.Node.Role = "client"
	cfg.Server.Address = ""

	_, err := NewDataSource(cfg)
	if err == nil {
		t.Fatal("expected error for missing server address")
	}
	expected := "server.address is required when node.role is \"client\""
	if err.Error() != expected {
		t.Errorf("expected error %q, got %q", expected, err.Error())
	}
}

func TestNewDataSource_RoleClientMissingTLS(t *testing.T) {
	cfg := &config.Config{}
	cfg.Node.Role = "client"
	cfg.Server.Address = "192.168.1.5"
	cfg.Server.CommandPort = 7400
	cfg.Server.DataPort = 7401
	// TLS fields left empty — should fail at TLS loading

	_, err := NewDataSource(cfg)
	if err == nil {
		t.Fatal("expected error for missing TLS config")
	}
	if !containsSubstring(err.Error(), "TLS") && !containsSubstring(err.Error(), "tls") {
		t.Errorf("expected error to mention TLS, got: %v", err)
	}
}

func TestNewDataSource_UnknownRole(t *testing.T) {
	cfg := &config.Config{}
	cfg.Node.Role = "unknown"

	_, err := NewDataSource(cfg)
	if err == nil {
		t.Fatal("expected error for unknown role")
	}
	if !containsSubstring(err.Error(), "unknown node role") {
		t.Errorf("expected error to mention unknown role, got: %v", err)
	}
}

func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
