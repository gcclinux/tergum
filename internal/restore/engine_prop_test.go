package restore

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/gcclinux/tergum/internal/crypto"
	"github.com/gcclinux/tergum/internal/db"
	"github.com/gcclinux/tergum/internal/model"
	"pgregory.net/rapid"
)

// **Validates: Requirements 7.8, 8.4, 20.1, 20.2, 20.4, 20.5**

// TestProperty_FileMetadataRoundTrip_Permissions verifies that for any valid Unix
// permission value (0o000â€“0o777), backing up and restoring a file preserves the
// permission bits exactly.
func TestProperty_FileMetadataRoundTrip_Permissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission tests skipped on Windows")
	}

	rapid.Check(t, func(rt *rapid.T) {
		// Setup test engine with in-memory DB and local CAS.
		storageDir := t.TempDir()
		repo, err := db.NewRepository(":memory:", false)
		if err != nil {
			rt.Fatalf("failed to create repository: %v", err)
		}
		defer repo.Close()

		source := &LocalDataSource{StorageDir: storageDir}
		engine := NewRestoreEngine(source, repo, nil, nil)

		// Generate random file content.
		size := rapid.IntRange(1, 4096).Draw(rt, "fileSize")
		content := rapid.SliceOfN(rapid.Byte(), size, size).Draw(rt, "content")

		// Generate random Unix permissions (0o000â€“0o777).
		permValue := rapid.Uint32Range(0o000, 0o777).Draw(rt, "permissions")

		hash := crypto.HashBytes(content)
		backupID := "backup-prop-perms"

		// Create backup job.
		job := model.BackupJob{
			BackupID:    backupID,
			Level:       "FULL",
			ClientID:    "test-client",
			InitiatedBy: "test",
			StartedAt:   time.Now().UTC(),
			Status:      model.JobCompleted,
		}
		if err := repo.CreateJob(context.Background(), job); err != nil {
			rt.Fatalf("failed to create job: %v", err)
		}

		// Store in CAS.
		casDir := filepath.Join(storageDir, hash[:2])
		if err := os.MkdirAll(casDir, 0o755); err != nil {
			rt.Fatalf("failed to create CAS dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(casDir, hash), content, 0o644); err != nil {
			rt.Fatalf("failed to write CAS file: %v", err)
		}

		// Insert DB entry with permissions metadata.
		entry := model.BackupEntry{
			BackupID:    backupID,
			Blake3Hash:  hash,
			FileName:    "testfile.bin",
			FilePath:    "/test/testfile.bin",
			FileSize:    int64(len(content)),
			OS:          runtime.GOOS,
			Permissions: &permValue,
			BackupDate:  time.Now().UTC(),
		}
		if err := repo.InsertBackupEntry(context.Background(), entry); err != nil {
			rt.Fatalf("failed to insert backup entry: %v", err)
		}

		// Restore the file.
		destDir := t.TempDir()
		dest := filepath.Join(destDir, "restored.bin")

		if err := engine.RestoreFile(context.Background(), hash, dest); err != nil {
			rt.Fatalf("RestoreFile failed: %v", err)
		}

		// Verify permissions match.
		info, err := os.Stat(dest)
		if err != nil {
			rt.Fatalf("failed to stat restored file: %v", err)
		}

		restoredPerm := uint32(info.Mode().Perm())
		if restoredPerm != permValue {
			rt.Fatalf("permissions mismatch: stored 0o%03o, restored 0o%03o", permValue, restoredPerm)
		}
	})
}

// TestProperty_FileMetadataRoundTrip_Timestamps verifies that for any valid
// timestamp, backing up and restoring a file preserves the modification time.
func TestProperty_FileMetadataRoundTrip_Timestamps(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Setup test engine with in-memory DB and local CAS.
		storageDir := t.TempDir()
		repo, err := db.NewRepository(":memory:", false)
		if err != nil {
			rt.Fatalf("failed to create repository: %v", err)
		}
		defer repo.Close()

		source := &LocalDataSource{StorageDir: storageDir}
		engine := NewRestoreEngine(source, repo, nil, nil)

		// Generate random file content.
		size := rapid.IntRange(1, 4096).Draw(rt, "fileSize")
		content := rapid.SliceOfN(rapid.Byte(), size, size).Draw(rt, "content")

		// Generate a random timestamp between year 2000 and 2030.
		// Using second precision to avoid filesystem rounding issues.
		unixSec := rapid.Int64Range(946684800, 1893456000).Draw(rt, "modTimeSec")
		modTime := time.Unix(unixSec, 0).UTC()

		hash := crypto.HashBytes(content)
		backupID := "backup-prop-ts"

		// Create backup job.
		job := model.BackupJob{
			BackupID:    backupID,
			Level:       "FULL",
			ClientID:    "test-client",
			InitiatedBy: "test",
			StartedAt:   time.Now().UTC(),
			Status:      model.JobCompleted,
		}
		if err := repo.CreateJob(context.Background(), job); err != nil {
			rt.Fatalf("failed to create job: %v", err)
		}

		// Store in CAS.
		casDir := filepath.Join(storageDir, hash[:2])
		if err := os.MkdirAll(casDir, 0o755); err != nil {
			rt.Fatalf("failed to create CAS dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(casDir, hash), content, 0o644); err != nil {
			rt.Fatalf("failed to write CAS file: %v", err)
		}

		// Insert DB entry with timestamp metadata.
		entry := model.BackupEntry{
			BackupID:   backupID,
			Blake3Hash: hash,
			FileName:   "timestamped.bin",
			FilePath:   "/test/timestamped.bin",
			FileSize:   int64(len(content)),
			OS:         runtime.GOOS,
			ModifiedAt: &modTime,
			BackupDate: time.Now().UTC(),
		}
		if err := repo.InsertBackupEntry(context.Background(), entry); err != nil {
			rt.Fatalf("failed to insert backup entry: %v", err)
		}

		// Restore the file.
		destDir := t.TempDir()
		dest := filepath.Join(destDir, "restored.bin")

		if err := engine.RestoreFile(context.Background(), hash, dest); err != nil {
			rt.Fatalf("RestoreFile failed: %v", err)
		}

		// Verify modification time matches.
		info, err := os.Stat(dest)
		if err != nil {
			rt.Fatalf("failed to stat restored file: %v", err)
		}

		restoredModTime := info.ModTime().UTC()
		if !restoredModTime.Equal(modTime) {
			rt.Fatalf("mod time mismatch: stored %v, restored %v", modTime, restoredModTime)
		}
	})
}

