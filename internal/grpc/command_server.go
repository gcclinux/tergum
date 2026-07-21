package grpc

import (
	"context"
	"fmt"
	"time"

	"github.com/gcclinux/tergum/internal/backup"
	"github.com/gcclinux/tergum/internal/db"
	"github.com/gcclinux/tergum/internal/grpc/proto"
	"github.com/gcclinux/tergum/internal/model"
	"github.com/gcclinux/tergum/internal/registry"
	versionPkg "github.com/gcclinux/tergum/internal/version"
	"github.com/google/uuid"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
)

// DeletionEngine defines the interface for deletion operations.
type DeletionEngine interface {
	Delete(ctx context.Context, filter db.DeleteFilter, dryRun bool) (entriesDeleted int64, bytesFreed int64, err error)
}

// RetentionEngine defines the interface for retention policy queries.
type RetentionEngine interface {
	ListPolicies(ctx context.Context) ([]model.RetentionPolicy, error)
}

// CommandServer implements the CommandServiceServer interface.
type CommandServer struct {
	proto.UnimplementedCommandServiceServer

	backupEngine    backup.Engine
	repo            db.Repository
	deletionEngine  DeletionEngine
	retentionEngine RetentionEngine
	registry        *registry.Registry
	tunnelHub       *TunnelHub
	backupSem       *Semaphore
	version         string
	startedAt       time.Time
	onClientConnect func(clientID string) // called after a tunnel client reconnects
}

// CommandServerConfig holds configuration for the CommandServer.
type CommandServerConfig struct {
	BackupEngine    backup.Engine
	Repo            db.Repository
	DeletionEngine  DeletionEngine
	RetentionEngine RetentionEngine
	Registry        *registry.Registry // optional; nil in local "both" mode
	TunnelHub       *TunnelHub         // optional; nil disables command tunnels
	MaxBackups      int                // max concurrent backups, default 4
	Version         string
	OnClientConnect func(clientID string) // called after a tunnel client reconnects
}

// NewCommandServer creates a new CommandServer with the given configuration.
func NewCommandServer(cfg CommandServerConfig) *CommandServer {
	maxBackups := cfg.MaxBackups
	if maxBackups <= 0 {
		maxBackups = 4
	}

	version := cfg.Version
	if version == "" {
		version = versionPkg.Version
	}

	return &CommandServer{
		backupEngine:    cfg.BackupEngine,
		repo:            cfg.Repo,
		deletionEngine:  cfg.DeletionEngine,
		retentionEngine: cfg.RetentionEngine,
		registry:        cfg.Registry,
		tunnelHub:       cfg.TunnelHub,
		backupSem:       NewSemaphore(maxBackups),
		version:         version,
		startedAt:       time.Now(),
		onClientConnect: cfg.OnClientConnect,
	}
}

// TriggerBackup initiates a backup operation. It acquires a concurrency slot,
// starts the backup in a goroutine, and immediately returns the backup ID.
func (s *CommandServer) TriggerBackup(ctx context.Context, req *proto.BackupRequest) (*proto.BackupResponse, error) {
	if req.ClientId == "" {
		return nil, MapError(&model.ConfigError{Message: "client_id is required"})
	}

	// Reject if the client is disabled.
	if s.registry != nil {
		if ci := s.registry.GetClient(req.ClientId); ci != nil && ci.Disabled {
			return nil, MapError(&model.ConfigError{Message: fmt.Sprintf("client %q is disabled", req.ClientId)})
		}
	}

	// Acquire backup semaphore slot.
	if err := s.backupSem.Acquire(ctx); err != nil {
		return nil, MapError(&model.ConnectionError{Message: "server at maximum backup capacity"})
	}

	// Map proto level to internal level.
	var level model.BackupLevel
	switch req.Level {
	case proto.BackupLevel_FULL:
		level = model.BackupLevelFull
	case proto.BackupLevel_ONGOING:
		level = model.BackupLevelOngoing
	default:
		level = model.BackupLevelAuto
	}

	initiatedBy := req.InitiatedBy
	if initiatedBy == "" {
		initiatedBy = "grpc"
	}

	backupID := uuid.New().String()

	// Start backup in background goroutine.
	go func() {
		defer s.backupSem.Release()
		backupReq := backup.BackupRequest{
			Level:       level,
			ClientID:    req.ClientId,
			InitiatedBy: initiatedBy,
		}
		// Errors are recorded in the job table by the engine itself.
		_, _ = s.backupEngine.RunBackup(context.Background(), backupReq)
	}()

	return &proto.BackupResponse{
		BackupId: backupID,
		Status:   "started",
		Message:  fmt.Sprintf("backup %s initiated for client %s", backupID, req.ClientId),
	}, nil
}

