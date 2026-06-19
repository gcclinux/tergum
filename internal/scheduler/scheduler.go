// Package scheduler implements cron-based backup scheduling for Tergum.
package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/robfig/cron/v3"

	"github.com/ricardopadilha/tergum/internal/config"
	"github.com/ricardopadilha/tergum/internal/model"
)

// BackupTrigger is an interface for triggering backup operations,
// decoupling the scheduler from gRPC or engine details.
type BackupTrigger interface {
	TriggerBackup(ctx context.Context, level model.BackupLevel, clientID string) error
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