// TestProperty_FileMetadataRoundTrip_Symlinks verifies that for any symlink,
// backing up and restoring preserves the symlink target path exactly.
func TestProperty_FileMetadataRoundTrip_Symlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Symlink tests skipped on Windows (requires elevated privileges)")
	}

	rapid.Check(t, func(rt *rapid.T) {
		// Setup test engine with in-memory DB and local CAS.
		storageDir := t.TempDir()
		repo, err := db.NewRepository(":memory:", false)
		if err != nil {
			rt.Fatalf("failed to create repository: %v", err)
		}
		defer repo.Close()

		source := &LocalDataSource{StorageDir: storageDir}
		engine := NewRestoreEngine(source, repo, nil, nil)

		// Create a real target file that the symlink will point to.
		destDir := t.TempDir()
		targetName := "target.txt"
		targetPath := filepath.Join(destDir, targetName)
		targetContent := []byte("symlink target content")
		if err := os.WriteFile(targetPath, targetContent, 0o644); err != nil {
			rt.Fatalf("failed to create target file: %v", err)
		}

		// The content stored in CAS for the symlink entry is the target path.
		symlinkContent := []byte(targetPath)
		hash := crypto.HashBytes(symlinkContent)

		backupID := "backup-prop-symlink"

		// Create backup job.
		job := model.BackupJob{
			BackupID:    backupID,
			Level:       "FULL",
			ClientID:    "test-client",
			InitiatedBy: "test",
			StartedAt:   time.Now().UTC(),
			Status:      model.JobCompleted,
		}
		if err := repo.CreateJob(context.Background(), job); err != nil {
			rt.Fatalf("failed to create job: %v", err)
		}

		// Store in CAS.
		casDir := filepath.Join(storageDir, hash[:2])
		if err := os.MkdirAll(casDir, 0o755); err != nil {
			rt.Fatalf("failed to create CAS dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(casDir, hash), symlinkContent, 0o644); err != nil {
			rt.Fatalf("failed to write CAS file: %v", err)
		}

		// Insert DB entry with symlink metadata.
		entry := model.BackupEntry{
			BackupID:      backupID,
			Blake3Hash:    hash,
			FileName:      "link.txt",
			FilePath:      "/test/link.txt",
			FileSize:      int64(len(symlinkContent)),
			OS:            runtime.GOOS,
			Symlink:       true,
			SymlinkTarget: targetPath,
			BackupDate:    time.Now().UTC(),
		}
		if err := repo.InsertBackupEntry(context.Background(), entry); err != nil {
			rt.Fatalf("failed to insert backup entry: %v", err)
		}

		// Restore the symlink.
		linkPath := filepath.Join(destDir, "restored_link.txt")

		if err := engine.RestoreFile(context.Background(), hash, linkPath); err != nil {
			rt.Fatalf("RestoreFile failed: %v", err)
		}

		// Verify it's a symlink.
		info, err := os.Lstat(linkPath)
		if err != nil {
			rt.Fatalf("failed to lstat restored path: %v", err)
		}
		if info.Mode()&fs.ModeSymlink == 0 {
			rt.Fatalf("expected a symlink, got mode %v", info.Mode())
		}

		// Verify symlink target matches.
		restoredTarget, err := os.Readlink(linkPath)
		if err != nil {
			rt.Fatalf("failed to read symlink target: %v", err)
		}
		if restoredTarget != targetPath {
			rt.Fatalf("symlink target mismatch: expected %q, got %q", targetPath, restoredTarget)
		}
	})
}
