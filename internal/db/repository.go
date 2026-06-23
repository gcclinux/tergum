// Package db provides the SQLite-backed repository for Tergum's metadata.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/gcclinux/tergum/internal/model"

	_ "modernc.org/sqlite"
)

// DeleteFilter specifies which backup entries to delete.
type DeleteFilter struct {
	BackupID   string // delete by backup_id
	FolderPath string // delete by path prefix
	FilePath   string // delete single file
	AllBackups bool   // across all backup sets
}

// JobUpdate holds optional fields for updating a backup job.
type JobUpdate struct {
	Status       *model.JobStatus
	FinishedAt   *time.Time
	FileCount    *int64
	BytesTotal   *int64
	BytesNew     *int64
	FilesDeduped *int64
	ErrorMessage *string
}

// JobFilter specifies criteria for listing backup jobs.
type JobFilter struct {
	ClientID *string
	Status   *model.JobStatus
	Limit    int
}

// RestoreRecord represents a single restore event.
type RestoreRecord struct {
	Blake3Hash   string
	FileName     string
	SourceBackup string
	RestoredTo   string
	RestoredAt   time.Time
	RestoredBy   string
	Success      bool
}

// Repository defines the interface for all database operations.
type Repository interface {
	// Backup operations
	InsertBackupEntry(ctx context.Context, entry model.BackupEntry) error
	GetManifest(ctx context.Context, backupID string) ([]model.ManifestEntry, error)
	FindByHash(ctx context.Context, hash string) ([]model.BackupEntry, error)
	FindByPath(ctx context.Context, pattern string) ([]model.BackupEntry, error)
	CountHashReferences(ctx context.Context, hash string) (int64, error)
	DeleteEntries(ctx context.Context, filter DeleteFilter) (int64, error)
	QueryEntries(ctx context.Context, filter DeleteFilter) ([]model.BackupEntry, error)

	// Deletion operations
	DeleteOrphanJobs(ctx context.Context) (int64, error)

	// Job operations
	CreateJob(ctx context.Context, job model.BackupJob) error
	UpdateJob(ctx context.Context, jobID string, update JobUpdate) error
	ListJobs(ctx context.Context, filter JobFilter) ([]model.BackupJob, error)
	FailStaleJobs(ctx context.Context, message string) (int64, error)

	// Retention operations
	GetFileVersions(ctx context.Context, filePath string) ([]model.BackupEntry, error)
	GetExpiredEntries(ctx context.Context, now time.Time) ([]model.BackupEntry, error)
	GetAllFilePaths(ctx context.Context) ([]string, error)
	DeleteEntryByID(ctx context.Context, id int64) error

	// Retention policy CRUD
	InsertRetentionPolicy(ctx context.Context, policy model.RetentionPolicy) error
	DeleteRetentionPolicy(ctx context.Context, name string) error
	ListRetentionPolicies(ctx context.Context) ([]model.RetentionPolicy, error)

	// Restore operations
	RecordRestore(ctx context.Context, entry RestoreRecord) error
	ListRestoreHistory(ctx context.Context, limit int) ([]RestoreRecord, error)
	DeleteAllActivity(ctx context.Context) (int64, error)

	// Watcher operations (exclusions)
	AddWatchExclude(ctx context.Context, path string) error
	RemoveWatchExclude(ctx context.Context, path string) error
	ListWatchExcludes(ctx context.Context) ([]string, error)

	// Include/exclude path operations
	AddIncludePath(ctx context.Context, path string) error
	RemoveIncludePath(ctx context.Context, path string) error
	ListIncludePaths(ctx context.Context) ([]string, error)
	AddExcludePattern(ctx context.Context, pattern string) error
	RemoveExcludePattern(ctx context.Context, pattern string) error
	ListExcludePatterns(ctx context.Context) ([]string, error)

	// Lifecycle
	Close() error
}

// SQLiteRepository implements Repository using modernc.org/sqlite.
type SQLiteRepository struct {
	db       *sql.DB
	isServer bool
}

