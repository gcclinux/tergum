package grpc

import (
	"context"
	"log/slog"
	"time"

	"github.com/gcclinux/tergum/internal/grpc/proto"
	"github.com/gcclinux/tergum/internal/observe"
)

// HeartbeatStateProvider supplies client state for heartbeat sync.
// Implementations report the current watcher and backup status so the server
// registry stays consistent without separate polling.
type HeartbeatStateProvider interface {
	// WatcherRunning returns true if the file watcher is currently active.
	WatcherRunning() bool
	// LastBackupTime returns the RFC3339 timestamp of the most recent completed backup, or "".
	LastBackupTime() string
	// BackupRunning returns true if a backup operation is currently in progress.
	BackupRunning() bool
}

// StartHeartbeat runs a blocking loop that periodically pings the server and,
// on the first successful ping, registers the client. It exits when ctx is cancelled.
// If stateProvider is non-nil, each heartbeat includes current client state
// (watcher status, last backup) for server-side registry sync.
//
// The caller is responsible for running this in a goroutine:
//
//	go grpc.StartHeartbeat(ctx, client, "myhost", "192.168.1.10:7400", 30*time.Second, stateProvider)
func StartHeartbeat(ctx context.Context, client *TergumClient, clientID string, address string, interval time.Duration, stateProvider HeartbeatStateProvider) {
	log := observe.Logger("heartbeat")
	registered := false

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	log.Info("heartbeat loop started",
		slog.String("client_id", clientID),
		slog.Duration("interval", interval),
	)

	for {
		select {
		case <-ctx.Done():
			log.Info("heartbeat loop stopped", slog.String("client_id", clientID))
			return
		case <-ticker.C:
			// Build ping request with current client state.
			req := proto.PingRequest{}
			if stateProvider != nil {
				req.WatcherActive = stateProvider.WatcherRunning()
				req.LastBackupAt = stateProvider.LastBackupTime()
				req.BackupActive = stateProvider.BackupRunning()
			}

			_, err := client.PingWithState(ctx, req)
			if err != nil {
				log.Warn("heartbeat ping failed",
					slog.String("client_id", clientID),
					slog.String("error", err.Error()),
				)
				continue
			}

			if !registered {
				_, regErr := client.RegisterClient(ctx, clientID, address)
				if regErr != nil {
					log.Warn("client registration failed",
						slog.String("client_id", clientID),
						slog.String("error", regErr.Error()),
					)
				} else {
					registered = true
					log.Info("client registered with server",
						slog.String("client_id", clientID),
						slog.String("address", address),
					)
				}
			}
		}
	}
}
