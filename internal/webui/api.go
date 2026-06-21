package webui

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ricardopadilha/tergum/internal/config"
	cryptoPkg "github.com/ricardopadilha/tergum/internal/crypto"
	"github.com/ricardopadilha/tergum/internal/db"
	"github.com/ricardopadilha/tergum/internal/model"
	registryPkg "github.com/ricardopadilha/tergum/internal/registry"
	"github.com/ricardopadilha/tergum/internal/restore"
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
		fmt.Fprint(w, `<p style="color:#9ca3af; font-style:italic;">Database not available.</p>`)
		return
	}

	w.Header().Set("Content-Type", "text/html")

	// Get recent backup jobs.
	jobs, err := s.repo.ListJobs(r.Context(), db.JobFilter{Limit: 50})
	if err != nil {
		fmt.Fprint(w, `<p style="color:#dc2626;">Failed to load activity.</p>`)
		return
	}

	// Get recent restores.
	restores, _ := s.repo.ListRestoreHistory(r.Context(), 50)

	if len(jobs) == 0 && len(restores) == 0 {
		fmt.Fprint(w, `<p style="color:#9ca3af; font-style:italic;">No recent activity.</p>`)
		return
	}

	// Build a unified activity list sorted by time (newest first).
	type activityRow struct {
		Timestamp time.Time
		Icon      string
		Type      string
		Status    string
		StatusClr string
		Date      string
		Details   string
		Files     string
		Size      string
		Duration  string
	}

	var rows []activityRow

	for _, j := range jobs {
		icon := "🔵"
		switch j.Status {
		case model.JobCompleted:
			icon = "✅"
		case model.JobFailed:
			icon = "❌"
		case model.JobStopped:
			icon = "⏹️"
		case model.JobRunning:
			icon = "🔄"
		}

		duration := "-"
		if j.FinishedAt != nil {
			duration = j.FinishedAt.Sub(j.StartedAt).Round(time.Second).String()
		} else if j.Status == model.JobRunning {
			duration = time.Since(j.StartedAt).Round(time.Second).String()
		}

		rows = append(rows, activityRow{
			Timestamp: j.StartedAt,
			Icon:      icon,
			Type:      "Backup " + j.Level,
			Status:    string(j.Status),
			StatusClr: "#4b5563",
			Date:      j.StartedAt.Format("2006-01-02 15:04:05"),
			Details:   j.BackupID,
			Files:     fmt.Sprintf("%d", j.FileCount),
			Size:      formatSize(j.BytesNew),
			Duration:  duration,
		})
	}

	for _, rec := range restores {
		icon := "📥"
		status := "success"
		statusClr := "#059669"
		if !rec.Success {
			icon = "❌"
			status = "failed"
			statusClr = "#dc2626"
		}

		rows = append(rows, activityRow{
			Timestamp: rec.RestoredAt,
			Icon:      icon,
			Type:      "Restore",
			Status:    status,
			StatusClr: statusClr,
			Date:      rec.RestoredAt.Format("2006-01-02 15:04:05"),
			Details:   rec.FileName,
			Files:     "1",
			Size:      "-",
			Duration:  "-",
		})
	}

	// Sort newest first.
	sort.Slice(rows, func(i, j int) bool {
		return rows[i].Timestamp.After(rows[j].Timestamp)
	})

	fmt.Fprint(w, `<table style="width:100%; border-collapse:collapse; font-size:13px; font-family:monospace;">`)
	fmt.Fprint(w, `<thead><tr style="border-bottom:2px solid #e5e7eb; text-transform:uppercase; font-size:11px; color:#6b7280;">`)
	fmt.Fprint(w, `<th style="padding:10px 16px; text-align:left; width:4%;"></th>`)
	fmt.Fprint(w, `<th style="padding:10px 16px; text-align:left; width:10%;">Type</th>`)
	fmt.Fprint(w, `<th style="padding:10px 16px; text-align:left; width:10%;">Status</th>`)
	fmt.Fprint(w, `<th style="padding:10px 16px; text-align:left; width:16%;">Date</th>`)
	fmt.Fprint(w, `<th style="padding:10px 16px; text-align:left; width:32%;">Details</th>`)
	fmt.Fprint(w, `<th style="padding:10px 16px; text-align:right; width:8%;">Files</th>`)
	fmt.Fprint(w, `<th style="padding:10px 16px; text-align:right; width:10%;">Size</th>`)
	fmt.Fprint(w, `<th style="padding:10px 16px; text-align:right; width:10%;">Duration</th>`)
	fmt.Fprint(w, `</tr></thead><tbody>`)

	for _, row := range rows {
		fmt.Fprintf(w, `<tr style="border-bottom:1px solid #f3f4f6;">`)
		fmt.Fprintf(w, `<td style="padding:8px 16px;">%s</td>`, row.Icon)
		fmt.Fprintf(w, `<td style="padding:8px 16px; color:#4b5563;">%s</td>`, row.Type)
		fmt.Fprintf(w, `<td style="padding:8px 16px; color:%s;">%s</td>`, row.StatusClr, row.Status)
		fmt.Fprintf(w, `<td style="padding:8px 16px; color:#4b5563; white-space:nowrap;">%s</td>`, row.Date)
		fmt.Fprintf(w, `<td style="padding:8px 16px; color:#9ca3af; font-size:11px;">%s</td>`, row.Details)
		fmt.Fprintf(w, `<td style="padding:8px 16px; text-align:right; color:#4b5563;">%s</td>`, row.Files)
		fmt.Fprintf(w, `<td style="padding:8px 16px; text-align:right; color:#4b5563;">%s</td>`, row.Size)
		fmt.Fprintf(w, `<td style="padding:8px 16px; text-align:right; color:#4b5563;">%s</td>`, row.Duration)
		fmt.Fprintf(w, `</tr>`)
	}
	fmt.Fprint(w, `</tbody></table>`)
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
		w.Header().Set("HX-Refresh", "true")
		fmt.Fprint(w, `<div class="p-3 bg-green-100 text-green-800 rounded text-sm">Watcher started.</div>`)

	case "stop":
		if !s.watcherController.IsRunning() {
			fmt.Fprint(w, `<div class="p-3 bg-blue-100 text-blue-800 rounded text-sm">Watcher is already stopped.</div>`)
			return
		}
		if err := s.watcherController.StopWatcher(); err != nil {
			fmt.Fprintf(w, `<div class="p-3 bg-red-100 text-red-800 rounded text-sm">Failed to stop watcher: %v</div>`, err)
			return
		}
		w.Header().Set("HX-Refresh", "true")
		fmt.Fprint(w, `<div class="p-3 bg-green-100 text-green-800 rounded text-sm">Watcher stopped.</div>`)

	default:
		http.Error(w, "invalid action", http.StatusBadRequest)
	}
}

