package grpc

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/gcclinux/tergum/internal/grpc/proto"
)

// TunnelHub manages active command tunnels from NAT clients. It allows the
// server to dispatch commands to clients that maintain an outbound tunnel
// stream rather than accepting inbound connections.
type TunnelHub struct {
	mu      sync.RWMutex
	tunnels map[string]*clientTunnel // keyed by clientID
	logger  *slog.Logger
}

// clientTunnel represents a single active tunnel to a client.
type clientTunnel struct {
	clientID string
	stream   proto.CommandService_CommandTunnelServer

	mu       sync.Mutex
	pending  map[string]chan *proto.TunnelResponse // pending request/response pairs
	seqID    int64
}

// NewTunnelHub creates a new TunnelHub instance.
func NewTunnelHub(logger *slog.Logger) *TunnelHub {
	if logger == nil {
		logger = slog.Default()
	}
	return &TunnelHub{
		tunnels: make(map[string]*clientTunnel),
		logger:  logger,
	}
}

// Register adds a tunnel for the given client. If a previous tunnel exists for
// the same clientID, it is replaced (the old stream will be closed by gRPC
// when the handler returns).
func (h *TunnelHub) Register(clientID string, stream proto.CommandService_CommandTunnelServer) *clientTunnel {
	h.mu.Lock()
	defer h.mu.Unlock()

	t := &clientTunnel{
		clientID: clientID,
		stream:   stream,
		pending:  make(map[string]chan *proto.TunnelResponse),
	}
	h.tunnels[clientID] = t
	h.logger.Info("tunnel registered", "client_id", clientID)
	return t
}

// Unregister removes a tunnel for the given client.
func (h *TunnelHub) Unregister(clientID string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if t, ok := h.tunnels[clientID]; ok {
		// Cancel all pending requests.
		t.mu.Lock()
		for _, ch := range t.pending {
			close(ch)
		}
		t.pending = make(map[string]chan *proto.TunnelResponse)
		t.mu.Unlock()

		delete(h.tunnels, clientID)
		h.logger.Info("tunnel unregistered", "client_id", clientID)
	}
}

// HasTunnel reports whether a tunnel is active for the given client.
func (h *TunnelHub) HasTunnel(clientID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	_, ok := h.tunnels[clientID]
	return ok
}

// SendCommand dispatches a command to the client via tunnel and waits for the response.
// Returns an error if no tunnel is active or the command times out.
func (h *TunnelHub) SendCommand(ctx context.Context, clientID string, cmd *proto.TunnelCommand, timeout time.Duration) (*proto.TunnelResponse, error) {
	h.mu.RLock()
	t, ok := h.tunnels[clientID]
	h.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("no active tunnel for client %q", clientID)
	}

	// Assign a request ID.
	t.mu.Lock()
	t.seqID++
	requestID := fmt.Sprintf("%s-%d", clientID, t.seqID)
	cmd.RequestId = requestID

	// Create response channel.
	respCh := make(chan *proto.TunnelResponse, 1)
	t.pending[requestID] = respCh
	t.mu.Unlock()

	// Send the command over the stream.
	if err := t.stream.Send(cmd); err != nil {
		t.mu.Lock()
		delete(t.pending, requestID)
		t.mu.Unlock()
		return nil, fmt.Errorf("send tunnel command to %s: %w", clientID, err)
	}

	// Wait for response or timeout.
	if timeout <= 0 {
		timeout = 60 * time.Second
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case resp, ok := <-respCh:
		// Response arrived normally — clean up and return.
		t.mu.Lock()
		delete(t.pending, requestID)
		t.mu.Unlock()
		if !ok {
			return nil, fmt.Errorf("tunnel closed for client %q", clientID)
		}
		return resp, nil
	case <-timer.C:
		// Command timed out — clean up immediately.
		t.mu.Lock()
		delete(t.pending, requestID)
		t.mu.Unlock()
		return nil, fmt.Errorf("tunnel command timed out for client %q (request %s)", clientID, requestID)
	case <-ctx.Done():
		// Context was cancelled (e.g. short HTTP timeout). The client may
		// still respond shortly, so delay removal of the pending entry to
		// avoid spurious "received tunnel response for unknown request"
		// warnings. A goroutine drains the late response or cleans up after
		// a grace period.
		go func() {
			grace := time.NewTimer(30 * time.Second)
			defer grace.Stop()
			select {
			case <-respCh:
				// Late response arrived — consumed and discarded.
			case <-grace.C:
				// Grace period expired — clean up.
			}
			t.mu.Lock()
			delete(t.pending, requestID)
			t.mu.Unlock()
		}()
		return nil, ctx.Err()
	}
}

