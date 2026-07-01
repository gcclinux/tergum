// Package registry implements server-side client tracking for Tergum.
// It maintains an in-memory registry of connected client nodes, persists
// state to SQLite, and runs a background goroutine to detect offline clients.
package registry

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// ScheduleConfig holds the cron expressions for a client's scheduled backups.
type ScheduleConfig struct {
	FullBackupCron string
	AutoBackupCron string
}

// MissedBackup records a backup that could not be executed because the client was offline.
type MissedBackup struct {
	ID          int64
	ClientID    string
	Level       string
	ScheduledAt time.Time
	Resolved    bool
	ResolvedAt  *time.Time
}

// ClientInfo describes a registered client and its current state.
type ClientInfo struct {
	ClientID      string
	Address       string
	Status        string // "online" or "offline"
	LastSeen      time.Time
	LastBackup    time.Time
	WatcherActive bool
	Schedule      *ScheduleConfig
	MissedBackups []MissedBackup
	RegisteredAt  time.Time
}

// Registry tracks connected clients with thread-safe in-memory state
// backed by SQLite persistence.
type Registry struct {
	mu      sync.RWMutex
	clients map[string]*ClientInfo
	db      *sql.DB
	logger  *slog.Logger

	offlineThreshold time.Duration // default 90s (3 × 30s heartbeat)
	checkInterval    time.Duration // how often to check for offline clients
}

// Config holds configuration for creating a new Registry.
type Config struct {
	DB               *sql.DB
	Logger           *slog.Logger
	OfflineThreshold time.Duration // defaults to 90s if zero
	CheckInterval    time.Duration // defaults to 30s if zero
}

// New creates a new Registry, creates the required database tables,
// and loads existing client records from the database.
func New(cfg Config) (*Registry, error) {
	if cfg.DB == nil {
		return nil, fmt.Errorf("registry: database connection is required")
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.OfflineThreshold == 0 {
		cfg.OfflineThreshold = 90 * time.Second
	}
	if cfg.CheckInterval == 0 {
		cfg.CheckInterval = 30 * time.Second
	}

	r := &Registry{
		clients:          make(map[string]*ClientInfo),
		db:               cfg.DB,
		logger:           cfg.Logger,
		offlineThreshold: cfg.OfflineThreshold,
		checkInterval:    cfg.CheckInterval,
	}

	if err := r.createTables(); err != nil {
		return nil, fmt.Errorf("registry: create tables: %w", err)
	}

	if err := r.loadClients(); err != nil {
		return nil, fmt.Errorf("registry: load clients: %w", err)
	}

	return r, nil
}

// createTables ensures the client_registry and missed_schedules tables exist.
func (r *Registry) createTables() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS client_registry (
			client_id        TEXT PRIMARY KEY,
			address          TEXT NOT NULL,
			status           TEXT NOT NULL DEFAULT 'offline',
			last_seen        TEXT,
			last_backup      TEXT,
			watcher_active   INTEGER DEFAULT 0,
			full_backup_cron TEXT DEFAULT '',
			auto_backup_cron TEXT DEFAULT '',
			registered_at    TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
		`CREATE TABLE IF NOT EXISTS missed_schedules (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			client_id    TEXT NOT NULL,
			level        TEXT NOT NULL,
			scheduled_at TEXT NOT NULL,
			resolved     INTEGER DEFAULT 0,
			resolved_at  TEXT,
			FOREIGN KEY (client_id) REFERENCES client_registry(client_id)
		)`,
	}

	for _, stmt := range stmts {
		if _, err := r.db.Exec(stmt); err != nil {
			return fmt.Errorf("exec schema: %w", err)
		}
	}
	return nil
}