// handleAPISystemCPU handles GET /api/system/cpu — returns CPU usage as an HTML fragment.
func (s *Server) handleAPISystemCPU(w http.ResponseWriter, r *http.Request) {
	cpu := getCPULoad()
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(w, `<h2 class="text-xs text-gray-500 uppercase">CPU Load</h2>`)
	fmt.Fprintf(w, `<p class="text-2xl font-bold mt-1">%s</p>`, cpu)
}

// handleAPISystemMemory handles GET /api/system/memory — returns memory usage as an HTML fragment.
func (s *Server) handleAPISystemMemory(w http.ResponseWriter, r *http.Request) {
	used, total, pct := getMemoryStats()
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(w, `<h2 class="text-xs text-gray-500 uppercase">Memory</h2>`)
	fmt.Fprintf(w, `<p class="text-2xl font-bold mt-1">%s</p>`, pct)
	fmt.Fprintf(w, `<p class="text-xs text-gray-400">%s / %s</p>`, used, total)
}

// handleAPIRestoreFiles handles GET /api/restore/files?backup_id=... — returns files in a backup as HTML table.
func (s *Server) handleAPIRestoreFiles(w http.ResponseWriter, r *http.Request) {
	if s.repo == nil {
		fmt.Fprint(w, `<p style="color:#dc2626;">Database not available.</p>`)
		return
	}

	backupID := strings.TrimSpace(r.URL.Query().Get("backup_id"))
	if backupID == "" {
		fmt.Fprint(w, `<p style="color:#dc2626;">backup_id is required.</p>`)
		return
	}

	entries, err := s.repo.GetManifest(r.Context(), backupID)
	if err != nil {
		s.logger.Error("get manifest failed", "error", err)
		fmt.Fprint(w, `<p style="color:#dc2626;">Failed to load files.</p>`)
		return
	}

	if len(entries) == 0 {
		fmt.Fprint(w, `<p style="color:#9ca3af; font-style:italic;">No files in this backup.</p>`)
		return
	}

	// Get full entries with file size.
	type fileRow struct {
		Hash string
		Path string
		Size int64
	}
	var files []fileRow
	for _, e := range entries {
		files = append(files, fileRow{Hash: e.Blake3Hash, Path: e.FilePath, Size: e.FileSize})
	}

	w.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(w, `<p style="font-size:12px; color:#6b7280; margin-bottom:12px;">%d file(s) in backup %s</p>`, len(files), backupID[:12])
	fmt.Fprint(w, `<table style="width:100%; border-collapse:collapse; font-size:12px; font-family:monospace;">`)
	fmt.Fprint(w, `<thead><tr style="border-bottom:2px solid #e5e7eb; text-transform:uppercase; font-size:10px; color:#6b7280;">`)
	fmt.Fprint(w, `<th style="padding:6px 12px; text-align:left; width:10%;">Hash</th>`)
	fmt.Fprint(w, `<th style="padding:6px 12px; text-align:left; width:55%;">Path</th>`)
	fmt.Fprint(w, `<th style="padding:6px 12px; text-align:right; width:12%;">Size</th>`)
	fmt.Fprint(w, `<th style="padding:6px 12px; text-align:center; width:23%;">Actions</th>`)
	fmt.Fprint(w, `</tr></thead><tbody>`)

	for _, f := range files {
		fmt.Fprintf(w, `<tr style="border-bottom:1px solid #f3f4f6;">`)
		fmt.Fprintf(w, `<td style="padding:4px 12px; color:#9ca3af;">%s</td>`, f.Hash[:12])
		fmt.Fprintf(w, `<td style="padding:4px 12px; color:#374151;" title="%s">%s</td>`, f.Path, truncatePath(f.Path, 100))
		fmt.Fprintf(w, `<td style="padding:4px 12px; text-align:right; color:#4b5563;">%s</td>`, formatSize(f.Size))
		fmt.Fprintf(w, `<td style="padding:4px 12px; text-align:center;">`)
		fmt.Fprintf(w, `<button onclick="restoreFile('%s','%s')" style="color:#059669; border:1px solid #6ee7b7; border-radius:4px; padding:2px 8px; font-size:10px; cursor:pointer; margin-right:4px;">Restore</button>`, f.Hash, escapeJS(f.Path))
		fmt.Fprintf(w, `</td>`)
		fmt.Fprintf(w, `</tr>`)
	}
	fmt.Fprint(w, `</tbody></table>`)

	// Add "Restore All" button at the bottom.
	fmt.Fprintf(w, `<div style="margin-top:16px; padding-top:12px; border-top:1px solid #e5e7eb;">`)
	fmt.Fprintf(w, `<button onclick="restoreBackup('%s')" style="background:#059669; color:white; border:none; border-radius:4px; padding:8px 16px; font-size:12px; cursor:pointer;">Restore All %d Files</button>`, backupID, len(files))
	fmt.Fprintf(w, `</div>`)
}

