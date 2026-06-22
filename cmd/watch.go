package cmd

import (
	"bufio"
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/gcclinux/tergum/internal/config"
	"github.com/gcclinux/tergum/internal/connection"
	"github.com/gcclinux/tergum/internal/crypto"
	"github.com/gcclinux/tergum/internal/db"
	"github.com/gcclinux/tergum/internal/scheduler"
	"github.com/gcclinux/tergum/internal/watcher"
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

	cmd.AddCommand(newWatchRunCmd())
	cmd.AddCommand(newWatchEnableCmd())
	cmd.AddCommand(newWatchDisableCmd())
	cmd.AddCommand(newWatchAddCmd())
	cmd.AddCommand(newWatchRemoveCmd())
	cmd.AddCommand(newWatchListCmd())

	return cmd
}

func newWatchRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run",
		Short: "Start file watcher in the foreground",
		RunE:  runWatch,
	}
}

func newWatchEnableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "enable",
		Short: "Enable the file watcher in configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			return updateWatcherEnabled(true)
		},
	}
}

func newWatchDisableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "disable",
		Short: "Disable the file watcher in configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			return updateWatcherEnabled(false)
		},
	}
}

func updateWatcherEnabled(enabled bool) error {
	path := cfgFile
	if path == "" {
		path = config.DefaultConfigPath()
	}

	cfg, err := config.Load(path)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	cfg.Watcher.Enabled = enabled

	if err := writeConfigTOML(path, cfg); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	statusStr := "disabled"
	if enabled {
		statusStr = "enabled"
	}

	printOutput(
		map[string]interface{}{
			"status":  "success",
			"enabled": enabled,
		},
		fmt.Sprintf("Watcher %s in configuration (%s).", statusStr, path),
	)

	return nil
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

	// Create server connection based on node role.
	serverConn, err := connection.NewServerConnection(cfg)
	if err != nil {
		fw.Stop()
		return fmt.Errorf("creating server connection: %w", err)
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

	// Resolve watch exclusions for status reporting.
	watchExcludes, _ := repo.ListWatchExcludes(ctx)

	fmt.Printf("File watcher started.\n")
	fmt.Printf("  Include paths:     %d\n", len(includePaths))
	fmt.Printf("  Watch exclusions:  %d\n", len(watchExcludes))
	fmt.Printf("  Exclude patterns:  %d\n", len(excludePatterns))
	fmt.Printf("  Debounce:          %dms\n", cfg.Watcher.DebounceMs)
	fmt.Printf("  Stability gate:    %ds\n", cfg.Watcher.StabilitySeconds)
	fmt.Printf("  Batch interval:    %s\n", batchInterval)
	fmt.Printf("  Storage:           %s\n", cfg.StorageDir())
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

	// Verify derived master key against key_verify if it exists
	verifyPath := filepath.Join(configDir, "key_verify")
	if verifyData, err := os.ReadFile(verifyPath); err == nil {
		if ok, err := enc.VerifyMasterKey(masterKey, string(verifyData)); err != nil || !ok {
			return nil, fmt.Errorf("invalid passphrase: key verification failed")
		}
	}

	return masterKey, nil
}

func newWatchAddCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "add [path...]",
		Short: "Remove one or more paths from the watch exclusion list (watches them again)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			repo, cleanup, err := openRepo()
			if err != nil {
				return err
			}
			defer cleanup()

			ctx := context.Background()
			var removed []string

			for _, p := range args {
				absPath, err := filepath.Abs(p)
				if err != nil {
					absPath = p
				}
				if err := repo.RemoveWatchExclude(ctx, absPath); err != nil {
					return fmt.Errorf("cannot remove watch exclusion %s: %w", absPath, err)
				}
				removed = append(removed, absPath)
			}

			printOutput(
				map[string]interface{}{"removed_exclusions": removed},
				fmt.Sprintf("Removed %d path(s) from watch exclusions.", len(removed)),
			)
			return nil
		},
	}
}

func newWatchRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove [path...]",
		Short: "Add one or more paths to the watch exclusion list (excludes them from watch)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			repo, cleanup, err := openRepo()
			if err != nil {
				return err
			}
			defer cleanup()

			ctx := context.Background()
			var added []string

			for _, p := range args {
				absPath, err := filepath.Abs(p)
				if err != nil {
					return fmt.Errorf("cannot resolve path %s: %w", p, err)
				}
				if err := repo.AddWatchExclude(ctx, absPath); err != nil {
					return fmt.Errorf("cannot add watch exclusion %s: %w", absPath, err)
				}
				added = append(added, absPath)
			}

			printOutput(
				map[string]interface{}{"added_exclusions": added},
				fmt.Sprintf("Added %d path(s) to watch exclusions.", len(added)),
			)
			return nil
		},
	}
}

func newWatchListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all watch exclusions",
		RunE: func(cmd *cobra.Command, args []string) error {
			repo, cleanup, err := openRepo()
			if err != nil {
				return err
			}
			defer cleanup()

			ctx := context.Background()
			excludes, err := repo.ListWatchExcludes(ctx)
			if err != nil {
				return fmt.Errorf("cannot list watch exclusions: %w", err)
			}

			sort.Strings(excludes)

			if jsonOut {
				printOutput(map[string]interface{}{
					"watch_exclusions": excludes,
				}, "")
			} else {
				fmt.Println("Watch Exclusions:")
				if len(excludes) == 0 {
					fmt.Println("  (none)")
				}
				for _, p := range excludes {
					fmt.Printf("  - %s\n", p)
				}
			}
			return nil
		},
	}
}
