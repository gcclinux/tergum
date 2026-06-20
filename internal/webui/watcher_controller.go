package webui

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/ricardopadilha/tergum/internal/backup"
	"github.com/ricardopadilha/tergum/internal/crypto"
	"github.com/ricardopadilha/tergum/internal/db"
	"github.com/ricardopadilha/tergum/internal/scheduler"
	"github.com/ricardopadilha/tergum/internal/watcher"
)

// LocalWatcherController manages a file watcher lifecycle from the Web UI.
type LocalWatcherController struct {
	repo             db.Repository
	storageDir       string
	masterKey        []byte
	encEnabled       bool
	debounceMs       int
	stabilitySec     int
	batchMinutes     int
	excludePatterns  []string

	mu       sync.Mutex
	fw       *watcher.FileWatcher
	ongoing  *scheduler.OngoingBackup
	ctx      context.Context
	cancel   context.CancelFunc
	running  bool
}

// LocalWatcherConfig holds configuration for the watcher controller.
type LocalWatcherConfig struct {
	Repo            db.Repository
	StorageDir      string
	MasterKey       []byte
	EncEnabled      bool
	DebounceMs      int
	StabilitySec    int
	BatchMinutes    int
	ExcludePatterns []string
}

// NewLocalWatcherController creates a new watcher controller.
func NewLocalWatcherController(cfg LocalWatcherConfig) *LocalWatcherController {
	return &LocalWatcherController{
		repo:            cfg.Repo,
		storageDir:      cfg.StorageDir,
		masterKey:       cfg.MasterKey,
		encEnabled:      cfg.EncEnabled,
		debounceMs:      cfg.DebounceMs,
		stabilitySec:    cfg.StabilitySec,
		batchMinutes:    cfg.BatchMinutes,
		excludePatterns: cfg.ExcludePatterns,
	}
}

// IsRunning returns whether the watcher is currently active.
func (c *LocalWatcherController) IsRunning() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.running
}

// StartWatcher starts the file watcher and ongoing backup processor.
func (c *LocalWatcherController) StartWatcher() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.running {
		return fmt.Errorf("watcher is already running")
	}

	ctx := context.Background()

	// Get include paths from DB.
	includePaths, err := c.repo.ListIncludePaths(ctx)
	if err != nil {
		return fmt.Errorf("listing paths: %w", err)
	}
	if len(includePaths) == 0 {
		return fmt.Errorf("no include paths configured")
	}

	// Get exclude patterns from DB.
	excludePatterns, _ := c.repo.ListExcludePatterns(ctx)
	if len(excludePatterns) == 0 {
		excludePatterns = c.excludePatterns
	}

	// Create file watcher.
	watcherCfg := watcher.Config{
		DebounceMs:       c.debounceMs,
		StabilitySeconds: c.stabilitySec,
		ExcludePatterns:  excludePatterns,
		Repository:       c.repo,
	}

	fw, err := watcher.NewFileWatcher(watcherCfg)
	if err != nil {
		return fmt.Errorf("creating watcher: %w", err)
	}

	// Start watcher first (sets internal context needed by AddPath).
	c.ctx, c.cancel = context.WithCancel(ctx)
	if err := fw.Start(c.ctx); err != nil {
		return fmt.Errorf("starting watcher: %w", err)
	}

	// Add paths after start so the internal context is available.
	for _, p := range includePaths {
		if err := fw.AddPath(p, true); err != nil {
			slog.Warn("watcher: cannot watch path", "path", p, "error", err)
		}
	}

	// Create ongoing backup.
	serverConn := &backup.LocalServerConnection{
		StorageDir: c.storageDir,
		Repo:       c.repo,
	}

	var encryptor *crypto.AESEncryptor
	if c.encEnabled {
		encryptor = crypto.NewEncryptor()
	}

	batchInterval := time.Duration(c.batchMinutes) * time.Minute
	if batchInterval <= 0 {
		batchInterval = 5 * time.Minute
	}

	ongoing := scheduler.NewOngoingBackup(scheduler.OngoingConfig{
		Watcher:       fw,
		Server:        serverConn,
		Repo:          c.repo,
		Encryptor:     encryptor,
		MasterKey:     c.masterKey,
		BatchInterval: batchInterval,
	})

	if err := ongoing.Start(c.ctx); err != nil {
		fw.Stop()
		return fmt.Errorf("starting ongoing backup: %w", err)
	}

	c.fw = fw
	c.ongoing = ongoing
	c.running = true

	slog.Info("watcher started from web UI")
	return nil
}

// StopWatcher stops the file watcher and ongoing backup processor.
func (c *LocalWatcherController) StopWatcher() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.running {
		return fmt.Errorf("watcher is not running")
	}

	if c.cancel != nil {
		c.cancel()
	}
	if c.ongoing != nil {
		c.ongoing.Stop()
	}
	if c.fw != nil {
		c.fw.Stop()
	}

	c.running = false
	c.fw = nil
	c.ongoing = nil

	slog.Info("watcher stopped from web UI")
	return nil
}

// Ensure interface is satisfied.
var _ WatcherController = (*LocalWatcherController)(nil)
