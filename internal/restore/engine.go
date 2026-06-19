// Package restore implements the Restore Engine for Tergum.
// It locates, downloads, decrypts, and restores files with original metadata.
package restore

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/ricardopadilha/tergum/internal/crypto"
	"github.com/ricardopadilha/tergum/internal/db"
	"github.com/ricardopadilha/tergum/internal/model"
)

// Engine defines the restore engine interface.
type Engine interface {
	// RestoreFile restores a single file by hash to dest.
	RestoreFile(ctx context.Context, hash string, dest string) error
	// RestoreBatch restores multiple files in parallel.
	RestoreBatch(ctx context.Context, entries []RestoreEntry, concurrency int) (*RestoreResult, error)
	// Search queries the database for matching files.
	Search(ctx context.Context, query SearchQuery) ([]model.BackupEntry, error)
}

// RestoreEntry describes a file to restore.
type RestoreEntry struct {
	Hash        string
	FileName    string
	Destination string
	BackupID    string
	Metadata    *model.BackupEntry // full metadata for restoration
}

// RestoreResult contains the outcome of a batch restore.
type RestoreResult struct {
	Restored int64
	Failed   int64
	Errors   []error
}

// SearchQuery specifies criteria for searching backup entries.
type SearchQuery struct {
	Name    string // match file name (exact or LIKE)
	Path    string // match file path (LIKE pattern)
	Pattern string // glob pattern for file name
}

// DataSource abstracts file retrieval (e.g., from CAS directory or via gRPC).
type DataSource interface {
	DownloadFile(ctx context.Context, hash string) ([]byte, error)
}

// RestoreEngine implements the Engine interface.
type RestoreEngine struct {
	source    DataSource
	repo      db.Repository
	encryptor *crypto.AESEncryptor
	masterKey []byte
}

// NewRestoreEngine creates a new RestoreEngine instance.
func NewRestoreEngine(source DataSource, repo db.Repository, encryptor *crypto.AESEncryptor, masterKey []byte) *RestoreEngine {
	return &RestoreEngine{
		source:    source,
		repo:      repo,
		encryptor: encryptor,
		masterKey: masterKey,
	}
}

// Search queries the local DB for matching entries by name, path, or glob pattern.
func (e *RestoreEngine) Search(ctx context.Context, query SearchQuery) ([]model.BackupEntry, error) {
	var results []model.BackupEntry

	if query.Path != "" {
		entries, err := e.repo.FindByPath(ctx, query.Path)
		if err != nil {
			return nil, fmt.Errorf("search by path: %w", err)
		}
		results = append(results, entries...)
	}

	if query.Name != "" {
		// Use FindByPath with a name-matching LIKE pattern.
		// We search for entries where the file_path ends with the given name.
		pattern := "%" + query.Name
		entries, err := e.repo.FindByPath(ctx, pattern)
		if err != nil {
			return nil, fmt.Errorf("search by name: %w", err)
		}
		// Filter to exact file name matches.
		for _, entry := range entries {
			if entry.FileName == query.Name {
				results = append(results, entry)
			}
		}
	}

	if query.Pattern != "" {
		// Use a broad LIKE query then filter with filepath.Match.
		// Convert the glob pattern to a simple LIKE prefix.
		likePattern := "%" + strings.TrimLeft(query.Pattern, "*")
		entries, err := e.repo.FindByPath(ctx, likePattern)
		if err != nil {
			return nil, fmt.Errorf("search by pattern: %w", err)
		}
		for _, entry := range entries {
			matched, err := filepath.Match(query.Pattern, entry.FileName)
			if err != nil {
				continue
			}
			if matched {
				results = append(results, entry)
			}
		}
	}

	// Deduplicate results by ID.
	seen := make(map[int64]bool, len(results))
	deduped := make([]model.BackupEntry, 0, len(results))
	for _, entry := range results {
		if !seen[entry.ID] {
			seen[entry.ID] = true
			deduped = append(deduped, entry)
		}
	}

	return deduped, nil
}

