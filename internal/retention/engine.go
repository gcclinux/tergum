// Package retention implements the Retention Engine that evaluates and enforces
// retention policies, removing expired file versions while protecting the latest version.
package retention

import (
	"context"
	"path/filepath"
	"sort"
	"time"

	"github.com/gcclinux/tergum/internal/db"
	"github.com/gcclinux/tergum/internal/model"
	"github.com/gcclinux/tergum/internal/storage"
)

// Engine defines the retention engine interface.
type Engine interface {
	// Evaluate runs retention evaluation across all file versions.
	// If dryRun is true, it reports what would be deleted without executing.
	Evaluate(ctx context.Context, dryRun bool) (*RetentionResult, error)
	// AddPolicy creates a new retention policy.
	AddPolicy(ctx context.Context, policy model.RetentionPolicy) error
	// RemovePolicy deletes a retention policy by name.
	RemovePolicy(ctx context.Context, name string) error
	// ListPolicies returns all configured retention policies.
	ListPolicies(ctx context.Context) ([]model.RetentionPolicy, error)
}

// RetentionResult holds the outcome of a retention evaluation.
type RetentionResult struct {
	EntriesEvaluated int64
	EntriesExpired   int64
	BytesFreed       int64
	FilesDeleted     int64 // physical storage files deleted
	Protected        int64 // entries that were protected (latest versions)
}

// RetentionEngine implements the Engine interface.
type RetentionEngine struct {
	repo  db.Repository
	store storage.Store
	now   func() time.Time // injectable clock for testing
}

// New creates a new RetentionEngine with the given repository and store.
func New(repo db.Repository, store storage.Store) *RetentionEngine {
	return &RetentionEngine{
		repo:  repo,
		store: store,
		now:   time.Now,
	}
}

// SetClock overrides the time source (used for testing).
func (e *RetentionEngine) SetClock(fn func() time.Time) {
	e.now = fn
}

// AddPolicy creates a new retention policy.
func (e *RetentionEngine) AddPolicy(ctx context.Context, policy model.RetentionPolicy) error {
	return e.repo.InsertRetentionPolicy(ctx, policy)
}

// RemovePolicy deletes a retention policy by name.
func (e *RetentionEngine) RemovePolicy(ctx context.Context, name string) error {
	return e.repo.DeleteRetentionPolicy(ctx, name)
}

// ListPolicies returns all configured retention policies.
func (e *RetentionEngine) ListPolicies(ctx context.Context) ([]model.RetentionPolicy, error) {
	return e.repo.ListRetentionPolicies(ctx)
}

