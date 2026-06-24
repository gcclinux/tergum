package webui

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log/slog"
	"time"

	grpcpkg "github.com/gcclinux/tergum/internal/grpc"
	"github.com/gcclinux/tergum/internal/grpc/proto"
	"github.com/gcclinux/tergum/internal/registry"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// RemoteClientConnector implements ClientConnector by connecting to remote
// client nodes via gRPC. It uses the registry to look up client addresses
// and TLS configuration for mTLS authentication.
type RemoteClientConnector struct {
	registry *registry.Registry
	tlsCfg   *tls.Config
	logger   *slog.Logger
}

// RemoteClientConnectorConfig holds configuration for the RemoteClientConnector.
type RemoteClientConnectorConfig struct {
	Registry *registry.Registry
	TLSCfg   *tls.Config
	Logger   *slog.Logger
}

// NewRemoteClientConnector creates a new RemoteClientConnector.
func NewRemoteClientConnector(cfg RemoteClientConnectorConfig) *RemoteClientConnector {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &RemoteClientConnector{
		registry: cfg.Registry,
		tlsCfg:   cfg.TLSCfg,
		logger:   logger,
	}
}

// TriggerClientBackup connects to the client and sends a TriggerBackup RPC.
func (c *RemoteClientConnector) TriggerClientBackup(ctx context.Context, clientID string) error {
	client, err := c.connectToClient(clientID)
	if err != nil {
		return err
	}

	_, err = client.TriggerBackup(ctx, proto.BackupLevel_AUTO, clientID, "webui")
	if err != nil {
		return fmt.Errorf("trigger backup on client %s: %w", clientID, err)
	}
	return nil
}

// StartClientWatcher connects to the client and sends a StartWatcher RPC.
// On success, it updates the registry watcher status.
func (c *RemoteClientConnector) StartClientWatcher(ctx context.Context, clientID string) error {
	client, err := c.connectToClient(clientID)
	if err != nil {
		return err
	}

	resp, err := client.StartWatcher(ctx, clientID)
	if err != nil {
		return fmt.Errorf("start watcher on client %s: %w", clientID, err)
	}

	if !resp.Success {
		return fmt.Errorf("start watcher on client %s: %s", clientID, resp.Message)
	}

	// Update registry watcher status.
	if setErr := c.registry.SetWatcherActive(clientID, true); setErr != nil {
		c.logger.Warn("failed to update watcher status in registry",
			"client_id", clientID, "error", setErr)
	}

	return nil
}

// StopClientWatcher connects to the client and sends a StopWatcher RPC.
// On success, it updates the registry watcher status.
func (c *RemoteClientConnector) StopClientWatcher(ctx context.Context, clientID string) error {
	client, err := c.connectToClient(clientID)
	if err != nil {
		return err
	}

	resp, err := client.StopWatcher(ctx, clientID)
	if err != nil {
		return fmt.Errorf("stop watcher on client %s: %w", clientID, err)
	}

	if !resp.Success {
		return fmt.Errorf("stop watcher on client %s: %s", clientID, resp.Message)
	}

	// Update registry watcher status.
	if setErr := c.registry.SetWatcherActive(clientID, false); setErr != nil {
		c.logger.Warn("failed to update watcher status in registry",
			"client_id", clientID, "error", setErr)
	}

	return nil
}

// GetClientStatus connects to the client and sends a GetStatus RPC.
func (c *RemoteClientConnector) GetClientStatus(ctx context.Context, clientID string) (*ClientStatusInfo, error) {
	client, err := c.connectToClient(clientID)
	if err != nil {
		return nil, err
	}

	resp, err := client.GetStatus(ctx, clientID)
	if err != nil {
		return nil, fmt.Errorf("get status from client %s: %w", clientID, err)
	}

	return &ClientStatusInfo{
		Status:           resp.Status,
		BackupID:         resp.BackupId,
		FilesProcessed:   resp.FilesProcessed,
		BytesTransferred: resp.BytesTransferred,
		StartedAt:        resp.StartedAt,
		Message:          resp.Message,
	}, nil
}

// StopClientBackup connects to the client and sends a StopBackup RPC.
func (c *RemoteClientConnector) StopClientBackup(ctx context.Context, clientID string) error {
	client, err := c.connectToClient(clientID)
	if err != nil {
		return err
	}

	_, err = client.StopBackup(ctx, "", clientID)
	if err != nil {
		return fmt.Errorf("stop backup on client %s: %w", clientID, err)
	}
	return nil
}


// connectToClient looks up the client address in the registry and creates
// a gRPC connection via mTLS. Returns an error if the client is not found
// or is offline.
func (c *RemoteClientConnector) connectToClient(clientID string) (*grpcpkg.TergumClient, error) {
	ci := c.registry.GetClient(clientID)
	if ci == nil {
		return nil, fmt.Errorf("client %q not found in registry", clientID)
	}
	if ci.Status != "online" {
		return nil, fmt.Errorf("client %q is offline", clientID)
	}

	// Connect to the client's command port using mTLS.
	// Since client certificates might be copied from the server and lack IP/hostname SANs
	// matching their dynamic IPs in the registry, we skip hostname validation but
	// strictly verify the CA signature manually.
	tlsCfg := c.tlsCfg.Clone()
	tlsCfg.InsecureSkipVerify = true
	tlsCfg.VerifyConnection = func(cs tls.ConnectionState) error {
		opts := x509.VerifyOptions{
			Roots:         c.tlsCfg.RootCAs,
			CurrentTime:   time.Now(),
			Intermediates: x509.NewCertPool(),
		}
		for _, cert := range cs.PeerCertificates[1:] {
			opts.Intermediates.AddCert(cert)
		}
		_, err := cs.PeerCertificates[0].Verify(opts)
		return err
	}

	creds := credentials.NewTLS(tlsCfg)
	conn, err := grpc.NewClient(ci.Address, grpc.WithTransportCredentials(creds))
	if err != nil {
		return nil, fmt.Errorf("connect to client %s at %s: %w", clientID, ci.Address, err)
	}

	// Create a TergumClient using the command connection only (no data port needed for commands).
	client := grpcpkg.NewTergumClient(conn, conn, grpcpkg.ClientConfig{
		MaxRetries: 2, // fewer retries for interactive web operations
	})

	return client, nil
}

// Ensure RemoteClientConnector satisfies the ClientConnector interface at compile time.
var _ ClientConnector = (*RemoteClientConnector)(nil)
