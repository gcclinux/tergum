package grpc

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/gcclinux/tergum/internal/backup"
	"github.com/gcclinux/tergum/internal/db"
	"github.com/gcclinux/tergum/internal/grpc/proto"
	"github.com/gcclinux/tergum/internal/model"
	"github.com/gcclinux/tergum/internal/observe"
	"github.com/gcclinux/tergum/internal/registry"
	versionPkg "github.com/gcclinux/tergum/internal/version"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

// DeletionEngine defines the interface for deletion operations.
type DeletionEngine interface {
	Delete(ctx context.Context, filter db.DeleteFilter, dryRun bool) (entriesDeleted int64, bytesFreed int64, err error)
}

// RetentionEngine defines the interface for retention policy queries.
type RetentionEngine interface {
	ListPolicies(ctx context.Context) ([]model.RetentionPolicy, error)
}

// CrossRestoreRequest describes a server-side cross-client restore push: pull
// files from the source client's backup and deliver them to the target client.
type CrossRestoreRequest struct {
	SourceClientID string
	TargetClientID string
	Query          string
	BackupID       string
	File           string
	Dest           string
}

// CrossRestoreResult reports the per-file outcome of a cross-client restore.
type CrossRestoreResult struct {
	FilesSent     int64
	FilesReceived int64
	FilesFailed   int64
}

// CrossRestorer orchestrates a cross-client restore push. The server injects a
// concrete implementation (internal/restore.CrossClientRestorer); it is nil in
// local "both" mode (RestoreToTarget then returns Unimplemented).
type CrossRestorer interface {
	RestoreToTarget(ctx context.Context, req CrossRestoreRequest) (CrossRestoreResult, error)
}

