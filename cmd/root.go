// Package cmd implements the Tergum CLI using cobra.
package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/ricardopadilha/tergum/internal/model"
	"github.com/spf13/cobra"
)

// Global flags accessible to all subcommands.
var (
	cfgFile string
	jsonOut bool
	dryRun  bool
)

// Version information set at build time via ldflags.
var (
	Version   = "dev"
	Commit    = "none"
	BuildDate = "unknown"
)

// rootCmd is the top-level Tergum command.
var rootCmd = &cobra.Command{
	Use:   "tergum",
	Short: "Tergum - Encrypted, deduplicated backup system",
	Long: `Tergum v3.0 is an encrypted, deduplicated backup system with gRPC streaming,
mutual TLS authentication, policy-based retention, and real-time file watching.

A single binary acts as client, server, or both depending on the subcommand
and node role configuration.`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file path (default: platform-specific tergum.toml)")
	rootCmd.PersistentFlags().BoolVar(&jsonOut, "json", false, "output machine-readable JSON")
	rootCmd.PersistentFlags().BoolVar(&dryRun, "dry-run", false, "preview destructive operations without executing")

	rootCmd.AddCommand(newSetupCmd())
	rootCmd.AddCommand(newServerCmd())
	rootCmd.AddCommand(newAdminCmd())
	rootCmd.AddCommand(newBackupCmd())
	rootCmd.AddCommand(newRestoreCmd())
	rootCmd.AddCommand(newDeleteCmd())
	rootCmd.AddCommand(newListCmd())
	rootCmd.AddCommand(newStopCmd())
	rootCmd.AddCommand(newWatchCmd())
	rootCmd.AddCommand(newRetentionCmd())
	rootCmd.AddCommand(newPathsCmd())
	rootCmd.AddCommand(newStatusCmd())
	rootCmd.AddCommand(newMigrateCmd())
	rootCmd.AddCommand(newVersionCmd())
}

// Execute runs the root command and returns the appropriate exit code.
// Exit codes are mapped from model.GetExitCode():
//
//	0 - success
//	1 - general error
//	2 - configuration error
//	3 - connection error
//	4 - authentication error
//	5 - storage error
//	10 - stopped by user
//	11 - backup failed
func Execute() int {
	if err := rootCmd.Execute(); err != nil {
		code := model.GetExitCode(err)
		if jsonOut {
			out := map[string]interface{}{
				"error":     err.Error(),
				"exit_code": code,
			}
			data, _ := json.Marshal(out)
			fmt.Fprintln(os.Stderr, string(data))
		} else {
			fmt.Fprintln(os.Stderr, "Error:", err)
		}
		return code
	}
	return model.ExitSuccess
}

// printOutput handles outputting results respecting the --json flag.
func printOutput(data interface{}, humanMsg string) {
	if jsonOut {
		out, _ := json.MarshalIndent(data, "", "  ")
		fmt.Println(string(out))
	} else {
		fmt.Println(humanMsg)
	}
}
