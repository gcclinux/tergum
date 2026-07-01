package webui

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gcclinux/tergum/internal/config"
	cryptoPkg "github.com/gcclinux/tergum/internal/crypto"
	"github.com/gcclinux/tergum/internal/db"
	"github.com/gcclinux/tergum/internal/model"
	"github.com/gcclinux/tergum/internal/observe"
	registryPkg "github.com/gcclinux/tergum/internal/registry"
	"github.com/gcclinux/tergum/internal/restore"
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
		setErrorToast(w, "Backup trigger not available")
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<div class="p-3 bg-yellow-100 text-yellow-800 rounded text-sm">`)
		fmt.Fprint(w, `Backup trigger not available. Start the server with <code>TERGUM_PASSPHRASE</code> set to enable web-triggered backups.`)
		fmt.Fprint(w, `</div>`)
		return
	}

	if err := s.backupTrigger.TriggerBackup(level); err != nil {
		setErrorToast(w, fmt.Sprintf("Failed to trigger backup: %v", err))
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<div class="p-3 bg-red-100 text-red-800 rounded text-sm">Backup failed to start: %v</div>`, err)
		return
	}

	setSuccessToast(w, "Backup triggered successfully")
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(w, `<div class="p-3 bg-green-100 text-green-800 rounded text-sm">%s backup started! Check the Active Jobs section above.</div>`, level)
}

// handleAPIBackupStop handles POST /api/backups/stop — stops the currently running local backup.
func (s *Server) handleAPIBackupStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.backupTrigger == nil {
		setErrorToast(w, "Backup trigger not available")
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<div class="p-3 bg-red-100 text-red-800 rounded text-sm">Backup trigger not available.</div>`)
		return
	}

	if err := s.backupTrigger.StopBackup(); err != nil {
		setErrorToast(w, fmt.Sprintf("Failed to stop backup: %v", err))
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<div class="p-3 bg-red-100 text-red-800 rounded text-sm">Failed to stop backup: %v</div>`, err)
		return
	}

	setSuccessToast(w, "Backup stop signal sent successfully")
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, `<div class="p-3 bg-green-100 text-green-800 rounded text-sm">Stop signal sent to backup!</div>`)
}

// handleAPIBackupsProgress handles GET /api/backups/progress — returns a progress
// fragment showing files count and bytes transferred for running backup jobs.
// This endpoint is designed to be called when SSE backup_progress events trigger a refresh.
func (s *Server) handleAPIBackupsProgress(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")

	if s.repo == nil {
		fmt.Fprint(w, `<div id="backup-progress" class="hidden"></div>`)
		return
	}

	running := model.JobRunning
	jobs, err := s.repo.ListJobs(r.Context(), db.JobFilter{Status: &running})
	if err != nil || len(jobs) == 0 {
		// No running jobs — return empty hidden div.
		fmt.Fprint(w, `<div id="backup-progress" class="hidden"></div>`)
		return
	}

	// Show progress for the first running job.
	j := jobs[0]
	elapsed := time.Since(j.StartedAt).Round(time.Second)

	fmt.Fprint(w, `<div id="backup-progress" class="flex items-center gap-4 p-3 bg-blue-50 dark:bg-blue-900/20 border border-blue-100 dark:border-blue-800 rounded-lg text-sm">`)
	fmt.Fprint(w, `<div class="flex items-center gap-2">`)
	fmt.Fprint(w, `<svg class="animate-spin h-4 w-4 text-blue-500" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24"><circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle><path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z"></path></svg>`)
	fmt.Fprintf(w, `<span class="text-blue-700 dark:text-blue-300 font-medium">%s</span>`, j.Level)
	fmt.Fprint(w, `</div>`)
	fmt.Fprintf(w, `<span class="text-gray-600 dark:text-gray-400">Files: <span class="font-mono font-medium text-gray-800 dark:text-gray-200">%d</span></span>`, j.FileCount)
	fmt.Fprintf(w, `<span class="text-gray-600 dark:text-gray-400">Transferred: <span class="font-mono font-medium text-gray-800 dark:text-gray-200">%s</span></span>`, formatSize(j.BytesNew))
	fmt.Fprintf(w, `<span class="text-gray-600 dark:text-gray-400">Elapsed: <span class="font-mono font-medium text-gray-800 dark:text-gray-200">%s</span></span>`, elapsed)
	fmt.Fprint(w, `</div>`)
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

// handleAPIBackupsStatus handles GET /api/backups/status — returns a running job banner or empty div.
func (s *Server) handleAPIBackupsStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")

	if s.repo == nil {
		return
	}

	running := model.JobRunning
	jobs, err := s.repo.ListJobs(r.Context(), db.JobFilter{Status: &running})
	if err != nil || len(jobs) == 0 {
		// No running jobs — return empty so the banner hides.
		fmt.Fprint(w, `<div id="backup-running-banner" class="hidden"></div>`)
		return
	}

	// Show banner for the first running job (usually only one at a time).
	j := jobs[0]
	elapsed := time.Since(j.StartedAt).Round(time.Second)
	shortID := j.BackupID
	if len(shortID) > 12 {
		shortID = shortID[:12]
	}

	fmt.Fprint(w, `<div class="flex items-center gap-3 p-4 bg-blue-50 dark:bg-blue-900/30 border border-blue-200 dark:border-blue-700 rounded-lg">`)
	// Animated spinner
	fmt.Fprint(w, `<svg class="animate-spin h-5 w-5 text-blue-600 dark:text-blue-400" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24"><circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle><path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z"></path></svg>`)
	fmt.Fprint(w, `<div class="flex-1">`)
	fmt.Fprintf(w, `<p class="text-sm font-medium text-blue-800 dark:text-blue-200">Backup Running</p>`)
	fmt.Fprintf(w, `<p class="text-xs text-blue-600 dark:text-blue-300">ID: <span class="font-mono">%s</span> · Client: %s · Level: %s · Elapsed: %s</p>`, shortID, j.ClientID, j.Level, elapsed)
	fmt.Fprint(w, `</div>`)
	fmt.Fprint(w, `<button class="px-3 py-1.5 bg-red-600 hover:bg-red-700 text-white text-xs font-semibold rounded-lg transition-colors"
		hx-post="/api/backups/stop" hx-swap="none" hx-confirm="Stop the running backup?">Stop Backup</button>`)
	fmt.Fprint(w, `</div>`)
}

