package webui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/gcclinux/tergum/internal/db"
	"github.com/gcclinux/tergum/internal/model"
)

// handleAPIDashboardFiles handles GET /api/dashboard/files — returns total files Data_Card content.
func (s *Server) handleAPIDashboardFiles(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")

	var totalFiles int64
	if s.repo != nil {
		jobs, err := s.repo.ListJobs(r.Context(), db.JobFilter{})
		if err == nil {
			for _, j := range jobs {
				totalFiles += j.FileCount
			}
		}
	}

	fmt.Fprint(w, `<div class="flex items-center gap-3">`)
	fmt.Fprint(w, `<div class="flex-shrink-0 w-10 h-10 flex items-center justify-center rounded-lg bg-emerald-100 dark:bg-emerald-900/30 text-emerald-600 dark:text-emerald-400">`)
	fmt.Fprint(w, `<svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">`)
	fmt.Fprint(w, `<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 19a2 2 0 01-2-2V7a2 2 0 012-2h4l2 2h4a2 2 0 012 2v1M5 19h14a2 2 0 002-2v-5a2 2 0 00-2-2H9a2 2 0 00-2 2v5a2 2 0 01-2 2z"/>`)
	fmt.Fprint(w, `</svg></div>`)
	fmt.Fprint(w, `<div class="flex-1 min-w-0">`)
	fmt.Fprintf(w, `<p class="text-xl font-bold text-gray-900 dark:text-gray-100 truncate">%d</p>`, totalFiles)
	fmt.Fprint(w, `<p class="text-xs text-gray-500 dark:text-gray-400 uppercase tracking-wide">Total Files</p>`)
	fmt.Fprint(w, `</div></div>`)
}

// handleAPIDashboardStorage handles GET /api/dashboard/storage — returns storage used Data_Card content.
func (s *Server) handleAPIDashboardStorage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")

	var totalSize string
	if s.repo != nil {
		jobs, err := s.repo.ListJobs(r.Context(), db.JobFilter{})
		if err == nil {
			var totalBytes int64
			for _, j := range jobs {
				totalBytes += j.BytesNew
			}
			totalSize = formatSize(totalBytes)
		} else {
			totalSize = "0 B"
		}
	} else {
		totalSize = "0 B"
	}

	fmt.Fprint(w, `<div class="flex items-center gap-3">`)
	fmt.Fprint(w, `<div class="flex-shrink-0 w-10 h-10 flex items-center justify-center rounded-lg bg-purple-100 dark:bg-purple-900/30 text-purple-600 dark:text-purple-400">`)
	fmt.Fprint(w, `<svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">`)
	fmt.Fprint(w, `<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 7v10c0 2.21 3.582 4 8 4s8-1.79 8-4V7M4 7c0 2.21 3.582 4 8 4s8-1.79 8-4M4 7c0-2.21 3.582-4 8-4s8 1.79 8 4m0 5c0 2.21-3.582 4-8 4s-8-1.79-8-4"/>`)
	fmt.Fprint(w, `</svg></div>`)
	fmt.Fprint(w, `<div class="flex-1 min-w-0">`)
	fmt.Fprintf(w, `<p class="text-xl font-bold text-gray-900 dark:text-gray-100 truncate">%s</p>`, totalSize)
	fmt.Fprint(w, `<p class="text-xs text-gray-500 dark:text-gray-400 uppercase tracking-wide">Storage Used</p>`)
	fmt.Fprint(w, `</div></div>`)
}

