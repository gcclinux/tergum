package retention

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gcclinux/tergum/internal/db"
	"github.com/gcclinux/tergum/internal/model"
	"github.com/gcclinux/tergum/internal/storage"
	"pgregory.net/rapid"
)

// **Validates: Requirements 9.2, 9.3, 9.4, 9.5, 9.6**

// propSetup creates an in-memory repo (server mode), a CAS store, and a retention engine for property tests.
func propSetup(t *rapid.T) (*RetentionEngine, *db.SQLiteRepository, func()) {
	t.Helper()
	repo, err := db.NewRepository(":memory:", true)
	if err != nil {
		t.Fatalf("NewRepository: %v", err)
	}

	storageDir, err := os.MkdirTemp("", "retention-prop-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}

	cas := storage.NewCAS(storageDir, repo)
	engine := New(repo, cas)

	cleanup := func() {
		repo.Close()
		os.RemoveAll(storageDir)
	}
	return engine, repo, cleanup
}

// genFilePaths generates a slice of 2-10 unique file paths.
func genFilePaths(t *rapid.T) []string {
	count := rapid.IntRange(2, 10).Draw(t, "fileCount")
	paths := make([]string, count)
	for i := range paths {
		paths[i] = fmt.Sprintf("/data/dir%d/file%d.txt", i%3, i)
	}
	return paths
}

// genVersionDates generates 1-5 distinct backup dates for a file, all before `now`.
func genVersionDates(t *rapid.T, now time.Time, label string) []time.Time {
	count := rapid.IntRange(1, 5).Draw(t, label+"_versionCount")
	dates := make([]time.Time, count)
	for i := range dates {
		// Offset between 1 hour and 365 days in the past.
		hoursAgo := rapid.IntRange(1, 365*24).Draw(t, fmt.Sprintf("%s_hoursAgo_%d", label, i))
		dates[i] = now.Add(-time.Duration(hoursAgo) * time.Hour)
	}
	return dates
}

// genRetentionPolicies generates 1-3 enabled retention policies with random parameters.
func genRetentionPolicies(t *rapid.T) []model.RetentionPolicy {
	count := rapid.IntRange(1, 3).Draw(t, "policyCount")
	policies := make([]model.RetentionPolicy, count)
	for i := range policies {
		keepDays := rapid.IntRange(1, 180).Draw(t, fmt.Sprintf("keepDays_%d", i))
		keepVersions := rapid.IntRange(1, 4).Draw(t, fmt.Sprintf("keepVersions_%d", i))
		priority := rapid.IntRange(1, 100).Draw(t, fmt.Sprintf("priority_%d", i))

		policies[i] = model.RetentionPolicy{
			Name:         fmt.Sprintf("policy-%d", i),
			KeepDays:     &keepDays,
			KeepVersions: keepVersions,
			Pattern:      "", // empty pattern matches everything
			Priority:     priority,
			Enabled:      true,
		}
	}
	return policies
}

func TestProperty_RetentionEvaluationSafetyAndCorrectness(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		engine, repo, cleanup := propSetup(t)
		defer cleanup()
		ctx := context.Background()

		now := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
		engine.SetClock(func() time.Time { return now })

		// Generate random file paths.
		filePaths := genFilePaths(t)

		// Track the state before retention: for each file path, store its versions.
		type fileState struct {
			versions   []time.Time
			latestDate time.Time
		}
		preState := make(map[string]*fileState)

		jobCounter := 0
		for _, fp := range filePaths {
			dates := genVersionDates(t, now, fp)
			preState[fp] = &fileState{versions: dates}

			// Find the latest date for this file.
			latest := dates[0]
			for _, d := range dates[1:] {
				if d.After(latest) {
					latest = d
				}
			}
			preState[fp].latestDate = latest

			// Insert a job and entry for each version.
			for j, d := range dates {
				jobID := fmt.Sprintf("job-%d", jobCounter)
				jobCounter++
				err := repo.CreateJob(ctx, model.BackupJob{
					BackupID:    jobID,
					Level:       "FULL",
					ClientID:    "test-client",
					InitiatedBy: "cli",
					StartedAt:   d,
					Status:      model.JobCompleted,
				})
				if err != nil {
					t.Fatalf("CreateJob: %v", err)
				}

				hash := fmt.Sprintf("%04x%04x", jobCounter, j) + strings.Repeat("f", 56)
				// Ensure hash is exactly 64 chars.
				hash = hash[:64]

				entry := model.BackupEntry{
					BackupID:   jobID,
					Blake3Hash: hash,
					FileName:   fmt.Sprintf("file%d.txt", jobCounter),
					FilePath:   fp,
					FileSize:   int64(rapid.IntRange(100, 10000).Draw(t, fmt.Sprintf("size_%d_%d", jobCounter, j))),
					OS:         "linux",
					BackupDate: d,
				}
				if err := repo.InsertBackupEntry(ctx, entry); err != nil {
					t.Fatalf("InsertBackupEntry: %v", err)
				}
			}
		}

		// Generate and insert retention policies.
		policies := genRetentionPolicies(t)
		for _, p := range policies {
			if err := engine.AddPolicy(ctx, p); err != nil {
				t.Fatalf("AddPolicy: %v", err)
			}
		}

		// Run retention evaluation (non-dry-run).
		_, err := engine.Evaluate(ctx, false)
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}

		// Property (e): After retention runs, every file_path SHALL have at least one version remaining.
		for _, fp := range filePaths {
			versions, err := repo.GetFileVersions(ctx, fp)
			if err != nil {
				t.Fatalf("GetFileVersions(%q): %v", fp, err)
			}
			if len(versions) == 0 {
				t.Fatalf("property violation: file_path %q has 0 versions after retention", fp)
			}
		}

		// Property (a): The most recent version of any file SHALL never be marked for deletion.
		for _, fp := range filePaths {
			versions, err := repo.GetFileVersions(ctx, fp)
			if err != nil {
				t.Fatalf("GetFileVersions(%q): %v", fp, err)
			}
			// The latest version (by backup_date) must still be present.
			latestDate := preState[fp].latestDate
			found := false
			for _, v := range versions {
				if v.BackupDate.Equal(latestDate) {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("property violation: latest version (date=%v) of %q was deleted", latestDate, fp)
			}
		}

		// Property (b): Files with only one version SHALL never be candidates for deletion.
		for _, fp := range filePaths {
			if len(preState[fp].versions) == 1 {
				versions, err := repo.GetFileVersions(ctx, fp)
				if err != nil {
					t.Fatalf("GetFileVersions(%q): %v", fp, err)
				}
				if len(versions) != 1 {
					t.Fatalf("property violation: single-version file %q was modified (versions=%d)", fp, len(versions))
				}
			}
		}
	})
}
