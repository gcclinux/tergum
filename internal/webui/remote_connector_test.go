package webui

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/ricardopadilha/tergum/internal/registry"
	_ "modernc.org/sqlite"
)

func newTestRegistryForConnector(t *testing.T) *registry.Registry {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	reg, err := registry.New(registry.Config{
		DB:               db,
		OfflineThreshold: 90 * time.Second,
		CheckInterval:    30 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

func TestRemoteClientConnector_TriggerBackup_ClientNotFound(t *testing.T) {
	reg := newTestRegistryForConnector(t)
	connector := NewRemoteClientConnector(RemoteClientConnectorConfig{
		Registry: reg,
	})

	err := connector.TriggerClientBackup(context.Background(), "nonexistent-client")
	if err == nil {
		t.Fatal("expected error for nonexistent client")
	}
	if got := err.Error(); got != `client "nonexistent-client" not found in registry` {
		t.Errorf("unexpected error message: %s", got)
	}
}

func TestRemoteClientConnector_TriggerBackup_ClientOffline(t *testing.T) {
	reg := newTestRegistryForConnector(t)

	// Register a client then mark it offline.
	_, err := reg.Register("test-client", "localhost:7400")
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.MarkOffline("test-client"); err != nil {
		t.Fatal(err)
	}

	connector := NewRemoteClientConnector(RemoteClientConnectorConfig{
		Registry: reg,
	})

	err = connector.TriggerClientBackup(context.Background(), "test-client")
	if err == nil {
		t.Fatal("expected error for offline client")
	}
	if got := err.Error(); got != `client "test-client" is offline` {
		t.Errorf("unexpected error message: %s", got)
	}
}

func TestRemoteClientConnector_StartWatcher_ClientNotFound(t *testing.T) {
	reg := newTestRegistryForConnector(t)
	connector := NewRemoteClientConnector(RemoteClientConnectorConfig{
		Registry: reg,
	})

	err := connector.StartClientWatcher(context.Background(), "nonexistent-client")
	if err == nil {
		t.Fatal("expected error for nonexistent client")
	}
}

func TestRemoteClientConnector_StopWatcher_ClientOffline(t *testing.T) {
	reg := newTestRegistryForConnector(t)

	_, err := reg.Register("test-client", "localhost:7400")
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.MarkOffline("test-client"); err != nil {
		t.Fatal(err)
	}

	connector := NewRemoteClientConnector(RemoteClientConnectorConfig{
		Registry: reg,
	})

	err = connector.StopClientWatcher(context.Background(), "test-client")
	if err == nil {
		t.Fatal("expected error for offline client")
	}
	if got := err.Error(); got != `client "test-client" is offline` {
		t.Errorf("unexpected error message: %s", got)
	}
}

func TestRemoteClientConnector_GetClientStatus_ClientNotFound(t *testing.T) {
	reg := newTestRegistryForConnector(t)
	connector := NewRemoteClientConnector(RemoteClientConnectorConfig{
		Registry: reg,
	})

	_, err := connector.GetClientStatus(context.Background(), "nonexistent-client")
	if err == nil {
		t.Fatal("expected error for nonexistent client")
	}
}

func TestRemoteClientConnector_ImplementsInterface(t *testing.T) {
	// Compile-time check that RemoteClientConnector implements ClientConnector.
	var _ ClientConnector = (*RemoteClientConnector)(nil)
}
