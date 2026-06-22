package grpc

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gcclinux/tergum/internal/grpc/proto"
	"google.golang.org/grpc"
)

// --- Heartbeat Tests ---

func TestStartHeartbeat_PingsAndRegisters(t *testing.T) {
	mock := &mockCommandForHeartbeat{}
	client := NewTergumClient(mock, mock, DefaultClientConfig())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		StartHeartbeat(ctx, client, "test-client", "192.168.1.10:7400", 10*time.Millisecond)
		close(done)
	}()

	// Wait for at least one ping and registration.
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done

	if mock.pingCount.Load() == 0 {
		t.Error("expected at least one ping call")
	}
	if mock.registerCount.Load() != 1 {
		t.Errorf("expected exactly 1 register call, got %d", mock.registerCount.Load())
	}

	// Verify registration details.
	mock.mu.Lock()
	defer mock.mu.Unlock()
	if mock.lastRegisterClientID != "test-client" {
		t.Errorf("registered clientID = %q, want %q", mock.lastRegisterClientID, "test-client")
	}
	if mock.lastRegisterAddress != "192.168.1.10:7400" {
		t.Errorf("registered address = %q, want %q", mock.lastRegisterAddress, "192.168.1.10:7400")
	}
}

func TestStartHeartbeat_RegistersOnlyOnce(t *testing.T) {
	mock := &mockCommandForHeartbeat{}
	client := NewTergumClient(mock, mock, DefaultClientConfig())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		StartHeartbeat(ctx, client, "test-client", "10.0.0.1:7400", 10*time.Millisecond)
		close(done)
	}()

	// Wait for multiple ticks.
	time.Sleep(80 * time.Millisecond)
	cancel()
	<-done

	if mock.pingCount.Load() < 2 {
		t.Error("expected at least 2 pings")
	}
	if mock.registerCount.Load() != 1 {
		t.Errorf("expected exactly 1 register call, got %d", mock.registerCount.Load())
	}
}

func TestStartHeartbeat_StopsOnContextCancel(t *testing.T) {
	mock := &mockCommandForHeartbeat{}
	client := NewTergumClient(mock, mock, DefaultClientConfig())

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		StartHeartbeat(ctx, client, "test-client", "10.0.0.1:7400", 10*time.Millisecond)
		close(done)
	}()

	// Cancel immediately after a short delay.
	time.Sleep(5 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// Good, function returned.
	case <-time.After(500 * time.Millisecond):
		t.Fatal("StartHeartbeat did not exit after context cancel")
	}
}

func TestStartHeartbeat_ContinuesOnPingFailure(t *testing.T) {
	// Use failPingUntil=2 so the first call's internal retry succeeds on attempt 2.
	// Then verify we still get registration after recovery.
	mock := &mockCommandForHeartbeat{
		failPingUntil: 2, // First ping attempt fails, second (retry within same call) succeeds.
	}
	// Use a config with short backoff so retries happen quickly in tests.
	cfg := ClientConfig{
		InitialBackoff: 1 * time.Millisecond,
		MaxBackoff:     5 * time.Millisecond,
		BackoffFactor:  2.0,
		MaxRetries:     5,
	}
	client := NewTergumClient(mock, mock, cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		StartHeartbeat(ctx, client, "test-client", "10.0.0.1:7400", 10*time.Millisecond)
		close(done)
	}()

	// Wait enough time for the heartbeat to tick and ping (with internal retry).
	time.Sleep(80 * time.Millisecond)
	cancel()
	<-done

	// Should have attempted multiple pings (at least the failed one and the successful retry).
	if mock.pingCount.Load() < 2 {
		t.Errorf("expected at least 2 ping attempts, got %d", mock.pingCount.Load())
	}
	// Should eventually register after ping succeeds.
	if mock.registerCount.Load() != 1 {
		t.Errorf("expected 1 register call after successful ping, got %d", mock.registerCount.Load())
	}
}

func TestStartHeartbeat_NoRegisterWhenPingAlwaysFails(t *testing.T) {
	// When ping always fails (exhausts retries), registration should never happen.
	mock := &mockCommandForHeartbeat{
		failPingUntil: 1000, // Never succeeds.
	}
	cfg := ClientConfig{
		InitialBackoff: 1 * time.Millisecond,
		MaxBackoff:     2 * time.Millisecond,
		BackoffFactor:  1.5,
		MaxRetries:     1, // Only 1 retry so it fails fast per tick.
	}
	client := NewTergumClient(mock, mock, cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		StartHeartbeat(ctx, client, "test-client", "10.0.0.1:7400", 10*time.Millisecond)
		close(done)
	}()

	// Let several ticks elapse.
	time.Sleep(60 * time.Millisecond)
	cancel()
	<-done

	if mock.registerCount.Load() != 0 {
		t.Errorf("expected 0 register calls when ping always fails, got %d", mock.registerCount.Load())
	}
}

// --- Mock for heartbeat tests ---

type mockCommandForHeartbeat struct {
	grpc.ClientConnInterface

	pingCount      atomic.Int64
	registerCount  atomic.Int64
	failPingUntil  int64 // Ping fails until pingCount reaches this value.

	mu                    sync.Mutex
	lastRegisterClientID  string
	lastRegisterAddress   string
}

func (m *mockCommandForHeartbeat) Invoke(ctx context.Context, method string, args any, reply any, opts ...grpc.CallOption) error {
	switch method {
	case "/tergum.v3.CommandService/Ping":
		count := m.pingCount.Add(1)
		if count < m.failPingUntil {
			return context.DeadlineExceeded
		}
		if resp, ok := reply.(*proto.PingResponse); ok {
			resp.Version = "3.0.0"
			resp.Uptime = "1h"
		}
	case "/tergum.v3.CommandService/RegisterClient":
		m.registerCount.Add(1)
		if req, ok := args.(*proto.RegisterRequest); ok {
			m.mu.Lock()
			m.lastRegisterClientID = req.ClientId
			m.lastRegisterAddress = req.Address
			m.mu.Unlock()
		}
		if resp, ok := reply.(*proto.RegisterResponse); ok {
			resp.Success = true
			resp.ServerVersion = "3.0.0"
		}
	}
	return nil
}

func (m *mockCommandForHeartbeat) NewStream(ctx context.Context, desc *grpc.StreamDesc, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
	return nil, nil
}
