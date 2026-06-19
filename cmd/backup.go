package cmd

import "github.com/spf13/cobra"

func newBackupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Trigger a backup operation",
		Long:  `Triggers a full or incremental backup of configured include paths.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			level, _ := cmd.Flags().GetString("level")
			printOutput(
				map[string]string{"status": "not_implemented", "command": "backup", "level": level},
				"tergum backup: backup operation (not yet wired)",
			)
			return nil
		},
	}

	cmd.Flags().StringP("level", "l", "auto", "backup level: auto, full")

	return cmd
}
