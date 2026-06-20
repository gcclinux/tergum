package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ricardopadilha/tergum/internal/config"
	"github.com/ricardopadilha/tergum/internal/db"
	"github.com/ricardopadilha/tergum/internal/model"
	"github.com/spf13/cobra"
)

func newStopCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop an in-progress backup",
		Long: `Sends a stop signal to gracefully terminate the current backup operation.
The backup will complete its current file transfer, update the job status to "stopped",
and exit cleanly.`,
		RunE: runStop,
	}

	return cmd
}

func runStop(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// Write stop signal file.
	configDir := filepath.Dir(cfg.Database.Path)
	stopFile := filepath.Join(configDir, "stop")
	if err := os.WriteFile(stopFile, []byte("stop"), 0600); err != nil {
		return fmt.Errorf("cannot write stop signal: %w", err)
	}

	// Check if there's actually a running backup.
	repo, err := db.NewRepository(cfg.Database.Path, cfg.Database.WALMode)
	if err != nil {
		printOutput(
			map[string]interface{}{"signal": "sent"},
			"Stop signal sent.",
		)
		return nil
	}
	defer repo.Close()

	running := model.JobRunning
	jobs, err := repo.ListJobs(context.Background(), db.JobFilter{Status: &running, Limit: 1})
	if err != nil || len(jobs) == 0 {
		// Remove the stop file since nothing is running.
		os.Remove(stopFile)
		printOutput(
			map[string]interface{}{"status": "no_running_backup"},
			"No backup is currently running.",
		)
		return nil
	}

	printOutput(
		map[string]interface{}{
			"signal":    "sent",
			"backup_id": jobs[0].BackupID,
		},
		fmt.Sprintf("Stop signal sent to backup %s. It will stop at the next checkpoint.", jobs[0].BackupID),
	)
	return nil
}