// loadClients reads all client records from the database into memory.
func (r *Registry) loadClients() error {
	rows, err := r.db.Query(
		`SELECT client_id, address, status, last_seen, last_backup,
		        watcher_active, full_backup_cron, auto_backup_cron, registered_at
		 FROM client_registry`)
	if err != nil {
		return err
	}

	// Collect all clients first, then close rows before querying missed backups.
	var clients []*ClientInfo
	for rows.Next() {
		var ci ClientInfo
		var lastSeen, lastBackup, registeredAt *string
		var watcherActive int
		var fullCron, autoCron string

		if err := rows.Scan(
			&ci.ClientID, &ci.Address, &ci.Status,
			&lastSeen, &lastBackup,
			&watcherActive, &fullCron, &autoCron, &registeredAt,
		); err != nil {
			rows.Close()
			return err
		}

		ci.WatcherActive = watcherActive == 1
		// Parse timestamps, handling both legacy local-time and current UTC storage.
		if lastSeen != nil {
			ci.LastSeen = parseDBTime(*lastSeen)
		}
		if lastBackup != nil {
			ci.LastBackup = parseDBTime(*lastBackup)
		}

		// If the client was persisted as "online" but its last heartbeat is
		// older than the offline threshold, correct the status immediately so
		// the UI shows the right state from the very first poll.
		if ci.Status == "online" && !ci.LastSeen.IsZero() {
			if time.Since(ci.LastSeen) > r.offlineThreshold {
				ci.Status = "offline"
			}
		}
		if registeredAt != nil {
			ci.RegisteredAt = parseDBTime(*registeredAt)
		}
		if fullCron != "" || autoCron != "" {
			ci.Schedule = &ScheduleConfig{
				FullBackupCron: fullCron,
				AutoBackupCron: autoCron,
			}
		}

		clients = append(clients, &ci)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	// Now load missed backups for each client (safe to query since rows are closed).
	for _, ci := range clients {
		missed, err := r.loadMissedBackups(ci.ClientID)
		if err != nil {
			return fmt.Errorf("load missed backups for %s: %w", ci.ClientID, err)
		}
		ci.MissedBackups = missed
		r.clients[ci.ClientID] = ci

		// Re-persist to normalise any legacy local-time values to UTC
		// and write the corrected offline status back to the database.
		_ = r.persistClientLocked(ci)
	}
	return nil
}

// parseDBTime parses a datetime string from the database.
// New records are stored as RFC3339 ("2026-06-24T20:17:54Z") which includes
// timezone info and is always unambiguous.
// Legacy records were stored as plain datetime strings in local time
// ("2026-06-24 21:17:54"). For those, we fall back to time.Local so
// that e.g. a BST-stored time is correctly interpreted as BST, not UTC.
func parseDBTime(s string) time.Time {
	// Try RFC3339 first — unambiguous timezone.
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	// Legacy format: stored without timezone in local time.
	if t, err := time.ParseInLocation(time.DateTime, s, time.Local); err == nil {
		return t
	}
	return time.Time{}
}

// loadMissedBackups retrieves unresolved missed schedules for a client.
func (r *Registry) loadMissedBackups(clientID string) ([]MissedBackup, error) {
	rows, err := r.db.Query(
		`SELECT id, client_id, level, scheduled_at, resolved, resolved_at
		 FROM missed_schedules WHERE client_id = ? AND resolved = 0`,
		clientID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var missed []MissedBackup
	for rows.Next() {
		var mb MissedBackup
		var scheduledAt string
		var resolved int
		var resolvedAt *string

		if err := rows.Scan(&mb.ID, &mb.ClientID, &mb.Level, &scheduledAt, &resolved, &resolvedAt); err != nil {
			return nil, err
		}
		mb.ScheduledAt, _ = time.Parse(time.DateTime, scheduledAt)
		mb.Resolved = resolved == 1
		if resolvedAt != nil {
			t, _ := time.Parse(time.DateTime, *resolvedAt)
			mb.ResolvedAt = &t
		}
		missed = append(missed, mb)
	}
	return missed, rows.Err()
}

// Start launches the background goroutine that periodically checks for
// clients that have exceeded the offline threshold. It blocks until the
// context is cancelled.
func (r *Registry) Start(ctx context.Context) {
	ticker := time.NewTicker(r.checkInterval)
	defer ticker.Stop()

	r.logger.Info("registry: background offline checker started",
		"threshold", r.offlineThreshold,
		"interval", r.checkInterval)

	for {
		select {
		case <-ctx.Done():
			r.logger.Info("registry: background offline checker stopped")
			return
		case <-ticker.C:
			r.checkOfflineClients()
		}
	}
}

// checkOfflineClients marks clients as offline if they haven't been seen
// within the offline threshold.
func (r *Registry) checkOfflineClients() {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	for _, ci := range r.clients {
		if ci.Status == "online" && !ci.LastSeen.IsZero() {
			if now.Sub(ci.LastSeen) > r.offlineThreshold {
				ci.Status = "offline"
				// An offline client cannot have a running watcher — reset to avoid stale state.
				ci.WatcherActive = false
				r.logger.Warn("registry: client marked offline",
					"client_id", ci.ClientID,
					"last_seen", ci.LastSeen)
				r.persistClientLocked(ci)
			}
		}
	}
}

// Register adds or updates a client in the registry. If the client already
// exists, its address and status are updated. Returns the client info.
func (r *Registry) Register(clientID, address string) (*ClientInfo, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	ci, exists := r.clients[clientID]
	if exists {
		ci.Address = address
		ci.Status = "online"
		ci.LastSeen = now
	} else {
		ci = &ClientInfo{
			ClientID:     clientID,
			Address:      address,
			Status:       "online",
			LastSeen:     now,
			RegisteredAt: now,
		}
		r.clients[clientID] = ci
	}

	if err := r.persistClientLocked(ci); err != nil {
		return nil, fmt.Errorf("registry: persist client %s: %w", clientID, err)
	}

	r.logger.Info("registry: client registered",
		"client_id", clientID,
		"address", address,
		"status", ci.Status)

	return ci, nil
}

// Heartbeat updates the last-seen timestamp for a client and marks it online.
func (r *Registry) Heartbeat(clientID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	ci, exists := r.clients[clientID]
	if !exists {
		return fmt.Errorf("registry: unknown client %q", clientID)
	}

	wasOffline := ci.Status == "offline"
	ci.LastSeen = time.Now()
	ci.Status = "online"

	if wasOffline {
		r.logger.Info("registry: client reconnected",
			"client_id", clientID)
	}

	return r.persistClientLocked(ci)
}

// MarkOffline sets a client's status to "offline".
func (r *Registry) MarkOffline(clientID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	ci, exists := r.clients[clientID]
	if !exists {
		return fmt.Errorf("registry: unknown client %q", clientID)
	}

	ci.Status = "offline"
	r.logger.Info("registry: client marked offline", "client_id", clientID)
	return r.persistClientLocked(ci)
}

// ListClients returns a snapshot of all registered clients.
func (r *Registry) ListClients() []ClientInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]ClientInfo, 0, len(r.clients))
	for _, ci := range r.clients {
		// Return a copy to avoid races.
		copy := *ci
		if ci.Schedule != nil {
			s := *ci.Schedule
			copy.Schedule = &s
		}
		// Copy missed backups slice.
		if ci.MissedBackups != nil {
			copy.MissedBackups = make([]MissedBackup, len(ci.MissedBackups))
			for i, mb := range ci.MissedBackups {
				copy.MissedBackups[i] = mb
			}
		}
		result = append(result, copy)
	}
	return result
}

