package cmd

import "github.com/spf13/cobra"

func newRestoreCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "restore [query]",
		Short: "Restore files from backup",
		Long:  `Search for and restore files by name, path, pattern, or backup set.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			dest, _ := cmd.Flags().GetString("dest")
			concurrency, _ := cmd.Flags().GetInt("concurrency")
			printOutput(
				map[string]interface{}{
					"status":      "not_implemented",
					"command":     "restore",
					"dest":        dest,
					"concurrency": concurrency,
				},
				"tergum restore: restore operation (not yet wired)",
			)
			return nil
		},
	}

	cmd.Flags().StringP("dest", "d", ".", "destination directory for restored files")
	cmd.Flags().IntP("concurrency", "c", 4, "number of parallel restore streams")
	cmd.Flags().String("backup-id", "", "restore from a specific backup set")

	return cmd
}