// handleAPIBackupsHistory handles GET /api/backups/history — returns backup history as an HTML table.
func (s *Server) handleAPIBackupsHistory(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")

	if s.repo == nil {
		fmt.Fprint(w, `<p class="text-gray-500 dark:text-gray-400 italic">Database not available.</p>`)
		return
	}

	jobs, err := s.repo.ListJobs(r.Context(), db.JobFilter{Limit: 100})
	if err != nil {
		fmt.Fprint(w, `<p class="text-red-500">Failed to load backup history.</p>`)
		return
	}

	if len(jobs) == 0 {
		fmt.Fprint(w, `<p class="text-gray-500 dark:text-gray-400 italic">No backup history.</p>`)
		return
	}

	// Build the table rows as JSON data for Alpine.js pagination and sorting.
	fmt.Fprint(w, `<div x-data="{
		page: 1,
		perPage: 10,
		sortCol: 'started',
		sortDir: 'desc',
		get totalPages() { return Math.ceil(this.rows.length / this.perPage); },
		get sortedRows() {
			let sorted = [...this.rows];
			let col = this.sortCol;
			let dir = this.sortDir === 'asc' ? 1 : -1;
			sorted.sort((a, b) => {
				let va = a[col], vb = b[col];
				if (typeof va === 'number') return (va - vb) * dir;
				return String(va).localeCompare(String(vb)) * dir;
			});
			return sorted;
		},
		get pagedRows() {
			let start = (this.page - 1) * this.perPage;
			return this.sortedRows.slice(start, start + this.perPage);
		},
		rows: [`)

	for i, j := range jobs {
		started := j.StartedAt.Format("2006-01-02 15:04:05")
		duration := "-"
		if j.FinishedAt != nil {
			duration = j.FinishedAt.Sub(j.StartedAt).Round(time.Second).String()
		} else if j.Status == model.JobRunning {
			duration = time.Since(j.StartedAt).Round(time.Second).String()
		}
		shortID := j.BackupID
		if len(shortID) > 12 {
			shortID = shortID[:12]
		}
		if i > 0 {
			fmt.Fprint(w, `,`)
		}
		fmt.Fprintf(w, `{id:'%s',client:'%s',level:'%s',status:'%s',files:%d,size:'%s',started:'%s',duration:'%s'}`,
			shortID, j.ClientID, j.Level, string(j.Status), j.FileCount, formatSize(j.BytesNew), started, duration)
	}

	fmt.Fprint(w, `]
	}" x-init="page = 1">`)

	// Table
	fmt.Fprint(w, `<table class="w-full text-sm text-left">`)
	fmt.Fprint(w, `<thead class="border-b border-gray-200 dark:border-gray-700">`)
	fmt.Fprint(w, `<tr>`)

	// Sortable column headers
	columns := []struct{ key, label string }{
		{"id", "ID"},
		{"client", "Client"},
		{"level", "Level"},
		{"status", "Status"},
		{"files", "Files"},
		{"size", "Size"},
		{"started", "Started"},
		{"duration", "Duration"},
	}
	for _, col := range columns {
		fmt.Fprintf(w, `<th class="px-4 py-3 text-xs font-medium text-gray-500 dark:text-gray-400 uppercase tracking-wider cursor-pointer select-none hover:bg-gray-100 dark:hover:bg-gray-700 transition-colors duration-150" @click="if (sortCol === '%s') { sortDir = sortDir === 'asc' ? 'desc' : 'asc'; } else { sortCol = '%s'; sortDir = 'asc'; } page = 1;">`, col.key, col.key)
		fmt.Fprintf(w, `<div class="flex items-center gap-1"><span>%s</span>`, col.label)
		fmt.Fprintf(w, `<span class="inline-flex flex-col"><svg x-show="sortCol === '%s' && sortDir === 'asc'" class="w-3 h-3 text-blue-500" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 15l7-7 7 7"/></svg>`, col.key)
		fmt.Fprintf(w, `<svg x-show="sortCol === '%s' && sortDir === 'desc'" class="w-3 h-3 text-blue-500" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 9l-7 7-7-7"/></svg>`, col.key)
		fmt.Fprintf(w, `<svg x-show="sortCol !== '%s'" class="w-3 h-3 text-gray-400" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M7 16V4m0 0L3 8m4-4l4 4m6 0v12m0 0l4-4m-4 4l-4-4"/></svg>`, col.key)
		fmt.Fprint(w, `</span></div></th>`)
	}
	fmt.Fprint(w, `<th class="px-4 py-3 text-xs font-medium text-gray-500 dark:text-gray-400 uppercase">Actions</th>`)
	fmt.Fprint(w, `</tr></thead>`)

	// Body rows rendered by Alpine.js from pagedRows
	fmt.Fprint(w, `<tbody>`)
	fmt.Fprint(w, `<template x-for="row in pagedRows" :key="row.id + row.started">`)
	fmt.Fprint(w, `<tr class="border-b border-gray-100 dark:border-gray-700 hover:bg-gray-50 dark:hover:bg-gray-750">`)
	fmt.Fprint(w, `<td class="px-4 py-3 font-mono text-xs text-gray-700 dark:text-gray-300" x-text="row.id"></td>`)
	fmt.Fprint(w, `<td class="px-4 py-3 text-gray-700 dark:text-gray-300" x-text="row.client"></td>`)
	fmt.Fprint(w, `<td class="px-4 py-3 text-gray-700 dark:text-gray-300" x-text="row.level"></td>`)
	// Status badge with color
	fmt.Fprint(w, `<td class="px-4 py-3">`)
	fmt.Fprint(w, `<span class="inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium"
		:class="{
			'bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200': row.status === 'completed',
			'bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-200': row.status === 'running',
			'bg-red-100 text-red-800 dark:bg-red-900 dark:text-red-200': row.status === 'failed',
			'bg-gray-100 text-gray-800 dark:bg-gray-700 dark:text-gray-200': row.status === 'stopped',
			'bg-amber-100 text-amber-800 dark:bg-amber-900 dark:text-amber-200': row.status === 'expired'
		}" x-text="row.status"></span>`)
	fmt.Fprint(w, `</td>`)
	fmt.Fprint(w, `<td class="px-4 py-3 text-gray-700 dark:text-gray-300" x-text="row.files"></td>`)
	fmt.Fprint(w, `<td class="px-4 py-3 text-gray-700 dark:text-gray-300" x-text="row.size"></td>`)
	fmt.Fprint(w, `<td class="px-4 py-3 text-gray-700 dark:text-gray-300 whitespace-nowrap" x-text="row.started"></td>`)
	fmt.Fprint(w, `<td class="px-4 py-3 text-gray-700 dark:text-gray-300" x-text="row.duration"></td>`)
	// Stop/Delete action
	fmt.Fprint(w, `<td class="px-4 py-3">
		<template x-if="row.status === 'running'">
			<button class="text-red-600 hover:text-red-800 dark:text-red-400 dark:hover:text-red-300 text-xs font-semibold"
				@click="if (confirm('Stop the running backup?')) { fetch('/api/backups/stop', { method: 'POST' }); }">Stop</button>
		</template>
		<template x-if="row.status !== 'running'">
			<button class="text-red-500 hover:text-red-700 dark:text-red-400 dark:hover:text-red-300 text-xs"
				@click="$store.app.openModal('Delete Backup', row.id, '/api/backups/' + row.id, 'DELETE')">Delete</button>
		</template>
	</td>`)
	fmt.Fprint(w, `</tr>`)
	fmt.Fprint(w, `</template>`)
	fmt.Fprint(w, `</tbody></table>`)

	// Pagination controls
	fmt.Fprint(w, `<div class="flex items-center justify-between mt-4 pt-4 border-t border-gray-200 dark:border-gray-700" x-show="totalPages > 1">`)
	fmt.Fprint(w, `<p class="text-sm text-gray-600 dark:text-gray-400">Page <span x-text="page"></span> of <span x-text="totalPages"></span></p>`)
	fmt.Fprint(w, `<div class="flex gap-2">`)
	fmt.Fprint(w, `<button @click="page = Math.max(1, page - 1)" :disabled="page <= 1" class="px-3 py-1 text-sm rounded border border-gray-300 dark:border-gray-600 text-gray-700 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-700 disabled:opacity-50 disabled:cursor-not-allowed">Previous</button>`)
	fmt.Fprint(w, `<button @click="page = Math.min(totalPages, page + 1)" :disabled="page >= totalPages" class="px-3 py-1 text-sm rounded border border-gray-300 dark:border-gray-600 text-gray-700 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-700 disabled:opacity-50 disabled:cursor-not-allowed">Next</button>`)
	fmt.Fprint(w, `</div></div>`)

	fmt.Fprint(w, `</div>`) // close x-data
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
	case http.MethodGet:
		s.handleAPIRetentionList(w, r)
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
		if err == nil && v >= 0 {
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
		setErrorToast(w, "Failed to add retention policy: "+err.Error())
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Trigger toast + retentionUpdated event so the policy table refreshes immediately.
	setToastAndEvent(w, "success", "Retention policy \""+name+"\" saved", "retentionUpdated")
	w.WriteHeader(http.StatusOK)
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
		setErrorToast(w, "Failed to remove retention policy: "+err.Error())
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Trigger toast + retentionUpdated event so the policy table refreshes immediately.
	setToastAndEvent(w, "success", "Retention policy \""+name+"\" removed", "retentionUpdated")
	w.WriteHeader(http.StatusOK)
}

// handleAPIRetentionList handles GET /api/retention/policies — returns policies table HTML fragment for htmx polling.
func (s *Server) handleAPIRetentionList(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")

	if s.repo == nil {
		fmt.Fprint(w, `<p class="text-gray-500 dark:text-gray-400 italic">Database not available.</p>`)
		return
	}

	policies, err := s.repo.ListRetentionPolicies(r.Context())
	if err != nil {
		s.logger.Error("list retention policies failed", "error", err)
		fmt.Fprint(w, `<p class="text-red-500 dark:text-red-400">Failed to load policies.</p>`)
		return
	}

	if len(policies) == 0 {
		fmt.Fprint(w, `<p class="text-gray-500 dark:text-gray-400 italic">No retention policies configured. All backups are kept forever.</p>`)
		return
	}

	fmt.Fprint(w, `<table class="w-full text-sm">`)
	fmt.Fprint(w, `<thead><tr class="border-b border-gray-200 dark:border-gray-700 text-xs uppercase text-gray-500 dark:text-gray-400">`)
	fmt.Fprint(w, `<th class="px-4 py-3 text-left">Name</th>`)
	fmt.Fprint(w, `<th class="px-4 py-3 text-left">Pattern</th>`)
	fmt.Fprint(w, `<th class="px-4 py-3 text-right">Keep Days</th>`)
	fmt.Fprint(w, `<th class="px-4 py-3 text-right">Keep Versions</th>`)
	fmt.Fprint(w, `<th class="px-4 py-3 text-right">Priority</th>`)
	fmt.Fprint(w, `<th class="px-4 py-3 text-center">Enabled</th>`)
	fmt.Fprint(w, `<th class="px-4 py-3 text-center">Actions</th>`)
	fmt.Fprint(w, `</tr></thead><tbody>`)

	for _, p := range policies {
		fmt.Fprint(w, `<tr class="border-b border-gray-100 dark:border-gray-700/50">`)
		fmt.Fprintf(w, `<td class="px-4 py-3 font-medium text-gray-900 dark:text-gray-100">%s</td>`, p.Name)
		fmt.Fprintf(w, `<td class="px-4 py-3 text-gray-600 dark:text-gray-300 text-xs font-mono">%s</td>`, p.Pattern)
		if p.KeepDays != nil {
			fmt.Fprintf(w, `<td class="px-4 py-3 text-right text-gray-600 dark:text-gray-300">%d</td>`, *p.KeepDays)
		} else {
			fmt.Fprint(w, `<td class="px-4 py-3 text-right text-gray-600 dark:text-gray-300"><span class="text-gray-400">Forever</span></td>`)
		}
		if p.KeepVersions == 0 {
			fmt.Fprint(w, `<td class="px-4 py-3 text-right text-gray-600 dark:text-gray-300"><span class="text-red-600 dark:text-red-400 font-bold">PURGE</span></td>`)
		} else {
			fmt.Fprintf(w, `<td class="px-4 py-3 text-right text-gray-600 dark:text-gray-300">%d</td>`, p.KeepVersions)
		}
		fmt.Fprintf(w, `<td class="px-4 py-3 text-right text-gray-600 dark:text-gray-300">%d</td>`, p.Priority)
		if p.Enabled {
			fmt.Fprint(w, `<td class="px-4 py-3 text-center"><span class="text-green-600 dark:text-green-400">Yes</span></td>`)
		} else {
			fmt.Fprint(w, `<td class="px-4 py-3 text-center"><span class="text-gray-400">No</span></td>`)
		}
		fmt.Fprintf(w, `<td class="px-4 py-3 text-center"><button @click="$store.app.openModal('Remove Retention Policy', '%s', '/api/retention/%s', 'DELETE')" class="text-red-600 dark:text-red-400 border border-red-300 dark:border-red-600 rounded px-2 py-0.5 text-xs hover:bg-red-50 dark:hover:bg-red-900/20 transition-colors duration-150">Remove</button></td>`, p.Name, p.Name)
		fmt.Fprint(w, `</tr>`)
	}

	fmt.Fprint(w, `</tbody></table>`)
}

// handleAPIRestoreSearch handles GET /api/restore/search?query=... — search files in backup.
func (s *Server) handleAPIRestoreSearch(w http.ResponseWriter, r *http.Request) {
	clientID := strings.TrimSpace(r.URL.Query().Get("client_id"))
	repo, closeFunc, err := s.getRepoForClient(r.Context(), clientID)
	if err != nil {
		fmt.Fprintf(w, `<p class="text-red-500 dark:text-red-400">Failed to load client database: %v</p>`, err)
		return
	}
	defer closeFunc()

	query := strings.TrimSpace(r.URL.Query().Get("query"))
	if query == "" {
		fmt.Fprint(w, `<p class="text-gray-500 italic">Type to search...</p>`)
		return
	}
	if len(query) < 2 {
		fmt.Fprint(w, `<p class="text-gray-500 italic">Type at least 2 characters...</p>`)
		return
	}

	backupID := strings.TrimSpace(r.URL.Query().Get("backup_id"))
	var entries []model.BackupEntry

	if backupID != "" {
		entries, err = repo.SearchBackupFiles(r.Context(), backupID, query)
	} else {
		// Search by path pattern across all backups (fallback/legacy)
		pattern := "%" + query + "%"
		entries, err = repo.FindByPath(r.Context(), pattern)
	}

	if err != nil {
		s.logger.Error("restore search failed", "error", err, "backup_id", backupID)
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
	fmt.Fprintf(w, `<p class="text-xs text-gray-500 dark:text-gray-400 mb-3">Found %d file(s):</p>`, len(entries))
	fmt.Fprint(w, `<table class="w-full text-sm text-left">`)
	fmt.Fprint(w, `<thead class="border-b border-gray-200 dark:border-gray-700 text-xs uppercase text-gray-500 dark:text-gray-400">`)
	fmt.Fprint(w, `<tr>`)
	fmt.Fprint(w, `<th class="px-4 py-2 font-medium text-left">Path</th>`)
	fmt.Fprint(w, `<th class="px-4 py-2 font-medium text-right">Size</th>`)
	fmt.Fprint(w, `<th class="px-4 py-2 font-medium text-left">Backup Date</th>`)
	fmt.Fprint(w, `<th class="px-4 py-2 font-medium text-center">Actions</th>`)
	fmt.Fprint(w, `</tr></thead><tbody>`)
	for _, e := range entries {
		date := e.BackupDate.Format("2006-01-02 15:04")
		fmt.Fprintf(w, `<tr class="border-b border-gray-100 dark:border-gray-700/50 hover:bg-gray-50 dark:hover:bg-gray-750 transition-colors">`)
		fmt.Fprintf(w, `<td class="px-4 py-2 font-mono text-xs text-gray-400 dark:text-gray-400 break-all" title="%s">%s</td>`, e.FilePath, truncatePath(e.FilePath, 100))
		fmt.Fprintf(w, `<td class="px-4 py-2 text-right text-gray-700 dark:text-gray-300">%s</td>`, formatSize(e.FileSize))
		fmt.Fprintf(w, `<td class="px-4 py-2 text-gray-700 dark:text-gray-300 whitespace-nowrap">%s</td>`, date)
		fmt.Fprintf(w, `<td class="px-4 py-2 text-center">`)
		fmt.Fprintf(w, `<button onclick="restoreFile('%s','%s')" class="px-2.5 py-1 text-green-600 dark:text-green-400 border border-green-300 dark:border-green-800 rounded text-xs font-semibold hover:bg-green-50 dark:hover:bg-green-900/20 transition-colors">Restore</button>`, e.Blake3Hash, escapeJS(e.FilePath))
		fmt.Fprintf(w, `</td>`)
		fmt.Fprintf(w, `</tr>`)
	}
	fmt.Fprint(w, `</tbody></table>`)
}

// handleAPIRestoreBackups handles GET /api/restore/backups — lists backups for browsing.
func (s *Server) handleAPIRestoreBackups(w http.ResponseWriter, r *http.Request) {
	clientID := strings.TrimSpace(r.URL.Query().Get("client_id"))
	repo, closeFunc, err := s.getRepoForClient(r.Context(), clientID)
	if err != nil {
		fmt.Fprintf(w, `<p class="text-red-500 dark:text-red-400 italic">Failed to load client database: %v</p>`, err)
		return
	}
	defer closeFunc()

	jobs, err := repo.ListJobs(r.Context(), db.JobFilter{Limit: 100})
	if err != nil || len(jobs) == 0 {
		fmt.Fprint(w, `<p class="text-gray-500 dark:text-gray-400 italic">No backups found.</p>`)
		return
	}

	var activeJobs []model.BackupJob
	for _, j := range jobs {
		if j.FileCount > 0 {
			activeJobs = append(activeJobs, j)
		}
	}
	if len(activeJobs) == 0 {
		fmt.Fprint(w, `<p class="text-gray-500 dark:text-gray-400 italic">No backups found.</p>`)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, `<div x-data="{
		page: 1,
		perPage: 10,
		get totalPages() { return Math.ceil(this.rows.length / this.perPage); },
		get pagedRows() {
			let start = (this.page - 1) * this.perPage;
			return this.rows.slice(start, start + this.perPage);
		},
		rows: [`)

	for i, j := range activeJobs {
		date := j.StartedAt.Format("2006-01-02 15:04")
		statusStr := string(j.Status)
		statusClass := "bg-gray-100 text-gray-800 dark:bg-gray-700 dark:text-gray-200"
		switch j.Status {
		case model.JobCompleted:
			statusClass = "bg-green-100 text-green-800 dark:bg-green-900/30 dark:text-green-300"
		case model.JobFailed:
			statusClass = "bg-red-100 text-red-800 dark:bg-red-900/30 dark:text-red-300"
		case model.JobRunning:
			statusClass = "bg-blue-100 text-blue-800 dark:bg-blue-900/30 dark:text-blue-300"
		}

		if i > 0 {
			fmt.Fprint(w, `,`)
		}
		fmt.Fprintf(w, `{id:'%s',date:'%s',files:%d,status:'%s',statusClass:'%s'}`,
			j.BackupID, date, j.FileCount, statusStr, statusClass)
	}

	fmt.Fprint(w, `]
	}" x-init="page = 1">`)

	fmt.Fprint(w, `<table class="w-full text-sm text-left">`)
	fmt.Fprint(w, `<thead class="border-b border-gray-200 dark:border-gray-700 text-xs uppercase text-gray-500 dark:text-gray-400">`)
	fmt.Fprint(w, `<tr>`)
	fmt.Fprint(w, `<th class="px-4 py-3 font-medium text-left">Backup ID</th>`)
	fmt.Fprint(w, `<th class="px-4 py-3 font-medium text-left">Date</th>`)
	fmt.Fprint(w, `<th class="px-4 py-3 font-medium text-right">Files</th>`)
	fmt.Fprint(w, `<th class="px-4 py-3 font-medium text-center">Status</th>`)
	fmt.Fprint(w, `<th class="px-4 py-3 font-medium text-center">Actions</th>`)
	fmt.Fprint(w, `</tr></thead>`)
	fmt.Fprint(w, `<tbody>`)

	fmt.Fprint(w, `<template x-for="row in pagedRows" :key="row.id">`)
	fmt.Fprint(w, `<tr class="border-b border-gray-100 dark:border-gray-700/50 hover:bg-gray-50 dark:hover:bg-gray-750 transition-colors">`)
	fmt.Fprint(w, `<td class="px-4 py-3 font-mono text-xs text-gray-700 dark:text-gray-300 break-all" x-text="row.id"></td>`)
	fmt.Fprint(w, `<td class="px-4 py-3 text-gray-700 dark:text-gray-300 whitespace-nowrap" x-text="row.date"></td>`)
	fmt.Fprint(w, `<td class="px-4 py-3 text-right text-gray-700 dark:text-gray-300" x-text="row.files"></td>`)
	fmt.Fprint(w, `<td class="px-4 py-3 text-center"><span class="inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium" :class="row.statusClass" x-text="row.status"></span></td>`)
	fmt.Fprint(w, `<td class="px-4 py-3 text-center">`)
	fmt.Fprint(w, `<button @click="selectBackup(row.id)" class="px-3 py-1 bg-blue-600 hover:bg-blue-700 dark:bg-blue-600 dark:hover:bg-blue-500 text-white text-xs font-semibold rounded transition-colors shadow-sm">Select</button>`)
	fmt.Fprint(w, `</td>`)
	fmt.Fprint(w, `</tr>`)
	fmt.Fprint(w, `</template>`)
	fmt.Fprint(w, `</tbody></table>`)

	// Pagination controls
	fmt.Fprint(w, `<div class="flex items-center justify-between mt-4 pt-4 border-t border-gray-200 dark:border-gray-700" x-show="totalPages > 1">`)
	fmt.Fprint(w, `<p class="text-sm text-gray-600 dark:text-gray-400">Page <span x-text="page"></span> of <span x-text="totalPages"></span></p>`)
	fmt.Fprint(w, `<div class="flex gap-2">`)
	fmt.Fprint(w, `<button @click="page = Math.max(1, page - 1)" :disabled="page <= 1" class="px-3 py-1 text-sm rounded border border-gray-300 dark:border-gray-600 text-gray-700 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-700 disabled:opacity-50 disabled:cursor-not-allowed">Previous</button>`)
	fmt.Fprint(w, `<button @click="page = Math.min(totalPages, page + 1)" :disabled="page >= totalPages" class="px-3 py-1 text-sm rounded border border-gray-300 dark:border-gray-600 text-gray-700 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-700 disabled:opacity-50 disabled:cursor-not-allowed">Next</button>`)
	fmt.Fprint(w, `</div></div>`)

	fmt.Fprint(w, `</div>`)
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

// handleAPIActivityRecent handles GET /api/activity/recent — returns recent activity as HTML or JSON.
func (s *Server) handleAPIActivityRecent(w http.ResponseWriter, r *http.Request) {
	format := r.URL.Query().Get("format")

	if format == "json" {
		s.handleAPIActivityRecentJSON(w, r)
		return
	}

	rawLogs := observe.GetLogHistory()
	if len(rawLogs) == 0 {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<p style="color:#9ca3af; font-style:italic; font-family:monospace; font-size:12px;">No console output logs.</p>`)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, `<div style="background-color:#090d16; color:#d1d5db; font-family:monospace; font-size:12px; padding:16px; border-radius:8px; max-height:500px; overflow-y:auto; line-height:1.6; border:1px solid #1f2937;">`)
	for i, raw := range rawLogs {
		parsed := parseLogLine(raw, i)
		color := "#9ca3af" // default info
		switch parsed.Level {
		case "ERROR":
			color = "#f87171" // red
		case "WARN":
			color = "#fbbf24" // amber
		case "DEBUG":
			color = "#c084fc" // purple
		case "INFO":
			color = "#e5e7eb" // light gray
		}
		fmt.Fprintf(w, `<div style="color:%s; white-space:pre-wrap; word-break:break-all;">%s</div>`, color, html.EscapeString(parsed.Message))
	}
	fmt.Fprint(w, `</div>`)
}

// handleAPIActivityRecentJSON returns recent console logs as a JSON array for the Activity page Alpine component.
func (s *Server) handleAPIActivityRecentJSON(w http.ResponseWriter, r *http.Request) {
	rawLogs := observe.GetLogHistory()
	events := make([]jsonLogLine, 0, len(rawLogs))
	for i, raw := range rawLogs {
		parsed := parseLogLine(raw, i)
		events = append(events, parsed)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(events)
}

var logIDCounter uint64

type jsonLogLine struct {
	ID        string `json:"id"`
	Level     string `json:"level"`
	Message   string `json:"message"`
	Timestamp string `json:"timestamp"`
}

func parseLogLine(raw string, index int) jsonLogLine {
	rawClean := strings.TrimSpace(raw)
	level := "INFO"

	if strings.Contains(rawClean, "level=WARN") || strings.Contains(rawClean, `"level":"WARN"`) {
		level = "WARN"
	} else if strings.Contains(rawClean, "level=ERROR") || strings.Contains(rawClean, `"level":"ERROR"`) || strings.Contains(rawClean, "level=ERRO") {
		level = "ERROR"
	} else if strings.Contains(rawClean, "level=DEBUG") || strings.Contains(rawClean, `"level":"DEBUG"`) {
		level = "DEBUG"
	}

	timestamp := time.Now().Format(time.RFC3339)
	if idx := strings.Index(rawClean, "time="); idx != -1 {
		rem := rawClean[idx+5:]
		if endIdx := strings.Index(rem, " "); endIdx != -1 {
			timestamp = rem[:endIdx]
		} else {
			timestamp = rem
		}
	} else if idx := strings.Index(rawClean, `"time":"`); idx != -1 {
		rem := rawClean[idx+8:]
		if endIdx := strings.Index(rem, `"`); endIdx != -1 {
			timestamp = rem[:endIdx]
		}
	}

	return jsonLogLine{
		ID:        fmt.Sprintf("log-%d-%d-%d", time.Now().UnixNano(), atomic.AddUint64(&logIDCounter, 1), index),
		Level:     level,
		Message:   rawClean,
		Timestamp: timestamp,
	}
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
			setErrorToast(w, fmt.Sprintf("Failed to start watcher: %v", err))
			fmt.Fprintf(w, `<div class="p-3 bg-red-100 text-red-800 rounded text-sm">Failed to start watcher: %v</div>`, err)
			return
		}
		setSuccessToast(w, "Watcher started")
		w.Header().Set("HX-Refresh", "true")
		fmt.Fprint(w, `<div class="p-3 bg-green-100 text-green-800 rounded text-sm">Watcher started.</div>`)

	case "stop":
		if !s.watcherController.IsRunning() {
			fmt.Fprint(w, `<div class="p-3 bg-blue-100 text-blue-800 rounded text-sm">Watcher is already stopped.</div>`)
			return
		}
		if err := s.watcherController.StopWatcher(); err != nil {
			setErrorToast(w, fmt.Sprintf("Failed to stop watcher: %v", err))
			fmt.Fprintf(w, `<div class="p-3 bg-red-100 text-red-800 rounded text-sm">Failed to stop watcher: %v</div>`, err)
			return
		}
		setSuccessToast(w, "Watcher stopped")
		w.Header().Set("HX-Refresh", "true")
		fmt.Fprint(w, `<div class="p-3 bg-green-100 text-green-800 rounded text-sm">Watcher stopped.</div>`)

	default:
		http.Error(w, "invalid action", http.StatusBadRequest)
	}
}

// handleAPISystemCPU handles GET /api/system/cpu — returns CPU usage as an HTML Data_Card fragment.
func (s *Server) handleAPISystemCPU(w http.ResponseWriter, r *http.Request) {
	cpu := getCPULoad()
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, `<div class="flex items-center gap-3">`)
	fmt.Fprint(w, `<div class="flex-shrink-0 w-10 h-10 flex items-center justify-center rounded-lg bg-blue-100 dark:bg-blue-900/30 text-blue-600 dark:text-blue-400">`)
	fmt.Fprint(w, `<svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">`)
	fmt.Fprint(w, `<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 3v2m6-2v2M9 19v2m6-2v2M5 9H3m2 6H3m18-6h-2m2 6h-2M7 19h10a2 2 0 002-2V7a2 2 0 00-2-2H7a2 2 0 00-2 2v10a2 2 0 002 2zM9 9h6v6H9V9z"/>`)
	fmt.Fprint(w, `</svg></div>`)
	fmt.Fprint(w, `<div class="flex-1 min-w-0">`)
	fmt.Fprintf(w, `<p class="text-xl font-bold text-gray-900 dark:text-gray-100 truncate">%s</p>`, cpu)
	fmt.Fprint(w, `<p class="text-xs text-gray-500 dark:text-gray-400 uppercase tracking-wide">CPU Load</p>`)
	fmt.Fprint(w, `</div></div>`)
}

