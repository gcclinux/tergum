package webui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/gcclinux/tergum/internal/config"
)

// handlePathsIncludes returns the current list of include paths as JSON.
func (s *Server) handlePathsIncludes(w http.ResponseWriter, r *http.Request) {
	if s.repo == nil {
		http.Error(w, "database not available", http.StatusServiceUnavailable)
		return
	}

	paths, err := s.repo.ListIncludePaths(r.Context())
	if err != nil {
		s.logger.Error("list include paths failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if paths == nil {
		paths = []string{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"paths": paths})
}

// handlePathsIncludeAdd adds a path to the include list.
func (s *Server) handlePathsIncludeAdd(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.repo == nil {
		http.Error(w, "database not available", http.StatusServiceUnavailable)
		return
	}

	var req struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	path := strings.TrimSpace(req.Path)
	if path == "" {
		http.Error(w, "path is required", http.StatusBadRequest)
		return
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid path: %v", err), http.StatusBadRequest)
		return
	}

	if err := s.repo.AddIncludePath(r.Context(), absPath); err != nil {
		s.logger.Error("add include path failed", "path", absPath, "error", err)
		setErrorToast(w, "Failed to add path: "+err.Error())
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	go s.syncPathsToConfig(r.Context())

	setSuccessToast(w, "Include path added")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"added": absPath})
}

// handlePathsIncludeRemove removes a path from the include list.
func (s *Server) handlePathsIncludeRemove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.repo == nil {
		http.Error(w, "database not available", http.StatusServiceUnavailable)
		return
	}

	var req struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	path := strings.TrimSpace(req.Path)
	if path == "" {
		http.Error(w, "path is required", http.StatusBadRequest)
		return
	}

	if err := s.repo.RemoveIncludePath(r.Context(), path); err != nil {
		s.logger.Error("remove include path failed", "path", path, "error", err)
		setErrorToast(w, "Failed to remove path: "+err.Error())
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	go s.syncPathsToConfig(r.Context())

	setSuccessToast(w, "Include path removed")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"removed": path})
}

// handlePathsExcludes returns the current list of exclude patterns as JSON.
func (s *Server) handlePathsExcludes(w http.ResponseWriter, r *http.Request) {
	if s.repo == nil {
		http.Error(w, "database not available", http.StatusServiceUnavailable)
		return
	}

	patterns, err := s.repo.ListExcludePatterns(r.Context())
	if err != nil {
		s.logger.Error("list exclude patterns failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if patterns == nil {
		patterns = []string{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"patterns": patterns})
}

// handlePathsExcludeAdd adds a pattern to the exclude list.
func (s *Server) handlePathsExcludeAdd(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.repo == nil {
		http.Error(w, "database not available", http.StatusServiceUnavailable)
		return
	}

	var req struct {
		Pattern string `json:"pattern"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	pattern := strings.TrimSpace(req.Pattern)
	if pattern == "" {
		http.Error(w, "pattern is required", http.StatusBadRequest)
		return
	}

	if err := s.repo.AddExcludePattern(r.Context(), pattern); err != nil {
		s.logger.Error("add exclude pattern failed", "pattern", pattern, "error", err)
		setErrorToast(w, "Failed to add path: "+err.Error())
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	go s.syncPathsToConfig(r.Context())

	setSuccessToast(w, "Exclude path added")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"added": pattern})
}

// handlePathsExcludeRemove removes a pattern from the exclude list.
func (s *Server) handlePathsExcludeRemove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.repo == nil {
		http.Error(w, "database not available", http.StatusServiceUnavailable)
		return
	}

	var req struct {
		Pattern string `json:"pattern"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	pattern := strings.TrimSpace(req.Pattern)
	if pattern == "" {
		http.Error(w, "pattern is required", http.StatusBadRequest)
		return
	}

	if err := s.repo.RemoveExcludePattern(r.Context(), pattern); err != nil {
		s.logger.Error("remove exclude pattern failed", "pattern", pattern, "error", err)
		setErrorToast(w, "Failed to remove path: "+err.Error())
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	go s.syncPathsToConfig(r.Context())

	setSuccessToast(w, "Exclude path removed")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"removed": pattern})
}

// handlePathsScan scans a directory and adds top-level folders as include paths.
func (s *Server) handlePathsScan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.repo == nil {
		http.Error(w, "database not available", http.StatusServiceUnavailable)
		return
	}

	var req struct {
		Directory     string `json:"directory"`
		IncludeHidden bool   `json:"include_hidden"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	scanDir := strings.TrimSpace(req.Directory)
	if scanDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			http.Error(w, "cannot determine home directory", http.StatusInternalServerError)
			return
		}
		scanDir = home
	}

	absDir, err := filepath.Abs(scanDir)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid directory: %v", err), http.StatusBadRequest)
		return
	}

	entries, err := os.ReadDir(absDir)
	if err != nil {
		http.Error(w, fmt.Sprintf("cannot read directory: %v", err), http.StatusBadRequest)
		return
	}

	var added []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !req.IncludeHidden && strings.HasPrefix(name, ".") {
			continue
		}
		fullPath := filepath.Join(absDir, name)
		if err := s.repo.AddIncludePath(r.Context(), fullPath); err != nil {
			s.logger.Error("scan: add include path failed", "path", fullPath, "error", err)
			continue
		}
		added = append(added, fullPath)
	}

	if len(added) > 0 {
		go s.syncPathsToConfig(r.Context())
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"scanned":     absDir,
		"paths_added": added,
		"count":       len(added),
	})
}

// syncPathsToConfig writes the current DB include paths and exclude patterns
// back to the TOML config file so it stays in sync with the Web UI.
func (s *Server) syncPathsToConfig(ctx context.Context) {
	if s.configPath == "" {
		return
	}

	// Use a fresh context since this runs async after request completion.
	bgCtx := context.Background()
	_ = ctx // original context only for caller reference

	// Load current config.
	cfg, err := config.Load(s.configPath)
	if err != nil {
		s.logger.Error("sync paths: cannot load config", "error", err)
		return
	}

	// Read current paths from DB.
	includes, err := s.repo.ListIncludePaths(bgCtx)
	if err != nil {
		s.logger.Error("sync paths: cannot list include paths", "error", err)
		return
	}
	excludes, err := s.repo.ListExcludePatterns(bgCtx)
	if err != nil {
		s.logger.Error("sync paths: cannot list exclude patterns", "error", err)
		return
	}

	// Update config.
	cfg.Client.IncludePaths = includes
	cfg.Client.ExcludePatterns = excludes

	// Write back to file.
	f, err := os.OpenFile(s.configPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		s.logger.Error("sync paths: cannot open config file", "error", err)
		return
	}
	defer f.Close()

	encoder := toml.NewEncoder(f)
	if err := encoder.Encode(cfg); err != nil {
		s.logger.Error("sync paths: cannot write config file", "error", err)
	}
}