// StopBackup stops an in-progress backup for a given client.
func (s *CommandServer) StopBackup(ctx context.Context, req *proto.StopRequest) (*proto.StopResponse, error) {
	if err := s.backupEngine.Stop(ctx); err != nil {
		return nil, MapError(err)
	}
	return &proto.StopResponse{
		Success: true,
		Message: "backup stop signal sent",
	}, nil
}

// GetStatus returns the status of running operations for a client.
func (s *CommandServer) GetStatus(ctx context.Context, req *proto.StatusRequest) (*proto.StatusResponse, error) {
	// Query repository for running jobs for this client.
	runningStatus := model.JobRunning
	filter := db.JobFilter{
		Status: &runningStatus,
		Limit:  1,
	}
	if req.ClientId != "" {
		filter.ClientID = &req.ClientId
	}

	jobs, err := s.repo.ListJobs(ctx, filter)
	if err != nil {
		return nil, MapError(err)
	}

	if len(jobs) == 0 {
		return &proto.StatusResponse{
			Status:  "idle",
			Message: "no active operations",
		}, nil
	}

	job := jobs[0]
	return &proto.StatusResponse{
		Status:           string(job.Status),
		BackupId:         job.BackupID,
		FilesProcessed:   job.FileCount,
		BytesTransferred: job.BytesNew,
		StartedAt:        job.StartedAt.Format(time.RFC3339),
		Message:          fmt.Sprintf("backup %s in progress", job.BackupID),
	}, nil
}

// Ping returns server version and uptime.
func (s *CommandServer) Ping(ctx context.Context, req *proto.PingRequest) (*proto.PingResponse, error) {
	// If registry is configured, update the client's heartbeat and sync state.
	if s.registry != nil {
		if clientID, err := clientIDFromContext(ctx); err == nil && clientID != "" {
			// Skip state updates for disabled clients but inform them.
			ci := s.registry.GetClient(clientID)
			if ci != nil && ci.Disabled {
				uptime := time.Since(s.startedAt).Truncate(time.Second)
				return &proto.PingResponse{
					Version:        s.version,
					Commit:         versionPkg.Commit,
					BuildDate:      versionPkg.BuildDate,
					Uptime:         uptime.String(),
					ClientDisabled: true,
				}, nil
			}

			_ = s.registry.Heartbeat(clientID)

			// Sync watcher state from client's heartbeat payload.
			_ = s.registry.SetWatcherActive(clientID, req.WatcherActive)

			// Sync last backup time if reported.
			if req.LastBackupAt != "" {
				if t, err := time.Parse(time.RFC3339, req.LastBackupAt); err == nil && !t.IsZero() {
					_ = s.registry.SetLastBackup(clientID, t)
				}
			}
		}
	}

	uptime := time.Since(s.startedAt).Truncate(time.Second)
	return &proto.PingResponse{
		Version:   s.version,
		Commit:    versionPkg.Commit,
		BuildDate: versionPkg.BuildDate,
		Uptime:    uptime.String(),
	}, nil
}

// ListBackups returns a list of backup jobs, optionally filtered by client.
func (s *CommandServer) ListBackups(ctx context.Context, req *proto.ListBackupsRequest) (*proto.ListBackupsResponse, error) {
	filter := db.JobFilter{
		Limit: int(req.Limit),
	}
	if req.ClientId != "" {
		filter.ClientID = &req.ClientId
	}
	if filter.Limit <= 0 {
		filter.Limit = 50
	}

	jobs, err := s.repo.ListJobs(ctx, filter)
	if err != nil {
		return nil, MapError(err)
	}

	var backups []*proto.BackupJobInfo
	for _, j := range jobs {
		info := &proto.BackupJobInfo{
			BackupId:     j.BackupID,
			Level:        j.Level,
			ClientId:     j.ClientID,
			InitiatedBy:  j.InitiatedBy,
			StartedAt:    j.StartedAt.Format(time.RFC3339),
			Status:       string(j.Status),
			FileCount:    j.FileCount,
			BytesTotal:   j.BytesTotal,
			BytesNew:     j.BytesNew,
			FilesDeduped: j.FilesDeduped,
			ErrorMessage: j.ErrorMessage,
		}
		if j.FinishedAt != nil {
			info.FinishedAt = j.FinishedAt.Format(time.RFC3339)
		}
		backups = append(backups, info)
	}

	return &proto.ListBackupsResponse{
		Backups: backups,
		Total:   int32(len(backups)),
	}, nil
}

