package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gcclinux/tergum/internal/config"
	"github.com/gcclinux/tergum/internal/db"
	"github.com/spf13/cobra"
)

func newPathsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "paths",
		Short: "Manage backup include/exclude paths",
		Long: `Manage which directories are included in backups and which patterns are excluded.

Include paths define the top-level directories that will be backed up and watched.
Exclude patterns define files or directories to skip (glob syntax).`,
	}

	cmd.AddCommand(newPathsScanCmd())
	cmd.AddCommand(newPathsAddCmd())
	cmd.AddCommand(newPathsRemoveCmd())
	cmd.AddCommand(newPathsExcludeCmd())
	cmd.AddCommand(newPathsUnexcludeCmd())
	cmd.AddCommand(newPathsListCmd())

	return cmd
}

func newPathsScanCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "scan [directory]",
		Short: "Scan a directory and add top-level folders as include paths",
		Long: `Scans the given directory (defaults to home folder) and adds each
top-level subdirectory as an include path. Hidden directories (starting with ".")
are excluded by default unless --include-hidden is set.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			includeHidden, _ := cmd.Flags().GetBool("include-hidden")

			scanDir := ""
			if len(args) > 0 {
				scanDir = args[0]
			} else {
				home, err := os.UserHomeDir()
				if err != nil {
					return fmt.Errorf("cannot determine home directory: %w", err)
				}
				scanDir = home
			}

			absDir, err := filepath.Abs(scanDir)
			if err != nil {
				return fmt.Errorf("cannot resolve path: %w", err)
			}

			entries, err := os.ReadDir(absDir)
			if err != nil {
				return fmt.Errorf("cannot read directory %s: %w", absDir, err)
			}

			repo, cleanup, err := openRepo()
			if err != nil {
				return err
			}
			defer cleanup()

			ctx := context.Background()
			var added []string

			for _, entry := range entries {
				if !entry.IsDir() {
					continue
				}
				name := entry.Name()
				if !includeHidden && strings.HasPrefix(name, ".") {
					continue
				}
				fullPath := filepath.Join(absDir, name)
				if err := repo.AddIncludePath(ctx, fullPath); err != nil {
					return fmt.Errorf("cannot add path %s: %w", fullPath, err)
				}
				added = append(added, fullPath)
			}

			printOutput(
				map[string]interface{}{
					"scanned":     absDir,
					"paths_added": added,
					"count":       len(added),
				},
				fmt.Sprintf("Scanned %s: added %d directories to include paths.", absDir, len(added)),
			)
			return nil
		},
	}

	cmd.Flags().Bool("include-hidden", false, "include hidden directories (starting with '.')")

	return cmd
}

func newPathsAddCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "add [path...]",
		Short: "Add one or more include paths",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			repo, cleanup, err := openRepo()
			if err != nil {
				return err
			}
			defer cleanup()

			ctx := context.Background()
			var added []string

			for _, p := range args {
				absPath, err := filepath.Abs(p)
				if err != nil {
					return fmt.Errorf("cannot resolve path %s: %w", p, err)
				}
				if err := repo.AddIncludePath(ctx, absPath); err != nil {
					return fmt.Errorf("cannot add path %s: %w", absPath, err)
				}
				added = append(added, absPath)
			}

			printOutput(
				map[string]interface{}{"added": added},
				fmt.Sprintf("Added %d include path(s).", len(added)),
			)
			return nil
		},
	}
}

func newPathsRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove [path...]",
		Short: "Remove one or more include paths",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			repo, cleanup, err := openRepo()
			if err != nil {
				return err
			}
			defer cleanup()

			ctx := context.Background()
			var removed []string

			for _, p := range args {
				absPath, err := filepath.Abs(p)
				if err != nil {
					absPath = p
				}
				if err := repo.RemoveIncludePath(ctx, absPath); err != nil {
					return fmt.Errorf("cannot remove path %s: %w", absPath, err)
				}
				removed = append(removed, absPath)
			}

			printOutput(
				map[string]interface{}{"removed": removed},
				fmt.Sprintf("Removed %d include path(s).", len(removed)),
			)
			return nil
		},
	}
}

func newPathsExcludeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "exclude [pattern...]",
		Short: "Add one or more exclude patterns",
		Long: `Add glob patterns to exclude from backups. Examples:
  tergum paths exclude "*.tmp" "*.log" "node_modules/"
  tergum paths exclude ".git/" "__pycache__/"`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			repo, cleanup, err := openRepo()
			if err != nil {
				return err
			}
			defer cleanup()

			ctx := context.Background()
			var added []string

			for _, pattern := range args {
				if err := repo.AddExcludePattern(ctx, pattern); err != nil {
					return fmt.Errorf("cannot add pattern %s: %w", pattern, err)
				}
				added = append(added, pattern)
			}

			printOutput(
				map[string]interface{}{"excluded": added},
				fmt.Sprintf("Added %d exclude pattern(s).", len(added)),
			)
			return nil
		},
	}
}

func newPathsUnexcludeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unexclude [pattern...]",
		Short: "Remove one or more exclude patterns",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			repo, cleanup, err := openRepo()
			if err != nil {
				return err
			}
			defer cleanup()

			ctx := context.Background()
			var removed []string

			for _, pattern := range args {
				if err := repo.RemoveExcludePattern(ctx, pattern); err != nil {
					return fmt.Errorf("cannot remove pattern %s: %w", pattern, err)
				}
				removed = append(removed, pattern)
			}

			printOutput(
				map[string]interface{}{"unexcluded": removed},
				fmt.Sprintf("Removed %d exclude pattern(s).", len(removed)),
			)
			return nil
		},
	}
}

func newPathsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all include paths and exclude patterns",
		RunE: func(cmd *cobra.Command, args []string) error {
			repo, cleanup, err := openRepo()
			if err != nil {
				return err
			}
			defer cleanup()

			ctx := context.Background()

			includes, err := repo.ListIncludePaths(ctx)
			if err != nil {
				return fmt.Errorf("cannot list include paths: %w", err)
			}

			excludes, err := repo.ListExcludePatterns(ctx)
			if err != nil {
				return fmt.Errorf("cannot list exclude patterns: %w", err)
			}

			sort.Strings(includes)
			sort.Strings(excludes)

			if jsonOut {
				printOutput(map[string]interface{}{
					"include_paths":    includes,
					"exclude_patterns": excludes,
				}, "")
			} else {
				fmt.Println("Include Paths:")
				if len(includes) == 0 {
					fmt.Println("  (none)")
				}
				for _, p := range includes {
					fmt.Printf("  + %s\n", p)
				}
				fmt.Println()
				fmt.Println("Exclude Patterns:")
				if len(excludes) == 0 {
					fmt.Println("  (none)")
				}
				for _, p := range excludes {
					fmt.Printf("  - %s\n", p)
				}
			}
			return nil
		},
	}
}

// openRepo loads the config and opens the database for CLI path operations.
func openRepo() (db.Repository, func(), error) {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return nil, nil, err
	}

	repo, err := db.NewRepository(cfg.Database.Path, cfg.Node.Role == "server" || cfg.Node.Role == "both")
	if err != nil {
		return nil, nil, fmt.Errorf("cannot open database: %w", err)
	}

	cleanup := func() {
		repo.Close()
	}
	return repo, cleanup, nil
}
