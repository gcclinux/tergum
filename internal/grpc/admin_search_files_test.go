package grpc

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/gcclinux/tergum/internal/config"
	"github.com/gcclinux/tergum/internal/grpc/proto"
	"google.golang.org/grpc/codes"
	_ "modernc.org/sqlite"
)

// This file tests the AdminSearchFiles RPC method which allows admin clients
// to search and restore files from another client's backup (the bug condition
// where --client is specified without --target).

// --- test helpers ---

// createTestClientDB creates a minimal client database with backup entries for testing.
func createTestClientDB(t *testing.T, dbPath string) {
	t.Helper()

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	defer db.Close()

	// Create the minimal schema needed for testing.
	schema := `
		CREATE TABLE IF NOT EXISTS backups (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			backup_id TEXT NOT NULL,
			blake3_hash TEXT NOT NULL,
			file_name TEXT NOT NULL,
			file_path TEXT NOT NULL,
			file_ext TEXT,
			file_size INTEGER DEFAULT 0,
			created_at DATETIME,
			modified_at DATETIME,
			accessed_at DATETIME,
			permissions INTEGER,
			owner TEXT,
			file_group TEXT,
			hidden INTEGER DEFAULT 0,
			symlink INTEGER DEFAULT 0,
			symlink_target TEXT,
			os TEXT,
			encrypted_dek BLOB,
			nonce BLOB,
			backup_date DATETIME DEFAULT CURRENT_TIMESTAMP,
			expires_at DATETIME
		);
		CREATE TABLE IF NOT EXISTS backup_jobs (
			backup_id TEXT PRIMARY KEY,
			level TEXT,
			client_id TEXT,
			client_ip TEXT,
			initiated_by TEXT,
			started_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			finished_at DATETIME,
			status TEXT DEFAULT 'running',
			file_count INTEGER DEFAULT 0,
			bytes_total INTEGER DEFAULT 0,
			bytes_new INTEGER DEFAULT 0,
			files_deduped INTEGER DEFAULT 0,
			error_message TEXT
		);`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}

	// Insert test data.
	now := time.Now()
	testData := []struct {
		backupID  string
		hash      string
		fileName  string
		filePath  string
		fileSize  int64
		modTime   time.Time
	}{
		{"backup-001", "hash-a", "main.go", "/src/main.go", 1024, now.Add(-time.Hour)},
		{"backup-001", "hash-b", "utils.go", "/src/utils.go", 512, now.Add(-2 * time.Hour)},
		{"backup-001", "hash-c", "config.toml", "/etc/config.toml", 256, now.Add(-3 * time.Hour)},
		{"backup-002", "hash-d", "main.go", "/src/main.go", 1100, now},
		{"backup-002", "hash-e", "readme.md", "/docs/readme.md", 2048, now},
	}

	for _, td := range testData {
		_, err := db.Exec(
			`INSERT INTO backups (backup_id, blake3_hash, file_name, file_path, file_size, modified_at)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			td.backupID, td.hash, td.fileName, td.filePath, td.fileSize, td.modTime,
		)
		if err != nil {
			t.Fatalf("insert test data: %v", err)
		}
	}

	// Insert completed backup jobs.
	_, err = db.Exec(
		`INSERT INTO backup_jobs (backup_id, status, finished_at) VALUES (?, ?, ?)`,
		"backup-001", "completed", now.Add(-time.Hour),
	)
	if err != nil {
		t.Fatalf("insert job-001: %v", err)
	}
	_, err = db.Exec(
		`INSERT INTO backup_jobs (backup_id, status, finished_at) VALUES (?, ?, ?)`,
		"backup-002", "completed", now,
	)
	if err != nil {
		t.Fatalf("insert job-002: %v", err)
	}
}

// --- (a) admin can search another client's backups ---

func TestAdminSearchFiles_AdminAllowed(t *testing.T) {
	reg := newAuthzRegistry(t)

	// Create admin client with SPKI fingerprint.
	adminCert, adminFP := makeNamedCert(t, "admin-ops")
	if _, err := reg.Register("admin-ops", "tunnel://admin-ops"); err != nil {
		t.Fatalf("register admin: %v", err)
	}
	if err := reg.SetSPKIFingerprint("admin-ops", adminFP); err != nil {
		t.Fatalf("set admin spki: %v", err)
	}

	// Register the source client.
	if _, err := reg.Register("client-a", "tunnel://client-a"); err != nil {
		t.Fatalf("register A: %v", err)
	}

	// Create a temp clients dir with a test client database.
	clientsDir := t.TempDir()
	clientDBPath := filepath.Join(clientsDir, "client-a.db")
	createTestClientDB(t, clientDBPath)

	// Build admin policy.
	cfgPath := writeAdminConfig(t, []config.AdminClient{{Name: "admin-ops", Fingerprint: adminFP}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	policy := NewConfigAdminPolicy(ctx, cfgPath, time.Hour)

	srv := NewCommandServer(CommandServerConfig{
		BackupEngine: &mockBackupEngine{},
		Repo:         &mockRepo{},
		Registry:     reg,
		AdminPolicy:  policy,
		ClientsDir:   clientsDir,
		Version:      "test",
	})

	adminContext := peerCtx(adminCert)

	// Test: Search by query pattern.
	resp, err := srv.AdminSearchFiles(adminContext, &proto.AdminSearchFilesRequest{
		SourceClientId: "client-a",
		Query:          "*.go",
		ListOnly:       true,
	})
	if err != nil {
		t.Fatalf("admin AdminSearchFiles query=*.go error: %v", err)
	}
	if !resp.Success {
		t.Errorf("expected success, got %+v", resp)
	}
	// Should find main.go and utils.go files (in both backups).
	if resp.FilesFound < 2 {
		t.Errorf("expected at least 2 files matching *.go, got %d", resp.FilesFound)
	}

	// Test: Search by backup ID.
	resp2, err := srv.AdminSearchFiles(adminContext, &proto.AdminSearchFilesRequest{
		SourceClientId: "client-a",
		BackupId:       "backup-001",
		ListOnly:       true,
	})
	if err != nil {
		t.Fatalf("admin AdminSearchFiles backup_id=backup-001 error: %v", err)
	}
	if resp2.FilesFound != 3 {
		t.Errorf("expected 3 files in backup-001, got %d", resp2.FilesFound)
	}

	// Test: Search by both backup ID and query.
	resp3, err := srv.AdminSearchFiles(adminContext, &proto.AdminSearchFilesRequest{
		SourceClientId: "client-a",
		BackupId:       "backup-001",
		Query:          "main.go",
		ListOnly:       true,
	})
	if err != nil {
		t.Fatalf("admin AdminSearchFiles backup_id+query error: %v", err)
	}
	if resp3.FilesFound != 1 {
		t.Errorf("expected 1 file matching main.go in backup-001, got %d", resp3.FilesFound)
	}
	if len(resp3.Files) != 1 || resp3.Files[0].FileName != "main.go" {
		t.Errorf("expected main.go result, got %+v", resp3.Files)
	}
}

// --- (b) non-admin is denied ---

func TestAdminSearchFiles_NonAdminDenied(t *testing.T) {
	reg := newAuthzRegistry(t)

	// A non-admin client.
	userCert, userFP := makeNamedCert(t, "client-a")
	if _, err := reg.Register("client-a", "tunnel://client-a"); err != nil {
		t.Fatalf("register A: %v", err)
	}
	if err := reg.SetSPKIFingerprint("client-a", userFP); err != nil {
		t.Fatalf("set A spki: %v", err)
	}

	// Admin exists but caller is NOT the admin.
	_, adminFP := makeNamedCert(t, "admin-ops")
	cfgPath := writeAdminConfig(t, []config.AdminClient{{Name: "admin-ops", Fingerprint: adminFP}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	policy := NewConfigAdminPolicy(ctx, cfgPath, time.Hour)

	// Create a temp clients dir with a test client database.
	clientsDir := t.TempDir()
	clientDBPath := filepath.Join(clientsDir, "client-a.db")
	createTestClientDB(t, clientDBPath)

	srv := NewCommandServer(CommandServerConfig{
		BackupEngine: &mockBackupEngine{},
		Repo:         &mockRepo{},
		Registry:     reg,
		AdminPolicy:  policy,
		ClientsDir:   clientsDir,
		Version:      "test",
	})

	userContext := peerCtx(userCert)

	// Non-admin trying to search another client's backup should be denied.
	_, err := srv.AdminSearchFiles(userContext, &proto.AdminSearchFilesRequest{
		SourceClientId: "client-b",
		Query:          "*.go",
		ListOnly:       true,
	})
	if codeOf(err) != codes.PermissionDenied {
		t.Errorf("non-admin AdminSearchFiles code = %v, want PermissionDenied", codeOf(err))
	}
}

// --- (c) validation errors ---

func TestAdminSearchFiles_ValidationErrors(t *testing.T) {
	reg := newAuthzRegistry(t)

	adminCert, adminFP := makeNamedCert(t, "admin-ops")
	if _, err := reg.Register("admin-ops", "tunnel://admin-ops"); err != nil {
		t.Fatalf("register admin: %v", err)
	}
	if err := reg.SetSPKIFingerprint("admin-ops", adminFP); err != nil {
		t.Fatalf("set admin spki: %v", err)
	}

	cfgPath := writeAdminConfig(t, []config.AdminClient{{Name: "admin-ops", Fingerprint: adminFP}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	policy := NewConfigAdminPolicy(ctx, cfgPath, time.Hour)

	clientsDir := t.TempDir()

	srv := NewCommandServer(CommandServerConfig{
		BackupEngine: &mockBackupEngine{},
		Repo:         &mockRepo{},
		Registry:     reg,
		AdminPolicy:  policy,
		ClientsDir:   clientsDir,
		Version:      "test",
	})

	adminContext := peerCtx(adminCert)

	// Missing source_client_id.
	_, err := srv.AdminSearchFiles(adminContext, &proto.AdminSearchFilesRequest{
		Query:    "*.go",
		ListOnly: true,
	})
	if err == nil || codeOf(err) != codes.InvalidArgument {
		t.Errorf("missing source_client_id: expected InvalidArgument, got %v", err)
	}

	// Missing query and backup_id.
	_, err = srv.AdminSearchFiles(adminContext, &proto.AdminSearchFilesRequest{
		SourceClientId: "client-a",
		ListOnly:       true,
	})
	if err == nil || codeOf(err) != codes.InvalidArgument {
		t.Errorf("missing query/backup_id: expected InvalidArgument, got %v", err)
	}

	// Client database not found.
	_, err = srv.AdminSearchFiles(adminContext, &proto.AdminSearchFilesRequest{
		SourceClientId: "nonexistent",
		Query:          "*.go",
		ListOnly:       true,
	})
	if err == nil || codeOf(err) != codes.InvalidArgument {
		t.Errorf("nonexistent client: expected InvalidArgument, got %v", err)
	}
}

// --- (d) clients dir not configured ---

func TestAdminSearchFiles_NoClientsDirConfigured(t *testing.T) {
	reg := newAuthzRegistry(t)

	adminCert, adminFP := makeNamedCert(t, "admin-ops")
	if _, err := reg.Register("admin-ops", "tunnel://admin-ops"); err != nil {
		t.Fatalf("register admin: %v", err)
	}
	if err := reg.SetSPKIFingerprint("admin-ops", adminFP); err != nil {
		t.Fatalf("set admin spki: %v", err)
	}

	cfgPath := writeAdminConfig(t, []config.AdminClient{{Name: "admin-ops", Fingerprint: adminFP}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	policy := NewConfigAdminPolicy(ctx, cfgPath, time.Hour)

	// Server with no clientsDir configured.
	srv := NewCommandServer(CommandServerConfig{
		BackupEngine: &mockBackupEngine{},
		Repo:         &mockRepo{},
		Registry:     reg,
		AdminPolicy:  policy,
		// ClientsDir not set!
		Version: "test",
	})

	adminContext := peerCtx(adminCert)

	_, err := srv.AdminSearchFiles(adminContext, &proto.AdminSearchFilesRequest{
		SourceClientId: "client-a",
		Query:          "*.go",
		ListOnly:       true,
	})
	if err == nil || codeOf(err) != codes.InvalidArgument {
		t.Errorf("no clients dir: expected InvalidArgument, got %v", err)
	}
}

// --- (e) glob pattern conversion ---

func TestGlobToSQLLike(t *testing.T) {
	tests := []struct {
		glob     string
		expected string
	}{
		{"*.go", "%.go"},
		{"main.?", "main._"},
		{"test", "%test%"},               // No wildcards, wrap in % for substring match
		{"**/file*", "%%/file%"},         // ** becomes %%, which is still valid LIKE pattern
		{"file_name", "file\\_name"},     // Underscore is escaped, but has wildcards, no wrapping
		{"%pattern%", "\\%pattern\\%"},   // % is escaped, but has wildcards from %, no wrapping
	}

	for _, tt := range tests {
		t.Run(tt.glob, func(t *testing.T) {
			result := globToSQLLike(tt.glob)
			if result != tt.expected {
				t.Errorf("globToSQLLike(%q) = %q, want %q", tt.glob, result, tt.expected)
			}
		})
	}
}

// writeAdminConfig is defined in admin_policy_test.go and is reused here.
