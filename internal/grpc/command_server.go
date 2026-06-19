package grpc

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/ricardopadilha/tergum/internal/backup"
	"github.com/ricardopadilha/tergum/internal/db"
	"github.com/ricardopadilha/tergum/internal/grpc/proto"
	"github.com/ricardopadilha/tergum/internal/model"
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
	backupSem       *Semaphore
	version         string
	startedAt       time.Time
}

// CommandServerConfig holds configuration for the CommandServer.
type CommandServerConfig struct {
	BackupEngine    backup.Engine
	Repo            db.Repository
	DeletionEngine  DeletionEngine
	RetentionEngine RetentionEngine
	MaxBackups      int // max concurrent backups, default 4
	Version         string
}

// NewCommandServer creates a new CommandServer with the given configuration.
func NewCommandServer(cfg CommandServerConfig) *CommandServer {
	maxBackups := cfg.MaxBackups
	if maxBackups <= 0 {
		maxBackups = 4
	}

	version := cfg.Version
	if version == "" {
		version = "3.0.0-dev"
	}

	return &CommandServer{
		backupEngine:    cfg.BackupEngine,
		repo:            cfg.Repo,
		deletionEngine:  cfg.DeletionEngine,
		retentionEngine: cfg.RetentionEngine,
		backupSem:       NewSemaphore(maxBackups),
		version:         version,
		startedAt:       time.Now(),
	}
}

// TriggerBackup initiates a backup operation. It acquires a concurrency slot,
// starts the backup in a goroutine, and immediately returns the backup ID.
func (s *CommandServer) TriggerBackup(ctx context.Context, req *proto.BackupRequest) (*proto.BackupResponse, error) {
	if req.ClientId == "" {
		return nil, MapError(&model.ConfigError{Message: "client_id is required"})
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
	uptime := time.Since(s.startedAt).Truncate(time.Second)
	return &proto.PingResponse{
		Version: s.version,
		Uptime:  uptime.String(),
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

// Ensure CommandServer satisfies the interface at compile time.
var _ proto.CommandServiceServer = (*CommandServer)(nil)
