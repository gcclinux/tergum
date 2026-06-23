package grpc

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/gcclinux/tergum/internal/backup"
	"github.com/gcclinux/tergum/internal/config"
	"github.com/gcclinux/tergum/internal/crypto"
	"github.com/gcclinux/tergum/internal/db"
	"github.com/gcclinux/tergum/internal/grpc/proto"
	"github.com/gcclinux/tergum/internal/model"
	versionPkg "github.com/gcclinux/tergum/internal/version"
	"github.com/gcclinux/tergum/internal/watcher"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ClientCommandServer handles incoming commands from the server on a client node.
// It implements the CommandServiceServer interface so that the remote server can
// trigger backups, stop them, query status, and health-check the client.
type ClientCommandServer struct {
	proto.UnimplementedCommandServiceServer

	mu     sync.Mutex
	engine backup.Engine // current running engine, nil when idle

	// Dependencies for creating new backup engines on each trigger.
	serverConn backup.ServerConnection
	repo       db.Repository
	encryptor  *crypto.AESEncryptor
	cfg        *config.Config
	masterKey  []byte

	// Watcher instance for server-initiated watcher control.
	watcher watcher.Watcher

	// Tracking fields for status and ping responses.
	version   string
	startedAt time.Time

	// Current backup metadata (set when a backup is running).
	activeBackupID string
}

// ClientCommandServerConfig holds configuration for the ClientCommandServer.
type ClientCommandServerConfig struct {
	ServerConn backup.ServerConnection
	Repo       db.Repository
	Encryptor  *crypto.AESEncryptor
	Cfg        *config.Config
	MasterKey  []byte
	Version    string
	Watcher    watcher.Watcher
}

// NewClientCommandServer creates a new ClientCommandServer with the given configuration.
func NewClientCommandServer(cfg ClientCommandServerConfig) *ClientCommandServer {
	version := cfg.Version
	if version == "" {
		version = versionPkg.Version
	}

	return &ClientCommandServer{
		serverConn: cfg.ServerConn,
		repo:       cfg.Repo,
		encryptor:  cfg.Encryptor,
		cfg:        cfg.Cfg,
		masterKey:  cfg.MasterKey,
		watcher:    cfg.Watcher,
		version:    version,
		startedAt:  time.Now(),
	}
}

// TriggerBackup starts a backup operation on this client node.
// If a backup is already running, it returns AlreadyExists.
func (s *ClientCommandServer) TriggerBackup(ctx context.Context, req *proto.BackupRequest) (*proto.BackupResponse, error) {
	s.mu.Lock()
	if s.engine != nil {
		backupID := s.activeBackupID
		s.mu.Unlock()
		return nil, status.Errorf(codes.AlreadyExists, "backup already running: %s", backupID)
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
		initiatedBy = "server"
	}

	// Build engine config from the client's configuration.
	maxFileSize, _ := config.ParseMaxFileSize(s.cfg.Client.MaxFileSize)
	engineCfg := backup.EngineConfig{
		IncludePaths:    s.cfg.Client.IncludePaths,
		ExcludePatterns: s.cfg.Client.ExcludePatterns,
		MaxFileSize:     maxFileSize,
		EncryptionOn:    s.cfg.Encryption.Enabled,
		MasterKey:       s.masterKey,
		DatabasePath:    s.cfg.Database.Path,
	}

	engine := backup.NewBackupEngine(s.serverConn, s.repo, s.encryptor, engineCfg)
	s.engine = engine
	s.activeBackupID = "" // will be set by the engine internally
	s.mu.Unlock()

	backupReq := backup.BackupRequest{
		Level:       level,
		ClientID:    req.ClientId,
		InitiatedBy: initiatedBy,
	}

	// Start backup in a goroutine — return immediately to the server.
	go func() {
		result, err := engine.RunBackup(context.Background(), backupReq)
		if err != nil {
			slog.Error("server-triggered backup failed", "error", err)
		} else {
			slog.Info("server-triggered backup completed",
				"backup_id", result.BackupID,
				"files", result.FilesProcessed,
				"bytes_new", result.BytesNew,
			)
		}

		// Clear the running engine reference.
		s.mu.Lock()
		s.engine = nil
		s.activeBackupID = ""
		s.mu.Unlock()
	}()

	return &proto.BackupResponse{
		Status:  "started",
		Message: fmt.Sprintf("backup initiated by %s", initiatedBy),
	}, nil
}

