package cmd

import "github.com/spf13/cobra"

func newWatchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Start file watcher for ongoing backup",
		Long: `Starts the file watcher to monitor configured paths. Files that pass the
stability gate are automatically backed up.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			daemon, _ := cmd.Flags().GetBool("daemon")
			printOutput(
				map[string]interface{}{
					"status":  "not_implemented",
					"command": "watch",
					"daemon":  daemon,
				},
				"tergum watch: file watcher (not yet wired)",
			)
			return nil
		},
	}

	cmd.Flags().BoolP("daemon", "d", false, "run as background daemon")

	return cmd
}
