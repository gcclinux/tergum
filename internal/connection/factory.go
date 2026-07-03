package connection

import (
	"context"
	"fmt"

	"github.com/gcclinux/tergum/internal/backup"
	"github.com/gcclinux/tergum/internal/config"
	tergumgrpc "github.com/gcclinux/tergum/internal/grpc"
	"github.com/gcclinux/tergum/internal/restore"
)

// CheckClientEnabled connects to the server and asks whether this client is
// disabled. It returns nil if the client is enabled (or the role is not
// "client"), and a descriptive error if the server reports the client as
// disabled. Network/TLS errors are returned as-is so the caller can decide
// whether to proceed or abort.
func CheckClientEnabled(cfg *config.Config) error {
	if cfg.Node.Role != "client" {
		// Hybrid/local nodes don't have a server-side disabled flag.
		return nil
	}

	if cfg.Server.Address == "" {
		return fmt.Errorf("server.address is required when node.role is \"client\"")
	}

	tlsCfg, _, err := LoadClientTLS(cfg)
	if err != nil {
		return fmt.Errorf("loading client TLS credentials: %w", err)
	}

	client, err := tergumgrpc.Connect(
		context.Background(),
		cfg.Server.Address,
		cfg.Server.CommandPort,
		cfg.Server.DataPort,
		tlsCfg,
	)
	if err != nil {
		return fmt.Errorf("connecting to server: %w", err)
	}

	resp, err := client.Ping(context.Background())
	if err != nil {
		return fmt.Errorf("server ping failed: %w", err)
	}

	if resp.ClientDisabled {
		return fmt.Errorf("this client has been disabled on the server — re-enable it from the server before running backup or restore")
	}

	return nil
}

// NewServerConnection creates the appropriate ServerConnection based on the node role.
//   - role "client" → connects to the remote server via gRPC and returns a RemoteServerConnection
//   - role "hybrid" → returns a LocalServerConnection using the local CAS storage directory
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

	case "hybrid":
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
//   - role "hybrid" → returns a LocalDataSource using the local CAS storage directory
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

	case "hybrid":
		return &restore.LocalDataSource{
			StorageDir: cfg.StorageDir(),
		}, nil

	case "server":
		return nil, fmt.Errorf("server nodes do not initiate restores")

	default:
		return nil, fmt.Errorf("unknown node role: %q", cfg.Node.Role)
	}
}