// escapeJS escapes a string for safe use in JavaScript string literals.
func escapeJS(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `'`, `\'`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}

// truncatePath shortens a path to maxLen characters, prefixing with "..." if needed.
func truncatePath(path string, maxLen int) string {
	if len(path) <= maxLen {
		return path
	}
	suffix := path[len(path)-(maxLen-3):]
	return "..." + suffix
}

// handleAPIRestoreFile handles POST /api/restore/run — restores file(s).
func (s *Server) handleAPIRestoreFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.repo == nil || s.backupTrigger == nil || !s.backupTrigger.IsAvailable() {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"error":"Restore not available. Start server with TERGUM_PASSPHRASE set."}`)
		return
	}

	if err := r.ParseForm(); err != nil {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"error":"invalid form data"}`)
		return
	}

	hash := strings.TrimSpace(r.FormValue("hash"))
	filePath := strings.TrimSpace(r.FormValue("path"))
	backupID := strings.TrimSpace(r.FormValue("backup_id"))
	dest := strings.TrimSpace(r.FormValue("dest"))

	if hash == "" && backupID == "" {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"error":"hash or backup_id required"}`)
		return
	}
	if dest == "" {
		dest = "/tmp/tergum-restored"
	}

	// Run restore in background.
	go s.runWebRestore(hash, filePath, backupID, dest)

	w.Header().Set("Content-Type", "application/json")
	if backupID != "" && hash == "" {
		fmt.Fprintf(w, `{"status":"started","dest":"%s","message":"Restoring entire backup to %s"}`, escapeJS(dest), escapeJS(dest))
	} else {
		fmt.Fprintf(w, `{"status":"started","dest":"%s","message":"Restoring file to %s"}`, escapeJS(dest), escapeJS(dest))
	}
}

// runWebRestore performs a restore operation in the background.
func (s *Server) runWebRestore(hash, filePath, backupID, dest string) {
	ctx := context.Background()

	// Get master key from the backup trigger.
	var masterKey []byte
	var encEnabled bool
	if bt, ok := s.backupTrigger.(*LocalBackupTrigger); ok {
		masterKey = bt.masterKey
		encEnabled = bt.encEnabled
	}

	// Determine storage dir.
	var storageDir string
	if bt, ok := s.backupTrigger.(*LocalBackupTrigger); ok {
		storageDir = bt.storageDir
	}
	if storageDir == "" && s.configPath != "" {
		cfg, err := config.Load(s.configPath)
		if err == nil {
			storageDir = cfg.StorageDir()
		}
	}

	// Create restore engine.
	source := &localDataSource{storageDir: storageDir}
	var encryptor *cryptoPkg.AESEncryptor
	if encEnabled {
		encryptor = cryptoPkg.NewEncryptor()
	}
	engine := restore.NewRestoreEngine(source, s.repo, encryptor, masterKey)

	if backupID != "" && hash == "" {
		// Restore entire backup.
		manifest, err := s.repo.GetManifest(ctx, backupID)
		if err != nil {
			slog.Error("web restore: get manifest failed", "error", err)
			return
		}

		var entries []restore.RestoreEntry
		for _, m := range manifest {
			found, err := s.repo.FindByHash(ctx, m.Blake3Hash)
			if err != nil || len(found) == 0 {
				continue
			}
			entry := found[0]
			destination := dest + entry.FilePath
			entries = append(entries, restore.RestoreEntry{
				Hash:        entry.Blake3Hash,
				FileName:    entry.FileName,
				Destination: destination,
				BackupID:    backupID,
				Metadata:    &entry,
			})
		}

		result, err := engine.RestoreBatch(ctx, entries, 4)
		if err != nil {
			slog.Error("web restore: batch restore failed", "error", err)
			return
		}
		slog.Info("web restore complete", "restored", result.Restored, "failed", result.Failed)

		if s.broker != nil {
			s.broker.Publish(ActivityEvent{
				Type:    "restore_completed",
				Message: fmt.Sprintf("Restored %d files to %s (%d failed)", result.Restored, dest, result.Failed),
			})
		}
	} else {
		// Restore single file.
		destination := dest + filePath
		err := engine.RestoreFile(ctx, hash, destination)
		if err != nil {
			slog.Error("web restore: file restore failed", "hash", hash, "error", err)
			// Record the failed restore in history.
			_ = s.repo.RecordRestore(ctx, db.RestoreRecord{
				Blake3Hash:   hash,
				FileName:     filepath.Base(filePath),
				SourceBackup: "webui",
				RestoredTo:   destination,
				RestoredBy:   "webui",
				Success:      false,
			})
			if s.broker != nil {
				s.broker.Publish(ActivityEvent{
					Type:    "restore_failed",
					Message: fmt.Sprintf("Restore failed: %s - %v", filePath, err),
				})
			}
			return
		}
		slog.Info("web restore: file restored", "path", destination)
		// The restore engine already records success internally.
		if s.broker != nil {
			s.broker.Publish(ActivityEvent{
				Type:    "restore_completed",
				Message: fmt.Sprintf("Restored %s to %s", filePath, dest),
			})
		}
	}
}

// localDataSource reads files from a local CAS directory.
type localDataSource struct {
	storageDir string
}

func (l *localDataSource) DownloadFile(ctx context.Context, hash string) ([]byte, error) {
	path := l.storageDir + "/" + hash[:2] + "/" + hash
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("hash %s not found in store", hash)
	}
	return data, nil
}

// handleAPIClients handles GET /api/clients — returns registered clients as JSON.
func (s *Server) handleAPIClients(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.clientRegistry == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]interface{}{})
		return
	}

	type clientJSON struct {
		ClientID       string `json:"client_id"`
		Address        string `json:"address"`
		Status         string `json:"status"`
		LastSeen       string `json:"last_seen"`
		LastBackup     string `json:"last_backup"`
		WatcherActive  bool   `json:"watcher_active"`
		FullBackupCron string `json:"full_backup_cron"`
		AutoBackupCron string `json:"auto_backup_cron"`
	}

	clients := s.clientRegistry.ListClients()
	result := make([]clientJSON, 0, len(clients))
	for _, ci := range clients {
		cj := clientJSON{
			ClientID:      ci.ClientID,
			Address:       ci.Address,
			Status:        ci.Status,
			WatcherActive: ci.WatcherActive,
		}
		if !ci.LastSeen.IsZero() {
			cj.LastSeen = ci.LastSeen.Format(time.RFC3339)
		}
		if !ci.LastBackup.IsZero() {
			cj.LastBackup = ci.LastBackup.Format(time.RFC3339)
		}
		if ci.Schedule != nil {
			cj.FullBackupCron = ci.Schedule.FullBackupCron
			cj.AutoBackupCron = ci.Schedule.AutoBackupCron
		}
		result = append(result, cj)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// handleAPIClientAction routes client-specific API calls:
//   - POST /api/clients/{id}/backup
//   - POST /api/clients/{id}/watcher/start
//   - POST /api/clients/{id}/watcher/stop
//   - GET  /api/clients/{id}/status
//   - PUT  /api/clients/{id}/schedule
func (s *Server) handleAPIClientAction(w http.ResponseWriter, r *http.Request) {
	// Parse path: /api/clients/{id}/{action...}
	path := strings.TrimPrefix(r.URL.Path, "/api/clients/")
	parts := strings.SplitN(path, "/", 3)
	if len(parts) < 2 {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}

	clientID := parts[0]
	action := parts[1]
	subAction := ""
	if len(parts) > 2 {
		subAction = parts[2]
	}

	switch {
	case action == "backup" && r.Method == http.MethodPost:
		s.handleAPIClientBackup(w, r, clientID)
	case action == "watcher" && subAction == "start" && r.Method == http.MethodPost:
		s.handleAPIClientWatcherStart(w, r, clientID)
	case action == "watcher" && subAction == "stop" && r.Method == http.MethodPost:
		s.handleAPIClientWatcherStop(w, r, clientID)
	case action == "status" && r.Method == http.MethodGet:
		s.handleAPIClientStatus(w, r, clientID)
	case action == "schedule" && r.Method == http.MethodPut:
		s.handleAPIClientSchedule(w, r, clientID)
	case action == "history" && r.Method == http.MethodGet:
		s.handleAPIClientHistory(w, r, clientID)
	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}

// handleAPIClientBackup handles POST /api/clients/{id}/backup — triggers backup on client via RPC.
func (s *Server) handleAPIClientBackup(w http.ResponseWriter, r *http.Request, clientID string) {
	if s.clientConnector == nil {
		writeJSONError(w, "client connector not available", http.StatusServiceUnavailable)
		return
	}
	if s.clientRegistry == nil {
		writeJSONError(w, "registry not available", http.StatusServiceUnavailable)
		return
	}

	ci := s.clientRegistry.GetClient(clientID)
	if ci == nil {
		writeJSONError(w, "client not found", http.StatusNotFound)
		return
	}
	if ci.Status != "online" {
		writeJSONError(w, "client is offline", http.StatusConflict)
		return
	}

	if err := s.clientConnector.TriggerClientBackup(r.Context(), clientID); err != nil {
		s.logger.Error("trigger backup on client failed", "client_id", clientID, "error", err)
		writeJSONError(w, fmt.Sprintf("trigger backup failed: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("backup triggered on client %s", clientID),
	})
}

// handleAPIClientWatcherStart handles POST /api/clients/{id}/watcher/start.
func (s *Server) handleAPIClientWatcherStart(w http.ResponseWriter, r *http.Request, clientID string) {
	if s.clientConnector == nil {
		writeJSONError(w, "client connector not available", http.StatusServiceUnavailable)
		return
	}
	if s.clientRegistry == nil {
		writeJSONError(w, "registry not available", http.StatusServiceUnavailable)
		return
	}

	ci := s.clientRegistry.GetClient(clientID)
	if ci == nil {
		writeJSONError(w, "client not found", http.StatusNotFound)
		return
	}
	if ci.Status != "online" {
		writeJSONError(w, "client is offline", http.StatusConflict)
		return
	}

	if err := s.clientConnector.StartClientWatcher(r.Context(), clientID); err != nil {
		s.logger.Error("start watcher on client failed", "client_id", clientID, "error", err)
		writeJSONError(w, fmt.Sprintf("start watcher failed: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("watcher started on client %s", clientID),
	})
}

// handleAPIClientWatcherStop handles POST /api/clients/{id}/watcher/stop.
func (s *Server) handleAPIClientWatcherStop(w http.ResponseWriter, r *http.Request, clientID string) {
	if s.clientConnector == nil {
		writeJSONError(w, "client connector not available", http.StatusServiceUnavailable)
		return
	}
	if s.clientRegistry == nil {
		writeJSONError(w, "registry not available", http.StatusServiceUnavailable)
		return
	}

	ci := s.clientRegistry.GetClient(clientID)
	if ci == nil {
		writeJSONError(w, "client not found", http.StatusNotFound)
		return
	}
	if ci.Status != "online" {
		writeJSONError(w, "client is offline", http.StatusConflict)
		return
	}

	if err := s.clientConnector.StopClientWatcher(r.Context(), clientID); err != nil {
		s.logger.Error("stop watcher on client failed", "client_id", clientID, "error", err)
		writeJSONError(w, fmt.Sprintf("stop watcher failed: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("watcher stopped on client %s", clientID),
	})
}

// handleAPIClientStatus handles GET /api/clients/{id}/status — queries client status via RPC.
func (s *Server) handleAPIClientStatus(w http.ResponseWriter, r *http.Request, clientID string) {
	if s.clientConnector == nil {
		writeJSONError(w, "client connector not available", http.StatusServiceUnavailable)
		return
	}
	if s.clientRegistry == nil {
		writeJSONError(w, "registry not available", http.StatusServiceUnavailable)
		return
	}

	ci := s.clientRegistry.GetClient(clientID)
	if ci == nil {
		writeJSONError(w, "client not found", http.StatusNotFound)
		return
	}

	// If client is offline, return cached registry info without RPC.
	if ci.Status != "online" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":  "offline",
			"message": "client is not reachable",
		})
		return
	}

	status, err := s.clientConnector.GetClientStatus(r.Context(), clientID)
	if err != nil {
		s.logger.Error("get client status failed", "client_id", clientID, "error", err)
		writeJSONError(w, fmt.Sprintf("get status failed: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

// handleAPIClientSchedule handles PUT /api/clients/{id}/schedule — updates client schedule.
func (s *Server) handleAPIClientSchedule(w http.ResponseWriter, r *http.Request, clientID string) {
	if s.clientRegistry == nil {
		writeJSONError(w, "registry not available", http.StatusServiceUnavailable)
		return
	}

	ci := s.clientRegistry.GetClient(clientID)
	if ci == nil {
		writeJSONError(w, "client not found", http.StatusNotFound)
		return
	}

	var req struct {
		FullBackupCron string `json:"full_backup_cron"`
		AutoBackupCron string `json:"auto_backup_cron"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	schedule := registryPkg.ScheduleConfig{
		FullBackupCron: req.FullBackupCron,
		AutoBackupCron: req.AutoBackupCron,
	}
	if err := s.clientRegistry.SetSchedule(clientID, schedule); err != nil {
		s.logger.Error("set client schedule failed", "client_id", clientID, "error", err)
		writeJSONError(w, fmt.Sprintf("set schedule failed: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("schedule updated for client %s", clientID),
	})
}