// NewRepository creates a new SQLite repository at the given path.
// If isServer is true, server-specific tables (retention_policies) are created.
// Use ":memory:" as dbPath for in-memory testing.
func NewRepository(dbPath string, isServer bool) (*SQLiteRepository, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// Enable WAL mode for concurrent reads.
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable WAL mode: %w", err)
	}

	repo := &SQLiteRepository{db: db, isServer: isServer}
	if err := repo.createSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("create schema: %w", err)
	}

	return repo, nil
}

func (r *SQLiteRepository) createSchema() error {
	// backup_jobs must be created before backups due to foreign key.
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS backup_jobs (
			backup_id       TEXT PRIMARY KEY,
			level           TEXT NOT NULL CHECK(level IN ('AUTO','FULL','ONGOING')),
			client_id       TEXT NOT NULL,
			client_ip       TEXT,
			initiated_by    TEXT DEFAULT 'cli',
			started_at      TEXT NOT NULL DEFAULT (datetime('now')),
			finished_at     TEXT,
			status          TEXT NOT NULL DEFAULT 'running'
			                CHECK(status IN ('running','completed','failed','stopped','expired')),
			file_count      INTEGER DEFAULT 0,
			bytes_total     INTEGER DEFAULT 0,
			bytes_new       INTEGER DEFAULT 0,
			files_deduped   INTEGER DEFAULT 0,
			error_message   TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_jobs_client ON backup_jobs(client_id)`,
		`CREATE INDEX IF NOT EXISTS idx_jobs_status ON backup_jobs(status)`,
		`CREATE INDEX IF NOT EXISTS idx_jobs_started ON backup_jobs(started_at)`,

		`CREATE TABLE IF NOT EXISTS backups (
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
		)`,
		`CREATE INDEX IF NOT EXISTS idx_backups_hash ON backups(blake3_hash)`,
		`CREATE INDEX IF NOT EXISTS idx_backups_job ON backups(backup_id)`,
		`CREATE INDEX IF NOT EXISTS idx_backups_path ON backups(file_path)`,
		`CREATE INDEX IF NOT EXISTS idx_backups_name ON backups(file_name)`,
		`CREATE INDEX IF NOT EXISTS idx_backups_ext ON backups(file_ext)`,
		`CREATE INDEX IF NOT EXISTS idx_backups_expires ON backups(expires_at)`,

		`CREATE TABLE IF NOT EXISTS restore_history (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			blake3_hash     TEXT NOT NULL,
			file_name       TEXT NOT NULL,
			source_backup   TEXT NOT NULL,
			restored_to     TEXT NOT NULL,
			restored_at     TEXT NOT NULL DEFAULT (datetime('now')),
			restored_by     TEXT DEFAULT 'cli',
			success         INTEGER DEFAULT 1
		)`,

		`CREATE TABLE IF NOT EXISTS config (
			key             TEXT PRIMARY KEY,
			value           TEXT NOT NULL,
			updated_at      TEXT NOT NULL DEFAULT (datetime('now'))
		)`,

		`CREATE TABLE IF NOT EXISTS include_paths (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			path            TEXT NOT NULL UNIQUE,
			added_at        TEXT NOT NULL DEFAULT (datetime('now'))
		)`,

		`CREATE TABLE IF NOT EXISTS exclude_patterns (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			pattern         TEXT NOT NULL UNIQUE,
			added_at        TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
		`CREATE TABLE IF NOT EXISTS watch_excludes (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			path            TEXT NOT NULL UNIQUE,
			added_at        TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
	}

	if r.isServer {
		stmts = append(stmts, `CREATE TABLE IF NOT EXISTS retention_policies (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			name            TEXT NOT NULL UNIQUE,
			keep_days       INTEGER,
			keep_versions   INTEGER DEFAULT 1,
			pattern         TEXT,
			priority        INTEGER DEFAULT 0,
			enabled         INTEGER DEFAULT 1,
			created_at      TEXT NOT NULL DEFAULT (datetime('now'))
		)`)

		stmts = append(stmts, `CREATE TABLE IF NOT EXISTS client_registry (
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

		stmts = append(stmts, `CREATE TABLE IF NOT EXISTS missed_schedules (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			client_id    TEXT NOT NULL,
			level        TEXT NOT NULL,
			scheduled_at TEXT NOT NULL,
			resolved     INTEGER DEFAULT 0,
			resolved_at  TEXT,
			FOREIGN KEY (client_id) REFERENCES client_registry(client_id)
		)`)
	}

	for _, stmt := range stmts {
		if _, err := r.db.Exec(stmt); err != nil {
			return fmt.Errorf("exec %q: %w", stmt[:40], err)
		}
	}
	return nil
}

// Close closes the underlying database connection.
func (r *SQLiteRepository) Close() error {
	return r.db.Close()
}

// InsertBackupEntry inserts a single backup entry into the backups table.
func (r *SQLiteRepository) InsertBackupEntry(ctx context.Context, entry model.BackupEntry) error {
	var createdAt, modifiedAt, accessedAt *int64
	if entry.CreatedAt != nil {
		v := entry.CreatedAt.Unix()
		createdAt = &v
	}
	if entry.ModifiedAt != nil {
		v := entry.ModifiedAt.Unix()
		modifiedAt = &v
	}
	if entry.AccessedAt != nil {
		v := entry.AccessedAt.Unix()
		accessedAt = &v
	}

	var permissions *int64
	if entry.Permissions != nil {
		v := int64(*entry.Permissions)
		permissions = &v
	}

	var hidden, symlink int
	if entry.Hidden {
		hidden = 1
	}
	if entry.Symlink {
		symlink = 1
	}

	backupDate := entry.BackupDate.Format(time.DateTime)

	var expiresAt *string
	if entry.ExpiresAt != nil {
		v := entry.ExpiresAt.Format(time.DateTime)
		expiresAt = &v
	}

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO backups (
			backup_id, blake3_hash, file_name, file_path, file_ext, file_size,
			created_at, modified_at, accessed_at, permissions, owner, file_group,
			hidden, symlink, symlink_target, os, encrypted_dek, nonce,
			backup_date, expires_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		entry.BackupID, entry.Blake3Hash, entry.FileName, entry.FilePath,
		entry.FileExt, entry.FileSize,
		createdAt, modifiedAt, accessedAt, permissions,
		entry.Owner, entry.FileGroup,
		hidden, symlink, entry.SymlinkTarget, entry.OS,
		entry.EncryptedDEK, entry.Nonce,
		backupDate, expiresAt,
	)
	return err
}

// GetManifest returns all manifest entries for a given backup ID.
func (r *SQLiteRepository) GetManifest(ctx context.Context, backupID string) ([]model.ManifestEntry, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT blake3_hash, file_path, file_size, modified_at FROM backups WHERE backup_id = ?`,
		backupID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []model.ManifestEntry
	for rows.Next() {
		var e model.ManifestEntry
		var modifiedAt *int64
		if err := rows.Scan(&e.Blake3Hash, &e.FilePath, &e.FileSize, &modifiedAt); err != nil {
			return nil, err
		}
		if modifiedAt != nil {
			e.ModifiedAt = *modifiedAt
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// FindByHash returns all backup entries with the given BLAKE3 hash.
func (r *SQLiteRepository) FindByHash(ctx context.Context, hash string) ([]model.BackupEntry, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+backupColumns()+` FROM backups WHERE blake3_hash = ?`, hash,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanBackupEntries(rows)
}

// FindByPath returns all backup entries matching the given LIKE pattern.
func (r *SQLiteRepository) FindByPath(ctx context.Context, pattern string) ([]model.BackupEntry, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+backupColumns()+` FROM backups WHERE file_path LIKE ?`, pattern,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanBackupEntries(rows)
}

// CountHashReferences returns the number of entries referencing the given hash.
func (r *SQLiteRepository) CountHashReferences(ctx context.Context, hash string) (int64, error) {
	var count int64
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM backups WHERE blake3_hash = ?`, hash,
	).Scan(&count)
	return count, err
}

// DeleteEntries deletes backup entries matching the filter and returns the count of deleted rows.
func (r *SQLiteRepository) DeleteEntries(ctx context.Context, filter DeleteFilter) (int64, error) {
	var conditions []string
	var args []interface{}

	if filter.BackupID != "" {
		conditions = append(conditions, "backup_id = ?")
		args = append(args, filter.BackupID)
	}
	if filter.FolderPath != "" {
		conditions = append(conditions, "file_path LIKE ?")
		args = append(args, filter.FolderPath+"%")
	}
	if filter.FilePath != "" {
		conditions = append(conditions, "file_path = ?")
		args = append(args, filter.FilePath)
	}

	// AllBackups with no other filter means delete everything.
	if len(conditions) == 0 && !filter.AllBackups {
		return 0, nil
	}

	query := "DELETE FROM backups"
	if len(conditions) > 0 {
		where := strings.Join(conditions, " AND ")
		query += " WHERE " + where
	}

	result, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// CreateJob inserts a new backup job record.
func (r *SQLiteRepository) CreateJob(ctx context.Context, job model.BackupJob) error {
	startedAt := job.StartedAt.Format(time.DateTime)
	var finishedAt *string
	if job.FinishedAt != nil {
		v := job.FinishedAt.Format(time.DateTime)
		finishedAt = &v
	}

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO backup_jobs (
			backup_id, level, client_id, client_ip, initiated_by,
			started_at, finished_at, status, file_count, bytes_total,
			bytes_new, files_deduped, error_message
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		job.BackupID, job.Level, job.ClientID, job.ClientIP, job.InitiatedBy,
		startedAt, finishedAt, string(job.Status), job.FileCount, job.BytesTotal,
		job.BytesNew, job.FilesDeduped, job.ErrorMessage,
	)
	return err
}

// UpdateJob applies partial updates to an existing backup job.
func (r *SQLiteRepository) UpdateJob(ctx context.Context, jobID string, update JobUpdate) error {
	var sets []string
	var args []interface{}

	if update.Status != nil {
		sets = append(sets, "status = ?")
		args = append(args, string(*update.Status))
	}
	if update.FinishedAt != nil {
		sets = append(sets, "finished_at = ?")
		args = append(args, update.FinishedAt.Format(time.DateTime))
	}
	if update.FileCount != nil {
		sets = append(sets, "file_count = ?")
		args = append(args, *update.FileCount)
	}
	if update.BytesTotal != nil {
		sets = append(sets, "bytes_total = ?")
		args = append(args, *update.BytesTotal)
	}
	if update.BytesNew != nil {
		sets = append(sets, "bytes_new = ?")
		args = append(args, *update.BytesNew)
	}
	if update.FilesDeduped != nil {
		sets = append(sets, "files_deduped = ?")
		args = append(args, *update.FilesDeduped)
	}
	if update.ErrorMessage != nil {
		sets = append(sets, "error_message = ?")
		args = append(args, *update.ErrorMessage)
	}

	if len(sets) == 0 {
		return nil
	}

	args = append(args, jobID)
	query := "UPDATE backup_jobs SET " + strings.Join(sets, ", ") + " WHERE backup_id = ?"

	_, err := r.db.ExecContext(ctx, query, args...)
	return err
}

// FailStaleJobs updates any job marked as 'running' to 'failed' status with a specific error message.
// This is used on startup to clean up jobs that were interrupted by a crash or restart.
func (r *SQLiteRepository) FailStaleJobs(ctx context.Context, message string) (int64, error) {
	now := time.Now().UTC().Format(time.DateTime)
	res, err := r.db.ExecContext(ctx,
		`UPDATE backup_jobs 
		 SET status = 'failed', finished_at = ?, error_message = ? 
		 WHERE status = 'running'`,
		now, message,
	)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}


// ListJobs returns backup jobs matching the filter, ordered by started_at DESC.
func (r *SQLiteRepository) ListJobs(ctx context.Context, filter JobFilter) ([]model.BackupJob, error) {
	var conditions []string
	var args []interface{}

	if filter.ClientID != nil {
		conditions = append(conditions, "client_id = ?")
		args = append(args, *filter.ClientID)
	}
	if filter.Status != nil {
		conditions = append(conditions, "status = ?")
		args = append(args, string(*filter.Status))
	}

	query := "SELECT backup_id, level, client_id, client_ip, initiated_by, started_at, finished_at, status, file_count, bytes_total, bytes_new, files_deduped, error_message FROM backup_jobs"
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY started_at DESC"
	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", filter.Limit)
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []model.BackupJob
	for rows.Next() {
		var j model.BackupJob
		var startedAt string
		var finishedAt *string
		var status string
		var clientIP *string
		var initiatedBy *string

		if err := rows.Scan(
			&j.BackupID, &j.Level, &j.ClientID, &clientIP, &initiatedBy,
			&startedAt, &finishedAt, &status, &j.FileCount, &j.BytesTotal,
			&j.BytesNew, &j.FilesDeduped, &j.ErrorMessage,
		); err != nil {
			return nil, err
		}

		j.Status = model.JobStatus(status)
		if clientIP != nil {
			j.ClientIP = *clientIP
		}
		if initiatedBy != nil {
			j.InitiatedBy = *initiatedBy
		}

		t, err := time.Parse(time.DateTime, startedAt)
		if err == nil {
			j.StartedAt = t
		}
		if finishedAt != nil {
			t, err := time.Parse(time.DateTime, *finishedAt)
			if err == nil {
				j.FinishedAt = &t
			}
		}

		jobs = append(jobs, j)
	}
	return jobs, rows.Err()
}

// GetFileVersions returns all backup entries for a given file path, ordered by backup_date DESC.
func (r *SQLiteRepository) GetFileVersions(ctx context.Context, filePath string) ([]model.BackupEntry, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+backupColumns()+` FROM backups WHERE file_path = ? ORDER BY backup_date DESC`,
		filePath,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanBackupEntries(rows)
}

// GetExpiredEntries returns all backup entries where expires_at is before the given time.
func (r *SQLiteRepository) GetExpiredEntries(ctx context.Context, now time.Time) ([]model.BackupEntry, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+backupColumns()+` FROM backups WHERE expires_at IS NOT NULL AND expires_at < ?`,
		now.Format(time.DateTime),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanBackupEntries(rows)
}

// RecordRestore inserts a restore event into restore_history.
func (r *SQLiteRepository) RecordRestore(ctx context.Context, entry RestoreRecord) error {
	var success int
	if entry.Success {
		success = 1
	}

	restoredBy := entry.RestoredBy
	if restoredBy == "" {
		restoredBy = "cli"
	}

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO restore_history (blake3_hash, file_name, source_backup, restored_to, restored_by, success)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		entry.Blake3Hash, entry.FileName, entry.SourceBackup, entry.RestoredTo, restoredBy, success,
	)
	return err
}

// ListRestoreHistory returns the most recent restore events, ordered by restored_at DESC.
func (r *SQLiteRepository) ListRestoreHistory(ctx context.Context, limit int) ([]RestoreRecord, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT blake3_hash, file_name, source_backup, restored_to, restored_at, restored_by, success
		 FROM restore_history ORDER BY restored_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []RestoreRecord
	for rows.Next() {
		var rec RestoreRecord
		var success int
		var restoredAt string
		if err := rows.Scan(&rec.Blake3Hash, &rec.FileName, &rec.SourceBackup, &rec.RestoredTo, &restoredAt, &rec.RestoredBy, &success); err != nil {
			return nil, err
		}
		rec.Success = success == 1
		t, err := time.Parse(time.DateTime, restoredAt)
		if err == nil {
			rec.RestoredAt = t
		}
		records = append(records, rec)
	}
	return records, rows.Err()
}

// DeleteAllActivity clears restore history and completed/failed/stopped backup job records.
func (r *SQLiteRepository) DeleteAllActivity(ctx context.Context) (int64, error) {
	var total int64

	// Delete restore history.
	res, err := r.db.ExecContext(ctx, `DELETE FROM restore_history`)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	total += n

	// Delete non-running backup jobs (keep running ones).
	res, err = r.db.ExecContext(ctx, `DELETE FROM backup_jobs WHERE status != 'running'`)
	if err != nil {
		return total, err
	}
	n, _ = res.RowsAffected()
	total += n

	return total, nil
}

// AddWatchExclude inserts a path into watch_excludes.
func (r *SQLiteRepository) AddWatchExclude(ctx context.Context, path string) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO watch_excludes (path) VALUES (?)`, path)
	return err
}

// RemoveWatchExclude deletes a path from watch_excludes.
func (r *SQLiteRepository) RemoveWatchExclude(ctx context.Context, path string) error {
	_, err := r.db.ExecContext(ctx,
		`DELETE FROM watch_excludes WHERE path = ?`, path)
	return err
}

// ListWatchExcludes returns all watch excludes, ordered by addition time.
func (r *SQLiteRepository) ListWatchExcludes(ctx context.Context) ([]string, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT path FROM watch_excludes ORDER BY added_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var paths []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		paths = append(paths, p)
	}
	return paths, rows.Err()
}

// AddIncludePath adds a path to the include_paths table.
func (r *SQLiteRepository) AddIncludePath(ctx context.Context, path string) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO include_paths (path) VALUES (?)`, path)
	return err
}

