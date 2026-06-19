package cmd

import (
	"context"
	"os"

	"github.com/spf13/cobra"

	"github.com/ricardopadilha/tergum/internal/config"
	"github.com/ricardopadilha/tergum/internal/server"
)

func newServerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "server",
		Short: "Start the Tergum server",
		Long: `Starts gRPC services (ports 7400, 7401), metrics (7490), retention engine,
and scheduler. Handles graceful shutdown on SIGTERM/SIGINT (exit code 10).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfgPath, _ := cmd.Flags().GetString("config")

			cfg, err := config.Load(cfgPath)
			if err != nil {
				return err
			}

			if err := cfg.Validate(); err != nil {
				return err
			}

			srv, err := server.New(cfg)
			if err != nil {
				return err
			}

			if err := srv.Start(context.Background()); err != nil {
				return err
			}

			// Graceful shutdown exit code 10 (stopped by user).
			os.Exit(10)
			return nil
		},
	}

	return cmd
}
