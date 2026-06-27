package grpc

import (
	"context"
	"io"
	"log/slog"
	"time"

	"github.com/gcclinux/tergum/internal/grpc/proto"
	"github.com/gcclinux/tergum/internal/observe"
)

// TunnelClientConfig configures the client-side command tunnel.
type TunnelClientConfig struct {
	// ServerClient is the connection to the server used to open the tunnel stream.
	ServerClient *TergumClient

	// ClientID identifies this client to the server.
	ClientID string

	// Handler processes incoming commands from the server.
	Handler proto.CommandServiceServer

	// ReconnectInterval is how long to wait before reconnecting after a failure.
	// Defaults to 5 seconds.
	ReconnectInterval time.Duration
}

// StartTunnel opens a bidirectional command tunnel to the server and processes
// commands received over it. It reconnects automatically on failure.
// Blocks until ctx is cancelled.
func StartTunnel(ctx context.Context, cfg TunnelClientConfig) {
	log := observe.Logger("tunnel")

	reconnectInterval := cfg.ReconnectInterval
	if reconnectInterval <= 0 {
		reconnectInterval = 5 * time.Second
	}

	for {
		select {
		case <-ctx.Done():
			log.Info("tunnel client stopped", "client_id", cfg.ClientID)
			return
		default:
		}

		err := runTunnelSession(ctx, cfg, log)
		if err != nil && ctx.Err() == nil {
			log.Warn("tunnel session ended, reconnecting",
				"client_id", cfg.ClientID,
				"error", err,
				"reconnect_in", reconnectInterval,
			)
		}

		// Wait before reconnecting.
		select {
		case <-ctx.Done():
			return
		case <-time.After(reconnectInterval):
		}
	}
}

// runTunnelSession runs a single tunnel session. Returns when the stream ends or errors.
func runTunnelSession(ctx context.Context, cfg TunnelClientConfig, log *slog.Logger) error {
	stream, err := cfg.ServerClient.CommandTunnel(ctx)
	if err != nil {
		return err
	}

	// Send registration as the first response message (identifies us).
	regResp := &proto.TunnelResponse{
		RequestId: "__register__",
		PingResponse: &proto.PingResponse{
			Version: cfg.ClientID, // Reuse PingResponse to carry clientID in registration
		},
	}
	if err := stream.Send(regResp); err != nil {
		return err
	}

	log.Info("tunnel session established", "client_id", cfg.ClientID)

	for {
		cmd, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}

		// Process the command and send a response.
		resp := handleTunnelCommand(ctx, cfg.Handler, cmd, log)
		resp.RequestId = cmd.RequestId

		if sendErr := stream.Send(resp); sendErr != nil {
			return sendErr
		}
	}
}

// handleTunnelCommand dispatches a tunnel command to the appropriate handler method.
func handleTunnelCommand(ctx context.Context, handler proto.CommandServiceServer, cmd *proto.TunnelCommand, log *slog.Logger) *proto.TunnelResponse {
	resp := &proto.TunnelResponse{}

	switch {
	case cmd.PingRequest != nil:
		r, err := handler.Ping(ctx, cmd.PingRequest)
		if err != nil {
			resp.Error = err.Error()
		} else {
			resp.PingResponse = r
		}

	case cmd.TriggerBackup != nil:
		r, err := handler.TriggerBackup(ctx, cmd.TriggerBackup)
		if err != nil {
			resp.Error = err.Error()
		} else {
			resp.BackupResponse = r
		}

	case cmd.StopBackup != nil:
		r, err := handler.StopBackup(ctx, cmd.StopBackup)
		if err != nil {
			resp.Error = err.Error()
		} else {
			resp.StopResponse = r
		}

	case cmd.GetStatus != nil:
		r, err := handler.GetStatus(ctx, cmd.GetStatus)
		if err != nil {
			resp.Error = err.Error()
		} else {
			resp.StatusResponse = r
		}

	case cmd.StartWatcher != nil:
		r, err := handler.StartWatcher(ctx, cmd.StartWatcher)
		if err != nil {
			resp.Error = err.Error()
		} else {
			resp.WatcherResponse = r
		}

	case cmd.StopWatcher != nil:
		r, err := handler.StopWatcher(ctx, cmd.StopWatcher)
		if err != nil {
			resp.Error = err.Error()
		} else {
			resp.WatcherResponse = r
		}

	default:
		log.Warn("unknown tunnel command received", "request_id", cmd.RequestId)
		resp.Error = "unknown command"
	}

	return resp
}
