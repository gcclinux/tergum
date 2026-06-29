package webui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"

	"github.com/BurntSushi/toml"
	"github.com/gcclinux/tergum/internal/config"
)

// handleAPIConfigSettings handles POST /api/config/settings.
// Accepts a JSON body with "key" and "value" fields, updates the corresponding
// config field in the TOML file, and syncs the in-memory config.
//
// Supported keys:
//   - server.address, server.command_port, server.data_port, server.bootstrap_port
//   - backup.storage_path, backup.chunk_size, backup.max_concurrent_uploads, backup.max_concurrent_downloads
//   - encryption.enabled
//   - webui.port, webui.session_timeout_hours, webui.username, webui.password
//   - logging.level, logging.format
//   - node.role, node.hostname, node.nat_mode
//   - watcher.debounce_ms, watcher.stability_seconds, watcher.batch_interval_minutes
//   - metrics.port, metrics.enabled
//   - scheduler.full_backup_cron, scheduler.auto_backup_cron
func (s *Server) handleAPIConfigSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.configPath == "" {
		http.Error(w, "configuration path not set", http.StatusBadRequest)
		return
	}

	var req struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Key == "" {
		http.Error(w, "key is required", http.StatusBadRequest)
		return
	}

	// Load current config from file.
	cfg, err := config.Load(s.configPath)
	if err != nil {
		s.logger.Error("config settings: cannot load config", "error", err)
		http.Error(w, "failed to load config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Apply the new value to the config.
	restartRequired, err := applyConfigValue(cfg, req.Key, req.Value)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Write back to file.
	f, err := os.OpenFile(s.configPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		s.logger.Error("config settings: cannot open config file", "error", err)
		http.Error(w, "failed to open config file: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer f.Close()

	encoder := toml.NewEncoder(f)
	if err := encoder.Encode(cfg); err != nil {
		s.logger.Error("config settings: cannot write config file", "error", err)
		http.Error(w, "failed to save config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Sync in-memory config.
	if s.fullCfg != nil {
		syncInMemoryConfig(s.fullCfg, cfg, req.Key)
	}

	s.logger.Info("config setting changed", "key", req.Key, "value", req.Value)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":           "success",
		"key":              req.Key,
		"value":            req.Value,
		"restart_required": restartRequired,
	})
}

// applyConfigValue sets the field identified by key to value on the config.
// Returns whether a restart is required, and an error for invalid keys or values.
func applyConfigValue(cfg *config.Config, key, value string) (bool, error) {
	restart := false

	switch key {
	// Node settings
	case "node.role":
		if value != "server" && value != "hybrid" && value != "client" {
			return false, fmt.Errorf("invalid role %q: must be server, hybrid, or client", value)
		}
		cfg.Node.Role = value
		restart = true
	case "node.hostname":
		cfg.Node.Hostname = value
		restart = true
	case "node.nat_mode":
		cfg.Node.NATMode = parseBool(value)

	// Server settings
	case "server.address":
		cfg.Server.Address = value
		restart = true
	case "server.command_port":
		v, err := strconv.Atoi(value)
		if err != nil || v < 1 || v > 65535 {
			return false, fmt.Errorf("invalid port %q: must be 1-65535", value)
		}
		cfg.Server.CommandPort = v
		restart = true
	case "server.data_port":
		v, err := strconv.Atoi(value)
		if err != nil || v < 1 || v > 65535 {
			return false, fmt.Errorf("invalid port %q: must be 1-65535", value)
		}
		cfg.Server.DataPort = v
		restart = true
	case "server.bootstrap_port":
		v, err := strconv.Atoi(value)
		if err != nil || v < 1 || v > 65535 {
			return false, fmt.Errorf("invalid port %q: must be 1-65535", value)
		}
		cfg.Server.BootstrapPort = v
		restart = true

	// Backup / Storage settings
	case "backup.storage_path":
		cfg.Backup.StoragePath = value
		restart = true
	case "backup.chunk_size":
		v, err := strconv.Atoi(value)
		if err != nil || v < 1024 {
			return false, fmt.Errorf("invalid chunk_size %q: must be at least 1024", value)
		}
		cfg.Backup.ChunkSize = v
	case "backup.max_concurrent_uploads":
		v, err := strconv.Atoi(value)
		if err != nil || v < 1 {
			return false, fmt.Errorf("invalid max_concurrent_uploads %q: must be >= 1", value)
		}
		cfg.Backup.MaxConcurrentUploads = v
	case "backup.max_concurrent_downloads":
		v, err := strconv.Atoi(value)
		if err != nil || v < 1 {
			return false, fmt.Errorf("invalid max_concurrent_downloads %q: must be >= 1", value)
		}
		cfg.Backup.MaxConcurrentDownloads = v

	// Encryption settings
	case "encryption.enabled":
		cfg.Encryption.Enabled = parseBool(value)
		restart = true

	// WebUI settings
	case "webui.port":
		v, err := strconv.Atoi(value)
		if err != nil || v < 1 || v > 65535 {
			return false, fmt.Errorf("invalid port %q: must be 1-65535", value)
		}
		cfg.WebUI.Port = v
		restart = true
	case "webui.session_timeout_hours":
		v, err := strconv.Atoi(value)
		if err != nil || v < 1 {
			return false, fmt.Errorf("invalid session_timeout_hours %q: must be >= 1", value)
		}
		cfg.WebUI.SessionTimeoutHours = v
	case "webui.username":
		if value == "" {
			return false, fmt.Errorf("username cannot be empty")
		}
		cfg.WebUI.Username = value
	case "webui.password":
		if value == "" {
			return false, fmt.Errorf("password cannot be empty")
		}
		cfg.WebUI.Password = value

	// Logging settings
	case "logging.level":
		switch value {
		case "debug", "info", "warn", "error":
			cfg.Logging.Level = value
		default:
			return false, fmt.Errorf("invalid log level %q: must be debug, info, warn, or error", value)
		}
	case "logging.format":
		switch value {
		case "text", "json":
			cfg.Logging.Format = value
		default:
			return false, fmt.Errorf("invalid log format %q: must be text or json", value)
		}

	// Watcher settings
	case "watcher.debounce_ms":
		v, err := strconv.Atoi(value)
		if err != nil || v < 0 {
			return false, fmt.Errorf("invalid debounce_ms %q: must be >= 0", value)
		}
		cfg.Watcher.DebounceMs = v
	case "watcher.stability_seconds":
		v, err := strconv.Atoi(value)
		if err != nil || v < 0 {
			return false, fmt.Errorf("invalid stability_seconds %q: must be >= 0", value)
		}
		cfg.Watcher.StabilitySeconds = v
	case "watcher.batch_interval_minutes":
		v, err := strconv.Atoi(value)
		if err != nil || v < 1 {
			return false, fmt.Errorf("invalid batch_interval_minutes %q: must be >= 1", value)
		}
		cfg.Watcher.BatchIntervalMinutes = v

	// Metrics settings
	case "metrics.enabled":
		cfg.Metrics.Enabled = parseBool(value)
	case "metrics.port":
		v, err := strconv.Atoi(value)
		if err != nil || v < 1 || v > 65535 {
			return false, fmt.Errorf("invalid port %q: must be 1-65535", value)
		}
		cfg.Metrics.Port = v
		restart = true

	// Scheduler settings
	case "scheduler.full_backup_cron":
		cfg.Scheduler.FullBackupCron = value
	case "scheduler.auto_backup_cron":
		cfg.Scheduler.AutoBackupCron = value

	default:
		return false, fmt.Errorf("unsupported config key %q", key)
	}

	return restart, nil
}

// syncInMemoryConfig copies the updated field from the newly loaded config to the
// running in-memory config. This avoids a full config reload.
func syncInMemoryConfig(live *config.Config, updated *config.Config, key string) {
	switch key {
	case "node.role":
		live.Node.Role = updated.Node.Role
	case "node.hostname":
		live.Node.Hostname = updated.Node.Hostname
	case "node.nat_mode":
		live.Node.NATMode = updated.Node.NATMode
	case "server.address":
		live.Server.Address = updated.Server.Address
	case "server.command_port":
		live.Server.CommandPort = updated.Server.CommandPort
	case "server.data_port":
		live.Server.DataPort = updated.Server.DataPort
	case "server.bootstrap_port":
		live.Server.BootstrapPort = updated.Server.BootstrapPort
	case "backup.storage_path":
		live.Backup.StoragePath = updated.Backup.StoragePath
	case "backup.chunk_size":
		live.Backup.ChunkSize = updated.Backup.ChunkSize
	case "backup.max_concurrent_uploads":
		live.Backup.MaxConcurrentUploads = updated.Backup.MaxConcurrentUploads
	case "backup.max_concurrent_downloads":
		live.Backup.MaxConcurrentDownloads = updated.Backup.MaxConcurrentDownloads
	case "encryption.enabled":
		live.Encryption.Enabled = updated.Encryption.Enabled
	case "webui.port":
		live.WebUI.Port = updated.WebUI.Port
	case "webui.session_timeout_hours":
		live.WebUI.SessionTimeoutHours = updated.WebUI.SessionTimeoutHours
	case "webui.username":
		live.WebUI.Username = updated.WebUI.Username
	case "webui.password":
		live.WebUI.Password = updated.WebUI.Password
	case "logging.level":
		live.Logging.Level = updated.Logging.Level
	case "logging.format":
		live.Logging.Format = updated.Logging.Format
	case "watcher.debounce_ms":
		live.Watcher.DebounceMs = updated.Watcher.DebounceMs
	case "watcher.stability_seconds":
		live.Watcher.StabilitySeconds = updated.Watcher.StabilitySeconds
	case "watcher.batch_interval_minutes":
		live.Watcher.BatchIntervalMinutes = updated.Watcher.BatchIntervalMinutes
	case "metrics.enabled":
		live.Metrics.Enabled = updated.Metrics.Enabled
	case "metrics.port":
		live.Metrics.Port = updated.Metrics.Port
	case "scheduler.full_backup_cron":
		live.Scheduler.FullBackupCron = updated.Scheduler.FullBackupCron
	case "scheduler.auto_backup_cron":
		live.Scheduler.AutoBackupCron = updated.Scheduler.AutoBackupCron
	}
}

// parseBool converts common boolean string representations.
func parseBool(s string) bool {
	switch s {
	case "true", "1", "yes", "on":
		return true
	default:
		return false
	}
}
