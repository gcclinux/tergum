package backup

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/gcclinux/tergum/internal/crypto"
	"github.com/gcclinux/tergum/internal/db"
	"github.com/gcclinux/tergum/internal/model"
)

// Engine defines the backup engine interface.
type Engine interface {
	// RunBackup executes a full or incremental backup.
	RunBackup(ctx context.Context, req BackupRequest) (*BackupResult, error)
	// Stop gracefully stops an in-progress backup.
	Stop(ctx context.Context) error
}

// BackupRequest specifies parameters for a backup operation.
type BackupRequest struct {
	Level       model.BackupLevel
	ClientID    string
	InitiatedBy string
}

// BackupResult contains the outcome of a backup operation.
type BackupResult struct {
	BackupID       string
	FilesProcessed int64
	BytesNew       int64
	FilesDeduped   int64
	Status         model.JobStatus
}

// ServerConnection abstracts server-side operations for the backup engine.
// This allows the engine to work with both local (direct) and remote (gRPC) backends.
type ServerConnection interface {
	ExchangeManifest(ctx context.Context, manifest []model.ManifestEntry) (ManifestDiff, error)
	UploadFile(ctx context.Context, hash string, data []byte, wrappedDEK []byte, nonce []byte, entry model.BackupEntry) error
	SyncDatabase(ctx context.Context, dbPath string) error
}

// EngineConfig holds configuration for the backup engine.
type EngineConfig struct {
	IncludePaths    []string
	ExcludePatterns []string
	MaxFileSize     int64
	EncryptionOn    bool
	MasterKey       []byte // 32-byte master key for encryption (required if EncryptionOn)
	DatabasePath    string // path to local SQLite database for SyncDatabase
}

// BackupEngine implements the Engine interface.
type BackupEngine struct {
	server    ServerConnection
	repo      db.Repository
	encryptor *crypto.AESEncryptor
	config    EngineConfig
	stopped   atomic.Bool
}

// NewBackupEngine creates a new BackupEngine instance.
func NewBackupEngine(server ServerConnection, repo db.Repository, encryptor *crypto.AESEncryptor, cfg EngineConfig) *BackupEngine {
	return &BackupEngine{
		server:    server,
		repo:      repo,
		encryptor: encryptor,
		config:    cfg,
	}
}

