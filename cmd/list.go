package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gcclinux/tergum/internal/config"
	"github.com/gcclinux/tergum/internal/db"
	"github.com/gcclinux/tergum/internal/model"
	"github.com/spf13/cobra"
)

func newListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List backup sets and files",
		Long:  `List backup jobs, files within a backup, or all backed-up files matching a pattern.`,
		RunE:  runList,
	}

	cmd.Flags().String("backup-id", "", "list files within a specific backup set")
	cmd.Flags().StringP("pattern", "p", "", "filter files by glob pattern")

	return cmd
}

func runList(cmd *cobra.Command, args []string) error {
	backupID, _ := cmd.Flags().GetString("backup-id")
	pattern, _ := cmd.Flags().GetString("pattern")

	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	repo, err := db.NewRepository(cfg.Database.Path, cfg.Database.WALMode)
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer repo.Close()

	ctx := context.Background()

	// If backup-id is given, list files in that backup.
	if backupID != "" {
		entries, err := repo.GetManifest(ctx, backupID)
		if err != nil {
			return fmt.Errorf("getting manifest: %w", err)
		}

		if len(entries) == 0 {
			printOutput(
				map[string]interface{}{"backup_id": backupID, "files": []string{}},
				fmt.Sprintf("No files found in backup %s", backupID),
			)
			return nil
		}

		if jsonOut {
			printOutput(map[string]interface{}{
				"backup_id": backupID,
				"files":     entries,
				"count":     len(entries),
			}, "")
		} else {
			fmt.Printf("Files in backup %s (%d files):\n\n", backupID, len(entries))
			for _, e := range entries {
				fmt.Printf("  %s  %s\n", e.Blake3Hash[:12], e.FilePath)
			}
		}
		return nil
	}

	// If pattern is given, search files by path.
	if pattern != "" {
		entries, err := repo.FindByPath(ctx, pattern)
		if err != nil {
			return fmt.Errorf("searching files: %w", err)
		}

		if jsonOut {
			printOutput(map[string]interface{}{
				"pattern": pattern,
				"files":   entries,
				"count":   len(entries),
			}, "")
		} else {
			fmt.Printf("Files matching %q (%d results):\n\n", pattern, len(entries))
			for _, e := range entries {
				fmt.Printf("  %s  %s  [backup: %s]\n", e.Blake3Hash[:12], e.FilePath, e.BackupID[:8])
			}
		}
		return nil
	}

	// Default: list all backup jobs.
	jobs, err := repo.ListJobs(ctx, db.JobFilter{Limit: 50})
	if err != nil {
		return fmt.Errorf("listing jobs: %w", err)
	}

	if len(jobs) == 0 {
		printOutput(
			map[string]interface{}{"jobs": []string{}},
			"No backup jobs found. Run 'tergum backup' to create one.",
		)
		return nil
	}

	if jsonOut {
		type jobJSON struct {
			BackupID     string  `json:"backup_id"`
			Level        string  `json:"level"`
			ClientID     string  `json:"client_id"`
			Status       string  `json:"status"`
			StartedAt    string  `json:"started_at"`
			FinishedAt   string  `json:"finished_at,omitempty"`
			FileCount    int64   `json:"file_count"`
			BytesNew     int64   `json:"bytes_new"`
			FilesDeduped int64   `json:"files_deduped"`
			Speed        string  `json:"speed,omitempty"`
			BytesPerSec  float64 `json:"bytes_per_sec,omitempty"`
		}
		var out []jobJSON
		for _, j := range jobs {
			jj := jobJSON{
				BackupID:     j.BackupID,
				Level:        j.Level,
				ClientID:     j.ClientID,
				Status:       string(j.Status),
				StartedAt:    j.StartedAt.Format("2006-01-02 15:04:05"),
				FileCount:    j.FileCount,
				BytesNew:     j.BytesNew,
				FilesDeduped: j.FilesDeduped,
			}
			if j.FinishedAt != nil {
				jj.FinishedAt = j.FinishedAt.Format("2006-01-02 15:04:05")
			}
			if j.Status == model.JobCompleted && j.FinishedAt != nil && j.BytesNew > 0 {
				elapsed := j.FinishedAt.Sub(j.StartedAt).Seconds()
				if elapsed > 0 {
					jj.BytesPerSec = float64(j.BytesNew) / elapsed
					jj.Speed = formatSpeed(jj.BytesPerSec)
				}
			} else if j.Status == model.JobRunning && j.BytesNew > 0 {
				elapsed := time.Since(j.StartedAt).Seconds()
				if elapsed > 0 {
					jj.BytesPerSec = float64(j.BytesNew) / elapsed
					jj.Speed = formatSpeed(jj.BytesPerSec)
				}
			}
			out = append(out, jj)
		}
		printOutput(map[string]interface{}{"jobs": out, "count": len(out)}, "")
	} else {
		fmt.Printf("Backup Jobs (%d):\n\n", len(jobs))
		fmt.Printf("  %-36s  %-6s  %-10s  %-9s  %8s  %12s  %10s  %s\n",
			"BACKUP ID", "LEVEL", "CLIENT", "STATUS", "FILES", "NEW BYTES", "SPEED", "STARTED")
		fmt.Printf("  %s\n", strings.Repeat("-", 125))
		for _, j := range jobs {
			started := j.StartedAt.Format("2006-01-02 15:04")
			speed := ""
			if j.Status == model.JobRunning && j.BytesNew > 0 {
				elapsed := time.Since(j.StartedAt).Seconds()
				if elapsed > 0 {
					bps := float64(j.BytesNew) / elapsed
					speed = formatSpeed(bps)
				}
			} else if j.Status == model.JobCompleted && j.FinishedAt != nil && j.BytesNew > 0 {
				elapsed := j.FinishedAt.Sub(j.StartedAt).Seconds()
				if elapsed > 0 {
					bps := float64(j.BytesNew) / elapsed
					speed = formatSpeed(bps)
				}
			}
			fmt.Printf("  %-36s  %-6s  %-10s  %-9s  %8d  %12d  %10s  %s\n",
				j.BackupID, j.Level, j.ClientID, string(j.Status),
				j.FileCount, j.BytesNew, speed, started)
		}
	}

	return nil
}

// formatSpeed converts bytes/sec to a human-readable string.
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
