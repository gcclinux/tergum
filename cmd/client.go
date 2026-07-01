package cmd

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/gcclinux/tergum/internal/config"
	"github.com/gcclinux/tergum/internal/registry"

	_ "modernc.org/sqlite"
)

func newClientCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "client",
		Short: "Manage and view remote clients (server-side)",
		Long: `View and manage remote backup clients registered with this server.
Requires the node role to be "server" or "hybrid".`,
	}

	cmd.AddCommand(newClientListCmd())
	cmd.AddCommand(newClientStatusCmd())
	cmd.AddCommand(newClientDisableCmd())
	cmd.AddCommand(newClientEnableCmd())

	return cmd
}

func newClientListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all registered clients and their online/offline status",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runClientList()
		},
	}

	return cmd
}

func newClientStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status <client-name>",
		Short: "Show detailed status for a specific client",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runClientStatus(args[0])
		},
	}

	return cmd
}

func newClientDisableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "disable <client-name>",
		Short: "Disable a client — no backups, restores, or status polling from the server",
		Long: `Disables a registered client on the server side. A disabled client:
  - Will not receive scheduled backups
  - Will not have its heartbeat processed (appears frozen)
  - Cannot be triggered for backup, watcher start/stop, or restore from the server
  - Remains registered and can be re-enabled at any time`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runClientSetDisabled(args[0], true)
		},
	}
}

func newClientEnableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "enable <client-name>",
		Short: "Re-enable a previously disabled client",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runClientSetDisabled(args[0], false)
		},
	}
}

func runClientSetDisabled(clientID string, disabled bool) error {
	reg, _, cleanup, err := openRegistry()
	if err != nil {
		return err
	}
	defer cleanup()

	ci := reg.GetClient(clientID)
	if ci == nil {
		return fmt.Errorf("client %q not found in registry", clientID)
	}

	if err := reg.SetDisabled(clientID, disabled); err != nil {
		return err
	}

	action := "disabled"
	if !disabled {
		action = "enabled"
	}

	printOutput(
		map[string]interface{}{
			"client_id": clientID,
			"disabled":  disabled,
			"status":    action,
		},
		fmt.Sprintf("Client %q %s.", clientID, action),
	)
	return nil
}

