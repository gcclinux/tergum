package migrate

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/gcclinux/tergum/internal/crypto"
	"pgregory.net/rapid"

	_ "modernc.org/sqlite"
)

// genV2Entry generates a random v2Entry with plausible data.
func genV2Entry(t *rapid.T) v2Entry {
	// Generate a 32-character hex string to mimic MD5 hash.
	md5Bytes := rapid.SliceOfN(rapid.Byte(), 16, 16).Draw(t, "md5bytes")
	md5Hash := fmt.Sprintf("%x", md5Bytes)

	fileName := rapid.StringMatching(`[a-z]{3,8}\.[a-z]{2,4}`).Draw(t, "fileName")
	filePath := "/" + rapid.StringMatching(`[a-z]{2,6}/[a-z]{2,6}/`).Draw(t, "dirPath") + fileName
	fileExt := filepath.Ext(fileName)
	fileSize := rapid.Int64Range(0, 4096).Draw(t, "fileSize")
	backupID := fmt.Sprintf("bkp-%03d", rapid.IntRange(1, 99).Draw(t, "backupID"))

	return v2Entry{
		MD5Hash:  md5Hash,
		FileName: fileName,
		FilePath: filePath,
		FileExt:  fileExt,
		FileSize: fileSize,
		BackupID: backupID,
	}
}

// setupV2DB creates a v2.0 SQLite database with the given entries and returns its path.
func setupV2DB(t *testing.T, dir string, entries []v2Entry) string {
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

	for _, e := range entries {
		_, err := db.Exec(
			`INSERT INTO BACKUPS (md5_hash, file_name, file_path, file_ext, file_size, backup_id) VALUES (?, ?, ?, ?, ?, ?)`,
			e.MD5Hash, e.FileName, e.FilePath, e.FileExt, e.FileSize, e.BackupID,
		)
		if err != nil {
			t.Fatal(err)
		}
	}

	return dbPath
}

// setupStorageFiles creates storage files for the given entries with random content.
// Returns a map of MD5 hash → file content.
func setupStorageFiles(t *testing.T, storageDir string, entries []v2Entry, contents map[string][]byte) {
	t.Helper()
	for _, e := range entries {
		content, ok := contents[e.MD5Hash]
		if !ok {
			continue
		}
		path := resolveStoragePath(storageDir, e.MD5Hash)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// TestProperty_MigrationDataPreservation verifies Property 14:
// - All v2.0 entries are preserved in v3.0 with correct schema transformation
// - With --rehash, files are renamed to BLAKE3 hash and the stored hash matches content
// - The original v2.0 database is byte-for-byte unchanged
// - A rollback script is generated
//
// **Validates: Requirements 19.1, 19.2, 19.4**
func TestProperty_MigrationDataPreservation(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate 2-10 random entries with unique MD5 hashes.
		numEntries := rapid.IntRange(2, 10).Draw(rt, "numEntries")
		entries := make([]v2Entry, 0, numEntries)
		seenHashes := make(map[string]bool)

		for i := 0; i < numEntries; i++ {
			e := genV2Entry(rt)
			// Ensure unique MD5 hashes across entries.
			for seenHashes[e.MD5Hash] {
				e = genV2Entry(rt)
			}
			seenHashes[e.MD5Hash] = true
			entries = append(entries, e)
		}

		// Create test directories.
		dir := t.TempDir()
		storageDir := filepath.Join(dir, "storage")

		// Generate random content for each entry's storage file.
		contents := make(map[string][]byte, numEntries)
		for _, e := range entries {
			// Small content (8-64 bytes) for speed.
			contentSize := rapid.IntRange(8, 64).Draw(rt, "contentSize")
			content := rapid.SliceOfN(rapid.Byte(), contentSize, contentSize).Draw(rt, "content")
			contents[e.MD5Hash] = content
		}

		// Set up v2.0 database and storage files.
		dbPath := setupV2DB(t, dir, entries)
		setupStorageFiles(t, storageDir, entries, contents)

		// Record the v2.0 database content before migration.
		v2Before, err := os.ReadFile(dbPath)
		if err != nil {
			t.Fatal(err)
		}

		// Run migration with rehash=true.
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

		// --- Verify: all entries are in the v3.0 database ---
		if result.EntriesMigrated != int64(numEntries) {
			t.Fatalf("EntriesMigrated = %d, want %d", result.EntriesMigrated, numEntries)
		}

		v3Path := deriveV3Path(dbPath)
		v3db, err := sql.Open("sqlite", v3Path)
		if err != nil {
			t.Fatal(err)
		}
		defer v3db.Close()

		var count int
		if err := v3db.QueryRow("SELECT COUNT(*) FROM backups").Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != numEntries {
			t.Fatalf("v3 backups count = %d, want %d", count, numEntries)
		}

		// Verify each entry's data was preserved with correct schema transformation.
		for _, e := range entries {
			var fileName, filePath string
			var fileExt sql.NullString
			var fileSize int64
			var blake3Hash string
			err := v3db.QueryRow(
				`SELECT blake3_hash, file_name, file_path, file_ext, file_size FROM backups WHERE file_path = ?`,
				e.FilePath,
			).Scan(&blake3Hash, &fileName, &filePath, &fileExt, &fileSize)
			if err != nil {
				t.Fatalf("entry not found in v3 for file_path=%q: %v", e.FilePath, err)
			}
			if fileName != e.FileName {
				t.Errorf("file_name mismatch: got %q, want %q", fileName, e.FileName)
			}
			if filePath != e.FilePath {
				t.Errorf("file_path mismatch: got %q, want %q", filePath, e.FilePath)
			}
			if fileSize != e.FileSize {
				t.Errorf("file_size mismatch: got %d, want %d", fileSize, e.FileSize)
			}

			// --- Verify: rehashed file exists at BLAKE3 path and hash matches content ---
			expectedHash := crypto.HashBytes(contents[e.MD5Hash])
			if blake3Hash != expectedHash {
				t.Errorf("blake3_hash in DB = %q, want %q (computed from content)", blake3Hash, expectedHash)
			}

			newPath := resolveStoragePath(storageDir, blake3Hash)
			storedContent, err := os.ReadFile(newPath)
			if err != nil {
				t.Fatalf("rehashed file not found at %s: %v", newPath, err)
			}

			// Verify the stored file's BLAKE3 hash matches the DB entry.
			actualHash := crypto.HashBytes(storedContent)
			if actualHash != blake3Hash {
				t.Errorf("file content hash = %q, DB hash = %q", actualHash, blake3Hash)
			}
		}

		// --- Verify: v2.0 database is byte-for-byte unchanged ---
		v2After, err := os.ReadFile(dbPath)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(v2Before, v2After) {
			t.Fatal("v2.0 database was modified during migration")
		}

		// --- Verify: rollback script was generated ---
		if result.RollbackScript == "" {
			t.Fatal("no rollback script generated")
		}
		if _, err := os.Stat(result.RollbackScript); os.IsNotExist(err) {
			t.Fatalf("rollback script file not found at %s", result.RollbackScript)
		}
	})
}
