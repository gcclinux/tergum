package webui

import (
	"fmt"
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
		fmt.Fprintf(w, `<td class="p-4"><button hx-delete="/api/watchers?path=%s" hx-target="closest tr" hx-swap="outerHTML" hx-confirm="Remove watcher for %s?">Remove</button></td>`, wp.Path, wp.Path)
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
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Return fresh form so user can add another path.
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
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Return empty string to remove the row from the table.
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
}