// Evaluate runs retention evaluation:
// 1. Get all unique file_path values from backup entries
// 2. For each file_path, get all versions (ordered by backup_date DESC)
// 3. If the matching policy has keep_versions > 0: skip single-version files and protect the latest
// 4. If the matching policy has keep_versions == 0 (purge mode): ALL versions are candidates including the latest
// 5. For candidate versions, evaluate against policies (priority order, first-match-wins)
// 6. If not dryRun: delete expired entries from DB, delete physical files if refcount drops to 0
// 7. Return result counts
func (e *RetentionEngine) Evaluate(ctx context.Context, dryRun bool) (*RetentionResult, error) {
	result := &RetentionResult{}

	// Load all enabled policies sorted by priority DESC.
	policies, err := e.repo.ListRetentionPolicies(ctx)
	if err != nil {
		return nil, err
	}

	// Filter to only enabled policies.
	var enabledPolicies []model.RetentionPolicy
	for _, p := range policies {
		if p.Enabled {
			enabledPolicies = append(enabledPolicies, p)
		}
	}

	// Sort by priority DESC (first-match-wins with highest priority first).
	sort.Slice(enabledPolicies, func(i, j int) bool {
		return enabledPolicies[i].Priority > enabledPolicies[j].Priority
	})

	// Get all unique file paths.
	filePaths, err := e.repo.GetAllFilePaths(ctx)
	if err != nil {
		return nil, err
	}

	now := e.now()

	for _, fp := range filePaths {
		// Get all versions for this file path (ordered by backup_date DESC).
		versions, err := e.repo.GetFileVersions(ctx, fp)
		if err != nil {
			return nil, err
		}

		result.EntriesEvaluated += int64(len(versions))

		if len(versions) == 0 {
			continue
		}

		// Determine which policy applies to this file path (using latest version's path).
		matchedPolicy := findMatchingPolicy(enabledPolicies, versions[0].FilePath)

		// Purge mode: keep_versions == 0 means ALL versions are candidates (no protection).
		if matchedPolicy != nil && matchedPolicy.KeepVersions == 0 {
			for _, entry := range versions {
				if shouldExpire(entry, *matchedPolicy, now, len(versions)) {
					result.EntriesExpired++
					result.BytesFreed += entry.FileSize

					if !dryRun {
						if err := e.repo.DeleteEntryByID(ctx, entry.ID); err != nil {
							return nil, err
						}

						refCount, err := e.store.RefCount(ctx, entry.Blake3Hash)
						if err != nil {
							return nil, err
						}

						if refCount == 0 {
							if err := e.store.Delete(ctx, entry.Blake3Hash); err != nil {
								if err != storage.ErrNotFound {
									return nil, err
								}
							}
							result.FilesDeleted++
						}
					}
				}
			}
			continue
		}

		// Standard mode (keep_versions >= 1): protect the latest version.

		// Skip single-version files (safety rule for standard mode).
		if len(versions) <= 1 {
			if len(versions) == 1 {
				result.Protected++
			}
			continue
		}

		// The latest version (first in list, ordered by backup_date DESC) is PROTECTED.
		result.Protected++

		// Evaluate older versions (index 1 onward).
		olderVersions := versions[1:]

		for _, entry := range olderVersions {
			// Find first matching policy for this entry's file path.
			entryPolicy := findMatchingPolicy(enabledPolicies, entry.FilePath)
			if entryPolicy == nil {
				// No matching policy â€” entry is not expired.
				continue
			}

			// Check expiration: backup_date + keep_days < now AND version count > keep_versions.
			if shouldExpire(entry, *entryPolicy, now, len(versions)) {
				result.EntriesExpired++
				result.BytesFreed += entry.FileSize

				if !dryRun {
					// Delete entry from DB.
					if err := e.repo.DeleteEntryByID(ctx, entry.ID); err != nil {
						return nil, err
					}

					// Check if physical file should be deleted (refcount == 0 after deletion).
					refCount, err := e.store.RefCount(ctx, entry.Blake3Hash)
					if err != nil {
						return nil, err
					}

					if refCount == 0 {
						if err := e.store.Delete(ctx, entry.Blake3Hash); err != nil {
							// Ignore not-found errors (file may already be gone).
							if err != storage.ErrNotFound {
								return nil, err
							}
						}
						result.FilesDeleted++
					}
				}
			}
		}
	}

	return result, nil
}

// findMatchingPolicy returns the first matching policy for the given file path.
// Policies are expected to be sorted by priority DESC (first-match-wins).
func findMatchingPolicy(policies []model.RetentionPolicy, filePath string) *model.RetentionPolicy {
	for i := range policies {
		if policies[i].Pattern == "" {
			// Empty pattern matches everything.
			return &policies[i]
		}
		matched, err := filepath.Match(policies[i].Pattern, filePath)
		if err != nil {
			// Invalid pattern â€” skip.
			continue
		}
		if matched {
			return &policies[i]
		}
	}
	return nil
}

// shouldExpire determines if a version should be expired based on the policy.
// Per design: expire if backup_date + keep_days < now AND exceeds keep_versions.
// If keep_days is nil, the time condition is never met (keep forever).
// If keep_versions == 0 (purge mode), the version count guard is skipped â€” only the
// time condition matters.
func shouldExpire(entry model.BackupEntry, policy model.RetentionPolicy, now time.Time, totalVersions int) bool {
	// Check keep_days: nil means keep forever (time condition never satisfied).
	if policy.KeepDays == nil {
		return false
	}

	// Check keep_versions: when > 0, we must keep at least keep_versions versions.
	// When == 0 (purge mode), skip this guard entirely â€” all versions are candidates.
	if policy.KeepVersions > 0 && totalVersions <= policy.KeepVersions {
		return false
	}

	expirationDate := entry.BackupDate.AddDate(0, 0, *policy.KeepDays)
	if !expirationDate.Before(now) {
		// Not yet expired by time.
		return false
	}

	return true
}
