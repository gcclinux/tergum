package cmd

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/gcclinux/tergum/internal/config"
	"github.com/gcclinux/tergum/internal/connection"
	grpcpkg "github.com/gcclinux/tergum/internal/grpc"
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
	cmd.AddCommand(newClientFingerprintCmd())

	return cmd
}

func newClientFingerprintCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "fingerprint",
		Short: "Print this node's TLS SPKI fingerprint (its trusted admin identity)",
		Long: `Prints the SHA-256 of this node's mTLS certificate SubjectPublicKeyInfo
(SPKI), lowercase hex. This is the stable, key-based identity the server uses to
recognize an admin client — paste it into 'tergum admin-client add --fingerprint'
on the server. Available on any node role.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runClientFingerprint()
		},
	}
}

// runClientFingerprint computes and prints this node's SPKI fingerprint. It
// parses the leaf certificate via x509.ParseCertificate (the loaded
// tls.Certificate has a nil Leaf) and hashes RawSubjectPublicKeyInfo, producing
// a value byte-identical to the server's clientSPKIFromContext.
func runClientFingerprint() error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	tlsCfg, _, err := connection.LoadClientTLS(cfg)
	if err != nil {
		return fmt.Errorf("loading TLS config: %w", err)
	}
	if len(tlsCfg.Certificates) == 0 || len(tlsCfg.Certificates[0].Certificate) == 0 {
		return fmt.Errorf("no client certificate loaded")
	}

	leaf, err := x509.ParseCertificate(tlsCfg.Certificates[0].Certificate[0])
	if err != nil {
		return fmt.Errorf("parsing client certificate: %w", err)
	}

	sum := sha256.Sum256(leaf.RawSubjectPublicKeyInfo)
	fp := hex.EncodeToString(sum[:])

	printOutput(
		map[string]interface{}{"fingerprint": fp},
		fp,
	)
	return nil
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
	reg, _, cleanup, err := openRegistry("client")
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

// clientListRow is a neutral, source-agnostic view of a single client row for
// `tergum client list`. Both the local (registry) path and the remote (gRPC)
// path build these so rendering goes through one formatter (renderClientList).
// LastBackup is already resolved by whichever path built the row.
type clientListRow struct {
	ClientID     string
	Address      string
	OSFamily     string
	Hostname     string
	MachineID    string
	Status       string
	LastSeen     time.Time
	LastBackup   time.Time
	RegisteredAt time.Time
}

// clientMissedBackup is a neutral view of one missed scheduled backup.
type clientMissedBackup struct {
	Level       string
	ScheduledAt time.Time
}

// clientSchedule is a neutral view of a client's backup schedule.
type clientSchedule struct {
	FullBackupCron string
	AutoBackupCron string
}

// clientStatusView is a neutral, source-agnostic view of a single client's full
// status for `tergum client status`. Both the local and remote paths build it
// so rendering goes through one formatter (renderClientStatus). LastBackup is
// already resolved by whichever path built the view.
type clientStatusView struct {
	ClientID      string
	Address       string
	OSFamily      string
	Hostname      string
	MachineID     string
	Status        string
	Disabled      bool
	WatcherActive bool
	LastSeen      time.Time
	LastBackup    time.Time
	RegisteredAt  time.Time
	Schedule      *clientSchedule
	MissedBackups []clientMissedBackup
}

func runClientList() error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// Admin client: route the read to the server over gRPC, authorized there by
	// this node's SPKI fingerprint. Server/hybrid nodes keep the local path.
	if cfg.Node.Role == "client" {
		rows, err := remoteClientList(cfg)
		if err != nil {
			return err
		}
		renderClientList(rows)
		return nil
	}

	reg, clientsDir, cleanup, err := openRegistry("client")
	if err != nil {
		return err
	}
	defer cleanup()

	clients := reg.ListClients()
	rows := make([]clientListRow, 0, len(clients))
	for i := range clients {
		c := clients[i]
		rows = append(rows, clientListRow{
			ClientID:     c.ClientID,
			Address:      c.Address,
			OSFamily:     c.OSFamily,
			Hostname:     c.Hostname,
			MachineID:    c.MachineID,
			Status:       c.Status,
			LastSeen:     c.LastSeen,
			LastBackup:   resolveLastBackup(&c, clientsDir),
			RegisteredAt: c.RegisteredAt,
		})
	}

	renderClientList(rows)
	return nil
}

// renderClientList writes the client list to stdout, honoring --json. This is
// the single formatter shared by the local and remote paths; the output is
// identical regardless of where the rows came from.
func renderClientList(rows []clientListRow) {
	if jsonOut {
		type clientEntry struct {
			ClientID     string `json:"client_id"`
			Address      string `json:"address"`
			OSFamily     string `json:"os_family,omitempty"`
			Hostname     string `json:"hostname,omitempty"`
			MachineID    string `json:"machine_id,omitempty"`
			Status       string `json:"status"`
			LastSeen     string `json:"last_seen,omitempty"`
			LastBackup   string `json:"last_backup,omitempty"`
			RegisteredAt string `json:"registered_at,omitempty"`
		}

		entries := make([]clientEntry, 0, len(rows))
		for _, c := range rows {
			entry := clientEntry{
				ClientID:  c.ClientID,
				Address:   c.Address,
				OSFamily:  c.OSFamily,
				Hostname:  c.Hostname,
				MachineID: c.MachineID,
				Status:    c.Status,
			}
			if !c.LastSeen.IsZero() {
				entry.LastSeen = c.LastSeen.Local().Format(time.DateTime)
			}
			if !c.LastBackup.IsZero() {
				entry.LastBackup = c.LastBackup.Local().Format(time.DateTime)
			}
			if !c.RegisteredAt.IsZero() {
				entry.RegisteredAt = c.RegisteredAt.Local().Format(time.DateTime)
			}
			entries = append(entries, entry)
		}

		printOutput(entries, "")
		return
	}

	if len(rows) == 0 {
		fmt.Println("No clients registered.")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintf(w, "CLIENT\tADDRESS\tOS\tSTATUS\tLAST SEEN\tLAST BACKUP\n")
	fmt.Fprintf(w, "------\t-------\t--\t------\t---------\t-----------\n")
	for _, c := range rows {
		lastSeen := "never"
		if !c.LastSeen.IsZero() {
			lastSeen = formatTimeAgo(c.LastSeen)
		}
		lastBackupStr := "never"
		if !c.LastBackup.IsZero() {
			lastBackupStr = formatTimeAgo(c.LastBackup)
		}
		osFamily := c.OSFamily
		if osFamily == "" {
			osFamily = "-"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", c.ClientID, c.Address, osFamily, c.Status, lastSeen, lastBackupStr)
	}
	w.Flush()
}

func runClientStatus(clientID string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// Admin client: route the read to the server over gRPC. Server/hybrid nodes
	// keep the local path.
	if cfg.Node.Role == "client" {
		view, err := remoteClientStatus(cfg, clientID)
		if err != nil {
			return err
		}
		renderClientStatus(view)
		return nil
	}

	reg, clientsDir, cleanup, err := openRegistry("client")
	if err != nil {
		return err
	}
	defer cleanup()

	ci := reg.GetClient(clientID)
	if ci == nil {
		return fmt.Errorf("client %q not found in registry", clientID)
	}

	view := clientStatusView{
		ClientID:      ci.ClientID,
		Address:       ci.Address,
		OSFamily:      ci.OSFamily,
		Hostname:      ci.Hostname,
		MachineID:     ci.MachineID,
		Status:        ci.Status,
		Disabled:      ci.Disabled,
		WatcherActive: ci.WatcherActive,
		LastSeen:      ci.LastSeen,
		LastBackup:    resolveLastBackup(ci, clientsDir),
		RegisteredAt:  ci.RegisteredAt,
	}
	if ci.Schedule != nil {
		view.Schedule = &clientSchedule{
			FullBackupCron: ci.Schedule.FullBackupCron,
			AutoBackupCron: ci.Schedule.AutoBackupCron,
		}
	}
	for _, mb := range ci.MissedBackups {
		view.MissedBackups = append(view.MissedBackups, clientMissedBackup{
			Level:       mb.Level,
			ScheduledAt: mb.ScheduledAt,
		})
	}

	renderClientStatus(view)
	return nil
}

// renderClientStatus writes a single client's status to stdout, honoring
// --json. This is the single formatter shared by the local and remote paths.
func renderClientStatus(view clientStatusView) {
	if jsonOut {
		type clientStatus struct {
			ClientID      string `json:"client_id"`
			Address       string `json:"address"`
			Status        string `json:"status"`
			Disabled      bool   `json:"disabled"`
			LastSeen      string `json:"last_seen,omitempty"`
			LastBackup    string `json:"last_backup,omitempty"`
			WatcherActive bool   `json:"watcher_active"`
			OSFamily      string `json:"os_family,omitempty"`
			Hostname      string `json:"hostname,omitempty"`
			MachineID     string `json:"machine_id,omitempty"`
			RegisteredAt  string `json:"registered_at,omitempty"`
			MissedBackups int    `json:"missed_backups"`
			Schedule      *struct {
				FullBackupCron string `json:"full_backup_cron,omitempty"`
				AutoBackupCron string `json:"auto_backup_cron,omitempty"`
			} `json:"schedule,omitempty"`
		}

		status := clientStatus{
			ClientID:      view.ClientID,
			Address:       view.Address,
			OSFamily:      view.OSFamily,
			Hostname:      view.Hostname,
			MachineID:     view.MachineID,
			Status:        view.Status,
			Disabled:      view.Disabled,
			WatcherActive: view.WatcherActive,
			MissedBackups: len(view.MissedBackups),
		}
		if !view.LastSeen.IsZero() {
			status.LastSeen = view.LastSeen.Local().Format(time.DateTime)
		}
		if !view.LastBackup.IsZero() {
			status.LastBackup = view.LastBackup.Local().Format(time.DateTime)
		}
		if !view.RegisteredAt.IsZero() {
			status.RegisteredAt = view.RegisteredAt.Local().Format(time.DateTime)
		}
		if view.Schedule != nil {
			status.Schedule = &struct {
				FullBackupCron string `json:"full_backup_cron,omitempty"`
				AutoBackupCron string `json:"auto_backup_cron,omitempty"`
			}{
				FullBackupCron: view.Schedule.FullBackupCron,
				AutoBackupCron: view.Schedule.AutoBackupCron,
			}
		}

		printOutput(status, "")
		return
	}

	// Human-friendly output.
	fmt.Printf("Client:         %s\n", view.ClientID)
	fmt.Printf("Address:        %s\n", view.Address)
	if view.OSFamily != "" {
		fmt.Printf("OS Family:      %s\n", view.OSFamily)
	}
	if view.Hostname != "" {
		fmt.Printf("Hostname:       %s\n", view.Hostname)
	}
	if view.MachineID != "" {
		fmt.Printf("Machine ID:     %s\n", view.MachineID)
	}
	fmt.Printf("Status:         %s\n", view.Status)
	if view.Disabled {
		fmt.Printf("Disabled:       true\n")
	}

	if !view.LastSeen.IsZero() {
		fmt.Printf("Last Seen:      %s (%s)\n", view.LastSeen.Local().Format(time.DateTime), formatTimeAgo(view.LastSeen))
	} else {
		fmt.Printf("Last Seen:      never\n")
	}

	if !view.LastBackup.IsZero() {
		fmt.Printf("Last Backup:    %s (%s)\n", view.LastBackup.Local().Format(time.DateTime), formatTimeAgo(view.LastBackup))
	} else {
		fmt.Printf("Last Backup:    never\n")
	}

	fmt.Printf("Watcher Active: %v\n", view.WatcherActive)

	if !view.RegisteredAt.IsZero() {
		fmt.Printf("Registered:     %s\n", view.RegisteredAt.Local().Format(time.DateTime))
	}

	if view.Schedule != nil {
		fmt.Printf("Schedule:\n")
		if view.Schedule.FullBackupCron != "" {
			fmt.Printf("  Full Backup:  %s\n", view.Schedule.FullBackupCron)
		}
		if view.Schedule.AutoBackupCron != "" {
			fmt.Printf("  Auto Backup:  %s\n", view.Schedule.AutoBackupCron)
		}
	}

	if len(view.MissedBackups) > 0 {
		fmt.Printf("Missed Backups: %d\n", len(view.MissedBackups))
		for _, mb := range view.MissedBackups {
			fmt.Printf("  - %s backup scheduled at %s\n", mb.Level, mb.ScheduledAt.Local().Format(time.DateTime))
		}
	}
}

// dialAdminServer connects to the server command channel as this (admin) client
// node, exactly like runAdminRemoteRestore. The server authorizes read access
// by this node's trusted mTLS SPKI fingerprint.
func dialAdminServer(ctx context.Context, cfg *config.Config) (*grpcpkg.TergumClient, error) {
	tlsCfg, clientID, err := connection.LoadClientTLS(cfg)
	if err != nil {
		return nil, fmt.Errorf("loading TLS config: %w", err)
	}
	client, err := grpcpkg.Connect(ctx, cfg.Server.Address, cfg.Server.CommandPort, cfg.Server.DataPort, tlsCfg)
	if err != nil {
		return nil, fmt.Errorf("connecting to server: %w", err)
	}
	client.SetClientID(clientID)
	return client, nil
}

// adminAuthzError converts a server authorization denial into an actionable
// message for the operator; other errors pass through unchanged.
func adminAuthzError(err error) error {
	if status.Code(err) == codes.PermissionDenied {
		return fmt.Errorf("this client is not authorized as an admin client on the server (ask the server operator to run 'tergum admin-client add')")
	}
	return err
}

// parseProtoTime parses an RFC3339 timestamp carried in a proto message back to
// a time.Time, returning the zero time for an empty or unparseable value.
func parseProtoTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	return time.Time{}
}

// remoteClientList asks the server for its registered-client list and maps the
// response into neutral rows for the shared renderer.
func remoteClientList(cfg *config.Config) ([]clientListRow, error) {
	ctx := context.Background()
	client, err := dialAdminServer(ctx, cfg)
	if err != nil {
		return nil, err
	}

	resp, err := client.ListClients(ctx)
	if err != nil {
		return nil, adminAuthzError(err)
	}

	rows := make([]clientListRow, 0, len(resp.Clients))
	for _, c := range resp.Clients {
		rows = append(rows, clientListRow{
			ClientID:     c.ClientId,
			Address:      c.Address,
			OSFamily:     c.OsFamily,
			Hostname:     c.Hostname,
			MachineID:    c.MachineId,
			Status:       c.Status,
			LastSeen:     parseProtoTime(c.LastSeen),
			LastBackup:   parseProtoTime(c.LastBackup),
			RegisteredAt: parseProtoTime(c.RegisteredAt),
		})
	}
	return rows, nil
}

// remoteClientStatus asks the server for a single client's status and maps the
// response into a neutral view for the shared renderer. A not-found client
// yields the same error as the local path.
func remoteClientStatus(cfg *config.Config, clientID string) (clientStatusView, error) {
	ctx := context.Background()
	client, err := dialAdminServer(ctx, cfg)
	if err != nil {
		return clientStatusView{}, err
	}

	resp, err := client.GetClientStatus(ctx, clientID)
	if err != nil {
		return clientStatusView{}, adminAuthzError(err)
	}
	if !resp.Found {
		return clientStatusView{}, fmt.Errorf("client %q not found in registry", clientID)
	}

	view := clientStatusView{
		ClientID:      resp.ClientId,
		Address:       resp.Address,
		OSFamily:      resp.OsFamily,
		Hostname:      resp.Hostname,
		MachineID:     resp.MachineId,
		Status:        resp.Status,
		Disabled:      resp.Disabled,
		WatcherActive: resp.WatcherActive,
		LastSeen:      parseProtoTime(resp.LastSeen),
		LastBackup:    parseProtoTime(resp.LastBackup),
		RegisteredAt:  parseProtoTime(resp.RegisteredAt),
	}
	if resp.HasSchedule {
		view.Schedule = &clientSchedule{
			FullBackupCron: resp.FullBackupCron,
			AutoBackupCron: resp.AutoBackupCron,
		}
	}
	for _, mb := range resp.MissedBackupDetails {
		view.MissedBackups = append(view.MissedBackups, clientMissedBackup{
			Level:       mb.Level,
			ScheduledAt: parseProtoTime(mb.ScheduledAt),
		})
	}
	return view, nil
}

// openRegistry opens a read-only connection to the registry database.
// cmdName names the invoking command group (e.g. "client", "admin-client") so
// the role-guard error reports the correct command. Returns the registry, the
// clients dir path, a cleanup function, and any error.
func openRegistry(cmdName string) (*registry.Registry, string, func(), error) {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return nil, "", nil, fmt.Errorf("loading config: %w", err)
	}

	if cfg.Node.Role == "client" {
		return nil, "", nil, fmt.Errorf("'tergum %s' commands are only available on server or hybrid nodes", cmdName)
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
