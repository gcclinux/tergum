package cmd

import "github.com/spf13/cobra"

func newRetentionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "retention",
		Short: "Manage retention policies",
		Long:  `Manage retention policies or manually trigger a retention evaluation.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			printOutput(
				map[string]interface{}{
					"status":  "not_implemented",
					"command": "retention",
					"dry_run": dryRun,
				},
				"tergum retention: retention management (not yet wired)",
			)
			return nil
		},
	}

	cmd.AddCommand(newRetentionRunCmd())
	cmd.AddCommand(newRetentionListCmd())
	cmd.AddCommand(newRetentionAddCmd())
	cmd.AddCommand(newRetentionRemoveCmd())

	return cmd
}

func newRetentionRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run",
		Short: "Manually trigger retention evaluation",
		Long:  `Evaluates all retention policies and expires matching versions. Use --dry-run to preview.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			printOutput(
				map[string]interface{}{
					"status":  "not_implemented",
					"command": "retention run",
					"dry_run": dryRun,
				},
				"tergum retention run: evaluation (not yet wired)",
			)
			return nil
		},
	}
}

func newRetentionListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List configured retention policies",
		RunE: func(cmd *cobra.Command, args []string) error {
			printOutput(
				map[string]interface{}{
					"status":  "not_implemented",
					"command": "retention list",
				},
				"tergum retention list: policy listing (not yet wired)",
			)
			return nil
		},
	}
}

func newRetentionAddCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add [name]",
		Short: "Add a retention policy",
		RunE: func(cmd *cobra.Command, args []string) error {
			printOutput(
				map[string]interface{}{
					"status":  "not_implemented",
					"command": "retention add",
				},
				"tergum retention add: add policy (not yet wired)",
			)
			return nil
		},
	}

	cmd.Flags().Int("keep-days", 0, "number of days to retain versions")
	cmd.Flags().Int("keep-versions", 1, "minimum number of versions to keep")
	cmd.Flags().String("pattern", "*", "glob pattern to match files")
	cmd.Flags().Int("priority", 0, "policy evaluation priority (higher = first)")

	return cmd
}

func newRetentionRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove [name]",
		Short: "Remove a retention policy",
		RunE: func(cmd *cobra.Command, args []string) error {
			printOutput(
				map[string]interface{}{
					"status":  "not_implemented",
					"command": "retention remove",
				},
				"tergum retention remove: remove policy (not yet wired)",
			)
			return nil
		},
	}
}
