package webui

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/gcclinux/tergum/internal/config"
)

// adminClientView is the JSON shape returned for a configured admin client.
type adminClientView struct {
	Name        string `json:"name"`
	Fingerprint string `json:"fingerprint"`
	Status      string `json:"status"`
}

// handleAPIAdminClients serves the admin-clients editor backed by the same
// tergum.toml the CLI uses:
//
//	GET    /api/admin-clients          list configured admin clients + live status
//	POST   /api/admin-clients          {name, fingerprint?} add (resolve name via registry)
//	DELETE /api/admin-clients          {name?, fingerprint?} remove
//
// Every mutation writes tergum.toml, updates the in-memory config, and triggers
// a synchronous adminPolicy.Reload() so a revoked client loses access at once.
func (s *Server) handleAPIAdminClients(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleAdminClientsList(w, r)
	case http.MethodPost:
		s.handleAdminClientsAdd(w, r)
	case http.MethodDelete:
		s.handleAdminClientsRemove(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleAdminClientsList(w http.ResponseWriter, r *http.Request) {
	if s.configPath == "" {
		http.Error(w, "configuration path not set", http.StatusBadRequest)
		return
	}

	cfg, err := config.Load(s.configPath)
	if err != nil {
		s.logger.Error("admin-clients: cannot load config", "error", err)
		http.Error(w, "failed to load config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	status := s.adminFingerprintStatus()

	views := make([]adminClientView, 0, len(cfg.Admin.Clients))
	for _, ac := range cfg.Admin.Clients {
		st := "offline"
		if v, ok := status[ac.Fingerprint]; ok {
			st = v
		}
		views = append(views, adminClientView{
			Name:        ac.Name,
			Fingerprint: ac.Fingerprint,
			Status:      st,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"clients": views})
}

func (s *Server) handleAdminClientsAdd(w http.ResponseWriter, r *http.Request) {
	if s.configPath == "" {
		http.Error(w, "configuration path not set", http.StatusBadRequest)
		return
	}

	var req struct {
		Name        string `json:"name"`
		Fingerprint string `json:"fingerprint"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	fp := strings.ToLower(strings.TrimSpace(req.Fingerprint))

	// Resolve the fingerprint from the registry by name when none was supplied
	// (the trusted raw-fingerprint path is used directly after normalization).
	if fp == "" {
		if req.Name == "" {
			http.Error(w, "name or fingerprint is required", http.StatusBadRequest)
			return
		}
		if s.clientRegistry == nil {
			http.Error(w, "client registry unavailable; supply a fingerprint", http.StatusBadRequest)
			return
		}
		ci := s.clientRegistry.GetClient(req.Name)
		if ci == nil || ci.SPKIFingerprint == "" {
			http.Error(w, "client has no recorded fingerprint; have it connect once or supply a fingerprint", http.StatusBadRequest)
			return
		}
		fp = ci.SPKIFingerprint
	}

	cfg, err := config.Load(s.configPath)
	if err != nil {
		s.logger.Error("admin-clients: cannot load config", "error", err)
		http.Error(w, "failed to load config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := cfg.AddAdminClient(req.Name, fp); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := s.writeAdminConfig(cfg); err != nil {
		s.logger.Error("admin-clients: cannot write config", "error", err)
		http.Error(w, "failed to save config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	s.logger.Info("admin client added", "event", "admin_client_added", "client", req.Name, "fingerprint", fp)
	s.reloadAdminPolicy()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":      "success",
		"name":        req.Name,
		"fingerprint": fp,
	})
}

func (s *Server) handleAdminClientsRemove(w http.ResponseWriter, r *http.Request) {
	if s.configPath == "" {
		http.Error(w, "configuration path not set", http.StatusBadRequest)
		return
	}

	var req struct {
		Name        string `json:"name"`
		Fingerprint string `json:"fingerprint"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	cfg, err := config.Load(s.configPath)
	if err != nil {
		s.logger.Error("admin-clients: cannot load config", "error", err)
		http.Error(w, "failed to load config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	var removed bool
	if strings.TrimSpace(req.Fingerprint) != "" {
		removed = cfg.RemoveAdminClientByFingerprint(req.Fingerprint)
	} else {
		removed = cfg.RemoveAdminClientByName(strings.TrimSpace(req.Name))
	}

	if !removed {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "unchanged"})
		return
	}

	if err := s.writeAdminConfig(cfg); err != nil {
		s.logger.Error("admin-clients: cannot write config", "error", err)
		http.Error(w, "failed to save config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	s.logger.Info("admin client removed", "event", "admin_client_removed", "client", req.Name, "fingerprint", req.Fingerprint)
	s.reloadAdminPolicy()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"status": "removed"})
}

// writeAdminConfig persists cfg to the TOML file and updates the in-memory
// config's admin list, mirroring the config_settings.go write pattern.
func (s *Server) writeAdminConfig(cfg *config.Config) error {
	f, err := os.OpenFile(s.configPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer f.Close()

	if err := toml.NewEncoder(f).Encode(cfg); err != nil {
		return err
	}

	if s.fullCfg != nil {
		s.fullCfg.Admin = cfg.Admin
	}
	return nil
}

// reloadAdminPolicy triggers a synchronous policy reload after a mutation so a
// revoked admin client loses access immediately. A reload error is logged at
// WARN and does not fail the request; the poke is skipped when no policy is set.
func (s *Server) reloadAdminPolicy() {
	if s.adminPolicy == nil {
		return
	}
	if err := s.adminPolicy.Reload(); err != nil {
		s.logger.Warn("admin policy reload failed", "error", err)
	}
}

// adminFingerprintStatus maps known SPKI fingerprints to a live status string
// from the client registry. Fingerprints not present map to "offline" by the
// caller's default.
func (s *Server) adminFingerprintStatus() map[string]string {
	status := make(map[string]string)
	if s.clientRegistry == nil {
		return status
	}
	for _, ci := range s.clientRegistry.ListClients() {
		if ci.SPKIFingerprint == "" {
			continue
		}
		st := ci.Status
		if st == "" {
			st = "online"
		}
		status[ci.SPKIFingerprint] = st
	}
	return status
}
