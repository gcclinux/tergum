package webui

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSSEBroker_NewBroker(t *testing.T) {
	broker := NewSSEBroker(50)
	if broker == nil {
		t.Fatal("NewSSEBroker() returned nil")
	}
	if broker.maxHistory != 50 {
		t.Errorf("maxHistory = %d, want 50", broker.maxHistory)
	}
}

func TestSSEBroker_DefaultMaxHistory(t *testing.T) {
	broker := NewSSEBroker(0)
	if broker.maxHistory != 100 {
		t.Errorf("maxHistory = %d, want 100 (default)", broker.maxHistory)
	}
}

func TestSSEBroker_Publish(t *testing.T) {
	broker := NewSSEBroker(10)

	ch := broker.subscribe()
	defer broker.unsubscribe(ch)

	event := ActivityEvent{
		ID:      "1",
		Type:    "backup_started",
		Message: "Backup started for client-1",
	}
	broker.Publish(event)

	select {
	case received := <-ch:
		if received.Type != "backup_started" {
			t.Errorf("event type = %q, want %q", received.Type, "backup_started")
		}
		if received.Message != "Backup started for client-1" {
			t.Errorf("message = %q, want %q", received.Message, "Backup started for client-1")
		}
		if received.Timestamp.IsZero() {
			t.Error("timestamp should be set automatically")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for event")
	}
}

func TestSSEBroker_History(t *testing.T) {
	broker := NewSSEBroker(5)

	for i := 0; i < 7; i++ {
		broker.Publish(ActivityEvent{
			Type:    "test",
			Message: "event",
		})
	}

	history := broker.History()
	if len(history) != 5 {
		t.Errorf("history length = %d, want 5 (capped at maxHistory)", len(history))
	}
}

func TestSSEBroker_ClientCount(t *testing.T) {
	broker := NewSSEBroker(10)

	if broker.ClientCount() != 0 {
		t.Errorf("initial client count = %d, want 0", broker.ClientCount())
	}

	ch1 := broker.subscribe()
	ch2 := broker.subscribe()

	if broker.ClientCount() != 2 {
		t.Errorf("client count = %d, want 2", broker.ClientCount())
	}

	broker.unsubscribe(ch1)
	if broker.ClientCount() != 1 {
		t.Errorf("client count after unsubscribe = %d, want 1", broker.ClientCount())
	}

	broker.unsubscribe(ch2)
	if broker.ClientCount() != 0 {
		t.Errorf("client count = %d, want 0", broker.ClientCount())
	}
}

func TestSSEBroker_ServeHTTP(t *testing.T) {
	broker := NewSSEBroker(10)

	// Start a test server.
	ts := httptest.NewServer(broker)
	defer ts.Close()

	// Create a context with timeout to cancel the SSE connection.
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	req := httptest.NewRequest("GET", "/", nil)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()

	// Run in goroutine since ServeHTTP blocks until context cancels.
	done := make(chan struct{})
	go func() {
		broker.ServeHTTP(w, req)
		close(done)
	}()

	// Publish an event while the connection is open.
	time.Sleep(10 * time.Millisecond)
	broker.Publish(ActivityEvent{
		ID:      "1",
		Type:    "test_event",
		Message: "hello",
	})

	// Wait for the handler to finish.
	<-done

	body := w.Body.String()
	if !strings.Contains(body, ": connected") {
		t.Error("expected connection comment in SSE stream")
	}
	if !strings.Contains(body, `"type":"test_event"`) {
		t.Error("expected JSON type field in SSE stream")
	}
	if !strings.Contains(body, `"message":"hello"`) {
		t.Error("expected JSON message field in SSE stream")
	}
	if !strings.Contains(body, "data:") {
		t.Error("expected data field in SSE stream")
	}

	// Verify headers.
	if ct := w.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want %q", ct, "text/event-stream")
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("Cache-Control = %q, want %q", cc, "no-cache")
	}
}

func TestSSEBroker_SlowClient(t *testing.T) {
	broker := NewSSEBroker(10)

	// Subscribe but don't read from channel (simulates slow client).
	ch := broker.subscribe()
	defer broker.unsubscribe(ch)

	// Fill the channel buffer (16).
	for i := 0; i < 20; i++ {
		broker.Publish(ActivityEvent{
			Type:    "test",
			Message: "event",
		})
	}

	// Should not panic or block — events are dropped for slow clients.
	// Drain the channel to verify we got the buffered ones.
	count := 0
	for {
		select {
		case <-ch:
			count++
		default:
			goto done
		}
	}
done:
	if count != 16 {
		t.Errorf("slow client received %d events, expected 16 (buffer size)", count)
	}
}
