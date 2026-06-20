// Package server provides the main Tergum server wiring that starts all subsystems
// (gRPC, metrics, retention, scheduler) and orchestrates graceful shutdown.
package server

import (
	"context"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/ricardopadilha/tergum/internal/backup"
	"github.com/ricardopadilha/tergum/internal/config"
	"github.com/ricardopadilha/tergum/internal/crypto"
	"github.com/ricardopadilha/tergum/internal/db"
	grpcpkg "github.com/ricardopadilha/tergum/internal/grpc"
	"github.com/ricardopadilha/tergum/internal/grpc/proto"
	"github.com/ricardopadilha/tergum/internal/model"
	"github.com/ricardopadilha/tergum/internal/observe"
	"github.com/ricardopadilha/tergum/internal/retention"
	"github.com/ricardopadilha/tergum/internal/scheduler"
	"github.com/ricardopadilha/tergum/internal/storage"
	tlspkg "github.com/ricardopadilha/tergum/internal/tls"
	"github.com/ricardopadilha/tergum/internal/webui"
)

// Version is the current Tergum version, set at build time.
var Version = "3.0.0-dev"

// Server holds all subsystems and manages their lifecycle.
type Server struct {
	cfg *config.Config

	// gRPC servers
	grpcCmd  *grpc.Server
	grpcData *grpc.Server

	// Metrics
	metrics *observe.MetricsServer

	// Web UI
	webUI *webui.Server

	// Background subsystems
	retentionEngine retention.Engine
	sched           scheduler.Scheduler

	// Infrastructure
	repo  db.Repository
	store storage.Store

	// Listeners (stored for shutdown)
	cmdListener  net.Listener
	dataListener net.Listener

	// Lifecycle
	logger   *slog.Logger
	stopOnce sync.Once
}

// New creates a new Server from the given configuration. It initializes all
// subsystems but does not start them. Call Start() to begin serving.
func New(cfg *config.Config) (*Server, error) {
	logger := observe.Logger("server")

	s := &Server{
		cfg:    cfg,
		logger: logger,
	}

	return s, nil
}

