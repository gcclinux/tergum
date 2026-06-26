package cmd

import (
	"fmt"

	"github.com/gcclinux/tergum/internal/config"
	"github.com/spf13/cobra"
)

func newNodeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "node",
		Short: "Manage node role and identity settings",
		Long: `View or change the node's role (server, hybrid) and hostname configuration.

Changing the role between "server" and "hybrid" controls whether the node can
run local backups and file watchers (hybrid) or only serve remote clients (server).

The hostname setting identifies which network interface address to advertise
to remote clients.`,
	}

	cmd.AddCommand(newNodeShowCmd())
	cmd.AddCommand(newNodeRoleCmd())
	cmd.AddCommand(newNodeHostnameCmd())

	return cmd
}

func newNodeShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Show current node role and hostname",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(cfgFile)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			hostname := cfg.Node.Hostname
			if hostname == "" {
				hostname = "(not set)"
			}

			printOutput(
				map[string]interface{}{
					"role":     cfg.Node.Role,
					"hostname": cfg.Node.Hostname,
				},
				fmt.Sprintf("Node Role: %s\nHostname:  %s", cfg.Node.Role, hostname),
			)
			return nil
		},
	}
}

func newNodeRoleCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "role",
		Short: "View or change the node role",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "set <server|hybrid>",
		Short: "Change the node role",
		Long: `Change the node role between "server" and "hybrid".

  server  — Serves remote clients only. No local backups, no local file watcher.
  hybrid  — Full server capabilities PLUS local backup, file watcher, and scheduling.

A restart of the tergum server process is required for the change to take effect.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			newRole := args[0]
			if newRole != "server" && newRole != "hybrid" {
				return fmt.Errorf("invalid role %q: must be \"server\" or \"hybrid\"", newRole)
			}

			path := cfgFile
			if path == "" {
				path = config.DefaultConfigPath()
			}

			cfg, err := config.Load(path)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			oldRole := cfg.Node.Role
			if oldRole == newRole {
				printOutput(
					map[string]interface{}{
						"status": "unchanged",
						"role":   newRole,
					},
					fmt.Sprintf("Node role is already %q. No change needed.", newRole),
				)
				return nil
			}

			cfg.Node.Role = newRole

			if err := writeConfigTOML(path, cfg); err != nil {
				return fmt.Errorf("saving config: %w", err)
			}

			printOutput(
				map[string]interface{}{
					"status":   "success",
					"old_role": oldRole,
					"new_role": newRole,
				},
				fmt.Sprintf("Node role changed from %q to %q.\nThe Web UI reflects this immediately on refresh. CLI backup/watch commands use the saved config.", oldRole, newRole),
			)
			return nil
		},
	})

	return cmd
}

func newNodeHostnameCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hostname",
		Short: "View or change the node hostname (network interface)",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "set <hostname>",
		Short: "Set the hostname (network interface address to advertise)",
		Long: `Set the hostname that identifies which network interface to use.

This is the address advertised to remote clients and used for binding services.
Useful when the node has multiple network interfaces and you want to control
which one is used for backup traffic.

Examples: 192.168.1.10, backup.internal.example.com, eth0-ip`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			newHostname := args[0]

			path := cfgFile
			if path == "" {
				path = config.DefaultConfigPath()
			}

			cfg, err := config.Load(path)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			oldHostname := cfg.Node.Hostname
			cfg.Node.Hostname = newHostname

			if err := writeConfigTOML(path, cfg); err != nil {
				return fmt.Errorf("saving config: %w", err)
			}

			msg := fmt.Sprintf("Hostname set to %q.", newHostname)
			if oldHostname != "" {
				msg = fmt.Sprintf("Hostname changed from %q to %q.", oldHostname, newHostname)
			}
			msg += "\nThe Web UI reflects this immediately on refresh."

			printOutput(
				map[string]interface{}{
					"status":       "success",
					"old_hostname": oldHostname,
					"new_hostname": newHostname,
				},
				msg,
			)
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "clear",
		Short: "Clear the hostname setting (use system default)",
		RunE: func(cmd *cobra.Command, args []string) error {
			path := cfgFile
			if path == "" {
				path = config.DefaultConfigPath()
			}

			cfg, err := config.Load(path)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			oldHostname := cfg.Node.Hostname
			if oldHostname == "" {
				printOutput(
					map[string]interface{}{
						"status":   "unchanged",
						"hostname": "",
					},
					"Hostname is already not set. No change needed.",
				)
				return nil
			}

			cfg.Node.Hostname = ""

			if err := writeConfigTOML(path, cfg); err != nil {
				return fmt.Errorf("saving config: %w", err)
			}

			printOutput(
				map[string]interface{}{
					"status":       "success",
					"old_hostname": oldHostname,
					"new_hostname": "",
				},
				fmt.Sprintf("Hostname cleared (was %q). System default will be used.\nThe Web UI reflects this immediately on refresh.", oldHostname),
			)
			return nil
		},
	})

	return cmd
}
