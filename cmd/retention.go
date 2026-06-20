package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/ricardopadilha/tergum/internal/config"
	"github.com/ricardopadilha/tergum/internal/db"
	"github.com/ricardopadilha/tergum/internal/model"
	"github.com/ricardopadilha/tergum/internal/retention"
	"github.com/ricardopadilha/tergum/internal/storage"
	"github.com/spf13/cobra"
)

func newRetentionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "retention",
		Short: "Manage retention policies",
		Long: `Manage retention policies or manually trigger a retention evaluation.

By default, all backup data is kept FOREVER unless a retention policy explicitly
targets it. Policies use glob patterns to match specific files or folders.

Safety rules:
  - The most recent version of any file is NEVER deleted
  - Files with only one version are NEVER touched
  - No matching policy = keep forever
  - KeepDays not set = keep forever (even if policy matches)

This means full backup images are preserved indefinitely unless you specifically
create a policy targeting them.`,
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
		Long: `Evaluates all enabled retention policies and expires matching older versions.
Use --dry-run to preview what would be deleted without actually deleting.

Only older versions of files are considered. The latest version is always protected.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(cfgFile)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			repo, err := db.NewRepository(cfg.Database.Path, cfg.Database.WALMode)
			if err != nil {
				return fmt.Errorf("opening database: %w", err)
			}
			defer repo.Close()

			storageDir := cfg.StorageDir()
			cas := storage.NewCAS(storageDir, repo)

			engine := retention.New(repo, cas)
			ctx := context.Background()

			if dryRun {
				fmt.Println("[DRY RUN] Evaluating retention policies...")
			} else {
				fmt.Println("Evaluating retention policies...")
			}

			result, err := engine.Evaluate(ctx, dryRun)
			if err != nil {
				return fmt.Errorf("retention evaluation failed: %w", err)
			}

			prefix := ""
			if dryRun {
				prefix = "[DRY RUN] "
			}

			printOutput(
				map[string]interface{}{
					"dry_run":           dryRun,
					"entries_evaluated": result.EntriesEvaluated,
					"entries_expired":   result.EntriesExpired,
					"bytes_freed":       result.BytesFreed,
					"files_deleted":     result.FilesDeleted,
					"protected":         result.Protected,
				},
				fmt.Sprintf("%sEvaluated: %d entries | Expired: %d | Bytes freed: %d | Storage files deleted: %d | Protected: %d",
					prefix, result.EntriesEvaluated, result.EntriesExpired, result.BytesFreed, result.FilesDeleted, result.Protected),
			)
			return nil
		},
	}
}

func newRetentionListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List configured retention policies",
		Long: `Lists all retention policies. Policies with no keep_days are "keep forever"
policies that just ensure a minimum number of versions is kept.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(cfgFile)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			repo, err := db.NewRepository(cfg.Database.Path, cfg.Database.WALMode)
			if err != nil {
				return fmt.Errorf("opening database: %w", err)
			}
			defer repo.Close()

			engine := retention.New(repo, nil)
			ctx := context.Background()

			policies, err := engine.ListPolicies(ctx)
			if err != nil {
				return fmt.Errorf("listing policies: %w", err)
			}

			if len(policies) == 0 {
				printOutput(
					map[string]interface{}{"policies": []string{}},
					"No retention policies configured. All backups are kept forever.",
				)
				return nil
			}

			if jsonOut {
				type policyJSON struct {
					Name         string `json:"name"`
					Pattern      string `json:"pattern"`
					KeepDays     *int   `json:"keep_days"`
					KeepVersions int    `json:"keep_versions"`
					Priority     int    `json:"priority"`
					Enabled      bool   `json:"enabled"`
				}
				var out []policyJSON
				for _, p := range policies {
					out = append(out, policyJSON{
						Name:         p.Name,
						Pattern:      p.Pattern,
						KeepDays:     p.KeepDays,
						KeepVersions: p.KeepVersions,
						Priority:     p.Priority,
						Enabled:      p.Enabled,
					})
				}
				printOutput(map[string]interface{}{"policies": out, "count": len(out)}, "")
			} else {
				fmt.Printf("Retention Policies (%d):\n\n", len(policies))
				fmt.Printf("  %-20s  %-30s  %10s  %10s  %8s  %s\n",
					"NAME", "PATTERN", "KEEP DAYS", "KEEP VERS", "PRIORITY", "ENABLED")
				fmt.Printf("  %s\n", strings.Repeat("-", 100))
				for _, p := range policies {
					keepDays := "forever"
					if p.KeepDays != nil {
						keepDays = fmt.Sprintf("%d", *p.KeepDays)
					}
					enabled := "yes"
					if !p.Enabled {
						enabled = "no"
					}
					fmt.Printf("  %-20s  %-30s  %10s  %10d  %8d  %s\n",
						p.Name, p.Pattern, keepDays, p.KeepVersions, p.Priority, enabled)
				}
				fmt.Println()
				fmt.Println("Files not matching any policy are kept FOREVER.")
			}
			return nil
		},
	}
}

func newRetentionAddCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add [name]",
		Short: "Add a retention policy",
		Long: `Add a retention policy that targets files matching a glob pattern.

Examples:
  # Keep only 7 days of logs, minimum 2 versions
  tergum retention add cleanup-logs --pattern "*.log" --keep-days 7 --keep-versions 2

  # Keep Downloads for 30 days, minimum 1 version  
  tergum retention add cleanup-downloads --pattern "/home/user/Downloads/*" --keep-days 30

  # Keep tmp files for 3 days
  tergum retention add cleanup-tmp --pattern "*.tmp" --keep-days 3 --priority 10

  # Keep everything else forever (this is the DEFAULT even without a policy)

Notes:
  - Omitting --keep-days means the policy will never expire entries (keep forever)
  - --keep-versions ensures at least N versions are kept regardless of age
  - The latest version of any file is ALWAYS protected regardless of policies
  - Higher --priority policies are evaluated first (first-match-wins)`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			keepDays, _ := cmd.Flags().GetInt("keep-days")
			keepVersions, _ := cmd.Flags().GetInt("keep-versions")
			pattern, _ := cmd.Flags().GetString("pattern")
			priority, _ := cmd.Flags().GetInt("priority")

			cfg, err := config.Load(cfgFile)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			repo, err := db.NewRepository(cfg.Database.Path, cfg.Database.WALMode)
			if err != nil {
				return fmt.Errorf("opening database: %w", err)
			}
			defer repo.Close()

			engine := retention.New(repo, nil)
			ctx := context.Background()

			policy := model.RetentionPolicy{
				Name:         name,
				KeepVersions: keepVersions,
				Pattern:      pattern,
				Priority:     priority,
				Enabled:      true,
			}

			// Only set KeepDays if explicitly provided (0 means "not set" = forever)
			if keepDays > 0 {
				policy.KeepDays = &keepDays
			}

			if err := engine.AddPolicy(ctx, policy); err != nil {
				return fmt.Errorf("adding policy: %w", err)
			}

			keepDaysStr := "forever"
			if policy.KeepDays != nil {
				keepDaysStr = fmt.Sprintf("%d days", *policy.KeepDays)
			}

			printOutput(
				map[string]interface{}{
					"name":          name,
					"pattern":       pattern,
					"keep_days":     policy.KeepDays,
					"keep_versions": keepVersions,
					"priority":      priority,
				},
				fmt.Sprintf("Policy %q added: pattern=%s, keep=%s, min_versions=%d, priority=%d",
					name, pattern, keepDaysStr, keepVersions, priority),
			)
			return nil
		},
	}

	cmd.Flags().Int("keep-days", 0, "number of days to retain versions (0 = forever)")
	cmd.Flags().Int("keep-versions", 1, "minimum number of versions to always keep")
	cmd.Flags().String("pattern", "*", "glob pattern to match file paths")
	cmd.Flags().Int("priority", 0, "policy evaluation priority (higher = evaluated first)")

	return cmd
}

func newRetentionRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove [name]",
		Short: "Remove a retention policy",
		Long:  `Remove a retention policy by name. Files previously managed by this policy will be kept forever.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			cfg, err := config.Load(cfgFile)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			repo, err := db.NewRepository(cfg.Database.Path, cfg.Database.WALMode)
			if err != nil {
				return fmt.Errorf("opening database: %w", err)
			}
			defer repo.Close()

			engine := retention.New(repo, nil)
			ctx := context.Background()

			if err := engine.RemovePolicy(ctx, name); err != nil {
				return fmt.Errorf("removing policy: %w", err)
			}

			printOutput(
				map[string]interface{}{"removed": name},
				fmt.Sprintf("Policy %q removed. Files it covered will now be kept forever.", name),
			)
			return nil
		},
	}
}
