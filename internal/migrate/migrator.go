// Package migrate implements v2.0 to v3.0 database and storage migration.
package migrate

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gcclinux/tergum/internal/crypto"

	_ "modernc.org/sqlite"
)

// Migrator defines the interface for v2.0 â†’ v3.0 migration.
type Migrator interface {
	// Migrate transforms v2.0 database to v3.0 format.
	Migrate(ctx context.Context, opts MigrateOptions) (*MigrateResult, error)
	// Verify checks integrity of a migrated v3.0 database against storage.
	Verify(ctx context.Context, dbPath string) (*VerifyResult, error)
}

// MigrateOptions holds configuration for the migration process.
type MigrateOptions struct {
	FromDB     string // Path to v2.0 SQLite database
	Rehash     bool   // Compute BLAKE3 replacing MD5
	Encrypt    bool   // Encrypt existing files
	Verify     bool   // Post-migration integrity check
	MasterKey  []byte // Master key for encryption (required if Encrypt is true)
	StorageDir string // Path to the storage directory containing backed-up files
}

// MigrateResult holds the outcome of a migration.
type MigrateResult struct {
	EntriesMigrated int64
	FilesRehashed   int64
	FilesEncrypted  int64
	RollbackScript  string // path to generated rollback script
}

// VerifyResult holds the outcome of a verification check.
type VerifyResult struct {
	TotalEntries   int64 // total entries in the database
	OrphanedDB     int64 // entries with no file on disk
	OrphanedDisk   int64 // files on disk with no DB entry
	HashMismatches int64 // files whose BLAKE3 doesn't match DB
	Verified       int64 // successfully verified entries
}

// v2Entry represents a row from the v2.0 BACKUPS table.
type v2Entry struct {
	ID        int64
	MD5Hash   string
	FileName  string
	FilePath  string
	FileExt   string
	FileSize  int64
	BackupID  string
	CreatedAt *time.Time
}

// DefaultMigrator implements the Migrator interface.
type DefaultMigrator struct {
	encryptor crypto.Encryptor
}

// NewMigrator creates a new DefaultMigrator.
func NewMigrator() *DefaultMigrator {
	return &DefaultMigrator{
		encryptor: crypto.NewEncryptor(),
	}
}

