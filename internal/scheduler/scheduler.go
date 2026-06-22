// Package scheduler implements cron-based backup scheduling for Tergum.
package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/gcclinux/tergum/internal/config"
	"github.com/gcclinux/tergum/internal/model"
	"github.com/gcclinux/tergum/internal/registry"
)

// BackupTrigger is an interface for triggering backup operations,
// decoupling the scheduler from gRPC or engine details.
type BackupTrigger interface {
	TriggerBackup(ctx context.Context, level model.BackupLevel, clientID string) error
}

// ClientRegistry defines the subset of registry.Registry methods
// needed by the ClientScheduler, enabling testability.
type ClientRegistry interface {
	ListClients() []registry.ClientInfo
	GetClient(clientID string) *registry.ClientInfo
	RecordMissedBackup(clientID, level string, scheduledAt time.Time) error
	ResolveMissedBackups(clientID string) ([]registry.MissedBackup, error)
	SetSchedule(clientID string, schedule registry.ScheduleConfig) error
}

// Scheduler manages cron-based backup scheduling.
type Scheduler interface {
	// Start begins the scheduler with configured cron expressions.
	Start(ctx context.Context) error
	// Stop halts the scheduler.
	Stop() error
}

// cronScheduler is the default implementation of Scheduler using robfig/cron.
type cronScheduler struct {
	cfg       config.SchedulerConfig
	trigger   BackupTrigger
	clientIDs []string
	cron      *cron.Cron
	logger    *slog.Logger

	mu      sync.Mutex
	running bool
}

// New creates a new Scheduler.
// cfg contains the cron expressions for full and auto backups.
// trigger is called when a scheduled backup fires.
// clientIDs lists the clients to trigger backups for. If empty, backups
// are triggered with an empty clientID (meaning "all clients").
func New(cfg config.SchedulerConfig, trigger BackupTrigger, clientIDs []string, logger *slog.Logger) Scheduler {
	if logger == nil {
		logger = slog.Default()
	}
	return &cronScheduler{
		cfg:       cfg,
		trigger:   trigger,
		clientIDs: clientIDs,
		logger:    logger,
	}
}

// Start registers cron jobs for full and auto backups and starts the scheduler.
// It returns an error if the cron expressions are invalid or the scheduler is
// already running.
func (s *cronScheduler) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return fmt.Errorf("scheduler already running")
	}

	s.cron = cron.New(cron.WithLogger(cron.PrintfLogger(slog.NewLogLogger(s.logger.Handler(), slog.LevelDebug))))

	// Register full backup cron job if configured.
	if s.cfg.FullBackupCron != "" {
		expr := s.cfg.FullBackupCron
		_, err := s.cron.AddFunc(expr, func() {
			s.triggerForClients(ctx, model.BackupLevelFull)
		})
		if err != nil {
			return fmt.Errorf("invalid full_backup_cron expression %q: %w", expr, err)
		}
		s.logger.Info("registered full backup schedule", "cron", expr)
	}

	// Register auto backup cron job if configured.
	if s.cfg.AutoBackupCron != "" {
		expr := s.cfg.AutoBackupCron
		_, err := s.cron.AddFunc(expr, func() {
			s.triggerForClients(ctx, model.BackupLevelAuto)
		})
		if err != nil {
			return fmt.Errorf("invalid auto_backup_cron expression %q: %w", expr, err)
		}
		s.logger.Info("registered auto backup schedule", "cron", expr)
	}

	s.cron.Start()
	s.running = true
	s.logger.Info("scheduler started")
	return nil
}

// Stop halts all scheduled cron jobs and waits for any running jobs to finish.
func (s *cronScheduler) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return nil
	}

	s.cron.Stop()
	s.running = false
	s.logger.Info("scheduler stopped")
	return nil
}

// triggerForClients triggers a backup for each configured client. If no clients
// are configured, it triggers with an empty clientID representing "all clients".
func (s *cronScheduler) triggerForClients(ctx context.Context, level model.BackupLevel) {
	if len(s.clientIDs) == 0 {
		if err := s.trigger.TriggerBackup(ctx, level, ""); err != nil {
			s.logger.Error("scheduled backup trigger failed", "level", level.String(), "client", "all", "error", err)
		}
		return
	}

	for _, clientID := range s.clientIDs {
		if err := s.trigger.TriggerBackup(ctx, level, clientID); err != nil {
			s.logger.Error("scheduled backup trigger failed", "level", level.String(), "client", clientID, "error", err)
		}
	}
}