// handleAPIDashboardDedup handles GET /api/dashboard/dedup — returns deduplication ratio Data_Card content.
func (s *Server) handleAPIDashboardDedup(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")

	var ratioStr string = "0.0%"
	var ratioPercent float64 = 0.0

	if s.repo != nil {
		jobs, err := s.repo.ListJobs(r.Context(), db.JobFilter{})
		if err == nil {
			var totalFiles, totalDeduped int64
			for _, j := range jobs {
				totalFiles += j.FileCount
				totalDeduped += j.FilesDeduped
			}
			if totalFiles > 0 {
				ratioPercent = float64(totalDeduped) / float64(totalFiles) * 100
				ratioStr = fmt.Sprintf("%.1f%%", ratioPercent)
			}
		}
	}

	fmt.Fprint(w, `<div class="flex items-center gap-3">`)
	fmt.Fprint(w, `<div class="flex-shrink-0 w-10 h-10 flex items-center justify-center rounded-lg bg-indigo-100 dark:bg-indigo-900/30 text-indigo-600 dark:text-indigo-400">`)
	fmt.Fprint(w, `<svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">`)
	fmt.Fprint(w, `<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 19v-6a2 2 0 00-2-2H5a2 2 0 00-2 2v6a2 2 0 002 2h2a2 2 0 002-2zm0 0V9a2 2 0 012-2h2a2 2 0 012 2v10m-6 0a2 2 0 002 2h2a2 2 0 002-2m0 0V5a2 2 0 012-2h2a2 2 0 012 2v14a2 2 0 01-2 2h-2a2 2 0 01-2-2z"/>`)
	fmt.Fprint(w, `</svg></div>`)
	fmt.Fprint(w, `<div class="flex-1 min-w-0">`)
	fmt.Fprintf(w, `<p class="text-xl font-bold text-gray-900 dark:text-gray-100 truncate">%s</p>`, ratioStr)
	fmt.Fprint(w, `<p class="text-xs text-gray-500 dark:text-gray-400 uppercase tracking-wide">Dedup Ratio</p>`)
	fmt.Fprint(w, `</div></div>`)
	fmt.Fprint(w, `<div class="mt-3 h-2 bg-gray-200 dark:bg-gray-700 rounded-full overflow-hidden">`)
	fmt.Fprintf(w, `<div class="h-full bg-indigo-500 rounded-full transition-all duration-300" style="width: %.1f%%"></div>`, ratioPercent)
	fmt.Fprint(w, `</div>`)
}

// handleAPIDashboardClients handles GET /api/dashboard/clients — returns active clients Data_Card content.
func (s *Server) handleAPIDashboardClients(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")

	// Set of unique online clients
	onlineClients := make(map[string]bool)

	if s.clientRegistry != nil {
		clients := s.clientRegistry.ListClients()
		for _, c := range clients {
			if c.Status == "online" || c.Status == "backing_up" {
				onlineClients[c.ClientID] = true
			}
		}
	}

	if s.repo != nil {
		runningStatus := model.JobRunning
		runningJobs, err := s.repo.ListJobs(r.Context(), db.JobFilter{Status: &runningStatus})
		if err == nil {
			for _, j := range runningJobs {
				if j.ClientID != "" {
					onlineClients[j.ClientID] = true
				} else {
					onlineClients["local"] = true
				}
			}
		}
	}

	activeClients := len(onlineClients)

	fmt.Fprint(w, `<div class="flex items-center gap-3">`)
	fmt.Fprint(w, `<div class="flex-shrink-0 w-10 h-10 flex items-center justify-center rounded-lg bg-indigo-100 dark:bg-indigo-900/30 text-indigo-600 dark:text-indigo-400">`)
	fmt.Fprint(w, `<svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">`)
	fmt.Fprint(w, `<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M17 20h5v-2a3 3 0 00-5.356-1.857M17 20H7m10 0v-2c0-.656-.126-1.283-.356-1.857M7 20H2v-2a3 3 0 015.356-1.857M7 20v-2c0-.656.126-1.283.356-1.857m0 0a5.002 5.002 0 019.288 0M15 7a3 3 0 11-6 0 3 3 0 016 0zm6 3a2 2 0 11-4 0 2 2 0 014 0zM7 10a2 2 0 11-4 0 2 2 0 014 0z"/>`)
	fmt.Fprint(w, `</svg></div>`)
	fmt.Fprint(w, `<div class="flex-1 min-w-0">`)
	fmt.Fprintf(w, `<p class="text-xl font-bold text-gray-900 dark:text-gray-100 truncate">%d</p>`, activeClients)
	fmt.Fprint(w, `<p class="text-xs text-gray-500 dark:text-gray-400 uppercase tracking-wide">Active Clients</p>`)
	fmt.Fprint(w, `</div></div>`)
}