// RestoreFile restores a single file: download → decrypt (if encrypted) → verify BLAKE3 → write → apply metadata.
func (e *RestoreEngine) RestoreFile(ctx context.Context, hash string, dest string) error {
	// Download file data.
	data, err := e.source.DownloadFile(ctx, hash)
	if err != nil {
		return fmt.Errorf("download file %s: %w", hash, err)
	}

	// Look up the backup entry for metadata.
	entries, err := e.repo.FindByHash(ctx, hash)
	if err != nil {
		return fmt.Errorf("find entry for hash %s: %w", hash, err)
	}

	var metadata *model.BackupEntry
	if len(entries) > 0 {
		metadata = &entries[0]
	}

	// Decrypt if the entry has encryption metadata.
	fileData := data
	if metadata != nil && len(metadata.EncryptedDEK) > 0 && len(metadata.Nonce) > 0 {
		if e.encryptor == nil || len(e.masterKey) == 0 {
			return fmt.Errorf("file is encrypted but no master key available")
		}
		decrypted, err := e.encryptor.Decrypt(data, metadata.EncryptedDEK, metadata.Nonce, e.masterKey)
		if err != nil {
			return fmt.Errorf("decrypt file %s: %w", hash, err)
		}
		fileData = decrypted
	}

	// Verify BLAKE3 hash of the decrypted content.
	actualHash := crypto.HashBytes(fileData)
	if actualHash != hash {
		return fmt.Errorf("BLAKE3 verification failed: expected %s, got %s", hash, actualHash)
	}

	// Ensure destination directory exists.
	destDir := filepath.Dir(dest)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("creating destination directory: %w", err)
	}

	// Handle symlink restoration.
	if metadata != nil && metadata.Symlink && metadata.SymlinkTarget != "" {
		// Remove existing file/symlink at dest if present.
		os.Remove(dest)
		if err := os.Symlink(metadata.SymlinkTarget, dest); err != nil {
			return fmt.Errorf("creating symlink: %w", err)
		}
		e.recordRestore(ctx, hash, metadata, dest, true)
		return nil
	}

	// Write file content.
	perm := fs.FileMode(0o644)
	if metadata != nil && metadata.Permissions != nil {
		perm = fs.FileMode(*metadata.Permissions)
	}
	if err := os.WriteFile(dest, fileData, perm); err != nil {
		return fmt.Errorf("writing file: %w", err)
	}

	// Apply metadata.
	if metadata != nil {
		applyMetadata(dest, metadata)
	}

	// Record restore in history.
	e.recordRestore(ctx, hash, metadata, dest, true)
	return nil
}

// RestoreBatch restores multiple files in parallel using a worker pool.
func (e *RestoreEngine) RestoreBatch(ctx context.Context, entries []RestoreEntry, concurrency int) (*RestoreResult, error) {
	if concurrency <= 0 {
		concurrency = 4
	}

	result := &RestoreResult{}
	var mu sync.Mutex

	// Create work channel.
	work := make(chan RestoreEntry, len(entries))
	for _, entry := range entries {
		work <- entry
	}
	close(work)

	// Spawn workers.
	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for entry := range work {
				select {
				case <-ctx.Done():
					mu.Lock()
					result.Failed++
					result.Errors = append(result.Errors, ctx.Err())
					mu.Unlock()
					return
				default:
				}

				err := e.restoreEntry(ctx, entry)
				mu.Lock()
				if err != nil {
					result.Failed++
					result.Errors = append(result.Errors, fmt.Errorf("%s: %w", entry.FileName, err))
				} else {
					result.Restored++
				}
				mu.Unlock()
			}
		}()
	}

	wg.Wait()
	return result, nil
}

