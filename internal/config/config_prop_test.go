package config

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/ricardopadilha/tergum/internal/model"
	"pgregory.net/rapid"
)

// **Validates: Requirements 15.3, 15.4**

// TestProperty_PartialConfigDefaultsApplied generates random partial TOML
// configurations and verifies that documented defaults are filled correctly
// for any unspecified values.
func TestProperty_PartialConfigDefaultsApplied(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Randomly decide which sections to include
		includeNode := rapid.Bool().Draw(rt, "includeNode")
		includeServer := rapid.Bool().Draw(rt, "includeServer")
		includeClient := rapid.Bool().Draw(rt, "includeClient")
		includeBackup := rapid.Bool().Draw(rt, "includeBackup")
		includeWatcher := rapid.Bool().Draw(rt, "includeWatcher")
		includeWebUI := rapid.Bool().Draw(rt, "includeWebUI")
		includeMetrics := rapid.Bool().Draw(rt, "includeMetrics")
		includeLogging := rapid.Bool().Draw(rt, "includeLogging")
		includeDatabase := rapid.Bool().Draw(rt, "includeDatabase")

		// Build partial TOML
		var tomlContent string

		// Determine role
		var role string
		if includeNode {
			role = rapid.SampledFrom([]string{"client", "server", "both"}).Draw(rt, "role")
			tomlContent += fmt.Sprintf("[node]\nrole = %q\n\n", role)
		} else {
			role = "client" // default
		}

		// Determine if we need server.address for validation to pass
		needsServerAddr := (role == "client" || role == "both")

		if includeServer {
			// Generate random valid server section
			cmdPort := rapid.IntRange(1024, 65535).Draw(rt, "cmdPort")
			dataPort := rapid.IntRange(1024, 65535).Draw(rt, "dataPort")
			addr := rapid.SampledFrom([]string{"10.0.0.1", "192.168.1.5", "myserver.local"}).Draw(rt, "serverAddr")
			tomlContent += fmt.Sprintf("[server]\naddress = %q\ncommand_port = %d\ndata_port = %d\n\n", addr, cmdPort, dataPort)
		} else if needsServerAddr {
			// Must provide server.address for client/both role to pass validation
			tomlContent += "[server]\naddress = \"192.168.1.100\"\n\n"
			includeServer = true // mark as included so we don't check defaults
		}

		if includeClient {
			maxSize := rapid.SampledFrom([]string{"1GB", "5GB", "500MB", "10GB"}).Draw(rt, "maxFileSize")
			tomlContent += fmt.Sprintf("[client]\nmax_file_size = %q\n\n", maxSize)
		}

		if includeBackup {
			chunkSize := rapid.SampledFrom([]int{32768, 65536, 131072}).Draw(rt, "chunkSize")
			uploads := rapid.IntRange(1, 16).Draw(rt, "uploads")
			downloads := rapid.IntRange(1, 16).Draw(rt, "downloads")
			tomlContent += fmt.Sprintf("[backup]\nchunk_size = %d\nmax_concurrent_uploads = %d\nmax_concurrent_downloads = %d\n\n", chunkSize, uploads, downloads)
		}

		if includeWatcher {
			debounce := rapid.IntRange(100, 2000).Draw(rt, "debounce")
			stability := rapid.IntRange(10, 300).Draw(rt, "stability")
			batch := rapid.IntRange(1, 30).Draw(rt, "batch")
			tomlContent += fmt.Sprintf("[watcher]\ndebounce_ms = %d\nstability_seconds = %d\nbatch_interval_minutes = %d\n\n", debounce, stability, batch)
		}

		if includeWebUI {
			port := rapid.IntRange(1024, 65535).Draw(rt, "webuiPort")
			timeout := rapid.IntRange(1, 168).Draw(rt, "sessionTimeout")
			tomlContent += fmt.Sprintf("[webui]\nport = %d\nsession_timeout_hours = %d\n\n", port, timeout)
		}

		if includeMetrics {
			port := rapid.IntRange(1024, 65535).Draw(rt, "metricsPort")
			tomlContent += fmt.Sprintf("[metrics]\nport = %d\n\n", port)
		}

		if includeLogging {
			level := rapid.SampledFrom([]string{"debug", "info", "warn", "error"}).Draw(rt, "level")
			format := rapid.SampledFrom([]string{"text", "json"}).Draw(rt, "format")
			tomlContent += fmt.Sprintf("[logging]\nlevel = %q\nformat = %q\n\n", level, format)
		}

		if includeDatabase {
			walMode := rapid.Bool().Draw(rt, "walMode")
			tomlContent += fmt.Sprintf("[database]\nwal_mode = %v\n\n", walMode)
		}

		// Write to temp file
		dir, err := os.MkdirTemp("", "tergum-prop-test-*")
		if err != nil {
			rt.Fatalf("failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(dir)

		cfgPath := filepath.Join(dir, "tergum.toml")
		if err := os.WriteFile(cfgPath, []byte(tomlContent), 0644); err != nil {
			rt.Fatalf("failed to write config: %v", err)
		}

		// Load config
		cfg, err := Load(cfgPath)
		if err != nil {
			rt.Fatalf("Load failed for valid partial config: %v\nContent:\n%s", err, tomlContent)
		}

		// Verify defaults are applied for any unspecified sections
		if !includeNode {
			if cfg.Node.Role != "client" {
				rt.Errorf("expected default node.role = \"client\", got %q", cfg.Node.Role)
			}
		}

		if !includeClient {
			if cfg.Client.MaxFileSize != "10GB" {
				rt.Errorf("expected default client.max_file_size = \"10GB\", got %q", cfg.Client.MaxFileSize)
			}
		}

		if !includeBackup {
			if cfg.Backup.ChunkSize != 65536 {
				rt.Errorf("expected default backup.chunk_size = 65536, got %d", cfg.Backup.ChunkSize)
			}
			if cfg.Backup.MaxConcurrentUploads != 4 {
				rt.Errorf("expected default backup.max_concurrent_uploads = 4, got %d", cfg.Backup.MaxConcurrentUploads)
			}
			if cfg.Backup.MaxConcurrentDownloads != 8 {
				rt.Errorf("expected default backup.max_concurrent_downloads = 8, got %d", cfg.Backup.MaxConcurrentDownloads)
			}
		}

		if !includeWatcher {
			if cfg.Watcher.DebounceMs != 500 {
				rt.Errorf("expected default watcher.debounce_ms = 500, got %d", cfg.Watcher.DebounceMs)
			}
			if cfg.Watcher.StabilitySeconds != 60 {
				rt.Errorf("expected default watcher.stability_seconds = 60, got %d", cfg.Watcher.StabilitySeconds)
			}
			if cfg.Watcher.BatchIntervalMinutes != 5 {
				rt.Errorf("expected default watcher.batch_interval_minutes = 5, got %d", cfg.Watcher.BatchIntervalMinutes)
			}
		}

		if !includeWebUI {
			if cfg.WebUI.Port != 7480 {
				rt.Errorf("expected default webui.port = 7480, got %d", cfg.WebUI.Port)
			}
			if cfg.WebUI.SessionTimeoutHours != 24 {
				rt.Errorf("expected default webui.session_timeout_hours = 24, got %d", cfg.WebUI.SessionTimeoutHours)
			}
		}

		if !includeMetrics {
			if cfg.Metrics.Port != 7490 {
				rt.Errorf("expected default metrics.port = 7490, got %d", cfg.Metrics.Port)
			}
		}

		if !includeLogging {
			if cfg.Logging.Level != "info" {
				rt.Errorf("expected default logging.level = \"info\", got %q", cfg.Logging.Level)
			}
			if cfg.Logging.Format != "text" {
				rt.Errorf("expected default logging.format = \"text\", got %q", cfg.Logging.Format)
			}
		}

		if !includeDatabase {
			if cfg.Database.WALMode != true {
				rt.Errorf("expected default database.wal_mode = true, got false")
			}
		}
	})
}

