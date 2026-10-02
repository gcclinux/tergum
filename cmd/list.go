package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gcclinux/tergum/internal/config"
	"github.com/gcclinux/tergum/internal/connection"
	"github.com/gcclinux/tergum/internal/db"
	grpcpkg "github.com/gcclinux/tergum/internal/grpc"
	"github.com/gcclinux/tergum/internal/grpc/proto"
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
	cmd.Flags().String("client", "", "target client ID for remote queries (admin only)")

	return cmd
}

func runList(cmd *cobra.Command, args []string) error {
	backupID, _ := cmd.Flags().GetString("backup-id")
	pattern, _ := cmd.Flags().GetString("pattern")
	clientID, _ := cmd.Flags().GetString("client")

	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// Admin remote list: a client node (admin client) asks the server to
	// list backup data from another client's catalog.
	if clientID != "" && cfg.Node.Role == "client" {
		return runAdminRemoteList(cfg, clientID, backupID, pattern)
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
				StartedAt:    j.StartedAt.Local().Format("2006-01-02 15:04:05"),
				FileCount:    j.FileCount,
				BytesNew:     j.BytesNew,
				FilesDeduped: j.FilesDeduped,
			}
			if j.FinishedAt != nil {
				jj.FinishedAt = j.FinishedAt.Local().Format("2006-01-02 15:04:05")
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
			started := j.StartedAt.Local().Format("2006-01-02 15:04")
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

// runAdminRemoteList handles admin client list operations when using --client flag.
// This allows an admin client to list backup data from another client's catalog
// via the server. The server enforces admin authorization via SPKI fingerprint.
//
// Modes:
//   - List backup jobs: --client X (no --backup-id or --pattern)
//   - List files in backup: --client X --backup-id abc123
//   - Search files by pattern: --client X --pattern "*.go"
func runAdminRemoteList(cfg *config.Config, clientID, backupID, pattern string) error {
	ctx := context.Background()

	tlsCfg, selfID, err := connection.LoadClientTLS(cfg)
	if err != nil {
		return fmt.Errorf("loading TLS config: %w", err)
	}

	client, err := grpcpkg.Connect(ctx, cfg.Server.Address, cfg.Server.CommandPort, cfg.Server.DataPort, tlsCfg)
	if err != nil {
		return fmt.Errorf("connecting to server: %w", err)
	}
	client.SetClientID(selfID)

	// Determine which mode to execute
	switch {
	case backupID == "" && pattern == "":
		// Mode 1: List backup jobs
		return runAdminListBackupJobs(ctx, client, clientID)
	case backupID != "" && pattern == "":
		// Mode 2: List files in a specific backup
		return runAdminListBackupFiles(ctx, client, clientID, backupID)
	default:
		// Mode 3: Search files by pattern
		return runAdminSearchFilesCmd(ctx, client, clientID, backupID, pattern)
	}
}

// runAdminListBackupJobs lists backup jobs for a client via the server.
// Calls AdminListBackups RPC and renders output in table or JSON format.
func runAdminListBackupJobs(ctx context.Context, client *grpcpkg.TergumClient, clientID string) error {
	resp, err := client.AdminListBackups(ctx, &proto.AdminListBackupsRequest{
		ClientId: clientID,
		Limit:    50,
	})
	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("list failed: %s", resp.Message)
	}

	if len(resp.Backups) == 0 {
		printOutput(
			map[string]interface{}{"client_id": clientID, "jobs": []string{}},
			fmt.Sprintf("No backup jobs found for client %s", clientID),
		)
		return nil
	}

	// Render using same format as local runList() backup jobs output
	if jsonOut {
		type jobJSON struct {
			BackupID     string `json:"backup_id"`
			Level        string `json:"level"`
			ClientID     string `json:"client_id"`
			Status       string `json:"status"`
			StartedAt    string `json:"started_at"`
			FinishedAt   string `json:"finished_at,omitempty"`
			FileCount    int64  `json:"file_count"`
			BytesNew     int64  `json:"bytes_new"`
			FilesDeduped int64  `json:"files_deduped"`
		}
		var out []jobJSON
		for _, j := range resp.Backups {
			jj := jobJSON{
				BackupID:     j.BackupId,
				Level:        j.Level,
				ClientID:     j.ClientId,
				Status:       j.Status,
				StartedAt:    j.StartedAt,
				FileCount:    j.FileCount,
				BytesNew:     j.BytesNew,
				FilesDeduped: j.FilesDeduped,
			}
			if j.FinishedAt != "" {
				jj.FinishedAt = j.FinishedAt
			}
			out = append(out, jj)
		}
		printOutput(map[string]interface{}{
			"client_id": clientID,
			"jobs":      out,
			"count":     len(out),
		}, "")
	} else {
		fmt.Printf("Backup Jobs for client %s (%d):\n\n", clientID, len(resp.Backups))
		fmt.Printf("  %-36s  %-6s  %-10s  %-9s  %8s  %12s  %s\n",
			"BACKUP ID", "LEVEL", "CLIENT", "STATUS", "FILES", "NEW BYTES", "STARTED")
		fmt.Printf("  %s\n", strings.Repeat("-", 105))
		for _, j := range resp.Backups {
			fmt.Printf("  %-36s  %-6s  %-10s  %-9s  %8d  %12d  %s\n",
				j.BackupId, j.Level, j.ClientId, j.Status,
				j.FileCount, j.BytesNew, j.StartedAt)
		}
	}

	return nil
}

// runAdminListBackupFiles lists files in a specific backup for a client via the server.
// Calls AdminSearchFiles RPC with ListOnly=true and renders output.
func runAdminListBackupFiles(ctx context.Context, client *grpcpkg.TergumClient, clientID, backupID string) error {
	resp, err := client.AdminSearchFiles(ctx, &proto.AdminSearchFilesRequest{
		SourceClientId: clientID,
		BackupId:       backupID,
		ListOnly:       true,
	})
	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("list failed: %s", resp.Message)
	}

	if len(resp.Files) == 0 {
		printOutput(
			map[string]interface{}{"client_id": clientID, "backup_id": backupID, "files": []string{}},
			fmt.Sprintf("No files found in backup %s for client %s", backupID, clientID),
		)
		return nil
	}

	// Render using same format as local list --backup-id (hash prefix, file path)
	if jsonOut {
		printOutput(map[string]interface{}{
			"client_id": clientID,
			"backup_id": backupID,
			"files":     resp.Files,
			"count":     len(resp.Files),
		}, "")
	} else {
		fmt.Printf("Files in backup %s for client %s (%d files):\n\n", backupID, clientID, len(resp.Files))
		for _, f := range resp.Files {
			hashPrefix := f.Blake3Hash
			if len(hashPrefix) > 12 {
				hashPrefix = hashPrefix[:12]
			}
			fmt.Printf("  %s  %s\n", hashPrefix, f.FilePath)
		}
	}
	return nil
}