// RemoveIncludePath removes a path from the include_paths table.
func (r *SQLiteRepository) RemoveIncludePath(ctx context.Context, path string) error {
	_, err := r.db.ExecContext(ctx,
		`DELETE FROM include_paths WHERE path = ?`, path)
	return err
}

// ListIncludePaths returns all registered include paths, ordered by addition time.
func (r *SQLiteRepository) ListIncludePaths(ctx context.Context) ([]string, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT path FROM include_paths ORDER BY added_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var paths []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		paths = append(paths, p)
	}
	return paths, rows.Err()
}

// AddExcludePattern adds a pattern to the exclude_patterns table.
func (r *SQLiteRepository) AddExcludePattern(ctx context.Context, pattern string) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO exclude_patterns (pattern) VALUES (?)`, pattern)
	return err
}

// RemoveExcludePattern removes a pattern from the exclude_patterns table.
func (r *SQLiteRepository) RemoveExcludePattern(ctx context.Context, pattern string) error {
	_, err := r.db.ExecContext(ctx,
		`DELETE FROM exclude_patterns WHERE pattern = ?`, pattern)
	return err
}

// ListExcludePatterns returns all registered exclude patterns, ordered by addition time.
func (r *SQLiteRepository) ListExcludePatterns(ctx context.Context) ([]string, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT pattern FROM exclude_patterns ORDER BY added_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var patterns []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		patterns = append(patterns, p)
	}
	return patterns, rows.Err()
}

// GetAllFilePaths returns all distinct file_path values from the backups table.
func (r *SQLiteRepository) GetAllFilePaths(ctx context.Context) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT DISTINCT file_path FROM backups`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var paths []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		paths = append(paths, p)
	}
	return paths, rows.Err()
}

