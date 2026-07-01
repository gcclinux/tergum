package grpc

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gcclinux/tergum/internal/backup"
	"github.com/gcclinux/tergum/internal/config"
	"github.com/gcclinux/tergum/internal/crypto"
	"github.com/gcclinux/tergum/internal/db"
	"github.com/gcclinux/tergum/internal/grpc/proto"
	"github.com/gcclinux/tergum/internal/model"
	"github.com/gcclinux/tergum/internal/scheduler"
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
	configPath string
	masterKey  []byte

	// Watcher instance for server-initiated watcher control.
	// May be nil on startup if watcher was disabled in config; created on-demand via factory.
	watcher        watcher.Watcher
	watcherFactory WatcherFactory // creates a watcher + ongoing backup on demand
	ongoingBackup  *scheduler.OngoingBackup
	watchCtx       context.Context
	watchCancel    context.CancelFunc

	// Tracking fields for status and ping responses.
	version   string
	startedAt time.Time

	// Current backup metadata (set when a backup is running).
	activeBackupID string
}

// WatcherFactory is a function that creates and starts a file watcher together with
// its associated OngoingBackup processor. It is called on-demand when the server
// issues a StartWatcher command but no watcher was running yet.
type WatcherFactory func(ctx context.Context) (watcher.Watcher, *scheduler.OngoingBackup, error)

// ClientCommandServerConfig holds configuration for the ClientCommandServer.
type ClientCommandServerConfig struct {
	ServerConn     backup.ServerConnection
	Repo           db.Repository
	Encryptor      *crypto.AESEncryptor
	Cfg            *config.Config
	ConfigPath     string // path to the config file for persisting changes
	MasterKey      []byte
	Version        string
	Watcher        watcher.Watcher          // pre-started watcher (optional)
	OngoingBackup  *scheduler.OngoingBackup // pre-started ongoing backup (optional)
	WatcherFactory WatcherFactory           // factory for on-demand watcher creation
}