// runAdminSearchFilesCmd searches files by pattern for a client via the server.
// Calls AdminSearchFiles RPC with the pattern and optional backupID.
// Output format matches local `runList --pattern` (hash prefix, file path, backup ID).
func runAdminSearchFilesCmd(ctx context.Context, client *grpcpkg.TergumClient, clientID, backupID, pattern string) error {
	resp, err := client.AdminSearchFiles(ctx, &proto.AdminSearchFilesRequest{
		SourceClientId: clientID,
		BackupId:       backupID,
		Query:          pattern,
		ListOnly:       true,
	})
	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("search failed: %s", resp.Message)
	}

	if len(resp.Files) == 0 {
		printOutput(
			map[string]interface{}{"client_id": clientID, "pattern": pattern, "files": []string{}},
			fmt.Sprintf("No files matching %q found for client %s", pattern, clientID),
		)
		return nil
	}

	// Render using same format as local list --pattern (hash prefix, file path, backup ID)
	if jsonOut {
		printOutput(map[string]interface{}{
			"client_id": clientID,
			"pattern":   pattern,
			"files":     resp.Files,
			"count":     len(resp.Files),
		}, "")
	} else {
		fmt.Printf("Files matching %q for client %s (%d results):\n\n", pattern, clientID, len(resp.Files))
		for _, f := range resp.Files {
			hashPrefix := f.Blake3Hash
			if len(hashPrefix) > 12 {
				hashPrefix = hashPrefix[:12]
			}
			backupIDPrefix := f.BackupId
			if len(backupIDPrefix) > 8 {
				backupIDPrefix = backupIDPrefix[:8]
			}
			fmt.Printf("  %s  %s  [backup: %s]\n", hashPrefix, f.FilePath, backupIDPrefix)
		}
	}
	return nil
}
