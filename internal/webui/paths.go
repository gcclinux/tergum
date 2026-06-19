package webui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
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
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

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
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

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
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

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
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

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

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"scanned":     absDir,
		"paths_added": added,
		"count":       len(added),
	})
}
