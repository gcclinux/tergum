package webui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// ActivityEvent represents an event to be broadcast via SSE.
type ActivityEvent struct {
	ID        string
	Type      string // e.g. "backup_started", "backup_completed", "retention_run", "client_connected"
	Message   string
	Timestamp time.Time
	Resource  string // Affected resource ID (backup_id, client_id, etc.)
}

// ssePayload is the JSON structure sent to SSE clients.
type ssePayload struct {
	ID        string `json:"id,omitempty"`
	Type      string `json:"type"`
	Message   string `json:"message"`
	Timestamp string `json:"timestamp"`
	Resource  string `json:"resource,omitempty"`
}

// SSEBroker manages Server-Sent Events connections and broadcasts events
// to all connected clients.
type SSEBroker struct {
	mu          sync.RWMutex
	clients     map[chan ActivityEvent]struct{}
	history     []ActivityEvent
	maxHistory  int
}

// NewSSEBroker creates a new SSE broker with a bounded event history.
func NewSSEBroker(maxHistory int) *SSEBroker {
	if maxHistory <= 0 {
		maxHistory = 100
	}
	return &SSEBroker{
		clients:    make(map[chan ActivityEvent]struct{}),
		history:    make([]ActivityEvent, 0, maxHistory),
		maxHistory: maxHistory,
	}
}

// Publish sends an event to all connected SSE clients.
func (b *SSEBroker) Publish(event ActivityEvent) {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	b.mu.Lock()
	// Add to history, evict oldest if full.
	if len(b.history) >= b.maxHistory {
		b.history = b.history[1:]
	}
	b.history = append(b.history, event)

	// Snapshot clients under lock.
	clients := make([]chan ActivityEvent, 0, len(b.clients))
	for ch := range b.clients {
		clients = append(clients, ch)
	}
	b.mu.Unlock()

	// Send to all clients (non-blocking to avoid slow client blocking others).
	for _, ch := range clients {
		select {
		case ch <- event:
		default:
			// Client is slow — drop this event for them.
		}
	}
}

// History returns the recent event history.
func (b *SSEBroker) History() []ActivityEvent {
	b.mu.RLock()
	defer b.mu.RUnlock()

	result := make([]ActivityEvent, len(b.history))
	copy(result, b.history)
	return result
}

// subscribe registers a new client channel.
func (b *SSEBroker) subscribe() chan ActivityEvent {
	ch := make(chan ActivityEvent, 16)
	b.mu.Lock()
	b.clients[ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

// unsubscribe removes a client channel.
func (b *SSEBroker) unsubscribe(ch chan ActivityEvent) {
	b.mu.Lock()
	delete(b.clients, ch)
	b.mu.Unlock()
	close(ch)
}

// ClientCount returns the number of connected SSE clients.
func (b *SSEBroker) ClientCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.clients)
}

// ClearByType removes events matching the given type from history.
// This is useful for removing transient progress events after a backup completes.
func (b *SSEBroker) ClearByType(eventType string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	filtered := b.history[:0]
	for _, e := range b.history {
		if e.Type != eventType {
			filtered = append(filtered, e)
		}
	}
	b.history = filtered
}

// ServeHTTP implements the SSE endpoint handler.
// It streams activity events to the client as Server-Sent Events.
func (b *SSEBroker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	ch := b.subscribe()
	defer b.unsubscribe(ch)

	// Send a comment to confirm connection.
	fmt.Fprintf(w, ": connected\n\n")
	flusher.Flush()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-ch:
			if !ok {
				return
			}
			if event.ID != "" {
				fmt.Fprintf(w, "id: %s\n", event.ID)
			}
			payload := ssePayload{
				ID:        event.ID,
				Type:      event.Type,
				Message:   event.Message,
				Timestamp: event.Timestamp.Format(time.RFC3339),
				Resource:  event.Resource,
			}
			jsonData, _ := json.Marshal(payload)
			fmt.Fprintf(w, "data: %s\n\n", jsonData)
			flusher.Flush()
		}
	}
}
