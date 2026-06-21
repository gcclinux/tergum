package grpc

import (
	"context"
	"log/slog"
	"time"

	"github.com/ricardopadilha/tergum/internal/observe"
)

// StartHeartbeat runs a blocking loop that periodically pings the server and,
// on the first successful ping, registers the client. It exits when ctx is cancelled.
//
// The caller is responsible for running this in a goroutine:
//
//	go grpc.StartHeartbeat(ctx, client, "myhost", "192.168.1.10:7400", 30*time.Second)
func StartHeartbeat(ctx context.Context, client *TergumClient, clientID string, address string, interval time.Duration) {
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
			_, err := client.Ping(ctx)
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
