package cmd

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gcclinux/tergum/internal/config"
	"github.com/gcclinux/tergum/internal/connection"
	"github.com/gcclinux/tergum/internal/db"
	"github.com/gcclinux/tergum/internal/model"
	"github.com/spf13/cobra"

	_ "modernc.org/sqlite"
)

func newStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show current system status",
		Long:  `Displays the current status of backup operations, storage usage, and configuration summary.`,
		RunE:  runStatus,
	}

	return cmd
}

func runStatus(cmd *cobra.Command, args []string) error {
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

	// Get all jobs.
	allJobs, err := repo.ListJobs(ctx, db.JobFilter{})
	if err != nil {
		return fmt.Errorf("listing jobs: %w", err)
	}

	// Count by status.
	var running, completed, failed, stopped int
	var lastBackup *model.BackupJob
	var totalFiles int64
	var totalBytes int64
	for i := range allJobs {
		switch allJobs[i].Status {
		case model.JobRunning:
			running++
		case model.JobCompleted:
			completed++
			if lastBackup == nil || allJobs[i].StartedAt.After(lastBackup.StartedAt) {
				lastBackup = &allJobs[i]
			}
		case model.JobFailed:
			failed++
		case model.JobStopped:
			stopped++
		}
		totalFiles += allJobs[i].FileCount
		totalBytes += allJobs[i].BytesNew
	}

	// Get include paths and exclude patterns.
	includes, _ := repo.ListIncludePaths(ctx)
	excludes, _ := repo.ListExcludePatterns(ctx)

	// Get storage size.
	storageDir := cfg.StorageDir()
	storageSize := dirSize(storageDir)

	// Get DB size.
	dbInfo, _ := os.Stat(cfg.Database.Path)
	var dbSize int64
	if dbInfo != nil {
		dbSize = dbInfo.Size()
	}

	// Get CA fingerprint (if TLS is configured).
	caFingerprint := getCACertFingerprint(cfg.TLS.CACert)

	// Get registered client count (server/hybrid only).
	var clientsTotal, clientsOnline int
	if cfg.Node.Role == "server" || cfg.Node.Role == "hybrid" {
		clientsTotal, clientsOnline = countRegisteredClients(cfg.Database.Path)
	}

	// Build status output.
	status := map[string]interface{}{
		"role":             cfg.Node.Role,
		"config_path":      config.DefaultConfigPath(),
		"database_path":    cfg.Database.Path,
		"storage_path":     storageDir,
		"storage_size":     storageSize,
		"database_size":    dbSize,
		"include_paths":    len(includes),
		"exclude_patterns": len(excludes),
		"backup_jobs":      len(allJobs),
		"running":          running,
		"completed":        completed,
		"failed":           failed,
		"stopped":          stopped,
		"total_files":      totalFiles,
		"total_bytes":      totalBytes,
		"encryption":       cfg.Encryption.Enabled,
		"watcher":          cfg.Watcher.Enabled,
		"webui":            cfg.WebUI.Enabled,
		"webui_port":       cfg.WebUI.Port,
		"command_port":     cfg.Server.CommandPort,
		"data_port":        cfg.Server.DataPort,
		"bootstrap_port":   cfg.Server.BootstrapPort,
		"metrics_port":     cfg.Metrics.Port,
	}

	if caFingerprint != "" {
		status["ca_fingerprint"] = caFingerprint
	}
	if cfg.Node.Role == "server" || cfg.Node.Role == "hybrid" {
		status["clients_registered"] = clientsTotal
		status["clients_online"] = clientsOnline
	}
	if cfg.Node.NATMode {
		status["nat_mode"] = true
	}

	// For client nodes, check whether the server has disabled this client.
	var clientDisabled *bool
	if cfg.Node.Role == "client" {
		if err := connection.CheckClientEnabled(cfg); err != nil {
			if strings.Contains(err.Error(), "disabled") {
				disabled := true
				clientDisabled = &disabled
				status["server_disabled"] = true
			}
		} else {
			disabled := false
			clientDisabled = &disabled
			status["server_disabled"] = false
		}
	}

	if lastBackup != nil {
		status["last_backup_id"] = lastBackup.BackupID
		status["last_backup_at"] = lastBackup.StartedAt.Format("2006-01-02 15:04:05")
		status["last_backup_files"] = lastBackup.FileCount
	}

	if jsonOut {
		printOutput(status, "")
	} else {
		fmt.Println("Tergum Status")
		fmt.Println("=============")
		fmt.Println()

		fmt.Println("Configuration:")
		fmt.Printf("  Role:             %s\n", cfg.Node.Role)
		fmt.Printf("  Config:           %s\n", config.DefaultConfigPath())
		fmt.Printf("  Database:         %s (%s)\n", cfg.Database.Path, formatBytes(dbSize))
		fmt.Printf("  Storage:          %s (%s)\n", storageDir, formatBytes(storageSize))
		fmt.Printf("  Encryption:       %v\n", cfg.Encryption.Enabled)
		fmt.Printf("  Watcher:          %v\n", cfg.Watcher.Enabled)
		if cfg.Node.NATMode {
			fmt.Printf("  NAT Mode:         true\n")
		}
		if clientDisabled != nil {
			if *clientDisabled {
				fmt.Printf("  Server Disabled:  true (this client is disabled on the server)\n")
			} else {
				fmt.Printf("  Server Disabled:  false\n")
			}
		}
		fmt.Printf("  Web UI:           %v (port %d)\n", cfg.WebUI.Enabled, cfg.WebUI.Port)
		fmt.Println()

		fmt.Println("Network:")
		fmt.Printf("  Command port:     %d\n", cfg.Server.CommandPort)
		fmt.Printf("  Data port:        %d\n", cfg.Server.DataPort)
		fmt.Printf("  Bootstrap port:   %d\n", cfg.Server.BootstrapPort)
		fmt.Printf("  Metrics port:     %d\n", cfg.Metrics.Port)
		if cfg.Node.Role == "client" && cfg.Server.Address != "" {
			fmt.Printf("  Server address:   %s\n", cfg.Server.Address)
		}
		fmt.Println()

		if caFingerprint != "" {
			fmt.Println("TLS:")
			fmt.Printf("  CA fingerprint:   %s\n", caFingerprint)
			fmt.Println()
		}

		if cfg.Node.Role == "server" || cfg.Node.Role == "hybrid" {
			fmt.Println("Clients:")
			fmt.Printf("  Registered:       %d\n", clientsTotal)
			fmt.Printf("  Online:           %d\n", clientsOnline)
			fmt.Println()
		}

		fmt.Println("Paths:")
		fmt.Printf("  Include paths:    %d\n", len(includes))
		fmt.Printf("  Exclude patterns: %d\n", len(excludes))
		fmt.Println()

		fmt.Println("Backups:")
		fmt.Printf("  Total jobs:       %d\n", len(allJobs))
		if running > 0 {
			fmt.Printf("  Running:          %d\n", running)
		}
		fmt.Printf("  Completed:        %d\n", completed)
		if failed > 0 {
			fmt.Printf("  Failed:           %d\n", failed)
		}
		if stopped > 0 {
			fmt.Printf("  Stopped:          %d\n", stopped)
		}
		fmt.Printf("  Total files:      %d\n", totalFiles)
		fmt.Printf("  Total new bytes:  %s\n", formatBytes(totalBytes))
		fmt.Println()

		if lastBackup != nil {
			elapsed := ""
			if lastBackup.FinishedAt != nil {
				elapsed = lastBackup.FinishedAt.Sub(lastBackup.StartedAt).Round(time.Second).String()
			}
			fmt.Println("Last Backup:")
			fmt.Printf("  ID:               %s\n", lastBackup.BackupID)
			fmt.Printf("  Date:             %s\n", lastBackup.StartedAt.Format("2006-01-02 15:04:05"))
			fmt.Printf("  Files:            %d\n", lastBackup.FileCount)
			fmt.Printf("  New bytes:        %s\n", formatBytes(lastBackup.BytesNew))
			if elapsed != "" {
				fmt.Printf("  Duration:         %s\n", elapsed)
			}
		} else {
			fmt.Println("Last Backup:        (none)")
		}
	}

	return nil
}