// restoreEntry restores a single entry within a batch operation.
func (e *RestoreEngine) restoreEntry(ctx context.Context, entry RestoreEntry) error {
	// Download file data.
	data, err := e.source.DownloadFile(ctx, entry.Hash)
	if err != nil {
		e.recordRestoreFromEntry(ctx, entry, false)
		return fmt.Errorf("download file %s: %w", entry.Hash, err)
	}

	// Use metadata from entry if available; otherwise look up from DB.
	metadata := entry.Metadata
	if metadata == nil {
		entries, err := e.repo.FindByHash(ctx, entry.Hash)
		if err == nil && len(entries) > 0 {
			metadata = &entries[0]
		}
	}

	// Decrypt if encrypted.
	fileData := data
	if metadata != nil && len(metadata.EncryptedDEK) > 0 && len(metadata.Nonce) > 0 {
		if e.encryptor == nil || len(e.masterKey) == 0 {
			e.recordRestoreFromEntry(ctx, entry, false)
			return fmt.Errorf("file is encrypted but no master key available")
		}
		decrypted, err := e.encryptor.Decrypt(data, metadata.EncryptedDEK, metadata.Nonce, e.masterKey)
		if err != nil {
			e.recordRestoreFromEntry(ctx, entry, false)
			return fmt.Errorf("decrypt file %s: %w", entry.Hash, err)
		}
		fileData = decrypted
	}

	// Verify BLAKE3 hash.
	actualHash := crypto.HashBytes(fileData)
	if actualHash != entry.Hash {
		e.recordRestoreFromEntry(ctx, entry, false)
		return fmt.Errorf("BLAKE3 verification failed: expected %s, got %s", entry.Hash, actualHash)
	}

	// Ensure destination directory exists.
	destDir := filepath.Dir(entry.Destination)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		e.recordRestoreFromEntry(ctx, entry, false)
		return fmt.Errorf("creating destination directory: %w", err)
	}

	// Handle symlink restoration.
	if metadata != nil && metadata.Symlink && metadata.SymlinkTarget != "" {
		os.Remove(entry.Destination)
		if err := os.Symlink(metadata.SymlinkTarget, entry.Destination); err != nil {
			e.recordRestoreFromEntry(ctx, entry, false)
			return fmt.Errorf("creating symlink: %w", err)
		}
		e.recordRestoreFromEntry(ctx, entry, true)
		return nil
	}

	// Write file.
	perm := fs.FileMode(0o644)
	if metadata != nil && metadata.Permissions != nil {
		perm = fs.FileMode(*metadata.Permissions)
	}
	if err := os.WriteFile(entry.Destination, fileData, perm); err != nil {
		e.recordRestoreFromEntry(ctx, entry, false)
		return fmt.Errorf("writing file: %w", err)
	}

	// Apply metadata.
	if metadata != nil {
		applyMetadata(entry.Destination, metadata)
	}

	// Record successful restore.
	e.recordRestoreFromEntry(ctx, entry, true)
	return nil
}

// applyMetadata applies file metadata (permissions, timestamps, hidden attribute).
func applyMetadata(path string, metadata *model.BackupEntry) {
	// Apply permissions.
	if metadata.Permissions != nil {
		_ = os.Chmod(path, fs.FileMode(*metadata.Permissions))
	}

	// Apply timestamps (modified and accessed).
	if metadata.ModifiedAt != nil || metadata.AccessedAt != nil {
		atime := time.Now()
		mtime := time.Now()
		if metadata.AccessedAt != nil {
			atime = *metadata.AccessedAt
		}
		if metadata.ModifiedAt != nil {
			mtime = *metadata.ModifiedAt
		}
		_ = os.Chtimes(path, atime, mtime)
	}

	// Apply hidden attribute on Windows.
	if runtime.GOOS == "windows" && metadata.Hidden {
		setHiddenAttribute(path)
	}
}

// recordRestore records a restore event in the restore_history table.
func (e *RestoreEngine) recordRestore(ctx context.Context, hash string, metadata *model.BackupEntry, dest string, success bool) {
	record := db.RestoreRecord{
		Blake3Hash: hash,
		RestoredTo: dest,
		RestoredBy: "cli",
		Success:    success,
	}
	if metadata != nil {
		record.FileName = metadata.FileName
		record.SourceBackup = metadata.BackupID
	} else {
		record.FileName = filepath.Base(dest)
		record.SourceBackup = "unknown"
	}
	_ = e.repo.RecordRestore(ctx, record)
}

// recordRestoreFromEntry records a restore event from a RestoreEntry.
func (e *RestoreEngine) recordRestoreFromEntry(ctx context.Context, entry RestoreEntry, success bool) {
	record := db.RestoreRecord{
		Blake3Hash:   entry.Hash,
		FileName:     entry.FileName,
		SourceBackup: entry.BackupID,
		RestoredTo:   entry.Destination,
		RestoredBy:   "cli",
		Success:      success,
	}
	if record.SourceBackup == "" {
		record.SourceBackup = "unknown"
	}
	if record.FileName == "" {
		record.FileName = filepath.Base(entry.Destination)
	}
	_ = e.repo.RecordRestore(ctx, record)
}

// LocalDataSource reads files from a local CAS directory (for testing and local mode).
type LocalDataSource struct {
	StorageDir string
}

// DownloadFile reads file content from the local CAS directory.
func (l *LocalDataSource) DownloadFile(ctx context.Context, hash string) ([]byte, error) {
	path := filepath.Join(l.StorageDir, hash[:2], hash)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("hash %s not found in store", hash)
		}
		return nil, fmt.Errorf("reading file: %w", err)
	}
	return data, nil
}

// Ensure interfaces are satisfied at compile time.
var _ Engine = (*RestoreEngine)(nil)
var _ DataSource = (*LocalDataSource)(nil)
