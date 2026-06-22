package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/gcclinux/tergum/internal/config"
	"github.com/gcclinux/tergum/internal/db"
	"github.com/gcclinux/tergum/internal/deletion"
	"github.com/gcclinux/tergum/internal/model"
	"github.com/gcclinux/tergum/internal/storage"
	"github.com/spf13/cobra"
)

func newDeleteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete [target]",
		Short: "Delete backup entries",
		Long: `Delete entire backup sets, folders within a backup, or individual files.
Supports --dry-run to preview what would be deleted.

Examples:
  tergum delete --backup-id abc123                  # delete entire backup set
  tergum delete /path/to/file --backup-id abc123    # delete file from backup
  tergum delete /path/to/folder/ --all-backups      # delete folder from all backups
  tergum delete --all-backups                       # delete ALL backups (dangerous)
  tergum delete --all-backups --dry-run             # preview full deletion
  tergum delete --all-activity                      # clear activity/restore history`,
		RunE: runDelete,
	}

	cmd.Flags().String("backup-id", "", "target a specific backup set")
	cmd.Flags().Bool("all-backups", false, "delete across all backup sets")
	cmd.Flags().Bool("all-activity", false, "clear all activity history (restore history and orphan job records)")

	return cmd
}

func runDelete(cmd *cobra.Command, args []string) error {
	backupID, _ := cmd.Flags().GetString("backup-id")
	allBackups, _ := cmd.Flags().GetBool("all-backups")
	allActivity, _ := cmd.Flags().GetBool("all-activity")

	// Handle --all-activity separately.
	if allActivity {
		return runDeleteActivity(cmd)
	}

	// Validate flags.
	if backupID == "" && !allBackups {
		return fmt.Errorf("must specify --backup-id, --all-backups, or --all-activity")
	}

	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	repo, err := db.NewRepository(cfg.Database.Path, cfg.Database.WALMode)
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer repo.Close()

	// Create CAS store for physical file deletion.
	storageDir := cfg.StorageDir()
	cas := storage.NewCAS(storageDir, repo)

	// Create deletion engine with adapter.
	adapter := &repoAdapter{repo: repo}
	engine := deletion.New(adapter, cas)

	ctx := context.Background()

	var result *deletion.DeleteResult
	target := ""
	if len(args) > 0 {
		target = args[0]
	}

	switch {
	case target == "" && backupID != "" && !allBackups:
		// Delete entire backup set.
		if !dryRun {
			fmt.Printf("Deleting backup set %s...\n", backupID)
		}
		result, err = engine.DeleteByBackupID(ctx, backupID, dryRun)

	case target == "" && allBackups:
		// Delete ALL backups.
		if !dryRun {
			fmt.Println("Deleting ALL backups...")
		}
		result, err = engine.DeleteAll(ctx, dryRun)

	case target != "" && strings.HasSuffix(target, "/"):
		// Target is a folder (ends with /).
		result, err = engine.DeleteByFolder(ctx, target, backupID, allBackups, dryRun)

	case target != "":
		// Target is a file.
		result, err = engine.DeleteByFile(ctx, target, backupID, allBackups, dryRun)

	default:
		return fmt.Errorf("invalid combination of flags; see --help")
	}

	if err != nil {
		return fmt.Errorf("delete failed: %w", err)
	}

	prefix := ""
	if dryRun {
		prefix = "[DRY RUN] "
	}

	printOutput(
		map[string]interface{}{
			"dry_run":         dryRun,
			"entries_deleted": result.EntriesDeleted,
			"bytes_freed":     result.BytesFreed,
			"files_removed":   result.FilesRemoved,
			"jobs_removed":    result.JobsRemoved,
		},
		fmt.Sprintf("%sDeleted: %d entries, %d bytes freed, %d storage files removed, %d jobs cleaned up",
			prefix, result.EntriesDeleted, result.BytesFreed, result.FilesRemoved, result.JobsRemoved),
	)

	return nil
}

func runDeleteActivity(cmd *cobra.Command) error {
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
	deleted, err := repo.DeleteAllActivity(ctx)
	if err != nil {
		return fmt.Errorf("deleting activity: %w", err)
	}

	printOutput(
		map[string]interface{}{"activity_deleted": deleted},
		fmt.Sprintf("Activity cleared: %d records deleted.", deleted),
	)
	return nil
}

// repoAdapter bridges db.Repository (uses db.DeleteFilter) to deletion.Repository (uses deletion.Filter).
type repoAdapter struct {
	repo db.Repository
}

func (a *repoAdapter) QueryEntries(ctx context.Context, filter deletion.Filter) ([]model.BackupEntry, error) {
	return a.repo.QueryEntries(ctx, db.DeleteFilter{
		BackupID:   filter.BackupID,
		FolderPath: filter.FolderPath,
		FilePath:   filter.FilePath,
		AllBackups: filter.AllBackups,
	})
}

func (a *repoAdapter) DeleteEntries(ctx context.Context, filter deletion.Filter) (int64, error) {
	return a.repo.DeleteEntries(ctx, db.DeleteFilter{
		BackupID:   filter.BackupID,
		FolderPath: filter.FolderPath,
		FilePath:   filter.FilePath,
		AllBackups: filter.AllBackups,
	})
}

func (a *repoAdapter) CountHashReferences(ctx context.Context, hash string) (int64, error) {
	return a.repo.CountHashReferences(ctx, hash)
}

func (a *repoAdapter) DeleteOrphanJobs(ctx context.Context) (int64, error) {
	return a.repo.DeleteOrphanJobs(ctx)
}