// RunBackup executes the backup pipeline:
// 1. Generate backup_id (UUID)
// 2. Create backup_jobs record (status "running")
// 3. Scan files with include paths, exclude patterns, max file size
// 4. Build manifest from scanned files
// 5. Exchange manifest with server to get ManifestDiff
// 6. For each needed hash: read file, encrypt if enabled, upload
// 7. For deduplicated files: insert backup entry (reuse existing hash)
// 8. Sync local database to server
// 9. Update backup_jobs record with completion status
// 10. Return BackupResult
func (e *BackupEngine) RunBackup(ctx context.Context, req BackupRequest) (*BackupResult, error) {
	// Reset stop flag for new backup.
	e.stopped.Store(false)

	// 1. Generate unique backup ID.
	backupID := uuid.New().String()

	// 2. Create backup job record.
	now := time.Now().UTC()
	levelStr := req.Level.String()
	job := model.BackupJob{
		BackupID:    backupID,
		Level:       levelStr,
		ClientID:    req.ClientID,
		InitiatedBy: req.InitiatedBy,
		StartedAt:   now,
		Status:      model.JobRunning,
	}
	if err := e.repo.CreateJob(ctx, job); err != nil {
		return nil, fmt.Errorf("creating backup job: %w", err)
	}

	// Helper to finalize job on error.
	finishJob := func(status model.JobStatus, result *BackupResult, errMsg string) (*BackupResult, error) {
		finishedAt := time.Now().UTC()
		update := db.JobUpdate{
			Status:     &status,
			FinishedAt: &finishedAt,
		}
		if result != nil {
			update.FileCount = &result.FilesProcessed
			update.BytesNew = &result.BytesNew
			update.FilesDeduped = &result.FilesDeduped
		}
		if errMsg != "" {
			update.ErrorMessage = &errMsg
		}
		_ = e.repo.UpdateJob(ctx, backupID, update)
		if result != nil {
			result.Status = status
		}
		return result, nil
	}

	// 3. Scan files.
	slog.Info("starting file scan", "backup_id", backupID, "level", levelStr)

	// Resolve effective include paths and exclude patterns.
	// DB-stored paths take priority; fall back to config if DB is empty.
	includePaths, excludePatterns := e.resolveEffectivePaths(ctx)

	files, err := Scan(ctx, includePaths, excludePatterns, e.config.MaxFileSize)
	if err != nil {
		result := &BackupResult{BackupID: backupID}
		return finishJob(model.JobFailed, result, fmt.Sprintf("scan failed: %v", err))
	}

	if e.stopped.Load() {
		result := &BackupResult{BackupID: backupID}
		return finishJob(model.JobStopped, result, "stopped before manifest build")
	}

	slog.Info("scan complete", "backup_id", backupID, "files_found", len(files))

	// 4. Build manifest.
	manifest, err := BuildManifest(files)
	if err != nil {
		result := &BackupResult{BackupID: backupID}
		return finishJob(model.JobFailed, result, fmt.Sprintf("manifest build failed: %v", err))
	}

	if e.stopped.Load() {
		result := &BackupResult{BackupID: backupID}
		return finishJob(model.JobStopped, result, "stopped before manifest exchange")
	}

	// 5. Exchange manifest with server to determine which files need uploading.
	diff, err := e.server.ExchangeManifest(ctx, manifest)
	if err != nil {
		result := &BackupResult{BackupID: backupID}
		return finishJob(model.JobFailed, result, fmt.Sprintf("manifest exchange failed: %v", err))
	}

	slog.Info("manifest exchange complete",
		"backup_id", backupID,
		"needed_hashes", len(diff.NeededHashes),
		"dedup_count", diff.DedupCount,
	)

	// Build a pathâ†’scannedFile map for reading content and metadata.
	pathToScanned := make(map[string]*ScannedFile, len(files))
	for i := range files {
		pathToScanned[files[i].Path] = &files[i]
	}

	// Build needed hash set for quick lookup.
	neededSet := make(map[string]bool, len(diff.NeededHashes))
	for _, h := range diff.NeededHashes {
		neededSet[h] = true
	}

	result := &BackupResult{
		BackupID: backupID,
	}

	// 6. Upload needed files (one upload per unique hash).
	uploadedHashes := make(map[string]bool, len(diff.NeededHashes))
	type encMeta struct {
		wrappedDEK []byte
		nonce      []byte
	}
	hashEncryption := make(map[string]encMeta, len(diff.NeededHashes))
	for _, hash := range diff.NeededHashes {
		if e.stopped.Load() {
			return finishJob(model.JobStopped, result, "stopped during upload")
		}

		// Find a scanned file with this hash.
		var sf *ScannedFile
		for _, entry := range manifest {
			if entry.Blake3Hash == hash {
				sf = pathToScanned[entry.FilePath]
				break
			}
		}
		if sf == nil {
			slog.Warn("no file found for needed hash", "hash", hash)
			continue
		}

		// Read file content.
		data, err := os.ReadFile(sf.Path)
		if err != nil {
			slog.Warn("failed to read file for upload", "path", sf.Path, "error", err)
			continue
		}

		var wrappedDEK, nonce []byte
		uploadData := data

		// Encrypt if enabled.
		if e.config.EncryptionOn && e.encryptor != nil {
			ciphertext, wDEK, n, err := e.encryptor.Encrypt(data, e.config.MasterKey)
			if err != nil {
				slog.Warn("failed to encrypt file", "path", sf.Path, "error", err)
				continue
			}
			uploadData = ciphertext
			wrappedDEK = wDEK
			nonce = n
		}

		// Store encryption metadata for use in step 7.
		hashEncryption[hash] = encMeta{wrappedDEK: wrappedDEK, nonce: nonce}

		// Build backup entry for this file.
		entry := buildBackupEntry(backupID, hash, sf, wrappedDEK, nonce)

		// Upload to server.
		if err := e.server.UploadFile(ctx, hash, uploadData, wrappedDEK, nonce, entry); err != nil {
			slog.Warn("failed to upload file", "path", sf.Path, "error", err)
			continue
		}

		uploadedHashes[hash] = true
		result.BytesNew += int64(len(data))
	}

	// 7. Insert backup entries for all manifest files.
	// Files whose hash was uploaded are "new"; files whose hash already existed on server are "deduped".
	// Within-backup dedup: if multiple files share the same hash and we uploaded it,
	// only the first counts as "new bytes" â€” the rest are intra-backup dedup.
	seenUploaded := make(map[string]bool, len(diff.NeededHashes))
	for _, mEntry := range manifest {
		if e.stopped.Load() {
			return finishJob(model.JobStopped, result, "stopped during entry insertion")
		}

		sf := pathToScanned[mEntry.FilePath]
		if sf == nil {
			continue
		}

		// Look up encryption metadata from the upload step.
		var wrappedDEK, nonce []byte
		if em, ok := hashEncryption[mEntry.Blake3Hash]; ok {
			wrappedDEK = em.wrappedDEK
			nonce = em.nonce
		}

		backupEntry := buildBackupEntry(backupID, mEntry.Blake3Hash, sf, wrappedDEK, nonce)
		if err := e.repo.InsertBackupEntry(ctx, backupEntry); err != nil {
			slog.Warn("failed to insert backup entry", "path", sf.Path, "error", err)
			continue
		}

		result.FilesProcessed++
		if !neededSet[mEntry.Blake3Hash] {
			// Hash already on server â€” server-side dedup.
			result.FilesDeduped++
		} else if seenUploaded[mEntry.Blake3Hash] {
			// Hash was needed but we already counted one file for this hash â€” intra-backup dedup.
			result.FilesDeduped++
		} else {
			seenUploaded[mEntry.Blake3Hash] = true
		}
	}

	// 8. Sync local database to server.
	if e.config.DatabasePath != "" {
		if err := e.server.SyncDatabase(ctx, e.config.DatabasePath); err != nil {
			slog.Warn("database sync failed", "error", err)
			// Non-fatal: backup data is already on server.
		}
	}

	// 9. Update job with completion status.
	return finishJob(model.JobCompleted, result, "")
}

