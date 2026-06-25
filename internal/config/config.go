// Package config handles loading, defaulting, and validating Tergum TOML configuration.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/gcclinux/tergum/internal/model"
)

// Config is the top-level configuration structure for Tergum.
type Config struct {
	Node       NodeConfig       `toml:"node"`
	Server     ServerConfig     `toml:"server"`
	Client     ClientConfig     `toml:"client"`
	TLS        TLSConfig        `toml:"tls"`
	Encryption EncryptionConfig `toml:"encryption"`
	Database   DatabaseConfig   `toml:"database"`
	Backup     BackupConfig     `toml:"backup"`
	Watcher    WatcherConfig    `toml:"watcher"`
	WebUI      WebUIConfig      `toml:"webui"`
	Metrics    MetricsConfig    `toml:"metrics"`
	Logging    LoggingConfig    `toml:"logging"`
	Scheduler  SchedulerConfig  `toml:"scheduler"`
}

// NodeConfig defines the role and identity of this Tergum instance.
type NodeConfig struct {
	Role     string `toml:"role"`
	Hostname string `toml:"hostname"`
}

// ServerConfig defines connection parameters for the Tergum server.
type ServerConfig struct {
	Address       string `toml:"address"`
	CommandPort   int    `toml:"command_port"`
	DataPort      int    `toml:"data_port"`
	BootstrapPort int    `toml:"bootstrap_port"`
}

// ClientConfig defines backup source paths and filters.
type ClientConfig struct {
	IncludePaths    []string `toml:"include_paths"`
	ExcludePatterns []string `toml:"exclude_patterns"`
	MaxFileSize     string   `toml:"max_file_size"`
}

// TLSConfig defines paths to TLS certificates for mTLS.
type TLSConfig struct {
	CACert string `toml:"ca_cert"`
	Cert   string `toml:"cert"`
	Key    string `toml:"key"`
}

// EncryptionConfig controls at-rest encryption behavior.
type EncryptionConfig struct {
	Enabled bool `toml:"enabled"`
}

// DatabaseConfig defines the SQLite database location and settings.
type DatabaseConfig struct {
	Path    string `toml:"path"`
	WALMode bool   `toml:"wal_mode"`
}

// BackupConfig defines chunking and concurrency parameters.
type BackupConfig struct {
	ChunkSize              int    `toml:"chunk_size"`
	StoragePath            string `toml:"storage_path"`
	MaxConcurrentUploads   int    `toml:"max_concurrent_uploads"`
	MaxConcurrentDownloads int    `toml:"max_concurrent_downloads"`
}

// WatcherConfig defines file watching behavior.
type WatcherConfig struct {
	Enabled              bool `toml:"enabled"`
	DebounceMs           int  `toml:"debounce_ms"`
	StabilitySeconds     int  `toml:"stability_seconds"`
	OngoingBackup        bool `toml:"ongoing_backup"`
	BatchIntervalMinutes int  `toml:"batch_interval_minutes"`
}

// WebUIConfig defines the embedded web management interface settings.
type WebUIConfig struct {
	Enabled             bool   `toml:"enabled"`
	Port                int    `toml:"port"`
	SessionTimeoutHours int    `toml:"session_timeout_hours"`
	Username            string `toml:"username"`
	Password            string `toml:"password"`
}

// MetricsConfig defines the Prometheus metrics endpoint.
type MetricsConfig struct {
	Enabled bool `toml:"enabled"`
	Port    int  `toml:"port"`
}

// LoggingConfig defines log output behavior.
type LoggingConfig struct {
	Level  string `toml:"level"`
	Format string `toml:"format"`
}

// SchedulerConfig defines cron expressions for scheduled backups.
type SchedulerConfig struct {
	FullBackupCron string `toml:"full_backup_cron"`
	AutoBackupCron string `toml:"auto_backup_cron"`
}

// DefaultConfigDir returns the platform-specific default configuration directory.
func DefaultConfigDir() string {
	switch runtime.GOOS {
	case "darwin":
		home, _ := os.UserHomeDir()
		return filepath.Join(home, "Library", "Application Support", "tergum")
	case "windows":
		appData := os.Getenv("APPDATA")
		if appData == "" {
			home, _ := os.UserHomeDir()
			appData = filepath.Join(home, "AppData", "Roaming")
		}
		return filepath.Join(appData, "tergum")
	default: // linux and others
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".config", "tergum")
	}
}

// DefaultConfigPath returns the full path to the default configuration file.
func DefaultConfigPath() string {
	return filepath.Join(DefaultConfigDir(), "tergum.toml")
}

// Load reads a TOML configuration file from the given path and applies defaults
// for any unspecified values. If path is empty, the platform-specific default
// path is used.
func Load(path string) (*Config, error) {
	if path == "" {
		path = DefaultConfigPath()
	}

	cfg := &Config{}
	applyDefaults(cfg)

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, &model.ConfigError{Message: fmt.Sprintf("cannot read config file: %v", err)}
	}

	if _, err := toml.Decode(string(data), cfg); err != nil {
		return nil, &model.ConfigError{Message: fmt.Sprintf("cannot parse config file: %v", err)}
	}

	return cfg, nil
}