// DeleteFromBackup delegates to the deletion engine.
func (s *CommandServer) DeleteFromBackup(ctx context.Context, req *proto.DeleteRequest) (*proto.DeleteResponse, error) {
	if s.deletionEngine == nil {
		return nil, MapError(&model.ConfigError{Message: "deletion engine not configured"})
	}

	filter := db.DeleteFilter{
		BackupID:   req.BackupId,
		FilePath:   req.FilePath,
		FolderPath: req.FolderPath,
		AllBackups: req.AllBackups,
	}

	entriesDeleted, bytesFreed, err := s.deletionEngine.Delete(ctx, filter, req.DryRun)
	if err != nil {
		return nil, MapError(err)
	}

	msg := fmt.Sprintf("deleted %d entries, freed %d bytes", entriesDeleted, bytesFreed)
	if req.DryRun {
		msg = fmt.Sprintf("dry-run: would delete %d entries, free %d bytes", entriesDeleted, bytesFreed)
	}

	return &proto.DeleteResponse{
		Success:        true,
		EntriesDeleted: entriesDeleted,
		BytesFreed:     bytesFreed,
		Message:        msg,
	}, nil
}

// GetRetention returns the list of configured retention policies.
func (s *CommandServer) GetRetention(ctx context.Context, req *proto.RetentionRequest) (*proto.RetentionResponse, error) {
	if s.retentionEngine == nil {
		return nil, MapError(&model.ConfigError{Message: "retention engine not configured"})
	}

	policies, err := s.retentionEngine.ListPolicies(ctx)
	if err != nil {
		return nil, MapError(err)
	}

	var protoPolicies []*proto.RetentionPolicyProto
	for _, p := range policies {
		pp := &proto.RetentionPolicyProto{
			Name:         p.Name,
			KeepVersions: int32(p.KeepVersions),
			Pattern:      p.Pattern,
			Priority:     int32(p.Priority),
			Enabled:      p.Enabled,
		}
		if p.KeepDays != nil {
			pp.KeepDays = int32(*p.KeepDays)
		}
		protoPolicies = append(protoPolicies, pp)
	}

	return &proto.RetentionResponse{
		Policies: protoPolicies,
	}, nil
}

// RegisterClient handles a client registration request. It records the client
// in the registry with its ID and callback address.
func (s *CommandServer) RegisterClient(ctx context.Context, req *proto.RegisterRequest) (*proto.RegisterResponse, error) {
	clientID := req.ClientId
	// Prefer the certificate CN as the authoritative client identity when available.
	if cn, err := clientIDFromContext(ctx); err == nil && cn != "" {
		if cn != "Tergum Client" {
			clientID = cn
		}
	}

	if clientID == "" {
		return nil, MapError(&model.ConfigError{Message: "client_id is required"})
	}

	if s.registry == nil {
		// In local "both" mode the registry is not configured; accept gracefully.
		return &proto.RegisterResponse{
			Success:       true,
			ServerVersion: s.version,
		}, nil
	}

	_, err := s.registry.Register(clientID, req.Address)
	if err != nil {
		return nil, MapError(err)
	}

	return &proto.RegisterResponse{
		Success:       true,
		ServerVersion: s.version,
	}, nil
}

