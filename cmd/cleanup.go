package cmd

import (
	"context"
	"fmt"

	"github.com/gcclinux/tergum/internal/config"
	"github.com/gcclinux/tergum/internal/db"
	"github.com/spf13/cobra"
)

func newCleanupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cleanup",
		Short: "Clean up stale running backup jobs",
		Long:  `Scans the database and marks any jobs stuck in "running" status as "failed".`,
		RunE:  runCleanup,
	}

	return cmd
}

func runCleanup(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	repo, err := db.NewRepository(cfg.Database.Path, cfg.Database.WALMode)
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer repo.Close()

	affected, err := repo.FailStaleJobs(context.Background(), "Interrupted by manual cleanup")
	if err != nil {
		return fmt.Errorf("failing stale jobs: %w", err)
	}

	printOutput(
		map[string]interface{}{
			"status":       "success",
			"cleaned_jobs": affected,
		},
		fmt.Sprintf("Successfully cleaned up %d stale running backup jobs.", affected),
	)
	return nil
}
