package cmd

import "github.com/spf13/cobra"

func newDeleteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete [target]",
		Short: "Delete backup entries",
		Long: `Delete entire backup sets, folders within a backup, or individual files.
Supports --dry-run to preview what would be deleted.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			backupID, _ := cmd.Flags().GetString("backup-id")
			allBackups, _ := cmd.Flags().GetBool("all-backups")
			printOutput(
				map[string]interface{}{
					"status":      "not_implemented",
					"command":     "delete",
					"dry_run":     dryRun,
					"backup_id":   backupID,
					"all_backups": allBackups,
				},
				"tergum delete: deletion operation (not yet wired)",
			)
			return nil
		},
	}

	cmd.Flags().String("backup-id", "", "target a specific backup set")
	cmd.Flags().Bool("all-backups", false, "delete across all backup sets")

	return cmd
}