// handleAPISystemMemory handles GET /api/system/memory — returns memory usage as an HTML Data_Card fragment.
func (s *Server) handleAPISystemMemory(w http.ResponseWriter, r *http.Request) {
	used, total, pct := getMemoryStats()
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, `<div class="flex items-center gap-3">`)
	fmt.Fprint(w, `<div class="flex-shrink-0 w-10 h-10 flex items-center justify-center rounded-lg bg-amber-100 dark:bg-amber-900/30 text-amber-600 dark:text-amber-400">`)
	fmt.Fprint(w, `<svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">`)
	fmt.Fprint(w, `<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 11H5m14 0a2 2 0 012 2v6a2 2 0 01-2 2H5a2 2 0 01-2-2v-6a2 2 0 012-2m14 0V9a2 2 0 00-2-2M5 11V9a2 2 0 012-2m0 0V5a2 2 0 012-2h6a2 2 0 012 2v2M7 7h10"/>`)
	fmt.Fprint(w, `</svg></div>`)
	fmt.Fprint(w, `<div class="flex-1 min-w-0">`)
	fmt.Fprintf(w, `<p class="text-xl font-bold text-gray-900 dark:text-gray-100 truncate">%s</p>`, pct)
	fmt.Fprintf(w, `<p class="text-xs text-gray-500 dark:text-gray-400 uppercase tracking-wide">Memory — %s / %s</p>`, used, total)
	fmt.Fprint(w, `</div></div>`)
}