// GetClient returns the info for a specific client, or nil if not found.
func (r *Registry) GetClient(clientID string) *ClientInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	ci, exists := r.clients[clientID]
	if !exists {
		return nil
	}

	// Return a copy.
	copy := *ci
	if ci.Schedule != nil {
		s := *ci.Schedule
		copy.Schedule = &s
	}
	if ci.MissedBackups != nil {
		copy.MissedBackups = make([]MissedBackup, len(ci.MissedBackups))
		for i, mb := range ci.MissedBackups {
			copy.MissedBackups[i] = mb
		}
	}
	return &copy
}

// SetSchedule configures the cron schedule for a client.
func (r *Registry) SetSchedule(clientID string, schedule ScheduleConfig) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	ci, exists := r.clients[clientID]
	if !exists {
		return fmt.Errorf("registry: unknown client %q", clientID)
	}

	ci.Schedule = &schedule
	r.logger.Info("registry: schedule updated",
		"client_id", clientID,
		"full_cron", schedule.FullBackupCron,
		"auto_cron", schedule.AutoBackupCron)

	return r.persistClientLocked(ci)
}

// SetWatcherActive updates the watcher active state for a client.
func (r *Registry) SetWatcherActive(clientID string, active bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	ci, exists := r.clients[clientID]
	if !exists {
		return fmt.Errorf("registry: unknown client %q", clientID)
	}

	ci.WatcherActive = active
	return r.persistClientLocked(ci)
}

