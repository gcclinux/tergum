package cmd

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/gcclinux/tergum/internal/config"
	"github.com/gcclinux/tergum/internal/observe"
	"github.com/spf13/cobra"
)

// adminFingerprintRe validates a canonical SPKI fingerprint (64 lowercase hex).
var adminClientFingerprintRe = regexp.MustCompile(`^[0-9a-f]{64}$`)

func newAdminClientCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "admin-client",
		Short: "Manage admin clients that may perform cross-client restores (server-side)",
		Long: `Manage the list of admin clients recorded in tergum.toml.

An admin client is a regular client whose trusted TLS SPKI fingerprint is granted
server-equivalent privileges: it may restore files from one client to another and
drive cross-client operations without being the server itself.

Requires the node role to be "server" or "hybrid". Identity is keyed on the
client's SPKI fingerprint (SHA-256 of its mTLS certificate public key), not on
its name or client-supplied metadata.`,
	}

	cmd.AddCommand(newAdminClientAddCmd())
	cmd.AddCommand(newAdminClientRemoveCmd())
	cmd.AddCommand(newAdminClientListCmd())

	return cmd
}

func newAdminClientAddCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add <name>",
		Short: "Grant admin privilege to a client",
		Long: `Grant admin privilege to a client by recording its SPKI fingerprint.

Two ways to resolve the fingerprint:
  - Pass --fingerprint with the client's SPKI fingerprint (from 'tergum client
    fingerprint' on that node). This is a trusted path and is used directly.
  - Omit --fingerprint to look the client up in the registry by name. The client
    must have connected at least once so the server has recorded its fingerprint.
    This prints the resolved fingerprint and requires confirmation (--yes or an
    interactive y/N prompt) before writing.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			fingerprint, _ := cmd.Flags().GetString("fingerprint")
			yes, _ := cmd.Flags().GetBool("yes")
			return runAdminClientAdd(args[0], fingerprint, yes)
		},
	}

	cmd.Flags().String("fingerprint", "", "SPKI fingerprint of the client (trusted; skips registry lookup)")
	cmd.Flags().Bool("yes", false, "skip the confirmation prompt for a registry-resolved fingerprint")

	return cmd
}

func newAdminClientRemoveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remove <name>",
		Short: "Revoke a client's admin privilege",
		Long: `Revoke a client's admin privilege. By default the entry is matched by name;
pass --fingerprint to match by SPKI fingerprint instead. Removal only reduces
privilege, so no confirmation is required.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			fingerprint, _ := cmd.Flags().GetString("fingerprint")
			return runAdminClientRemove(args[0], fingerprint)
		},
	}

	cmd.Flags().String("fingerprint", "", "remove by SPKI fingerprint instead of by name")

	return cmd
}

func newAdminClientListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List configured admin clients and their current status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAdminClientList()
		},
	}
}

