package cmd

import "github.com/spf13/cobra"

func newStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show current system status",
		Long:  `Displays the current status of backup operations, server connectivity, and watcher state.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			printOutput(
				map[string]string{"status": "not_implemented", "command": "status"},
				"tergum status: system status (not yet wired)",
			)
			return nil
		},
	}

	return cmd
}
