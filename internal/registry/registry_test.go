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

func TestRegister_WithIdentity(t *testing.T) {
	reg := newTestRegistry(t)

	ci, err := reg.RegisterWithIdentity("laptop1", "192.168.1.50:7400", "linux", "mach-uuid-1", "my-laptop")
	if err != nil {
		t.Fatalf("RegisterWithIdentity failed: %v", err)
	}

	if ci.OSFamily != "linux" {
		t.Errorf("got OSFamily %q, want %q", ci.OSFamily, "linux")
	}
	if ci.MachineID != "mach-uuid-1" {
		t.Errorf("got MachineID %q, want %q", ci.MachineID, "mach-uuid-1")
	}
	if ci.Hostname != "my-laptop" {
		t.Errorf("got Hostname %q, want %q", ci.Hostname, "my-laptop")
	}

	// Reconnecting from same machine should succeed
	ci2, err := reg.RegisterWithIdentity("laptop1", "192.168.1.55:7400", "linux", "mach-uuid-1", "my-laptop")
	if err != nil {
		t.Fatalf("RegisterWithIdentity same machine failed: %v", err)
	}
	if ci2.Address != "192.168.1.55:7400" {
		t.Errorf("got address %q, want %q", ci2.Address, "192.168.1.55:7400")
	}
}

func TestRegister_MachineConflict(t *testing.T) {
	reg := newTestRegistry(t)

	_, err := reg.RegisterWithIdentity("laptop1", "192.168.1.50:7400", "linux", "mach-uuid-1", "my-laptop")
	if err != nil {
		t.Fatalf("initial register failed: %v", err)
	}

	// A different physical machine trying to register while online should be rejected
	_, err = reg.RegisterWithIdentity("laptop1", "192.168.1.99:7400", "linux", "mach-uuid-2", "other-laptop")
	if err == nil {
		t.Fatal("expected conflict error when registering different machine while online, got nil")
	}
}

func TestRebindClient(t *testing.T) {
	reg := newTestRegistry(t)

	_, err := reg.RegisterWithIdentity("laptop1", "192.168.1.50:7400", "linux", "mach-uuid-1", "old-ubuntu")
	if err != nil {
		t.Fatalf("initial register failed: %v", err)
	}

	// Rebind with force=false while online should fail
	_, err = reg.RebindClient("laptop1", "192.168.1.60:7400", "linux", "mach-uuid-rebuilt", "new-fedora", false)
	if err == nil {
		t.Fatal("expected error without force while online, got nil")
	}

	// Incompatible OS (e.g. windows to linux) should fail even with force
	_, err = reg.RebindClient("laptop1", "192.168.1.60:7400", "windows", "mach-uuid-rebuilt", "new-windows", true)
	if err == nil {
		t.Fatal("expected error on incompatible OS family rebind, got nil")
	}

	// Compatible OS family (linux -> linux, e.g. Ubuntu -> Fedora) with force should succeed
	rebound, err := reg.RebindClient("laptop1", "192.168.1.60:7400", "linux", "mach-uuid-rebuilt", "new-fedora", true)
	if err != nil {
		t.Fatalf("RebindClient failed: %v", err)
	}

	if rebound.Hostname != "new-fedora" {
		t.Errorf("got Hostname %q, want %q", rebound.Hostname, "new-fedora")
	}
	if rebound.MachineID != "mach-uuid-rebuilt" {
		t.Errorf("got MachineID %q, want %q", rebound.MachineID, "mach-uuid-rebuilt")
	}
	if rebound.Address != "192.168.1.60:7400" {
		t.Errorf("got Address %q, want %q", rebound.Address, "192.168.1.60:7400")
	}
}


func TestMigration_AddsSPKIColumnOnLegacyDB(t *testing.T) {
	db := openTestDB(t)

	// Create a legacy client_registry table WITHOUT the spki_fingerprint column.
	_, err := db.Exec(`CREATE TABLE client_registry (
		client_id        TEXT PRIMARY KEY,
		address          TEXT NOT NULL,
		status           TEXT NOT NULL DEFAULT 'offline',
		last_seen        TEXT,
		last_backup      TEXT,
		watcher_active   INTEGER DEFAULT 0,
		full_backup_cron TEXT DEFAULT '',
		auto_backup_cron TEXT DEFAULT '',
		registered_at    TEXT NOT NULL DEFAULT (datetime('now'))
	)`)
	if err != nil {
		t.Fatalf("create legacy table: %v", err)
	}

	reg, err := New(Config{DB: db})
	if err != nil {
		t.Fatalf("new registry on legacy db: %v", err)
	}
	_ = reg

	// The spki_fingerprint column must now exist.
	var hasColumn bool
	rows, err := db.Query(`PRAGMA table_info(client_registry)`)
	if err != nil {
		t.Fatalf("pragma table_info: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt interface{}
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			t.Fatalf("scan column: %v", err)
		}
		if name == "spki_fingerprint" {
			hasColumn = true
		}
	}
	if !hasColumn {
		t.Error("expected spki_fingerprint column to exist after migration")
	}
}

func TestSetAndFindSPKIFingerprint(t *testing.T) {
	reg := newTestRegistry(t)

	if _, err := reg.Register("node1", "10.0.0.1:7400"); err != nil {
		t.Fatalf("register: %v", err)
	}

	fp := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if err := reg.SetSPKIFingerprint("node1", fp); err != nil {
		t.Fatalf("set spki: %v", err)
	}

	found := reg.FindClientBySPKI(fp)
	if found == nil {
		t.Fatal("expected to find client by SPKI")
	}
	if found.ClientID != "node1" {
		t.Errorf("got client %q, want node1", found.ClientID)
	}
	if found.SPKIFingerprint != fp {
		t.Errorf("got fingerprint %q, want %q", found.SPKIFingerprint, fp)
	}

	// Mutating the returned copy must not affect the registry.
	found.SPKIFingerprint = "mutated"
	if again := reg.FindClientBySPKI(fp); again == nil {
		t.Error("copy-on-read violated: mutation leaked into registry")
	}

	if reg.FindClientBySPKI("") != nil {
		t.Error("empty fingerprint must not match any client")
	}
	if reg.FindClientBySPKI("nomatch") != nil {
		t.Error("unknown fingerprint must not match any client")
	}
}

func TestSetSPKIFingerprint_UnknownClientIsNoOp(t *testing.T) {
	reg := newTestRegistry(t)
	if err := reg.SetSPKIFingerprint("ghost", "deadbeef"); err != nil {
		t.Errorf("expected no error for unknown client, got: %v", err)
	}
}

func TestSetSPKIFingerprint_EmptyDoesNotErase(t *testing.T) {
	reg := newTestRegistry(t)

	if _, err := reg.Register("node1", "10.0.0.1:7400"); err != nil {
		t.Fatalf("register: %v", err)
	}
	fp := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if err := reg.SetSPKIFingerprint("node1", fp); err != nil {
		t.Fatalf("set spki: %v", err)
	}

	// Persist an empty fingerprint via another registry operation. The upsert
	// guard must preserve the previously stored value in the database.
	if err := reg.SetSPKIFingerprint("node1", ""); err != nil {
		t.Fatalf("set empty spki: %v", err)
	}

	// In-memory is now empty, but the DB row must still hold the old value.
	var stored string
	err := reg.db.QueryRow(`SELECT spki_fingerprint FROM client_registry WHERE client_id = ?`, "node1").Scan(&stored)
	if err != nil {
		t.Fatalf("query stored fingerprint: %v", err)
	}
	if stored != fp {
		t.Errorf("expected stored fingerprint preserved as %q, got %q", fp, stored)
	}
}