// handleAPISystemNetwork handles GET /api/system/network — returns active network speed (upload/download rate of running backups) as HTML.
func (s *Server) handleAPISystemNetwork(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")

	var speedBps float64
	var active bool

	if s.repo != nil {
		runningStatus := model.JobRunning
		jobs, err := s.repo.ListJobs(r.Context(), db.JobFilter{Status: &runningStatus})
		if err == nil {
			for _, j := range jobs {
				elapsed := time.Since(j.StartedAt).Seconds()
				if elapsed > 0.5 && j.BytesNew > 0 {
					speedBps += float64(j.BytesNew) / elapsed
					active = true
				}
			}
		}
	}

	speedStr := "0 B/s"
	if active {
		speedStr = formatSpeed(speedBps)
	}

	// Choose color: green/emerald if active, gray if idle
	iconColor := "text-gray-600 dark:text-gray-400 bg-gray-100 dark:bg-gray-700"
	if active {
		iconColor = "text-teal-600 dark:text-teal-400 bg-teal-100 dark:bg-teal-900/30"
	}

	fmt.Fprint(w, `<div class="flex items-center gap-3">`)
	fmt.Fprintf(w, `<div class="flex-shrink-0 w-10 h-10 flex items-center justify-center rounded-lg %s">`, iconColor)
	// Thunderbolt / speed icon
	fmt.Fprint(w, `<svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">`)
	fmt.Fprint(w, `<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M13 10V3L4 14h7v7l9-11h-7z"/>`)
	fmt.Fprint(w, `</svg></div>`)
	fmt.Fprint(w, `<div class="flex-1 min-w-0">`)
	fmt.Fprintf(w, `<p class="text-xl font-bold text-gray-900 dark:text-gray-100 truncate">%s</p>`, speedStr)
	fmt.Fprint(w, `<p class="text-xs text-gray-500 dark:text-gray-400 uppercase tracking-wide">Network Speed</p>`)
	fmt.Fprint(w, `</div></div>`)
}

// formatSpeed converts bytes/sec to a human-readable speed.
func formatSpeed(bps float64) string {
	switch {
	case bps >= 1024*1024*1024:
		return fmt.Sprintf("%.1f GB/s", bps/(1024*1024*1024))
	case bps >= 1024*1024:
		return fmt.Sprintf("%.1f MB/s", bps/(1024*1024))
	case bps >= 1024:
		return fmt.Sprintf("%.1f KB/s", bps/1024)
	default:
		return fmt.Sprintf("%.0f B/s", bps)
	}
}

