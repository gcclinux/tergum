package cmd

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/ricardopadilha/tergum/internal/config"
	"github.com/ricardopadilha/tergum/internal/crypto"
	"github.com/ricardopadilha/tergum/internal/db"
	"github.com/ricardopadilha/tergum/internal/observe"
	"github.com/ricardopadilha/tergum/internal/webui"
	"github.com/spf13/cobra"
)

func newAdminCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "admin",
		Short: "Start the web management UI only",
		Long: `Starts just the Web UI server without gRPC services, scheduler, or watcher.
Use this for browser-based management without running the full server.

If no config exists, the Web UI will show a setup wizard on first access.`,
		RunE: runAdmin,
	}

	cmd.Flags().IntP("port", "p", 0, "override web UI port")

	return cmd
}

func runAdmin(cmd *cobra.Command, args []string) error {
	portOverride, _ := cmd.Flags().GetInt("port")

	cfg, err := config.Load(cfgFile)
	if err != nil {
		// If config doesn't exist, use defaults so the admin panel can start.
		cfg = &config.Config{}
		cfg.WebUI.Enabled = true
		cfg.WebUI.Port = 7480
		cfg.WebUI.Username = "admin"
		cfg.WebUI.Password = "admin"
		cfg.WebUI.SessionTimeoutHours = 24
		cfg.Database.Path = config.DefaultConfigPath()
		cfg.Database.Path = config.DefaultConfigDir() + "/tergum.db"
		cfg.Database.WALMode = true
	}

	if portOverride > 0 {
		cfg.WebUI.Port = portOverride
	}

	// Ensure database exists.
	repo, err := db.NewRepository(cfg.Database.Path, true)
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}

	// Create web UI server.
	var opts []webui.ServerOption
	opts = append(opts, webui.WithLogger(observe.Logger("webui")))
	opts = append(opts, webui.WithRepository(repo))
	opts = append(opts, webui.WithConfigPath(config.DefaultConfigPath()))

	// Enable web-triggered backups if TERGUM_PASSPHRASE is set.
	if passphrase := os.Getenv("TERGUM_PASSPHRASE"); passphrase != "" {
		masterKey, err := deriveKeyFromEnv(cfg)
		if err == nil {
			trigger := webui.NewLocalBackupTrigger(repo, cfg.StorageDir(), masterKey, cfg.Encryption.Enabled)
			opts = append(opts, webui.WithBackupTrigger(trigger))
			fmt.Println("Web backup trigger enabled (TERGUM_PASSPHRASE set).")
		}
	}

	uiServer, err := webui.NewServer(
		cfg.WebUI,
		cfg.WebUI.Username,
		cfg.WebUI.Password,
		opts...,
	)
	if err != nil {
		repo.Close()
		return fmt.Errorf("creating web UI server: %w", err)
	}

	// Start in background.
	go func() {
		if err := uiServer.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "web UI error: %v\n", err)
		}
	}()

	fmt.Printf("Tergum Admin Panel running at http://localhost:%d\n", cfg.WebUI.Port)
	fmt.Printf("Login: %s / %s\n", cfg.WebUI.Username, cfg.WebUI.Password)
	fmt.Println("\nPress Ctrl+C to stop.")

	// Wait for shutdown signal.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	<-sigCh

	fmt.Println("\nShutting down...")
	ctx := context.Background()
	uiServer.Shutdown(ctx)
	repo.Close()

	return nil
}

// deriveKeyFromEnv derives master key from TERGUM_PASSPHRASE and stored salt.
func deriveKeyFromEnv(cfg *config.Config) ([]byte, error) {
	passphrase := os.Getenv("TERGUM_PASSPHRASE")
	if passphrase == "" {
		return nil, fmt.Errorf("TERGUM_PASSPHRASE not set")
	}

	configDir := filepath.Dir(cfg.Database.Path)
	saltPath := filepath.Join(configDir, "salt")

	saltHex, err := os.ReadFile(saltPath)
	if err != nil {
		return nil, fmt.Errorf("cannot read salt: %w", err)
	}

	salt, err := hex.DecodeString(strings.TrimSpace(string(saltHex)))
	if err != nil {
		return nil, fmt.Errorf("invalid salt: %w", err)
	}

	enc := crypto.NewEncryptor()
	return enc.DeriveKey(passphrase, salt)
}