func runClientList() error {
	reg, clientsDir, cleanup, err := openRegistry()
	if err != nil {
		return err
	}
	defer cleanup()

	clients := reg.ListClients()

	if jsonOut {
		type clientEntry struct {
			ClientID     string `json:"client_id"`
			Address      string `json:"address"`
			Status       string `json:"status"`
			LastSeen     string `json:"last_seen,omitempty"`
			LastBackup   string `json:"last_backup,omitempty"`
			RegisteredAt string `json:"registered_at,omitempty"`
		}

		entries := make([]clientEntry, 0, len(clients))
		for _, c := range clients {
			entry := clientEntry{
				ClientID: c.ClientID,
				Address:  c.Address,
				Status:   c.Status,
			}
			if !c.LastSeen.IsZero() {
				entry.LastSeen = c.LastSeen.Local().Format(time.DateTime)
			}
			lastBackup := resolveLastBackup(&c, clientsDir)
			if !lastBackup.IsZero() {
				entry.LastBackup = lastBackup.Local().Format(time.DateTime)
			}
			if !c.RegisteredAt.IsZero() {
				entry.RegisteredAt = c.RegisteredAt.Local().Format(time.DateTime)
			}
			entries = append(entries, entry)
		}

		printOutput(entries, "")
		return nil
	}

	if len(clients) == 0 {
		fmt.Println("No clients registered.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintf(w, "CLIENT\tADDRESS\tSTATUS\tLAST SEEN\tLAST BACKUP\n")
	fmt.Fprintf(w, "------\t-------\t------\t---------\t-----------\n")
	for _, c := range clients {
		lastSeen := "never"
		if !c.LastSeen.IsZero() {
			lastSeen = formatTimeAgo(c.LastSeen)
		}
		lastBackup := resolveLastBackup(&c, clientsDir)
		lastBackupStr := "never"
		if !lastBackup.IsZero() {
			lastBackupStr = formatTimeAgo(lastBackup)
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", c.ClientID, c.Address, c.Status, lastSeen, lastBackupStr)
	}
	w.Flush()

	return nil
}

func runClientStatus(clientID string) error {
	reg, clientsDir, cleanup, err := openRegistry()
	if err != nil {
		return err
	}
	defer cleanup()

	ci := reg.GetClient(clientID)
	if ci == nil {
		return fmt.Errorf("client %q not found in registry", clientID)
	}

	lastBackup := resolveLastBackup(ci, clientsDir)

	if jsonOut {
		type clientStatus struct {
			ClientID      string `json:"client_id"`
			Address       string `json:"address"`
			Status        string `json:"status"`
			Disabled      bool   `json:"disabled"`
			LastSeen      string `json:"last_seen,omitempty"`
			LastBackup    string `json:"last_backup,omitempty"`
			WatcherActive bool   `json:"watcher_active"`
			RegisteredAt  string `json:"registered_at,omitempty"`
			MissedBackups int    `json:"missed_backups"`
			Schedule      *struct {
				FullBackupCron string `json:"full_backup_cron,omitempty"`
				AutoBackupCron string `json:"auto_backup_cron,omitempty"`
			} `json:"schedule,omitempty"`
		}

		status := clientStatus{
			ClientID:      ci.ClientID,
			Address:       ci.Address,
			Status:        ci.Status,
			Disabled:      ci.Disabled,
			WatcherActive: ci.WatcherActive,
			MissedBackups: len(ci.MissedBackups),
		}
		if !ci.LastSeen.IsZero() {
			status.LastSeen = ci.LastSeen.Local().Format(time.DateTime)
		}
		if !lastBackup.IsZero() {
			status.LastBackup = lastBackup.Local().Format(time.DateTime)
		}
		if !ci.RegisteredAt.IsZero() {
			status.RegisteredAt = ci.RegisteredAt.Local().Format(time.DateTime)
		}
		if ci.Schedule != nil {
			status.Schedule = &struct {
				FullBackupCron string `json:"full_backup_cron,omitempty"`
				AutoBackupCron string `json:"auto_backup_cron,omitempty"`
			}{
				FullBackupCron: ci.Schedule.FullBackupCron,
				AutoBackupCron: ci.Schedule.AutoBackupCron,
			}
		}

		printOutput(status, "")
		return nil
	}

	// Human-friendly output.
	fmt.Printf("Client:         %s\n", ci.ClientID)
	fmt.Printf("Address:        %s\n", ci.Address)
	fmt.Printf("Status:         %s\n", ci.Status)
	if ci.Disabled {
		fmt.Printf("Disabled:       true\n")
	}

	if !ci.LastSeen.IsZero() {
		fmt.Printf("Last Seen:      %s (%s)\n", ci.LastSeen.Local().Format(time.DateTime), formatTimeAgo(ci.LastSeen))
	} else {
		fmt.Printf("Last Seen:      never\n")
	}

	if !lastBackup.IsZero() {
		fmt.Printf("Last Backup:    %s (%s)\n", lastBackup.Local().Format(time.DateTime), formatTimeAgo(lastBackup))
	} else {
		fmt.Printf("Last Backup:    never\n")
	}

	fmt.Printf("Watcher Active: %v\n", ci.WatcherActive)

	if !ci.RegisteredAt.IsZero() {
		fmt.Printf("Registered:     %s\n", ci.RegisteredAt.Local().Format(time.DateTime))
	}

	if ci.Schedule != nil {
		fmt.Printf("Schedule:\n")
		if ci.Schedule.FullBackupCron != "" {
			fmt.Printf("  Full Backup:  %s\n", ci.Schedule.FullBackupCron)
		}
		if ci.Schedule.AutoBackupCron != "" {
			fmt.Printf("  Auto Backup:  %s\n", ci.Schedule.AutoBackupCron)
		}
	}

	if len(ci.MissedBackups) > 0 {
		fmt.Printf("Missed Backups: %d\n", len(ci.MissedBackups))
		for _, mb := range ci.MissedBackups {
			fmt.Printf("  - %s backup scheduled at %s\n", mb.Level, mb.ScheduledAt.Local().Format(time.DateTime))
		}
	}

	return nil
}

// openRegistry opens a read-only connection to the registry database.
// Returns the registry, the clients dir path, a cleanup function, and any error.
func openRegistry() (*registry.Registry, string, func(), error) {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return nil, "", nil, fmt.Errorf("loading config: %w", err)
	}

	if cfg.Node.Role == "client" {
		return nil, "", nil, fmt.Errorf("'tergum client' commands are only available on server or hybrid nodes")
	}

	dbPath := cfg.Database.Path
	// Ensure the database file exists.
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return nil, "", nil, fmt.Errorf("database not found at %s (has the server been started?)", dbPath)
	}

	db, err := sql.Open("sqlite", filepath.Clean(dbPath))
	if err != nil {
		return nil, "", nil, fmt.Errorf("open database: %w", err)
	}

	reg, err := registry.New(registry.Config{
		DB: db,
	})
	if err != nil {
		db.Close()
		return nil, "", nil, fmt.Errorf("open registry: %w", err)
	}

	clientsDir := filepath.Join(filepath.Dir(dbPath), "clients")

	cleanup := func() {
		db.Close()
	}

	return reg, clientsDir, cleanup, nil
}

// resolveLastBackup returns the last backup time for a client.
// It checks the registry first, and falls back to querying the client's
// synced database for the most recent completed backup.
func resolveLastBackup(ci *registry.ClientInfo, clientsDir string) time.Time {
	if !ci.LastBackup.IsZero() {
		return ci.LastBackup
	}

	// Fall back to querying the client's synced DB.
	clientDBPath := filepath.Join(clientsDir, ci.ClientID+".db")
	if _, err := os.Stat(clientDBPath); err != nil {
		return time.Time{}
	}

	clientDB, err := sql.Open("sqlite", clientDBPath)
	if err != nil {
		return time.Time{}
	}
	defer clientDB.Close()

	var finishedAt *string
	err = clientDB.QueryRow(
		`SELECT finished_at FROM backup_jobs
		 WHERE status = 'completed' AND finished_at IS NOT NULL
		 ORDER BY finished_at DESC LIMIT 1`,
	).Scan(&finishedAt)
	if err != nil || finishedAt == nil {
		return time.Time{}
	}

	// Try RFC3339 first, then legacy datetime format.
	if t, err := time.Parse(time.RFC3339, *finishedAt); err == nil {
		return t
	}
	if t, err := time.ParseInLocation(time.DateTime, *finishedAt, time.UTC); err == nil {
		return t
	}
	return time.Time{}
}

// formatTimeAgo returns a human-friendly relative time string.
func formatTimeAgo(t time.Time) string {
	d := time.Since(t)

	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		mins := int(d.Minutes())
		if mins == 1 {
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", mins)
	case d < 24*time.Hour:
		hours := int(d.Hours())
		if hours == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", hours)
	default:
		days := int(d.Hours() / 24)
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", days)
	}
}