// Migrate reads the v2.0 database and creates a new v3.0 database alongside it.
// It optionally rehashes files (MD5â†’BLAKE3), encrypts them, and verifies integrity.
func (m *DefaultMigrator) Migrate(ctx context.Context, opts MigrateOptions) (*MigrateResult, error) {
	if opts.FromDB == "" {
		return nil, fmt.Errorf("migrate: FromDB path is required")
	}
	if opts.StorageDir == "" {
		return nil, fmt.Errorf("migrate: StorageDir path is required")
	}
	if opts.Encrypt && len(opts.MasterKey) != 32 {
		return nil, fmt.Errorf("migrate: MasterKey must be 32 bytes when Encrypt is enabled")
	}

	// Open v2.0 database read-only.
	v2db, err := sql.Open("sqlite", opts.FromDB+"?mode=ro")
	if err != nil {
		return nil, fmt.Errorf("migrate: open v2 database: %w", err)
	}
	defer v2db.Close()

	// Read all entries from v2.0 BACKUPS table.
	entries, err := readV2Entries(ctx, v2db)
	if err != nil {
		return nil, fmt.Errorf("migrate: read v2 entries: %w", err)
	}

	// Create the new v3.0 database alongside the v2.0 one.
	v3Path := deriveV3Path(opts.FromDB)
	v3db, err := createV3Database(v3Path)
	if err != nil {
		return nil, fmt.Errorf("migrate: create v3 database: %w", err)
	}
	defer v3db.Close()

	result := &MigrateResult{}
	var rollbackEntries []rollbackEntry

	// Create a single backup job for the migration.
	migrationJobID := uuid.New().String()
	if err := insertMigrationJob(ctx, v3db, migrationJobID); err != nil {
		return nil, fmt.Errorf("migrate: create migration job: %w", err)
	}

	for _, entry := range entries {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		hash := entry.MD5Hash
		oldStoragePath := resolveStoragePath(opts.StorageDir, entry.MD5Hash)

		// Rehash: compute BLAKE3 and rename file.
		if opts.Rehash {
			blake3Hash, err := crypto.HashFile(oldStoragePath)
			if err != nil {
				// Skip files that don't exist on disk - they'll be caught by verify.
				if os.IsNotExist(err) {
					hash = entry.MD5Hash // keep the old hash for the DB entry
				} else {
					return nil, fmt.Errorf("migrate: hash file %s: %w", oldStoragePath, err)
				}
			} else {
				hash = blake3Hash
				newStoragePath := resolveStoragePath(opts.StorageDir, blake3Hash)

				// Only rename if paths differ.
				if oldStoragePath != newStoragePath {
					// Ensure target directory exists.
					if err := os.MkdirAll(filepath.Dir(newStoragePath), 0o755); err != nil {
						return nil, fmt.Errorf("migrate: create directory for %s: %w", newStoragePath, err)
					}
					if err := os.Rename(oldStoragePath, newStoragePath); err != nil {
						if !os.IsNotExist(err) {
							return nil, fmt.Errorf("migrate: rename %s â†’ %s: %w", oldStoragePath, newStoragePath, err)
						}
					} else {
						rollbackEntries = append(rollbackEntries, rollbackEntry{
							From: newStoragePath,
							To:   oldStoragePath,
						})
						result.FilesRehashed++
					}
				}
			}
		}

		// Encrypt: read file, encrypt with per-file DEK, write back.
		var encryptedDEK, nonce []byte
		if opts.Encrypt {
			storagePath := resolveStoragePath(opts.StorageDir, hash)
			plaintext, err := os.ReadFile(storagePath)
			if err != nil {
				if !os.IsNotExist(err) {
					return nil, fmt.Errorf("migrate: read file for encryption %s: %w", storagePath, err)
				}
				// File missing on disk; skip encryption for this entry.
			} else {
				ciphertext, wrappedDEK, n, err := m.encryptor.Encrypt(plaintext, opts.MasterKey)
				if err != nil {
					return nil, fmt.Errorf("migrate: encrypt file %s: %w", storagePath, err)
				}
				if err := os.WriteFile(storagePath, ciphertext, 0o600); err != nil {
					return nil, fmt.Errorf("migrate: write encrypted file %s: %w", storagePath, err)
				}
				encryptedDEK = wrappedDEK
				nonce = n
				result.FilesEncrypted++
			}
		}

		// Insert into v3.0 database.
		if err := insertV3Entry(ctx, v3db, migrationJobID, entry, hash, encryptedDEK, nonce); err != nil {
			return nil, fmt.Errorf("migrate: insert v3 entry: %w", err)
		}
		result.EntriesMigrated++
	}

	// Update migration job as completed.
	if err := completeMigrationJob(ctx, v3db, migrationJobID, result.EntriesMigrated); err != nil {
		return nil, fmt.Errorf("migrate: complete migration job: %w", err)
	}

	// Generate rollback script.
	if len(rollbackEntries) > 0 {
		scriptPath, err := generateRollbackScript(opts.StorageDir, rollbackEntries)
		if err != nil {
			return nil, fmt.Errorf("migrate: generate rollback script: %w", err)
		}
		result.RollbackScript = scriptPath
	}

	// Post-migration verify.
	if opts.Verify {
		_, err := m.Verify(ctx, v3Path)
		if err != nil {
			return nil, fmt.Errorf("migrate: post-migration verify: %w", err)
		}
	}

	return result, nil
}

// Verify checks that all DB entries have files on disk and vice versa,
// and that BLAKE3 hashes match file content.
func (m *DefaultMigrator) Verify(ctx context.Context, dbPath string) (*VerifyResult, error) {
	db, err := sql.Open("sqlite", dbPath+"?mode=ro")
	if err != nil {
		return nil, fmt.Errorf("verify: open database: %w", err)
	}
	defer db.Close()

	// Get the storage dir from the config table, or infer from db location.
	storageDir, err := getStorageDir(db, dbPath)
	if err != nil {
		return nil, fmt.Errorf("verify: get storage dir: %w", err)
	}

	result := &VerifyResult{}

	// Get all entries from the database.
	rows, err := db.QueryContext(ctx, `SELECT blake3_hash FROM backups`)
	if err != nil {
		return nil, fmt.Errorf("verify: query entries: %w", err)
	}
	defer rows.Close()

	dbHashes := make(map[string]int64) // hash â†’ count of entries referencing it
	for rows.Next() {
		var hash string
		if err := rows.Scan(&hash); err != nil {
			return nil, fmt.Errorf("verify: scan row: %w", err)
		}
		dbHashes[hash]++
		result.TotalEntries++
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("verify: iterate rows: %w", err)
	}

	// Check DBâ†’disk: every unique hash should have a file.
	for hash := range dbHashes {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		storagePath := resolveStoragePath(storageDir, hash)
		if _, err := os.Stat(storagePath); os.IsNotExist(err) {
			result.OrphanedDB += dbHashes[hash]
		} else if err == nil {
			// Verify BLAKE3 hash.
			actualHash, err := crypto.HashFile(storagePath)
			if err != nil {
				result.HashMismatches += dbHashes[hash]
			} else if actualHash != hash {
				result.HashMismatches += dbHashes[hash]
			} else {
				result.Verified += dbHashes[hash]
			}
		}
	}

	// Check diskâ†’DB: walk storage directory and find orphans.
	diskHashes := make(map[string]bool)
	err = filepath.WalkDir(storageDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		if d.IsDir() {
			return nil
		}
		// Extract hash from filename (the filename IS the hash).
		hash := d.Name()
		diskHashes[hash] = true
		return nil
	})
	if err != nil {
		// If storage dir doesn't exist, all DB entries are orphaned.
		if os.IsNotExist(err) {
			result.OrphanedDB = result.TotalEntries
			return result, nil
		}
		return nil, fmt.Errorf("verify: walk storage: %w", err)
	}

	for hash := range diskHashes {
		if _, exists := dbHashes[hash]; !exists {
			result.OrphanedDisk++
		}
	}

	return result, nil
}

