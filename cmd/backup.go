package cmd

import (
	"bufio"
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gcclinux/tergum/internal/backup"
	"github.com/gcclinux/tergum/internal/config"
	"github.com/gcclinux/tergum/internal/connection"
	"github.com/gcclinux/tergum/internal/crypto"
	"github.com/gcclinux/tergum/internal/db"
	"github.com/gcclinux/tergum/internal/model"
	"github.com/spf13/cobra"
)

func newBackupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Trigger a backup operation",
		Long:  `Triggers a full or incremental backup of configured include paths.`,
		RunE:  runBackup,
	}

	cmd.Flags().StringP("level", "l", "auto", "backup level: auto, full")

	return cmd
}

func runBackup(cmd *cobra.Command, args []string) error {
	level, _ := cmd.Flags().GetString("level")

	// Load configuration.
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// Open database.
	repo, err := db.NewRepository(cfg.Database.Path, cfg.Database.WALMode)
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer repo.Close()

	// Resolve include paths: DB paths take priority, then config file paths.
	ctx := context.Background()
	includePaths, err := repo.ListIncludePaths(ctx)
	if err != nil {
		return fmt.Errorf("listing include paths: %w", err)
	}
	if len(includePaths) == 0 {
		includePaths = cfg.Client.IncludePaths
	}
	if len(includePaths) == 0 {
		return fmt.Errorf("no include paths configured. Run 'tergum setup' or 'tergum paths add <path>' first")
	}

	// Resolve exclude patterns: merge DB + config.
	excludePatterns, err := repo.ListExcludePatterns(ctx)
	if err != nil {
		return fmt.Errorf("listing exclude patterns: %w", err)
	}
	if len(excludePatterns) == 0 {
		excludePatterns = cfg.Client.ExcludePatterns
	}

	// Parse max file size.
	maxFileSize := parseMaxFileSize(cfg.Client.MaxFileSize)

	// Load master key for encryption.
	var masterKey []byte
	if cfg.Encryption.Enabled {
		key, err := loadMasterKey(cfg)
		if err != nil {
			return fmt.Errorf("loading encryption key: %w", err)
		}
		masterKey = key
	}

	// Create server connection based on node role.
	serverConn, err := connection.NewServerConnection(cfg)
	if err != nil {
		return fmt.Errorf("creating server connection: %w", err)
	}

	// Create encryptor.
	var encryptor *crypto.AESEncryptor
	if cfg.Encryption.Enabled {
		encryptor = crypto.NewEncryptor()
	}

	// Build engine config.
	engineCfg := backup.EngineConfig{
		IncludePaths:    includePaths,
		ExcludePatterns: excludePatterns,
		MaxFileSize:     maxFileSize,
		EncryptionOn:    cfg.Encryption.Enabled,
		MasterKey:       masterKey,
		DatabasePath:    cfg.Database.Path,
	}

	// Create backup engine.
	engine := backup.NewBackupEngine(serverConn, repo, encryptor, engineCfg)

	// Remove any stale stop signal file before starting.
	configDir := filepath.Dir(cfg.Database.Path)
	stopFile := filepath.Join(configDir, "stop")
	os.Remove(stopFile)

	// Monitor for stop signal in the background.
	stopCtx, stopCancel := context.WithCancel(ctx)
	defer stopCancel()
	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stopCtx.Done():
				return
			case <-ticker.C:
				if _, err := os.Stat(stopFile); err == nil {
					engine.Stop(ctx)
					os.Remove(stopFile)
					return
				}
			}
		}
	}()

	// Determine backup level.
	var backupLevel model.BackupLevel
	switch strings.ToLower(level) {
	case "full":
		backupLevel = model.BackupLevelFull
	default:
		backupLevel = model.BackupLevelAuto
	}

	// Generate a client ID.
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "local"
	}

	fmt.Printf("Starting %s backup...\n", level)
	fmt.Printf("Include paths: %d\n", len(includePaths))
	fmt.Printf("Exclude patterns: %d\n", len(excludePatterns))
	fmt.Println()

	// Run backup.
	result, err := engine.RunBackup(ctx, backup.BackupRequest{
		Level:       backupLevel,
		ClientID:    hostname,
		InitiatedBy: "cli",
	})
	if err != nil {
		return fmt.Errorf("backup failed: %w", err)
	}

	printOutput(
		map[string]interface{}{
			"backup_id":       result.BackupID,
			"status":          string(result.Status),
			"files_processed": result.FilesProcessed,
			"bytes_new":       result.BytesNew,
			"files_deduped":   result.FilesDeduped,
		},
		fmt.Sprintf("Backup complete: %s\n  Files: %d | New bytes: %d | Deduped: %d",
			result.BackupID, result.FilesProcessed, result.BytesNew, result.FilesDeduped),
	)

	return nil
}