// handleAPIDashboardActivity handles GET /api/dashboard/activity — returns recent activity feed as HTML.
func (s *Server) handleAPIDashboardActivity(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")

	if s.repo == nil {
		fmt.Fprint(w, `<p class="text-sm text-gray-400 dark:text-gray-500 italic">No recent activity.</p>`)
		return
	}

	type activityItem struct {
		Timestamp   time.Time
		Icon        string
		HtmlContent string
		Status      string
		StatusColor string
	}

	var items []activityItem

	// 1. Local backup jobs
	jobs, err := s.repo.ListJobs(r.Context(), db.JobFilter{Limit: 10})
	if err == nil {
		for _, j := range jobs {
			icon := getStatusIcon(string(j.Status))
			statusColor := getStatusColor(string(j.Status))
			started := j.StartedAt.Local().Format("Jan 02, 15:04")
			backupIDTrunc := j.BackupID
			if len(backupIDTrunc) > 12 {
				backupIDTrunc = backupIDTrunc[:12]
			}
			htmlContent := fmt.Sprintf(`<p class="text-sm text-gray-700 dark:text-gray-300 truncate">Backup <span class="font-medium">%s</span> — %s</p><p class="text-xs text-gray-400 dark:text-gray-500">%s</p>`, j.Level, backupIDTrunc, started)

			items = append(items, activityItem{
				Timestamp:   j.StartedAt,
				Icon:        icon,
				HtmlContent: htmlContent,
				Status:      string(j.Status),
				StatusColor: statusColor,
			})
		}
	}

	// 2. Local restores
	restores, _ := s.repo.ListRestoreHistory(r.Context(), 10)
	for _, rec := range restores {
		icon := "📥"
		status := "success"
		statusColor := "bg-green-100 dark:bg-green-900/30 text-green-700 dark:text-green-400"
		if !rec.Success {
			icon = "❌"
			status = "failed"
			statusColor = "bg-red-100 dark:bg-red-900/30 text-red-700 dark:text-red-400"
		}
		restoredTime := rec.RestoredAt.Format("Jan 02, 15:04")
		htmlContent := fmt.Sprintf(`<p class="text-sm text-gray-700 dark:text-gray-300 truncate">Restore — <span class="font-medium">%s</span></p><p class="text-xs text-gray-400 dark:text-gray-500">%s</p>`, rec.FileName, restoredTime)

		items = append(items, activityItem{
			Timestamp:   rec.RestoredAt,
			Icon:        icon,
			HtmlContent: htmlContent,
			Status:      status,
			StatusColor: statusColor,
		})
	}

	// 3. Remote client backup jobs and restores
	if s.clientRegistry != nil {
		clients := s.clientRegistry.ListClients()
		for _, client := range clients {
			clientRepo, cleanup, err := s.getRepoForClient(r.Context(), client.ClientID)
			if err != nil {
				continue
			}

			cJobs, err := clientRepo.ListJobs(r.Context(), db.JobFilter{Limit: 10})
			if err == nil {
				for _, j := range cJobs {
					icon := getStatusIcon(string(j.Status))
					statusColor := getStatusColor(string(j.Status))
					started := j.StartedAt.Local().Format("Jan 02, 15:04")
					backupIDTrunc := j.BackupID
					if len(backupIDTrunc) > 12 {
						backupIDTrunc = backupIDTrunc[:12]
					}
					htmlContent := fmt.Sprintf(`<p class="text-sm text-gray-700 dark:text-gray-300 truncate">Remote Backup <span class="font-medium">%s</span> - <span class="font-medium">%s</span></p><p class="text-xs text-gray-400 dark:text-gray-500">%s</p>`, client.ClientID, backupIDTrunc, started)

					items = append(items, activityItem{
						Timestamp:   j.StartedAt,
						Icon:        icon,
						HtmlContent: htmlContent,
						Status:      string(j.Status),
						StatusColor: statusColor,
					})
				}
			}

			cRestores, _ := clientRepo.ListRestoreHistory(r.Context(), 10)
			for _, rec := range cRestores {
				icon := "📥"
				status := "success"
				statusColor := "bg-green-100 dark:bg-green-900/30 text-green-700 dark:text-green-400"
				if !rec.Success {
					icon = "❌"
					status = "failed"
					statusColor = "bg-red-100 dark:bg-red-900/30 text-red-700 dark:text-red-400"
				}
				restoredTime := rec.RestoredAt.Format("Jan 02, 15:04")
				htmlContent := fmt.Sprintf(`<p class="text-sm text-gray-700 dark:text-gray-300 truncate">Remote Restore <span class="font-medium">%s</span> - %s</p><p class="text-xs text-gray-400 dark:text-gray-500">%s</p>`, client.ClientID, rec.FileName, restoredTime)

				items = append(items, activityItem{
					Timestamp:   rec.RestoredAt,
					Icon:        icon,
					HtmlContent: htmlContent,
					Status:      status,
					StatusColor: statusColor,
				})
			}
			cleanup()
		}
	}

	if len(items) == 0 {
		fmt.Fprint(w, `<p class="text-sm text-gray-400 dark:text-gray-500 italic">No recent activity.</p>`)
		return
	}

	// Sort newest first
	sort.Slice(items, func(i, j int) bool {
		return items[i].Timestamp.After(items[j].Timestamp)
	})

	// Limit to 10 max recent items
	if len(items) > 10 {
		items = items[:10]
	}

	type clientActivityItem struct {
		Icon   string `json:"icon"`
		Html   string `json:"html"`
		Status string `json:"status"`
		Color  string `json:"color"`
	}

	var jsonItems []clientActivityItem
	for _, item := range items {
		jsonItems = append(jsonItems, clientActivityItem{
			Icon:   item.Icon,
			Html:   item.HtmlContent,
			Status: item.Status,
			Color:  item.StatusColor,
		})
	}

	jsonData, err := json.Marshal(jsonItems)
	if err != nil {
		fmt.Fprint(w, `<p class="text-sm text-red-500">Failed to render activity.</p>`)
		return
	}

	fmt.Fprintf(w, `<div x-data='{
		page: 1,
		perPage: 5,
		get totalPages() { return Math.ceil(this.rows.length / this.perPage); },
		get pagedRows() {
			let start = (this.page - 1) * this.perPage;
			return this.rows.slice(start, start + this.perPage);
		},
		rows: %s
	}' x-init="page = 1">`, string(jsonData))

	fmt.Fprint(w, `<div class="space-y-3">`)
	fmt.Fprint(w, `<template x-for="row in pagedRows">`)
	fmt.Fprint(w, `<div class="flex items-center gap-3 py-2 border-b border-gray-100 dark:border-gray-700 last:border-0">`)
	fmt.Fprint(w, `<span class="text-base" x-text="row.icon"></span>`)
	fmt.Fprint(w, `<div class="flex-1 min-w-0" x-html="row.html"></div>`)
	fmt.Fprint(w, `<span class="text-xs font-medium px-2 py-0.5 rounded-full" :class="row.color" x-text="row.status"></span>`)
	fmt.Fprint(w, `</div>`)
	fmt.Fprint(w, `</template>`)
	fmt.Fprint(w, `</div>`)

	// Pagination controls
	fmt.Fprint(w, `<div class="flex items-center justify-between mt-4 pt-4 border-t border-gray-200 dark:border-gray-700" x-show="totalPages > 1">`)
	fmt.Fprint(w, `<p class="text-xs text-gray-500 dark:text-gray-400">Page <span x-text="page"></span> of <span x-text="totalPages"></span></p>`)
	fmt.Fprint(w, `<div class="flex gap-2">`)
	fmt.Fprint(w, `<button @click="page = Math.max(1, page - 1)" :disabled="page <= 1" class="px-2 py-0.5 text-xs rounded border border-gray-300 dark:border-gray-600 text-gray-700 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-700 disabled:opacity-50 disabled:cursor-not-allowed">Previous</button>`)
	fmt.Fprint(w, `<button @click="page = Math.min(totalPages, page + 1)" :disabled="page >= totalPages" class="px-2 py-0.5 text-xs rounded border border-gray-300 dark:border-gray-600 text-gray-700 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-700 disabled:opacity-50 disabled:cursor-not-allowed">Next</button>`)
	fmt.Fprint(w, `</div></div>`)

	fmt.Fprint(w, `</div>`)
}

// getStatusIcon returns an emoji icon for a job status.
func getStatusIcon(status string) string {
	switch status {
	case "completed":
		return "✅"
	case "running":
		return "🔄"
	case "failed":
		return "❌"
	case "stopped":
		return "⏹️"
	default:
		return "🔵"
	}
}

// getStatusColor returns Tailwind CSS classes for a status badge.
func getStatusColor(status string) string {
	switch status {
	case "completed":
		return "bg-green-100 dark:bg-green-900/30 text-green-700 dark:text-green-400"
	case "running":
		return "bg-blue-100 dark:bg-blue-900/30 text-blue-700 dark:text-blue-400"
	case "failed":
		return "bg-red-100 dark:bg-red-900/30 text-red-700 dark:text-red-400"
	case "stopped":
		return "bg-gray-100 dark:bg-gray-700 text-gray-700 dark:text-gray-400"
	default:
		return "bg-gray-100 dark:bg-gray-700 text-gray-600 dark:text-gray-400"
	}
}