// applyDefaults sets all default values on a Config struct.
func applyDefaults(cfg *Config) {
	cfg.Node.Role = "client"

	cfg.Server.CommandPort = 7400
	cfg.Server.DataPort = 7401
	cfg.Server.BootstrapPort = 7402

	cfg.Client.MaxFileSize = "10GB"

	cfg.Database.WALMode = true

	cfg.Backup.ChunkSize = 65536
	cfg.Backup.MaxConcurrentUploads = 4
	cfg.Backup.MaxConcurrentDownloads = 8

	cfg.Watcher.DebounceMs = 500
	cfg.Watcher.StabilitySeconds = 60
	cfg.Watcher.BatchIntervalMinutes = 5

	cfg.WebUI.Port = 7480
	cfg.WebUI.SessionTimeoutHours = 24
	cfg.WebUI.Username = "admin"
	cfg.WebUI.Password = "admin"

	cfg.Metrics.Port = 7490

	cfg.Logging.Level = "info"
	cfg.Logging.Format = "text"
}

// StorageDir returns the CAS storage directory path.
// If backup.storage_path is set in config, that is used.
// Otherwise falls back to a "storage" directory sibling to the database file.
func (c *Config) StorageDir() string {
	if c.Backup.StoragePath != "" {
		return c.Backup.StoragePath
	}
	return filepath.Join(filepath.Dir(c.Database.Path), "storage")
}

// Validate checks the configuration for logical errors and returns a ConfigError
// (exit code 2) if any validation rule is violated.
func (c *Config) Validate() error {
	var errs []string

	// node.role must be one of "client", "server", "hybrid"
	switch c.Node.Role {
	case "client", "server", "hybrid":
		// valid
	default:
		errs = append(errs, fmt.Sprintf("node.role must be one of \"client\", \"server\", \"hybrid\"; got %q", c.Node.Role))
	}

	// server.address is required when role is "client" or "hybrid" (need server to connect to)
	if c.Node.Role == "client" {
		if c.Server.Address == "" {
			errs = append(errs, "server.address is required when node.role is \"client\"")
		}
	}

	// TLS paths should be non-empty when TLS is used
	if c.TLS.CACert != "" || c.TLS.Cert != "" || c.TLS.Key != "" {
		if c.TLS.CACert == "" {
			errs = append(errs, "tls.ca_cert must not be empty when TLS is configured")
		}
		if c.TLS.Cert == "" {
			errs = append(errs, "tls.cert must not be empty when TLS is configured")
		}
		if c.TLS.Key == "" {
			errs = append(errs, "tls.key must not be empty when TLS is configured")
		}
	}

	// logging.level must be one of "debug", "info", "warn", "error"
	switch c.Logging.Level {
	case "debug", "info", "warn", "error":
		// valid
	default:
		errs = append(errs, fmt.Sprintf("logging.level must be one of \"debug\", \"info\", \"warn\", \"error\"; got %q", c.Logging.Level))
	}

	// logging.format must be one of "text", "json"
	switch c.Logging.Format {
	case "text", "json":
		// valid
	default:
		errs = append(errs, fmt.Sprintf("logging.format must be one of \"text\", \"json\"; got %q", c.Logging.Format))
	}

	// webui credentials are required when webui is enabled
	if c.WebUI.Enabled {
		if c.WebUI.Username == "" {
			errs = append(errs, "webui.username must not be empty when webui is enabled")
		}
		if c.WebUI.Password == "" {
			errs = append(errs, "webui.password must not be empty when webui is enabled")
		}
	}

	if len(errs) > 0 {
		return &model.ConfigError{Message: strings.Join(errs, "; ")}
	}
	return nil
}

// ParseMaxFileSize parses a human-readable file size string (e.g., "10GB", "500MB")
// and returns the size in bytes. Supports KB, MB, GB, TB suffixes (case-insensitive).
func ParseMaxFileSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty file size string")
	}

	s = strings.ToUpper(s)

	// Check suffixes in order from longest to shortest to avoid "B" matching "MB", etc.
	type suffixMult struct {
		suffix string
		mult   int64
	}
	suffixes := []suffixMult{
		{"TB", 1024 * 1024 * 1024 * 1024},
		{"GB", 1024 * 1024 * 1024},
		{"MB", 1024 * 1024},
		{"KB", 1024},
		{"B", 1},
	}

	for _, sm := range suffixes {
		if strings.HasSuffix(s, sm.suffix) {
			numStr := strings.TrimSuffix(s, sm.suffix)
			numStr = strings.TrimSpace(numStr)
			val, err := strconv.ParseFloat(numStr, 64)
			if err != nil {
				return 0, fmt.Errorf("invalid file size %q: %v", s, err)
			}
			return int64(val * float64(sm.mult)), nil
		}
	}

	// Try parsing as plain bytes
	val, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid file size %q: must have a suffix (B, KB, MB, GB, TB) or be a plain integer", s)
	}
	return val, nil
}