// handleAPIRestoreFiles handles GET /api/restore/files?backup_id=... — returns files in a backup as HTML table.
func (s *Server) handleAPIRestoreFiles(w http.ResponseWriter, r *http.Request) {
	clientID := strings.TrimSpace(r.URL.Query().Get("client_id"))
	repo, closeFunc, err := s.getRepoForClient(r.Context(), clientID)
	if err != nil {
		fmt.Fprintf(w, `<p class="text-red-500 dark:text-red-400">Failed to load client database: %v</p>`, err)
		return
	}
	defer closeFunc()

	backupID := strings.TrimSpace(r.URL.Query().Get("backup_id"))
	if backupID == "" {
		fmt.Fprint(w, `<p class="text-red-500 dark:text-red-400">Backup ID is required.</p>`)
		return
	}

	entries, err := repo.GetManifest(r.Context(), backupID)
	if err != nil {
		s.logger.Error("get manifest failed", "error", err)
		fmt.Fprint(w, `<p class="text-red-500 dark:text-red-400">Failed to load files.</p>`)
		return
	}

	if len(entries) == 0 {
		fmt.Fprint(w, `<p class="text-gray-500 dark:text-gray-400 italic">No files in this backup.</p>`)
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
	fmt.Fprintf(w, `<p class="text-xs text-gray-500 dark:text-gray-400 mb-3">%d file(s) in backup %s</p>`, len(files), backupID)
	fmt.Fprint(w, `<table class="w-full text-sm text-left">`)
	fmt.Fprint(w, `<thead class="border-b border-gray-200 dark:border-gray-700 text-xs uppercase text-gray-500 dark:text-gray-400">`)
	fmt.Fprint(w, `<tr>`)
	fmt.Fprint(w, `<th class="px-4 py-2 font-medium text-left">Hash</th>`)
	fmt.Fprint(w, `<th class="px-4 py-2 font-medium text-left">Path</th>`)
	fmt.Fprint(w, `<th class="px-4 py-2 font-medium text-right">Size</th>`)
	fmt.Fprint(w, `<th class="px-4 py-2 font-medium text-center">Actions</th>`)
	fmt.Fprint(w, `</tr></thead><tbody>`)

	for _, f := range files {
		fmt.Fprintf(w, `<tr class="border-b border-gray-100 dark:border-gray-700/50 hover:bg-gray-50 dark:hover:bg-gray-750 transition-colors">`)
		fmt.Fprintf(w, `<td class="px-4 py-2 font-mono text-xs text-gray-400 dark:text-gray-400">%s</td>`, f.Hash[:12])
		fmt.Fprintf(w, `<td class="px-4 py-2 font-mono text-xs text-gray-400 dark:text-gray-400 break-all" title="%s">%s</td>`, f.Path, truncatePath(f.Path, 100))
		fmt.Fprintf(w, `<td class="px-4 py-2 text-right text-gray-700 dark:text-gray-300">%s</td>`, formatSize(f.Size))
		fmt.Fprintf(w, `<td class="px-4 py-2 text-center">`)
		fmt.Fprintf(w, `<button onclick="restoreFile('%s','%s')" class="px-2.5 py-1 text-green-600 dark:text-green-400 border border-green-300 dark:border-green-800 rounded text-xs font-semibold hover:bg-green-50 dark:hover:bg-green-900/20 transition-colors">Restore</button>`, f.Hash, escapeJS(f.Path))
		fmt.Fprintf(w, `</td>`)
		fmt.Fprintf(w, `</tr>`)
	}
	fmt.Fprint(w, `</tbody></table>`)

	// Add "Restore All" button at the bottom.
	fmt.Fprintf(w, `<div class="mt-4 pt-4 border-t border-gray-200 dark:border-gray-700 flex justify-start">`)
	fmt.Fprintf(w, `<button onclick="restoreBackup('%s')" class="px-4 py-2 bg-green-600 hover:bg-green-700 dark:bg-green-600 dark:hover:bg-green-500 text-white text-xs font-semibold rounded-lg transition-colors shadow-sm">Restore All %d Files</button>`, backupID, len(files))
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
	clientID := strings.TrimSpace(r.FormValue("client_id"))

	if hash == "" && backupID == "" {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"error":"hash or backup_id required"}`)
		return
	}
	if clientID != "" && clientID != "local" && clientID != "server" {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"error":"Restores for remote clients cannot be executed on the server because decryption keys are kept client-side for security. Please run the restore directly from the remote client using the CLI."}`)
		return
	}
	if dest == "" {
		dest = "/tmp/tergum-restored"
	}

	// Run restore in background.
	go s.runWebRestore(hash, filePath, backupID, dest, clientID)

	w.Header().Set("Content-Type", "application/json")
	if backupID != "" && hash == "" {
		fmt.Fprintf(w, `{"status":"started","dest":"%s","message":"Restoring entire backup to %s"}`, escapeJS(dest), escapeJS(dest))
	} else {
		fmt.Fprintf(w, `{"status":"started","dest":"%s","message":"Restoring file to %s"}`, escapeJS(dest), escapeJS(dest))
	}
}

// runWebRestore performs a restore operation in the background.
func (s *Server) runWebRestore(hash, filePath, backupID, dest, clientID string) {
	ctx := context.Background()

	// Get client repository.
	repo, closeFunc, err := s.getRepoForClient(ctx, clientID)
	if err != nil {
		slog.Error("web restore: get repository for client failed", "client_id", clientID, "error", err)
		return
	}
	defer closeFunc()

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
	engine := restore.NewRestoreEngine(source, repo, encryptor, masterKey)

	if backupID != "" && hash == "" {
		// Restore entire backup.
		manifest, err := repo.GetManifest(ctx, backupID)
		if err != nil {
			slog.Error("web restore: get manifest failed", "error", err)
			return
		}

		var entries []restore.RestoreEntry
		for _, m := range manifest {
			found, err := repo.FindByHash(ctx, m.Blake3Hash)
			if err != nil || len(found) == 0 {
				continue
			}
			entry := found[0]
			destination := resolveDestination(dest, entry.FilePath)
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
		destination := resolveDestination(dest, filePath)
		err := engine.RestoreFile(ctx, hash, destination)
		if err != nil {
			slog.Error("web restore: file restore failed", "hash", hash, "error", err)
			// Record the failed restore in history.
			_ = repo.RecordRestore(ctx, db.RestoreRecord{
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

// resolveDestination determines where to write a restored file.
// If dest is set, files are placed under dest preserving their relative structure.
// If dest is empty, files are restored to their original paths.
func resolveDestination(dest, originalPath string) string {
	if dest == "" {
		return originalPath
	}
	// Strip volume name (e.g. "C:") on Windows or UNC prefixes
	vol := filepath.VolumeName(originalPath)
	rel := originalPath[len(vol):]
	// Strip any leading slashes or backslashes to make it a relative path component
	for len(rel) > 0 && (rel[0] == '/' || rel[0] == '\\') {
		rel = rel[1:]
	}
	return filepath.Join(dest, rel)
}

// handleAPIRestoreJobs handles GET /api/restore/jobs — returns recent restore jobs.
func (s *Server) handleAPIRestoreJobs(w http.ResponseWriter, r *http.Request) {
	if s.repo == nil {
		fmt.Fprint(w, `<p class="text-gray-500 dark:text-gray-400 italic text-sm">Database not available.</p>`)
		return
	}

	restores, err := s.repo.ListRestoreHistory(r.Context(), 10)
	if err != nil {
		s.logger.Error("list restore history failed", "error", err)
		fmt.Fprint(w, `<p class="text-red-500 dark:text-red-400 text-sm">Failed to load restore history.</p>`)
		return
	}

	if len(restores) == 0 {
		fmt.Fprint(w, `<p class="text-gray-500 dark:text-gray-400 italic text-sm">No recent restore jobs.</p>`)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")

	fmt.Fprint(w, `<div class="overflow-x-auto"><table class="w-full text-left text-sm text-gray-500 dark:text-gray-400">`)
	fmt.Fprint(w, `<thead class="text-xs text-gray-700 dark:text-gray-300 uppercase bg-gray-50 dark:bg-gray-700/50"><tr>`)
	fmt.Fprint(w, `<th class="px-4 py-3">File Name</th>`)
	fmt.Fprint(w, `<th class="px-4 py-3">Restored To</th>`)
	fmt.Fprint(w, `<th class="px-4 py-3">Restored At</th>`)
	fmt.Fprint(w, `<th class="px-4 py-3">User</th>`)
	fmt.Fprint(w, `<th class="px-4 py-3">Status</th>`)
	fmt.Fprint(w, `</tr></thead><tbody>`)

	for _, rec := range restores {
		statusText := "Success"
		statusClass := "bg-green-100 dark:bg-green-900/30 text-green-800 dark:text-green-300"
		if !rec.Success {
			statusText = "Failed"
			statusClass = "bg-red-100 dark:bg-red-900/30 text-red-800 dark:text-red-300"
		}
		date := rec.RestoredAt.Format("2006-01-02 15:04:05")

		fmt.Fprintf(w, `<tr class="border-b border-gray-200 dark:border-gray-700 hover:bg-gray-50 dark:hover:bg-gray-800/50">`)
		fmt.Fprintf(w, `<td class="px-4 py-3 font-medium text-gray-900 dark:text-white">%s</td>`, rec.FileName)
		fmt.Fprintf(w, `<td class="px-4 py-3 font-mono text-xs">%s</td>`, rec.RestoredTo)
		fmt.Fprintf(w, `<td class="px-4 py-3">%s</td>`, date)
		fmt.Fprintf(w, `<td class="px-4 py-3">%s</td>`, rec.RestoredBy)
		fmt.Fprintf(w, `<td class="px-4 py-3"><span class="inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium %s">%s</span></td>`, statusClass, statusText)
		fmt.Fprintf(w, `</tr>`)
	}
	fmt.Fprint(w, `</tbody></table></div>`)
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
		lastBackup := ci.LastBackup
		if lastBackup.IsZero() {
			lastBackup = s.resolveClientLastBackup(ci.ClientID)
		}
		if !lastBackup.IsZero() {
			cj.LastBackup = lastBackup.Format(time.RFC3339)
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
	case action == "backup" && subAction == "stop" && r.Method == http.MethodPost:
		s.handleAPIClientBackupStop(w, r, clientID)
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
	case action == "disable" && r.Method == http.MethodPost:
		s.handleAPIClientDisable(w, r, clientID)
	case action == "enable" && r.Method == http.MethodPost:
		s.handleAPIClientEnable(w, r, clientID)
	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}

// handleAPIClientBackup handles POST /api/clients/{id}/backup — triggers backup on client via RPC.
func (s *Server) handleAPIClientBackup(w http.ResponseWriter, r *http.Request, clientID string) {
	if s.clientConnector == nil {
		setErrorToast(w, "Client connector not available")
		writeJSONError(w, "client connector not available", http.StatusServiceUnavailable)
		return
	}
	if s.clientRegistry == nil {
		setErrorToast(w, "Client registry not available")
		writeJSONError(w, "registry not available", http.StatusServiceUnavailable)
		return
	}

	ci := s.clientRegistry.GetClient(clientID)
	if ci == nil {
		setErrorToast(w, fmt.Sprintf("Client %s not found", clientID))
		writeJSONError(w, "client not found", http.StatusNotFound)
		return
	}
	if ci.Status != "online" {
		setErrorToast(w, fmt.Sprintf("Client %s is offline", clientID))
		writeJSONError(w, "client is offline", http.StatusConflict)
		return
	}

	if err := s.clientConnector.TriggerClientBackup(r.Context(), clientID); err != nil {
		s.logger.Error("trigger backup on client failed", "client_id", clientID, "error", err)
		setErrorToast(w, fmt.Sprintf("Failed to trigger backup on client %s: %v", clientID, err))
		writeJSONError(w, fmt.Sprintf("trigger backup failed: %v", err), http.StatusInternalServerError)
		return
	}

	setSuccessToast(w, fmt.Sprintf("Backup triggered on client %s", clientID))
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("backup triggered on client %s", clientID),
	})
}

// handleAPIClientBackupStop handles POST /api/clients/{id}/backup/stop — stops backup on client via RPC.
func (s *Server) handleAPIClientBackupStop(w http.ResponseWriter, r *http.Request, clientID string) {
	if s.clientConnector == nil {
		setErrorToast(w, "Client connector not available")
		writeJSONError(w, "client connector not available", http.StatusServiceUnavailable)
		return
	}
	if s.clientRegistry == nil {
		setErrorToast(w, "Client registry not available")
		writeJSONError(w, "registry not available", http.StatusServiceUnavailable)
		return
	}

	ci := s.clientRegistry.GetClient(clientID)
	if ci == nil {
		setErrorToast(w, fmt.Sprintf("Client %s not found", clientID))
		writeJSONError(w, "client not found", http.StatusNotFound)
		return
	}
	if ci.Status != "online" {
		setErrorToast(w, fmt.Sprintf("Client %s is offline", clientID))
		writeJSONError(w, "client is offline", http.StatusConflict)
		return
	}

	if err := s.clientConnector.StopClientBackup(r.Context(), clientID); err != nil {
		s.logger.Error("stop backup on client failed", "client_id", clientID, "error", err)
		setErrorToast(w, fmt.Sprintf("Failed to stop backup on client %s: %v", clientID, err))
		writeJSONError(w, fmt.Sprintf("stop backup failed: %v", err), http.StatusInternalServerError)
		return
	}

	setSuccessToast(w, fmt.Sprintf("Backup stop signal sent to client %s", clientID))
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("backup stop signal sent to client %s", clientID),
	})
}

// handleAPIClientWatcherStart handles POST /api/clients/{id}/watcher/start.
func (s *Server) handleAPIClientWatcherStart(w http.ResponseWriter, r *http.Request, clientID string) {
	if s.clientConnector == nil {
		setErrorToast(w, "Client connector not available")
		writeJSONError(w, "client connector not available", http.StatusServiceUnavailable)
		return
	}
	if s.clientRegistry == nil {
		setErrorToast(w, "Client registry not available")
		writeJSONError(w, "registry not available", http.StatusServiceUnavailable)
		return
	}

	ci := s.clientRegistry.GetClient(clientID)
	if ci == nil {
		setErrorToast(w, fmt.Sprintf("Client %s not found", clientID))
		writeJSONError(w, "client not found", http.StatusNotFound)
		return
	}
	if ci.Status != "online" {
		setErrorToast(w, fmt.Sprintf("Client %s is offline", clientID))
		writeJSONError(w, "client is offline", http.StatusConflict)
		return
	}

	if err := s.clientConnector.StartClientWatcher(r.Context(), clientID); err != nil {
		s.logger.Error("start watcher on client failed", "client_id", clientID, "error", err)
		setErrorToast(w, fmt.Sprintf("Failed to start watcher on client %s: %v", clientID, err))
		writeJSONError(w, fmt.Sprintf("start watcher failed: %v", err), http.StatusInternalServerError)
		return
	}

	setSuccessToast(w, fmt.Sprintf("Watcher started on client %s", clientID))
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("watcher started on client %s", clientID),
	})
}

// handleAPIClientWatcherStop handles POST /api/clients/{id}/watcher/stop.
func (s *Server) handleAPIClientWatcherStop(w http.ResponseWriter, r *http.Request, clientID string) {
	if s.clientConnector == nil {
		setErrorToast(w, "Client connector not available")
		writeJSONError(w, "client connector not available", http.StatusServiceUnavailable)
		return
	}
	if s.clientRegistry == nil {
		setErrorToast(w, "Client registry not available")
		writeJSONError(w, "registry not available", http.StatusServiceUnavailable)
		return
	}

	ci := s.clientRegistry.GetClient(clientID)
	if ci == nil {
		setErrorToast(w, fmt.Sprintf("Client %s not found", clientID))
		writeJSONError(w, "client not found", http.StatusNotFound)
		return
	}
	if ci.Status != "online" {
		setErrorToast(w, fmt.Sprintf("Client %s is offline", clientID))
		writeJSONError(w, "client is offline", http.StatusConflict)
		return
	}

	if err := s.clientConnector.StopClientWatcher(r.Context(), clientID); err != nil {
		s.logger.Error("stop watcher on client failed", "client_id", clientID, "error", err)
		setErrorToast(w, fmt.Sprintf("Failed to stop watcher on client %s: %v", clientID, err))
		writeJSONError(w, fmt.Sprintf("stop watcher failed: %v", err), http.StatusInternalServerError)
		return
	}

	setSuccessToast(w, fmt.Sprintf("Watcher stopped on client %s", clientID))
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("watcher stopped on client %s", clientID),
	})
}

