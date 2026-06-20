package webui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ricardopadilha/tergum/internal/db"
	"github.com/ricardopadilha/tergum/internal/model"
)

// handleAPIBackupTrigger handles POST /api/backups/trigger — triggers a backup.
func (s *Server) handleAPIBackupTrigger(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}

	level := r.FormValue("level")
	if level == "" {
		level = "auto"
	}

	if s.backupTrigger == nil || !s.backupTrigger.IsAvailable() {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<div class="p-3 bg-yellow-100 text-yellow-800 rounded text-sm">`)
		fmt.Fprint(w, `Backup trigger not available. Start the server with <code>TERGUM_PASSPHRASE</code> set to enable web-triggered backups.`)
		fmt.Fprint(w, `</div>`)
		return
	}

	if err := s.backupTrigger.TriggerBackup(level); err != nil {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<div class="p-3 bg-red-100 text-red-800 rounded text-sm">Backup failed to start: %v</div>`, err)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(w, `<div class="p-3 bg-green-100 text-green-800 rounded text-sm">%s backup started! Check the Active Jobs section above.</div>`, level)
}

// handleAPIBackupsActive handles GET /api/backups/active — returns running jobs as HTML.
func (s *Server) handleAPIBackupsActive(w http.ResponseWriter, r *http.Request) {
	if s.repo == nil {
		fmt.Fprint(w, `<p class="text-gray-500 italic">Database not available.</p>`)
		return
	}

	running := model.JobRunning
	jobs, err := s.repo.ListJobs(r.Context(), db.JobFilter{Status: &running})
	if err != nil || len(jobs) == 0 {
		fmt.Fprint(w, `<p class="text-gray-500 italic">No active backup jobs.</p>`)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	for _, j := range jobs {
		elapsed := time.Since(j.StartedAt).Round(time.Second)
		fmt.Fprintf(w, `<div class="p-3 bg-blue-50 rounded mb-2">`)
		fmt.Fprintf(w, `<span class="font-mono text-sm">%s</span> — `, j.BackupID[:12])
		fmt.Fprintf(w, `<span class="text-blue-700">%s</span> running for %s`, j.Level, elapsed)
		fmt.Fprintf(w, `</div>`)
	}
}

// handleAPIBackupDelete handles DELETE /api/backups/{id} — deletes a backup set.
func (s *Server) handleAPIBackupDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.repo == nil {
		http.Error(w, "database not available", http.StatusServiceUnavailable)
		return
	}

	// Extract backup ID from URL path: /api/backups/<id>
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 4 {
		http.Error(w, "backup ID required", http.StatusBadRequest)
		return
	}
	backupID := parts[len(parts)-1]
	if backupID == "" {
		http.Error(w, "backup ID required", http.StatusBadRequest)
		return
	}

	// Delete entries for this backup.
	deleted, err := s.repo.DeleteEntries(r.Context(), db.DeleteFilter{BackupID: backupID})
	if err != nil {
		s.logger.Error("delete backup entries failed", "backup_id", backupID, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Clean up orphan jobs.
	s.repo.DeleteOrphanJobs(r.Context())

	w.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(w, `<tr><td colspan="7" class="p-4 text-green-600">Deleted %d entries from backup %s</td></tr>`, deleted, backupID[:12])
}

// handleAPIRetention handles /api/retention — POST to add, DELETE to remove.
func (s *Server) handleAPIRetention(w http.ResponseWriter, r *http.Request) {
	if s.repo == nil {
		http.Error(w, "database not available", http.StatusServiceUnavailable)
		return
	}

	switch r.Method {
	case http.MethodPost:
		s.handleAPIRetentionAdd(w, r)
	case http.MethodDelete:
		s.handleAPIRetentionRemove(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleAPIRetentionAdd(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	keepDaysStr := r.FormValue("keep_days")
	keepVersionsStr := r.FormValue("keep_versions")
	pattern := r.FormValue("pattern")
	priorityStr := r.FormValue("priority")

	policy := model.RetentionPolicy{
		Name:         name,
		Pattern:      pattern,
		KeepVersions: 1,
		Enabled:      true,
	}

	if keepDaysStr != "" {
		days, err := strconv.Atoi(keepDaysStr)
		if err == nil && days > 0 {
			policy.KeepDays = &days
		}
	}
	if keepVersionsStr != "" {
		v, err := strconv.Atoi(keepVersionsStr)
		if err == nil && v > 0 {
			policy.KeepVersions = v
		}
	}
	if priorityStr != "" {
		p, err := strconv.Atoi(priorityStr)
		if err == nil {
			policy.Priority = p
		}
	}

	if err := s.repo.InsertRetentionPolicy(r.Context(), policy); err != nil {
		s.logger.Error("add retention policy failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Return success message as HTML for HTMX swap.
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, `<form hx-post="/api/retention" hx-swap="outerHTML">`)
	fmt.Fprint(w, `<label class="p-4">Name: <input type="text" name="name" required></label>`)
	fmt.Fprint(w, `<label class="p-4">Keep Days: <input type="number" name="keep_days"></label>`)
	fmt.Fprint(w, `<label class="p-4">Keep Versions: <input type="number" name="keep_versions" value="1" min="1"></label>`)
	fmt.Fprint(w, `<label class="p-4">Pattern: <input type="text" name="pattern" placeholder="*.log"></label>`)
	fmt.Fprint(w, `<label class="p-4">Priority: <input type="number" name="priority" value="0"></label>`)
	fmt.Fprint(w, `<div class="p-4"><button type="submit" class="bg-gray-100 rounded p-4">Add Policy</button>`)
	fmt.Fprintf(w, `<span class="ml-4 text-green-600 text-sm">Policy "%s" added.</span>`, name)
	fmt.Fprint(w, `</div></form>`)
}

func (s *Server) handleAPIRetentionRemove(w http.ResponseWriter, r *http.Request) {
	// Extract name from URL: /api/retention/<name>
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 4 {
		http.Error(w, "policy name required", http.StatusBadRequest)
		return
	}
	name := parts[len(parts)-1]
	if name == "" {
		http.Error(w, "policy name required", http.StatusBadRequest)
		return
	}

	if err := s.repo.DeleteRetentionPolicy(r.Context(), name); err != nil {
		s.logger.Error("remove retention policy failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Return empty to remove the row.
	w.WriteHeader(http.StatusOK)
}

// handleAPIRestoreSearch handles GET /api/restore/search?query=... — search files in backup.
func (s *Server) handleAPIRestoreSearch(w http.ResponseWriter, r *http.Request) {
	if s.repo == nil {
		fmt.Fprint(w, `<p class="text-gray-500">Database not available.</p>`)
		return
	}

	query := strings.TrimSpace(r.URL.Query().Get("query"))
	if query == "" {
		fmt.Fprint(w, `<p class="text-gray-500 italic">Type to search...</p>`)
		return
	}
	if len(query) < 2 {
		fmt.Fprint(w, `<p class="text-gray-500 italic">Type at least 2 characters...</p>`)
		return
	}

	// Search by path pattern.
	pattern := "%" + query + "%"
	entries, err := s.repo.FindByPath(r.Context(), pattern)
	if err != nil {
		s.logger.Error("restore search failed", "error", err)
		fmt.Fprint(w, `<p class="text-red-500">Search failed.</p>`)
		return
	}

	if len(entries) == 0 {
		fmt.Fprintf(w, `<p class="text-gray-500 italic">No files matching "%s"</p>`, query)
		return
	}

	// Limit results.
	max := 50
	if len(entries) > max {
		entries = entries[:max]
	}

	w.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(w, `<p class="text-sm text-gray-600 mb-2">Found %d file(s):</p>`, len(entries))
	fmt.Fprint(w, `<table class="w-full text-sm"><thead><tr>`)
	fmt.Fprint(w, `<th class="p-2 text-left">Path</th><th class="p-2 text-left">Size</th><th class="p-2 text-left">Backup Date</th>`)
	fmt.Fprint(w, `</tr></thead><tbody>`)
	for _, e := range entries {
		date := e.BackupDate.Format("2006-01-02 15:04")
		fmt.Fprintf(w, `<tr class="hover:bg-gray-50"><td class="p-2 font-mono text-xs">%s</td>`, e.FilePath)
		fmt.Fprintf(w, `<td class="p-2">%s</td><td class="p-2">%s</td></tr>`, formatSize(e.FileSize), date)
	}
	fmt.Fprint(w, `</tbody></table>`)
}

// handleAPIRestoreBackups handles GET /api/restore/backups — lists backups for browsing.
func (s *Server) handleAPIRestoreBackups(w http.ResponseWriter, r *http.Request) {
	if s.repo == nil {
		fmt.Fprint(w, `<p class="text-gray-500">Database not available.</p>`)
		return
	}

	jobs, err := s.repo.ListJobs(r.Context(), db.JobFilter{Limit: 20})
	if err != nil || len(jobs) == 0 {
		fmt.Fprint(w, `<p class="text-gray-500 italic">No backups found.</p>`)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, `<table class="w-full text-sm"><thead><tr>`)
	fmt.Fprint(w, `<th class="p-2">Backup ID</th><th class="p-2">Date</th><th class="p-2">Files</th><th class="p-2">Status</th>`)
	fmt.Fprint(w, `</tr></thead><tbody>`)
	for _, j := range jobs {
		date := j.StartedAt.Format("2006-01-02 15:04")
		fmt.Fprintf(w, `<tr class="hover:bg-gray-50">`)
		fmt.Fprintf(w, `<td class="p-2 font-mono text-xs">%s</td>`, j.BackupID[:12])
		fmt.Fprintf(w, `<td class="p-2">%s</td><td class="p-2">%d</td><td class="p-2">%s</td>`, date, j.FileCount, string(j.Status))
		fmt.Fprintf(w, `</tr>`)
	}
	fmt.Fprint(w, `</tbody></table>`)
}

// handleAPIDashboard handles GET /api/dashboard — returns dashboard stats as JSON for HTMX.
func (s *Server) handleAPIDashboard(w http.ResponseWriter, r *http.Request) {
	if s.repo == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"error": "database not available"})
		return
	}

	jobs, _ := s.repo.ListJobs(r.Context(), db.JobFilter{})
	var totalFiles, totalBytes int64
	var completed, running int
	for _, j := range jobs {
		totalFiles += j.FileCount
		totalBytes += j.BytesNew
		if j.Status == model.JobCompleted {
			completed++
		} else if j.Status == model.JobRunning {
			running++
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"total_jobs":  len(jobs),
		"completed":   completed,
		"running":     running,
		"total_files": totalFiles,
		"total_bytes": totalBytes,
		"total_size":  formatSize(totalBytes),
	})
}

// formatSize converts bytes to human-readable.
func formatSize(b int64) string {
	switch {
	case b >= 1024*1024*1024:
		return fmt.Sprintf("%.1f GB", float64(b)/(1024*1024*1024))
	case b >= 1024*1024:
		return fmt.Sprintf("%.1f MB", float64(b)/(1024*1024))
	case b >= 1024:
		return fmt.Sprintf("%.1f KB", float64(b)/1024)
	default:
		return fmt.Sprintf("%d B", b)
	}
}

// handleAPIActivityRecent handles GET /api/activity/recent — returns recent activity as HTML.
func (s *Server) handleAPIActivityRecent(w http.ResponseWriter, r *http.Request) {
	if s.repo == nil {
		fmt.Fprint(w, `<p class="text-gray-500 italic">Database not available.</p>`)
		return
	}

	w.Header().Set("Content-Type", "text/html")

	// Get recent backup jobs.
	jobs, err := s.repo.ListJobs(r.Context(), db.JobFilter{Limit: 20})
	if err != nil {
		fmt.Fprint(w, `<p class="text-red-500">Failed to load activity.</p>`)
		return
	}

	if len(jobs) == 0 {
		fmt.Fprint(w, `<p class="text-gray-500 italic">No recent activity.</p>`)
		return
	}

	fmt.Fprint(w, `<div class="divide-y">`)
	for _, j := range jobs {
		icon := "🔵"
		color := "text-blue-700"
		switch j.Status {
		case model.JobCompleted:
			icon = "✅"
			color = "text-green-700"
		case model.JobFailed:
			icon = "❌"
			color = "text-red-700"
		case model.JobStopped:
			icon = "⏹️"
			color = "text-yellow-700"
		case model.JobRunning:
			icon = "🔄"
			color = "text-blue-700"
		}

		started := j.StartedAt.Format("2006-01-02 15:04:05")
		duration := ""
		if j.FinishedAt != nil {
			duration = j.FinishedAt.Sub(j.StartedAt).Round(time.Second).String()
		} else if j.Status == model.JobRunning {
			duration = time.Since(j.StartedAt).Round(time.Second).String() + " (ongoing)"
		}

		fmt.Fprintf(w, `<div class="py-2 flex items-start gap-3">`)
		fmt.Fprintf(w, `<span class="text-lg">%s</span>`, icon)
		fmt.Fprintf(w, `<div class="flex-1">`)
		fmt.Fprintf(w, `<p class="text-sm %s font-medium">Backup %s — %s</p>`, color, j.Level, string(j.Status))
		fmt.Fprintf(w, `<p class="text-xs text-gray-500">%s | %d files | %s`, started, j.FileCount, formatSize(j.BytesNew))
		if duration != "" {
			fmt.Fprintf(w, ` | %s`, duration)
		}
		fmt.Fprintf(w, `</p>`)
		fmt.Fprintf(w, `<p class="text-xs text-gray-400 font-mono">%s</p>`, j.BackupID)
		fmt.Fprintf(w, `</div></div>`)
	}
	fmt.Fprint(w, `</div>`)
}

// handleAPIWatcherControl handles POST /api/watcher/start and /api/watcher/stop.
func (s *Server) handleAPIWatcherControl(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.watcherController == nil {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<div class="p-3 bg-yellow-100 text-yellow-800 rounded text-sm">`)
		fmt.Fprint(w, `Watcher control not available. Start the server with <code>TERGUM_PASSPHRASE</code> set to enable watcher control.`)
		fmt.Fprint(w, `</div>`)
		return
	}

	action := ""
	if strings.HasSuffix(r.URL.Path, "/start") {
		action = "start"
	} else if strings.HasSuffix(r.URL.Path, "/stop") {
		action = "stop"
	}

	w.Header().Set("Content-Type", "text/html")

	switch action {
	case "start":
		if s.watcherController.IsRunning() {
			fmt.Fprint(w, `<div class="p-3 bg-blue-100 text-blue-800 rounded text-sm">Watcher is already running.</div>`)
			return
		}
		if err := s.watcherController.StartWatcher(); err != nil {
			fmt.Fprintf(w, `<div class="p-3 bg-red-100 text-red-800 rounded text-sm">Failed to start watcher: %v</div>`, err)
			return
		}
		fmt.Fprint(w, `<div class="p-3 bg-green-100 text-green-800 rounded text-sm">Watcher started. Reload the page to see updated status.</div>`)

	case "stop":
		if !s.watcherController.IsRunning() {
			fmt.Fprint(w, `<div class="p-3 bg-blue-100 text-blue-800 rounded text-sm">Watcher is already stopped.</div>`)
			return
		}
		if err := s.watcherController.StopWatcher(); err != nil {
			fmt.Fprintf(w, `<div class="p-3 bg-red-100 text-red-800 rounded text-sm">Failed to stop watcher: %v</div>`, err)
			return
		}
		fmt.Fprint(w, `<div class="p-3 bg-green-100 text-green-800 rounded text-sm">Watcher stopped. Reload the page to see updated status.</div>`)

	default:
		http.Error(w, "invalid action", http.StatusBadRequest)
	}
}
