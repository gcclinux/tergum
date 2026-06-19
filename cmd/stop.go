package cmd

import "github.com/spf13/cobra"

func newStopCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop an in-progress backup",
		Long:  `Sends a stop signal to gracefully terminate the current backup operation.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			printOutput(
				map[string]string{"status": "not_implemented", "command": "stop"},
				"tergum stop: stop operation (not yet wired)",
			)
			return nil
		},
	}

	return cmd
}