// handleAPIClientDisable handles POST /api/clients/{id}/disable — disables a client.
func (s *Server) handleAPIClientDisable(w http.ResponseWriter, r *http.Request, clientID string) {
	if s.clientRegistry == nil {
		setErrorToast(w, "Client registry not available")
		writeJSONError(w, "registry not available", http.StatusServiceUnavailable)
		return
	}

	ci := s.clientRegistry.GetClient(clientID)
	if ci == nil {
		setErrorToast(w, fmt.Sprintf("Client %s not found", clientID))
		writeJSONError(w, "client not found", http.StatusNotFound)
		return
	}

	if err := s.clientRegistry.SetDisabled(clientID, true); err != nil {
		s.logger.Error("disable client failed", "client_id", clientID, "error", err)
		setErrorToast(w, fmt.Sprintf("Failed to disable client %s: %v", clientID, err))
		writeJSONError(w, fmt.Sprintf("disable failed: %v", err), http.StatusInternalServerError)
		return
	}

	setSuccessToast(w, fmt.Sprintf("Client %s disabled", clientID))
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("client %s disabled", clientID),
	})
}

// handleAPIClientEnable handles POST /api/clients/{id}/enable — re-enables a disabled client.
func (s *Server) handleAPIClientEnable(w http.ResponseWriter, r *http.Request, clientID string) {
	if s.clientRegistry == nil {
		setErrorToast(w, "Client registry not available")
		writeJSONError(w, "registry not available", http.StatusServiceUnavailable)
		return
	}

	ci := s.clientRegistry.GetClient(clientID)
	if ci == nil {
		setErrorToast(w, fmt.Sprintf("Client %s not found", clientID))
		writeJSONError(w, "client not found", http.StatusNotFound)
		return
	}

	if err := s.clientRegistry.SetDisabled(clientID, false); err != nil {
		s.logger.Error("enable client failed", "client_id", clientID, "error", err)
		setErrorToast(w, fmt.Sprintf("Failed to enable client %s: %v", clientID, err))
		writeJSONError(w, fmt.Sprintf("enable failed: %v", err), http.StatusInternalServerError)
		return
	}

	setSuccessToast(w, fmt.Sprintf("Client %s enabled", clientID))
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("client %s enabled", clientID),
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

// handleAPIClientsList handles GET /api/clients/list — returns client cards as HTML fragment for htmx polling.
func (s *Server) handleAPIClientsList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "text/html")

	if s.clientRegistry == nil {
		fmt.Fprint(w, `<p class="text-gray-500 dark:text-gray-400 italic col-span-full">No client registry available.</p>`)
		return
	}

	clients := s.clientRegistry.ListClients()
	if len(clients) == 0 {
		fmt.Fprint(w, `<p class="text-gray-500 dark:text-gray-400 italic col-span-full">No clients registered.</p>`)
		return
	}

	fmt.Fprint(w, `<div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">`)
	for _, ci := range clients {
		// Derive online/offline from the heartbeat timestamp directly so the
		// UI reflects a dropped connection within one poll cycle (~5 s) rather
		// than waiting up to 30 s for the registry background checker.
		// Treat clients whose LastSeen is more than 90 s ago as offline,
		// matching the registry's default offlineThreshold.
		const clientOfflineThreshold = 90 * time.Second
		isOffline := ci.Status != "online"
		if !isOffline && !ci.LastSeen.IsZero() {
			if time.Since(ci.LastSeen) > clientOfflineThreshold {
				isOffline = true
			}
		}
		borderClass := "border-gray-200 dark:border-gray-700"
		if isOffline {
			borderClass = "border-amber-400 dark:border-amber-500"
		}

		lastSeen := "never"
		if !ci.LastSeen.IsZero() {
			lastSeen = ci.LastSeen.Local().Format("2006-01-02 15:04:05")
		}
		lastBackup := "never"
		lastBackupTime := ci.LastBackup
		if lastBackupTime.IsZero() {
			lastBackupTime = s.resolveClientLastBackup(ci.ClientID)
		}
		if !lastBackupTime.IsZero() {
			lastBackup = lastBackupTime.Local().Format("2006-01-02 15:04:05")
		}

		watcherStatus := "Stopped"
		watcherColor := "text-gray-500"
		// Only show the watcher as running if the client is actually online.
		// An offline client cannot have an active watcher.
		if ci.WatcherActive && !isOffline {
			watcherStatus = "Running"
			watcherColor = "text-green-600 dark:text-green-400"
		}

		clientStatus := "Idle"
		clientStatusColor := "text-gray-500"
		clientStatusDetail := ""
		if !isOffline && s.clientConnector != nil {
			// Query client status with a very short timeout.
			ctx, cancel := context.WithTimeout(r.Context(), 500*time.Millisecond)
			status, err := s.clientConnector.GetClientStatus(ctx, ci.ClientID)
			cancel()
			if err == nil && status != nil {
				// Use the real-time watcher state from the client, which is
				// authoritative over the registry's potentially stale value.
				if status.WatcherActive {
					watcherStatus = "Running"
					watcherColor = "text-green-600 dark:text-green-400"
				} else {
					watcherStatus = "Stopped"
					watcherColor = "text-gray-500"
				}

				if status.Status == "running" {
					clientStatus = "Running"
					clientStatusColor = "text-blue-600 dark:text-blue-400 font-semibold"
					if status.FilesProcessed > 0 {
						clientStatusDetail = fmt.Sprintf(" (%d files, %s)", status.FilesProcessed, formatSize(status.BytesTransferred))
					} else {
						clientStatusDetail = " (starting...)"
					}
				}
			}
		}

		// Card start
		fmt.Fprintf(w, `<div class="bg-white dark:bg-gray-800 border %s rounded-lg p-4 space-y-3">`, borderClass)

		// Header: Client ID + status dot + offline badge
		fmt.Fprint(w, `<div class="flex items-center justify-between">`)
		fmt.Fprintf(w, `<span class="font-semibold text-sm text-gray-800 dark:text-gray-100 truncate" title="%s">%s</span>`, ci.ClientID, ci.ClientID)
		fmt.Fprint(w, `<div class="flex items-center gap-2">`)
		if isOffline {
			fmt.Fprint(w, `<span class="text-xs font-medium px-2 py-0.5 rounded-full bg-amber-100 text-amber-800 dark:bg-amber-900 dark:text-amber-200">Offline</span>`)
			fmt.Fprint(w, `<span class="w-3 h-3 rounded-full bg-red-500 shrink-0"></span>`)
		} else if ci.Disabled {
			fmt.Fprint(w, `<span class="text-xs font-medium px-2 py-0.5 rounded-full bg-gray-200 text-gray-600 dark:bg-gray-700 dark:text-gray-400">Disabled</span>`)
			fmt.Fprint(w, `<span class="w-3 h-3 rounded-full bg-gray-400 shrink-0"></span>`)
		} else {
			fmt.Fprint(w, `<span class="w-3 h-3 rounded-full bg-green-500 shrink-0"></span>`)
		}
		fmt.Fprint(w, `</div></div>`)

		// Details
		fmt.Fprint(w, `<div class="space-y-1 text-xs text-gray-600 dark:text-gray-400">`)
		fmt.Fprintf(w, `<p><span class="font-medium">Last seen:</span> %s</p>`, lastSeen)
		fmt.Fprintf(w, `<p><span class="font-medium">Last backup:</span> %s</p>`, lastBackup)
		fmt.Fprintf(w, `<p><span class="font-medium">Watcher:</span> <span class="%s">%s</span></p>`, watcherColor, watcherStatus)
		if ci.Disabled {
			fmt.Fprint(w, `<p><span class="font-medium">Status:</span> <span class="text-gray-400">Disabled</span></p>`)
		} else if clientStatus == "Running" {
			fmt.Fprintf(w, `<p><span class="font-medium">Status:</span> <span class="inline-flex items-center %s"><span class="w-2 h-2 rounded-full bg-blue-500 animate-pulse mr-1"></span>%s%s</span></p>`, clientStatusColor, clientStatus, clientStatusDetail)
		} else {
			fmt.Fprintf(w, `<p><span class="font-medium">Status:</span> <span class="%s">%s</span></p>`, clientStatusColor, clientStatus)
		}
		fmt.Fprint(w, `</div>`)

		// Action buttons
		disabledAttr := ""
		disabledClass := "hover:bg-blue-50 dark:hover:bg-gray-700 cursor-pointer"
		if isOffline || ci.Disabled {
			disabledAttr = " disabled"
			disabledClass = "opacity-50 cursor-not-allowed"
		}
		fmt.Fprint(w, `<div class="flex flex-wrap gap-2 pt-2 border-t border-gray-100 dark:border-gray-700">`)
		if ci.Disabled {
			// Disabled client: only show Enable button.
			fmt.Fprintf(w, `<button hx-post="/api/clients/%s/enable" hx-swap="none" class="text-xs px-2.5 py-1.5 rounded border border-green-200 dark:border-green-700 text-green-700 dark:text-green-300 hover:bg-green-50 dark:hover:bg-gray-700 cursor-pointer">Enable Client</button>`, ci.ClientID)
		} else {
			if clientStatus == "Running" {
				fmt.Fprintf(w, `<button hx-post="/api/clients/%s/backup/stop" hx-swap="none" class="text-xs px-2.5 py-1.5 rounded border border-red-200 dark:border-red-700 text-red-700 dark:text-red-300 hover:bg-red-50 dark:hover:bg-gray-700 cursor-pointer">Stop Backup</button>`, ci.ClientID)
			} else {
				fmt.Fprintf(w, `<button hx-post="/api/clients/%s/backup" hx-swap="none" class="text-xs px-2.5 py-1.5 rounded border border-blue-200 dark:border-blue-700 text-blue-700 dark:text-blue-300 %s"%s>Trigger Backup</button>`, ci.ClientID, disabledClass, disabledAttr)
			}
			fmt.Fprintf(w, `<button hx-post="/api/clients/%s/watcher/start" hx-swap="none" class="text-xs px-2.5 py-1.5 rounded border border-green-200 dark:border-green-700 text-green-700 dark:text-green-300 %s"%s>Start Watcher</button>`, ci.ClientID, disabledClass, disabledAttr)
			fmt.Fprintf(w, `<a href="/restore?client=%s" hx-get="/restore?client=%s" hx-target="#content-area" hx-push-url="true" class="text-xs px-2.5 py-1.5 rounded border border-yellow-200 dark:border-yellow-700 text-yellow-700 dark:text-yellow-300 hover:bg-yellow-50 dark:hover:bg-gray-700 cursor-pointer no-underline">Restore</a>`, ci.ClientID, ci.ClientID)
			fmt.Fprintf(w, `<button hx-post="/api/clients/%s/watcher/stop" hx-swap="none" class="text-xs px-2.5 py-1.5 rounded border border-red-200 dark:border-red-700 text-red-700 dark:text-red-300 %s"%s>Stop Watcher</button>`, ci.ClientID, disabledClass, disabledAttr)
			fmt.Fprintf(w, `<button hx-post="/api/clients/%s/disable" hx-swap="none" hx-confirm="Disable client %s? It will no longer receive backups or be monitored." class="text-xs px-2.5 py-1.5 rounded border border-gray-300 dark:border-gray-600 text-gray-600 dark:text-gray-400 hover:bg-gray-100 dark:hover:bg-gray-700 cursor-pointer">Disable</button>`, ci.ClientID, ci.ClientID)
		}
		fmt.Fprint(w, `</div>`)

		// Card end
		fmt.Fprint(w, `</div>`)
	}
	fmt.Fprint(w, `</div>`)
}