// Start initializes all subsystems and begins serving. It blocks until a
// SIGTERM or SIGINT signal is received, then performs graceful shutdown.
// Returns exit code 10 on graceful shutdown.
func (s *Server) Start(ctx context.Context) error {
	// Setup logging.
	if err := observe.SetupLogging(s.cfg.Logging.Level, s.cfg.Logging.Format); err != nil {
		return fmt.Errorf("setup logging: %w", err)
	}

	// Initialize database.
	repo, err := db.NewRepository(s.cfg.Database.Path, true)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	s.repo = repo

	// Initialize CAS store.
	storageDir := s.cfg.StorageDir()
	cas := storage.NewCAS(storageDir, repo)
	s.store = cas

	// Load TLS credentials for mTLS.
	tlsMgr := tlspkg.NewManager()
	serverTLS, err := tlsMgr.LoadServerTLS(s.cfg.TLS.CACert, s.cfg.TLS.Cert, s.cfg.TLS.Key)
	if err != nil {
		return fmt.Errorf("load server TLS: %w", err)
	}

	// Initialize engines.
	retEngine := retention.New(repo, cas)
	s.retentionEngine = retEngine

	// Create a stub backup engine for the command server.
	// The actual backup engine on the server side handles incoming streams.
	backupEng := &noopBackupEngine{}

	// Build gRPC command server.
	cmdServer := grpcpkg.NewCommandServer(grpcpkg.CommandServerConfig{
		BackupEngine:    backupEng,
		Repo:            repo,
		DeletionEngine:  nil, // wired separately when deletion adapter is complete
		RetentionEngine: retEngine,
		MaxBackups:      s.cfg.Backup.MaxConcurrentUploads,
		Version:         Version,
	})

	// Build gRPC data server.
	clientsDir := clientsDirFromDB(s.cfg.Database.Path)
	dataServer := grpcpkg.NewDataServer(grpcpkg.DataServerConfig{
		Store:       cas,
		Repo:        repo,
		ClientsDir:  clientsDir,
		MaxRestores: s.cfg.Backup.MaxConcurrentDownloads,
	})

	// Create gRPC servers with TLS credentials.
	creds := credentials.NewTLS(serverTLS)
	s.grpcCmd = grpc.NewServer(grpc.Creds(creds))
	s.grpcData = grpc.NewServer(grpc.Creds(creds))

	proto.RegisterCommandServiceServer(s.grpcCmd, cmdServer)
	proto.RegisterDataServiceServer(s.grpcData, dataServer)

	// Start gRPC command server.
	cmdAddr := fmt.Sprintf(":%d", s.cfg.Server.CommandPort)
	s.cmdListener, err = net.Listen("tcp", cmdAddr)
	if err != nil {
		return fmt.Errorf("listen command port %d: %w", s.cfg.Server.CommandPort, err)
	}
	s.logger.Info("gRPC command server listening", "port", s.cfg.Server.CommandPort)

	go func() {
		if err := s.grpcCmd.Serve(s.cmdListener); err != nil {
			s.logger.Error("gRPC command server error", "error", err)
		}
	}()

	// Start gRPC data server.
	dataAddr := fmt.Sprintf(":%d", s.cfg.Server.DataPort)
	s.dataListener, err = net.Listen("tcp", dataAddr)
	if err != nil {
		return fmt.Errorf("listen data port %d: %w", s.cfg.Server.DataPort, err)
	}
	s.logger.Info("gRPC data server listening", "port", s.cfg.Server.DataPort)

	go func() {
		if err := s.grpcData.Serve(s.dataListener); err != nil {
			s.logger.Error("gRPC data server error", "error", err)
		}
	}()

	// Start metrics server.
	if s.cfg.Metrics.Enabled {
		s.metrics = observe.NewMetricsServer(s.cfg.Metrics.Port, Version)
		metricsCtx, metricsCancel := context.WithCancel(ctx)
		defer metricsCancel()

		go func() {
			s.logger.Info("metrics server listening", "port", s.cfg.Metrics.Port)
			if err := s.metrics.Start(metricsCtx); err != nil {
				s.logger.Error("metrics server error", "error", err)
			}
		}()
	}

	// Start web UI server.
	if s.cfg.WebUI.Enabled {
		var webuiOpts []webui.ServerOption
		webuiOpts = append(webuiOpts, webui.WithLogger(observe.Logger("webui")))
		webuiOpts = append(webuiOpts, webui.WithRepository(s.repo))
		webuiOpts = append(webuiOpts, webui.WithConfigPath(config.DefaultConfigPath()))
		webuiOpts = append(webuiOpts, webui.WithFullConfig(s.cfg))

		// Enable web-triggered backups if TERGUM_PASSPHRASE is set.
		if passphrase := os.Getenv("TERGUM_PASSPHRASE"); passphrase != "" {
			masterKey, err := loadMasterKeyFromEnv(s.cfg)
			if err == nil {
				trigger := webui.NewLocalBackupTrigger(s.repo, s.cfg.StorageDir(), masterKey, s.cfg.Encryption.Enabled)
				webuiOpts = append(webuiOpts, webui.WithBackupTrigger(trigger))
				s.logger.Info("web backup trigger enabled (TERGUM_PASSPHRASE set)")

				// Also enable watcher controller.
				excludes, _ := s.repo.ListExcludePatterns(context.Background())
				wc := webui.NewLocalWatcherController(webui.LocalWatcherConfig{
					Repo:            s.repo,
					StorageDir:      s.cfg.StorageDir(),
					MasterKey:       masterKey,
					EncEnabled:      s.cfg.Encryption.Enabled,
					DebounceMs:      s.cfg.Watcher.DebounceMs,
					StabilitySec:    s.cfg.Watcher.StabilitySeconds,
					BatchMinutes:    s.cfg.Watcher.BatchIntervalMinutes,
					ExcludePatterns: excludes,
				})
				webuiOpts = append(webuiOpts, webui.WithWatcherController(wc))
			} else {
				s.logger.Warn("web backup trigger disabled: cannot derive key", "error", err)
			}
		}
		// Note: The gRPC mTLS certificates use Ed25519 which browsers don't support.
		// The web UI runs on plain HTTP. For production, put it behind a reverse proxy
		// with a browser-compatible certificate (ECDSA/RSA).

		uiServer, err := webui.NewServer(
			s.cfg.WebUI,
			s.cfg.WebUI.Username,
			s.cfg.WebUI.Password,
			webuiOpts...,
		)
		if err != nil {
			return fmt.Errorf("create web UI server: %w", err)
		}
		s.webUI = uiServer

		go func() {
			s.logger.Info("web UI server listening", "port", s.cfg.WebUI.Port)
			if err := s.webUI.Start(); err != nil {
				s.logger.Error("web UI server error", "error", err)
			}
		}()
	}

	// Start retention engine (hourly ticker).
	retCtx, retCancel := context.WithCancel(ctx)
	defer retCancel()
	go s.runRetentionLoop(retCtx)

	// Start scheduler.
	if s.cfg.Scheduler.FullBackupCron != "" || s.cfg.Scheduler.AutoBackupCron != "" {
		trigger := &localBackupTrigger{cmdServer: cmdServer}
		s.sched = scheduler.New(s.cfg.Scheduler, trigger, nil, s.logger)
		if err := s.sched.Start(ctx); err != nil {
			s.logger.Warn("scheduler start failed", "error", err)
		} else {
			s.logger.Info("scheduler started")
		}
	}

	s.logger.Info("tergum server started",
		"command_port", s.cfg.Server.CommandPort,
		"data_port", s.cfg.Server.DataPort,
		"webui_port", s.cfg.WebUI.Port,
		"metrics_port", s.cfg.Metrics.Port,
	)

	// Wait for shutdown signal.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	select {
	case sig := <-sigCh:
		s.logger.Info("received shutdown signal", "signal", sig)
	case <-ctx.Done():
		s.logger.Info("context cancelled, shutting down")
	}

	// Graceful shutdown.
	return s.Stop()
}

