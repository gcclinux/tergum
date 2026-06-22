package migrate

import (
	"context"
	"crypto/rand"
	"database/sql"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/gcclinux/tergum/internal/crypto"

	_ "modernc.org/sqlite"
)

// createV2TestDB creates a v2.0-style SQLite database with test data.
func createV2TestDB(t *testing.T, dir string) (string, []v2Entry) {
	t.Helper()
	dbPath := filepath.Join(dir, "tergum_v2.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_, err = db.Exec(`
		CREATE TABLE BACKUPS (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			md5_hash TEXT NOT NULL,
			file_name TEXT NOT NULL,
			file_path TEXT NOT NULL,
			file_ext TEXT,
			file_size INTEGER NOT NULL,
			backup_id TEXT NOT NULL,
			created_at TEXT
		);
		CREATE TABLE CONFIG (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		);
		CREATE TABLE BKPINDEX (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			backup_id TEXT NOT NULL,
			client_name TEXT
		);
		CREATE TABLE BKPSERVER (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			server_name TEXT,
			server_port INTEGER
		);
	`)
	if err != nil {
		t.Fatal(err)
	}

	entries := []v2Entry{
		{MD5Hash: "d41d8cd98f00b204e9800998ecf8427e", FileName: "empty.txt", FilePath: "/home/user/docs/empty.txt", FileExt: ".txt", FileSize: 0, BackupID: "bkp-001"},
		{MD5Hash: "098f6bcd4621d373cade4e832627b4f6", FileName: "test.log", FilePath: "/home/user/logs/test.log", FileExt: ".log", FileSize: 1024, BackupID: "bkp-001"},
		{MD5Hash: "5d41402abc4b2a76b9719d911017c592", FileName: "hello.go", FilePath: "/home/user/src/hello.go", FileExt: ".go", FileSize: 256, BackupID: "bkp-002"},
	}

	for _, e := range entries {
		_, err := db.Exec(
			`INSERT INTO BACKUPS (md5_hash, file_name, file_path, file_ext, file_size, backup_id) VALUES (?, ?, ?, ?, ?, ?)`,
			e.MD5Hash, e.FileName, e.FilePath, e.FileExt, e.FileSize, e.BackupID,
		)
		if err != nil {
			t.Fatal(err)
		}
	}

	return dbPath, entries
}

// createTestStorageFiles creates storage files in the two-level CAS layout.
func createTestStorageFiles(t *testing.T, storageDir string, hashes []string, content [][]byte) {
	t.Helper()
	for i, hash := range hashes {
		path := resolveStoragePath(storageDir, hash)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, content[i], 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMigrate_BasicSchemaTransform(t *testing.T) {
	dir := t.TempDir()
	storageDir := filepath.Join(dir, "storage")

	dbPath, entries := createV2TestDB(t, dir)

	// Create storage files (using MD5 hashes as filenames).
	hashes := make([]string, len(entries))
	contents := make([][]byte, len(entries))
	for i, e := range entries {
		hashes[i] = e.MD5Hash
		contents[i] = []byte("content-" + e.FileName)
	}
	createTestStorageFiles(t, storageDir, hashes, contents)

	migrator := NewMigrator()
	result, err := migrator.Migrate(context.Background(), MigrateOptions{
		FromDB:     dbPath,
		StorageDir: storageDir,
		Rehash:     false,
		Encrypt:    false,
		Verify:     false,
	})
	if err != nil {
		t.Fatalf("Migrate failed: %v", err)
	}

	if result.EntriesMigrated != int64(len(entries)) {
		t.Errorf("EntriesMigrated = %d, want %d", result.EntriesMigrated, len(entries))
	}
	if result.FilesRehashed != 0 {
		t.Errorf("FilesRehashed = %d, want 0", result.FilesRehashed)
	}
	if result.FilesEncrypted != 0 {
		t.Errorf("FilesEncrypted = %d, want 0", result.FilesEncrypted)
	}

	// Verify v3 database was created.
	v3Path := deriveV3Path(dbPath)
	if _, err := os.Stat(v3Path); os.IsNotExist(err) {
		t.Fatal("v3 database was not created")
	}

	// Open v3 DB and verify entries.
	v3db, err := sql.Open("sqlite", v3Path)
	if err != nil {
		t.Fatal(err)
	}
	defer v3db.Close()

	var count int
	if err := v3db.QueryRow("SELECT COUNT(*) FROM backups").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != len(entries) {
		t.Errorf("v3 backups count = %d, want %d", count, len(entries))
	}

	// Verify v2 database is unchanged.
	v2db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer v2db.Close()

	if err := v2db.QueryRow("SELECT COUNT(*) FROM BACKUPS").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != len(entries) {
		t.Errorf("v2 BACKUPS count changed: got %d, want %d", count, len(entries))
	}
}

func TestMigrate_Rehash(t *testing.T) {
	dir := t.TempDir()
	storageDir := filepath.Join(dir, "storage")

	dbPath, entries := createV2TestDB(t, dir)

	// Create storage files with known content.
	hashes := make([]string, len(entries))
	contents := make([][]byte, len(entries))
	for i, e := range entries {
		hashes[i] = e.MD5Hash
		contents[i] = []byte("file-content-" + e.FileName)
	}
	createTestStorageFiles(t, storageDir, hashes, contents)

	migrator := NewMigrator()
	result, err := migrator.Migrate(context.Background(), MigrateOptions{
		FromDB:     dbPath,
		StorageDir: storageDir,
		Rehash:     true,
		Encrypt:    false,
		Verify:     false,
	})
	if err != nil {
		t.Fatalf("Migrate with rehash failed: %v", err)
	}

	if result.FilesRehashed != int64(len(entries)) {
		t.Errorf("FilesRehashed = %d, want %d", result.FilesRehashed, len(entries))
	}

	// Verify files were renamed to BLAKE3 hashes.
	for i, content := range contents {
		expectedHash := crypto.HashBytes(content)
		newPath := resolveStoragePath(storageDir, expectedHash)
		if _, err := os.Stat(newPath); os.IsNotExist(err) {
			t.Errorf("rehashed file not found at %s", newPath)
		}

		// Old path should no longer exist.
		oldPath := resolveStoragePath(storageDir, entries[i].MD5Hash)
		if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
			t.Errorf("old file still exists at %s", oldPath)
		}
	}

	// Verify rollback script was generated.
	if result.RollbackScript == "" {
		t.Error("RollbackScript is empty")
	} else if _, err := os.Stat(result.RollbackScript); os.IsNotExist(err) {
		t.Errorf("rollback script not found at %s", result.RollbackScript)
	}
}

func TestMigrate_Encrypt(t *testing.T) {
	dir := t.TempDir()
	storageDir := filepath.Join(dir, "storage")

	dbPath, entries := createV2TestDB(t, dir)

	// Create storage files.
	hashes := make([]string, len(entries))
	contents := make([][]byte, len(entries))
	for i, e := range entries {
		hashes[i] = e.MD5Hash
		contents[i] = []byte("plaintext-" + e.FileName)
	}
	createTestStorageFiles(t, storageDir, hashes, contents)

	// Generate a master key.
	masterKey := make([]byte, 32)
	if _, err := rand.Read(masterKey); err != nil {
		t.Fatal(err)
	}

	migrator := NewMigrator()
	result, err := migrator.Migrate(context.Background(), MigrateOptions{
		FromDB:     dbPath,
		StorageDir: storageDir,
		Rehash:     false,
		Encrypt:    true,
		MasterKey:  masterKey,
		Verify:     false,
	})
	if err != nil {
		t.Fatalf("Migrate with encrypt failed: %v", err)
	}

	if result.FilesEncrypted != int64(len(entries)) {
		t.Errorf("FilesEncrypted = %d, want %d", result.FilesEncrypted, len(entries))
	}

	// Verify files are encrypted (content should differ from plaintext).
	encryptor := crypto.NewEncryptor()
	v3Path := deriveV3Path(dbPath)
	v3db, err := sql.Open("sqlite", v3Path)
	if err != nil {
		t.Fatal(err)
	}
	defer v3db.Close()

	rows, err := v3db.Query("SELECT blake3_hash, encrypted_dek, nonce FROM backups")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	idx := 0
	for rows.Next() {
		var hash string
		var encDEK, nonce []byte
		if err := rows.Scan(&hash, &encDEK, &nonce); err != nil {
			t.Fatal(err)
		}

		if encDEK == nil {
			t.Error("encrypted_dek is nil")
			continue
		}
		if nonce == nil {
			t.Error("nonce is nil")
			continue
		}

		// Read encrypted file.
		storagePath := resolveStoragePath(storageDir, hash)
		ciphertext, err := os.ReadFile(storagePath)
		if err != nil {
			t.Errorf("read encrypted file: %v", err)
			continue
		}

		// Decrypt and verify original content.
		plaintext, err := encryptor.Decrypt(ciphertext, encDEK, nonce, masterKey)
		if err != nil {
			t.Errorf("decrypt file: %v", err)
			continue
		}

		expected := contents[idx]
		if string(plaintext) != string(expected) {
			t.Errorf("decrypted content = %q, want %q", plaintext, expected)
		}
		idx++
	}
}

func TestMigrate_RehashAndEncrypt(t *testing.T) {
	dir := t.TempDir()
	storageDir := filepath.Join(dir, "storage")

	dbPath, entries := createV2TestDB(t, dir)

	// Create storage files.
	hashes := make([]string, len(entries))
	contents := make([][]byte, len(entries))
	for i, e := range entries {
		hashes[i] = e.MD5Hash
		contents[i] = []byte("combined-test-" + e.FileName)
	}
	createTestStorageFiles(t, storageDir, hashes, contents)

	masterKey := make([]byte, 32)
	if _, err := rand.Read(masterKey); err != nil {
		t.Fatal(err)
	}

	migrator := NewMigrator()
	result, err := migrator.Migrate(context.Background(), MigrateOptions{
		FromDB:     dbPath,
		StorageDir: storageDir,
		Rehash:     true,
		Encrypt:    true,
		MasterKey:  masterKey,
		Verify:     false,
	})
	if err != nil {
		t.Fatalf("Migrate with rehash+encrypt failed: %v", err)
	}

	if result.FilesRehashed != int64(len(entries)) {
		t.Errorf("FilesRehashed = %d, want %d", result.FilesRehashed, len(entries))
	}
	if result.FilesEncrypted != int64(len(entries)) {
		t.Errorf("FilesEncrypted = %d, want %d", result.FilesEncrypted, len(entries))
	}

	// Verify the files exist at BLAKE3 hash paths and are encrypted.
	encryptor := crypto.NewEncryptor()
	for i, content := range contents {
		blake3Hash := crypto.HashBytes(content)
		storagePath := resolveStoragePath(storageDir, blake3Hash)

		ciphertext, err := os.ReadFile(storagePath)
		if err != nil {
			t.Errorf("file[%d]: read encrypted file at BLAKE3 path: %v", i, err)
			continue
		}

		// The ciphertext should NOT equal the original plaintext.
		if string(ciphertext) == string(content) {
			t.Errorf("file[%d]: content was not encrypted", i)
		}

		// Old MD5 path should be gone.
		oldPath := resolveStoragePath(storageDir, entries[i].MD5Hash)
		if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
			t.Errorf("file[%d]: old MD5 path still exists", i)
		}

		// Decrypt using stored DEK from v3 database.
		v3Path := deriveV3Path(dbPath)
		v3db, err := sql.Open("sqlite", v3Path)
		if err != nil {
			t.Fatal(err)
		}
		var encDEK, nonce []byte
		err = v3db.QueryRow("SELECT encrypted_dek, nonce FROM backups WHERE blake3_hash = ?", blake3Hash).Scan(&encDEK, &nonce)
		v3db.Close()
		if err != nil {
			t.Errorf("file[%d]: query DEK: %v", i, err)
			continue
		}

		plaintext, err := encryptor.Decrypt(ciphertext, encDEK, nonce, masterKey)
		if err != nil {
			t.Errorf("file[%d]: decrypt: %v", i, err)
			continue
		}
		if string(plaintext) != string(content) {
			t.Errorf("file[%d]: decrypted = %q, want %q", i, plaintext, content)
		}
	}
}

func TestMigrate_Verify(t *testing.T) {
	dir := t.TempDir()
	storageDir := filepath.Join(dir, "storage")

	dbPath, entries := createV2TestDB(t, dir)

	// Create storage files with known content.
	hashes := make([]string, len(entries))
	contents := make([][]byte, len(entries))
	for i, e := range entries {
		hashes[i] = e.MD5Hash
		contents[i] = []byte("verify-test-" + e.FileName)
	}
	createTestStorageFiles(t, storageDir, hashes, contents)

	// Migrate with rehash so we have BLAKE3 hashes in v3 DB.
	migrator := NewMigrator()
	_, err := migrator.Migrate(context.Background(), MigrateOptions{
		FromDB:     dbPath,
		StorageDir: storageDir,
		Rehash:     true,
		Encrypt:    false,
		Verify:     false,
	})
	if err != nil {
		t.Fatalf("Migrate failed: %v", err)
	}

	// Store the storage_dir in the v3 config table for Verify to find.
	v3Path := deriveV3Path(dbPath)
	v3db, err := sql.Open("sqlite", v3Path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = v3db.Exec("INSERT INTO config (key, value) VALUES ('storage_dir', ?)", storageDir)
	v3db.Close()
	if err != nil {
		t.Fatal(err)
	}

	// Verify should find everything consistent.
	verifyResult, err := migrator.Verify(context.Background(), v3Path)
	if err != nil {
		t.Fatalf("Verify failed: %v", err)
	}

	if verifyResult.TotalEntries != int64(len(entries)) {
		t.Errorf("TotalEntries = %d, want %d", verifyResult.TotalEntries, len(entries))
	}
	if verifyResult.OrphanedDB != 0 {
		t.Errorf("OrphanedDB = %d, want 0", verifyResult.OrphanedDB)
	}
	if verifyResult.HashMismatches != 0 {
		t.Errorf("HashMismatches = %d, want 0", verifyResult.HashMismatches)
	}
	if verifyResult.Verified != int64(len(entries)) {
		t.Errorf("Verified = %d, want %d", verifyResult.Verified, len(entries))
	}
}

func TestMigrate_VerifyDetectsOrphans(t *testing.T) {
	dir := t.TempDir()
	storageDir := filepath.Join(dir, "storage")

	dbPath, entries := createV2TestDB(t, dir)

	// Create only 2 of 3 storage files (one will be orphaned in DB).
	hashes := make([]string, 2)
	contents := make([][]byte, 2)
	for i := 0; i < 2; i++ {
		hashes[i] = entries[i].MD5Hash
		contents[i] = []byte("orphan-test-" + entries[i].FileName)
	}
	createTestStorageFiles(t, storageDir, hashes, contents)

	// Also create an extra file on disk with no DB entry.
	extraHash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	extraPath := resolveStoragePath(storageDir, extraHash)
	if err := os.MkdirAll(filepath.Dir(extraPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(extraPath, []byte("orphan"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Migrate with rehash.
	migrator := NewMigrator()
	_, err := migrator.Migrate(context.Background(), MigrateOptions{
		FromDB:     dbPath,
		StorageDir: storageDir,
		Rehash:     true,
		Encrypt:    false,
		Verify:     false,
	})
	if err != nil {
		t.Fatalf("Migrate failed: %v", err)
	}

	// Set storage_dir in config.
	v3Path := deriveV3Path(dbPath)
	v3db, err := sql.Open("sqlite", v3Path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = v3db.Exec("INSERT INTO config (key, value) VALUES ('storage_dir', ?)", storageDir)
	v3db.Close()
	if err != nil {
		t.Fatal(err)
	}

	verifyResult, err := migrator.Verify(context.Background(), v3Path)
	if err != nil {
		t.Fatalf("Verify failed: %v", err)
	}

	// The third entry was never on disk, so it'll be OrphanedDB.
	if verifyResult.OrphanedDB == 0 {
		t.Error("expected OrphanedDB > 0 for missing file entry")
	}

	// The extra file has no DB entry, so it'll be OrphanedDisk.
	if verifyResult.OrphanedDisk == 0 {
		t.Error("expected OrphanedDisk > 0 for extra file on disk")
	}
}

func TestMigrate_NeverModifiesV2Database(t *testing.T) {
	dir := t.TempDir()
	storageDir := filepath.Join(dir, "storage")

	dbPath, entries := createV2TestDB(t, dir)

	// Create storage files.
	hashes := make([]string, len(entries))
	contents := make([][]byte, len(entries))
	for i, e := range entries {
		hashes[i] = e.MD5Hash
		contents[i] = []byte("preserve-test-" + e.FileName)
	}
	createTestStorageFiles(t, storageDir, hashes, contents)

	// Read v2 DB file bytes before migration.
	v2Before, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatal(err)
	}

	masterKey := make([]byte, 32)
	if _, err := rand.Read(masterKey); err != nil {
		t.Fatal(err)
	}

	migrator := NewMigrator()
	_, err = migrator.Migrate(context.Background(), MigrateOptions{
		FromDB:     dbPath,
		StorageDir: storageDir,
		Rehash:     true,
		Encrypt:    true,
		MasterKey:  masterKey,
		Verify:     false,
	})
	if err != nil {
		t.Fatalf("Migrate failed: %v", err)
	}

	// Read v2 DB file bytes after migration.
	v2After, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatal(err)
	}

	if len(v2Before) != len(v2After) {
		t.Errorf("v2 database size changed: before=%d, after=%d", len(v2Before), len(v2After))
	}
	for i := range v2Before {
		if v2Before[i] != v2After[i] {
			t.Fatal("v2 database was modified during migration")
		}
	}
}

func TestMigrate_RollbackScript(t *testing.T) {
	dir := t.TempDir()
	storageDir := filepath.Join(dir, "storage")

	dbPath, entries := createV2TestDB(t, dir)

	// Create storage files.
	hashes := make([]string, len(entries))
	contents := make([][]byte, len(entries))
	for i, e := range entries {
		hashes[i] = e.MD5Hash
		contents[i] = []byte("rollback-test-" + e.FileName)
	}
	createTestStorageFiles(t, storageDir, hashes, contents)

	migrator := NewMigrator()
	result, err := migrator.Migrate(context.Background(), MigrateOptions{
		FromDB:     dbPath,
		StorageDir: storageDir,
		Rehash:     true,
		Encrypt:    false,
		Verify:     false,
	})
	if err != nil {
		t.Fatalf("Migrate failed: %v", err)
	}

	if result.RollbackScript == "" {
		t.Fatal("no rollback script generated")
	}

	// Read the rollback script and verify it contains rename commands.
	scriptContent, err := os.ReadFile(result.RollbackScript)
	if err != nil {
		t.Fatalf("read rollback script: %v", err)
	}

	script := string(scriptContent)
	if runtime.GOOS == "windows" {
		if len(script) == 0 || !containsAny(script, "move") {
			t.Error("rollback script doesn't contain move commands")
		}
	} else {
		if len(script) == 0 || !containsAny(script, "mv") {
			t.Error("rollback script doesn't contain mv commands")
		}
	}

	// Verify it references BLAKE3 hashes (the from paths).
	for _, content := range contents {
		blake3Hash := crypto.HashBytes(content)
		if !containsAny(script, blake3Hash) {
			t.Errorf("rollback script doesn't reference BLAKE3 hash %s", blake3Hash)
		}
	}
}

func TestMigrate_ValidationErrors(t *testing.T) {
	migrator := NewMigrator()

	// Missing FromDB.
	_, err := migrator.Migrate(context.Background(), MigrateOptions{
		StorageDir: "/tmp/storage",
	})
	if err == nil {
		t.Error("expected error for missing FromDB")
	}

	// Missing StorageDir.
	_, err = migrator.Migrate(context.Background(), MigrateOptions{
		FromDB: "/tmp/db.sqlite",
	})
	if err == nil {
		t.Error("expected error for missing StorageDir")
	}

	// Encrypt without proper MasterKey.
	_, err = migrator.Migrate(context.Background(), MigrateOptions{
		FromDB:     "/tmp/db.sqlite",
		StorageDir: "/tmp/storage",
		Encrypt:    true,
		MasterKey:  []byte("short"),
	})
	if err == nil {
		t.Error("expected error for short MasterKey")
	}
}

func TestDeriveV3Path(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"/path/to/tergum.db", "/path/to/tergum_v3.db"},
		{"/path/to/backup.sqlite", "/path/to/backup_v3.sqlite"},
		{"database.db", "database_v3.db"},
	}

	for _, tt := range tests {
		// Normalize paths for the current OS.
		input := filepath.FromSlash(tt.input)
		expected := filepath.FromSlash(tt.expected)
		got := deriveV3Path(input)
		if got != expected {
			t.Errorf("deriveV3Path(%q) = %q, want %q", input, got, expected)
		}
	}
}

func TestResolveStoragePath(t *testing.T) {
	storageDir := filepath.FromSlash("/data/storage")

	tests := []struct {
		hash     string
		expected string
	}{
		{"abcdef1234567890", filepath.Join(storageDir, "ab", "abcdef1234567890")},
		{"x", filepath.Join(storageDir, "x")}, // short hash edge case
		{"", filepath.Join(storageDir, "")},
	}

	for _, tt := range tests {
		got := resolveStoragePath(storageDir, tt.hash)
		if got != tt.expected {
			t.Errorf("resolveStoragePath(%q, %q) = %q, want %q", storageDir, tt.hash, got, tt.expected)
		}
	}
}

func TestMigrate_ContextCancellation(t *testing.T) {
	dir := t.TempDir()
	storageDir := filepath.Join(dir, "storage")

	dbPath, entries := createV2TestDB(t, dir)

	// Create storage files.
	hashes := make([]string, len(entries))
	contents := make([][]byte, len(entries))
	for i, e := range entries {
		hashes[i] = e.MD5Hash
		contents[i] = []byte("cancel-test-" + e.FileName)
	}
	createTestStorageFiles(t, storageDir, hashes, contents)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	migrator := NewMigrator()
	_, err := migrator.Migrate(ctx, MigrateOptions{
		FromDB:     dbPath,
		StorageDir: storageDir,
		Rehash:     true,
	})
	if err == nil {
		t.Error("expected error for cancelled context")
	}
}

// containsAny checks if the string contains the substring.
func containsAny(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && contains(s, substr)
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
