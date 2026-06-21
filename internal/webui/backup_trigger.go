package webui

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"

	"github.com/ricardopadilha/tergum/internal/backup"
	"github.com/ricardopadilha/tergum/internal/crypto"
	"github.com/ricardopadilha/tergum/internal/db"
	"github.com/ricardopadilha/tergum/internal/model"
)

// LocalBackupTrigger triggers backups directly using the backup engine.
type LocalBackupTrigger struct {
	repo       db.Repository
	storageDir string
	masterKey  []byte
	encEnabled bool
	broker     *SSEBroker
	mu         sync.Mutex
	running    bool
}

// NewLocalBackupTrigger creates a backup trigger with the given parameters.
// If masterKey is nil and encryption is enabled, backups cannot be triggered.
func NewLocalBackupTrigger(repo db.Repository, storageDir string, masterKey []byte, encEnabled bool) *LocalBackupTrigger {
	return &LocalBackupTrigger{
		repo:       repo,
		storageDir: storageDir,
		masterKey:  masterKey,
		encEnabled: encEnabled,
	}
}

// SetBroker sets the SSE broker for publishing backup events.
func (t *LocalBackupTrigger) SetBroker(b *SSEBroker) {
	t.broker = b
}

// IsAvailable returns true if the trigger can start a backup.
func (t *LocalBackupTrigger) IsAvailable() bool {
	if t.encEnabled && len(t.masterKey) == 0 {
		return false
	}
	return true
}

// TriggerBackup starts a backup in a goroutine.
func (t *LocalBackupTrigger) TriggerBackup(level string) error {
	t.mu.Lock()
	if t.running {
		t.mu.Unlock()
		return fmt.Errorf("a backup is already running")
	}
	t.running = true
	t.mu.Unlock()

	go func() {
		defer func() {
			t.mu.Lock()
			t.running = false
			t.mu.Unlock()
		}()

		ctx := context.Background()

		// Resolve paths from DB.
		includePaths, _ := t.repo.ListIncludePaths(ctx)
		excludePatterns, _ := t.repo.ListExcludePatterns(ctx)

		if len(includePaths) == 0 {
			slog.Error("web backup trigger: no include paths configured")
			return
		}

		// Create server connection.
		serverConn := &backup.LocalServerConnection{
			StorageDir: t.storageDir,
			Repo:       t.repo,
		}

		// Create encryptor.
		var encryptor *crypto.AESEncryptor
		if t.encEnabled {
			encryptor = crypto.NewEncryptor()
		}

		// Engine config.
		engineCfg := backup.EngineConfig{
			IncludePaths:    includePaths,
			ExcludePatterns: excludePatterns,
			MaxFileSize:     10 * 1024 * 1024 * 1024, // 10GB
			EncryptionOn:    t.encEnabled,
			MasterKey:       t.masterKey,
		}

		engine := backup.NewBackupEngine(serverConn, t.repo, encryptor, engineCfg)

		// Determine level.
		var backupLevel model.BackupLevel
		switch strings.ToLower(level) {
		case "full":
			backupLevel = model.BackupLevelFull
		default:
			backupLevel = model.BackupLevelAuto
		}

		hostname, _ := os.Hostname()
		if hostname == "" {
			hostname = "local"
		}

		slog.Info("web-triggered backup starting", "level", level)
		if t.broker != nil {
			t.broker.Publish(ActivityEvent{
				Type:    "backup_started",
				Message: fmt.Sprintf("Backup %s started (web UI)", level),
			})
		}
		result, err := engine.RunBackup(ctx, backup.BackupRequest{
			Level:       backupLevel,
			ClientID:    hostname,
			InitiatedBy: "webui",
		})
		if err != nil {
			slog.Error("web-triggered backup failed", "error", err)
			if t.broker != nil {
				t.broker.Publish(ActivityEvent{
					Type:    "backup_failed",
					Message: fmt.Sprintf("Backup %s failed: %v", level, err),
				})
			}
			return
		}
		slog.Info("web-triggered backup completed",
			"backup_id", result.BackupID,
			"files", result.FilesProcessed,
			"bytes_new", result.BytesNew,
		)
		if t.broker != nil {
			t.broker.Publish(ActivityEvent{
				Type:    "backup_completed",
				Message: fmt.Sprintf("Backup %s completed: %d files, %d bytes new", level, result.FilesProcessed, result.BytesNew),
			})
		}
	}()

	return nil
}
