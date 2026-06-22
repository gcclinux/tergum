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
	broker           *SSEBroker

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

// SetBroker sets the SSE broker for publishing watcher events.
func (c *LocalWatcherController) SetBroker(b *SSEBroker) {
	c.broker = b
}

// StartWatcher starts the file watcher and ongoing backup processor.
func (c *LocalWatcherController) StartWatcher() error {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return fmt.Errorf("watcher is already running")
	}

	c.running = true
	ctx := context.Background()
	c.ctx, c.cancel = context.WithCancel(ctx)
	c.mu.Unlock()

	// Spawn a background goroutine to perform the directory walking and startup.
	go func() {
		bgCtx := c.ctx

		// 1. Get include paths from DB.
		includePaths, err := c.repo.ListIncludePaths(bgCtx)
		if err != nil {
			slog.Error("watcher: failed to list include paths", "error", err)
			c.failStartup(err)
			return
		}
		if len(includePaths) == 0 {
			slog.Warn("watcher: no include paths configured")
			c.failStartup(fmt.Errorf("no include paths configured"))
			return
		}

		// 2. Get exclude patterns from DB.
		excludePatterns, _ := c.repo.ListExcludePatterns(bgCtx)
		if len(excludePatterns) == 0 {
			excludePatterns = c.excludePatterns
		}

		// 3. Create file watcher.
		watcherCfg := watcher.Config{
			DebounceMs:       c.debounceMs,
			StabilitySeconds: c.stabilitySec,
			ExcludePatterns:  excludePatterns,
			Repository:       c.repo,
		}

		fw, err := watcher.NewFileWatcher(watcherCfg)
		if err != nil {
			slog.Error("watcher: failed to create file watcher", "error", err)
			c.failStartup(err)
			return
		}

		// Start watcher first (sets internal context needed by AddPath).
		if err := fw.Start(bgCtx); err != nil {
			slog.Error("watcher: failed to start file watcher", "error", err)
			c.failStartup(err)
			return
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

		if err := ongoing.Start(bgCtx); err != nil {
			fw.Stop()
			slog.Error("watcher: failed to start ongoing backup", "error", err)
			c.failStartup(err)
			return
		}

		// Verify we weren't cancelled during initialization.
		c.mu.Lock()
		if bgCtx.Err() != nil {
			c.mu.Unlock()
			fw.Stop()
			ongoing.Stop()
			c.failStartup(bgCtx.Err())
			return
		}

		c.fw = fw
		c.ongoing = ongoing
		c.mu.Unlock()

		slog.Info("watcher started from web UI (async complete)")
		if c.broker != nil {
			c.broker.Publish(ActivityEvent{
				Type:    "watcher_started",
				Message: "File watcher started",
			})
		}
	}()

	return nil
}

func (c *LocalWatcherController) failStartup(err error) {
	c.mu.Lock()
	c.running = false
	if c.cancel != nil {
		c.cancel()
	}
	c.fw = nil
	c.ongoing = nil
	c.mu.Unlock()

	if c.broker != nil {
		c.broker.Publish(ActivityEvent{
			Type:    "watcher_failed",
			Message: fmt.Sprintf("File watcher failed to start: %v", err),
		})
	}
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
	if c.broker != nil {
		c.broker.Publish(ActivityEvent{
			Type:    "watcher_stopped",
			Message: "File watcher stopped",
		})
	}
	return nil
}

// Ensure interface is satisfied.
var _ WatcherController = (*LocalWatcherController)(nil)