// DeleteEntryByID deletes a single backup entry by its primary key ID.
func (r *SQLiteRepository) DeleteEntryByID(ctx context.Context, id int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM backups WHERE id = ?`, id)
	return err
}

// InsertRetentionPolicy inserts a new retention policy into the retention_policies table.
func (r *SQLiteRepository) InsertRetentionPolicy(ctx context.Context, policy model.RetentionPolicy) error {
	var enabled int
	if policy.Enabled {
		enabled = 1
	}
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO retention_policies (name, keep_days, keep_versions, pattern, priority, enabled)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		policy.Name, policy.KeepDays, policy.KeepVersions, policy.Pattern, policy.Priority, enabled,
	)
	return err
}

// DeleteRetentionPolicy deletes a retention policy by name.
func (r *SQLiteRepository) DeleteRetentionPolicy(ctx context.Context, name string) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM retention_policies WHERE name = ?`, name)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("retention policy %q not found", name)
	}
	return nil
}

// ListRetentionPolicies returns all retention policies, ordered by priority DESC.
func (r *SQLiteRepository) ListRetentionPolicies(ctx context.Context) ([]model.RetentionPolicy, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, name, keep_days, keep_versions, pattern, priority, enabled, created_at
		 FROM retention_policies ORDER BY priority DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var policies []model.RetentionPolicy
	for rows.Next() {
		var p model.RetentionPolicy
		var enabled int
		var createdAt string
		if err := rows.Scan(&p.ID, &p.Name, &p.KeepDays, &p.KeepVersions, &p.Pattern, &p.Priority, &enabled, &createdAt); err != nil {
			return nil, err
		}
		p.Enabled = enabled == 1
		t, err := time.Parse(time.DateTime, createdAt)
		if err == nil {
			p.CreatedAt = t
		}
		policies = append(policies, p)
	}
	return policies, rows.Err()
}

