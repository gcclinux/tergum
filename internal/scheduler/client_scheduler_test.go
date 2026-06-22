package scheduler

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/gcclinux/tergum/internal/model"
	"github.com/gcclinux/tergum/internal/registry"
)

// mockRegistry implements ClientRegistry for testing.
type mockRegistry struct {
	mu      sync.Mutex
	clients map[string]*registry.ClientInfo
	missed  map[string][]registry.MissedBackup
}

func newMockRegistry() *mockRegistry {
	return &mockRegistry{
		clients: make(map[string]*registry.ClientInfo),
		missed:  make(map[string][]registry.MissedBackup),
	}
}

func (m *mockRegistry) addClient(ci *registry.ClientInfo) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.clients[ci.ClientID] = ci
}

func (m *mockRegistry) ListClients() []registry.ClientInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []registry.ClientInfo
	for _, ci := range m.clients {
		copy := *ci
		if ci.Schedule != nil {
			s := *ci.Schedule
			copy.Schedule = &s
		}
		result = append(result, copy)
	}
	return result
}

func (m *mockRegistry) GetClient(clientID string) *registry.ClientInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	ci, exists := m.clients[clientID]
	if !exists {
		return nil
	}
	copy := *ci
	if ci.Schedule != nil {
		s := *ci.Schedule
		copy.Schedule = &s
	}
	return &copy
}

func (m *mockRegistry) RecordMissedBackup(clientID, level string, scheduledAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.missed[clientID] = append(m.missed[clientID], registry.MissedBackup{
		ClientID:    clientID,
		Level:       level,
		ScheduledAt: scheduledAt,
	})
	return nil
}

func (m *mockRegistry) ResolveMissedBackups(clientID string) ([]registry.MissedBackup, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	missed := m.missed[clientID]
	m.missed[clientID] = nil
	return missed, nil
}

func (m *mockRegistry) SetSchedule(clientID string, schedule registry.ScheduleConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	ci, exists := m.clients[clientID]
	if !exists {
		return nil
	}
	ci.Schedule = &schedule
	return nil
}

func (m *mockRegistry) getMissed(clientID string) []registry.MissedBackup {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]registry.MissedBackup, len(m.missed[clientID]))
	copy(result, m.missed[clientID])
	return result
}

func TestClientScheduler_OnlineClientTriggered(t *testing.T) {
	reg := newMockRegistry()
	reg.addClient(&registry.ClientInfo{
		ClientID: "workstation1",
		Status:   "online",
		Schedule: &registry.ScheduleConfig{
			AutoBackupCron: "@every 1s",
		},
	})

	trigger := &mockTrigger{}
	cs := NewClientScheduler(ClientSchedulerConfig{
		Registry: reg,
		Trigger:  trigger,
	})

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- cs.Start(ctx)
	}()

	// Wait for at least one trigger.
	time.Sleep(1500 * time.Millisecond)
	cancel()
	<-errCh

	calls := trigger.getCalls()
	if len(calls) == 0 {
		t.Fatal("expected at least one backup trigger call for online client")
	}

	for _, c := range calls {
		if c.Level != model.BackupLevelAuto {
			t.Errorf("expected level AUTO, got %v", c.Level)
		}
		if c.ClientID != "workstation1" {
			t.Errorf("expected clientID 'workstation1', got %q", c.ClientID)
		}
	}
}

func TestClientScheduler_OfflineClientRecordsMissed(t *testing.T) {
	reg := newMockRegistry()
	reg.addClient(&registry.ClientInfo{
		ClientID: "laptop1",
		Status:   "offline",
		Schedule: &registry.ScheduleConfig{
			FullBackupCron: "@every 1s",
		},
	})

	trigger := &mockTrigger{}
	cs := NewClientScheduler(ClientSchedulerConfig{
		Registry: reg,
		Trigger:  trigger,
	})

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- cs.Start(ctx)
	}()

	time.Sleep(1500 * time.Millisecond)
	cancel()
	<-errCh

	// Trigger should NOT have been called (client is offline).
	calls := trigger.getCalls()
	if len(calls) != 0 {
		t.Errorf("expected no trigger calls for offline client, got %d", len(calls))
	}

	// Missed backups should have been recorded.
	missed := reg.getMissed("laptop1")
	if len(missed) == 0 {
		t.Fatal("expected at least one missed backup to be recorded")
	}
	for _, mb := range missed {
		if mb.Level != "FULL" {
			t.Errorf("expected missed backup level FULL, got %q", mb.Level)
		}
	}
}