func runAdminClientAdd(name, fingerprint string, yes bool) error {
	// The role guard and registry access mirror the client command group.
	reg, _, cleanup, err := openRegistry("admin-client")
	if err != nil {
		return err
	}
	defer cleanup()

	var fp string
	if fingerprint != "" {
		// Trusted path: use the supplied fingerprint directly after normalizing.
		fp = strings.ToLower(strings.TrimSpace(fingerprint))
		if !adminClientFingerprintRe.MatchString(fp) {
			return fmt.Errorf("invalid --fingerprint: must be 64 hex characters")
		}
	} else {
		// Resolve the fingerprint from the registry by client name.
		ci := reg.GetClient(name)
		if ci == nil || ci.SPKIFingerprint == "" {
			return fmt.Errorf("client %q has no recorded fingerprint; have it connect once then re-run with --yes, or pass --fingerprint", name)
		}
		fp = ci.SPKIFingerprint

		// Show the resolved identity and require confirmation before writing.
		fmt.Printf("Resolved admin client %q:\n", name)
		fmt.Printf("  Fingerprint: %s\n", fp)
		if !ci.LastSeen.IsZero() {
			fmt.Printf("  Last seen:   %s (%s)\n", ci.LastSeen.Local().Format(time.DateTime), formatTimeAgo(ci.LastSeen))
		} else {
			fmt.Printf("  Last seen:   never\n")
		}
		if ci.MachineID != "" {
			fmt.Printf("  Machine ID:  %s\n", ci.MachineID)
		}
		if ci.Hostname != "" {
			fmt.Printf("  Hostname:    %s\n", ci.Hostname)
		}

		if !yes {
			fmt.Print("Grant admin privilege to this client? [y/N]: ")
			scanner := bufio.NewScanner(os.Stdin)
			answer := ""
			if scanner.Scan() {
				answer = strings.ToLower(strings.TrimSpace(scanner.Text()))
			}
			if answer != "y" && answer != "yes" {
				return fmt.Errorf("aborted: confirmation declined")
			}
		}
	}

	configPath := resolveConfigPath()

	if dryRun {
		printOutput(
			map[string]interface{}{
				"client":      name,
				"fingerprint": fp,
				"status":      "dry-run",
			},
			fmt.Sprintf("[DRY RUN] Would grant admin privilege to %q (%s); config not written.", name, fp),
		)
		return nil
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	already := cfg.IsAdminFingerprint(fp)
	if err := cfg.AddAdminClient(name, fp); err != nil {
		return err
	}

	status := "added"
	if already {
		status = "unchanged"
	}

	if !already {
		if err := writeConfigTOML(configPath, cfg); err != nil {
			return fmt.Errorf("writing config: %w", err)
		}
		observe.Logger("admin-client").Info("admin client added",
			"event", "admin_client_added", "client", name, "fingerprint", fp)
	}

	printOutput(
		map[string]interface{}{
			"client":      name,
			"fingerprint": fp,
			"status":      status,
		},
		fmt.Sprintf("Admin client %q %s (%s).", name, status, fp),
	)
	return nil
}

func runAdminClientRemove(name, fingerprint string) error {
	// Role guard (and config path resolution) consistent with add.
	_, _, cleanup, err := openRegistry("admin-client")
	if err != nil {
		return err
	}
	defer cleanup()

	configPath := resolveConfigPath()

	if dryRun {
		printOutput(
			map[string]interface{}{
				"client": name,
				"status": "dry-run",
			},
			fmt.Sprintf("[DRY RUN] Would remove admin client %q; config not written.", name),
		)
		return nil
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	var removed bool
	if fingerprint != "" {
		removed = cfg.RemoveAdminClientByFingerprint(fingerprint)
	} else {
		removed = cfg.RemoveAdminClientByName(name)
	}

	if !removed {
		printOutput(
			map[string]interface{}{
				"client": name,
				"status": "unchanged",
			},
			fmt.Sprintf("No matching admin client found for %q; nothing changed.", name),
		)
		return nil
	}

	if err := writeConfigTOML(configPath, cfg); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}

	observe.Logger("admin-client").Info("admin client removed",
		"event", "admin_client_removed", "client", name, "fingerprint", fingerprint)

	printOutput(
		map[string]interface{}{
			"client": name,
			"status": "removed",
		},
		fmt.Sprintf("Admin client %q removed.", name),
	)
	return nil
}

func runAdminClientList() error {
	reg, _, cleanup, err := openRegistry("admin-client")
	if err != nil {
		return err
	}
	defer cleanup()

	cfg, err := config.Load(resolveConfigPath())
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// Build a fingerprint -> live status lookup from the registry.
	type regInfo struct {
		name   string
		status string
	}
	byFingerprint := make(map[string]regInfo)
	for _, ci := range reg.ListClients() {
		if ci.SPKIFingerprint != "" {
			byFingerprint[ci.SPKIFingerprint] = regInfo{name: ci.ClientID, status: ci.Status}
		}
	}

	type adminEntry struct {
		Name        string `json:"name"`
		Fingerprint string `json:"fingerprint"`
		Status      string `json:"status"`
	}

	entries := make([]adminEntry, 0, len(cfg.Admin.Clients))
	for _, ac := range cfg.Admin.Clients {
		status := "offline"
		if ri, ok := byFingerprint[ac.Fingerprint]; ok {
			if ri.status != "" {
				status = ri.status
			} else {
				status = "online"
			}
		}
		entries = append(entries, adminEntry{
			Name:        ac.Name,
			Fingerprint: ac.Fingerprint,
			Status:      status,
		})
	}

	if jsonOut {
		printOutput(entries, "")
		return nil
	}

	if len(entries) == 0 {
		fmt.Println("No admin clients configured.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintf(w, "NAME\tFINGERPRINT\tSTATUS\n")
	fmt.Fprintf(w, "----\t-----------\t------\n")
	for _, e := range entries {
		name := e.Name
		if name == "" {
			name = "-"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\n", name, e.Fingerprint, e.Status)
	}
	w.Flush()

	return nil
}

// resolveConfigPath returns the config path to read/write, honoring the global
// --config flag and falling back to the platform default.
func resolveConfigPath() string {
	if cfgFile != "" {
		return cfgFile
	}
	return config.DefaultConfigPath()
}