// handleAPIMetricsCards handles GET /api/metrics/cards — returns all metric Data_Cards as HTML fragment for bulk polling.
func (s *Server) handleAPIMetricsCards(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "text/html")

	// Gather metrics data (same logic as handleMetrics).
	metrics := metricsView{
		FilesBackedUp:     0,
		BytesTransferred:  "0 B",
		DedupRatio:        "0%",
		DedupRatioPercent: 0,
		StorageUsed:       "0 B",
		StoragePercent:    0,
		StorageColor:      "blue",
		UniqueFiles:       0,
		GRPCRequests:      0,
		GRPCErrors:        0,
		ConnectedClients:  0,
	}

	if s.repo != nil {
		var totalFiles, totalBytes, totalDeduped int64

		// Local jobs.
		jobs, err := s.repo.ListJobs(r.Context(), db.JobFilter{})
		if err == nil {
			for _, j := range jobs {
				totalFiles += j.FileCount
				totalBytes += j.BytesNew
				totalDeduped += j.FilesDeduped
			}
		}

		// Remote client jobs.
		if s.clientRegistry != nil {
			clients := s.clientRegistry.ListClients()
			for _, client := range clients {
				clientRepo, cleanup, err := s.getRepoForClient(r.Context(), client.ClientID)
				if err != nil {
					continue
				}
				cJobs, err := clientRepo.ListJobs(r.Context(), db.JobFilter{})
				if err == nil {
					for _, j := range cJobs {
						totalFiles += j.FileCount
						totalBytes += j.BytesNew
						totalDeduped += j.FilesDeduped
					}
				}
				cleanup()
			}
		}

		metrics.FilesBackedUp = totalFiles
		metrics.BytesTransferred = formatSize(totalBytes)

		if totalFiles > 0 {
			ratio := float64(totalDeduped) / float64(totalFiles) * 100
			metrics.DedupRatio = fmt.Sprintf("%.1f%%", ratio)
			metrics.DedupRatioPercent = ratio
		}

		paths, err := s.repo.GetAllFilePaths(r.Context())
		if err == nil {
			metrics.UniqueFiles = int64(len(paths))
		}
	}

	// Get storage size and disk usage percent.
	var storageDir string
	if s.fullCfg != nil {
		storageDir = s.fullCfg.StorageDir()
	} else if s.configPath != "" {
		cfg, err := config.Load(s.configPath)
		if err == nil {
			storageDir = cfg.StorageDir()
		}
	}
	if storageDir != "" {
		metrics.StorageUsed = formatSize(dirSizeQuick(storageDir))
		metrics.StoragePercent = diskUsagePercent(storageDir)
		metrics.StorageColor = StorageColorScheme(metrics.StoragePercent)
	}

	// Count connected clients.
	if s.clientRegistry != nil {
		clients := s.clientRegistry.ListClients()
		count := 0
		for _, ci := range clients {
			if ci.Status == "online" {
				count++
			}
		}
		metrics.ConnectedClients = count
	}

	// Render each Data_Card as HTML.
	cardClass := `bg-white dark:bg-gray-800 rounded-lg shadow-sm p-4 border border-gray-200 dark:border-gray-700`

	// Files Backed Up
	fmt.Fprintf(w, `<div class="%s">`, cardClass)
	fmt.Fprint(w, `<div class="flex items-center gap-3">`)
	fmt.Fprint(w, `<div class="flex-shrink-0 w-10 h-10 flex items-center justify-center rounded-lg bg-blue-100 dark:bg-blue-900/30 text-blue-600 dark:text-blue-400">`)
	fmt.Fprint(w, `<svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 19a2 2 0 01-2-2V7a2 2 0 012-2h4l2 2h4a2 2 0 012 2v1M5 19h14a2 2 0 002-2v-5a2 2 0 00-2-2H9a2 2 0 00-2 2v5a2 2 0 01-2 2z"/></svg>`)
	fmt.Fprint(w, `</div>`)
	fmt.Fprint(w, `<div class="flex-1 min-w-0">`)
	fmt.Fprint(w, `<p class="text-xs font-medium text-gray-500 dark:text-gray-400 uppercase tracking-wide">Total Files</p>`)
	fmt.Fprintf(w, `<p class="text-2xl font-bold text-gray-900 dark:text-white mt-1">%d</p>`, metrics.FilesBackedUp)
	fmt.Fprint(w, `</div></div></div>`)

	// Bytes Transferred
	fmt.Fprintf(w, `<div class="%s">`, cardClass)
	fmt.Fprint(w, `<div class="flex items-center gap-3">`)
	fmt.Fprint(w, `<div class="flex-shrink-0 w-10 h-10 flex items-center justify-center rounded-lg bg-emerald-100 dark:bg-emerald-900/30 text-emerald-600 dark:text-emerald-400">`)
	fmt.Fprint(w, `<svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M7 16a4 4 0 01-.88-7.903A5 5 0 1115.9 6L16 6a5 5 0 011 9.9M15 13l-3-3m0 0l-3 3m3-3v12"/></svg>`)
	fmt.Fprint(w, `</div>`)
	fmt.Fprint(w, `<div class="flex-1 min-w-0">`)
	fmt.Fprint(w, `<p class="text-xs font-medium text-gray-500 dark:text-gray-400 uppercase tracking-wide">Bytes Transferred</p>`)
	fmt.Fprintf(w, `<p class="text-2xl font-bold text-gray-900 dark:text-white mt-1">%s</p>`, metrics.BytesTransferred)
	fmt.Fprint(w, `</div></div></div>`)

	// Deduplication Ratio (with progress bar)
	fmt.Fprintf(w, `<div class="%s">`, cardClass)
	fmt.Fprint(w, `<div class="flex items-center gap-3">`)
	fmt.Fprint(w, `<div class="flex-shrink-0 w-10 h-10 flex items-center justify-center rounded-lg bg-indigo-100 dark:bg-indigo-900/30 text-indigo-600 dark:text-indigo-400">`)
	fmt.Fprint(w, `<svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 19v-6a2 2 0 00-2-2H5a2 2 0 00-2 2v6a2 2 0 002 2h2a2 2 0 002-2zm0 0V9a2 2 0 012-2h2a2 2 0 012 2v10m-6 0a2 2 0 002 2h2a2 2 0 002-2m0 0V5a2 2 0 012-2h2a2 2 0 012 2v14a2 2 0 01-2 2h-2a2 2 0 01-2-2z"/></svg>`)
	fmt.Fprint(w, `</div>`)
	fmt.Fprint(w, `<div class="flex-1 min-w-0">`)
	fmt.Fprint(w, `<p class="text-xs font-medium text-gray-500 dark:text-gray-400 uppercase tracking-wide">Dedup Ratio</p>`)
	fmt.Fprintf(w, `<p class="text-2xl font-bold text-gray-900 dark:text-white mt-1">%s</p>`, metrics.DedupRatio)
	fmt.Fprint(w, `</div></div>`)
	// Progress bar for dedup ratio
	fmt.Fprint(w, `<div class="mt-3 h-2 bg-gray-200 dark:bg-gray-700 rounded-full overflow-hidden">`)
	fmt.Fprintf(w, `<div class="h-full bg-indigo-500 rounded-full transition-all duration-300" style="width: %.1f%%"></div>`, metrics.DedupRatioPercent)
	fmt.Fprint(w, `</div>`)
	fmt.Fprint(w, `</div>`)

	// Storage Used (with progress bar and color scheme)
	storageColor := metrics.StorageColor
	storageBorderClass := ""
	storageIconBg := fmt.Sprintf("bg-%s-100 dark:bg-%s-900/30 text-%s-600 dark:text-%s-400", storageColor, storageColor, storageColor, storageColor)
	storageBarBg := fmt.Sprintf("bg-%s-500", storageColor)
	switch storageColor {
	case "amber":
		storageBorderClass = `bg-white dark:bg-gray-800 rounded-lg shadow-sm p-4 border border-amber-300 dark:border-amber-600`
		storageIconBg = "bg-amber-100 dark:bg-amber-900/30 text-amber-600 dark:text-amber-400"
		storageBarBg = "bg-amber-500"
	case "red":
		storageBorderClass = `bg-white dark:bg-gray-800 rounded-lg shadow-sm p-4 border border-red-300 dark:border-red-600`
		storageIconBg = "bg-red-100 dark:bg-red-900/30 text-red-600 dark:text-red-400"
		storageBarBg = "bg-red-500"
	default:
		storageBorderClass = cardClass
		storageIconBg = "bg-purple-100 dark:bg-purple-900/30 text-purple-600 dark:text-purple-400"
		storageBarBg = "bg-purple-500"
	}

	fmt.Fprintf(w, `<div class="%s">`, storageBorderClass)
	fmt.Fprint(w, `<div class="flex items-center gap-3">`)
	fmt.Fprintf(w, `<div class="flex-shrink-0 w-10 h-10 flex items-center justify-center rounded-lg %s">`, storageIconBg)
	fmt.Fprint(w, `<svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 7v10c0 2.21 3.582 4 8 4s8-1.79 8-4V7M4 7c0 2.21 3.582 4 8 4s8-1.79 8-4M4 7c0-2.21 3.582-4 8-4s8 1.79 8 4m0 5c0 2.21-3.582 4-8 4s-8-1.79-8-4"/></svg>`)
	fmt.Fprint(w, `</div>`)
	fmt.Fprint(w, `<div class="flex-1 min-w-0">`)
	fmt.Fprint(w, `<p class="text-xs font-medium text-gray-500 dark:text-gray-400 uppercase tracking-wide">Storage Used</p>`)
	fmt.Fprintf(w, `<p class="text-2xl font-bold text-gray-900 dark:text-white mt-1">%s</p>`, metrics.StorageUsed)
	fmt.Fprint(w, `</div></div>`)
	// Progress bar for storage capacity
	fmt.Fprint(w, `<div class="mt-3 h-2 bg-gray-200 dark:bg-gray-700 rounded-full overflow-hidden">`)
	fmt.Fprintf(w, `<div class="h-full %s rounded-full transition-all duration-300" style="width: %.1f%%"></div>`, storageBarBg, metrics.StoragePercent)
	fmt.Fprint(w, `</div>`)
	fmt.Fprintf(w, `<p class="mt-1 text-xs text-gray-400 dark:text-gray-500">%.1f%% of disk space</p>`, metrics.StoragePercent)
	fmt.Fprint(w, `</div>`)

	// Unique Files
	fmt.Fprintf(w, `<div class="%s">`, cardClass)
	fmt.Fprint(w, `<div class="flex items-center gap-3">`)
	fmt.Fprint(w, `<div class="flex-shrink-0 w-10 h-10 flex items-center justify-center rounded-lg bg-teal-100 dark:bg-teal-900/30 text-teal-600 dark:text-teal-400">`)
	fmt.Fprint(w, `<svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z"/></svg>`)
	fmt.Fprint(w, `</div>`)
	fmt.Fprint(w, `<div class="flex-1 min-w-0">`)
	fmt.Fprint(w, `<p class="text-xs font-medium text-gray-500 dark:text-gray-400 uppercase tracking-wide">Unique Files</p>`)
	fmt.Fprintf(w, `<p class="text-2xl font-bold text-gray-900 dark:text-white mt-1">%d</p>`, metrics.UniqueFiles)
	fmt.Fprint(w, `</div></div></div>`)

	// gRPC Requests
	fmt.Fprintf(w, `<div class="%s">`, cardClass)
	fmt.Fprint(w, `<div class="flex items-center gap-3">`)
	fmt.Fprint(w, `<div class="flex-shrink-0 w-10 h-10 flex items-center justify-center rounded-lg bg-cyan-100 dark:bg-cyan-900/30 text-cyan-600 dark:text-cyan-400">`)
	fmt.Fprint(w, `<svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M13 10V3L4 14h7v7l9-11h-7z"/></svg>`)
	fmt.Fprint(w, `</div>`)
	fmt.Fprint(w, `<div class="flex-1 min-w-0">`)
	fmt.Fprint(w, `<p class="text-xs font-medium text-gray-500 dark:text-gray-400 uppercase tracking-wide">gRPC Requests</p>`)
	fmt.Fprintf(w, `<p class="text-2xl font-bold text-gray-900 dark:text-white mt-1">%d</p>`, metrics.GRPCRequests)
	fmt.Fprint(w, `</div></div></div>`)

	// gRPC Errors
	fmt.Fprintf(w, `<div class="%s">`, cardClass)
	fmt.Fprint(w, `<div class="flex items-center gap-3">`)
	fmt.Fprint(w, `<div class="flex-shrink-0 w-10 h-10 flex items-center justify-center rounded-lg bg-rose-100 dark:bg-rose-900/30 text-rose-600 dark:text-rose-400">`)
	fmt.Fprint(w, `<svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z"/></svg>`)
	fmt.Fprint(w, `</div>`)
	fmt.Fprint(w, `<div class="flex-1 min-w-0">`)
	fmt.Fprint(w, `<p class="text-xs font-medium text-gray-500 dark:text-gray-400 uppercase tracking-wide">gRPC Errors</p>`)
	fmt.Fprintf(w, `<p class="text-2xl font-bold text-gray-900 dark:text-white mt-1">%d</p>`, metrics.GRPCErrors)
	fmt.Fprint(w, `</div></div></div>`)

	// Connected Clients
	fmt.Fprintf(w, `<div class="%s">`, cardClass)
	fmt.Fprint(w, `<div class="flex items-center gap-3">`)
	fmt.Fprint(w, `<div class="flex-shrink-0 w-10 h-10 flex items-center justify-center rounded-lg bg-indigo-100 dark:bg-indigo-900/30 text-indigo-600 dark:text-indigo-400">`)
	fmt.Fprint(w, `<svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M17 20h5v-2a3 3 0 00-5.356-1.857M17 20H7m10 0v-2c0-.656-.126-1.283-.356-1.857M7 20H2v-2a3 3 0 015.356-1.857M7 20v-2c0-.656.126-1.283.356-1.857m0 0a5.002 5.002 0 019.288 0M15 7a3 3 0 11-6 0 3 3 0 016 0zm6 3a2 2 0 11-4 0 2 2 0 014 0zM7 10a2 2 0 11-4 0 2 2 0 014 0z"/></svg>`)
	fmt.Fprint(w, `</div>`)
	fmt.Fprint(w, `<div class="flex-1 min-w-0">`)
	fmt.Fprint(w, `<p class="text-xs font-medium text-gray-500 dark:text-gray-400 uppercase tracking-wide">Connected Clients</p>`)
	fmt.Fprintf(w, `<p class="text-2xl font-bold text-gray-900 dark:text-white mt-1">%d</p>`, metrics.ConnectedClients)
	fmt.Fprint(w, `</div></div></div>`)
}