func TestClientScheduler_HandleReconnect(t *testing.T) {
	reg := newMockRegistry()
	reg.addClient(&registry.ClientInfo{
		ClientID: "laptop1",
		Status:   "online",
		Schedule: &registry.ScheduleConfig{
			FullBackupCron: "0 0 31 2 *", // never fires
		},
	})

	// Pre-load a missed backup.
	reg.mu.Lock()
	reg.missed["laptop1"] = []registry.MissedBackup{
		{ClientID: "laptop1", Level: "FULL", ScheduledAt: time.Now().Add(-1 * time.Hour)},
	}
	reg.mu.Unlock()

	trigger := &mockTrigger{}
	cs := NewClientScheduler(ClientSchedulerConfig{
		Registry:       reg,
		Trigger:        trigger,
		ReconnectGrace: 50 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- cs.Start(ctx)
	}()

	// Allow scheduler to start.
	time.Sleep(100 * time.Millisecond)

	// Simulate reconnection.
	cs.HandleReconnect("laptop1")

	// Wait for grace period + processing.
	time.Sleep(200 * time.Millisecond)
	cancel()
	<-errCh

	calls := trigger.getCalls()
	if len(calls) == 0 {
		t.Fatal("expected missed backup to be triggered on reconnect")
	}
	if calls[0].ClientID != "laptop1" {
		t.Errorf("expected clientID 'laptop1', got %q", calls[0].ClientID)
	}
	if calls[0].Level != model.BackupLevelFull {
		t.Errorf("expected level FULL, got %v", calls[0].Level)
	}
}

func TestClientScheduler_SetClientSchedule(t *testing.T) {
	reg := newMockRegistry()
	reg.addClient(&registry.ClientInfo{
		ClientID: "workstation1",
		Status:   "online",
	})

	trigger := &mockTrigger{}
	cs := NewClientScheduler(ClientSchedulerConfig{
		Registry: reg,
		Trigger:  trigger,
	})

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- cs.Start(ctx)
	}()

	// Allow scheduler to start.
	time.Sleep(100 * time.Millisecond)

	// Dynamically add a schedule.
	if err := cs.SetClientSchedule("workstation1", "", "@every 1s"); err != nil {
		t.Fatalf("SetClientSchedule failed: %v", err)
	}

	// Wait for trigger to fire.
	time.Sleep(1500 * time.Millisecond)
	cancel()
	<-errCh

	calls := trigger.getCalls()
	if len(calls) == 0 {
		t.Fatal("expected trigger after SetClientSchedule")
	}
	if calls[0].Level != model.BackupLevelAuto {
		t.Errorf("expected level AUTO, got %v", calls[0].Level)
	}
}

func TestClientScheduler_NoScheduleNoTrigger(t *testing.T) {
	reg := newMockRegistry()
	reg.addClient(&registry.ClientInfo{
		ClientID: "workstation1",
		Status:   "online",
		// No schedule configured.
	})

	trigger := &mockTrigger{}
	cs := NewClientScheduler(ClientSchedulerConfig{
		Registry: reg,
		Trigger:  trigger,
	})

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- cs.Start(ctx)
	}()

	time.Sleep(200 * time.Millisecond)
	cancel()
	<-errCh

	calls := trigger.getCalls()
	if len(calls) != 0 {
		t.Errorf("expected no trigger calls for client without schedule, got %d", len(calls))
	}
}

func TestClientScheduler_StopBeforeStart(t *testing.T) {
	reg := newMockRegistry()
	trigger := &mockTrigger{}
	cs := NewClientScheduler(ClientSchedulerConfig{
		Registry: reg,
		Trigger:  trigger,
	})

	// Stop before Start should be a no-op.
	cs.Stop()
}

func TestMissedBackupLevel(t *testing.T) {
	tests := []struct {
		input    string
		expected model.BackupLevel
	}{
		{"FULL", model.BackupLevelFull},
		{"AUTO", model.BackupLevelAuto},
		{"ONGOING", model.BackupLevelOngoing},
		{"unknown", model.BackupLevelAuto},
	}

	for _, tt := range tests {
		got := missedBackupLevel(tt.input)
		if got != tt.expected {
			t.Errorf("missedBackupLevel(%q) = %v, want %v", tt.input, got, tt.expected)
		}
	}
}
