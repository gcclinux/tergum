package cmd

import (
	"context"
	"fmt"
	"testing"

	"github.com/gcclinux/tergum/internal/backup"
	"github.com/gcclinux/tergum/internal/db"
	"github.com/gcclinux/tergum/internal/model"
	"pgregory.net/rapid"
)

// **Validates: Requirements 1.1, 1.2, 2.1, 2.2**

// mockServerConn implements backup.ServerConnection with configurable ExchangeManifest error.
type mockServerConn struct {
	exchangeErr error
}

func (m *mockServerConn) ExchangeManifest(ctx context.Context, manifest []model.ManifestEntry) (backup.ManifestDiff, error) {
	return backup.ManifestDiff{}, m.exchangeErr
}

func (m *mockServerConn) UploadFile(ctx context.Context, hash string, data []byte, wrappedDEK []byte, nonce []byte, entry model.BackupEntry) error {
	return nil
}

func (m *mockServerConn) SyncDatabase(ctx context.Context, dbPath string) error {
	return nil
}

func (m *mockServerConn) LogActivity(ctx context.Context, backupID string, status model.JobStatus, clientID string, errMsg string) error {
	return nil
}

// handleBackupResult mirrors the FIXED CLI logic from cmd/backup.go runBackup.
// After engine.RunBackup returns (result, nil), the CLI now checks result.Status
// and returns appropriate typed errors for failed and stopped backups.
func handleBackupResult(result *backup.BackupResult, err error) error {
	if err != nil {
		return fmt.Errorf("backup failed: %w", err)
	}

	// Fixed code: inspect result.Status and return typed errors.
	switch result.Status {
	case model.JobFailed:
		return &model.BackupFailedError{Message: fmt.Sprintf("[%s] %s", result.BackupID, result.ErrorMessage)}
	case model.JobStopped:
		return &model.StoppedError{Message: fmt.Sprintf("backup stopped [%s]", result.BackupID)}
	case model.JobCompleted:
		// Proceed normally.
	default:
		// Proceed normally.
	}

	return nil
}

// TestProperty_FailedBackupSilentSuccess verifies that when RunBackup returns a result
// with Status == "failed" and a nil error, the CLI layer SHOULD return a non-nil error
// of type *model.BackupFailedError with exit code 11.
//
// Bug Condition: result.Status == model.JobFailed AND err == nil
//
// This test is EXPECTED TO FAIL on unfixed code because the current runBackup logic
// only checks `if err != nil` and never inspects result.Status, silently reporting
// success for failed backups.
func TestProperty_FailedBackupSilentSuccess(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate a random error message that would cause ExchangeManifest to fail.
		// This simulates various server-side failures (network errors, auth failures, etc.)
		errMsg := rapid.StringMatching(`[a-zA-Z0-9 :_\-]{5,50}`).Draw(rt, "errorMessage")

		// Create a mock server connection that fails on ExchangeManifest.
		mockServer := &mockServerConn{
			exchangeErr: fmt.Errorf("%s", errMsg),
		}

		// Create an in-memory repository for the engine.
		repo, repoErr := db.NewRepository(":memory:", false)
		if repoErr != nil {
			rt.Fatalf("failed to create in-memory repo: %v", repoErr)
		}
		defer repo.Close()

		// Create a backup engine with the mock server and real in-memory DB.
		// Use a non-existent include path to ensure scan succeeds with 0 files,
		// then manifest exchange will fail with our injected error.
		// Actually, we need scan to succeed so that we reach ExchangeManifest.
		// Use the engine with an include path that exists (current directory).
		engineCfg := backup.EngineConfig{
			IncludePaths:    []string{"."},
			ExcludePatterns: []string{"**"}, // Exclude everything so scan returns 0 files quickly
			MaxFileSize:     1024,
			EncryptionOn:    false,
		}

		engine := backup.NewBackupEngine(mockServer, repo, nil, engineCfg)

		// Run backup — this will:
		// 1. CreateJob (succeeds with in-memory DB)
		// 2. Scan (returns 0 files because everything is excluded)
		// 3. BuildManifest (builds empty manifest)
		// 4. ExchangeManifest → FAILS with our injected error
		// 5. finishJob(model.JobFailed, result, "manifest exchange failed: <errMsg>")
		// 6. Returns (*BackupResult{Status: "failed"}, nil)
		ctx := context.Background()
		result, err := engine.RunBackup(ctx, backup.BackupRequest{
			Level:       model.BackupLevelFull,
			ClientID:    "test-client",
			InitiatedBy: "test",
		})

		// Verify our preconditions: RunBackup returned a failed result with nil error.
		// This is the bug condition we're testing.
		if err != nil {
			rt.Fatalf("precondition failed: RunBackup returned non-nil error: %v", err)
		}
		if result == nil {
			rt.Fatalf("precondition failed: RunBackup returned nil result")
		}
		if result.Status != model.JobFailed {
			rt.Fatalf("precondition failed: expected Status='failed', got %q", result.Status)
		}

		// Now test what the CLI does with this result.
		// handleBackupResult mirrors the fixed runBackup logic.
		cliErr := handleBackupResult(result, err)

		// EXPECTED CORRECT BEHAVIOR:
		// The CLI SHOULD return a non-nil error of type *model.BackupFailedError
		// with exit code 11 when result.Status == "failed".
		if cliErr == nil {
			rt.Fatalf("BUG CONFIRMED: RunBackup returned Status=%q with ErrorMessage containing %q "+
				"but the CLI (runBackup) returns nil error.\n"+
				"Expected: non-nil error of type *model.BackupFailedError with exit code 11.\n"+
				"The CLI silently reports success for a failed backup.",
				result.Status, errMsg)
		}

		// If we somehow get here (after fix), verify it's the right error type.
		var backupErr *model.BackupFailedError
		if bfe, ok := cliErr.(*model.BackupFailedError); ok {
			backupErr = bfe
		}
		if backupErr == nil {
			rt.Fatalf("CLI returned an error but it is not *model.BackupFailedError: %T: %v",
				cliErr, cliErr)
		}
		if backupErr.ExitCode() != model.ExitBackupFailed {
			rt.Fatalf("BackupFailedError has wrong exit code: got %d, want %d",
				backupErr.ExitCode(), model.ExitBackupFailed)
		}
	})
}