// clientIDFromContext extracts the client identity from the mTLS peer
// certificate Common Name (CN) or gRPC metadata. Returns empty string if TLS info is unavailable.
func clientIDFromContext(ctx context.Context) (string, error) {
	cn := ""
	if p, ok := peer.FromContext(ctx); ok {
		if tlsInfo, ok := p.AuthInfo.(credentials.TLSInfo); ok && len(tlsInfo.State.PeerCertificates) > 0 {
			cn = tlsInfo.State.PeerCertificates[0].Subject.CommonName
		}
	}

	// If CN is set and is not the generic "Tergum Client", it is the authoritative ID.
	if cn != "" && cn != "Tergum Client" {
		return cn, nil
	}

	// Fallback to client-id metadata if the CN is generic "Tergum Client" or missing.
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if ids := md.Get("client-id"); len(ids) > 0 && ids[0] != "" {
			return ids[0], nil
		}
	}

	if cn != "" {
		return cn, nil
	}

	return "", fmt.Errorf("no client identity found")
}

// CommandTunnel handles bidirectional command tunnels from NAT clients.
// The client opens this stream and keeps it alive; the server pushes commands
// and receives responses over it. This eliminates the need for inbound
// connectivity to the client.
func (s *CommandServer) CommandTunnel(stream proto.CommandService_CommandTunnelServer) error {
	// The first message from the client is always a registration response
	// with RequestId "__register__" that carries the clientID.
	firstMsg, err := stream.Recv()
	if err != nil {
		return fmt.Errorf("tunnel: waiting for registration: %w", err)
	}

	var clientID string
	if firstMsg.RequestId == "__register__" && firstMsg.PingResponse != nil {
		clientID = firstMsg.PingResponse.Version // Version field carries clientID during registration
	}

	// Fallback: try to get clientID from the mTLS certificate or metadata.
	if clientID == "" {
		cn, cnErr := clientIDFromContext(stream.Context())
		if cnErr == nil && cn != "" {
			clientID = cn
		}
	}

	if clientID == "" {
		return fmt.Errorf("tunnel: client did not identify itself")
	}

	if s.tunnelHub == nil {
		return fmt.Errorf("tunnel: tunnel hub not configured on this server")
	}

	// Register the tunnel.
	s.tunnelHub.Register(clientID, stream)
	defer func() {
		s.tunnelHub.Unregister(clientID)
		// When the tunnel disconnects, reset watcher status since the client's
		// watcher is no longer reachable from the server.
		if s.registry != nil {
			_ = s.registry.SetWatcherActive(clientID, false)
		}
	}()

	// Also mark the client as online in the registry if available.
	if s.registry != nil {
		// Register with a special "tunnel" address marker so the connector
		// knows to use the tunnel instead of dialing directly.
		_, _ = s.registry.Register(clientID, "tunnel://"+clientID)
	}

	// Query the client's actual watcher/backup state asynchronously so the
	// registry reflects reality after a reconnect (e.g. client restarted
	// with watcher disabled).
	go s.syncClientStateOnConnect(stream.Context(), clientID)

	// Read responses from the client and deliver them to waiting callers.
	for {
		resp, recvErr := stream.Recv()
		if recvErr != nil {
			return recvErr // stream closed or error — client disconnected
		}

		s.tunnelHub.DeliverResponse(clientID, resp)
	}
}

// syncClientStateOnConnect queries the client's actual status via the tunnel
// shortly after connection and updates the registry to reflect reality. This
// handles the case where the server thinks the watcher is running (from a
// previous session) but the client restarted with it disabled.
func (s *CommandServer) syncClientStateOnConnect(ctx context.Context, clientID string) {
	if s.tunnelHub == nil || s.registry == nil {
		return
	}

	// Small delay to let the tunnel fully establish before sending commands.
	select {
	case <-ctx.Done():
		return
	case <-time.After(2 * time.Second):
	}

	// Query the client's status via the tunnel.
	statusResp, err := s.tunnelHub.GetStatus(ctx, clientID, &proto.StatusRequest{ClientId: clientID})
	if err != nil {
		// Client may have disconnected already — that's fine.
		return
	}

	// Update watcher status from the client's actual response.
	_ = s.registry.SetWatcherActive(clientID, statusResp.WatcherActive)

	// Invoke the onClientConnect callback to refresh last backup time
	// from the server's copy of the client's synced database.
	if s.onClientConnect != nil {
		s.onClientConnect(clientID)
	}
}

// TunnelHub returns the server's TunnelHub for use by other components
// (e.g., the RemoteClientConnector) to send commands via tunnel.
func (s *CommandServer) TunnelHub() *TunnelHub {
	return s.tunnelHub
}

// Ensure CommandServer satisfies the interface at compile time.
var _ proto.CommandServiceServer = (*CommandServer)(nil)
