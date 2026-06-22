// Package server provides the main Tergum server wiring that starts all subsystems
// (gRPC, metrics, retention, scheduler) and orchestrates graceful shutdown.
package server

import (
	"context"
	"crypto/tls"
	"database/sql"
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

	"github.com/gcclinux/tergum/internal/backup"
	"github.com/gcclinux/tergum/internal/config"
	"github.com/gcclinux/tergum/internal/connection"
	"github.com/gcclinux/tergum/internal/crypto"
	"github.com/gcclinux/tergum/internal/db"
	grpcpkg "github.com/gcclinux/tergum/internal/grpc"
	"github.com/gcclinux/tergum/internal/grpc/proto"
	"github.com/gcclinux/tergum/internal/model"
	"github.com/gcclinux/tergum/internal/observe"
	"github.com/gcclinux/tergum/internal/registry"
	"github.com/gcclinux/tergum/internal/retention"
	"github.com/gcclinux/tergum/internal/scheduler"
	"github.com/gcclinux/tergum/internal/storage"
	tlspkg "github.com/gcclinux/tergum/internal/tls"
	"github.com/gcclinux/tergum/internal/watcher"
	"github.com/gcclinux/tergum/internal/webui"

	_ "modernc.org/sqlite"
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
	repo       db.Repository
	store      storage.Store
	registry   *registry.Registry
	registryDB *sql.DB

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

	// If role is "client", start the client daemon flow instead of the server.
	if s.cfg.Node.Role == "client" {
		return s.startClient(ctx)
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

	// Initialize client registry.
	// The registry uses its own database connection to the same SQLite file,
	// managing the client_registry and missed_schedules tables.
	registryDB, err := openRegistryDB(s.cfg.Database.Path)
	if err != nil {
		return fmt.Errorf("open registry database: %w", err)
	}
	reg, err := registry.New(registry.Config{
		DB:     registryDB,
		Logger: observe.Logger("registry"),
	})
	if err != nil {
		return fmt.Errorf("create client registry: %w", err)
	}
	s.registry = reg
	s.registryDB = registryDB

	// Start registry background offline checker.
	regCtx, regCancel := context.WithCancel(ctx)
	defer regCancel()
	go reg.Start(regCtx)

	// Load client TLS for connecting to clients (server acts as client when sending RPCs).
	clientTLS, err := tlsMgr.LoadClientTLS(s.cfg.TLS.CACert, s.cfg.TLS.Cert, s.cfg.TLS.Key)
	if err != nil {
		s.logger.Warn("could not load client TLS for remote connector (client management disabled)", "error", err)
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
		Registry:        reg,
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

		// Wire client registry and remote connector for client management.
		webuiOpts = append(webuiOpts, webui.WithClientRegistry(reg))
		if clientTLS != nil {
			connector := webui.NewRemoteClientConnector(webui.RemoteClientConnectorConfig{
				Registry: reg,
				TLSCfg:   clientTLS,
				Logger:   observe.Logger("client-connector"),
			})
			webuiOpts = append(webuiOpts, webui.WithClientConnector(connector))
			s.logger.Info("remote client connector enabled")
		}

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

		// Close registry database connection.
		if s.registryDB != nil {
			if err := s.registryDB.Close(); err != nil {
				s.logger.Error("registry database close error", "error", err)
			}
		}

		s.logger.Info("graceful shutdown complete")
	})

	return stopErr
}

