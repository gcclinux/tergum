package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/gcclinux/tergum/internal/config"
	"github.com/gcclinux/tergum/internal/server"
)

func newClientCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "client",
		Short: "Start the Tergum client daemon",
		Long:  `Starts the Tergum client daemon, registering with the server and enabling automated backups.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfgPath, _ := cmd.Flags().GetString("config")

			cfg, err := config.Load(cfgPath)
			if err != nil {
				return err
			}

			if cfg.Node.Role != "client" {
				return fmt.Errorf("cannot start client daemon on a node configured with the %q role. Use 'tergum server' instead", cfg.Node.Role)
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
