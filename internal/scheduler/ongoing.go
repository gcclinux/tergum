// Package scheduler provides cron-based scheduling and ongoing backup orchestration.
package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gcclinux/tergum/internal/backup"
	"github.com/gcclinux/tergum/internal/crypto"
	"github.com/gcclinux/tergum/internal/db"
	"github.com/gcclinux/tergum/internal/model"
	"github.com/gcclinux/tergum/internal/watcher"
	"github.com/google/uuid"
)

// OngoingBackup wires the file watcher's StableFiles channel to the backup engine,
// batching uploads into logical jobs every batchInterval.
type OngoingBackup struct {
	watcher       watcher.Watcher
	server        backup.ServerConnection
	repo          db.Repository
	encryptor     *crypto.AESEncryptor
	masterKey     []byte
	batchInterval time.Duration
	databasePath  string

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu           sync.Mutex
	currentBatch []watcher.StableFile
}

// OngoingConfig holds configuration for the ongoing backup mode.
type OngoingConfig struct {
	Watcher       watcher.Watcher
	Server        backup.ServerConnection
	Repo          db.Repository
	Encryptor     *crypto.AESEncryptor
	MasterKey     []byte
	BatchInterval time.Duration // default 5 minutes
	DatabasePath  string        // path to local SQLite DB for SyncDatabase after each batch
}

// NewOngoingBackup creates a new OngoingBackup instance with the given configuration.
func NewOngoingBackup(cfg OngoingConfig) *OngoingBackup {
	interval := cfg.BatchInterval
	if interval <= 0 {
		interval = 5 * time.Minute
	}

	return &OngoingBackup{
		watcher:       cfg.Watcher,
		server:        cfg.Server,
		repo:          cfg.Repo,
		encryptor:     cfg.Encryptor,
		masterKey:     cfg.MasterKey,
		batchInterval: interval,
		databasePath:  cfg.DatabasePath,
	}
}

// Start begins the ongoing backup mode. It reads from the watcher's StableFiles channel
// and batches files into backup jobs at the configured interval.
// This method blocks until the context is cancelled or Stop is called.
func (o *OngoingBackup) Start(ctx context.Context) error {
	o.ctx, o.cancel = context.WithCancel(ctx)
	o.currentBatch = make([]watcher.StableFile, 0)

	// Start the batch ticker goroutine.
	o.wg.Add(1)
	go o.batchTicker()

	// Start the file receiver goroutine.
	o.wg.Add(1)
	go o.receiveFiles()

	return nil
}

// Stop gracefully stops the ongoing backup, flushing any pending batch.
func (o *OngoingBackup) Stop() error {
	if o.cancel != nil {
		o.cancel()
	}
	o.wg.Wait()

	// Flush any remaining files in the batch.
	o.mu.Lock()
	remaining := o.currentBatch
	o.currentBatch = nil
	o.mu.Unlock()

	if len(remaining) > 0 {
		// Use a background context for the final flush since o.ctx is cancelled.
		flushCtx, flushCancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer flushCancel()
		o.processBatch(flushCtx, remaining)
	}

	return nil
}

// receiveFiles reads from the watcher's StableFiles channel and accumulates files.
func (o *OngoingBackup) receiveFiles() {
	defer o.wg.Done()

	stableFiles := o.watcher.StableFiles()
	for {
		select {
		case <-o.ctx.Done():
			return
		case sf, ok := <-stableFiles:
			if !ok {
				return
			}
			o.mu.Lock()
			o.currentBatch = append(o.currentBatch, sf)
			o.mu.Unlock()
		}
	}
}

// batchTicker fires at batchInterval to flush accumulated files.
func (o *OngoingBackup) batchTicker() {
	defer o.wg.Done()

	ticker := time.NewTicker(o.batchInterval)
	defer ticker.Stop()

	for {
		select {
		case <-o.ctx.Done():
			return
		case <-ticker.C:
			o.mu.Lock()
			batch := o.currentBatch
			o.currentBatch = make([]watcher.StableFile, 0)
			o.mu.Unlock()

			if len(batch) > 0 {
				o.processBatch(o.ctx, batch)
			}
		}
	}
}

