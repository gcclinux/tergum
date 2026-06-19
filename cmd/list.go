package cmd

import "github.com/spf13/cobra"

func newListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List backup sets and files",
		Long:  `List backup jobs, files within a backup, or all backed-up files matching a pattern.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			backupID, _ := cmd.Flags().GetString("backup-id")
			printOutput(
				map[string]interface{}{
					"status":    "not_implemented",
					"command":   "list",
					"backup_id": backupID,
				},
				"tergum list: list operation (not yet wired)",
			)
			return nil
		},
	}

	cmd.Flags().String("backup-id", "", "list files within a specific backup set")
	cmd.Flags().StringP("pattern", "p", "", "filter files by glob pattern")

	return cmd
}
