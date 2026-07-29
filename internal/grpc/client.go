package grpc

import (
	"context"
	"crypto/tls"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/gcclinux/tergum/internal/grpc/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// maxMsgSize is the maximum gRPC message size for data connections (64 MB).
// The default 4 MB limit is too small for large manifests (23k+ files).
const maxMsgSize = 64 << 20

// ClientConfig configures retry behavior for the TergumClient.
type ClientConfig struct {
	InitialBackoff time.Duration // default 1s
	MaxBackoff     time.Duration // default 30s
	BackoffFactor  float64       // default 2.0
	MaxRetries     int           // default 5
	ClientID       string
}

// DefaultClientConfig returns a ClientConfig with default values.
func DefaultClientConfig() ClientConfig {
	return ClientConfig{
		InitialBackoff: 1 * time.Second,
		MaxBackoff:     30 * time.Second,
		BackoffFactor:  2.0,
		MaxRetries:     5,
	}
}

// applyDefaults fills in zero-value fields with defaults.
func (c ClientConfig) applyDefaults() ClientConfig {
	if c.InitialBackoff == 0 {
		c.InitialBackoff = 1 * time.Second
	}
	if c.MaxBackoff == 0 {
		c.MaxBackoff = 30 * time.Second
	}
	if c.BackoffFactor == 0 {
		c.BackoffFactor = 2.0
	}
	if c.MaxRetries == 0 {
		c.MaxRetries = 5
	}
	return c
}

// TergumClient wraps the CommandService and DataService gRPC clients
// with retry logic and exponential backoff for transient errors.
type TergumClient struct {
	command proto.CommandServiceClient
	data    proto.DataServiceClient
	config  ClientConfig
}

// SetClientID sets the client identifier used in request metadata.
func (c *TergumClient) SetClientID(id string) {
	c.config.ClientID = id
}

// contextWithMetadata appends the client ID to the outgoing context metadata.
func (c *TergumClient) contextWithMetadata(ctx context.Context) context.Context {
	if c.config.ClientID != "" {
		return metadata.AppendToOutgoingContext(ctx, "client-id", c.config.ClientID)
	}
	return ctx
}

// NewTergumClient creates a TergumClient from existing gRPC connections.
func NewTergumClient(commandConn, dataConn grpc.ClientConnInterface, cfg ClientConfig) *TergumClient {
	cfg = cfg.applyDefaults()
	return &TergumClient{
		command: proto.NewCommandServiceClient(commandConn),
		data:    proto.NewDataServiceClient(dataConn),
		config:  cfg,
	}
}

// Connect establishes gRPC connections to the command and data services
// and returns a configured TergumClient.
func Connect(ctx context.Context, address string, commandPort int, dataPort int, tlsConfig *tls.Config) (*TergumClient, error) {
	creds := credentials.NewTLS(tlsConfig)

	commandAddr := fmt.Sprintf("%s:%d", address, commandPort)
	commandConn, err := grpc.NewClient(commandAddr, grpc.WithTransportCredentials(creds))
	if err != nil {
		return nil, fmt.Errorf("connecting to command service at %s: %w", commandAddr, err)
	}

	dataAddr := fmt.Sprintf("%s:%d", address, dataPort)
	dataConn, err := grpc.NewClient(dataAddr,
		grpc.WithTransportCredentials(creds),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(maxMsgSize),
			grpc.MaxCallSendMsgSize(maxMsgSize),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("connecting to data service at %s: %w", dataAddr, err)
	}

	return NewTergumClient(commandConn, dataConn, DefaultClientConfig()), nil
}

// ConnectWithConfig is like Connect but allows specifying a custom ClientConfig.
func ConnectWithConfig(ctx context.Context, address string, commandPort int, dataPort int, tlsConfig *tls.Config, cfg ClientConfig) (*TergumClient, error) {
	creds := credentials.NewTLS(tlsConfig)

	commandAddr := fmt.Sprintf("%s:%d", address, commandPort)
	commandConn, err := grpc.NewClient(commandAddr, grpc.WithTransportCredentials(creds))
	if err != nil {
		return nil, fmt.Errorf("connecting to command service at %s: %w", commandAddr, err)
	}

	dataAddr := fmt.Sprintf("%s:%d", address, dataPort)
	dataConn, err := grpc.NewClient(dataAddr,
		grpc.WithTransportCredentials(creds),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(maxMsgSize),
			grpc.MaxCallSendMsgSize(maxMsgSize),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("connecting to data service at %s: %w", dataAddr, err)
	}

	return NewTergumClient(commandConn, dataConn, cfg), nil
}

// DataClient returns the underlying DataServiceClient for use by components
// that need direct access to the data service (e.g., RemoteServerConnection, RemoteDataSource).
func (c *TergumClient) DataClient() proto.DataServiceClient {
	return c.data
}

// --- CommandService Methods ---

// TriggerBackup sends a backup request to the server.
func (c *TergumClient) TriggerBackup(ctx context.Context, level proto.BackupLevel, clientID, initiatedBy string) (*proto.BackupResponse, error) {
	ctx = c.contextWithMetadata(ctx)
	var resp *proto.BackupResponse
	err := c.withRetry(ctx, func() error {
		var e error
		resp, e = c.command.TriggerBackup(ctx, &proto.BackupRequest{
			Level:       level,
			ClientId:    clientID,
			InitiatedBy: initiatedBy,
		})
		return e
	})
	return resp, err
}

// StopBackup stops an in-progress backup.
func (c *TergumClient) StopBackup(ctx context.Context, backupID, clientID string) (*proto.StopResponse, error) {
	ctx = c.contextWithMetadata(ctx)
	var resp *proto.StopResponse
	err := c.withRetry(ctx, func() error {
		var e error
		resp, e = c.command.StopBackup(ctx, &proto.StopRequest{
			BackupId: backupID,
			ClientId: clientID,
		})
		return e
	})
	return resp, err
}

// GetStatus queries the current operation status.
func (c *TergumClient) GetStatus(ctx context.Context, clientID string) (*proto.StatusResponse, error) {
	ctx = c.contextWithMetadata(ctx)
	var resp *proto.StatusResponse
	err := c.withRetry(ctx, func() error {
		var e error
		resp, e = c.command.GetStatus(ctx, &proto.StatusRequest{
			ClientId: clientID,
		})
		return e
	})
	return resp, err
}

// Ping checks server health.
func (c *TergumClient) Ping(ctx context.Context) (*proto.PingResponse, error) {
	return c.PingWithState(ctx, proto.PingRequest{})
}

// PingWithState checks server health and reports client state (watcher, last backup)
// for bidirectional sync on every heartbeat cycle.
func (c *TergumClient) PingWithState(ctx context.Context, req proto.PingRequest) (*proto.PingResponse, error) {
	ctx = c.contextWithMetadata(ctx)
	var resp *proto.PingResponse
	err := c.withRetry(ctx, func() error {
		var e error
		resp, e = c.command.Ping(ctx, &req)
		return e
	})
	return resp, err
}

// ListBackups queries backup history.
func (c *TergumClient) ListBackups(ctx context.Context, clientID string, limit int32) (*proto.ListBackupsResponse, error) {
	ctx = c.contextWithMetadata(ctx)
	var resp *proto.ListBackupsResponse
	err := c.withRetry(ctx, func() error {
		var e error
		resp, e = c.command.ListBackups(ctx, &proto.ListBackupsRequest{
			ClientId: clientID,
			Limit:    limit,
		})
		return e
	})
	return resp, err
}

// DeleteFromBackup deletes backup entries.
func (c *TergumClient) DeleteFromBackup(ctx context.Context, req *proto.DeleteRequest) (*proto.DeleteResponse, error) {
	ctx = c.contextWithMetadata(ctx)
	var resp *proto.DeleteResponse
	err := c.withRetry(ctx, func() error {
		var e error
		resp, e = c.command.DeleteFromBackup(ctx, req)
		return e
	})
	return resp, err
}

// GetRetention queries retention policies.
func (c *TergumClient) GetRetention(ctx context.Context, clientID string) (*proto.RetentionResponse, error) {
	ctx = c.contextWithMetadata(ctx)
	var resp *proto.RetentionResponse
	err := c.withRetry(ctx, func() error {
		var e error
		resp, e = c.command.GetRetention(ctx, &proto.RetentionRequest{
			ClientId: clientID,
		})
		return e
	})
	return resp, err
}

// StartWatcher sends a start watcher request to the target.
func (c *TergumClient) StartWatcher(ctx context.Context, clientID string) (*proto.WatcherResponse, error) {
	ctx = c.contextWithMetadata(ctx)
	var resp *proto.WatcherResponse
	err := c.withRetry(ctx, func() error {
		var e error
		resp, e = c.command.StartWatcher(ctx, &proto.WatcherRequest{
			ClientId: clientID,
		})
		return e
	})
	return resp, err
}

// StopWatcher sends a stop watcher request to the target.
func (c *TergumClient) StopWatcher(ctx context.Context, clientID string) (*proto.WatcherResponse, error) {
	ctx = c.contextWithMetadata(ctx)
	var resp *proto.WatcherResponse
	err := c.withRetry(ctx, func() error {
		var e error
		resp, e = c.command.StopWatcher(ctx, &proto.WatcherRequest{
			ClientId: clientID,
		})
		return e
	})
	return resp, err
}

// RegisterClient registers this client with the server.
func (c *TergumClient) RegisterClient(ctx context.Context, clientID, address string) (*proto.RegisterResponse, error) {
	ctx = c.contextWithMetadata(ctx)
	var resp *proto.RegisterResponse
	err := c.withRetry(ctx, func() error {
		var e error
		resp, e = c.command.RegisterClient(ctx, &proto.RegisterRequest{
			ClientId: clientID,
			Address:  address,
		})
		return e
	})
	return resp, err
}

// PushRestore opens a streaming client for pushing restored files to a target client.
// Streaming RPCs are not retried — the caller manages the stream lifecycle.
func (c *TergumClient) PushRestore(ctx context.Context) (proto.CommandService_PushRestoreClient, error) {
	ctx = c.contextWithMetadata(ctx)
	return c.command.PushRestore(ctx)
}

// CommandTunnel opens a bidirectional command tunnel stream to the server.
// Used by NAT clients to receive commands from the server without inbound connectivity.
// Streaming RPCs are not retried — the caller manages the stream lifecycle.
func (c *TergumClient) CommandTunnel(ctx context.Context) (proto.CommandService_CommandTunnelClient, error) {
	ctx = c.contextWithMetadata(ctx)
	return c.command.CommandTunnel(ctx)
}

// --- DataService Methods ---

// Upload returns a streaming client for uploading file chunks.
// Streaming RPCs are not retried — the caller manages the stream lifecycle.
func (c *TergumClient) Upload(ctx context.Context) (proto.DataService_UploadClient, error) {
	ctx = c.contextWithMetadata(ctx)
	return c.data.Upload(ctx)
}

// Download returns a streaming client for downloading file chunks.
// Streaming RPCs are not retried — the caller manages the stream lifecycle.
func (c *TergumClient) Download(ctx context.Context, hash, clientID string) (proto.DataService_DownloadClient, error) {
	ctx = c.contextWithMetadata(ctx)
	return c.data.Download(ctx, &proto.RestoreRequest{
		Blake3Hash: hash,
		ClientId:   clientID,
	})
}

// SyncDatabase returns a streaming client for database sync.
// Streaming RPCs are not retried — the caller manages the stream lifecycle.
func (c *TergumClient) SyncDatabase(ctx context.Context) (proto.DataService_SyncDatabaseClient, error) {
	ctx = c.contextWithMetadata(ctx)
	return c.data.SyncDatabase(ctx)
}

// ExchangeManifest sends a manifest and receives the diff of needed files.
func (c *TergumClient) ExchangeManifest(ctx context.Context, manifest *proto.Manifest) (*proto.ManifestDiff, error) {
	ctx = c.contextWithMetadata(ctx)
	var resp *proto.ManifestDiff
	err := c.withRetry(ctx, func() error {
		var e error
		resp, e = c.data.ExchangeManifest(ctx, manifest)
		return e
	})
	return resp, err
}

// --- Retry Logic ---

// withRetry executes an operation with exponential backoff retry for transient errors.
// It fails immediately on non-retryable errors (auth, config, storage full, etc.).
func (c *TergumClient) withRetry(ctx context.Context, operation func() error) error {
	backoff := c.config.InitialBackoff
	for attempt := 0; attempt <= c.config.MaxRetries; attempt++ {
		err := operation()
		if err == nil {
			return nil
		}
		if !isRetryable(err) {
			return err // fail immediately on non-retryable errors
		}
		if attempt == c.config.MaxRetries {
			return err // exhausted retries
		}
		// Add jitter: ±25% of backoff duration
		jitter := time.Duration(float64(backoff) * (0.75 + rand.Float64()*0.5))

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(jitter):
		}

		backoff = time.Duration(float64(backoff) * c.config.BackoffFactor)
		if backoff > c.config.MaxBackoff {
			backoff = c.config.MaxBackoff
		}
	}
	return nil
}

// isRetryable determines whether an error is transient and safe to retry.
func isRetryable(err error) bool {
	st, ok := status.FromError(err)
	if !ok {
		// Non-gRPC errors (e.g., connection refused) are retryable
		return true
	}
	switch st.Code() {
	case codes.Unavailable, codes.DeadlineExceeded, codes.Internal, codes.Aborted:
		return true
	case codes.Unauthenticated, codes.InvalidArgument, codes.ResourceExhausted,
		codes.PermissionDenied, codes.Unimplemented, codes.NotFound,
		codes.AlreadyExists, codes.FailedPrecondition, codes.Canceled:
		return false
	default:
		return false
	}
}