// Stop gracefully shuts down all subsystems: drains gRPC connections,
// stops the retention engine and scheduler, stops metrics, and flushes logs.
func (s *Server) Stop() error {
	var stopErr error

	s.stopOnce.Do(func() {
		s.logger.Info("starting graceful shutdown")

		// Stop scheduler first.
		if s.sched != nil {
			if err := s.sched.Stop(); err != nil {
				s.logger.Error("scheduler stop error", "error", err)
			}
		}

		// Gracefully stop gRPC servers (completes in-flight RPCs).
		if s.grpcCmd != nil {
			s.grpcCmd.GracefulStop()
			s.logger.Info("gRPC command server stopped")
		}
		if s.grpcData != nil {
			s.grpcData.GracefulStop()
			s.logger.Info("gRPC data server stopped")
		}

		// Stop metrics server.
		if s.metrics != nil {
			if err := s.metrics.Stop(); err != nil {
				s.logger.Error("metrics server stop error", "error", err)
			}
			s.logger.Info("metrics server stopped")
		}

		// Stop web UI server.
		if s.webUI != nil {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := s.webUI.Shutdown(shutdownCtx); err != nil {
				s.logger.Error("web UI server stop error", "error", err)
			}
			s.logger.Info("web UI server stopped")
		}

		// Close database.
		if s.repo != nil {
			if err := s.repo.Close(); err != nil {
				s.logger.Error("database close error", "error", err)
			}
		}

		s.logger.Info("graceful shutdown complete")
	})

	return stopErr
}

// runRetentionLoop runs the retention engine every hour.
func (s *Server) runRetentionLoop(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	s.logger.Info("retention engine started (hourly)")

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("retention engine stopped")
			return
		case <-ticker.C:
			result, err := s.retentionEngine.Evaluate(ctx, false)
			if err != nil {
				s.logger.Error("retention evaluation failed", "error", err)
				continue
			}
			if result.EntriesExpired > 0 {
				s.logger.Info("retention evaluation complete",
					"entries_expired", result.EntriesExpired,
					"bytes_freed", result.BytesFreed,
					"files_deleted", result.FilesDeleted,
				)
			}
		}
	}
}

// storagePathFromDB derives the CAS storage path from the database path.
// Convention: storage/ directory is a sibling of the database file.
func storagePathFromDB(dbPath string) string {
	dir := filepath.Dir(dbPath)
	return filepath.Join(dir, "storage")
}

// clientsDirFromDB derives the clients/ directory from the database path.
func clientsDirFromDB(dbPath string) string {
	dir := filepath.Dir(dbPath)
	return filepath.Join(dir, "clients")
}

// noopBackupEngine is a stub backup engine for the server side.
// The server doesn't initiate backups itself; it receives them via gRPC streams.
type noopBackupEngine struct{}

func (e *noopBackupEngine) RunBackup(ctx context.Context, req backup.BackupRequest) (*backup.BackupResult, error) {
	return nil, fmt.Errorf("server does not initiate backups directly")
}

func (e *noopBackupEngine) Stop(ctx context.Context) error {
	return nil
}

// localBackupTrigger adapts the CommandServer to the scheduler.BackupTrigger interface.
type localBackupTrigger struct {
	cmdServer *grpcpkg.CommandServer
}

func (t *localBackupTrigger) TriggerBackup(ctx context.Context, level model.BackupLevel, clientID string) error {
	var protoLevel proto.BackupLevel
	switch level {
	case model.BackupLevelFull:
		protoLevel = proto.BackupLevel_FULL
	case model.BackupLevelOngoing:
		protoLevel = proto.BackupLevel_ONGOING
	default:
		protoLevel = proto.BackupLevel_AUTO
	}

	_, err := t.cmdServer.TriggerBackup(ctx, &proto.BackupRequest{
		ClientId:    clientID,
		Level:       protoLevel,
		InitiatedBy: "scheduler",
	})
	return err
}

// LoadTLSConfig returns the server's TLS configuration from config (useful for testing).
func LoadTLSConfig(cfg *config.Config) (*tls.Config, error) {
	tlsMgr := tlspkg.NewManager()
	return tlsMgr.LoadServerTLS(cfg.TLS.CACert, cfg.TLS.Cert, cfg.TLS.Key)
}

// loadMasterKeyFromEnv derives the master key from the TERGUM_PASSPHRASE env var and stored salt.
func loadMasterKeyFromEnv(cfg *config.Config) ([]byte, error) {
	passphrase := os.Getenv("TERGUM_PASSPHRASE")
	if passphrase == "" {
		return nil, fmt.Errorf("TERGUM_PASSPHRASE not set")
	}

	configDir := filepath.Dir(cfg.Database.Path)
	saltPath := filepath.Join(configDir, "salt")

	saltHex, err := os.ReadFile(saltPath)
	if err != nil {
		return nil, fmt.Errorf("cannot read salt file: %w", err)
	}

	salt, err := hex.DecodeString(strings.TrimSpace(string(saltHex)))
	if err != nil {
		return nil, fmt.Errorf("invalid salt: %w", err)
	}

	enc := crypto.NewEncryptor()
	return enc.DeriveKey(passphrase, salt)
}
