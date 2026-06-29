package cmd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gcclinux/tergum/internal/config"
	"github.com/gcclinux/tergum/internal/server"
)

func newServerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "server",
		Short: "Start the Tergum daemon",
		Long: `Starts the Tergum daemon in role-aware mode based on [node].role in config:
  - role "server": gRPC services (7400, 7401), web UI (7480), metrics (7490),
    retention engine, scheduler, client registry
  - role "client": client CommandService (7400), heartbeat to server,
    file watcher (if enabled), accepts remote triggers
  - role "hybrid": full local server (same as "server" without client registry)

Handles graceful shutdown on SIGTERM/SIGINT (exit code 10).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfgPath, _ := cmd.Flags().GetString("config")

			cfg, err := config.Load(cfgPath)
			if err != nil {
				return err
			}

			if getCerts, _ := cmd.Flags().GetBool("get-certs"); getCerts {
				return printServerCACertFingerprint(cfg)
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

	cmd.Flags().Bool("get-certs", false, "print the SHA-256 fingerprint of the server's CA certificate")

	return cmd
}

func printServerCACertFingerprint(cfg *config.Config) error {
	caPath := cfg.TLS.CACert
	if caPath == "" {
		return fmt.Errorf("TLS CA certificate path is not configured (tls.ca_cert)")
	}

	// Expand tilde if present
	if strings.HasPrefix(caPath, "~") {
		home, err := os.UserHomeDir()
		if err == nil {
			if caPath == "~" {
				caPath = home
			} else if strings.HasPrefix(caPath, "~/") || strings.HasPrefix(caPath, "~\\") {
				caPath = filepath.Join(home, caPath[2:])
			}
		}
	}

	caPEM, err := os.ReadFile(caPath)
	if err != nil {
		return fmt.Errorf("read CA certificate: %w", err)
	}

	block, _ := pem.Decode(caPEM)
	if block == nil {
		return fmt.Errorf("failed to decode CA certificate PEM")
	}

	fingerprint := sha256.Sum256(block.Bytes)
	fingerprintHex := hex.EncodeToString(fingerprint[:])
	var pairs []string
	for i := 0; i < len(fingerprintHex)-1; i += 2 {
		pairs = append(pairs, fingerprintHex[i:i+2])
	}

	fmt.Println("Server CA certificate fingerprint (SHA-256):")
	fmt.Printf("  %s\n", strings.Join(pairs, ":"))
	return nil
}