// startClient runs the client daemon flow: opens the local database, derives the
// master key, starts a client-side CommandService, connects to the remote server,
// registers, starts heartbeat, optionally starts file watcher, and waits for shutdown.
func (s *Server) startClient(ctx context.Context) error {
	// 1. Open local database.
	repo, err := db.NewRepository(s.cfg.Database.Path, s.cfg.Database.WALMode)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	s.repo = repo

	// 2. Derive master key from TERGUM_PASSPHRASE.
	masterKey, err := loadMasterKeyFromEnv(s.cfg)
	if err != nil {
		return fmt.Errorf("derive master key: %w", err)
	}

	var encryptor *crypto.AESEncryptor
	if s.cfg.Encryption.Enabled {
		encryptor = crypto.NewEncryptor()
	}

	// 3. Load TLS for the client's own CommandService listener (server TLS for accepting connections).
	tlsMgr := tlspkg.NewManager()
	serverTLS, err := tlsMgr.LoadServerTLS(s.cfg.TLS.CACert, s.cfg.TLS.Cert, s.cfg.TLS.Key)
	if err != nil {
		return fmt.Errorf("load server TLS for client listener: %w", err)
	}

	// Load client TLS to get the clientID (certificate CN) and connect to the server.
	clientTLS, clientID, err := connection.LoadClientTLS(s.cfg)
	if err != nil {
		return fmt.Errorf("load client TLS: %w", err)
	}

	// Create remote server connection for backup operations.
	serverConn, err := connection.NewServerConnection(s.cfg)
	if err != nil {
		return fmt.Errorf("create server connection: %w", err)
	}

	// Build the ClientCommandServer.
	cmdHandler := grpcpkg.NewClientCommandServer(grpcpkg.ClientCommandServerConfig{
		ServerConn: serverConn,
		Repo:       repo,
		Encryptor:  encryptor,
		Cfg:        s.cfg,
		MasterKey:  masterKey,
		Version:    Version,
	})

	// Start gRPC server for client-side CommandService on :7400.
	creds := credentials.NewTLS(serverTLS)
	s.grpcCmd = grpc.NewServer(grpc.Creds(creds))
	proto.RegisterCommandServiceServer(s.grpcCmd, cmdHandler)

	cmdAddr := fmt.Sprintf(":%d", s.cfg.Server.CommandPort)
	s.cmdListener, err = net.Listen("tcp", cmdAddr)
	if err != nil {
		return fmt.Errorf("listen client command port %d: %w", s.cfg.Server.CommandPort, err)
	}
	s.logger.Info("client CommandService listening", "port", s.cfg.Server.CommandPort)

	go func() {
		if err := s.grpcCmd.Serve(s.cmdListener); err != nil {
			s.logger.Error("client gRPC server error", "error", err)
		}
	}()

	// 4. Connect to remote server.
	serverClient, err := grpcpkg.Connect(
		ctx,
		s.cfg.Server.Address,
		s.cfg.Server.CommandPort,
		s.cfg.Server.DataPort,
		clientTLS,
	)
	if err != nil {
		return fmt.Errorf("connect to server: %w", err)
	}

	// Determine this client's advertised address for the server to call back.
	hostname := s.cfg.Node.Hostname
	if hostname == "" {
		hostname, _ = os.Hostname()
	}
	clientAddress := fmt.Sprintf("%s:%d", hostname, s.cfg.Server.CommandPort)

	// 5. Send RegisterClient RPC.
	_, regErr := serverClient.RegisterClient(ctx, clientID, clientAddress)
	if regErr != nil {
		// Log but don't fail â€” the heartbeat loop will retry registration.
		s.logger.Warn("initial client registration failed (will retry via heartbeat)",
			"error", regErr,
		)
	} else {
		s.logger.Info("registered with server",
			"client_id", clientID,
			"address", clientAddress,
		)
	}

	// 6. Start heartbeat loop.
	heartbeatCtx, heartbeatCancel := context.WithCancel(ctx)
	go grpcpkg.StartHeartbeat(heartbeatCtx, serverClient, clientID, clientAddress, 30*time.Second)

	// 7. If watcher is enabled, start file watcher with RemoteServerConnection.
	var fw watcher.Watcher
	var ongoing *scheduler.OngoingBackup
	var watchCancel context.CancelFunc

	if s.cfg.Watcher.Enabled {
		// Resolve include and exclude paths.
		includePaths, _ := repo.ListIncludePaths(ctx)
		if len(includePaths) == 0 {
			includePaths = s.cfg.Client.IncludePaths
		}

		excludePatterns, _ := repo.ListExcludePatterns(ctx)
		if len(excludePatterns) == 0 {
			excludePatterns = s.cfg.Client.ExcludePatterns
		}

		if len(includePaths) > 0 {
			watcherCfg := watcher.Config{
				DebounceMs:       s.cfg.Watcher.DebounceMs,
				StabilitySeconds: s.cfg.Watcher.StabilitySeconds,
				ExcludePatterns:  excludePatterns,
				Repository:       repo,
			}

			fileWatcher, fwErr := watcher.NewFileWatcher(watcherCfg)
			if fwErr != nil {
				s.logger.Warn("failed to create file watcher", "error", fwErr)
			} else {
				var watchCtx context.Context
				watchCtx, watchCancel = context.WithCancel(ctx)

				if startErr := fileWatcher.Start(watchCtx); startErr != nil {
					s.logger.Warn("failed to start file watcher", "error", startErr)
					watchCancel()
				} else {
					fw = fileWatcher
					cmdHandler.SetWatcher(fw)

					// Start ongoing backup processor.
					batchInterval := time.Duration(s.cfg.Watcher.BatchIntervalMinutes) * time.Minute
					if batchInterval <= 0 {
						batchInterval = 5 * time.Minute
					}

					ongoing = scheduler.NewOngoingBackup(scheduler.OngoingConfig{
						Watcher:       fw,
						Server:        serverConn,
						Repo:          repo,
						Encryptor:     encryptor,
						MasterKey:     masterKey,
						BatchInterval: batchInterval,
					})

					if startErr := ongoing.Start(watchCtx); startErr != nil {
						s.logger.Warn("failed to start ongoing backup", "error", startErr)
					} else {
						s.logger.Info("file watcher and ongoing backup started",
							"paths", len(includePaths),
							"batch_interval", batchInterval,
						)
					}
				}
			}
		} else {
			s.logger.Warn("watcher enabled but no include paths configured")
		}
	}

	s.logger.Info("tergum client daemon started",
		"client_id", clientID,
		"server", s.cfg.Server.Address,
		"command_port", s.cfg.Server.CommandPort,
	)

	// 8. Wait for SIGTERM/SIGINT, then graceful shutdown.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	select {
	case sig := <-sigCh:
		s.logger.Info("received shutdown signal", "signal", sig)
	case <-ctx.Done():
		s.logger.Info("context cancelled, shutting down")
	}

	// Graceful shutdown.
	s.logger.Info("starting client graceful shutdown")

	// Stop heartbeat.
	heartbeatCancel()

	// Stop watcher and ongoing backup.
	if ongoing != nil {
		ongoing.Stop()
	}
	if fw != nil {
		fw.Stop()
	}
	if watchCancel != nil {
		watchCancel()
	}

	// Stop gRPC command server.
	if s.grpcCmd != nil {
		s.grpcCmd.GracefulStop()
		s.logger.Info("client gRPC server stopped")
	}

	// Close database.
	if s.repo != nil {
		if err := s.repo.Close(); err != nil {
			s.logger.Error("database close error", "error", err)
		}
	}

	s.logger.Info("client graceful shutdown complete")
	return nil
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
	masterKey, err := enc.DeriveKey(passphrase, salt)
	if err != nil {
		return nil, fmt.Errorf("key derivation failed: %w", err)
	}

	// Verify derived master key against key_verify if it exists
	verifyPath := filepath.Join(configDir, "key_verify")
	if verifyData, err := os.ReadFile(verifyPath); err == nil {
		if ok, err := enc.VerifyMasterKey(masterKey, string(verifyData)); err != nil || !ok {
			return nil, fmt.Errorf("invalid passphrase: key verification failed")
		}
	}

	return masterKey, nil
}

// openRegistryDB opens a SQLite connection to the same database file for the
// client registry. The registry manages its own tables (client_registry, missed_schedules)
// and needs a separate connection to avoid lifecycle conflicts with the main repository.
func openRegistryDB(dbPath string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite for registry: %w", err)
	}

	// Enable WAL mode for better concurrency with the main repo connection.
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("set WAL mode: %w", err)
	}

	return db, nil
}