// handleAPIClientHistory handles GET /api/clients/{id}/history — returns backup job history for a client.
func (s *Server) handleAPIClientHistory(w http.ResponseWriter, r *http.Request, clientID string) {
	if s.repo == nil {
		writeJSONError(w, "database not available", http.StatusServiceUnavailable)
		return
	}

	// Return the last 10 backup jobs for this client.
	filter := db.JobFilter{
		ClientID: &clientID,
		Limit:    10,
	}
	jobs, err := s.repo.ListJobs(r.Context(), filter)
	if err != nil {
		s.logger.Error("list client history failed", "client_id", clientID, "error", err)
		writeJSONError(w, "failed to load history", http.StatusInternalServerError)
		return
	}

	type jobJSON struct {
		BackupID   string `json:"backup_id"`
		Level      string `json:"level"`
		Status     string `json:"status"`
		FileCount  int64  `json:"file_count"`
		BytesNew   int64  `json:"bytes_new"`
		StartedAt  string `json:"started_at"`
		FinishedAt string `json:"finished_at,omitempty"`
		Duration   string `json:"duration,omitempty"`
		Error      string `json:"error,omitempty"`
	}

	result := make([]jobJSON, 0, len(jobs))
	for _, j := range jobs {
		jj := jobJSON{
			BackupID:  j.BackupID,
			Level:     j.Level,
			Status:    string(j.Status),
			FileCount: j.FileCount,
			BytesNew:  j.BytesNew,
			StartedAt: j.StartedAt.Format(time.RFC3339),
			Error:     j.ErrorMessage,
		}
		if j.FinishedAt != nil {
			jj.FinishedAt = j.FinishedAt.Format(time.RFC3339)
			jj.Duration = j.FinishedAt.Sub(j.StartedAt).Round(time.Second).String()
		}
		result = append(result, jj)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// writeJSONError writes a JSON error response.
func writeJSONError(w http.ResponseWriter, message string, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}
