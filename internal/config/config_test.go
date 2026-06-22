package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/gcclinux/tergum/internal/model"
)

func TestApplyDefaults(t *testing.T) {
	cfg := &Config{}
	applyDefaults(cfg)

	if cfg.Node.Role != "client" {
		t.Errorf("expected node.role = \"client\", got %q", cfg.Node.Role)
	}
	if cfg.Server.CommandPort != 7400 {
		t.Errorf("expected server.command_port = 7400, got %d", cfg.Server.CommandPort)
	}
	if cfg.Server.DataPort != 7401 {
		t.Errorf("expected server.data_port = 7401, got %d", cfg.Server.DataPort)
	}
	if cfg.Client.MaxFileSize != "10GB" {
		t.Errorf("expected client.max_file_size = \"10GB\", got %q", cfg.Client.MaxFileSize)
	}
	if cfg.Database.WALMode != true {
		t.Errorf("expected database.wal_mode = true, got false")
	}
	if cfg.Backup.ChunkSize != 65536 {
		t.Errorf("expected backup.chunk_size = 65536, got %d", cfg.Backup.ChunkSize)
	}
	if cfg.Backup.MaxConcurrentUploads != 4 {
		t.Errorf("expected backup.max_concurrent_uploads = 4, got %d", cfg.Backup.MaxConcurrentUploads)
	}
	if cfg.Backup.MaxConcurrentDownloads != 8 {
		t.Errorf("expected backup.max_concurrent_downloads = 8, got %d", cfg.Backup.MaxConcurrentDownloads)
	}
	if cfg.Watcher.DebounceMs != 500 {
		t.Errorf("expected watcher.debounce_ms = 500, got %d", cfg.Watcher.DebounceMs)
	}
	if cfg.Watcher.StabilitySeconds != 60 {
		t.Errorf("expected watcher.stability_seconds = 60, got %d", cfg.Watcher.StabilitySeconds)
	}
	if cfg.Watcher.BatchIntervalMinutes != 5 {
		t.Errorf("expected watcher.batch_interval_minutes = 5, got %d", cfg.Watcher.BatchIntervalMinutes)
	}
	if cfg.WebUI.Port != 7480 {
		t.Errorf("expected webui.port = 7480, got %d", cfg.WebUI.Port)
	}
	if cfg.WebUI.SessionTimeoutHours != 24 {
		t.Errorf("expected webui.session_timeout_hours = 24, got %d", cfg.WebUI.SessionTimeoutHours)
	}
	if cfg.Metrics.Port != 7490 {
		t.Errorf("expected metrics.port = 7490, got %d", cfg.Metrics.Port)
	}
	if cfg.Logging.Level != "info" {
		t.Errorf("expected logging.level = \"info\", got %q", cfg.Logging.Level)
	}
	if cfg.Logging.Format != "text" {
		t.Errorf("expected logging.format = \"text\", got %q", cfg.Logging.Format)
	}
}

