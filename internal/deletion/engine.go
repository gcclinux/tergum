// Package deletion implements granular delete operations for backup entries.
package deletion

import (
	"context"
	"fmt"
	"strings"

	"github.com/ricardopadilha/tergum/internal/model"
	"github.com/ricardopadilha/tergum/internal/storage"
)

// DeleteResult summarizes what a deletion operation affected.
type DeleteResult struct {
	EntriesDeleted int64 // number of backup entries removed
	BytesFreed     int64 // total size of entries removed
	FilesRemoved   int64 // physical CAS files removed (refcount reached 0)
	JobsRemoved    int64 // backup_jobs records removed (became empty)
}

// Filter specifies which backup entries to target.
type Filter struct {
	BackupID   string // delete by backup_id
	FolderPath string // delete by path prefix
	FilePath   string // delete single file
	AllBackups bool   // across all backup sets
}

// Repository defines the subset of db.Repository needed by the deletion engine.
type Repository interface {
	// QueryEntries returns entries matching the filter without deleting them.
	QueryEntries(ctx context.Context, filter Filter) ([]model.BackupEntry, error)
	// DeleteEntries deletes entries matching the filter, returns count deleted.
	DeleteEntries(ctx context.Context, filter Filter) (int64, error)
	// CountHashReferences returns how many entries reference the given hash.
	CountHashReferences(ctx context.Context, hash string) (int64, error)
	// DeleteOrphanJobs removes backup_jobs records that have no remaining entries.
	DeleteOrphanJobs(ctx context.Context) (int64, error)
}

// Engine defines the deletion operations.
type Engine interface {
	// DeleteByBackupID deletes all entries in a specific backup set.
	DeleteByBackupID(ctx context.Context, backupID string, dryRun bool) (*DeleteResult, error)
	// DeleteByFolder deletes all entries whose file_path starts with the given prefix.
	DeleteByFolder(ctx context.Context, folderPath string, backupID string, allBackups bool, dryRun bool) (*DeleteResult, error)
	// DeleteByFile deletes a specific file entry.
	DeleteByFile(ctx context.Context, filePath string, backupID string, allBackups bool, dryRun bool) (*DeleteResult, error)
	// DeleteAll deletes all backups (dangerous, requires confirmation).
	DeleteAll(ctx context.Context, dryRun bool) (*DeleteResult, error)
}

// DeletionEngine implements Engine using a Repository and a storage.Store.
type DeletionEngine struct {
	repo  Repository
	store storage.Store
}

// New creates a new DeletionEngine.
func New(repo Repository, store storage.Store) *DeletionEngine {
	return &DeletionEngine{
		repo:  repo,
		store: store,
	}
}

// DeleteByBackupID removes all entries belonging to the given backup ID.
func (e *DeletionEngine) DeleteByBackupID(ctx context.Context, backupID string, dryRun bool) (*DeleteResult, error) {
	if backupID == "" {
		return nil, fmt.Errorf("backup_id must not be empty")
	}
	filter := Filter{BackupID: backupID}
	return e.executeDelete(ctx, filter, dryRun)
}

// DeleteByFolder removes all entries whose file_path starts with folderPath.
// If allBackups is true, deletes across all backup sets; otherwise scoped to backupID.
func (e *DeletionEngine) DeleteByFolder(ctx context.Context, folderPath string, backupID string, allBackups bool, dryRun bool) (*DeleteResult, error) {
	if folderPath == "" {
		return nil, fmt.Errorf("folder path must not be empty")
	}
	if !allBackups && backupID == "" {
		return nil, fmt.Errorf("backup_id required when all_backups is false")
	}
	filter := Filter{
		FolderPath: folderPath,
		AllBackups: allBackups,
	}
	if !allBackups {
		filter.BackupID = backupID
	}
	return e.executeDelete(ctx, filter, dryRun)
}

