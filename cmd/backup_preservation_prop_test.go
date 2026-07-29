package cmd

import (
	"testing"

	"github.com/gcclinux/tergum/internal/backup"
	"github.com/gcclinux/tergum/internal/model"
	"pgregory.net/rapid"
)

// **Validates: Requirements 3.1, 3.2, 3.3, 3.4**

// TestProperty_SuccessfulBackupReturnsNil verifies that the current CLI logic
// returns nil (exit code 0) for all successful backup results.
//
// Property 2a: For all random successful backup results (Status="completed",
// random FilesProcessed > 0, random BytesNew, random FilesDeduped),
// runBackup SHALL return nil error and exit code 0.
//
// This test captures the baseline behavior on UNFIXED code that must be preserved.
func TestProperty_SuccessfulBackupReturnsNil(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate random successful backup results.
		filesProcessed := rapid.Int64Range(1, 100000).Draw(rt, "filesProcessed")
		bytesNew := rapid.Int64Range(0, 10*1024*1024*1024).Draw(rt, "bytesNew")
		filesDeduped := rapid.Int64Range(0, filesProcessed).Draw(rt, "filesDeduped")

		result := &backup.BackupResult{
			BackupID:       "test-backup-id",
			FilesProcessed: filesProcessed,
			BytesNew:       bytesNew,
			FilesDeduped:   filesDeduped,
			Status:         model.JobCompleted,
		}

		// The CLI receives (result, nil) from RunBackup.
		// handleBackupResult mirrors the CLI's fixed logic.
		cliErr := handleBackupResult(result, nil)

		// On UNFIXED code, successful backups return nil (correct behavior to preserve).
		if cliErr != nil {
			rt.Fatalf("PRESERVATION VIOLATION: successful backup (Status=%q, Files=%d, Bytes=%d, Deduped=%d) "+
				"returned non-nil error: %v\nExpected: nil error (exit code 0)",
				result.Status, filesProcessed, bytesNew, filesDeduped, cliErr)
		}

		// Verify exit code would be 0.
		exitCode := model.GetExitCode(cliErr)
		if exitCode != model.ExitSuccess {
			rt.Fatalf("PRESERVATION VIOLATION: successful backup should have exit code 0, got %d",
				exitCode)
		}
	})
}

// TestProperty_StoppedBackupHandledGracefully verifies that the fixed CLI logic
// handles stopped backup results by returning a StoppedError with exit code 10.
//
// Property 2b: For all random stopped backup results (Status="stopped"),
// runBackup SHALL return a *model.StoppedError with exit code 10.
//
// After the fix: the CLI inspects result.Status and returns StoppedError for stopped backups.
func TestProperty_StoppedBackupHandledGracefully(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate random stopped backup results.
		// When stopped, some files may have been processed before the stop.
		filesProcessed := rapid.Int64Range(0, 50000).Draw(rt, "filesProcessed")
		bytesNew := rapid.Int64Range(0, 5*1024*1024*1024).Draw(rt, "bytesNew")
		filesDeduped := rapid.Int64Range(0, filesProcessed+1).Draw(rt, "filesDeduped")

		result := &backup.BackupResult{
			BackupID:       "test-stopped-backup-id",
			FilesProcessed: filesProcessed,
			BytesNew:       bytesNew,
			FilesDeduped:   filesDeduped,
			Status:         model.JobStopped,
		}

		// The CLI receives (result, nil) from RunBackup for stopped backups.
		// handleBackupResult mirrors the CLI's fixed logic.
		cliErr := handleBackupResult(result, nil)

		// After fix: the CLI returns a StoppedError for stopped backups.
		if cliErr == nil {
			rt.Fatalf("PRESERVATION VIOLATION: stopped backup (Status=%q, Files=%d) "+
				"returned nil error.\nExpected: *model.StoppedError with exit code 10",
				result.Status, filesProcessed)
		}

		// Verify it's a StoppedError.
		var stoppedErr *model.StoppedError
		if se, ok := cliErr.(*model.StoppedError); ok {
			stoppedErr = se
		}
		if stoppedErr == nil {
			rt.Fatalf("stopped backup returned wrong error type: got %T: %v, want *model.StoppedError",
				cliErr, cliErr)
		}

		// Verify exit code is 10.
		exitCode := model.GetExitCode(cliErr)
		if exitCode != model.ExitStopped {
			rt.Fatalf("PRESERVATION VIOLATION: stopped backup should have exit code %d, got %d",
				model.ExitStopped, exitCode)
		}
	})
}