// SetLastBackup updates the last backup timestamp for a client.
func (r *Registry) SetLastBackup(clientID string, t time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	ci, exists := r.clients[clientID]
	if !exists {
		return fmt.Errorf("registry: unknown client %q", clientID)
	}

	ci.LastBackup = t
	return r.persistClientLocked(ci)
}

// RecordMissedBackup records that a scheduled backup could not fire because
// the client was offline.
func (r *Registry) RecordMissedBackup(clientID, level string, scheduledAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	ci, exists := r.clients[clientID]
	if !exists {
		return fmt.Errorf("registry: unknown client %q", clientID)
	}

	_, err := r.db.Exec(
		`INSERT INTO missed_schedules (client_id, level, scheduled_at) VALUES (?, ?, ?)`,
		clientID, level, scheduledAt.Format(time.DateTime))
	if err != nil {
		return fmt.Errorf("registry: record missed backup: %w", err)
	}

	mb := MissedBackup{
		ClientID:    clientID,
		Level:       level,
		ScheduledAt: scheduledAt,
	}
	ci.MissedBackups = append(ci.MissedBackups, mb)
	return nil
}

// ResolveMissedBackups marks all unresolved missed schedules for a client
// as resolved and returns them.
func (r *Registry) ResolveMissedBackups(clientID string) ([]MissedBackup, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	ci, exists := r.clients[clientID]
	if !exists {
		return nil, fmt.Errorf("registry: unknown client %q", clientID)
	}

	now := time.Now()
	nowStr := now.Format(time.DateTime)

	_, err := r.db.Exec(
		`UPDATE missed_schedules SET resolved = 1, resolved_at = ? WHERE client_id = ? AND resolved = 0`,
		nowStr, clientID)
	if err != nil {
		return nil, fmt.Errorf("registry: resolve missed backups: %w", err)
	}

	// Return the unresolved ones and clear from memory.
	var resolved []MissedBackup
	for _, mb := range ci.MissedBackups {
		if !mb.Resolved {
			mb.Resolved = true
			mb.ResolvedAt = &now
			resolved = append(resolved, mb)
		}
	}
	ci.MissedBackups = nil

	return resolved, nil
}

// persistClientLocked writes the client info to the database.
// Must be called while holding r.mu.
func (r *Registry) persistClientLocked(ci *ClientInfo) error {
	var lastSeen, lastBackup *string
	// Store timestamps as RFC3339 UTC ("2006-01-02T15:04:05Z") so the
	// timezone is unambiguous when loaded back by parseDBTime.
	if !ci.LastSeen.IsZero() {
		v := ci.LastSeen.UTC().Format(time.RFC3339)
		lastSeen = &v
	}
	if !ci.LastBackup.IsZero() {
		v := ci.LastBackup.UTC().Format(time.RFC3339)
		lastBackup = &v
	}

	var watcherActive int
	if ci.WatcherActive {
		watcherActive = 1
	}

	var fullCron, autoCron string
	if ci.Schedule != nil {
		fullCron = ci.Schedule.FullBackupCron
		autoCron = ci.Schedule.AutoBackupCron
	}

	registeredAt := ci.RegisteredAt.UTC().Format(time.RFC3339)

	_, err := r.db.Exec(
		`INSERT INTO client_registry (client_id, address, status, last_seen, last_backup,
		                              watcher_active, full_backup_cron, auto_backup_cron, registered_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(client_id) DO UPDATE SET
		   address = excluded.address,
		   status = excluded.status,
		   last_seen = excluded.last_seen,
		   last_backup = excluded.last_backup,
		   watcher_active = excluded.watcher_active,
		   full_backup_cron = excluded.full_backup_cron,
		   auto_backup_cron = excluded.auto_backup_cron`,
		ci.ClientID, ci.Address, ci.Status, lastSeen, lastBackup,
		watcherActive, fullCron, autoCron, registeredAt,
	)
	return err
}