// TestProperty_InvalidConfigReturnsConfigError generates configs with invalid or
// missing required values and verifies that validation returns a *model.ConfigError
// with exit code 2.
func TestProperty_InvalidConfigReturnsConfigError(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Choose which type of invalid config to generate
		invalidType := rapid.IntRange(0, 3).Draw(rt, "invalidType")

		var tomlContent string

		switch invalidType {
		case 0:
			// Invalid role
			invalidRole := rapid.StringMatching(`[a-z]{3,10}`).Draw(rt, "invalidRole")
			// Ensure it's not a valid role
			if invalidRole == "client" || invalidRole == "server" || invalidRole == "both" {
				invalidRole = "invalid_role"
			}
			tomlContent = fmt.Sprintf("[node]\nrole = %q\n\n[server]\naddress = \"10.0.0.1\"\n", invalidRole)

		case 1:
			// Invalid logging level
			invalidLevel := rapid.StringMatching(`[a-z]{3,10}`).Draw(rt, "invalidLevel")
			if invalidLevel == "debug" || invalidLevel == "info" || invalidLevel == "warn" || invalidLevel == "error" {
				invalidLevel = "verbose"
			}
			tomlContent = fmt.Sprintf("[server]\naddress = \"10.0.0.1\"\n\n[logging]\nlevel = %q\n", invalidLevel)

		case 2:
			// Invalid logging format
			invalidFormat := rapid.StringMatching(`[a-z]{3,8}`).Draw(rt, "invalidFormat")
			if invalidFormat == "text" || invalidFormat == "json" {
				invalidFormat = "yaml"
			}
			tomlContent = fmt.Sprintf("[server]\naddress = \"10.0.0.1\"\n\n[logging]\nformat = %q\n", invalidFormat)

		case 3:
			// Client role with missing server.address
			tomlContent = "[node]\nrole = \"client\"\n"
		}

		// Write to temp file
		dir, err := os.MkdirTemp("", "tergum-prop-test-*")
		if err != nil {
			rt.Fatalf("failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(dir)

		cfgPath := filepath.Join(dir, "tergum.toml")
		if err := os.WriteFile(cfgPath, []byte(tomlContent), 0644); err != nil {
			rt.Fatalf("failed to write config: %v", err)
		}

		// Load config (should succeed — parsing is valid TOML)
		cfg, err := Load(cfgPath)
		if err != nil {
			rt.Fatalf("Load failed unexpectedly: %v\nContent:\n%s", err, tomlContent)
		}

		// Validate should fail with ConfigError
		err = cfg.Validate()
		if err == nil {
			rt.Fatalf("expected validation error for invalid config, got nil\nContent:\n%s", tomlContent)
		}

		cfgErr, ok := err.(*model.ConfigError)
		if !ok {
			rt.Fatalf("expected *model.ConfigError, got %T: %v", err, err)
		}

		if cfgErr.ExitCode() != model.ExitConfigError {
			rt.Errorf("expected exit code %d, got %d", model.ExitConfigError, cfgErr.ExitCode())
		}
	})
}