func TestLoadValidConfig(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "tergum.toml")

	content := `
[node]
role = "server"
hostname = "myhost"

[server]
address = "10.0.0.1"
command_port = 8400
data_port = 8401

[client]
include_paths = ["/home/user/docs"]
exclude_patterns = ["*.tmp"]
max_file_size = "5GB"

[logging]
level = "debug"
format = "json"
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Node.Role != "server" {
		t.Errorf("expected role = \"server\", got %q", cfg.Node.Role)
	}
	if cfg.Node.Hostname != "myhost" {
		t.Errorf("expected hostname = \"myhost\", got %q", cfg.Node.Hostname)
	}
	if cfg.Server.Address != "10.0.0.1" {
		t.Errorf("expected address = \"10.0.0.1\", got %q", cfg.Server.Address)
	}
	if cfg.Server.CommandPort != 8400 {
		t.Errorf("expected command_port = 8400, got %d", cfg.Server.CommandPort)
	}
	if cfg.Server.DataPort != 8401 {
		t.Errorf("expected data_port = 8401, got %d", cfg.Server.DataPort)
	}
	if len(cfg.Client.IncludePaths) != 1 || cfg.Client.IncludePaths[0] != "/home/user/docs" {
		t.Errorf("unexpected include_paths: %v", cfg.Client.IncludePaths)
	}
	if cfg.Logging.Level != "debug" {
		t.Errorf("expected logging.level = \"debug\", got %q", cfg.Logging.Level)
	}
	if cfg.Logging.Format != "json" {
		t.Errorf("expected logging.format = \"json\", got %q", cfg.Logging.Format)
	}
	// Defaults should be preserved for unspecified sections
	if cfg.Backup.ChunkSize != 65536 {
		t.Errorf("expected default chunk_size = 65536, got %d", cfg.Backup.ChunkSize)
	}
	if cfg.Metrics.Port != 7490 {
		t.Errorf("expected default metrics.port = 7490, got %d", cfg.Metrics.Port)
	}
}

func TestLoadPartialConfig_DefaultsPreserved(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "tergum.toml")

	// Minimal config: only set server address (required for client role)
	content := `
[server]
address = "192.168.1.1"
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// All defaults should be applied
	if cfg.Node.Role != "client" {
		t.Errorf("expected default role \"client\", got %q", cfg.Node.Role)
	}
	if cfg.Server.CommandPort != 7400 {
		t.Errorf("expected default command_port 7400, got %d", cfg.Server.CommandPort)
	}
	if cfg.Server.DataPort != 7401 {
		t.Errorf("expected default data_port 7401, got %d", cfg.Server.DataPort)
	}
	if cfg.Client.MaxFileSize != "10GB" {
		t.Errorf("expected default max_file_size \"10GB\", got %q", cfg.Client.MaxFileSize)
	}
	if cfg.Database.WALMode != true {
		t.Error("expected default wal_mode true")
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load("/nonexistent/path/tergum.toml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	cfgErr, ok := err.(*model.ConfigError)
	if !ok {
		t.Fatalf("expected *model.ConfigError, got %T", err)
	}
	if cfgErr.ExitCode() != model.ExitConfigError {
		t.Errorf("expected exit code %d, got %d", model.ExitConfigError, cfgErr.ExitCode())
	}
}

func TestLoadInvalidTOML(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "tergum.toml")

	content := `[node
role = "broken"`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(cfgPath)
	if err == nil {
		t.Fatal("expected error for invalid TOML")
	}
	if _, ok := err.(*model.ConfigError); !ok {
		t.Fatalf("expected *model.ConfigError, got %T", err)
	}
}

func TestValidate_ValidServerConfig(t *testing.T) {
	cfg := &Config{}
	applyDefaults(cfg)
	cfg.Node.Role = "server"

	if err := cfg.Validate(); err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
}

func TestValidate_ValidClientConfig(t *testing.T) {
	cfg := &Config{}
	applyDefaults(cfg)
	cfg.Node.Role = "client"
	cfg.Server.Address = "192.168.1.5"

	if err := cfg.Validate(); err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
}

func TestValidate_InvalidRole(t *testing.T) {
	cfg := &Config{}
	applyDefaults(cfg)
	cfg.Node.Role = "invalid"
	cfg.Server.Address = "192.168.1.5"

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error for invalid role")
	}
	cfgErr, ok := err.(*model.ConfigError)
	if !ok {
		t.Fatalf("expected *model.ConfigError, got %T", err)
	}
	if cfgErr.ExitCode() != model.ExitConfigError {
		t.Errorf("expected exit code %d, got %d", model.ExitConfigError, cfgErr.ExitCode())
	}
}

func TestValidate_ClientRequiresServerAddress(t *testing.T) {
	cfg := &Config{}
	applyDefaults(cfg)
	cfg.Node.Role = "client"
	cfg.Server.Address = "" // Missing!

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error for missing server.address")
	}
}

