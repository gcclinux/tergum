package cmd

import (
	"bufio"
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/ricardopadilha/tergum/internal/backup"
	"github.com/ricardopadilha/tergum/internal/config"
	"github.com/ricardopadilha/tergum/internal/crypto"
	"github.com/ricardopadilha/tergum/internal/db"
	"github.com/ricardopadilha/tergum/internal/scheduler"
	"github.com/ricardopadilha/tergum/internal/watcher"
	"github.com/spf13/cobra"
)

func newWatchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Start file watcher for ongoing backup",
		Long: `Starts the file watcher to monitor configured include paths for changes.
Files that pass the stability gate (unchanged for the configured duration) are
automatically backed up.

Pipeline: filesystem event → exclude filter → debounce → stability gate → BLAKE3 hash → encrypt → upload

The watcher batches stable files into backup jobs at a configurable interval
(default: 5 minutes). It runs in the foreground until stopped with Ctrl+C or SIGTERM.`,
		RunE: runWatch,
	}

	return cmd
}

func runWatch(cmd *cobra.Command, args []string) error {
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

	// Resolve include paths.
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

	// Resolve exclude patterns.
	excludePatterns, err := repo.ListExcludePatterns(ctx)
	if err != nil {
		return fmt.Errorf("listing exclude patterns: %w", err)
	}
	if len(excludePatterns) == 0 {
		excludePatterns = cfg.Client.ExcludePatterns
	}

	// Load master key for encryption.
	var masterKey []byte
	var encryptor *crypto.AESEncryptor
	if cfg.Encryption.Enabled {
		key, err := loadWatchMasterKey(cfg)
		if err != nil {
			return fmt.Errorf("loading encryption key: %w", err)
		}
		masterKey = key
		encryptor = crypto.NewEncryptor()
	}

	// Create file watcher.
	watcherCfg := watcher.Config{
		DebounceMs:       cfg.Watcher.DebounceMs,
		StabilitySeconds: cfg.Watcher.StabilitySeconds,
		ExcludePatterns:  excludePatterns,
		Repository:       repo,
	}

	fw, err := watcher.NewFileWatcher(watcherCfg)
	if err != nil {
		return fmt.Errorf("creating file watcher: %w", err)
	}

	// Start the watcher first.
	watchCtx, watchCancel := context.WithCancel(ctx)
	defer watchCancel()

	if err := fw.Start(watchCtx); err != nil {
		return fmt.Errorf("starting file watcher: %w", err)
	}

	// Add include paths to the watcher (can be slow for large trees).
	fmt.Printf("Registering watch paths...\n")
	pathCount := 0
	for _, p := range includePaths {
		if err := fw.AddPath(p, true); err != nil {
			fmt.Fprintf(os.Stderr, "  Warning: cannot watch %s: %v\n", p, err)
		} else {
			pathCount++
			fmt.Printf("  + %s\n", p)
		}
	}

	if pathCount == 0 {
		fw.Stop()
		return fmt.Errorf("no paths could be watched")
	}

	// Create local server connection.
	storageDir := cfg.StorageDir()
	serverConn := &backup.LocalServerConnection{
		StorageDir: storageDir,
		Repo:       repo,
	}

	// Create ongoing backup processor.
	batchInterval := time.Duration(cfg.Watcher.BatchIntervalMinutes) * time.Minute
	if batchInterval <= 0 {
		batchInterval = 5 * time.Minute
	}

	ongoing := scheduler.NewOngoingBackup(scheduler.OngoingConfig{
		Watcher:       fw,
		Server:        serverConn,
		Repo:          repo,
		Encryptor:     encryptor,
		MasterKey:     masterKey,
		BatchInterval: batchInterval,
	})

	// Start the ongoing backup processor.
	if err := ongoing.Start(watchCtx); err != nil {
		fw.Stop()
		return fmt.Errorf("starting ongoing backup: %w", err)
	}

	fmt.Printf("File watcher started.\n")
	fmt.Printf("  Watching:          %d paths\n", len(includePaths))
	fmt.Printf("  Exclude patterns:  %d\n", len(excludePatterns))
	fmt.Printf("  Debounce:          %dms\n", cfg.Watcher.DebounceMs)
	fmt.Printf("  Stability gate:    %ds\n", cfg.Watcher.StabilitySeconds)
	fmt.Printf("  Batch interval:    %s\n", batchInterval)
	fmt.Printf("  Storage:           %s\n", storageDir)
	fmt.Printf("\nPress Ctrl+C to stop.\n\n")

	// Wait for shutdown signal.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	// Print status periodically.
	statusTicker := time.NewTicker(30 * time.Second)
	defer statusTicker.Stop()

	for {
		select {
		case sig := <-sigCh:
			fmt.Printf("\nReceived %s, shutting down...\n", sig)
			watchCancel()
			ongoing.Stop()
			fw.Stop()

			status := fw.Status()
			printOutput(
				map[string]interface{}{
					"events_received": status.EventCount,
					"files_backed_up": status.StableCount,
				},
				fmt.Sprintf("Watcher stopped. Events: %d | Files backed up: %d",
					status.EventCount, status.StableCount),
			)
			return nil

		case <-statusTicker.C:
			status := fw.Status()
			if status.EventCount > 0 || status.StableCount > 0 {
				fmt.Printf("[%s] Events: %d | Stable files backed up: %d\n",
					time.Now().Format("15:04:05"), status.EventCount, status.StableCount)
			}
		}
	}
}

// loadWatchMasterKey loads the encryption key for the watcher.
func loadWatchMasterKey(cfg *config.Config) ([]byte, error) {
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

	return masterKey, nil
}
