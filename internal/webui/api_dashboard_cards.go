package webui

import (
	"fmt"
	"net/http"
	"time"

	"github.com/ricardopadilha/tergum/internal/db"
	"github.com/ricardopadilha/tergum/internal/model"
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

// handleAPIDashboardClients handles GET /api/dashboard/clients — returns active clients Data_Card content.
func (s *Server) handleAPIDashboardClients(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")

	// Set of unique online clients
	onlineClients := make(map[string]bool)

	if s.clientRegistry != nil {
		clients := s.clientRegistry.ListClients()
		for _, c := range clients {
			if c.Status == "online" {
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

	jobs, err := s.repo.ListJobs(r.Context(), db.JobFilter{Limit: 10})
	if err != nil {
		fmt.Fprint(w, `<p class="text-sm text-red-500">Failed to load activity.</p>`)
		return
	}

	if len(jobs) == 0 {
		fmt.Fprint(w, `<p class="text-sm text-gray-400 dark:text-gray-500 italic">No recent activity.</p>`)
		return
	}

	fmt.Fprint(w, `<div class="space-y-3">`)
	for _, j := range jobs {
		icon := getStatusIcon(string(j.Status))
		statusColor := getStatusColor(string(j.Status))
		started := j.StartedAt.Format("Jan 02, 15:04")
		fmt.Fprintf(w, `<div class="flex items-center gap-3 py-2 border-b border-gray-100 dark:border-gray-700 last:border-0">`)
		fmt.Fprintf(w, `<span class="text-base">%s</span>`, icon)
		fmt.Fprintf(w, `<div class="flex-1 min-w-0">`)
		fmt.Fprintf(w, `<p class="text-sm text-gray-700 dark:text-gray-300 truncate">Backup <span class="font-medium">%s</span> — %s</p>`, j.Level, j.BackupID[:min(12, len(j.BackupID))])
		fmt.Fprintf(w, `<p class="text-xs text-gray-400 dark:text-gray-500">%s</p>`, started)
		fmt.Fprintf(w, `</div>`)
		fmt.Fprintf(w, `<span class="text-xs font-medium px-2 py-0.5 rounded-full %s">%s</span>`, statusColor, j.Status)
		fmt.Fprintf(w, `</div>`)
	}
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