func TestValidate_InvalidLoggingLevel(t *testing.T) {
	cfg := &Config{}
	applyDefaults(cfg)
	cfg.Node.Role = "server"
	cfg.Logging.Level = "verbose"

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error for invalid logging.level")
	}
}

func TestValidate_InvalidLoggingFormat(t *testing.T) {
	cfg := &Config{}
	applyDefaults(cfg)
	cfg.Node.Role = "server"
	cfg.Logging.Format = "yaml"

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error for invalid logging.format")
	}
}

func TestValidate_PartialTLS(t *testing.T) {
	cfg := &Config{}
	applyDefaults(cfg)
	cfg.Node.Role = "server"
	cfg.TLS.CACert = "/path/to/ca.crt"
	// cert and key missing

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error for partial TLS config")
	}
}

func TestValidate_CompleteTLS(t *testing.T) {
	cfg := &Config{}
	applyDefaults(cfg)
	cfg.Node.Role = "server"
	cfg.TLS.CACert = "/path/to/ca.crt"
	cfg.TLS.Cert = "/path/to/cert.crt"
	cfg.TLS.Key = "/path/to/key.pem"

	if err := cfg.Validate(); err != nil {
		t.Errorf("expected no error for complete TLS config, got: %v", err)
	}
}

func TestParseMaxFileSize(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
		wantErr  bool
	}{
		{"10GB", 10 * 1024 * 1024 * 1024, false},
		{"500MB", 500 * 1024 * 1024, false},
		{"1TB", 1024 * 1024 * 1024 * 1024, false},
		{"256KB", 256 * 1024, false},
		{"100B", 100, false},
		{"65536", 65536, false},
		{"10gb", 10 * 1024 * 1024 * 1024, false}, // case insensitive
		{"", 0, true},
		{"abc", 0, true},
		{"10XB", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := ParseMaxFileSize(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error for input %q", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for input %q: %v", tt.input, err)
			}
			if result != tt.expected {
				t.Errorf("ParseMaxFileSize(%q) = %d, want %d", tt.input, result, tt.expected)
			}
		})
	}
}

func TestDefaultConfigDir(t *testing.T) {
	dir := DefaultConfigDir()
	if dir == "" {
		t.Fatal("DefaultConfigDir() returned empty string")
	}

	switch runtime.GOOS {
	case "darwin":
		if !filepath.IsAbs(dir) {
			t.Errorf("expected absolute path, got %q", dir)
		}
		if !contains(dir, "Library") {
			t.Errorf("expected macOS path containing 'Library', got %q", dir)
		}
	case "windows":
		if !filepath.IsAbs(dir) {
			t.Errorf("expected absolute path, got %q", dir)
		}
		if !contains(dir, "tergum") {
			t.Errorf("expected path containing 'tergum', got %q", dir)
		}
	default: // linux
		if !filepath.IsAbs(dir) {
			t.Errorf("expected absolute path, got %q", dir)
		}
		if !contains(dir, ".config") {
			t.Errorf("expected Linux path containing '.config', got %q", dir)
		}
	}
}

func TestDefaultConfigPath(t *testing.T) {
	p := DefaultConfigPath()
	if !filepath.IsAbs(p) {
		t.Errorf("expected absolute path, got %q", p)
	}
	if filepath.Base(p) != "tergum.toml" {
		t.Errorf("expected filename 'tergum.toml', got %q", filepath.Base(p))
	}
}

func TestValidate_MultipleErrors(t *testing.T) {
	cfg := &Config{}
	applyDefaults(cfg)
	cfg.Node.Role = "invalid"
	cfg.Logging.Level = "verbose"
	cfg.Logging.Format = "xml"

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
	// Should report multiple errors in one message
	msg := err.Error()
	if !contains(msg, "node.role") {
		t.Errorf("expected error to mention node.role, got: %s", msg)
	}
	if !contains(msg, "logging.level") {
		t.Errorf("expected error to mention logging.level, got: %s", msg)
	}
	if !contains(msg, "logging.format") {
		t.Errorf("expected error to mention logging.format, got: %s", msg)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
