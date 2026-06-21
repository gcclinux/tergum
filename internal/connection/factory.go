package connection

import (
	"context"
	"fmt"

	"github.com/ricardopadilha/tergum/internal/backup"
	"github.com/ricardopadilha/tergum/internal/config"
	tergumgrpc "github.com/ricardopadilha/tergum/internal/grpc"
	"github.com/ricardopadilha/tergum/internal/restore"
)

// NewServerConnection creates the appropriate ServerConnection based on the node role.
//   - role "client" → connects to the remote server via gRPC and returns a RemoteServerConnection
//   - role "both"   → returns a LocalServerConnection using the local CAS storage directory
//   - role "server" → returns an error (server nodes do not initiate backups)
func NewServerConnection(cfg *config.Config) (backup.ServerConnection, error) {
	switch cfg.Node.Role {
	case "client":
		if cfg.Server.Address == "" {
			return nil, fmt.Errorf("server.address is required when node.role is \"client\"")
		}

		tlsCfg, clientID, err := LoadClientTLS(cfg)
		if err != nil {
			return nil, fmt.Errorf("loading client TLS credentials: %w", err)
		}

		client, err := tergumgrpc.Connect(
			context.Background(),
			cfg.Server.Address,
			cfg.Server.CommandPort,
			cfg.Server.DataPort,
			tlsCfg,
		)
		if err != nil {
			return nil, fmt.Errorf("connecting to server: %w", err)
		}

		return tergumgrpc.NewRemoteServerConnection(client.DataClient(), clientID), nil

	case "both":
		return &backup.LocalServerConnection{
			StorageDir: cfg.StorageDir(),
		}, nil

	case "server":
		return nil, fmt.Errorf("server nodes do not initiate backups")

	default:
		return nil, fmt.Errorf("unknown node role: %q", cfg.Node.Role)
	}
}

// NewDataSource creates the appropriate DataSource for restore operations based on the node role.
//   - role "client" → connects to the remote server via gRPC and returns a RemoteDataSource
//   - role "both"   → returns a LocalDataSource using the local CAS storage directory
//   - role "server" → returns an error (server nodes do not initiate restores)
func NewDataSource(cfg *config.Config) (restore.DataSource, error) {
	switch cfg.Node.Role {
	case "client":
		if cfg.Server.Address == "" {
			return nil, fmt.Errorf("server.address is required when node.role is \"client\"")
		}

		tlsCfg, clientID, err := LoadClientTLS(cfg)
		if err != nil {
			return nil, fmt.Errorf("loading client TLS credentials: %w", err)
		}

		client, err := tergumgrpc.Connect(
			context.Background(),
			cfg.Server.Address,
			cfg.Server.CommandPort,
			cfg.Server.DataPort,
			tlsCfg,
		)
		if err != nil {
			return nil, fmt.Errorf("connecting to server: %w", err)
		}

		return tergumgrpc.NewRemoteDataSource(client.DataClient(), clientID), nil

	case "both":
		return &restore.LocalDataSource{
			StorageDir: cfg.StorageDir(),
		}, nil

	case "server":
		return nil, fmt.Errorf("server nodes do not initiate restores")

	default:
		return nil, fmt.Errorf("unknown node role: %q", cfg.Node.Role)
	}
}