// NewClientCommandServer creates a new ClientCommandServer with the given configuration.
func NewClientCommandServer(cfg ClientCommandServerConfig) *ClientCommandServer {
	version := cfg.Version
	if version == "" {
		version = versionPkg.Version
	}

	return &ClientCommandServer{
		serverConn:     cfg.ServerConn,
		repo:           cfg.Repo,
		encryptor:      cfg.Encryptor,
		cfg:            cfg.Cfg,
		configPath:     cfg.ConfigPath,
		masterKey:      cfg.MasterKey,
		watcher:        cfg.Watcher,
		ongoingBackup:  cfg.OngoingBackup,
		watcherFactory: cfg.WatcherFactory,
		version:        version,
		startedAt:      time.Now(),
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
	watcherRunning := s.watcher != nil && s.watcher.Status().Running
	s.mu.Unlock()

	if engine == nil {
		return &proto.StatusResponse{
			Status:        "idle",
			Message:       "no active operations",
			WatcherActive: watcherRunning,
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
			Status:        "running",
			Message:       "backup in progress",
			WatcherActive: watcherRunning,
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
		WatcherActive:    watcherRunning,
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
// If a watcher is already configured and running, returns success immediately.
// If no watcher exists yet (e.g. watcher was disabled in config), creates one
// on-demand using the WatcherFactory so the administrator can enable it remotely.
func (s *ClientCommandServer) StartWatcher(ctx context.Context, req *proto.WatcherRequest) (*proto.WatcherResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Already running — nothing to do.
	if s.watcher != nil && s.watcher.Status().Running {
		return &proto.WatcherResponse{
			Success: true,
			Message: "watcher is already running",
		}, nil
	}

	// If we have a pre-existing (stopped) watcher, restart it.
	if s.watcher != nil {
		s.watchCtx, s.watchCancel = context.WithCancel(context.Background())
		if err := s.watcher.Start(s.watchCtx); err != nil {
			s.watchCancel()
			s.watchCtx, s.watchCancel = nil, nil
			slog.Error("failed to restart watcher via server command", "error", err)
			return &proto.WatcherResponse{
				Success: false,
				Message: fmt.Sprintf("failed to start watcher: %v", err),
			}, nil
		}
		slog.Info("watcher restarted via server command", "client_id", req.ClientId)
		s.persistWatcherEnabled(true)
		return &proto.WatcherResponse{
			Success: true,
			Message: "watcher started successfully",
		}, nil
	}

	// No watcher at all — try to create one on-demand via the factory.
	if s.watcherFactory == nil {
		return &proto.WatcherResponse{
			Success: false,
			Message: "no include paths configured — add paths with 'tergum paths add <dir>' then retry",
		}, nil
	}

	s.watchCtx, s.watchCancel = context.WithCancel(context.Background())
	fw, ongoing, err := s.watcherFactory(s.watchCtx)
	if err != nil {
		s.watchCancel()
		s.watchCtx, s.watchCancel = nil, nil
		slog.Error("failed to create watcher on-demand via server command", "error", err)
		return &proto.WatcherResponse{
			Success: false,
			Message: fmt.Sprintf("failed to create watcher: %v", err),
		}, nil
	}

	s.watcher = fw
	s.ongoingBackup = ongoing
	slog.Info("watcher created and started on-demand via server command", "client_id", req.ClientId)

	// Persist watcher enabled state to config so it survives a service restart.
	s.persistWatcherEnabled(true)

	return &proto.WatcherResponse{
		Success: true,
		Message: "watcher created and started successfully",
	}, nil
}

// StopWatcher stops the local file watcher on this client node.
// If no watcher is configured or it's not running, returns a success response.
// Also stops the associated OngoingBackup processor if one is running.
func (s *ClientCommandServer) StopWatcher(ctx context.Context, req *proto.WatcherRequest) (*proto.WatcherResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

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

	// Stop the ongoing backup processor first (it drains the watcher channel).
	if s.ongoingBackup != nil {
		s.ongoingBackup.Stop()
		s.ongoingBackup = nil
	}

	// Cancel the watch context to signal all goroutines.
	if s.watchCancel != nil {
		s.watchCancel()
		s.watchCtx, s.watchCancel = nil, nil
	}

	if err := s.watcher.Stop(); err != nil {
		slog.Error("failed to stop watcher via server command", "error", err)
		return &proto.WatcherResponse{
			Success: false,
			Message: fmt.Sprintf("failed to stop watcher: %v", err),
		}, nil
	}

	slog.Info("watcher stopped via server command", "client_id", req.ClientId)
	s.persistWatcherEnabled(false)
	return &proto.WatcherResponse{
		Success: true,
		Message: "watcher stopped successfully",
	}, nil
}

// WatcherRunning implements HeartbeatStateProvider — reports whether the file watcher is active.
func (s *ClientCommandServer) WatcherRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.watcher != nil && s.watcher.Status().Running
}

// LastBackupTime implements HeartbeatStateProvider — returns the most recent
// completed backup timestamp in RFC3339, or "" if unknown.
func (s *ClientCommandServer) LastBackupTime() string {
	if s.repo == nil {
		return ""
	}
	completed := model.JobCompleted
	jobs, err := s.repo.ListJobs(context.Background(), db.JobFilter{Status: &completed, Limit: 1})
	if err != nil || len(jobs) == 0 {
		return ""
	}
	if jobs[0].FinishedAt != nil {
		return jobs[0].FinishedAt.UTC().Format(time.RFC3339)
	}
	return ""
}

// persistWatcherEnabled saves the watcher.enabled state to the config file
// so that a remotely started/stopped watcher survives a service restart.
func (s *ClientCommandServer) persistWatcherEnabled(enabled bool) {
	if s.cfg == nil || s.configPath == "" {
		return
	}
	s.cfg.Watcher.Enabled = enabled
	if err := config.Save(s.configPath, s.cfg); err != nil {
		slog.Warn("failed to persist watcher enabled state to config",
			"enabled", enabled, "error", err)
	} else {
		slog.Info("persisted watcher enabled state to config",
			"enabled", enabled, "path", s.configPath)
	}
}

// SetWatcher sets or replaces the watcher instance on the ClientCommandServer.
// This allows the watcher to be configured after server construction.
func (s *ClientCommandServer) SetWatcher(w watcher.Watcher) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.watcher = w
}

// PushRestore handles incoming file data from the server for cross-client restore.
// The server decrypts files from the source client's backup and streams them here.
// Stream protocol:
//  1. First chunk: FileHeader with FilePath (destination), file metadata
//  2. Data chunks: raw decrypted file content
//  3. Trailer: blake3 hash for verification
//  4. Repeat 1-3 for each file
//
// After all files are received, the client returns a PushRestoreResponse summary.
func (s *ClientCommandServer) PushRestore(stream proto.CommandService_PushRestoreServer) error {
	var filesReceived int64
	var filesFailed int64
	var bytesTotal int64

	// State for current file being received.
	var currentHeader *proto.FileHeader
	var currentData []byte
	var destBase string // base destination directory (from first header's metadata)

	for {
		chunk, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			slog.Error("push restore: stream receive error", "error", err)
			return status.Errorf(codes.Internal, "stream error: %v", err)
		}

		switch {
		case chunk.GetHeader() != nil:
			// Start of a new file.
			currentHeader = chunk.GetHeader()
			currentData = nil

			// Use the FileName field to pass the dest base path on the first file only.
			// After that, each header's FilePath defines the destination.
			if destBase == "" && currentHeader.Os != "" {
				// The Os field carries the dest base path (repurposed for PushRestore).
				destBase = currentHeader.Os
			}

		case chunk.GetData() != nil:
			// Accumulate file data.
			currentData = append(currentData, chunk.GetData()...)

		case chunk.GetTrailer() != nil:
			// End of current file — write to disk.
			if currentHeader == nil {
				filesFailed++
				continue
			}

			// Determine destination path.
			// If destBase is set, resolve the file's path under it (same logic as CLI --dest).
			destPath := currentHeader.FilePath
			if destBase != "" && destPath != "" {
				// Strip volume/leading slashes and join under destBase.
				vol := filepath.VolumeName(destPath)
				rel := destPath[len(vol):]
				for len(rel) > 0 && (rel[0] == '/' || rel[0] == '\\') {
					rel = rel[1:]
				}
				destPath = filepath.Join(destBase, rel)
			} else if destBase != "" && destPath == "" {
				destPath = filepath.Join(destBase, currentHeader.FileName)
			}
			if destPath == "" {
				slog.Error("push restore: no destination path for file", "file_name", currentHeader.FileName)
				filesFailed++
				currentHeader = nil
				currentData = nil
				continue
			}

			// Ensure destination directory exists.
			destDir := filepath.Dir(destPath)
			if err := os.MkdirAll(destDir, 0o755); err != nil {
				slog.Error("push restore: create directory failed", "path", destDir, "error", err)
				filesFailed++
				currentHeader = nil
				currentData = nil
				continue
			}

			// Determine file permissions.
			perm := fs.FileMode(0o644)
			if currentHeader.Permissions != 0 {
				perm = fs.FileMode(currentHeader.Permissions)
			}

			// Handle symlink restoration.
			if currentHeader.Symlink && currentHeader.SymlinkTarget != "" {
				os.Remove(destPath)
				if err := os.Symlink(currentHeader.SymlinkTarget, destPath); err != nil {
					slog.Error("push restore: create symlink failed", "path", destPath, "error", err)
					filesFailed++
				} else {
					filesReceived++
				}
				currentHeader = nil
				currentData = nil
				continue
			}

			// Write file.
			if err := os.WriteFile(destPath, currentData, perm); err != nil {
				slog.Error("push restore: write file failed", "path", destPath, "error", err)
				filesFailed++
			} else {
				bytesTotal += int64(len(currentData))
				filesReceived++
				slog.Debug("push restore: file written", "path", destPath, "size", len(currentData))
			}

			currentHeader = nil
			currentData = nil
		}
	}

	msg := fmt.Sprintf("push restore complete: %d files received, %d failed", filesReceived, filesFailed)
	slog.Info(msg)

	return stream.SendAndClose(&proto.PushRestoreResponse{
		Success:       filesFailed == 0,
		FilesReceived: filesReceived,
		BytesTotal:    bytesTotal,
		FilesFailed:   filesFailed,
		Message:       msg,
	})
}

// Ensure ClientCommandServer satisfies the interface at compile time.
var _ proto.CommandServiceServer = (*ClientCommandServer)(nil)
