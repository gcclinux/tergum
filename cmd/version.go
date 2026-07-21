package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/gcclinux/tergum/internal/config"
	"github.com/gcclinux/tergum/internal/connection"
	tergumgrpc "github.com/gcclinux/tergum/internal/grpc"
)

func newVersionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Long:  `Displays the client and server Tergum version, git commit, and build date.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			clientLine := fmt.Sprintf("client version %s (commit: %s, built: %s)", Version, Commit, BuildDate)

			// Try to get the server version.
			serverLine := ""
			cfg, err := config.Load(cfgFile)
			if err == nil && cfg.Node.Role == "client" && cfg.Server.Address != "" {
				serverLine = queryServerVersion(cfg)
			}

			if jsonOut {
				info := map[string]interface{}{
					"client": map[string]string{
						"version":    Version,
						"commit":     Commit,
						"build_date": BuildDate,
					},
				}
				if serverLine != "" {
					// serverLine is only set when we got a valid response
					info["server"] = serverVersionInfo
				}
				printOutput(info, "")
			} else {
				fmt.Println(clientLine)
				if serverLine != "" {
					fmt.Println(serverLine)
				}
			}

			return nil
		},
	}

	return cmd
}

// serverVersionInfo holds the last successful server version query for JSON output.
var serverVersionInfo map[string]string

// queryServerVersion attempts to ping the server and return a formatted version string.
// Returns an empty string if the server cannot be reached within 3 seconds.
func queryServerVersion(cfg *config.Config) string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	tlsCfg, clientID, err := connection.LoadClientTLS(cfg)
	if err != nil {
		return ""
	}

	client, err := tergumgrpc.Connect(
		ctx,
		cfg.Server.Address,
		cfg.Server.CommandPort,
		cfg.Server.DataPort,
		tlsCfg,
	)
	if err != nil {
		return ""
	}

	client.SetClientID(clientID)

	resp, err := client.Ping(ctx)
	if err != nil {
		return ""
	}

	commit := resp.Commit
	if commit == "" {
		commit = "unknown"
	}
	buildDate := resp.BuildDate
	if buildDate == "" {
		buildDate = "unknown"
	}

	serverVersionInfo = map[string]string{
		"version":    resp.Version,
		"commit":     commit,
		"build_date": buildDate,
	}

	return fmt.Sprintf("server version %s (commit: %s, built: %s)", resp.Version, commit, buildDate)
}
