package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newVersionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Long:  `Displays the Tergum version, git commit, and build date.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			info := map[string]string{
				"version":    Version,
				"commit":     Commit,
				"build_date": BuildDate,
			}
			printOutput(
				info,
				fmt.Sprintf("tergum version %s (commit: %s, built: %s)", Version, Commit, BuildDate),
			)
			return nil
		},
	}

	return cmd
}