// StopBackup stops the currently running backup on this client.
func (s *ClientCommandServer) StopBackup(ctx context.Context, req *proto.StopRequest) (*proto.StopResponse, error) {
	s.mu.Lock()
	engine := s.engine
	s.mu.Unlock()

	if engine == nil {
		return &proto.StopResponse{
			Success: true,
			Message: "no backup is currently running",
		}, nil
	}

	if err := engine.Stop(ctx); err != nil {
		return nil, MapError(err)
	}

	return &proto.StopResponse{
		Success: true,
		Message: "backup stop signal sent",
	}, nil
}

// GetStatus returns the current backup status of this client node.
func (s *ClientCommandServer) GetStatus(ctx context.Context, req *proto.StatusRequest) (*proto.StatusResponse, error) {
	s.mu.Lock()
	engine := s.engine
	s.mu.Unlock()

	if engine == nil {
		return &proto.StatusResponse{
			Status:  "idle",
			Message: "no active operations",
		}, nil
	}

	// Query the repository for the running job to get file/byte counts.
	runningStatus := model.JobRunning
	filter := db.JobFilter{
		Status: &runningStatus,
		Limit:  1,
	}

	jobs, err := s.repo.ListJobs(ctx, filter)
	if err != nil || len(jobs) == 0 {
		// Engine is set but no running job found yet (just started).
		return &proto.StatusResponse{
			Status:  "running",
			Message: "backup in progress",
		}, nil
	}

	job := jobs[0]
	return &proto.StatusResponse{
		Status:           "running",
		BackupId:         job.BackupID,
		FilesProcessed:   job.FileCount,
		BytesTransferred: job.BytesNew,
		StartedAt:        job.StartedAt.Format(time.RFC3339),
		Message:          fmt.Sprintf("backup %s in progress", job.BackupID),
	}, nil
}

// Ping returns the client version and uptime, used by the server for heartbeat tracking.
func (s *ClientCommandServer) Ping(ctx context.Context, req *proto.PingRequest) (*proto.PingResponse, error) {
	uptime := time.Since(s.startedAt).Truncate(time.Second)
	return &proto.PingResponse{
		Version: s.version,
		Uptime:  uptime.String(),
	}, nil
}

// StartWatcher starts the local file watcher on this client node.
// If no watcher is configured, it returns an error.
// If the watcher is already running, it returns a success message indicating it's already active.
func (s *ClientCommandServer) StartWatcher(ctx context.Context, req *proto.WatcherRequest) (*proto.WatcherResponse, error) {
	if s.watcher == nil {
		return &proto.WatcherResponse{
			Success: false,
			Message: "no watcher configured on this client",
		}, nil
	}

	ws := s.watcher.Status()
	if ws.Running {
		return &proto.WatcherResponse{
			Success: true,
			Message: "watcher is already running",
		}, nil
	}

	if err := s.watcher.Start(context.Background()); err != nil {
		slog.Error("failed to start watcher via server command", "error", err)
		return &proto.WatcherResponse{
			Success: false,
			Message: fmt.Sprintf("failed to start watcher: %v", err),
		}, nil
	}

	slog.Info("watcher started via server command", "client_id", req.ClientId)
	return &proto.WatcherResponse{
		Success: true,
		Message: "watcher started successfully",
	}, nil
}

// StopWatcher stops the local file watcher on this client node.
// If no watcher is configured or it's not running, returns a success response.
func (s *ClientCommandServer) StopWatcher(ctx context.Context, req *proto.WatcherRequest) (*proto.WatcherResponse, error) {
	if s.watcher == nil {
		return &proto.WatcherResponse{
			Success: true,
			Message: "no watcher configured on this client",
		}, nil
	}

	ws := s.watcher.Status()
	if !ws.Running {
		return &proto.WatcherResponse{
			Success: true,
			Message: "watcher is not running",
		}, nil
	}

	if err := s.watcher.Stop(); err != nil {
		slog.Error("failed to stop watcher via server command", "error", err)
		return &proto.WatcherResponse{
			Success: false,
			Message: fmt.Sprintf("failed to stop watcher: %v", err),
		}, nil
	}

	slog.Info("watcher stopped via server command", "client_id", req.ClientId)
	return &proto.WatcherResponse{
		Success: true,
		Message: "watcher stopped successfully",
	}, nil
}

// SetWatcher sets or replaces the watcher instance on the ClientCommandServer.
// This allows the watcher to be configured after server construction.
func (s *ClientCommandServer) SetWatcher(w watcher.Watcher) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.watcher = w
}

// Ensure ClientCommandServer satisfies the interface at compile time.
var _ proto.CommandServiceServer = (*ClientCommandServer)(nil)
