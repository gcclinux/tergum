package backup

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gcclinux/tergum/internal/crypto"
	"github.com/gcclinux/tergum/internal/db"
	"github.com/gcclinux/tergum/internal/model"
	"github.com/google/uuid"
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

	// Monitor stop file if DatabasePath is set.
	if e.config.DatabasePath != "" {
		stopCtx, stopCancel := context.WithCancel(ctx)
		defer stopCancel()
		go func() {
			stopFile := filepath.Join(filepath.Dir(e.config.DatabasePath), "stop")
			ticker := time.NewTicker(500 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-stopCtx.Done():
					return
				case <-ticker.C:
					if _, err := os.Stat(stopFile); err == nil {
						slog.Info("stop file detected, stopping backup engine")
						e.stopped.Store(true)
						_ = os.Remove(stopFile)
						return
					}
				}
			}
		}()
	}

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
			if result.FilesProcessed > 0 || status == model.JobCompleted {
				update.FileCount = &result.FilesProcessed
			}
			update.BytesNew = &result.BytesNew
			update.FilesDeduped = &result.FilesDeduped
		}
		if errMsg != "" {
			update.ErrorMessage = &errMsg
		}
		_ = e.repo.UpdateJob(ctx, backupID, update)
		if e.config.DatabasePath != "" {
			if cp, ok := e.repo.(interface {
				Checkpoint(context.Context) error
			}); ok {
				if err := cp.Checkpoint(ctx); err != nil {
					slog.Warn("database checkpoint failed before sync", "error", err)
				}
			}
			// Retry SyncDatabase up to 5 times with exponential backoff to ensure
			// the server gets the final job status. A failed sync leaves the
			// server's copy of the client DB showing "running" indefinitely.
			// On macOS, FD exhaustion from the file watcher can persist for a
			// while; the extended retry gives time for FDs to be released.
			var syncErr error
			syncBackoff := 2 * time.Second
			for attempt := 0; attempt < 5; attempt++ {
				// Use a fresh context if the original was cancelled (e.g. during shutdown).
				syncCtx := ctx
				if syncCtx.Err() != nil {
					var cancel context.CancelFunc
					syncCtx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
					defer cancel()
				}
				syncErr = e.server.SyncDatabase(syncCtx, e.config.DatabasePath)
				if syncErr == nil {
					break
				}
				slog.Warn("database sync failed during job finalization (retrying)",
					"status", status, "attempt", attempt+1, "error", syncErr)
				time.Sleep(syncBackoff)
				syncBackoff *= 2
				if syncBackoff > 30*time.Second {
					syncBackoff = 30 * time.Second
				}
			}
			if syncErr != nil {
				slog.Error("database sync failed after retries — server may show stale job status",
					"backup_id", backupID, "status", status, "error", syncErr)
			}
		}
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

	// Update job in DB with the scanned file count and total size immediately.
	var totalBytes int64
	for i := range files {
		totalBytes += files[i].Size
	}
	scannedCount := int64(len(files))
	_ = e.repo.UpdateJob(ctx, backupID, db.JobUpdate{
		FileCount:  &scannedCount,
		BytesTotal: &totalBytes,
	})

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

	// Build a path→scannedFile map for reading content and metadata.
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

	var lastUpdate time.Time
	updateProgress := func() {
		if time.Since(lastUpdate) < 1*time.Second {
			return
		}
		lastUpdate = time.Now()
		update := db.JobUpdate{
			BytesNew:     &result.BytesNew,
			FilesDeduped: &result.FilesDeduped,
		}
		// Only write FileCount when files have actually been processed.
		// Before any uploads complete, FilesProcessed is 0 and would overwrite
		// the scan count set earlier, causing the UI to show "0 files".
		if result.FilesProcessed > 0 {
			update.FileCount = &result.FilesProcessed
		}
		_ = e.repo.UpdateJob(ctx, backupID, update)
	}

	// 6. Upload needed files (one upload per unique hash).
	// Build a map of hash → number of files sharing that hash in the manifest,
	// so we can report accurate file-level progress during uploads.
	hashFileCount := make(map[string]int64, len(diff.NeededHashes))
	for _, mEntry := range manifest {
		if neededSet[mEntry.Blake3Hash] {
			hashFileCount[mEntry.Blake3Hash]++
		}
	}

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
		data, err := readFileWithRetry(ctx, sf.Path)
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
		result.FilesProcessed += hashFileCount[hash]
		updateProgress()
	}

	// 7. Insert backup entries for all manifest files.
	// Files whose hash was uploaded are "new"; files whose hash already existed on server are "deduped".
	// Within-backup dedup: if multiple files share the same hash and we uploaded it,
	// only the first counts as "new bytes" — the rest are intra-backup dedup.
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

		if !neededSet[mEntry.Blake3Hash] {
			// Hash already on server — server-side dedup (not counted in step 6).
			result.FilesProcessed++
			result.FilesDeduped++
		} else if seenUploaded[mEntry.Blake3Hash] {
			// Hash was needed but we already counted one file for this hash — intra-backup dedup.
			result.FilesDeduped++
		} else {
			seenUploaded[mEntry.Blake3Hash] = true
		}
		updateProgress()
	}

	// 8. Update job with completion status and sync database.
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
	// In local mode, client and server share the same database — nothing to sync.
	return nil
}

// readFileWithRetry reads a file, retrying with exponential backoff if the
// error is "too many open files" (EMFILE/ENFILE). This handles transient FD
// pressure caused by the file watcher holding many directory handles open.
func readFileWithRetry(ctx context.Context, path string) ([]byte, error) {
	const maxRetries = 5
	backoff := 500 * time.Millisecond

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		data, err := os.ReadFile(path)
		if err == nil {
			return data, nil
		}

		if !isEMFILE(err) {
			return nil, err
		}

		lastErr = err
		if attempt < maxRetries {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
			backoff *= 2
			if backoff > 10*time.Second {
				backoff = 10 * time.Second
			}
		}
	}
	return nil, lastErr
}

// isEMFILE returns true if the error is caused by file descriptor exhaustion
// (EMFILE "too many open files" or ENFILE "too many open files in system").
func isEMFILE(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "too many open files")
}

// Ensure interfaces are satisfied at compile time.
var _ Engine = (*BackupEngine)(nil)
var _ ServerConnection = (*LocalServerConnection)(nil)