// loadMasterKey derives the master key from the stored salt and passphrase.
// The passphrase is read from TERGUM_PASSPHRASE env var or prompted via stdin.
func loadMasterKey(cfg *config.Config) ([]byte, error) {
	configDir := filepath.Dir(cfg.Database.Path)
	saltPath := filepath.Join(configDir, "salt")

	saltHex, err := os.ReadFile(saltPath)
	if err != nil {
		return nil, fmt.Errorf("cannot read salt file %s: %w (run 'tergum setup' first)", saltPath, err)
	}

	salt, err := hex.DecodeString(strings.TrimSpace(string(saltHex)))
	if err != nil {
		return nil, fmt.Errorf("invalid salt file: %w", err)
	}

	// Get passphrase from environment or prompt.
	passphrase := os.Getenv("TERGUM_PASSPHRASE")
	if passphrase == "" {
		fmt.Print("Encryption passphrase: ")
		scanner := bufio.NewScanner(os.Stdin)
		if scanner.Scan() {
			passphrase = strings.TrimSpace(scanner.Text())
		}
		if passphrase == "" {
			return nil, fmt.Errorf("passphrase is required: set TERGUM_PASSPHRASE env var")
		}
	}

	enc := crypto.NewEncryptor()
	masterKey, err := enc.DeriveKey(passphrase, salt)
	if err != nil {
		return nil, fmt.Errorf("key derivation failed: %w", err)
	}

	// Verify derived master key against key_verify if it exists
	verifyPath := filepath.Join(configDir, "key_verify")
	if verifyData, err := os.ReadFile(verifyPath); err == nil {
		if ok, err := enc.VerifyMasterKey(masterKey, string(verifyData)); err != nil || !ok {
			return nil, fmt.Errorf("invalid passphrase: key verification failed")
		}
	}

	return masterKey, nil
}

// parseMaxFileSize converts a human-readable size string (e.g. "10GB") to bytes.
func parseMaxFileSize(s string) int64 {
	s = strings.TrimSpace(strings.ToUpper(s))
	if s == "" {
		return 10 * 1024 * 1024 * 1024 // default 10GB
	}

	multiplier := int64(1)
	if strings.HasSuffix(s, "TB") {
		multiplier = 1024 * 1024 * 1024 * 1024
		s = strings.TrimSuffix(s, "TB")
	} else if strings.HasSuffix(s, "GB") {
		multiplier = 1024 * 1024 * 1024
		s = strings.TrimSuffix(s, "GB")
	} else if strings.HasSuffix(s, "MB") {
		multiplier = 1024 * 1024
		s = strings.TrimSuffix(s, "MB")
	} else if strings.HasSuffix(s, "KB") {
		multiplier = 1024
		s = strings.TrimSuffix(s, "KB")
	}

	// Use a simple approach to avoid importing math or using ParseFloat.
	val, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 10 * 1024 * 1024 * 1024
	}
	return val * multiplier
}