// writeJSONError writes a JSON error response.
func writeJSONError(w http.ResponseWriter, message string, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}

// getRepoForClient returns a db.Repository for the specified clientID.
// If clientID is "local", "server", or empty, it returns the server's own database.
// Otherwise, it opens the SQLite database file for that client inside the clients/ directory.
func (s *Server) getRepoForClient(ctx context.Context, clientID string) (db.Repository, func(), error) {
	if clientID == "" || clientID == "local" || clientID == "server" {
		return s.repo, func() {}, nil
	}

	var dbDir string
	if s.fullCfg != nil {
		dbDir = filepath.Dir(s.fullCfg.Database.Path)
	} else if s.configPath != "" {
		cfg, err := config.Load(s.configPath)
		if err == nil {
			dbDir = filepath.Dir(cfg.Database.Path)
		}
	}
	if dbDir == "" {
		dbDir = "."
	}
	dbPath := filepath.Join(dbDir, "clients", clientID+".db")

	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return nil, nil, fmt.Errorf("client %q database copy not found on server", clientID)
	}

	repo, err := db.NewRepository(dbPath, false)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open client database copy: %w", err)
	}

	// Clean up stale "running" jobs that have been running for more than 24 hours.
	// This handles cases where a DB sync after backup completion failed, leaving
	// the server's copy of the client DB with a permanently "running" job.
	if affected, cleanErr := repo.FailStaleJobsOlderThan(ctx, 24*time.Hour, "Stale: DB sync likely failed after completion"); cleanErr == nil && affected > 0 {
		s.logger.Info("cleaned stale running jobs in client DB copy",
			"client_id", clientID, "count", affected)
	}

	closeFunc := func() {
		_ = repo.Close()
	}

	return repo, closeFunc, nil
}

// resolveClientLastBackup queries a client's synced database for the most
// recent completed backup time. Used as a fallback when the registry's
// last_backup field is empty (e.g., for clients that registered before the
// OnSync hook was added).
func (s *Server) resolveClientLastBackup(clientID string) time.Time {
	var dbDir string
	if s.fullCfg != nil {
		dbDir = filepath.Dir(s.fullCfg.Database.Path)
	} else if s.configPath != "" {
		cfg, err := config.Load(s.configPath)
		if err == nil {
			dbDir = filepath.Dir(cfg.Database.Path)
		}
	}
	if dbDir == "" {
		return time.Time{}
	}

	dbPath := filepath.Join(dbDir, "clients", clientID+".db")
	if _, err := os.Stat(dbPath); err != nil {
		return time.Time{}
	}

	repo, err := db.NewRepository(dbPath, false)
	if err != nil {
		return time.Time{}
	}
	defer repo.Close()

	completed := model.JobCompleted
	jobs, err := repo.ListJobs(context.Background(), db.JobFilter{Status: &completed, Limit: 1})
	if err != nil || len(jobs) == 0 {
		return time.Time{}
	}

	if jobs[0].FinishedAt != nil {
		return *jobs[0].FinishedAt
	}
	return time.Time{}
}

// handleAPIRestoreRemote handles POST /api/restore/remote — restores files from a remote client's
// backup using the client's encryption passphrase provided at request time.
// This enables cross-client restores from the WebUI without requiring CLI access.
func (s *Server) handleAPIRestoreRemote(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := r.ParseForm(); err != nil {
		writeRemoteRestoreJSON(w, map[string]string{"error": "invalid form data"})
		return
	}

	clientID := strings.TrimSpace(r.FormValue("client_id"))
	passphrase := r.FormValue("passphrase")
	backupID := strings.TrimSpace(r.FormValue("backup_id"))
	query := strings.TrimSpace(r.FormValue("query"))
	dest := strings.TrimSpace(r.FormValue("dest"))
	hash := strings.TrimSpace(r.FormValue("hash"))
	filePath := strings.TrimSpace(r.FormValue("path"))
	targetClient := strings.TrimSpace(r.FormValue("target_client"))

	// Validate required fields.
	if clientID == "" || clientID == "local" || clientID == "server" {
		writeRemoteRestoreJSON(w, map[string]string{"error": "A remote client_id is required for remote restore."})
		return
	}
	if passphrase == "" {
		writeRemoteRestoreJSON(w, map[string]string{"error": "Encryption passphrase is required to decrypt remote client backups."})
		return
	}
	if hash == "" && backupID == "" && query == "" {
		writeRemoteRestoreJSON(w, map[string]string{"error": "Provide a hash, backup_id, or search query."})
		return
	}
	if dest == "" {
		dest = "/tmp/tergum-restored"
	}

	// Open client database.
	repo, closeFunc, err := s.getRepoForClient(r.Context(), clientID)
	if err != nil {
		writeRemoteRestoreJSON(w, map[string]string{"error": fmt.Sprintf("Failed to open client database: %v", err)})
		return
	}

	// Derive master key from passphrase + client salt.
	// First try to get salt from the client's database (encryption_salt config key).
	var salt []byte
	if saltHex, err := repo.GetConfig(r.Context(), "encryption_salt"); err == nil && saltHex != "" {
		if s, err := hex.DecodeString(saltHex); err == nil {
			salt = s
		}
	}

	// Fallback: try the client salt file on disk.
	if len(salt) == 0 {
		var dbDir string
		if s.fullCfg != nil {
			dbDir = filepath.Dir(s.fullCfg.Database.Path)
		} else if s.configPath != "" {
			cfg, err := config.Load(s.configPath)
			if err == nil {
				dbDir = filepath.Dir(cfg.Database.Path)
			}
		}
		if dbDir != "" {
			clientSaltPath := filepath.Join(dbDir, "clients", clientID+".salt")
			if saltHex, err := os.ReadFile(clientSaltPath); err == nil {
				if s, err := hex.DecodeString(strings.TrimSpace(string(saltHex))); err == nil {
					salt = s
				}
			}
		}
	}

	if len(salt) == 0 {
		closeFunc()
		writeRemoteRestoreJSON(w, map[string]string{"error": "Could not find encryption salt for this client. Ensure the client has synced its database to the server."})
		return
	}

	encryptor := cryptoPkg.NewEncryptor()
	masterKey, err := encryptor.DeriveKey(passphrase, salt)
	if err != nil {
		closeFunc()
		writeRemoteRestoreJSON(w, map[string]string{"error": fmt.Sprintf("Key derivation failed: %v", err)})
		return
	}

	// Verify the derived key against key_verify if available in the client DB.
	if verifyData, err := repo.GetConfig(r.Context(), "key_verify"); err == nil && verifyData != "" {
		valid, verifyErr := encryptor.VerifyMasterKey(masterKey, verifyData)
		if verifyErr != nil || !valid {
			closeFunc()
			writeRemoteRestoreJSON(w, map[string]string{"error": "Invalid passphrase: decryption key verification failed. Please check the passphrase for this client."})
			return
		}
	} else {
		// Fallback: check for key_verify file on disk (clients/{id}.key_verify).
		var dbDir string
		if s.fullCfg != nil {
			dbDir = filepath.Dir(s.fullCfg.Database.Path)
		} else if s.configPath != "" {
			cfg, err := config.Load(s.configPath)
			if err == nil {
				dbDir = filepath.Dir(cfg.Database.Path)
			}
		}
		if dbDir != "" {
			verifyPath := filepath.Join(dbDir, "clients", clientID+".key_verify")
			if verifyBytes, err := os.ReadFile(verifyPath); err == nil {
				valid, verifyErr := encryptor.VerifyMasterKey(masterKey, strings.TrimSpace(string(verifyBytes)))
				if verifyErr != nil || !valid {
					closeFunc()
					writeRemoteRestoreJSON(w, map[string]string{"error": "Invalid passphrase: decryption key verification failed. Please check the passphrase for this client."})
					return
				}
			}
		}
	}

	// Determine storage directory.
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
	if storageDir == "" && s.fullCfg != nil {
		storageDir = s.fullCfg.StorageDir()
	}

	if storageDir == "" {
		closeFunc()
		writeRemoteRestoreJSON(w, map[string]string{"error": "Storage directory not configured on server."})
		return
	}

	// Run restore in background.
	go s.runRemoteRestore(repo, closeFunc, encryptor, masterKey, storageDir, clientID, hash, filePath, backupID, query, dest, targetClient)

	// Build an informative message showing the actual server path where files will land.
	var msg string
	if targetClient != "" {
		msg = fmt.Sprintf("Remote restore started: pushing files from client '%s' to target client '%s' (dest: %s)", clientID, targetClient, dest)
	} else if query != "" {
		// For query restores, show the resolved base path as best we can.
		resolvedExample := resolveDestination(dest, query)
		msg = fmt.Sprintf("Remote restore started for client '%s' back to server location %s", clientID, filepath.Dir(resolvedExample))
	} else if filePath != "" {
		resolved := resolveDestination(dest, filePath)
		msg = fmt.Sprintf("Remote restore started for client '%s' back to server location %s", clientID, filepath.Dir(resolved))
	} else {
		// Full backup restore — files will be under dest with original structure.
		absDest, _ := filepath.Abs(dest)
		msg = fmt.Sprintf("Remote restore started for client '%s' back to server location %s", clientID, absDest)
	}

	writeRemoteRestoreJSON(w, map[string]string{
		"status":  "started",
		"dest":    dest,
		"message": msg,
	})
}