// ClientWatcherController starts/stops the file watcher on a remote client. The
// server injects the existing RemoteClientConnector; nil disables the feature.
type ClientWatcherController interface {
	StartClientWatcher(ctx context.Context, clientID string) error
	StopClientWatcher(ctx context.Context, clientID string) error
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
	clientsDir      string

	// Admin-client authorization (server-side only).
	adminPolicy       AdminPolicy
	crossRestorer     CrossRestorer
	watcherController ClientWatcherController
	auditLog          *slog.Logger
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
	ClientsDir      string

	// Admin-client authorization (server-side only). All optional; nil disables
	// the corresponding capability.
	AdminPolicy       AdminPolicy
	CrossRestorer     CrossRestorer
	WatcherController ClientWatcherController
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
		backupEngine:      cfg.BackupEngine,
		repo:              cfg.Repo,
		deletionEngine:    cfg.DeletionEngine,
		retentionEngine:   cfg.RetentionEngine,
		registry:          cfg.Registry,
		tunnelHub:         cfg.TunnelHub,
		backupSem:         NewSemaphore(maxBackups),
		version:           version,
		startedAt:         time.Now(),
		onClientConnect:   cfg.OnClientConnect,
		clientsDir:        cfg.ClientsDir,
		adminPolicy:       cfg.AdminPolicy,
		crossRestorer:     cfg.CrossRestorer,
		watcherController: cfg.WatcherController,
		auditLog:          observe.Logger("command-server"),
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

			// Sync backup active state from client's heartbeat payload.
			// The client service checks both its own engine and a signal file
			// left by CLI-initiated backups, so this is authoritative.
			_ = s.registry.SetBackupActive(clientID, req.BackupActive)

			// Sync last backup time if reported.
			if req.LastBackupAt != "" {
				if t, err := time.Parse(time.RFC3339, req.LastBackupAt); err == nil && !t.IsZero() {
					_ = s.registry.SetLastBackup(clientID, t)
				}
			}

			// Record the caller's trusted SPKI fingerprint for admin lookups.
			if fp := clientSPKIFromContext(ctx); fp != "" {
				_ = s.registry.SetSPKIFingerprint(clientID, fp)
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

	switch {
	case s.registry == nil:
		// Local "both" mode: no registry means no cross-client scoping — behave
		// exactly as before (honor req.ClientId only when set).
		if req.ClientId != "" {
			filter.ClientID = &req.ClientId
		}
	case s.callerIsAdmin(ctx):
		// Admins may list any client's backups (or all when unset).
		if req.ClientId != "" {
			filter.ClientID = &req.ClientId
		}
	default:
		// Non-admin callers are unconditionally scoped to their own backups,
		// regardless of req.ClientId (including empty). Resolve the trusted
		// identity from the SPKI fingerprint; fall back to the context identity.
		scope := ""
		if fp := clientSPKIFromContext(ctx); fp != "" {
			if ci := s.registry.FindClientBySPKI(fp); ci != nil {
				scope = ci.ClientID
			}
		}
		if scope == "" {
			if cn, err := clientIDFromContext(ctx); err == nil {
				scope = cn
			}
			s.auditLog.Warn("list_backups scope fallback: SPKI not found in registry",
				"event", "scope_fallback", "resolved_client", scope)
		}
		filter.ClientID = &scope
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
	// Hard gate: deletion over the server command service is an admin-only
	// operation. Only enforced when a registry/policy is present (server mode);
	// local "both" mode (no registry) keeps today's behavior. Per §3.2.4 the
	// authorization gate precedes the nil-engine precondition so an admin still
	// receives the existing "not configured" error.
	if s.registry != nil && !s.callerIsAdmin(ctx) {
		s.auditLog.Warn("admin-gated delete denied",
			"event", "admin_denied", "op", "delete_from_backup",
			"caller_fp", shortFP(clientSPKIFromContext(ctx)))
		return nil, status.Error(codes.PermissionDenied, "delete over the server command service requires admin-client privilege")
	}

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
// in the registry with its ID, callback address, and system identity.
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

	_, err := s.registry.RegisterWithIdentity(clientID, req.Address, req.OsFamily, req.MachineId, req.Hostname)
	if err != nil {
		return nil, MapError(err)
	}

	// Record the caller's trusted SPKI fingerprint for admin lookups.
	if fp := clientSPKIFromContext(ctx); fp != "" {
		_ = s.registry.SetSPKIFingerprint(clientID, fp)
	}

	return &proto.RegisterResponse{
		Success:       true,
		ServerVersion: s.version,
	}, nil
}

// ListRecoverableClients returns a list of clients whose backup databases and configuration
// can be recovered onto a rebuilt machine.
func (s *CommandServer) ListRecoverableClients(ctx context.Context, req *proto.ListRecoverableClientsRequest) (*proto.ListRecoverableClientsResponse, error) {
	var results []proto.RecoverableClientInfo

	seen := make(map[string]bool)

	if s.registry != nil {
		for _, ci := range s.registry.ListAllClients() {
			seen[ci.ClientID] = true
			info := proto.RecoverableClientInfo{
				ClientId:  ci.ClientID,
				Hostname:  ci.Hostname,
				OsFamily:  ci.OSFamily,
				MachineId: ci.MachineID,
				Status:    ci.Status,
			}
			if !ci.LastSeen.IsZero() {
				info.LastSeen = ci.LastSeen.UTC().Format(time.RFC3339)
			}
			if !ci.LastBackup.IsZero() {
				info.LastBackup = ci.LastBackup.UTC().Format(time.RFC3339)
			}
			if !ci.RegisteredAt.IsZero() {
				info.RegisteredAt = ci.RegisteredAt.UTC().Format(time.RFC3339)
			}

			// Inspect database copy if available
			if s.clientsDir != "" {
				dbPath := filepath.Join(s.clientsDir, ci.ClientID+".db")
				if fi, err := os.Stat(dbPath); err == nil {
					info.HasDatabase = true
					info.BytesTotal = fi.Size()

					if info.OsFamily == "" {
						info.OsFamily = s.registry.GetClientOS(ci.ClientID, s.clientsDir)
					}

					// Query file count and latest backup if not set
					if clientDB, err := sql.Open("sqlite", dbPath); err == nil {
						var count int64
						_ = clientDB.QueryRow(`SELECT COUNT(*) FROM backups`).Scan(&count)
						info.FileCount = count

						if info.LastBackup == "" {
							var finishedAt *string
							_ = clientDB.QueryRow(`SELECT finished_at FROM backup_jobs WHERE status = 'completed' AND finished_at IS NOT NULL ORDER BY finished_at DESC LIMIT 1`).Scan(&finishedAt)
							if finishedAt != nil {
								info.LastBackup = *finishedAt
							}
						}
						clientDB.Close()
					}
				}
			}

			results = append(results, info)
		}
	}

	// Also check clientsDir for any clients that have a database copy but might not be in the registry
	if s.clientsDir != "" {
		if entries, err := os.ReadDir(s.clientsDir); err == nil {
			for _, entry := range entries {
				if !entry.IsDir() && filepath.Ext(entry.Name()) == ".db" {
					cid := entry.Name()[:len(entry.Name())-3]
					if seen[cid] {
						continue
					}
					seen[cid] = true

					fi, _ := entry.Info()
					var size int64
					if fi != nil {
						size = fi.Size()
					}

					info := proto.RecoverableClientInfo{
						ClientId:    cid,
						HasDatabase: true,
						BytesTotal:  size,
						Status:      "offline",
					}

					dbPath := filepath.Join(s.clientsDir, entry.Name())
					if clientDB, err := sql.Open("sqlite", dbPath); err == nil {
						var count int64
						_ = clientDB.QueryRow(`SELECT COUNT(*) FROM backups`).Scan(&count)
						info.FileCount = count

						var osFamily string
						_ = clientDB.QueryRow(`SELECT os FROM backups WHERE os IS NOT NULL AND os != '' LIMIT 1`).Scan(&osFamily)
						info.OsFamily = osFamily

						var finishedAt *string
						_ = clientDB.QueryRow(`SELECT finished_at FROM backup_jobs WHERE status = 'completed' AND finished_at IS NOT NULL ORDER BY finished_at DESC LIMIT 1`).Scan(&finishedAt)
						if finishedAt != nil {
							info.LastBackup = *finishedAt
						}

						clientDB.Close()
					}

					results = append(results, info)
				}
			}
		}
	}

	return &proto.ListRecoverableClientsResponse{
		Clients: results,
	}, nil
}

// RebindClient rebinds client ownership to a rebuilt machine, updating its identity and address.
func (s *CommandServer) RebindClient(ctx context.Context, req *proto.RebindClientRequest) (*proto.RebindClientResponse, error) {
	clientID := req.ClientId
	if cn, err := clientIDFromContext(ctx); err == nil && cn != "" {
		if cn != "Tergum Client" {
			clientID = cn
		}
	}

	if clientID == "" {
		return nil, MapError(&model.ConfigError{Message: "client_id is required"})
	}

	if s.registry == nil {
		return &proto.RebindClientResponse{
			Success: true,
			Message: "registry not configured (local mode)",
		}, nil
	}

	// Close old tunnel if present and forced
	if req.Force && s.tunnelHub != nil && s.tunnelHub.HasTunnel(clientID) {
		s.tunnelHub.Unregister(clientID)
	}

	_, err := s.registry.RebindClient(clientID, req.Address, req.OsFamily, req.MachineId, req.Hostname, req.Force)
	if err != nil {
		return nil, MapError(err)
	}

	return &proto.RebindClientResponse{
		Success: true,
		Message: fmt.Sprintf("client %q successfully rebound to rebuilt machine", clientID),
	}, nil
}

// shortFP returns the first 12 hex characters of a fingerprint for audit logs,
// or "" if the fingerprint is empty.
func shortFP(fp string) string {
	if len(fp) <= 12 {
		return fp
	}
	return fp[:12]
}

// RestoreToTarget performs an admin-authorized cross-client restore: it pulls
// files from the source client's backup and pushes them to the target client.
// Authorization is enforced server-side via the admin policy.
func (s *CommandServer) RestoreToTarget(ctx context.Context, req *proto.RestoreToTargetRequest) (*proto.RestoreToTargetResponse, error) {
	callerFP := clientSPKIFromContext(ctx)

	// 1. Authorize.
	if !s.callerIsAdmin(ctx) {
		s.auditLog.Warn("admin-gated restore denied",
			"event", "admin_denied", "op", "restore_to_target",
			"caller_fp", shortFP(callerFP), "target", req.TargetClientId)
		return nil, status.Error(codes.PermissionDenied, "operation requires admin-client privilege")
	}

	// 2. Validate the request.
	if req.SourceClientId == "" || req.TargetClientId == "" {
		return nil, MapError(&model.ConfigError{Message: "source_client_id and target_client_id are required"})
	}
	if req.SourceClientId == req.TargetClientId {
		return nil, MapError(&model.ConfigError{Message: "source_client_id and target_client_id must differ"})
	}
	if req.Query == "" && req.BackupId == "" && req.File == "" {
		return nil, MapError(&model.ConfigError{Message: "one of query, backup_id, or file is required"})
	}
	if err := s.requireActiveClient(req.SourceClientId); err != nil {
		return nil, err
	}
	if err := s.requireActiveClient(req.TargetClientId); err != nil {
		return nil, err
	}

	// 3. Delegate to the injected cross-client restorer.
	if s.crossRestorer == nil {
		return nil, status.Error(codes.Unimplemented, "cross-client restore is not available on this server")
	}

	result, err := s.crossRestorer.RestoreToTarget(ctx, CrossRestoreRequest{
		SourceClientID: req.SourceClientId,
		TargetClientID: req.TargetClientId,
		Query:          req.Query,
		BackupID:       req.BackupId,
		File:           req.File,
		Dest:           req.Dest,
	})
	if err != nil {
		return nil, MapError(err)
	}

	s.auditLog.Info("admin cross-client restore",
		"event", "admin_cross_client_restore",
		"caller_fp", shortFP(callerFP),
		"source", req.SourceClientId,
		"target", req.TargetClientId,
		"files_sent", result.FilesSent,
		"files_failed", result.FilesFailed,
	)

	return &proto.RestoreToTargetResponse{
		Success:       result.FilesFailed == 0,
		FilesSent:     result.FilesSent,
		FilesReceived: result.FilesReceived,
		FilesFailed:   result.FilesFailed,
		Message: fmt.Sprintf("restored from %s to %s: %d sent, %d received, %d failed",
			req.SourceClientId, req.TargetClientId, result.FilesSent, result.FilesReceived, result.FilesFailed),
	}, nil
}

// ControlClientWatcher starts or stops the file watcher on a target client.
// Authorization is enforced server-side via the admin policy.
func (s *CommandServer) ControlClientWatcher(ctx context.Context, req *proto.ControlWatcherRequest) (*proto.WatcherResponse, error) {
	callerFP := clientSPKIFromContext(ctx)

	if !s.callerIsAdmin(ctx) {
		s.auditLog.Warn("admin-gated watcher control denied",
			"event", "admin_denied", "op", "control_client_watcher",
			"caller_fp", shortFP(callerFP), "target", req.TargetClientId)
		return nil, status.Error(codes.PermissionDenied, "operation requires admin-client privilege")
	}

	if req.TargetClientId == "" {
		return nil, MapError(&model.ConfigError{Message: "target_client_id is required"})
	}
	if err := s.requireActiveClient(req.TargetClientId); err != nil {
		return nil, err
	}

	if s.watcherController == nil {
		return nil, status.Error(codes.Unimplemented, "client watcher control is not available on this server")
	}

	var err error
	action := "stop"
	if req.Start {
		action = "start"
		err = s.watcherController.StartClientWatcher(ctx, req.TargetClientId)
	} else {
		err = s.watcherController.StopClientWatcher(ctx, req.TargetClientId)
	}
	if err != nil {
		return nil, MapError(err)
	}

	return &proto.WatcherResponse{
		Success: true,
		Message: fmt.Sprintf("watcher %s requested on client %s", action, req.TargetClientId),
	}, nil
}

// ListClients returns the server's registered-client list. It is an
// admin-gated read-only operation: an admin client may view the registry
// remotely, exactly like RestoreToTarget authorizes. Authorization keys off the
// caller's trusted mTLS SPKI fingerprint, never a client-supplied identity.
func (s *CommandServer) ListClients(ctx context.Context, req *proto.ListClientsRequest) (*proto.ListClientsResponse, error) {
	callerFP := clientSPKIFromContext(ctx)

	if !s.callerIsAdmin(ctx) {
		s.auditLog.Warn("admin-gated list clients denied",
			"event", "admin_denied", "op", "list_clients",
			"caller_fp", shortFP(callerFP))
		return nil, status.Error(codes.PermissionDenied, "operation requires admin-client privilege")
	}

	if s.registry == nil {
		return nil, MapError(&model.ConfigError{Message: "client registry not configured"})
	}

	clients := s.registry.ListClients()
	summaries := make([]*proto.ClientSummary, 0, len(clients))
	for i := range clients {
		ci := clients[i]
		summary := &proto.ClientSummary{
			ClientId:  ci.ClientID,
			Address:   ci.Address,
			OsFamily:  ci.OSFamily,
			Hostname:  ci.Hostname,
			MachineId: ci.MachineID,
			Status:    ci.Status,
		}
		if !ci.LastSeen.IsZero() {
			summary.LastSeen = ci.LastSeen.UTC().Format(time.RFC3339)
		}
		if lb := s.resolveClientLastBackup(&ci); !lb.IsZero() {
			summary.LastBackup = lb.UTC().Format(time.RFC3339)
		}
		if !ci.RegisteredAt.IsZero() {
			summary.RegisteredAt = ci.RegisteredAt.UTC().Format(time.RFC3339)
		}
		summaries = append(summaries, summary)
	}

	return &proto.ListClientsResponse{Clients: summaries}, nil
}

// GetClientStatus returns detailed status for a single registered client. It is
// an admin-gated read-only operation mirroring ListClients' authorization. A
// client that is not registered yields Found=false (not an error) so the CLI
// can reproduce the local "not found" message. Disabled clients are returned
// (viewing is allowed), matching the local path.
func (s *CommandServer) GetClientStatus(ctx context.Context, req *proto.GetClientStatusRequest) (*proto.ClientStatusResponse, error) {
	callerFP := clientSPKIFromContext(ctx)

	if !s.callerIsAdmin(ctx) {
		s.auditLog.Warn("admin-gated client status denied",
			"event", "admin_denied", "op", "get_client_status",
			"caller_fp", shortFP(callerFP), "target", req.ClientId)
		return nil, status.Error(codes.PermissionDenied, "operation requires admin-client privilege")
	}

	if s.registry == nil {
		return nil, MapError(&model.ConfigError{Message: "client registry not configured"})
	}

	ci := s.registry.GetClient(req.ClientId)
	if ci == nil {
		return &proto.ClientStatusResponse{Found: false}, nil
	}

	resp := &proto.ClientStatusResponse{
		Found:         true,
		ClientId:      ci.ClientID,
		Address:       ci.Address,
		OsFamily:      ci.OSFamily,
		Hostname:      ci.Hostname,
		MachineId:     ci.MachineID,
		Status:        ci.Status,
		Disabled:      ci.Disabled,
		WatcherActive: ci.WatcherActive,
		MissedBackups: int32(len(ci.MissedBackups)),
	}
	if !ci.LastSeen.IsZero() {
		resp.LastSeen = ci.LastSeen.UTC().Format(time.RFC3339)
	}
	if lb := s.resolveClientLastBackup(ci); !lb.IsZero() {
		resp.LastBackup = lb.UTC().Format(time.RFC3339)
	}
	if !ci.RegisteredAt.IsZero() {
		resp.RegisteredAt = ci.RegisteredAt.UTC().Format(time.RFC3339)
	}
	if ci.Schedule != nil {
		resp.HasSchedule = true
		resp.FullBackupCron = ci.Schedule.FullBackupCron
		resp.AutoBackupCron = ci.Schedule.AutoBackupCron
	}
	for _, mb := range ci.MissedBackups {
		detail := &proto.MissedBackupDetail{Level: mb.Level}
		if !mb.ScheduledAt.IsZero() {
			detail.ScheduledAt = mb.ScheduledAt.UTC().Format(time.RFC3339)
		}
		resp.MissedBackupDetails = append(resp.MissedBackupDetails, detail)
	}

	return resp, nil
}

// resolveClientLastBackup returns the client's last backup time. It prefers the
// registry value and falls back to querying the client's synced database under
// clientsDir for the most recent completed backup. This is the server-side
// equivalent of cmd.resolveLastBackup (the admin client has no access to the
// synced databases, so the resolution happens here).
func (s *CommandServer) resolveClientLastBackup(ci *registry.ClientInfo) time.Time {
	if !ci.LastBackup.IsZero() {
		return ci.LastBackup
	}
	if s.clientsDir == "" {
		return time.Time{}
	}

	clientDBPath := filepath.Join(s.clientsDir, ci.ClientID+".db")
	if _, err := os.Stat(clientDBPath); err != nil {
		return time.Time{}
	}

	clientDB, err := sql.Open("sqlite", clientDBPath)
	if err != nil {
		return time.Time{}
	}
	defer clientDB.Close()

	var finishedAt *string
	err = clientDB.QueryRow(
		`SELECT finished_at FROM backup_jobs
		 WHERE status = 'completed' AND finished_at IS NOT NULL
		 ORDER BY finished_at DESC LIMIT 1`,
	).Scan(&finishedAt)
	if err != nil || finishedAt == nil {
		return time.Time{}
	}

	// Try RFC3339 first, then legacy datetime format (matches cmd.resolveLastBackup).
	if t, err := time.Parse(time.RFC3339, *finishedAt); err == nil {
		return t
	}
	if t, err := time.ParseInLocation(time.DateTime, *finishedAt, time.UTC); err == nil {
		return t
	}
	return time.Time{}
}

// requireActiveClient returns an error if the client is not registered or is
// disabled. When no registry is configured the check is skipped.
func (s *CommandServer) requireActiveClient(clientID string) error {
	if s.registry == nil {
		return nil
	}
	ci := s.registry.GetClient(clientID)
	if ci == nil {
		return MapError(&model.ConfigError{Message: fmt.Sprintf("client %q is not registered", clientID)})
	}
	if ci.Disabled {
		return MapError(&model.ConfigError{Message: fmt.Sprintf("client %q is disabled", clientID)})
	}
	return nil
}

// clientSPKIFromContext returns the SHA-256 fingerprint (lowercase hex) of the
// verified mTLS peer certificate's SubjectPublicKeyInfo (SPKI). This is the
// trusted client identity used for admin authorization — distinct from the
// shared certificate CN and from any client-supplied client-id metadata.
// Returns "" when there is no verified peer certificate.
func clientSPKIFromContext(ctx context.Context) string {
	p, ok := peer.FromContext(ctx)
	if !ok {
		return ""
	}
	tlsInfo, ok := p.AuthInfo.(credentials.TLSInfo)
	if !ok || len(tlsInfo.State.PeerCertificates) == 0 {
		return ""
	}
	sum := sha256.Sum256(tlsInfo.State.PeerCertificates[0].RawSubjectPublicKeyInfo)
	return hex.EncodeToString(sum[:])
}

// callerIsAdmin reports whether the caller holds admin-client privilege. It is
// fail-closed: a nil policy or an empty/unknown SPKI fingerprint is not admin.
func (s *CommandServer) callerIsAdmin(ctx context.Context) bool {
	if s.adminPolicy == nil {
		return false
	}
	fp := clientSPKIFromContext(ctx)
	if fp == "" {
		return false
	}
	return s.adminPolicy.IsAdmin(fp)
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

	// Record the caller's trusted SPKI fingerprint for admin lookups.
	if s.registry != nil {
		if fp := clientSPKIFromContext(stream.Context()); fp != "" {
			_ = s.registry.SetSPKIFingerprint(clientID, fp)
		}
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

// AdminSearchFiles allows an admin client to search or restore files from
// another client's backup. This handles the bug condition where --client is
// specified without --target: the admin client wants to pull files locally
// rather than push them to a different target.
//
// For ListOnly=true, it returns matching files without restoring.
// For ListOnly=false (restore mode), it returns the file metadata that matches;
// the actual file streaming is handled separately (the admin client can then
// use the DataService to fetch file chunks).
func (s *CommandServer) AdminSearchFiles(ctx context.Context, req *proto.AdminSearchFilesRequest) (*proto.AdminSearchFilesResponse, error) {
	callerFP := clientSPKIFromContext(ctx)

	// 1. Authorize: only admin clients can search other clients' backups.
	if !s.callerIsAdmin(ctx) {
		s.auditLog.Warn("admin-gated search denied",
			"event", "admin_denied", "op", "admin_search_files",
			"caller_fp", shortFP(callerFP), "source", req.SourceClientId)
		return nil, status.Error(codes.PermissionDenied, "operation requires admin-client privilege")
	}

	// 2. Validate the request.
	if req.SourceClientId == "" {
		return nil, MapError(&model.ConfigError{Message: "source_client_id is required"})
	}
	if req.Query == "" && req.BackupId == "" {
		return nil, MapError(&model.ConfigError{Message: "one of query or backup_id is required"})
	}

	// 3. Locate and open the source client's database.
	if s.clientsDir == "" {
		return nil, MapError(&model.ConfigError{Message: "clients directory not configured on server"})
	}

	clientDBPath := filepath.Join(s.clientsDir, req.SourceClientId+".db")
	if _, err := os.Stat(clientDBPath); os.IsNotExist(err) {
		return nil, MapError(&model.ConfigError{
			Message: fmt.Sprintf("backup database for client %q not found", req.SourceClientId),
		})
	}

	clientDB, err := sql.Open("sqlite", clientDBPath)
	if err != nil {
		return nil, MapError(fmt.Errorf("failed to open client database: %w", err))
	}
	defer clientDB.Close()

	// 4. Build and execute the search query.
	var entries []fileEntry
	var queryErr error

	if req.BackupId != "" && req.Query != "" {
		// Search within a specific backup using both backup_id and query pattern.
		entries, queryErr = s.searchClientDB(ctx, clientDB, req.BackupId, req.Query)
	} else if req.BackupId != "" {
		// List all files in a specific backup.
		entries, queryErr = s.listBackupFiles(ctx, clientDB, req.BackupId)
	} else {
		// Search across all backups using query pattern.
		entries, queryErr = s.searchClientDBByPattern(ctx, clientDB, req.Query)
	}

	if queryErr != nil {
		return nil, MapError(fmt.Errorf("search query failed: %w", queryErr))
	}

	// 5. Convert to proto response format.
	files := make([]*proto.FileSearchResult, 0, len(entries))
	for _, e := range entries {
		files = append(files, &proto.FileSearchResult{
			FilePath:   e.FilePath,
			FileName:   e.FileName,
			FileSize:   e.FileSize,
			ModifiedAt: e.ModifiedAt,
			Blake3Hash: e.Blake3Hash,
			BackupId:   e.BackupID,
		})
	}

	s.auditLog.Info("admin search files",
		"event", "admin_search_files",
		"caller_fp", shortFP(callerFP),
		"source", req.SourceClientId,
		"query", req.Query,
		"backup_id", req.BackupId,
		"list_only", req.ListOnly,
		"files_found", len(files),
	)

	return &proto.AdminSearchFilesResponse{
		Success:    true,
		Files:      files,
		FilesFound: int64(len(files)),
		Message:    fmt.Sprintf("found %d files matching query in client %s", len(files), req.SourceClientId),
	}, nil
}

// fileEntry is an internal representation of a backup file entry for search results.
type fileEntry struct {
	BackupID   string
	Blake3Hash string
	FileName   string
	FilePath   string
	FileSize   int64
	ModifiedAt int64
}

// searchClientDB searches a client's database for files matching a pattern within a specific backup.
func (s *CommandServer) searchClientDB(ctx context.Context, clientDB *sql.DB, backupID, pattern string) ([]fileEntry, error) {
	// Convert glob pattern to SQL LIKE pattern (basic conversion).
	sqlPattern := globToSQLLike(pattern)

	rows, err := clientDB.QueryContext(ctx,
		`SELECT backup_id, blake3_hash, file_name, file_path, file_size, COALESCE(CAST(strftime('%s', modified_at) AS INTEGER), 0)
		 FROM backups 
		 WHERE backup_id = ? AND (file_path LIKE ? OR file_name LIKE ?)
		 ORDER BY file_path`,
		backupID, sqlPattern, sqlPattern,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanFileEntries(rows)
}

// listBackupFiles lists all files in a specific backup.
func (s *CommandServer) listBackupFiles(ctx context.Context, clientDB *sql.DB, backupID string) ([]fileEntry, error) {
	rows, err := clientDB.QueryContext(ctx,
		`SELECT backup_id, blake3_hash, file_name, file_path, file_size, COALESCE(CAST(strftime('%s', modified_at) AS INTEGER), 0)
		 FROM backups 
		 WHERE backup_id = ?
		 ORDER BY file_path`,
		backupID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanFileEntries(rows)
}

// searchClientDBByPattern searches a client's database for files matching a pattern across all backups.
func (s *CommandServer) searchClientDBByPattern(ctx context.Context, clientDB *sql.DB, pattern string) ([]fileEntry, error) {
	// Convert glob pattern to SQL LIKE pattern (basic conversion).
	sqlPattern := globToSQLLike(pattern)

	rows, err := clientDB.QueryContext(ctx,
		`SELECT backup_id, blake3_hash, file_name, file_path, file_size, COALESCE(CAST(strftime('%s', modified_at) AS INTEGER), 0)
		 FROM backups 
		 WHERE file_path LIKE ? OR file_name LIKE ?
		 ORDER BY backup_id DESC, file_path`,
		sqlPattern, sqlPattern,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanFileEntries(rows)
}

// scanFileEntries scans rows from a backup query into fileEntry structs.
func scanFileEntries(rows *sql.Rows) ([]fileEntry, error) {
	var entries []fileEntry
	for rows.Next() {
		var e fileEntry
		if err := rows.Scan(&e.BackupID, &e.Blake3Hash, &e.FileName, &e.FilePath, &e.FileSize, &e.ModifiedAt); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// globToSQLLike converts a glob-style pattern to a SQL LIKE pattern.
// Supports basic wildcards: * -> %, ? -> _
func globToSQLLike(pattern string) string {
	// Escape SQL LIKE special characters first
	result := pattern
	result = replaceAll(result, "%", "\\%")
	result = replaceAll(result, "_", "\\_")
	// Convert glob wildcards to SQL wildcards
	result = replaceAll(result, "*", "%")
	result = replaceAll(result, "?", "_")
	// If pattern doesn't have wildcards, wrap with % for substring match
	if !containsAny(result, "%_") {
		result = "%" + result + "%"
	}
	return result
}

// replaceAll is a simple string replacement helper.
func replaceAll(s, old, new string) string {
	var result string
	for i := 0; i < len(s); i++ {
		if i+len(old) <= len(s) && s[i:i+len(old)] == old {
			result += new
			i += len(old) - 1
		} else {
			result += string(s[i])
		}
	}
	return result
}

// containsAny checks if s contains any of the characters in chars.
func containsAny(s, chars string) bool {
	for _, c := range chars {
		for _, sc := range s {
			if c == sc {
				return true
			}
		}
	}
	return false
}

// AdminListBackups allows an admin client to list backup jobs from another
// client's backup catalog. Authorization is enforced via SPKI fingerprint.
func (s *CommandServer) AdminListBackups(ctx context.Context, req *proto.AdminListBackupsRequest) (*proto.AdminListBackupsResponse, error) {
	callerFP := clientSPKIFromContext(ctx)

	// 1. Authorize: only admin clients can list other clients' backups.
	if !s.callerIsAdmin(ctx) {
		s.auditLog.Warn("admin-gated list denied",
			"event", "admin_denied", "op", "admin_list_backups",
			"caller_fp", shortFP(callerFP), "client", req.ClientId)
		return nil, status.Error(codes.PermissionDenied,
			"this client is not authorized as an admin client on the server")
	}

	// 2. Validate the request.
	if req.ClientId == "" {
		return nil, MapError(&model.ConfigError{Message: "client_id is required"})
	}

	// 3. Locate and open the client's database.
	if s.clientsDir == "" {
		return nil, MapError(&model.ConfigError{
			Message: "clients directory not configured on server"})
	}

	clientDBPath := filepath.Join(s.clientsDir, req.ClientId+".db")
	if _, err := os.Stat(clientDBPath); os.IsNotExist(err) {
		return nil, MapError(&model.ConfigError{
			Message: fmt.Sprintf("client %q not found", req.ClientId)})
	}

	clientDB, err := sql.Open("sqlite", clientDBPath)
	if err != nil {
		return nil, MapError(fmt.Errorf("failed to open client database: %w", err))
	}
	defer clientDB.Close()

	// 4. Query backup jobs.
	limit := req.Limit
	if limit <= 0 {
		limit = 50
	}

	rows, err := clientDB.QueryContext(ctx,
		`SELECT backup_id, level, client_id, initiated_by, started_at, 
                finished_at, status, file_count, bytes_total, bytes_new, 
                files_deduped, error_message
         FROM backup_jobs
         ORDER BY started_at DESC
         LIMIT ?`, limit)
	if err != nil {
		return nil, MapError(fmt.Errorf("query failed: %w", err))
	}
	defer rows.Close()

	var backups []*proto.BackupJobInfo
	for rows.Next() {
		var job proto.BackupJobInfo
		var finishedAt sql.NullString
		var errorMsg sql.NullString

		if err := rows.Scan(
			&job.BackupId, &job.Level, &job.ClientId, &job.InitiatedBy,
			&job.StartedAt, &finishedAt, &job.Status, &job.FileCount,
			&job.BytesTotal, &job.BytesNew, &job.FilesDeduped, &errorMsg,
		); err != nil {
			return nil, MapError(fmt.Errorf("scan failed: %w", err))
		}

		if finishedAt.Valid {
			job.FinishedAt = finishedAt.String
		}
		if errorMsg.Valid {
			job.ErrorMessage = errorMsg.String
		}
		backups = append(backups, &job)
	}

	if err := rows.Err(); err != nil {
		return nil, MapError(fmt.Errorf("rows iteration failed: %w", err))
	}

	s.auditLog.Info("admin list backups",
		"event", "admin_list_backups",
		"caller_fp", shortFP(callerFP),
		"client", req.ClientId,
		"jobs_returned", len(backups))

	return &proto.AdminListBackupsResponse{
		Success: true,
		Backups: backups,
		Total:   int32(len(backups)),
		Message: fmt.Sprintf("found %d backup jobs for client %s", len(backups), req.ClientId),
	}, nil
}

// Ensure CommandServer satisfies the interface at compile time.
var _ proto.CommandServiceServer = (*CommandServer)(nil)
