package webui

import (
	"fmt"
	"html"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/ricardopadilha/tergum/internal/db"
)

// handleWatchersAPI handles GET /api/watchers — returns the current watch paths as an HTML table body.
func (s *Server) handleWatchersAPI(w http.ResponseWriter, r *http.Request) {
	if s.repo == nil {
		http.Error(w, "database not available", http.StatusServiceUnavailable)
		return
	}

	paths, err := s.repo.LoadWatchPaths(r.Context())
	if err != nil {
		s.logger.Error("load watch paths failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, `<table><thead><tr>`)
	fmt.Fprint(w, `<th class="p-4">Path</th>`)
	fmt.Fprint(w, `<th class="p-4">Recursive</th>`)
	fmt.Fprint(w, `<th class="p-4">Enabled</th>`)
	fmt.Fprint(w, `<th class="p-4">Last Event</th>`)
	fmt.Fprint(w, `<th class="p-4">Event Count</th>`)
	fmt.Fprint(w, `<th class="p-4">Actions</th>`)
	fmt.Fprint(w, `</tr></thead><tbody>`)

	for _, wp := range paths {
		recursive := "No"
		if wp.Recursive {
			recursive = "Yes"
		}
		enabled := "No"
		if wp.Enabled {
			enabled = "Yes"
		}
		lastEvent := "-"
		if wp.LastEvent != nil {
			lastEvent = wp.LastEvent.Format("2006-01-02 15:04:05")
		}
		fmt.Fprintf(w, `<tr>`)
		fmt.Fprintf(w, `<td class="p-4">%s</td>`, wp.Path)
		fmt.Fprintf(w, `<td class="p-4">%s</td>`, recursive)
		fmt.Fprintf(w, `<td class="p-4">%s</td>`, enabled)
		fmt.Fprintf(w, `<td class="p-4">%s</td>`, lastEvent)
		fmt.Fprintf(w, `<td class="p-4">%d</td>`, wp.EventCount)
		fmt.Fprintf(w, `<td class="p-4"><button @click="$store.app.openModal('Remove Watch Path', '%s', '/api/watchers?path=%s', 'DELETE')" class="text-red-600 dark:text-red-400 border border-red-300 dark:border-red-600 rounded px-2 py-0.5 text-xs hover:bg-red-50 dark:hover:bg-red-900/20 transition-colors duration-150">Remove</button></td>`, wp.Path, wp.Path)
		fmt.Fprintf(w, `</tr>`)
	}

	fmt.Fprint(w, `</tbody></table>`)
}

// handleWatchersAdd handles POST /api/watchers — adds a new watch path.
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

	recursive := r.FormValue("recursive") == "on"

	wp := db.WatchPath{
		Path:      absPath,
		Recursive: recursive,
		Enabled:   true,
	}

	if err := s.repo.SaveWatchPath(r.Context(), wp); err != nil {
		s.logger.Error("save watch path failed", "path", absPath, "error", err)
		setErrorToast(w, "Failed to add watch path: "+err.Error())
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Return fresh form so user can add another path.
	setSuccessToast(w, "Watch path added")
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, `<form hx-post="/api/watchers" hx-swap="outerHTML">`)
	fmt.Fprint(w, `<label class="p-4">Path: <input type="text" name="path" required></label>`)
	fmt.Fprint(w, `<label class="p-4"><input type="checkbox" name="recursive" checked> Recursive</label>`)
	fmt.Fprint(w, `<div class="p-4"><button type="submit" class="bg-gray-100 rounded p-4">Add Path</button>`)
	fmt.Fprintf(w, `<span class="ml-4 text-green-600 text-sm">Added: %s</span>`, absPath)
	fmt.Fprint(w, `</div></form>`)
}

// handleWatchersDelete handles DELETE /api/watchers?path=... — removes a watch path.
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

	if err := s.repo.DeleteWatchPath(r.Context(), path); err != nil {
		s.logger.Error("delete watch path failed", "path", path, "error", err)
		setErrorToast(w, "Failed to remove watch path: "+err.Error())
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Return empty string to remove the row from the table.
	setSuccessToast(w, "Watch path removed")
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

// handleWatchersPaths handles GET /api/watchers/paths — returns the watch paths table for polling refresh.
func (s *Server) handleWatchersPaths(w http.ResponseWriter, r *http.Request) {
	if s.repo == nil {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<p class="text-gray-500 dark:text-gray-400 italic">Database not available.</p>`)
		return
	}

	paths, err := s.repo.LoadWatchPaths(r.Context())
	if err != nil {
		s.logger.Error("load watch paths failed", "error", err)
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<p class="text-red-500">Failed to load watch paths.</p>`)
		return
	}

	w.Header().Set("Content-Type", "text/html")

	if len(paths) == 0 {
		fmt.Fprint(w, `<p class="text-gray-500 dark:text-gray-400 italic">No watch paths configured. Add a path above to start monitoring for changes.</p>`)
		return
	}

	fmt.Fprint(w, `<table class="w-full text-sm">`)
	fmt.Fprint(w, `<thead><tr class="border-b border-gray-200 dark:border-gray-700 text-xs uppercase text-gray-500 dark:text-gray-400">`)
	fmt.Fprint(w, `<th class="px-4 py-3 text-left">Path</th>`)
	fmt.Fprint(w, `<th class="px-4 py-3 text-center">Recursive</th>`)
	fmt.Fprint(w, `<th class="px-4 py-3 text-center">Enabled</th>`)
	fmt.Fprint(w, `<th class="px-4 py-3 text-left">Last Event</th>`)
	fmt.Fprint(w, `<th class="px-4 py-3 text-right">Event Count</th>`)
	fmt.Fprint(w, `<th class="px-4 py-3 text-center">Actions</th>`)
	fmt.Fprint(w, `</tr></thead><tbody>`)

	for _, wp := range paths {
		recursive := "No"
		if wp.Recursive {
			recursive = "Yes"
		}
		enabledHTML := `<span class="text-gray-400">No</span>`
		if wp.Enabled {
			enabledHTML = `<span class="text-green-600 dark:text-green-400">Yes</span>`
		}
		lastEvent := "-"
		if wp.LastEvent != nil {
			lastEvent = wp.LastEvent.Format("2006-01-02 15:04:05")
		}
		escapedPath := html.EscapeString(wp.Path)
		fmt.Fprintf(w, `<tr class="border-b border-gray-100 dark:border-gray-700/50">`)
		fmt.Fprintf(w, `<td class="px-4 py-3 font-mono text-gray-900 dark:text-gray-100 break-all">%s</td>`, escapedPath)
		fmt.Fprintf(w, `<td class="px-4 py-3 text-center text-gray-600 dark:text-gray-300">%s</td>`, recursive)
		fmt.Fprintf(w, `<td class="px-4 py-3 text-center">%s</td>`, enabledHTML)
		fmt.Fprintf(w, `<td class="px-4 py-3 text-gray-600 dark:text-gray-300 whitespace-nowrap">%s</td>`, lastEvent)
		fmt.Fprintf(w, `<td class="px-4 py-3 text-right text-gray-600 dark:text-gray-300">%d</td>`, wp.EventCount)
		fmt.Fprintf(w, `<td class="px-4 py-3 text-center"><button @click="$store.app.openModal('Remove Watch Path', '%s', '/api/watchers?path=%s', 'DELETE')" class="text-red-600 dark:text-red-400 border border-red-300 dark:border-red-600 rounded px-2 py-0.5 text-xs hover:bg-red-50 dark:hover:bg-red-900/20 transition-colors duration-150">Remove</button></td>`, escapedPath, escapedPath)
		fmt.Fprint(w, `</tr>`)
	}

	fmt.Fprint(w, `</tbody></table>`)
}

// handleWatchersPathsAdd handles POST /api/watchers/paths — adds a new watch path (new endpoint for fragment form).
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

	recursive := r.FormValue("recursive") == "true" || r.FormValue("recursive") == "on"

	wp := db.WatchPath{
		Path:      absPath,
		Recursive: recursive,
		Enabled:   true,
	}

	if err := s.repo.SaveWatchPath(r.Context(), wp); err != nil {
		s.logger.Error("save watch path failed", "path", absPath, "error", err)
		setErrorToast(w, "Failed to add watch path: "+err.Error())
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	setSuccessToast(w, fmt.Sprintf("Watch path added: %s", absPath))
	w.WriteHeader(http.StatusOK)
}