// readV2Entries reads all rows from the v2.0 BACKUPS table.
func readV2Entries(ctx context.Context, db *sql.DB) ([]v2Entry, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, md5_hash, file_name, file_path, file_ext, file_size, backup_id, created_at
		 FROM BACKUPS`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []v2Entry
	for rows.Next() {
		var e v2Entry
		var createdAt *string
		if err := rows.Scan(&e.ID, &e.MD5Hash, &e.FileName, &e.FilePath, &e.FileExt, &e.FileSize, &e.BackupID, &createdAt); err != nil {
			return nil, err
		}
		if createdAt != nil {
			t, err := time.Parse(time.DateTime, *createdAt)
			if err == nil {
				e.CreatedAt = &t
			}
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// createV3Database creates a new v3.0 SQLite database.
func createV3Database(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}

	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, err
	}

	schema := `
	CREATE TABLE IF NOT EXISTS backup_jobs (
		backup_id       TEXT PRIMARY KEY,
		level           TEXT NOT NULL CHECK(level IN ('AUTO','FULL','ONGOING')),
		client_id       TEXT NOT NULL,
		client_ip       TEXT,
		initiated_by    TEXT DEFAULT 'migrate',
		started_at      TEXT NOT NULL DEFAULT (datetime('now')),
		finished_at     TEXT,
		status          TEXT NOT NULL DEFAULT 'running'
		                CHECK(status IN ('running','completed','failed','stopped','expired')),
		file_count      INTEGER DEFAULT 0,
		bytes_total     INTEGER DEFAULT 0,
		bytes_new       INTEGER DEFAULT 0,
		files_deduped   INTEGER DEFAULT 0,
		error_message   TEXT
	);
	CREATE INDEX IF NOT EXISTS idx_jobs_client ON backup_jobs(client_id);
	CREATE INDEX IF NOT EXISTS idx_jobs_status ON backup_jobs(status);
	CREATE INDEX IF NOT EXISTS idx_jobs_started ON backup_jobs(started_at);

	CREATE TABLE IF NOT EXISTS backups (
		id              INTEGER PRIMARY KEY AUTOINCREMENT,
		backup_id       TEXT NOT NULL,
		blake3_hash     TEXT NOT NULL,
		file_name       TEXT NOT NULL,
		file_path       TEXT NOT NULL,
		file_ext        TEXT,
		file_size       INTEGER NOT NULL,
		created_at      INTEGER,
		modified_at     INTEGER,
		accessed_at     INTEGER,
		permissions     INTEGER,
		owner           TEXT,
		file_group      TEXT,
		hidden          INTEGER DEFAULT 0,
		symlink         INTEGER DEFAULT 0,
		symlink_target  TEXT,
		os              TEXT NOT NULL,
		encrypted_dek   BLOB,
		nonce           BLOB,
		backup_date     TEXT NOT NULL DEFAULT (datetime('now')),
		expires_at      TEXT,
		FOREIGN KEY (backup_id) REFERENCES backup_jobs(backup_id)
	);
	CREATE INDEX IF NOT EXISTS idx_backups_hash ON backups(blake3_hash);
	CREATE INDEX IF NOT EXISTS idx_backups_job ON backups(backup_id);
	CREATE INDEX IF NOT EXISTS idx_backups_path ON backups(file_path);
	CREATE INDEX IF NOT EXISTS idx_backups_name ON backups(file_name);
	CREATE INDEX IF NOT EXISTS idx_backups_ext ON backups(file_ext);
	CREATE INDEX IF NOT EXISTS idx_backups_expires ON backups(expires_at);

	CREATE TABLE IF NOT EXISTS config (
		key             TEXT PRIMARY KEY,
		value           TEXT NOT NULL,
		updated_at      TEXT NOT NULL DEFAULT (datetime('now'))
	);
	`

	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}

	return db, nil
}

// insertMigrationJob creates a backup_jobs entry for the migration.
func insertMigrationJob(ctx context.Context, db *sql.DB, jobID string) error {
	_, err := db.ExecContext(ctx,
		`INSERT INTO backup_jobs (backup_id, level, client_id, initiated_by, status)
		 VALUES (?, 'FULL', 'migration', 'migrate', 'running')`, jobID)
	return err
}

// completeMigrationJob marks the migration job as completed.
func completeMigrationJob(ctx context.Context, db *sql.DB, jobID string, fileCount int64) error {
	_, err := db.ExecContext(ctx,
		`UPDATE backup_jobs SET status = 'completed', finished_at = datetime('now'), file_count = ?
		 WHERE backup_id = ?`, fileCount, jobID)
	return err
}

// insertV3Entry inserts a single entry into the v3.0 backups table.
func insertV3Entry(ctx context.Context, db *sql.DB, jobID string, entry v2Entry, blake3Hash string, encryptedDEK, nonce []byte) error {
	var createdAt *int64
	if entry.CreatedAt != nil {
		v := entry.CreatedAt.Unix()
		createdAt = &v
	}

	_, err := db.ExecContext(ctx,
		`INSERT INTO backups (backup_id, blake3_hash, file_name, file_path, file_ext, file_size, created_at, os, encrypted_dek, nonce)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		jobID, blake3Hash, entry.FileName, entry.FilePath, entry.FileExt, entry.FileSize,
		createdAt, runtime.GOOS, encryptedDEK, nonce)
	return err
}

