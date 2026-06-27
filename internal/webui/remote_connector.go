package webui

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log/slog"
	"strings"
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
// When a client has an active command tunnel (NAT mode), commands are dispatched
// via the tunnel instead of dialing back to the client.
type RemoteClientConnector struct {
	registry  *registry.Registry
	tlsCfg    *tls.Config
	tunnelHub *grpcpkg.TunnelHub
	logger    *slog.Logger
}

// RemoteClientConnectorConfig holds configuration for the RemoteClientConnector.
type RemoteClientConnectorConfig struct {
	Registry  *registry.Registry
	TLSCfg   *tls.Config
	TunnelHub *grpcpkg.TunnelHub // optional; enables NAT tunnel dispatch
	Logger    *slog.Logger
}

// NewRemoteClientConnector creates a new RemoteClientConnector.
func NewRemoteClientConnector(cfg RemoteClientConnectorConfig) *RemoteClientConnector {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &RemoteClientConnector{
		registry:  cfg.Registry,
		tlsCfg:    cfg.TLSCfg,
		tunnelHub: cfg.TunnelHub,
		logger:    logger,
	}
}

// TriggerClientBackup connects to the client and sends a TriggerBackup RPC.
func (c *RemoteClientConnector) TriggerClientBackup(ctx context.Context, clientID string) error {
	// Try tunnel first if available.
	if c.tunnelHub != nil && c.tunnelHub.HasTunnel(clientID) {
		_, err := c.tunnelHub.TriggerBackup(ctx, clientID, &proto.BackupRequest{
			Level:       proto.BackupLevel_AUTO,
			ClientId:    clientID,
			InitiatedBy: "webui",
		})
		return err
	}

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
	// Try tunnel first if available.
	if c.tunnelHub != nil && c.tunnelHub.HasTunnel(clientID) {
		resp, err := c.tunnelHub.StartWatcher(ctx, clientID, &proto.WatcherRequest{ClientId: clientID})
		if err != nil {
			return fmt.Errorf("start watcher on client %s via tunnel: %w", clientID, err)
		}
		if !resp.Success {
			return fmt.Errorf("start watcher on client %s: %s", clientID, resp.Message)
		}
		if setErr := c.registry.SetWatcherActive(clientID, true); setErr != nil {
			c.logger.Warn("failed to update watcher status in registry",
				"client_id", clientID, "error", setErr)
		}
		return nil
	}

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
	// Try tunnel first if available.
	if c.tunnelHub != nil && c.tunnelHub.HasTunnel(clientID) {
		resp, err := c.tunnelHub.StopWatcher(ctx, clientID, &proto.WatcherRequest{ClientId: clientID})
		if err != nil {
			return fmt.Errorf("stop watcher on client %s via tunnel: %w", clientID, err)
		}
		if !resp.Success {
			return fmt.Errorf("stop watcher on client %s: %s", clientID, resp.Message)
		}
		if setErr := c.registry.SetWatcherActive(clientID, false); setErr != nil {
			c.logger.Warn("failed to update watcher status in registry",
				"client_id", clientID, "error", setErr)
		}
		return nil
	}

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
	// Try tunnel first if available.
	if c.tunnelHub != nil && c.tunnelHub.HasTunnel(clientID) {
		resp, err := c.tunnelHub.GetStatus(ctx, clientID, &proto.StatusRequest{ClientId: clientID})
		if err != nil {
			return nil, fmt.Errorf("get status from client %s via tunnel: %w", clientID, err)
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
	// Try tunnel first if available.
	if c.tunnelHub != nil && c.tunnelHub.HasTunnel(clientID) {
		_, err := c.tunnelHub.StopBackup(ctx, clientID, &proto.StopRequest{ClientId: clientID})
		return err
	}

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

	// If the address is a tunnel marker, the client is NAT and should be
	// reached via tunnel, not direct connection.
	if strings.HasPrefix(ci.Address, "tunnel://") {
		return nil, fmt.Errorf("client %q is behind NAT (use tunnel)", clientID)
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

// PushRestoreToClient connects to the target client and streams decrypted file data
// via the PushRestore RPC. It reads each file from CAS, decrypts it with the source
// client's master key, and pushes it to the target client for writing.
// Returns the PushRestoreResponse from the target client or an error.
func (c *RemoteClientConnector) PushRestoreToClient(ctx context.Context, targetClientID string, files []PushRestoreFile, dest string) (*proto.PushRestoreResponse, error) {
	client, err := c.connectToClient(targetClientID)
	if err != nil {
		return nil, fmt.Errorf("connect to target client %s: %w", targetClientID, err)
	}

	stream, err := client.PushRestore(ctx)
	if err != nil {
		return nil, fmt.Errorf("open PushRestore stream to %s: %w", targetClientID, err)
	}

	const chunkSize = 64 * 1024 // 64KB chunks

	for i, f := range files {
		// Send file header.
		header := &proto.FileHeader{
			Blake3Hash:    f.Hash,
			FileName:      f.FileName,
			FilePath:      f.DestPath,
			FileSize:      int64(len(f.Data)),
			Permissions:   f.Permissions,
			Owner:         f.Owner,
			FileGroup:     f.Group,
			Symlink:       f.Symlink,
			SymlinkTarget: f.SymlinkTarget,
		}
		// Pass the base dest path in the Os field on the first file.
		if i == 0 {
			header.Os = dest
		}

		if err := stream.Send(&proto.FileChunk{
			Payload: &proto.FileChunk_Header{Header: header},
		}); err != nil {
			return nil, fmt.Errorf("send header for %s: %w", f.FileName, err)
		}

		// Send data in chunks (skip for symlinks).
		if !f.Symlink {
			for offset := 0; offset < len(f.Data); offset += chunkSize {
				end := offset + chunkSize
				if end > len(f.Data) {
					end = len(f.Data)
				}
				if err := stream.Send(&proto.FileChunk{
					Payload: &proto.FileChunk_Data{Data: f.Data[offset:end]},
				}); err != nil {
					return nil, fmt.Errorf("send data chunk for %s: %w", f.FileName, err)
				}
			}
		}

		// Send trailer.
		if err := stream.Send(&proto.FileChunk{
			Payload: &proto.FileChunk_Trailer{Trailer: &proto.FileTrailer{
				Blake3Hash: f.Hash,
				BytesTotal: int64(len(f.Data)),
			}},
		}); err != nil {
			return nil, fmt.Errorf("send trailer for %s: %w", f.FileName, err)
		}
	}

	// Close and get response.
	resp, err := stream.CloseAndRecv()
	if err != nil {
		return nil, fmt.Errorf("close PushRestore stream: %w", err)
	}

	return resp, nil
}

// PushRestoreFile describes a single decrypted file to push to a target client.
type PushRestoreFile struct {
	Hash          string
	FileName      string
	DestPath      string // full destination path on the target
	Data          []byte // decrypted file content
	Permissions   uint32
	Owner         string
	Group         string
	Symlink       bool
	SymlinkTarget string
}
