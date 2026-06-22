package webui

import (
	"fmt"
	"html"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/gcclinux/tergum/internal/config"
)

// handleWatchersAPI handles GET /api/watchers — returns the current watch excludes as an HTML table body.
func (s *Server) handleWatchersAPI(w http.ResponseWriter, r *http.Request) {
	if s.repo == nil {
		http.Error(w, "database not available", http.StatusServiceUnavailable)
		return
	}

	excludes, err := s.repo.ListWatchExcludes(r.Context())
	if err != nil {
		s.logger.Error("list watch excludes failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, `<table><thead><tr>`)
	fmt.Fprint(w, `<th class="p-4">Excluded Path</th>`)
	fmt.Fprint(w, `<th class="p-4">Actions</th>`)
	fmt.Fprint(w, `</tr></thead><tbody>`)

	for _, p := range excludes {
		fmt.Fprintf(w, `<tr>`)
		fmt.Fprintf(w, `<td class="p-4">%s</td>`, html.EscapeString(p))
		fmt.Fprintf(w, `<td class="p-4"><button @click="$store.app.openModal('Remove Watch Exclusion', '%s', '/api/watchers?path=%s', 'DELETE')" class="text-red-600 dark:text-red-400 border border-red-300 dark:border-red-600 rounded px-2 py-0.5 text-xs hover:bg-red-50 dark:hover:bg-red-900/20 transition-colors duration-150">Remove</button></td>`, html.EscapeString(p), html.EscapeString(p))
		fmt.Fprintf(w, `</tr>`)
	}

	fmt.Fprint(w, `</tbody></table>`)
}

// handleWatchersAdd handles POST /api/watchers — adds a new watch exclusion.
func (s *Server) handleWatchersAdd(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.repo == nil {
		http.Error(w, "database not available", http.StatusServiceUnavailable)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form data", http.StatusBadRequest)
		return
	}

	path := strings.TrimSpace(r.FormValue("path"))
	if path == "" {
		http.Error(w, "path is required", http.StatusBadRequest)
		return
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid path: %v", err), http.StatusBadRequest)
		return
	}

	if err := s.repo.AddWatchExclude(r.Context(), absPath); err != nil {
		s.logger.Error("add watch exclude failed", "path", absPath, "error", err)
		setErrorToast(w, "Failed to exclude watch path: "+err.Error())
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Return fresh form so user can add another path.
	setSuccessToast(w, "Watch path excluded")
	w.Header().Set("Content-Type", "text/html")

	includes, _ := s.repo.ListIncludePaths(r.Context())
	fmt.Fprint(w, `<form hx-post="/api/watchers" hx-swap="outerHTML">`)
	fmt.Fprint(w, `<label class="p-4">Path to Exclude: `)
	fmt.Fprint(w, `<select name="path" required>`)
	fmt.Fprint(w, `<option value="">-- Select a path to exclude --</option>`)
	for _, inc := range includes {
		fmt.Fprintf(w, `<option value="%s">%s</option>`, html.EscapeString(inc), html.EscapeString(inc))
	}
	fmt.Fprint(w, `</select></label>`)
	fmt.Fprint(w, `<div class="p-4"><button type="submit" class="bg-gray-100 rounded p-4">Exclude Path</button>`)
	fmt.Fprintf(w, `<span class="ml-4 text-green-600 text-sm">Excluded: %s</span>`, html.EscapeString(absPath))
	fmt.Fprint(w, `</div></form>`)
}

// handleWatchersDelete handles DELETE /api/watchers?path=... — removes a watch exclusion.
func (s *Server) handleWatchersDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.repo == nil {
		http.Error(w, "database not available", http.StatusServiceUnavailable)
		return
	}

	path := strings.TrimSpace(r.URL.Query().Get("path"))
	if path == "" {
		http.Error(w, "path is required", http.StatusBadRequest)
		return
	}

	if err := s.repo.RemoveWatchExclude(r.Context(), path); err != nil {
		s.logger.Error("remove watch exclude failed", "path", path, "error", err)
		setErrorToast(w, "Failed to remove watch exclusion: "+err.Error())
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	setSuccessToast(w, "Watch exclusion removed")
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
}

// handleWatchersStatus handles GET /api/watchers/status — returns the watcher status indicator and control button.
func (s *Server) handleWatchersStatus(w http.ResponseWriter, r *http.Request) {
	running := false
	if s.watcherController != nil {
		running = s.watcherController.IsRunning()
	}

	w.Header().Set("Content-Type", "text/html")

	if running {
		fmt.Fprint(w, `<span class="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-xs font-medium bg-green-100 dark:bg-green-900/30 text-green-800 dark:text-green-300">`)
		fmt.Fprint(w, `<span class="w-2 h-2 rounded-full bg-green-500 animate-pulse"></span>Running</span>`)
		fmt.Fprint(w, ` <button hx-post="/api/watcher/stop" hx-swap="none" class="px-3 py-1.5 text-xs font-medium rounded-lg border border-red-300 dark:border-red-600 text-red-600 dark:text-red-400 hover:bg-red-50 dark:hover:bg-red-900/20 transition-colors duration-150">Stop</button>`)
	} else {
		fmt.Fprint(w, `<span class="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-xs font-medium bg-gray-100 dark:bg-gray-700 text-gray-600 dark:text-gray-400">`)
		fmt.Fprint(w, `<span class="w-2 h-2 rounded-full bg-gray-400"></span>Stopped</span>`)
		fmt.Fprint(w, ` <button hx-post="/api/watcher/start" hx-swap="none" class="px-3 py-1.5 text-xs font-medium rounded-lg border border-green-300 dark:border-green-600 text-green-600 dark:text-green-400 hover:bg-green-50 dark:hover:bg-green-900/20 transition-colors duration-150">Start</button>`)
	}
}

// handleWatchersSettings handles GET /api/watchers/settings — returns the watcher configuration summary.
func (s *Server) handleWatchersSettings(w http.ResponseWriter, r *http.Request) {
	debounceMs := 500
	stabilitySec := 60
	batchMinutes := 5

	if s.fullCfg != nil {
		if s.fullCfg.Watcher.DebounceMs > 0 {
			debounceMs = s.fullCfg.Watcher.DebounceMs
		}
		if s.fullCfg.Watcher.StabilitySeconds > 0 {
			stabilitySec = s.fullCfg.Watcher.StabilitySeconds
		}
		if s.fullCfg.Watcher.BatchIntervalMinutes > 0 {
			batchMinutes = s.fullCfg.Watcher.BatchIntervalMinutes
		}
	}

	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, `<div class="grid grid-cols-1 md:grid-cols-3 gap-4">`)
	fmt.Fprintf(w, `<div><span class="block text-xs text-gray-500 dark:text-gray-400 uppercase tracking-wide">Debounce</span><span class="text-sm font-medium text-gray-900 dark:text-gray-100">%dms</span></div>`, debounceMs)
	fmt.Fprintf(w, `<div><span class="block text-xs text-gray-500 dark:text-gray-400 uppercase tracking-wide">Stability Gate</span><span class="text-sm font-medium text-gray-900 dark:text-gray-100">%ds</span></div>`, stabilitySec)
	fmt.Fprintf(w, `<div><span class="block text-xs text-gray-500 dark:text-gray-400 uppercase tracking-wide">Batch Interval</span><span class="text-sm font-medium text-gray-900 dark:text-gray-100">%d min</span></div>`, batchMinutes)
	fmt.Fprint(w, `</div>`)
}

// handleWatchersPaths handles GET /api/watchers/paths — returns the watch exclusions table for polling refresh.
func (s *Server) handleWatchersPaths(w http.ResponseWriter, r *http.Request) {
	if s.repo == nil {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<p class="text-gray-500 dark:text-gray-400 italic">Database not available.</p>`)
		return
	}

	excludes, err := s.repo.ListWatchExcludes(r.Context())
	if err != nil {
		s.logger.Error("list watch excludes failed", "error", err)
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<p class="text-red-500">Failed to load watch exclusions.</p>`)
		return
	}

	w.Header().Set("Content-Type", "text/html")

	if len(excludes) == 0 {
		fmt.Fprint(w, `<p class="text-gray-500 dark:text-gray-400 italic">No watch exclusions configured.</p>`)
		return
	}

	fmt.Fprint(w, `<table class="w-full text-sm">`)
	fmt.Fprint(w, `<thead><tr class="border-b border-gray-200 dark:border-gray-700 text-xs uppercase text-gray-500 dark:text-gray-400">`)
	fmt.Fprint(w, `<th class="px-4 py-3 text-left">Excluded Path</th>`)
	fmt.Fprint(w, `<th class="px-4 py-3 text-center">Actions</th>`)
	fmt.Fprint(w, `</tr></thead><tbody>`)

	for _, p := range excludes {
		escapedPath := html.EscapeString(p)
		fmt.Fprintf(w, `<tr class="border-b border-gray-100 dark:border-gray-700/50">`)
		fmt.Fprintf(w, `<td class="px-4 py-3 font-mono text-gray-900 dark:text-gray-100 break-all">%s</td>`, escapedPath)
		fmt.Fprintf(w, `<td class="px-4 py-3 text-center"><button @click="confirmRemove($el.dataset.path)" data-path="%s" class="text-red-600 dark:text-red-400 border border-red-300 dark:border-red-600 rounded px-2 py-0.5 text-xs hover:bg-red-50 dark:hover:bg-red-900/20 transition-colors duration-150">Remove</button></td>`, escapedPath)
		fmt.Fprint(w, `</tr>`)
	}

	fmt.Fprint(w, `</tbody></table>`)
}

// handleWatchersPathsAdd handles POST /api/watchers/paths — adds a new watch exclusion (new endpoint for fragment form).
func (s *Server) handleWatchersPathsAdd(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.repo == nil {
		http.Error(w, "database not available", http.StatusServiceUnavailable)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form data", http.StatusBadRequest)
		return
	}

	path := strings.TrimSpace(r.FormValue("path"))
	if path == "" {
		setErrorToast(w, "Path must not be blank")
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		setErrorToast(w, fmt.Sprintf("Invalid path: %v", err))
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	if err := s.repo.AddWatchExclude(r.Context(), absPath); err != nil {
		s.logger.Error("add watch exclude failed", "path", absPath, "error", err)
		setErrorToast(w, "Failed to exclude watch path: "+err.Error())
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	setSuccessToast(w, fmt.Sprintf("Watch path excluded: %s", absPath))
	w.WriteHeader(http.StatusOK)
}

// handleAPIWatcherAutostart handles POST /api/watchers/config/autostart.
func (s *Server) handleAPIWatcherAutostart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.configPath == "" {
		http.Error(w, "configuration path not set", http.StatusBadRequest)
		return
	}

	enabled := r.FormValue("enabled") == "true"

	// Load current config.
	cfg, err := config.Load(s.configPath)
	if err != nil {
		s.logger.Error("watcher autostart: cannot load config", "error", err)
		setErrorToast(w, "Failed to load config: "+err.Error())
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Update config.
	cfg.Watcher.Enabled = enabled

	// Write back to file.
	f, err := os.OpenFile(s.configPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		s.logger.Error("watcher autostart: cannot open config file", "error", err)
		setErrorToast(w, "Failed to open config file: "+err.Error())
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer f.Close()

	encoder := toml.NewEncoder(f)
	if err := encoder.Encode(cfg); err != nil {
		s.logger.Error("watcher autostart: cannot write config file", "error", err)
		setErrorToast(w, "Failed to save config: "+err.Error())
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Sync in-memory config.
	if s.fullCfg != nil {
		s.fullCfg.Watcher.Enabled = enabled
	}

	setSuccessToast(w, "Autostart on boot updated")
	w.WriteHeader(http.StatusOK)
}