// resolveStoragePath returns the full path to a file in the two-level CAS layout.
func resolveStoragePath(storageDir, hash string) string {
	if len(hash) < 2 {
		return filepath.Join(storageDir, hash)
	}
	return filepath.Join(storageDir, hash[:2], hash)
}

// deriveV3Path computes the output path for the v3.0 database from the v2.0 path.
func deriveV3Path(v2Path string) string {
	dir := filepath.Dir(v2Path)
	base := filepath.Base(v2Path)
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)
	return filepath.Join(dir, name+"_v3"+ext)
}

// rollbackEntry represents a single file rename to undo.
type rollbackEntry struct {
	From string // current path (after rename)
	To   string // original path (before rename)
}

// generateRollbackScript writes a shell script that undoes all file renames.
func generateRollbackScript(storageDir string, entries []rollbackEntry) (string, error) {
	var ext, header string
	if runtime.GOOS == "windows" {
		ext = ".bat"
		header = "@echo off\r\nREM Tergum migration rollback script\r\nREM Generated: " + time.Now().Format(time.RFC3339) + "\r\n\r\n"
	} else {
		ext = ".sh"
		header = "#!/bin/bash\n# Tergum migration rollback script\n# Generated: " + time.Now().Format(time.RFC3339) + "\n\nset -e\n\n"
	}

	scriptPath := filepath.Join(storageDir, "rollback"+ext)
	var sb strings.Builder
	sb.WriteString(header)

	for _, e := range entries {
		if runtime.GOOS == "windows" {
			sb.WriteString(fmt.Sprintf("move \"%s\" \"%s\"\r\n", e.From, e.To))
		} else {
			sb.WriteString(fmt.Sprintf("mv \"%s\" \"%s\"\n", e.From, e.To))
		}
	}

	if runtime.GOOS != "windows" {
		sb.WriteString("\necho \"Rollback complete.\"\n")
	} else {
		sb.WriteString("\r\necho Rollback complete.\r\n")
	}

	if err := os.WriteFile(scriptPath, []byte(sb.String()), 0o755); err != nil {
		return "", err
	}

	return scriptPath, nil
}

// getStorageDir attempts to read the storage directory from the config table,
// falling back to a sibling "storage" directory relative to the database.
func getStorageDir(db *sql.DB, dbPath string) (string, error) {
	var value string
	err := db.QueryRow(`SELECT value FROM config WHERE key = 'storage_dir'`).Scan(&value)
	if err == nil && value != "" {
		return value, nil
	}
	// Fallback: assume storage/ is a sibling of the database file.
	return filepath.Join(filepath.Dir(dbPath), "storage"), nil
}

// CopyFile copies a file from src to dst, creating parent directories as needed.
func CopyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}