// Stop signals the engine to stop the current backup gracefully.
func (e *BackupEngine) Stop(ctx context.Context) error {
	e.stopped.Store(true)
	return nil
}

// resolveEffectivePaths returns the include paths and exclude patterns to use for scanning.
// DB-stored paths take priority over config. If the DB has no include paths, fall back to config.
// Exclude patterns from DB are merged with config-based patterns.
func (e *BackupEngine) resolveEffectivePaths(ctx context.Context) ([]string, []string) {
	includePaths := e.config.IncludePaths
	excludePatterns := e.config.ExcludePatterns

	// Try loading DB-stored paths.
	if e.repo != nil {
		dbIncludes, err := e.repo.ListIncludePaths(ctx)
		if err == nil && len(dbIncludes) > 0 {
			includePaths = dbIncludes
		}

		dbExcludes, err := e.repo.ListExcludePatterns(ctx)
		if err == nil && len(dbExcludes) > 0 {
			// Merge: DB excludes take priority, append config excludes that aren't duplicates.
			seen := make(map[string]bool, len(dbExcludes))
			for _, p := range dbExcludes {
				seen[p] = true
			}
			merged := append([]string{}, dbExcludes...)
			for _, p := range e.config.ExcludePatterns {
				if !seen[p] {
					merged = append(merged, p)
				}
			}
			excludePatterns = merged
		}
	}

	return includePaths, excludePatterns
}

// buildBackupEntry creates a model.BackupEntry from a scanned file.
func buildBackupEntry(backupID, hash string, sf *ScannedFile, wrappedDEK, nonce []byte) model.BackupEntry {
	return model.BackupEntry{
		BackupID:      backupID,
		Blake3Hash:    hash,
		FileName:      sf.Name,
		FilePath:      sf.Path,
		FileExt:       sf.Ext,
		FileSize:      sf.Size,
		CreatedAt:     sf.CreatedAt,
		ModifiedAt:    sf.ModifiedAt,
		AccessedAt:    sf.AccessedAt,
		Permissions:   sf.Permissions,
		Owner:         sf.Owner,
		FileGroup:     sf.FileGroup,
		Hidden:        sf.Hidden,
		Symlink:       sf.Symlink,
		SymlinkTarget: sf.SymlinkTarget,
		OS:            sf.OS,
		EncryptedDEK:  wrappedDEK,
		Nonce:         nonce,
		BackupDate:    time.Now().UTC(),
	}
}

// LocalServerConnection implements ServerConnection using direct calls to the CAS store
// and repository, suitable for testing and local (single-binary) mode.
type LocalServerConnection struct {
	StorageDir string
	Repo       db.Repository
}

// ExchangeManifest compares the client manifest against hashes that exist in the local CAS store.
func (l *LocalServerConnection) ExchangeManifest(ctx context.Context, manifest []model.ManifestEntry) (ManifestDiff, error) {
	serverHashes := make(map[string]bool)

	for _, entry := range manifest {
		// Check if the hash file exists in the CAS directory structure.
		path := filepath.Join(l.StorageDir, entry.Blake3Hash[:2], entry.Blake3Hash)
		if _, err := os.Stat(path); err == nil {
			serverHashes[entry.Blake3Hash] = true
		}
	}

	return ComputeDiff(manifest, serverHashes), nil
}

// UploadFile stores the file data in the local CAS directory.
func (l *LocalServerConnection) UploadFile(ctx context.Context, hash string, data []byte, wrappedDEK []byte, nonce []byte, entry model.BackupEntry) error {
	dir := filepath.Join(l.StorageDir, hash[:2])
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating storage directory: %w", err)
	}

	path := filepath.Join(dir, hash)

	// Write atomically via temp file.
	tmp, err := os.CreateTemp(dir, ".upload-tmp-*")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("writing data: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("closing temp file: %w", err)
	}

	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("renaming temp file: %w", err)
	}

	return nil
}

// SyncDatabase is a no-op for local connections (DB is already local).
func (l *LocalServerConnection) SyncDatabase(ctx context.Context, dbPath string) error {
	// In local mode, client and server share the same database â€” nothing to sync.
	return nil
}

// Ensure interfaces are satisfied at compile time.
var _ Engine = (*BackupEngine)(nil)
var _ ServerConnection = (*LocalServerConnection)(nil)
