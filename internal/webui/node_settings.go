package webui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"github.com/BurntSushi/toml"
	"github.com/gcclinux/tergum/internal/config"
)

// handleAPINodeRole handles POST /api/config/node/role.
// Changes the node role between "server" and "hybrid" in the TOML config file.
func (s *Server) handleAPINodeRole(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.configPath == "" {
		http.Error(w, "configuration path not set", http.StatusBadRequest)
		return
	}

	var req struct {
		Role string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Validate role — only server and hybrid are allowed for this operation.
	if req.Role != "server" && req.Role != "hybrid" {
		http.Error(w, fmt.Sprintf("invalid role %q: must be \"server\" or \"hybrid\"", req.Role), http.StatusBadRequest)
		return
	}

	// Load current config.
	cfg, err := config.Load(s.configPath)
	if err != nil {
		s.logger.Error("node role: cannot load config", "error", err)
		http.Error(w, "failed to load config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	restartRequired := cfg.Node.Role != req.Role
	cfg.Node.Role = req.Role

	// Write back to file.
	f, err := os.OpenFile(s.configPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		s.logger.Error("node role: cannot open config file", "error", err)
		http.Error(w, "failed to open config file: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer f.Close()

	encoder := toml.NewEncoder(f)
	if err := encoder.Encode(cfg); err != nil {
		s.logger.Error("node role: cannot write config file", "error", err)
		http.Error(w, "failed to save config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Sync in-memory config.
	if s.fullCfg != nil {
		s.fullCfg.Node.Role = req.Role
	}

	if restartRequired {
		s.logger.Info("node role changed", "new_role", req.Role)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":           "success",
		"role":             req.Role,
		"restart_required": restartRequired,
	})
}

// handleAPINodeHostname handles POST /api/config/node/hostname.
// Sets or clears the node hostname used to identify the network interface.
func (s *Server) handleAPINodeHostname(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.configPath == "" {
		http.Error(w, "configuration path not set", http.StatusBadRequest)
		return
	}

	var req struct {
		Hostname string `json:"hostname"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Load current config.
	cfg, err := config.Load(s.configPath)
	if err != nil {
		s.logger.Error("node hostname: cannot load config", "error", err)
		http.Error(w, "failed to load config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	oldHostname := cfg.Node.Hostname
	cfg.Node.Hostname = req.Hostname

	// Write back to file.
	f, err := os.OpenFile(s.configPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		s.logger.Error("node hostname: cannot open config file", "error", err)
		http.Error(w, "failed to open config file: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer f.Close()

	encoder := toml.NewEncoder(f)
	if err := encoder.Encode(cfg); err != nil {
		s.logger.Error("node hostname: cannot write config file", "error", err)
		http.Error(w, "failed to save config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Sync in-memory config.
	if s.fullCfg != nil {
		s.fullCfg.Node.Hostname = req.Hostname
	}

	s.logger.Info("node hostname changed", "old_hostname", oldHostname, "new_hostname", req.Hostname)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":           "success",
		"hostname":         req.Hostname,
		"restart_required": true,
	})
}