// getCACertFingerprint reads the CA certificate and returns its SHA-256 fingerprint
// in colon-separated hex format. Returns empty string if the cert cannot be read.
func getCACertFingerprint(caPath string) string {
	if caPath == "" {
		return ""
	}
	caBytes, err := os.ReadFile(caPath)
	if err != nil {
		return ""
	}
	block, _ := pem.Decode(caBytes)
	if block == nil {
		return ""
	}
	fingerprint := sha256.Sum256(block.Bytes)
	fingerprintHex := hex.EncodeToString(fingerprint[:])
	var pairs []string
	for i := 0; i < len(fingerprintHex)-1; i += 2 {
		pairs = append(pairs, fingerprintHex[i:i+2])
	}
	return strings.Join(pairs, ":")
}

// countRegisteredClients queries the client_registry table directly to count
// total and online clients without starting the full registry subsystem.
func countRegisteredClients(dbPath string) (total, online int) {
	sqlDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return 0, 0
	}
	defer sqlDB.Close()

	// Total registered.
	row := sqlDB.QueryRow("SELECT COUNT(*) FROM client_registry")
	if row.Scan(&total) != nil {
		total = 0
	}

	// Currently online.
	row = sqlDB.QueryRow("SELECT COUNT(*) FROM client_registry WHERE status = 'online'")
	if row.Scan(&online) != nil {
		online = 0
	}

	return total, online
}

// dirSize calculates the total size of all files in a directory tree.
func dirSize(path string) int64 {
	var size int64
	filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		size += info.Size()
		return nil
	})
	return size
}

// formatBytes converts bytes to a human-readable string.
func formatBytes(b int64) string {
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