// ---------------------------------------------------------------------------
// ClientScheduler — per-client cron scheduling for server-side management.
// ---------------------------------------------------------------------------

// clientCronEntry tracks cron entry IDs for a client's schedules.
type clientCronEntry struct {
	fullEntryID cron.EntryID
	autoEntryID cron.EntryID
	fullCron    string
	autoCron    string
}

// ClientScheduler manages per-client cron schedules on the server.
// It dynamically adds/removes cron entries as client schedules change,
// triggers backups for online clients, records missed backups for offline
// clients, and handles reconnection catch-up.
type ClientScheduler struct {
	registry ClientRegistry
	trigger  BackupTrigger
	cron     *cron.Cron
	logger   *slog.Logger

	mu      sync.Mutex
	running bool
	entries map[string]*clientCronEntry // keyed by clientID

	// reconnectGrace is how long after reconnection the scheduler waits
	// before triggering missed backups.
	reconnectGrace time.Duration
}

// ClientSchedulerConfig configures the ClientScheduler.
type ClientSchedulerConfig struct {
	Registry       ClientRegistry
	Trigger        BackupTrigger
	Logger         *slog.Logger
	ReconnectGrace time.Duration // defaults to 5s if zero
}

// NewClientScheduler creates a new ClientScheduler.
func NewClientScheduler(cfg ClientSchedulerConfig) *ClientScheduler {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.ReconnectGrace == 0 {
		cfg.ReconnectGrace = 5 * time.Second
	}
	return &ClientScheduler{
		registry:       cfg.Registry,
		trigger:        cfg.Trigger,
		logger:         cfg.Logger,
		entries:        make(map[string]*clientCronEntry),
		reconnectGrace: cfg.ReconnectGrace,
	}
}

// Start initializes the cron engine, loads current client schedules from
// the registry, and begins evaluating them. It blocks until ctx is cancelled.
func (cs *ClientScheduler) Start(ctx context.Context) error {
	cs.mu.Lock()
	if cs.running {
		cs.mu.Unlock()
		return fmt.Errorf("client scheduler already running")
	}
	cs.cron = cron.New(cron.WithLogger(cron.PrintfLogger(slog.NewLogLogger(cs.logger.Handler(), slog.LevelDebug))))
	cs.running = true
	cs.mu.Unlock()

	// Load existing client schedules.
	cs.syncSchedules()

	cs.cron.Start()
	cs.logger.Info("client scheduler started")

	// Block until context is done.
	<-ctx.Done()
	cs.Stop()
	return nil
}

// Stop halts the client scheduler and removes all cron entries.
func (cs *ClientScheduler) Stop() {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	if !cs.running {
		return
	}

	cs.cron.Stop()
	cs.running = false
	cs.entries = make(map[string]*clientCronEntry)
	cs.logger.Info("client scheduler stopped")
}

// SetClientSchedule updates (or adds) the cron schedule for a client.
// It persists the change to the registry and updates the cron engine.
func (cs *ClientScheduler) SetClientSchedule(clientID, fullCron, autoCron string) error {
	// Persist to registry.
	err := cs.registry.SetSchedule(clientID, registry.ScheduleConfig{
		FullBackupCron: fullCron,
		AutoBackupCron: autoCron,
	})
	if err != nil {
		return fmt.Errorf("set client schedule: %w", err)
	}

	cs.mu.Lock()
	defer cs.mu.Unlock()

	if !cs.running {
		return nil
	}

	// Remove old entries for this client.
	cs.removeClientEntriesLocked(clientID)

	// Add new entries.
	cs.addClientEntriesLocked(clientID, fullCron, autoCron)

	cs.logger.Info("client schedule updated",
		"client_id", clientID,
		"full_cron", fullCron,
		"auto_cron", autoCron)
	return nil
}

