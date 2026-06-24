package cmd

import (
	"path/filepath"
	"testing"

	"github.com/gcclinux/tergum/internal/config"
)

func TestWatchSubcommands(t *testing.T) {
	// Backup original global variables
	origCfgFile := cfgFile
	origJsonOut := jsonOut
	defer func() {
		cfgFile = origCfgFile
		jsonOut = origJsonOut
	}()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "tergum.toml")

	// Write an initial config file where watcher is disabled
	cfg := buildConfig("client", "localhost", "", filepath.Join(tmpDir, "storage"), filepath.Join(tmpDir, "certs"), tmpDir, false)
	cfg.Watcher.Enabled = false
	if err := writeConfigTOML(configPath, cfg); err != nil {
		t.Fatalf("failed to write initial config: %v", err)
	}

	// 1. Enable watcher
	cfgFile = "" // Reset parsed global flag variable
	rootCmd.SetArgs([]string{"watch", "enable", "--config", configPath})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("watch enable failed: %v", err)
	}

	// Read and verify config
	loaded, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("failed to load config after enable: %v", err)
	}
	if !loaded.Watcher.Enabled {
		t.Error("expected watcher to be enabled in config")
	}

	// 2. Disable watcher
	cfgFile = "" // Reset parsed global flag variable
	rootCmd.SetArgs([]string{"watch", "disable", "--config", configPath})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("watch disable failed: %v", err)
	}

	// Read and verify config
	loaded, err = config.Load(configPath)
	if err != nil {
		t.Fatalf("failed to load config after disable: %v", err)
	}
	if loaded.Watcher.Enabled {
		t.Error("expected watcher to be disabled in config")
	}
}