// DeleteByFile removes a specific file entry.
// If allBackups is true, deletes the file across all backup sets; otherwise scoped to backupID.
func (e *DeletionEngine) DeleteByFile(ctx context.Context, filePath string, backupID string, allBackups bool, dryRun bool) (*DeleteResult, error) {
	if filePath == "" {
		return nil, fmt.Errorf("file path must not be empty")
	}
	if !allBackups && backupID == "" {
		return nil, fmt.Errorf("backup_id required when all_backups is false")
	}
	filter := Filter{
		FilePath:   filePath,
		AllBackups: allBackups,
	}
	if !allBackups {
		filter.BackupID = backupID
	}
	return e.executeDelete(ctx, filter, dryRun)
}

// DeleteAll removes all backup entries across all backup sets.
func (e *DeletionEngine) DeleteAll(ctx context.Context, dryRun bool) (*DeleteResult, error) {
	filter := Filter{AllBackups: true}
	return e.executeDelete(ctx, filter, dryRun)
}

// executeDelete is the shared implementation for all delete operations.
func (e *DeletionEngine) executeDelete(ctx context.Context, filter Filter, dryRun bool) (*DeleteResult, error) {
	// Step 1: Query matching entries to get hashes and sizes.
	entries, err := e.repo.QueryEntries(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("querying entries: %w", err)
	}

	result := &DeleteResult{
		EntriesDeleted: int64(len(entries)),
	}

	// Compute bytes freed and collect unique hashes.
	uniqueHashes := make(map[string]struct{})
	for _, entry := range entries {
		result.BytesFreed += entry.FileSize
		uniqueHashes[entry.Blake3Hash] = struct{}{}
	}

	if dryRun {
		// In dry-run: estimate physical files that would be removed.
		for hash := range uniqueHashes {
			refCount, err := e.repo.CountHashReferences(ctx, hash)
			if err != nil {
				return nil, fmt.Errorf("counting references for %s: %w", hash, err)
			}
			// Count how many of the entries we're deleting reference this hash.
			deleteCount := countHashInEntries(entries, hash)
			if refCount-int64(deleteCount) <= 0 {
				result.FilesRemoved++
			}
		}
		// Estimate orphan jobs.
		result.JobsRemoved = e.estimateOrphanJobs(ctx, entries)
		return result, nil
	}

	// Step 2: Delete entries from DB.
	_, err = e.repo.DeleteEntries(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("deleting entries: %w", err)
	}

	// Step 3: For each unique hash, check refcount and delete physical file if 0.
	for hash := range uniqueHashes {
		refCount, err := e.repo.CountHashReferences(ctx, hash)
		if err != nil {
			return nil, fmt.Errorf("counting references for %s: %w", hash, err)
		}
		if refCount == 0 {
			if err := e.store.Delete(ctx, hash); err != nil {
				// If file not found, that's fine (already cleaned up).
				if !strings.Contains(err.Error(), "not found") {
					return nil, fmt.Errorf("deleting storage file %s: %w", hash, err)
				}
			}
			result.FilesRemoved++
		}
	}

	// Step 4: Remove orphan backup_jobs records.
	jobsRemoved, err := e.repo.DeleteOrphanJobs(ctx)
	if err != nil {
		return nil, fmt.Errorf("deleting orphan jobs: %w", err)
	}
	result.JobsRemoved = jobsRemoved

	return result, nil
}

// countHashInEntries counts how many entries in the slice have the given hash.
func countHashInEntries(entries []model.BackupEntry, hash string) int {
	count := 0
	for _, e := range entries {
		if e.Blake3Hash == hash {
			count++
		}
	}
	return count
}

// estimateOrphanJobs estimates how many jobs would become empty after deletion.
func (e *DeletionEngine) estimateOrphanJobs(ctx context.Context, entries []model.BackupEntry) int64 {
	// Collect distinct backup IDs from the entries to delete.
	jobIDs := make(map[string]struct{})
	for _, entry := range entries {
		jobIDs[entry.BackupID] = struct{}{}
	}

	// For each job, count total entries vs entries being deleted.
	var orphans int64
	for jobID := range jobIDs {
		allForJob, err := e.repo.QueryEntries(ctx, Filter{BackupID: jobID})
		if err != nil {
			continue
		}
		// Count how many are in our deletion set.
		deleteCount := 0
		for _, entry := range entries {
			if entry.BackupID == jobID {
				deleteCount++
			}
		}
		if int64(deleteCount) >= int64(len(allForJob)) {
			orphans++
		}
	}
	return orphans
}