// DeliverResponse routes an incoming response from the client to the waiting caller.
// Called by the CommandTunnel stream handler when it receives a TunnelResponse.
func (h *TunnelHub) DeliverResponse(clientID string, resp *proto.TunnelResponse) {
	h.mu.RLock()
	t, ok := h.tunnels[clientID]
	h.mu.RUnlock()

	if !ok {
		h.logger.Warn("received tunnel response for unknown client", "client_id", clientID, "request_id", resp.RequestId)
		return
	}

	t.mu.Lock()
	ch, exists := t.pending[resp.RequestId]
	t.mu.Unlock()

	if !exists {
		h.logger.Warn("received tunnel response for unknown request",
			"client_id", clientID, "request_id", resp.RequestId)
		return
	}

	// Non-blocking send (channel is buffered with size 1).
	select {
	case ch <- resp:
	default:
		h.logger.Warn("duplicate tunnel response dropped",
			"client_id", clientID, "request_id", resp.RequestId)
	}
}

// Ping sends a Ping command through the tunnel and returns the response.
func (h *TunnelHub) Ping(ctx context.Context, clientID string) (*proto.PingResponse, error) {
	cmd := &proto.TunnelCommand{
		PingRequest: &proto.PingRequest{},
	}
	resp, err := h.SendCommand(ctx, clientID, cmd, 10*time.Second)
	if err != nil {
		return nil, err
	}
	if resp.Error != "" {
		return nil, fmt.Errorf("tunnel ping error: %s", resp.Error)
	}
	return resp.PingResponse, nil
}

// TriggerBackup sends a TriggerBackup command through the tunnel.
func (h *TunnelHub) TriggerBackup(ctx context.Context, clientID string, req *proto.BackupRequest) (*proto.BackupResponse, error) {
	cmd := &proto.TunnelCommand{
		TriggerBackup: req,
	}
	resp, err := h.SendCommand(ctx, clientID, cmd, 30*time.Second)
	if err != nil {
		return nil, err
	}
	if resp.Error != "" {
		return nil, fmt.Errorf("tunnel trigger backup error: %s", resp.Error)
	}
	return resp.BackupResponse, nil
}

// StopBackup sends a StopBackup command through the tunnel.
func (h *TunnelHub) StopBackup(ctx context.Context, clientID string, req *proto.StopRequest) (*proto.StopResponse, error) {
	cmd := &proto.TunnelCommand{
		StopBackup: req,
	}
	resp, err := h.SendCommand(ctx, clientID, cmd, 15*time.Second)
	if err != nil {
		return nil, err
	}
	if resp.Error != "" {
		return nil, fmt.Errorf("tunnel stop backup error: %s", resp.Error)
	}
	return resp.StopResponse, nil
}

// GetStatus sends a GetStatus command through the tunnel.
func (h *TunnelHub) GetStatus(ctx context.Context, clientID string, req *proto.StatusRequest) (*proto.StatusResponse, error) {
	cmd := &proto.TunnelCommand{
		GetStatus: req,
	}
	resp, err := h.SendCommand(ctx, clientID, cmd, 10*time.Second)
	if err != nil {
		return nil, err
	}
	if resp.Error != "" {
		return nil, fmt.Errorf("tunnel get status error: %s", resp.Error)
	}
	return resp.StatusResponse, nil
}

// StartWatcher sends a StartWatcher command through the tunnel.
func (h *TunnelHub) StartWatcher(ctx context.Context, clientID string, req *proto.WatcherRequest) (*proto.WatcherResponse, error) {
	cmd := &proto.TunnelCommand{
		StartWatcher: req,
	}
	resp, err := h.SendCommand(ctx, clientID, cmd, 15*time.Second)
	if err != nil {
		return nil, err
	}
	if resp.Error != "" {
		return nil, fmt.Errorf("tunnel start watcher error: %s", resp.Error)
	}
	return resp.WatcherResponse, nil
}

// StopWatcher sends a StopWatcher command through the tunnel.
func (h *TunnelHub) StopWatcher(ctx context.Context, clientID string, req *proto.WatcherRequest) (*proto.WatcherResponse, error) {
	cmd := &proto.TunnelCommand{
		StopWatcher: req,
	}
	resp, err := h.SendCommand(ctx, clientID, cmd, 15*time.Second)
	if err != nil {
		return nil, err
	}
	if resp.Error != "" {
		return nil, fmt.Errorf("tunnel stop watcher error: %s", resp.Error)
	}
	return resp.WatcherResponse, nil
}