// TestProperty_ClientRoleMissingServerAddress generates client-role configs
// without server.address and verifies validation fails with exit code 2.
func TestProperty_ClientRoleMissingServerAddress(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate random valid values for other fields but omit server.address
		var tomlContent string
		tomlContent += "[node]\nrole = \"client\"\n\n"

		// Optionally include other sections
		if rapid.Bool().Draw(rt, "includeLogging") {
			level := rapid.SampledFrom([]string{"debug", "info", "warn", "error"}).Draw(rt, "level")
			format := rapid.SampledFrom([]string{"text", "json"}).Draw(rt, "format")
			tomlContent += fmt.Sprintf("[logging]\nlevel = %q\nformat = %q\n\n", level, format)
		}

		if rapid.Bool().Draw(rt, "includeBackup") {
			chunkSize := rapid.SampledFrom([]int{32768, 65536, 131072}).Draw(rt, "chunkSize")
			tomlContent += fmt.Sprintf("[backup]\nchunk_size = %d\n\n", chunkSize)
		}

		if rapid.Bool().Draw(rt, "includeWatcher") {
			debounce := rapid.IntRange(100, 2000).Draw(rt, "debounce")
			tomlContent += fmt.Sprintf("[watcher]\ndebounce_ms = %d\n\n", debounce)
		}

		// Write to temp file
		dir, err := os.MkdirTemp("", "tergum-prop-test-*")
		if err != nil {
			rt.Fatalf("failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(dir)

		cfgPath := filepath.Join(dir, "tergum.toml")
		if err := os.WriteFile(cfgPath, []byte(tomlContent), 0644); err != nil {
			rt.Fatalf("failed to write config: %v", err)
		}

		// Load should succeed (valid TOML)
		cfg, err := Load(cfgPath)
		if err != nil {
			rt.Fatalf("Load failed unexpectedly: %v\nContent:\n%s", err, tomlContent)
		}

		// Validate should fail because server.address is missing for client role
		err = cfg.Validate()
		if err == nil {
			rt.Fatalf("expected validation error for client role without server.address\nContent:\n%s", tomlContent)
		}

		cfgErr, ok := err.(*model.ConfigError)
		if !ok {
			rt.Fatalf("expected *model.ConfigError, got %T: %v", err, err)
		}

		if cfgErr.ExitCode() != model.ExitConfigError {
			rt.Errorf("expected exit code %d, got %d", model.ExitConfigError, cfgErr.ExitCode())
		}
	})
}