// QueryEntries returns backup entries matching the filter without deleting them.
func (r *SQLiteRepository) QueryEntries(ctx context.Context, filter DeleteFilter) ([]model.BackupEntry, error) {
	var conditions []string
	var args []interface{}

	if filter.BackupID != "" {
		conditions = append(conditions, "backup_id = ?")
		args = append(args, filter.BackupID)
	}
	if filter.FolderPath != "" {
		conditions = append(conditions, "file_path LIKE ?")
		args = append(args, filter.FolderPath+"%")
	}
	if filter.FilePath != "" {
		conditions = append(conditions, "file_path = ?")
		args = append(args, filter.FilePath)
	}

	query := `SELECT ` + backupColumns() + ` FROM backups`
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanBackupEntries(rows)
}

// DeleteOrphanJobs removes backup_jobs records that have no remaining entries in the backups table.
func (r *SQLiteRepository) DeleteOrphanJobs(ctx context.Context) (int64, error) {
	result, err := r.db.ExecContext(ctx,
		`DELETE FROM backup_jobs WHERE backup_id NOT IN (SELECT DISTINCT backup_id FROM backups)`,
	)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// backupColumns returns the column list for scanning backup entries.
func backupColumns() string {
	return `id, backup_id, blake3_hash, file_name, file_path, file_ext, file_size,
		created_at, modified_at, accessed_at, permissions, owner, file_group,
		hidden, symlink, symlink_target, os, encrypted_dek, nonce,
		backup_date, expires_at`
}

// scanBackupEntries scans rows into a slice of BackupEntry.
func scanBackupEntries(rows *sql.Rows) ([]model.BackupEntry, error) {
	var entries []model.BackupEntry
	for rows.Next() {
		var e model.BackupEntry
		var createdAt, modifiedAt, accessedAt *int64
		var permissions *int64
		var hidden, symlink int
		var backupDate string
		var expiresAt *string

		if err := rows.Scan(
			&e.ID, &e.BackupID, &e.Blake3Hash, &e.FileName, &e.FilePath,
			&e.FileExt, &e.FileSize,
			&createdAt, &modifiedAt, &accessedAt, &permissions,
			&e.Owner, &e.FileGroup,
			&hidden, &symlink, &e.SymlinkTarget, &e.OS,
			&e.EncryptedDEK, &e.Nonce,
			&backupDate, &expiresAt,
		); err != nil {
			return nil, err
		}

		if createdAt != nil {
			t := time.Unix(*createdAt, 0).UTC()
			e.CreatedAt = &t
		}
		if modifiedAt != nil {
			t := time.Unix(*modifiedAt, 0).UTC()
			e.ModifiedAt = &t
		}
		if accessedAt != nil {
			t := time.Unix(*accessedAt, 0).UTC()
			e.AccessedAt = &t
		}
		if permissions != nil {
			v := uint32(*permissions)
			e.Permissions = &v
		}
		e.Hidden = hidden == 1
		e.Symlink = symlink == 1

		t, err := time.Parse(time.DateTime, backupDate)
		if err == nil {
			e.BackupDate = t
		}
		if expiresAt != nil {
			t, err := time.Parse(time.DateTime, *expiresAt)
			if err == nil {
				e.ExpiresAt = &t
			}
		}

		entries = append(entries, e)
	}
	return entries, rows.Err()
}