// processBatch creates a backup job and uploads all files in the batch independently.
func (o *OngoingBackup) processBatch(ctx context.Context, batch []watcher.StableFile) {
	backupID := uuid.New().String()
	now := time.Now().UTC()

	// Create backup job record.
	job := model.BackupJob{
		BackupID:    backupID,
		Level:       model.BackupLevelOngoing.String(),
		ClientID:    "local",
		InitiatedBy: "watcher",
		StartedAt:   now,
		Status:      model.JobRunning,
	}
	if err := o.repo.CreateJob(ctx, job); err != nil {
		slog.Error("ongoing backup: failed to create job", "error", err)
		return
	}

	slog.Info("ongoing backup: processing batch",
		"backup_id", backupID,
		"file_count", len(batch),
	)

	var (
		filesProcessed int64
		bytesNew       int64
		filesDeduped   int64
		lastErr        error
	)

	var lastUpdate time.Time
	updateProgress := func() {
		if time.Since(lastUpdate) < 1*time.Second {
			return
		}
		lastUpdate = time.Now()
		_ = o.repo.UpdateJob(ctx, backupID, db.JobUpdate{
			FileCount:    &filesProcessed,
			BytesNew:     &bytesNew,
			FilesDeduped: &filesDeduped,
		})
	}

	// Process each file independently (no global cooldown).
	for _, sf := range batch {
		if ctx.Err() != nil {
			break
		}

		processed, bytes, deduped, err := o.processFile(ctx, backupID, sf)
		if err != nil {
			slog.Warn("ongoing backup: file processing failed",
				"path", sf.Path,
				"error", err,
			)
			lastErr = err
			continue
		}

		filesProcessed += processed
		bytesNew += bytes
		filesDeduped += deduped
		updateProgress()
	}

	// Update job with completion status.
	finishedAt := time.Now().UTC()
	status := model.JobCompleted
	var errMsg string
	if lastErr != nil && filesProcessed == 0 {
		status = model.JobFailed
		errMsg = lastErr.Error()
	}
	if ctx.Err() != nil {
		status = model.JobStopped
		errMsg = "stopped"
	}

	update := db.JobUpdate{
		Status:       &status,
		FinishedAt:   &finishedAt,
		FileCount:    &filesProcessed,
		BytesNew:     &bytesNew,
		FilesDeduped: &filesDeduped,
	}
	if errMsg != "" {
		update.ErrorMessage = &errMsg
	}
	if err := o.repo.UpdateJob(ctx, backupID, update); err != nil {
		slog.Error("ongoing backup: failed to update job", "backup_id", backupID, "error", err)
	}

	slog.Info("ongoing backup: batch complete",
		"backup_id", backupID,
		"status", status,
		"files_processed", filesProcessed,
		"bytes_new", bytesNew,
		"files_deduped", filesDeduped,
	)

	// Sync the local database to the server so the server's activity feed
	// and "last backup" display reflect this ongoing backup immediately.
	if o.databasePath != "" {
		if cp, ok := o.repo.(interface {
			Checkpoint(context.Context) error
		}); ok {
			if err := cp.Checkpoint(ctx); err != nil {
				slog.Warn("ongoing backup: database checkpoint failed before sync", "error", err)
			}
		}
		syncCtx := ctx
		if syncCtx.Err() != nil {
			// Use a fresh context if the batch context was cancelled (e.g. shutdown).
			var cancel context.CancelFunc
			syncCtx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
		}
		var syncErr error
		for attempt := 0; attempt < 3; attempt++ {
			syncErr = o.server.SyncDatabase(syncCtx, o.databasePath)
			if syncErr == nil {
				break
			}
			slog.Warn("ongoing backup: database sync failed (retrying)",
				"backup_id", backupID, "attempt", attempt+1, "error", syncErr)
			time.Sleep(time.Duration(attempt+1) * 2 * time.Second)
		}
		if syncErr != nil {
			slog.Error("ongoing backup: database sync failed after retries — server may show stale data",
				"backup_id", backupID, "error", syncErr)
		}
	}
}

