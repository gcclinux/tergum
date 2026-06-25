package grpc

import (
	"context"
	"testing"

	"github.com/gcclinux/tergum/internal/config"
	"github.com/gcclinux/tergum/internal/grpc/proto"
	"github.com/gcclinux/tergum/internal/scheduler"
	"github.com/gcclinux/tergum/internal/watcher"
)

type mockTestWatcher struct {
	startCalled bool
	stopCalled  bool
	running     bool
}

func (m *mockTestWatcher) Start(ctx context.Context) error {
	m.startCalled = true
	m.running = true
	return nil
}

func (m *mockTestWatcher) Stop() error {
	m.stopCalled = true
	m.running = false
	return nil
}

func (m *mockTestWatcher) StableFiles() <-chan watcher.StableFile {
	return make(chan watcher.StableFile)
}

func (m *mockTestWatcher) Status() watcher.WatcherStatus {
	return watcher.WatcherStatus{
		Running: m.running,
	}
}

func TestClientCommandServer_WatcherControl(t *testing.T) {
	// 1. StartWatcher with no factory and no watcher should return an error.
	s1 := NewClientCommandServer(ClientCommandServerConfig{
		Cfg: &config.Config{},
	})
	resp1, err := s1.StartWatcher(context.Background(), &proto.WatcherRequest{ClientId: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp1.Success {
		t.Error("expected failure when no watcher or factory is configured")
	}

	// 2. StartWatcher with on-demand factory.
	mw := &mockTestWatcher{}
	factoryCalled := false
	factory := func(ctx context.Context) (watcher.Watcher, *scheduler.OngoingBackup, error) {
		factoryCalled = true
		_ = mw.Start(ctx)
		// We can return nil for ongoing backup since we won't test full streaming here.
		return mw, nil, nil
	}

	s2 := NewClientCommandServer(ClientCommandServerConfig{
		Cfg:            &config.Config{},
		WatcherFactory: factory,
	})

	resp2, err := s2.StartWatcher(context.Background(), &proto.WatcherRequest{ClientId: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp2.Success {
		t.Errorf("expected success starting watcher on-demand, got message: %s", resp2.Message)
	}
	if !factoryCalled {
		t.Error("expected watcher factory to be called")
	}
	if !mw.running {
		t.Error("expected watcher to be running")
	}

	// 3. StartWatcher again when already running should return success immediately.
	factoryCalled = false
	resp3, err := s2.StartWatcher(context.Background(), &proto.WatcherRequest{ClientId: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp3.Success || resp3.Message != "watcher is already running" {
		t.Errorf("expected watcher is already running, got: %+v", resp3)
	}
	if factoryCalled {
		t.Error("factory should not be called again if watcher is already running")
	}

	// 4. StopWatcher should stop the running watcher.
	resp4, err := s2.StopWatcher(context.Background(), &proto.WatcherRequest{ClientId: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp4.Success {
		t.Errorf("expected success stopping watcher, got message: %s", resp4.Message)
	}
	if mw.running {
		t.Error("expected watcher to be stopped")
	}
	if !mw.stopCalled {
		t.Error("expected Stop to have been called on the watcher")
	}
}
