package grpc

import (
	"context"
	"sync"
	"time"

	"github.com/gcclinux/tergum/internal/config"
	"github.com/gcclinux/tergum/internal/observe"
)

// AdminPolicy decides whether a caller (identified by its mTLS SPKI
// fingerprint) has admin-client privilege. Reload forces a synchronous refresh
// of the underlying admin set so front-ends (e.g. the Web UI) can apply config
// changes immediately without waiting for the background refresh.
type AdminPolicy interface {
	// IsAdmin reports whether the given SPKI fingerprint is an admin client.
	IsAdmin(spkiFingerprint string) bool
	// Reload synchronously refreshes the admin set from the config source.
	Reload() error
}

// configAdminPolicy is an AdminPolicy backed by the TOML config file. It keeps a
// cached copy of the admin-client list and refreshes it periodically from disk
// on a dedicated ticker, so admin changes made via the CLI or Web UI take effect
// without a server restart. Load failures are logged and the last-known-good set
// is retained (fail-closed on the very first load).
type configAdminPolicy struct {
	configPath string
	logger     logger

	mu      sync.RWMutex
	clients []config.AdminClient
}

// logger is the minimal logging surface used by configAdminPolicy. It matches
// *slog.Logger's Warn method so observe.Logger can be passed directly.
type logger interface {
	Warn(msg string, args ...any)
}

// NewConfigAdminPolicy builds a configAdminPolicy from the resolved config path.
// It performs an initial synchronous load (an initial failure leaves the admin
// set empty = fail-closed) and starts a goroutine that reloads the admin set
// every refreshInterval until ctx is cancelled. If refreshInterval <= 0 it
// defaults to 5s.
func NewConfigAdminPolicy(ctx context.Context, configPath string, refreshInterval time.Duration) *configAdminPolicy {
	if refreshInterval <= 0 {
		refreshInterval = 5 * time.Second
	}

	p := &configAdminPolicy{
		configPath: configPath,
		logger:     observe.Logger("admin-policy"),
	}

	// Initial synchronous load. On failure we retain the empty set (fail-closed).
	if err := p.Reload(); err != nil {
		p.logger.Warn("admin policy initial load failed; starting with empty admin set (fail-closed)",
			"error", err, "config_path", configPath)
	}

	go p.refreshLoop(ctx, refreshInterval)

	return p
}

// refreshLoop reloads the admin set on each tick until ctx is cancelled.
func (p *configAdminPolicy) refreshLoop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := p.Reload(); err != nil {
				p.logger.Warn("admin policy reload failed; retaining last-known-good admin set",
					"error", err, "config_path", p.configPath)
			}
		}
	}
}

// Reload synchronously reloads the admin-client list from the config file. On
// error the previously cached set is retained and the error is returned.
func (p *configAdminPolicy) Reload() error {
	cfg, err := config.Load(p.configPath)
	if err != nil {
		return err
	}

	// Copy the slice so callers mutating the config cannot affect our cache.
	clients := make([]config.AdminClient, len(cfg.Admin.Clients))
	copy(clients, cfg.Admin.Clients)

	p.mu.Lock()
	p.clients = clients
	p.mu.Unlock()
	return nil
}

// IsAdmin reports whether the given SPKI fingerprint matches a configured admin
// client. The fingerprint is compared against the already-canonical stored
// values (lowercase 64-hex). An empty fingerprint never matches.
func (p *configAdminPolicy) IsAdmin(spkiFingerprint string) bool {
	if spkiFingerprint == "" {
		return false
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	for _, c := range p.clients {
		if c.Fingerprint == spkiFingerprint {
			return true
		}
	}
	return false
}

// Ensure configAdminPolicy satisfies the AdminPolicy interface at compile time.
var _ AdminPolicy = (*configAdminPolicy)(nil)