// HandleReconnect is called when a client reconnects (heartbeat resumes).
// It checks for missed schedules and triggers them within the reconnect grace period.
func (cs *ClientScheduler) HandleReconnect(clientID string) {
	cs.logger.Info("handling client reconnection", "client_id", clientID)

	// Wait a short grace period before triggering missed backups.
	go func() {
		time.Sleep(cs.reconnectGrace)

		missed, err := cs.registry.ResolveMissedBackups(clientID)
		if err != nil {
			cs.logger.Error("failed to resolve missed backups on reconnect",
				"client_id", clientID, "error", err)
			return
		}

		if len(missed) == 0 {
			cs.logger.Debug("no missed backups for reconnected client", "client_id", clientID)
			return
		}

		cs.logger.Info("triggering missed backups for reconnected client",
			"client_id", clientID, "count", len(missed))

		for _, mb := range missed {
			level := missedBackupLevel(mb.Level)
			if err := cs.trigger.TriggerBackup(context.Background(), level, clientID); err != nil {
				cs.logger.Error("failed to trigger missed backup on reconnect",
					"client_id", clientID, "level", mb.Level, "error", err)
			}
		}
	}()
}

// syncSchedules reads all clients from the registry and adds cron entries
// for those with non-empty schedules.
func (cs *ClientScheduler) syncSchedules() {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	clients := cs.registry.ListClients()
	for _, ci := range clients {
		if ci.Schedule == nil {
			continue
		}
		if ci.Schedule.FullBackupCron == "" && ci.Schedule.AutoBackupCron == "" {
			continue
		}
		cs.addClientEntriesLocked(ci.ClientID, ci.Schedule.FullBackupCron, ci.Schedule.AutoBackupCron)
	}
}

// addClientEntriesLocked adds cron entries for a client. Must be called with cs.mu held.
func (cs *ClientScheduler) addClientEntriesLocked(clientID, fullCron, autoCron string) {
	entry := &clientCronEntry{
		fullCron: fullCron,
		autoCron: autoCron,
	}

	if fullCron != "" {
		id, err := cs.cron.AddFunc(fullCron, cs.makeJob(clientID, model.BackupLevelFull))
		if err != nil {
			cs.logger.Error("invalid full_backup_cron for client",
				"client_id", clientID, "cron", fullCron, "error", err)
		} else {
			entry.fullEntryID = id
			cs.logger.Debug("registered full backup schedule for client",
				"client_id", clientID, "cron", fullCron)
		}
	}

	if autoCron != "" {
		id, err := cs.cron.AddFunc(autoCron, cs.makeJob(clientID, model.BackupLevelAuto))
		if err != nil {
			cs.logger.Error("invalid auto_backup_cron for client",
				"client_id", clientID, "cron", autoCron, "error", err)
		} else {
			entry.autoEntryID = id
			cs.logger.Debug("registered auto backup schedule for client",
				"client_id", clientID, "cron", autoCron)
		}
	}

	cs.entries[clientID] = entry
}

// removeClientEntriesLocked removes existing cron entries for a client.
// Must be called with cs.mu held.
func (cs *ClientScheduler) removeClientEntriesLocked(clientID string) {
	entry, exists := cs.entries[clientID]
	if !exists {
		return
	}

	if entry.fullEntryID != 0 {
		cs.cron.Remove(entry.fullEntryID)
	}
	if entry.autoEntryID != 0 {
		cs.cron.Remove(entry.autoEntryID)
	}
	delete(cs.entries, clientID)
}

// makeJob creates a cron job function that triggers a backup for a client,
// checking online status first.
func (cs *ClientScheduler) makeJob(clientID string, level model.BackupLevel) func() {
	return func() {
		ci := cs.registry.GetClient(clientID)
		if ci == nil {
			cs.logger.Warn("scheduled backup for unknown client (removed?)",
				"client_id", clientID, "level", level.String())
			return
		}

		if ci.Status == "online" {
			cs.logger.Info("triggering scheduled backup for online client",
				"client_id", clientID, "level", level.String())
			if err := cs.trigger.TriggerBackup(context.Background(), level, clientID); err != nil {
				cs.logger.Error("scheduled backup trigger failed",
					"client_id", clientID, "level", level.String(), "error", err)
			}
		} else {
			// Client is offline — record missed backup.
			cs.logger.Warn("client offline, recording missed backup",
				"client_id", clientID, "level", level.String())
			if err := cs.registry.RecordMissedBackup(clientID, level.String(), time.Now()); err != nil {
				cs.logger.Error("failed to record missed backup",
					"client_id", clientID, "level", level.String(), "error", err)
			}
		}
	}
}

// missedBackupLevel converts a level string back to a model.BackupLevel.
func missedBackupLevel(level string) model.BackupLevel {
	switch level {
	case "FULL":
		return model.BackupLevelFull
	case "AUTO":
		return model.BackupLevelAuto
	case "ONGOING":
		return model.BackupLevelOngoing
	default:
		return model.BackupLevelAuto
	}
}
