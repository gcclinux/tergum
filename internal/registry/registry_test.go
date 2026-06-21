package registry

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	// Use a shared in-memory database so that multiple connections within
	// the sql.DB pool see the same tables.
	db, err := sql.Open("sqlite", "file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	// Force a single connection to avoid per-connection database isolation.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		t.Fatalf("wal: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func newTestRegistry(t *testing.T) *Registry {
	t.Helper()
	db := openTestDB(t)
	reg, err := New(Config{
		DB:               db,
		OfflineThreshold: 90 * time.Second,
		CheckInterval:    30 * time.Second,
	})
	if err != nil {
		t.Fatalf("new registry: %v", err)
	}
	return reg
}

func TestRegister_NewClient(t *testing.T) {
	reg := newTestRegistry(t)

	ci, err := reg.Register("node1", "192.168.1.10:7400")
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	if ci.ClientID != "node1" {
		t.Errorf("got clientID %q, want %q", ci.ClientID, "node1")
	}
	if ci.Address != "192.168.1.10:7400" {
		t.Errorf("got address %q, want %q", ci.Address, "192.168.1.10:7400")
	}
	if ci.Status != "online" {
		t.Errorf("got status %q, want %q", ci.Status, "online")
	}
}

func TestRegister_ExistingClient(t *testing.T) {
	reg := newTestRegistry(t)

	_, err := reg.Register("node1", "10.0.0.1:7400")
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	// Re-register with a new address.
	ci, err := reg.Register("node1", "10.0.0.2:7400")
	if err != nil {
		t.Fatalf("re-register: %v", err)
	}

	if ci.Address != "10.0.0.2:7400" {
		t.Errorf("got address %q, want %q", ci.Address, "10.0.0.2:7400")
	}
	if ci.Status != "online" {
		t.Errorf("got status %q, want %q", ci.Status, "online")
	}
}

func TestHeartbeat(t *testing.T) {
	reg := newTestRegistry(t)

	_, err := reg.Register("node1", "10.0.0.1:7400")
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	time.Sleep(10 * time.Millisecond)

	err = reg.Heartbeat("node1")
	if err != nil {
		t.Fatalf("heartbeat: %v", err)
	}

	ci := reg.GetClient("node1")
	if ci == nil {
		t.Fatal("client not found")
	}
	if ci.Status != "online" {
		t.Errorf("got status %q, want %q", ci.Status, "online")
	}
}

func TestHeartbeat_UnknownClient(t *testing.T) {
	reg := newTestRegistry(t)

	err := reg.Heartbeat("unknown")
	if err == nil {
		t.Fatal("expected error for unknown client")
	}
}

func TestMarkOffline(t *testing.T) {
	reg := newTestRegistry(t)

	_, err := reg.Register("node1", "10.0.0.1:7400")
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	err = reg.MarkOffline("node1")
	if err != nil {
		t.Fatalf("mark offline: %v", err)
	}

	ci := reg.GetClient("node1")
	if ci.Status != "offline" {
		t.Errorf("got status %q, want %q", ci.Status, "offline")
	}
}

func TestListClients(t *testing.T) {
	reg := newTestRegistry(t)

	reg.Register("node1", "10.0.0.1:7400")
	reg.Register("node2", "10.0.0.2:7400")

	clients := reg.ListClients()
	if len(clients) != 2 {
		t.Fatalf("got %d clients, want 2", len(clients))
	}
}

func TestGetClient_NotFound(t *testing.T) {
	reg := newTestRegistry(t)

	ci := reg.GetClient("nonexistent")
	if ci != nil {
		t.Errorf("expected nil, got %+v", ci)
	}
}

func TestSetSchedule(t *testing.T) {
	reg := newTestRegistry(t)

	_, err := reg.Register("node1", "10.0.0.1:7400")
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	err = reg.SetSchedule("node1", ScheduleConfig{
		FullBackupCron: "0 2 * * *",
		AutoBackupCron: "*/15 * * * *",
	})
	if err != nil {
		t.Fatalf("set schedule: %v", err)
	}

	ci := reg.GetClient("node1")
	if ci.Schedule == nil {
		t.Fatal("schedule is nil")
	}
	if ci.Schedule.FullBackupCron != "0 2 * * *" {
		t.Errorf("got full cron %q, want %q", ci.Schedule.FullBackupCron, "0 2 * * *")
	}
	if ci.Schedule.AutoBackupCron != "*/15 * * * *" {
		t.Errorf("got auto cron %q, want %q", ci.Schedule.AutoBackupCron, "*/15 * * * *")
	}
}

func TestSetSchedule_UnknownClient(t *testing.T) {
	reg := newTestRegistry(t)

	err := reg.SetSchedule("unknown", ScheduleConfig{})
	if err == nil {
		t.Fatal("expected error for unknown client")
	}
}

func TestPersistence(t *testing.T) {
	db := openTestDB(t)

	// Create registry and register a client.
	reg1, err := New(Config{DB: db})
	if err != nil {
		t.Fatalf("new registry: %v", err)
	}

	_, err = reg1.Register("node1", "10.0.0.1:7400")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	err = reg1.SetSchedule("node1", ScheduleConfig{
		FullBackupCron: "0 3 * * *",
	})
	if err != nil {
		t.Fatalf("set schedule: %v", err)
	}

	// Create a new registry from the same DB — it should load existing data.
	reg2, err := New(Config{DB: db})
	if err != nil {
		t.Fatalf("new registry 2: %v", err)
	}

	ci := reg2.GetClient("node1")
	if ci == nil {
		t.Fatal("client not found after reload")
	}
	if ci.Address != "10.0.0.1:7400" {
		t.Errorf("got address %q, want %q", ci.Address, "10.0.0.1:7400")
	}
	if ci.Schedule == nil || ci.Schedule.FullBackupCron != "0 3 * * *" {
		t.Errorf("schedule not persisted correctly")
	}
}

func TestBackgroundOfflineCheck(t *testing.T) {
	db := openTestDB(t)

	// Use a very short offline threshold for testing.
	reg, err := New(Config{
		DB:               db,
		OfflineThreshold: 50 * time.Millisecond,
		CheckInterval:    20 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("new registry: %v", err)
	}

	_, err = reg.Register("node1", "10.0.0.1:7400")
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	// Start the background checker.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go reg.Start(ctx)

	// Wait for the offline threshold to be exceeded plus one check interval.
	time.Sleep(100 * time.Millisecond)

	ci := reg.GetClient("node1")
	if ci == nil {
		t.Fatal("client not found")
	}
	if ci.Status != "offline" {
		t.Errorf("got status %q, want %q after timeout", ci.Status, "offline")
	}
}

func TestMissedBackups(t *testing.T) {
	reg := newTestRegistry(t)

	_, err := reg.Register("node1", "10.0.0.1:7400")
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	scheduledAt := time.Now()
	err = reg.RecordMissedBackup("node1", "FULL", scheduledAt)
	if err != nil {
		t.Fatalf("record missed: %v", err)
	}

	ci := reg.GetClient("node1")
	if len(ci.MissedBackups) != 1 {
		t.Fatalf("got %d missed backups, want 1", len(ci.MissedBackups))
	}
	if ci.MissedBackups[0].Level != "FULL" {
		t.Errorf("got level %q, want %q", ci.MissedBackups[0].Level, "FULL")
	}

	// Resolve missed backups.
	resolved, err := reg.ResolveMissedBackups("node1")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(resolved) != 1 {
		t.Fatalf("got %d resolved, want 1", len(resolved))
	}

	// After resolving, there should be no unresolved missed backups.
	ci = reg.GetClient("node1")
	if len(ci.MissedBackups) != 0 {
		t.Errorf("got %d missed backups after resolve, want 0", len(ci.MissedBackups))
	}
}

func TestHeartbeat_ReconnectsOfflineClient(t *testing.T) {
	reg := newTestRegistry(t)

	_, err := reg.Register("node1", "10.0.0.1:7400")
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	// Mark offline.
	err = reg.MarkOffline("node1")
	if err != nil {
		t.Fatalf("mark offline: %v", err)
	}

	// Heartbeat should bring it back online.
	err = reg.Heartbeat("node1")
	if err != nil {
		t.Fatalf("heartbeat: %v", err)
	}

	ci := reg.GetClient("node1")
	if ci.Status != "online" {
		t.Errorf("got status %q, want %q after heartbeat", ci.Status, "online")
	}
}
