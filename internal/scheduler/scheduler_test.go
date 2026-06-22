package scheduler

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/gcclinux/tergum/internal/config"
	"github.com/gcclinux/tergum/internal/model"
)

// mockTrigger records all TriggerBackup calls for verification.
type mockTrigger struct {
	mu    sync.Mutex
	calls []triggerCall
}

type triggerCall struct {
	Level    model.BackupLevel
	ClientID string
}

func (m *mockTrigger) TriggerBackup(_ context.Context, level model.BackupLevel, clientID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, triggerCall{Level: level, ClientID: clientID})
	return nil
}

func (m *mockTrigger) getCalls() []triggerCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]triggerCall, len(m.calls))
	copy(out, m.calls)
	return out
}

func TestNew(t *testing.T) {
	cfg := config.SchedulerConfig{
		FullBackupCron: "0 2 * * 0",
		AutoBackupCron: "0 3 * * *",
	}
	trigger := &mockTrigger{}
	s := New(cfg, trigger, nil, nil)
	if s == nil {
		t.Fatal("expected non-nil scheduler")
	}
}

func TestStart_InvalidFullCron(t *testing.T) {
	cfg := config.SchedulerConfig{
		FullBackupCron: "invalid cron",
	}
	trigger := &mockTrigger{}
	s := New(cfg, trigger, nil, nil)

	err := s.Start(context.Background())
	if err == nil {
		t.Fatal("expected error for invalid full_backup_cron")
	}
}

func TestStart_InvalidAutoCron(t *testing.T) {
	cfg := config.SchedulerConfig{
		AutoBackupCron: "bad expression !!!",
	}
	trigger := &mockTrigger{}
	s := New(cfg, trigger, nil, nil)

	err := s.Start(context.Background())
	if err == nil {
		t.Fatal("expected error for invalid auto_backup_cron")
	}
}

func TestStart_AlreadyRunning(t *testing.T) {
	cfg := config.SchedulerConfig{
		FullBackupCron: "0 2 * * 0",
	}
	trigger := &mockTrigger{}
	s := New(cfg, trigger, nil, nil)

	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("unexpected error on first start: %v", err)
	}
	defer s.Stop()

	err := s.Start(context.Background())
	if err == nil {
		t.Fatal("expected error when starting already-running scheduler")
	}
}

func TestStop_NotRunning(t *testing.T) {
	cfg := config.SchedulerConfig{}
	trigger := &mockTrigger{}
	s := New(cfg, trigger, nil, nil)

	// Stopping a scheduler that was never started should not error.
	if err := s.Stop(); err != nil {
		t.Fatalf("unexpected error stopping non-running scheduler: %v", err)
	}
}

func TestScheduler_TriggersBackup(t *testing.T) {
	// Use @every descriptor that fires every second for testing.
	cfg := config.SchedulerConfig{
		AutoBackupCron: "@every 1s",
	}
	trigger := &mockTrigger{}
	s := New(cfg, trigger, nil, nil)

	ctx := context.Background()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("unexpected start error: %v", err)
	}

	// Wait for at least one trigger.
	time.Sleep(1500 * time.Millisecond)
	s.Stop()

	calls := trigger.getCalls()
	if len(calls) == 0 {
		t.Fatal("expected at least one backup trigger call")
	}

	for _, c := range calls {
		if c.Level != model.BackupLevelAuto {
			t.Errorf("expected level AUTO, got %v", c.Level)
		}
		if c.ClientID != "" {
			t.Errorf("expected empty clientID for all-clients mode, got %q", c.ClientID)
		}
	}
}

func TestScheduler_PerClientTriggering(t *testing.T) {
	cfg := config.SchedulerConfig{
		FullBackupCron: "@every 1s",
	}
	clients := []string{"workstation1", "laptop1"}
	trigger := &mockTrigger{}
	s := New(cfg, trigger, clients, nil)

	ctx := context.Background()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("unexpected start error: %v", err)
	}

	time.Sleep(1500 * time.Millisecond)
	s.Stop()

	calls := trigger.getCalls()
	if len(calls) == 0 {
		t.Fatal("expected at least one backup trigger call")
	}

	// Verify that each trigger fires for both clients.
	clientSeen := map[string]bool{}
	for _, c := range calls {
		if c.Level != model.BackupLevelFull {
			t.Errorf("expected level FULL, got %v", c.Level)
		}
		clientSeen[c.ClientID] = true
	}

	for _, id := range clients {
		if !clientSeen[id] {
			t.Errorf("expected trigger for client %q", id)
		}
	}
}

func TestScheduler_EmptyConfig(t *testing.T) {
	// No cron expressions configured — scheduler starts fine, just does nothing.
	cfg := config.SchedulerConfig{}
	trigger := &mockTrigger{}
	s := New(cfg, trigger, nil, nil)

	ctx := context.Background()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("unexpected start error with empty config: %v", err)
	}

	time.Sleep(100 * time.Millisecond)
	s.Stop()

	calls := trigger.getCalls()
	if len(calls) != 0 {
		t.Errorf("expected no trigger calls with empty config, got %d", len(calls))
	}
}