// processFile handles a single stable file: manifest exchange, encrypt, upload, and record.
// Returns (filesProcessed, bytesNew, filesDeduped, error).
func (o *OngoingBackup) processFile(ctx context.Context, backupID string, sf watcher.StableFile) (int64, int64, int64, error) {
	// Build a single-entry manifest for this file.
	info, err := os.Stat(sf.Path)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("stat file: %w", err)
	}

	manifest := []model.ManifestEntry{
		{
			Blake3Hash: sf.Hash,
			FilePath:   sf.Path,
			FileSize:   sf.Size,
			ModifiedAt: sf.ModifiedAt.Unix(),
		},
	}

	// Exchange manifest with server to check if file is already stored.
	diff, err := o.server.ExchangeManifest(ctx, manifest)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("manifest exchange: %w", err)
	}

	// If hash already exists on server, just record the backup entry (dedup).
	if len(diff.NeededHashes) == 0 {
		entry := o.buildEntry(backupID, sf, info, nil, nil)
		if err := o.repo.InsertBackupEntry(ctx, entry); err != nil {
			return 0, 0, 0, fmt.Errorf("insert dedup entry: %w", err)
		}
		return 1, 0, 1, nil
	}

	// Read file content.
	data, err := readFileRetry(ctx, sf.Path)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("read file: %w", err)
	}

	var wrappedDEK, nonce []byte
	uploadData := data

	// Encrypt if encryptor and master key are configured.
	if o.encryptor != nil && len(o.masterKey) > 0 {
		ciphertext, wDEK, n, encErr := o.encryptor.Encrypt(data, o.masterKey)
		if encErr != nil {
			return 0, 0, 0, fmt.Errorf("encrypt: %w", encErr)
		}
		uploadData = ciphertext
		wrappedDEK = wDEK
		nonce = n
	}

	// Build backup entry.
	entry := o.buildEntry(backupID, sf, info, wrappedDEK, nonce)

	// Upload to server.
	if err := o.server.UploadFile(ctx, sf.Hash, uploadData, wrappedDEK, nonce, entry); err != nil {
		return 0, 0, 0, fmt.Errorf("upload: %w", err)
	}

	// Record in local DB.
	if err := o.repo.InsertBackupEntry(ctx, entry); err != nil {
		return 0, 0, 0, fmt.Errorf("insert entry: %w", err)
	}

	return 1, int64(len(data)), 0, nil
}

// buildEntry constructs a model.BackupEntry from a StableFile and file info.
func (o *OngoingBackup) buildEntry(backupID string, sf watcher.StableFile, info os.FileInfo, wrappedDEK, nonce []byte) model.BackupEntry {
	modTime := info.ModTime()
	name := filepath.Base(sf.Path)
	ext := filepath.Ext(sf.Path)

	return model.BackupEntry{
		BackupID:     backupID,
		Blake3Hash:   sf.Hash,
		FileName:     name,
		FilePath:     sf.Path,
		FileExt:      ext,
		FileSize:     sf.Size,
		ModifiedAt:   &modTime,
		OS:           "local",
		EncryptedDEK: wrappedDEK,
		Nonce:        nonce,
		BackupDate:   time.Now().UTC(),
	}
}

// readFileRetry reads a file, retrying with exponential backoff on "too many open files" errors.
func readFileRetry(ctx context.Context, path string) ([]byte, error) {
	const maxRetries = 5
	backoff := 500 * time.Millisecond

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		data, err := os.ReadFile(path)
		if err == nil {
			return data, nil
		}

		if !strings.Contains(err.Error(), "too many open files") {
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