// writeRemoteRestoreJSON writes a JSON response with proper encoding (handles special chars in paths).
func writeRemoteRestoreJSON(w http.ResponseWriter, data map[string]string) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

// runRemoteRestore performs a remote client restore operation in the background.
// It uses the provided passphrase-derived master key to decrypt the client's backup data.
// If targetClient is set, files are pushed to the target client via PushRestore RPC
// instead of being written to the server's local filesystem.
func (s *Server) runRemoteRestore(repo db.Repository, closeFunc func(), encryptor *cryptoPkg.AESEncryptor, masterKey []byte, storageDir, clientID, hash, filePath, backupID, query, dest, targetClient string) {
	defer closeFunc()
	ctx := context.Background()

	// Create local data source for reading from CAS.
	source := &localDataSource{storageDir: storageDir}

	// If targeting a remote client, use the push restore flow.
	if targetClient != "" {
		s.runRemoteRestorePush(ctx, repo, encryptor, masterKey, source, clientID, targetClient, hash, filePath, backupID, query, dest)
		return
	}

	// Otherwise, restore to local server filesystem using the restore engine.
	engine := restore.NewRestoreEngine(source, repo, encryptor, masterKey)

	if hash != "" {
		// Single file restore by hash.
		destination := resolveDestination(dest, filePath)
		err := engine.RestoreFile(ctx, hash, destination)
		if err != nil {
			slog.Error("remote restore: file restore failed", "client_id", clientID, "hash", hash, "error", err)
			if s.broker != nil {
				s.broker.Publish(ActivityEvent{
					Type:    "restore_failed",
					Message: fmt.Sprintf("Remote restore failed (client %s): %s - %v", clientID, filePath, err),
				})
			}
			return
		}
		slog.Info("remote restore: file restored", "client_id", clientID, "path", destination)
		if s.broker != nil {
			s.broker.Publish(ActivityEvent{
				Type:    "restore_completed",
				Message: fmt.Sprintf("Remote restore (client %s): %s to %s", clientID, filePath, dest),
			})
		}
		return
	}

	if backupID != "" && query == "" {
		// Restore entire backup set.
		manifest, err := repo.GetManifest(ctx, backupID)
		if err != nil {
			slog.Error("remote restore: get manifest failed", "client_id", clientID, "error", err)
			if s.broker != nil {
				s.broker.Publish(ActivityEvent{
					Type:    "restore_failed",
					Message: fmt.Sprintf("Remote restore failed (client %s): could not load manifest - %v", clientID, err),
				})
			}
			return
		}

		var entries []restore.RestoreEntry
		for _, m := range manifest {
			found, err := repo.FindByHash(ctx, m.Blake3Hash)
			if err != nil || len(found) == 0 {
				continue
			}
			entry := found[0]
			destination := resolveDestination(dest, entry.FilePath)
			entries = append(entries, restore.RestoreEntry{
				Hash:        entry.Blake3Hash,
				FileName:    entry.FileName,
				Destination: destination,
				BackupID:    backupID,
				Metadata:    &entry,
			})
		}

		if len(entries) == 0 {
			slog.Warn("remote restore: no files in backup", "client_id", clientID, "backup_id", backupID)
			if s.broker != nil {
				s.broker.Publish(ActivityEvent{
					Type:    "restore_failed",
					Message: fmt.Sprintf("Remote restore (client %s): no files found in backup %s", clientID, backupID),
				})
			}
			return
		}

		result, err := engine.RestoreBatch(ctx, entries, 4)
		if err != nil {
			slog.Error("remote restore: batch restore failed", "client_id", clientID, "error", err)
			if s.broker != nil {
				s.broker.Publish(ActivityEvent{
					Type:    "restore_failed",
					Message: fmt.Sprintf("Remote restore failed (client %s): %v", clientID, err),
				})
			}
			return
		}
		slog.Info("remote restore complete", "client_id", clientID, "restored", result.Restored, "failed", result.Failed)
		if s.broker != nil {
			s.broker.Publish(ActivityEvent{
				Type:    "restore_completed",
				Message: fmt.Sprintf("Remote restore (client %s): %d files restored to %s (%d failed)", clientID, result.Restored, dest, result.Failed),
			})
		}
		return
	}

	// Search by query and restore matching files.
	if query != "" {
		searchQuery := restore.SearchQuery{}
		if strings.Contains(query, "/") || strings.Contains(query, "\\") {
			searchQuery.Path = "%" + query + "%"
		} else if strings.Contains(query, "*") || strings.Contains(query, "?") {
			searchQuery.Pattern = query
		} else {
			searchQuery.Name = query
		}

		results, err := engine.Search(ctx, searchQuery)
		if err != nil {
			slog.Error("remote restore: search failed", "client_id", clientID, "query", query, "error", err)
			if s.broker != nil {
				s.broker.Publish(ActivityEvent{
					Type:    "restore_failed",
					Message: fmt.Sprintf("Remote restore search failed (client %s): %v", clientID, err),
				})
			}
			return
		}

		if len(results) == 0 {
			slog.Warn("remote restore: no files matching query", "client_id", clientID, "query", query)
			if s.broker != nil {
				s.broker.Publish(ActivityEvent{
					Type:    "restore_failed",
					Message: fmt.Sprintf("Remote restore (client %s): no files matching '%s'", clientID, query),
				})
			}
			return
		}

		// Deduplicate by hash.
		seen := make(map[string]bool)
		var entries []restore.RestoreEntry
		for _, r := range results {
			if seen[r.Blake3Hash] {
				continue
			}
			seen[r.Blake3Hash] = true
			destination := resolveDestination(dest, r.FilePath)
			entry := r
			entries = append(entries, restore.RestoreEntry{
				Hash:        r.Blake3Hash,
				FileName:    r.FileName,
				Destination: destination,
				Metadata:    &entry,
			})
		}

		result, err := engine.RestoreBatch(ctx, entries, 4)
		if err != nil {
			slog.Error("remote restore: batch restore failed", "client_id", clientID, "error", err)
			if s.broker != nil {
				s.broker.Publish(ActivityEvent{
					Type:    "restore_failed",
					Message: fmt.Sprintf("Remote restore failed (client %s): %v", clientID, err),
				})
			}
			return
		}
		slog.Info("remote restore complete", "client_id", clientID, "query", query, "restored", result.Restored, "failed", result.Failed)
		if s.broker != nil {
			s.broker.Publish(ActivityEvent{
				Type:    "restore_completed",
				Message: fmt.Sprintf("Remote restore (client %s): %d files matching '%s' restored to %s (%d failed)", clientID, result.Restored, query, dest, result.Failed),
			})
		}
	}
}

// runRemoteRestorePush decrypts files from source client and pushes them to the target client
// via the PushRestore gRPC stream.
func (s *Server) runRemoteRestorePush(ctx context.Context, repo db.Repository, encryptor *cryptoPkg.AESEncryptor, masterKey []byte, source *localDataSource, sourceClient, targetClient, hash, filePath, backupID, query, dest string) {
	// Verify we have a client connector that supports PushRestore.
	connector, ok := s.clientConnector.(*RemoteClientConnector)
	if !ok || connector == nil {
		slog.Error("remote restore push: client connector not available or wrong type")
		if s.broker != nil {
			s.broker.Publish(ActivityEvent{
				Type:    "restore_failed",
				Message: fmt.Sprintf("Push restore to %s failed: client connector not available", targetClient),
			})
		}
		return
	}

	// Collect files to restore.
	var entries []model.BackupEntry

	if hash != "" {
		// Single file by hash.
		found, err := repo.FindByHash(ctx, hash)
		if err != nil || len(found) == 0 {
			slog.Error("remote restore push: file not found", "hash", hash)
			return
		}
		entries = append(entries, found[0])
	} else if backupID != "" && query == "" {
		// Entire backup.
		manifest, err := repo.GetManifest(ctx, backupID)
		if err != nil {
			slog.Error("remote restore push: get manifest failed", "error", err)
			if s.broker != nil {
				s.broker.Publish(ActivityEvent{
					Type:    "restore_failed",
					Message: fmt.Sprintf("Push restore to %s failed: could not load manifest - %v", targetClient, err),
				})
			}
			return
		}
		for _, m := range manifest {
			found, err := repo.FindByHash(ctx, m.Blake3Hash)
			if err != nil || len(found) == 0 {
				continue
			}
			entries = append(entries, found[0])
		}
	} else if query != "" {
		// Search by query.
		engine := restore.NewRestoreEngine(source, repo, encryptor, masterKey)
		searchQuery := restore.SearchQuery{}
		if strings.Contains(query, "/") || strings.Contains(query, "\\") {
			searchQuery.Path = "%" + query + "%"
		} else if strings.Contains(query, "*") || strings.Contains(query, "?") {
			searchQuery.Pattern = query
		} else {
			searchQuery.Name = query
		}

		results, err := engine.Search(ctx, searchQuery)
		if err != nil {
			slog.Error("remote restore push: search failed", "query", query, "error", err)
			if s.broker != nil {
				s.broker.Publish(ActivityEvent{
					Type:    "restore_failed",
					Message: fmt.Sprintf("Push restore to %s failed: search error - %v", targetClient, err),
				})
			}
			return
		}

		// Deduplicate.
		seen := make(map[string]bool)
		for _, r := range results {
			if !seen[r.Blake3Hash] {
				seen[r.Blake3Hash] = true
				entries = append(entries, r)
			}
		}
	}

	if len(entries) == 0 {
		slog.Warn("remote restore push: no files to restore")
		if s.broker != nil {
			s.broker.Publish(ActivityEvent{
				Type:    "restore_failed",
				Message: fmt.Sprintf("Push restore to %s: no files found", targetClient),
			})
		}
		return
	}

	// Decrypt each file and build the push list.
	var pushFiles []PushRestoreFile
	for _, entry := range entries {
		data, err := source.DownloadFile(ctx, entry.Blake3Hash)
		if err != nil {
			slog.Error("remote restore push: download failed", "hash", entry.Blake3Hash, "error", err)
			continue
		}

		// Decrypt if encrypted.
		fileData := data
		if len(entry.EncryptedDEK) > 0 && len(entry.Nonce) > 0 {
			decrypted, err := encryptor.Decrypt(data, entry.EncryptedDEK, entry.Nonce, masterKey)
			if err != nil {
				slog.Error("remote restore push: decrypt failed", "hash", entry.Blake3Hash, "error", err)
				continue
			}
			fileData = decrypted
		}

		// Send the original file path — the target client resolves with destBase + its own OS filepath.
		var perm uint32
		if entry.Permissions != nil {
			perm = uint32(*entry.Permissions)
		}

		pushFiles = append(pushFiles, PushRestoreFile{
			Hash:          entry.Blake3Hash,
			FileName:      entry.FileName,
			DestPath:      entry.FilePath, // original path, NOT resolved on server
			Data:          fileData,
			Permissions:   perm,
			Owner:         entry.Owner,
			Group:         entry.FileGroup,
			Symlink:       entry.Symlink,
			SymlinkTarget: entry.SymlinkTarget,
		})
	}

	if len(pushFiles) == 0 {
		slog.Warn("remote restore push: all files failed to decrypt")
		if s.broker != nil {
			s.broker.Publish(ActivityEvent{
				Type:    "restore_failed",
				Message: fmt.Sprintf("Push restore to %s: all files failed to decrypt", targetClient),
			})
		}
		return
	}

	// Push to target client.
	slog.Info("remote restore push: sending files to target",
		"source_client", sourceClient, "target_client", targetClient, "files", len(pushFiles))

	resp, err := connector.PushRestoreToClient(ctx, targetClient, pushFiles, dest)
	if err != nil {
		slog.Error("remote restore push: push failed", "target", targetClient, "error", err)
		if s.broker != nil {
			s.broker.Publish(ActivityEvent{
				Type:    "restore_failed",
				Message: fmt.Sprintf("Push restore to %s failed: %v", targetClient, err),
			})
		}
		return
	}

	slog.Info("remote restore push complete",
		"source_client", sourceClient, "target_client", targetClient,
		"files_received", resp.FilesReceived, "files_failed", resp.FilesFailed)
	if s.broker != nil {
		s.broker.Publish(ActivityEvent{
			Type:    "restore_completed",
			Message: fmt.Sprintf("Push restore from '%s' to '%s': %d files delivered (%d failed)", sourceClient, targetClient, resp.FilesReceived, resp.FilesFailed),
		})
	}
}
