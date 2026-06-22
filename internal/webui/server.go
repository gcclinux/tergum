package webui

import (
	"context"
	"crypto/tls"
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/ricardopadilha/tergum/internal/config"
	"github.com/ricardopadilha/tergum/internal/db"
	"github.com/ricardopadilha/tergum/internal/model"
	registryPkg "github.com/ricardopadilha/tergum/internal/registry"
)

//go:embed assets
var assetsFS embed.FS

//go:embed templates
var templatesFS embed.FS

// Server is the embedded HTTPS web management UI server.
type Server struct {
	httpServer        *http.Server
	templates         map[string]*template.Template
	fragmentTmpl      fragmentTemplates // shell+fragment template sets
	auth              *AuthMiddleware
	sessions          *SessionStore
	broker            *SSEBroker
	logger            *slog.Logger
	cfg               config.WebUIConfig
	fullCfg           *config.Config
	repo              db.Repository
	configPath        string // path to tergum.toml for syncing paths back
	backupTrigger     BackupTrigger
	watcherController WatcherController
	clientRegistry    ClientRegistry
	clientConnector   ClientConnector
}

// BackupTrigger is an interface for triggering backups from the Web UI.
type BackupTrigger interface {
	TriggerBackup(level string) error
	IsAvailable() bool
}

// WatcherController is an interface for starting/stopping the file watcher from the Web UI.
type WatcherController interface {
	StartWatcher() error
	StopWatcher() error
	IsRunning() bool
}

// ClientConnector sends commands to remote client nodes via gRPC.
type ClientConnector interface {
	TriggerClientBackup(ctx context.Context, clientID string) error
	StartClientWatcher(ctx context.Context, clientID string) error
	StopClientWatcher(ctx context.Context, clientID string) error
	GetClientStatus(ctx context.Context, clientID string) (*ClientStatusInfo, error)
}

// ClientStatusInfo holds the status reported by a remote client.
type ClientStatusInfo struct {
	Status           string `json:"status"`
	BackupID         string `json:"backup_id,omitempty"`
	FilesProcessed   int64  `json:"files_processed"`
	BytesTransferred int64  `json:"bytes_transferred"`
	StartedAt        string `json:"started_at,omitempty"`
	Message          string `json:"message,omitempty"`
}

// ClientRegistry is the subset of registry.Registry needed by the webui.
type ClientRegistry interface {
	ListClients() []registryPkg.ClientInfo
	GetClient(clientID string) *registryPkg.ClientInfo
	SetSchedule(clientID string, schedule registryPkg.ScheduleConfig) error
}

// ServerOption is a functional option for configuring the web UI server.
type ServerOption func(*Server)

// WithTLSConfig sets a custom TLS configuration for the server.
func WithTLSConfig(tlsCfg *tls.Config) ServerOption {
	return func(s *Server) {
		s.httpServer.TLSConfig = tlsCfg
	}
}

// WithLogger sets a custom logger for the server.
func WithLogger(logger *slog.Logger) ServerOption {
	return func(s *Server) {
		s.logger = logger
	}
}

// WithRepository sets the database repository for path management operations.
func WithRepository(repo db.Repository) ServerOption {
	return func(s *Server) {
		s.repo = repo
	}
}

// WithConfigPath sets the path to the TOML config file so the web UI can sync paths back.
func WithConfigPath(path string) ServerOption {
	return func(s *Server) {
		s.configPath = path
	}
}

// WithBackupTrigger sets a backup trigger for web-initiated backups.
func WithBackupTrigger(bt BackupTrigger) ServerOption {
	return func(s *Server) {
		s.backupTrigger = bt
	}
}

// WithFullConfig sets the full application config for status display.
func WithFullConfig(cfg *config.Config) ServerOption {
	return func(s *Server) {
		s.fullCfg = cfg
	}
}

// WithWatcherController sets a watcher controller for start/stop from the Web UI.
func WithWatcherController(wc WatcherController) ServerOption {
	return func(s *Server) {
		s.watcherController = wc
	}
}

// WithClientRegistry sets the client registry for client management.
func WithClientRegistry(reg ClientRegistry) ServerOption {
	return func(s *Server) {
		s.clientRegistry = reg
	}
}

// WithClientConnector sets the client connector for sending commands to remote clients.
func WithClientConnector(cc ClientConnector) ServerOption {
	return func(s *Server) {
		s.clientConnector = cc
	}
}

// NewServer creates a new web UI server with the given configuration and credentials.
// The password parameter is the plaintext password for initial setup; it is hashed
// immediately with Argon2id and not stored in plaintext.
func NewServer(cfg config.WebUIConfig, username, password string, opts ...ServerOption) (*Server, error) {
	// Hash the password with Argon2id.
	params := DefaultArgon2idParams()
	hashedPwd, err := HashPassword(password, params)
	if err != nil {
		return nil, fmt.Errorf("hashing password: %w", err)
	}

	// Configure session timeout.
	timeout := time.Duration(cfg.SessionTimeoutHours) * time.Hour
	if timeout <= 0 {
		timeout = 24 * time.Hour
	}
	sessions := NewSessionStore(timeout)

	// Parse templates — each page template includes the shared layout.
	tmpl, err := parseTemplates()
	if err != nil {
		return nil, fmt.Errorf("parsing templates: %w", err)
	}

	// Parse shell+fragment template sets for the new architecture.
	fragTmpl, err := parseFragmentTemplates()
	if err != nil {
		return nil, fmt.Errorf("parsing fragment templates: %w", err)
	}

	// Create SSE broker.
	broker := NewSSEBroker(100)

	// Create auth middleware.
	auth := NewAuthMiddleware(username, hashedPwd, sessions)

	s := &Server{
		httpServer: &http.Server{
			Addr:         fmt.Sprintf(":%d", cfg.Port),
			ReadTimeout:  15 * time.Second,
			WriteTimeout: 0, // SSE requires no write timeout
			IdleTimeout:  60 * time.Second,
		},
		templates:    tmpl,
		fragmentTmpl: fragTmpl,
		auth:         auth,
		sessions:     sessions,
		broker:       broker,
		logger:       slog.Default().With(slog.String("component", "webui")),
		cfg:          cfg,
	}

	// Apply options.
	for _, opt := range opts {
		opt(s)
	}

	// Wire broker to backup trigger for SSE events.
	if s.backupTrigger != nil {
		if bt, ok := s.backupTrigger.(*LocalBackupTrigger); ok {
			bt.SetBroker(s.broker)
		}
	}
	if s.watcherController != nil {
		if wc, ok := s.watcherController.(*LocalWatcherController); ok {
			wc.SetBroker(s.broker)
		}
	}

	// Register routes.
	s.httpServer.Handler = s.routes()

	return s, nil
}

// routes builds the HTTP router with authentication.
func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()

	// Static assets (no auth required).
	assetsSubFS, _ := fs.Sub(assetsFS, "assets")
	mux.Handle("/assets/", http.StripPrefix("/assets/", NewAssetHandler(assetsSubFS)))

	// Authenticated routes.
	authed := http.NewServeMux()
	authed.HandleFunc("/", s.handleDashboard)
	authed.HandleFunc("/backups", s.handleBackups)
	authed.HandleFunc("/restore", s.handleRestore)
	authed.HandleFunc("/config", s.handleConfig)
	authed.HandleFunc("/paths", s.handlePaths)
	authed.HandleFunc("/retention", s.handleRetention)
	authed.HandleFunc("/watchers", s.handleWatchers)
	authed.HandleFunc("/activity", s.handleActivity)
	authed.HandleFunc("/clients", s.handleClients)
	authed.HandleFunc("/metrics", s.handleMetrics)

	// SSE endpoint (authenticated).
	authed.Handle("/api/activity/stream", s.broker)

	// Path management API endpoints.
	authed.HandleFunc("/api/paths/includes", s.handlePathsIncludes)
	authed.HandleFunc("/api/paths/includes/add", s.handlePathsIncludeAdd)
	authed.HandleFunc("/api/paths/includes/remove", s.handlePathsIncludeRemove)
	authed.HandleFunc("/api/paths/excludes", s.handlePathsExcludes)
	authed.HandleFunc("/api/paths/excludes/add", s.handlePathsExcludeAdd)
	authed.HandleFunc("/api/paths/excludes/remove", s.handlePathsExcludeRemove)
	authed.HandleFunc("/api/paths/scan", s.handlePathsScan)

	// Watcher API endpoints.
	authed.HandleFunc("/api/watchers", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			s.handleWatchersAPI(w, r)
		case http.MethodPost:
			s.handleWatchersAdd(w, r)
		case http.MethodDelete:
			s.handleWatchersDelete(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	authed.HandleFunc("/api/watchers/status", s.handleWatchersStatus)
	authed.HandleFunc("/api/watchers/settings", s.handleWatchersSettings)
	authed.HandleFunc("/api/watchers/paths", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			s.handleWatchersPaths(w, r)
		case http.MethodPost:
			s.handleWatchersPathsAdd(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	authed.HandleFunc("/api/watchers/config/autostart", s.handleAPIWatcherAutostart)


	// Backup API endpoints.
	authed.HandleFunc("/api/backups/trigger", s.handleAPIBackupTrigger)
	authed.HandleFunc("/api/backups/active", s.handleAPIBackupsActive)
	authed.HandleFunc("/api/backups/status", s.handleAPIBackupsStatus)
	authed.HandleFunc("/api/backups/progress", s.handleAPIBackupsProgress)
	authed.HandleFunc("/api/backups/history", s.handleAPIBackupsHistory)
	authed.HandleFunc("/api/backups/", s.handleAPIBackupDelete)

	// Retention API endpoints.
	authed.HandleFunc("/api/retention/", s.handleAPIRetention)
	authed.HandleFunc("/api/retention", s.handleAPIRetention)

	// Restore API endpoints.
	authed.HandleFunc("/api/restore/search", s.handleAPIRestoreSearch)
	authed.HandleFunc("/api/restore/backups", s.handleAPIRestoreBackups)
	authed.HandleFunc("/api/restore/files", s.handleAPIRestoreFiles)
	authed.HandleFunc("/api/restore/run", s.handleAPIRestoreFile)
	authed.HandleFunc("/api/restore/jobs", s.handleAPIRestoreJobs)

	// Dashboard API.
	authed.HandleFunc("/api/dashboard", s.handleAPIDashboard)
	authed.HandleFunc("/api/dashboard/files", s.handleAPIDashboardFiles)
	authed.HandleFunc("/api/dashboard/storage", s.handleAPIDashboardStorage)
	authed.HandleFunc("/api/dashboard/clients", s.handleAPIDashboardClients)
	authed.HandleFunc("/api/dashboard/activity", s.handleAPIDashboardActivity)

	// Activity API.
	authed.HandleFunc("/api/activity/recent", s.handleAPIActivityRecent)

	// Watcher control API.
	authed.HandleFunc("/api/watcher/start", s.handleAPIWatcherControl)
	authed.HandleFunc("/api/watcher/stop", s.handleAPIWatcherControl)

	// System stats API.
	authed.HandleFunc("/api/system/cpu", s.handleAPISystemCPU)
	authed.HandleFunc("/api/system/memory", s.handleAPISystemMemory)

	// Metrics API.
	authed.HandleFunc("/api/metrics/cards", s.handleAPIMetricsCards)

	// Client management API.
	authed.HandleFunc("/api/clients", s.handleAPIClients)
	authed.HandleFunc("/api/clients/list", s.handleAPIClientsList)
	authed.HandleFunc("/api/clients/", s.handleAPIClientAction)

	mux.Handle("/", s.auth.Wrap(authed))

	return mux
}

// Start begins listening and serving HTTPS connections.
// If TLS is not configured, it falls back to plain HTTP (for development).
func (s *Server) Start() error {
	s.logger.Info("starting web UI server", "addr", s.httpServer.Addr)

	// Start session cleanup goroutine.
	go s.cleanupSessions()

	// Start job activity watcher (publishes SSE events for CLI-initiated backups).
	go s.watchJobActivity()

	if s.httpServer.TLSConfig != nil {
		return s.httpServer.ListenAndServeTLS("", "")
	}
	return s.httpServer.ListenAndServe()
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info("shutting down web UI server")
	return s.httpServer.Shutdown(ctx)
}

// Broker returns the SSE broker for publishing events from other components.
func (s *Server) Broker() *SSEBroker {
	return s.broker
}

// cleanupSessions periodically removes expired sessions.
func (s *Server) cleanupSessions() {
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		s.sessions.Cleanup()
	}
}

// watchJobActivity polls the database for job changes and publishes SSE events.
func (s *Server) watchJobActivity() {
	if s.repo == nil {
		return
	}

	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	// Track known jobs to detect new ones and status changes.
	knownJobs := make(map[string]string) // backup_id -> status

	// Initial load.
	ctx := context.Background()
	jobs, err := s.repo.ListJobs(ctx, db.JobFilter{Limit: 50})
	if err == nil {
		for _, j := range jobs {
			knownJobs[j.BackupID] = string(j.Status)
		}
	}

	for range ticker.C {
		jobs, err := s.repo.ListJobs(ctx, db.JobFilter{Limit: 50})
		if err != nil {
			continue
		}

		for _, j := range jobs {
			prevStatus, known := knownJobs[j.BackupID]
			currentStatus := string(j.Status)

			if !known {
				// New job appeared.
				knownJobs[j.BackupID] = currentStatus
				s.broker.Publish(ActivityEvent{
					Type:    "backup_started",
					Message: fmt.Sprintf("Backup %s started (%s)", j.Level, j.BackupID[:12]),
				})
			} else if prevStatus != currentStatus {
				// Status changed.
				knownJobs[j.BackupID] = currentStatus
				switch j.Status {
				case "completed":
					s.broker.Publish(ActivityEvent{
						Type:    "backup_completed",
						Message: fmt.Sprintf("Backup %s completed: %d files, %s", j.Level, j.FileCount, formatSize(j.BytesNew)),
					})
				case "failed":
					s.broker.Publish(ActivityEvent{
						Type:    "backup_failed",
						Message: fmt.Sprintf("Backup %s failed: %s", j.Level, j.ErrorMessage),
					})
				case "stopped":
					s.broker.Publish(ActivityEvent{
						Type:    "backup_stopped",
						Message: fmt.Sprintf("Backup %s stopped", j.Level),
					})
				}
			}
		}
	}
}

// parseTemplates parses each page template together with the shared layout and partials.
func parseTemplates() (map[string]*template.Template, error) {
	pages := []string{
		"dashboard.html",
		"backups.html",
		"restore.html",
		"config.html",
		"retention.html",
		"watchers.html",
		"activity.html",
		"clients.html",
		"metrics.html",
	}

	templates := make(map[string]*template.Template, len(pages))
	for _, page := range pages {
		t, err := template.ParseFS(templatesFS,
			"templates/layout.html",
			"templates/partials/*.html",
			"templates/"+page,
		)
		if err != nil {
			return nil, fmt.Errorf("parsing template %s: %w", page, err)
		}
		templates[page] = t
	}
	return templates, nil
}

// renderTemplate executes a named page template with the given data.
func (s *Server) renderTemplate(w http.ResponseWriter, page string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	tmpl, ok := s.templates[page]
	if !ok {
		s.logger.Error("template not found", "page", page)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	if err := tmpl.ExecuteTemplate(w, page, data); err != nil {
		s.logger.Error("template execution failed", "page", page, "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// nodeRole returns the configured node role, defaulting to "both" when
// fullCfg is nil (e.g., when running via `tergum admin`).
func (s *Server) nodeRole() string {
	if s.fullCfg != nil && s.fullCfg.Node.Role != "" {
		return s.fullCfg.Node.Role
	}
	return "both"
}

// Page data types.

type dashboardData struct {
	Title         string
	NodeRole      string
	NavItems      []NavItem
	Uptime        string
	Version       string
	TotalFiles    int64
	TotalSize     string
	ActiveClients int
	CPUUsage      string
	MemUsed       string
	MemTotal      string
	MemPercent    string
}

type backupsData struct {
	Title    string
	NodeRole string
	NavItems []NavItem
	Jobs     []backupJobView
}

type backupJobView struct {
	BackupID  string
	ClientID  string
	Level     string
	Status    string
	FileCount int64
	BytesNew  int64
	Size      string
	StartedAt string
	Duration  string
}

type restoreData struct {
	Title    string
	NodeRole string
	NavItems []NavItem
	Jobs     []backupJobView
}

type configData struct {
	Title    string
	NodeRole string
	NavItems []NavItem
	Config   *config.Config
}

type pathsData struct {
	Title    string
	NodeRole string
	NavItems []NavItem
}

type retentionData struct {
	Title    string
	NodeRole string
	NavItems []NavItem
	Policies []retentionPolicyView
}

type retentionPolicyView struct {
	Name         string
	KeepDays     *int
	KeepVersions int
	Pattern      string
	Priority     int
	Enabled      bool
}

type watchersData struct {
	Title          string
	NodeRole       string
	NavItems       []NavItem
	WatchExcludes  []string
	IncludePaths   []string
	WatcherEnabled bool
	WatcherRunning bool
	DebounceMs     int
	StabilitySec   int
	BatchMinutes   int
}

type watchPathView struct {
	Path       string
	Recursive  bool
	Enabled    bool
	LastEvent  string
	EventCount int64
}

type activityData struct {
	Title    string
	NodeRole string
	NavItems []NavItem
}

type clientsData struct {
	Title    string
	NodeRole string
	NavItems []NavItem
	Clients  []clientView
}

type clientView struct {
	ClientID       string
	Status         string
	LastSeen       string
	LastBackup     string
	WatcherActive  bool
	FullBackupCron string
	AutoBackupCron string
}

type metricsData struct {
	Title    string
	NodeRole string
	NavItems []NavItem
	Metrics  metricsView
}

type metricsView struct {
	FilesBackedUp    int64
	BytesTransferred string
	DedupRatio       string
	DedupRatioPercent float64
	StorageUsed      string
	StoragePercent   float64
	StorageColor     string
	UniqueFiles      int64
	GRPCRequests     int64
	GRPCErrors       int64
	ConnectedClients int
}

// Handlers — each renders the corresponding template with placeholder data.
// In production these would query the database/engines for real data.

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	data := dashboardData{
		Title:         "Dashboard",
		NodeRole:      s.nodeRole(),
		NavItems:      FilterNavItems(s.nodeRole()),
		Uptime:        "N/A",
		Version:       "3.0.0",
		TotalFiles:    0,
		TotalSize:     "0 B",
		ActiveClients: 0,
	}

	if s.repo != nil {
		jobs, err := s.repo.ListJobs(r.Context(), db.JobFilter{})
		if err == nil {
			var totalFiles, totalBytes int64
			for _, j := range jobs {
				totalFiles += j.FileCount
				totalBytes += j.BytesNew
			}
			data.TotalFiles = totalFiles
			data.TotalSize = formatSize(totalBytes)
		}
	}

	// System stats.
	cpu, memUsed, memTotal, memPct := getSystemStats()
	data.CPUUsage = cpu
	data.MemUsed = memUsed
	data.MemTotal = memTotal
	data.MemPercent = memPct

	s.renderFragment(w, r, "dashboard", data)
}

func (s *Server) handleBackups(w http.ResponseWriter, r *http.Request) {
	data := backupsData{
		Title:    "Backups",
		NodeRole: s.nodeRole(),
		NavItems: FilterNavItems(s.nodeRole()),
		Jobs:     []backupJobView{},
	}

	if s.repo != nil {
		jobs, err := s.repo.ListJobs(r.Context(), db.JobFilter{Limit: 100})
		if err != nil {
			s.logger.Error("list jobs failed", "error", err)
		} else {
			for _, j := range jobs {
				started := j.StartedAt.Format("2006-01-02 15:04:05")
				duration := "-"
				if j.FinishedAt != nil {
					duration = j.FinishedAt.Sub(j.StartedAt).Round(time.Second).String()
				} else if j.Status == model.JobRunning {
					duration = time.Since(j.StartedAt).Round(time.Second).String()
				}
				view := backupJobView{
					BackupID:  j.BackupID,
					ClientID:  j.ClientID,
					Level:     j.Level,
					Status:    string(j.Status),
					FileCount: j.FileCount,
					BytesNew:  j.BytesNew,
					Size:      formatSize(j.BytesNew),
					StartedAt: started,
					Duration:  duration,
				}
				data.Jobs = append(data.Jobs, view)
			}
		}
	}

	s.renderFragment(w, r, "backups", data)
}

func (s *Server) handleRestore(w http.ResponseWriter, r *http.Request) {
	data := restoreData{
		Title:    "Restore",
		NodeRole: s.nodeRole(),
		NavItems: FilterNavItems(s.nodeRole()),
		Jobs:     []backupJobView{},
	}

	if s.repo != nil {
		jobs, err := s.repo.ListJobs(r.Context(), db.JobFilter{Limit: 100})
		if err == nil {
			for _, j := range jobs {
				data.Jobs = append(data.Jobs, backupJobView{
					BackupID:  j.BackupID,
					ClientID:  j.ClientID,
					Level:     j.Level,
					Status:    string(j.Status),
					FileCount: j.FileCount,
					StartedAt: j.StartedAt.Format("2006-01-02 15:04:05"),
				})
			}
		}
	}

	s.renderFragment(w, r, "restore", data)
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	data := configData{Title: "Configuration", NodeRole: s.nodeRole(), NavItems: FilterNavItems(s.nodeRole())}
	if s.fullCfg != nil {
		data.Config = s.fullCfg
	} else if s.configPath != "" {
		cfg, err := config.Load(s.configPath)
		if err == nil {
			data.Config = cfg
		}
	}
	s.renderFragment(w, r, "config", data)
}

func (s *Server) handlePaths(w http.ResponseWriter, r *http.Request) {
	data := pathsData{Title: "Paths", NodeRole: s.nodeRole(), NavItems: FilterNavItems(s.nodeRole())}
	s.renderFragment(w, r, "paths", data)
}

func (s *Server) handleRetention(w http.ResponseWriter, r *http.Request) {
	data := retentionData{
		Title:    "Retention Policies",
		NodeRole: s.nodeRole(),
		NavItems: FilterNavItems(s.nodeRole()),
		Policies: []retentionPolicyView{},
	}

	if s.repo != nil {
		policies, err := s.repo.ListRetentionPolicies(r.Context())
		if err == nil {
			for _, p := range policies {
				view := retentionPolicyView{
					Name:         p.Name,
					KeepDays:     p.KeepDays,
					KeepVersions: p.KeepVersions,
					Pattern:      p.Pattern,
					Priority:     p.Priority,
					Enabled:      p.Enabled,
				}
				data.Policies = append(data.Policies, view)
			}
		}
	}

	s.renderFragment(w, r, "retention", data)
}

func (s *Server) handleWatchers(w http.ResponseWriter, r *http.Request) {
	data := watchersData{
		Title:         "Watchers",
		NodeRole:      s.nodeRole(),
		NavItems:      FilterNavItems(s.nodeRole()),
		WatchExcludes: []string{},
		IncludePaths:  []string{},
	}

	// Fill watcher config status.
	if s.fullCfg != nil {
		data.WatcherEnabled = s.fullCfg.Watcher.Enabled
		data.DebounceMs = s.fullCfg.Watcher.DebounceMs
		data.StabilitySec = s.fullCfg.Watcher.StabilitySeconds
		data.BatchMinutes = s.fullCfg.Watcher.BatchIntervalMinutes
	} else if s.configPath != "" {
		cfg, err := config.Load(s.configPath)
		if err == nil {
			data.WatcherEnabled = cfg.Watcher.Enabled
			data.DebounceMs = cfg.Watcher.DebounceMs
			data.StabilitySec = cfg.Watcher.StabilitySeconds
			data.BatchMinutes = cfg.Watcher.BatchIntervalMinutes
		}
	}

	// Check if watcher is actually running via the controller.
	if s.watcherController != nil {
		data.WatcherRunning = s.watcherController.IsRunning()
	}

	if s.repo != nil {
		excludes, err := s.repo.ListWatchExcludes(r.Context())
		if err == nil {
			data.WatchExcludes = excludes
		}
		includes, err := s.repo.ListIncludePaths(r.Context())
		if err == nil {
			data.IncludePaths = includes
		}
	}

	s.renderFragment(w, r, "watchers", data)
}

func (s *Server) handleActivity(w http.ResponseWriter, r *http.Request) {
	data := activityData{Title: "Activity Log", NodeRole: s.nodeRole(), NavItems: FilterNavItems(s.nodeRole())}
	s.renderFragment(w, r, "activity", data)
}

func (s *Server) handleClients(w http.ResponseWriter, r *http.Request) {
	data := clientsData{
		Title:    "Clients",
		NodeRole: s.nodeRole(),
		NavItems: FilterNavItems(s.nodeRole()),
		Clients:  []clientView{},
	}

	if s.clientRegistry != nil {
		clients := s.clientRegistry.ListClients()
		for _, ci := range clients {
			lastSeen := "never"
			if !ci.LastSeen.IsZero() {
				lastSeen = ci.LastSeen.Format("2006-01-02 15:04:05")
			}
			lastBackup := "never"
			if !ci.LastBackup.IsZero() {
				lastBackup = ci.LastBackup.Format("2006-01-02 15:04:05")
			}
			view := clientView{
				ClientID:      ci.ClientID,
				Status:        ci.Status,
				LastSeen:      lastSeen,
				LastBackup:    lastBackup,
				WatcherActive: ci.WatcherActive,
			}
			if ci.Schedule != nil {
				view.FullBackupCron = ci.Schedule.FullBackupCron
				view.AutoBackupCron = ci.Schedule.AutoBackupCron
			}
			data.Clients = append(data.Clients, view)
		}
	}

	s.renderFragment(w, r, "clients", data)
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	data := metricsData{
		Title:    "Metrics",
		NodeRole: s.nodeRole(),
		NavItems: FilterNavItems(s.nodeRole()),
		Metrics: metricsView{
			FilesBackedUp:    0,
			BytesTransferred: "0 B",
			DedupRatio:       "0%",
			DedupRatioPercent: 0,
			StorageUsed:      "0 B",
			StoragePercent:   0,
			StorageColor:     "blue",
			UniqueFiles:      0,
			GRPCRequests:     0,
			GRPCErrors:       0,
			ConnectedClients: 0,
		},
	}

	if s.repo != nil {
		jobs, err := s.repo.ListJobs(r.Context(), db.JobFilter{})
		if err == nil {
			var totalFiles, totalBytes, totalDeduped int64
			for _, j := range jobs {
				totalFiles += j.FileCount
				totalBytes += j.BytesNew
				totalDeduped += j.FilesDeduped
			}
			data.Metrics.FilesBackedUp = totalFiles
			data.Metrics.BytesTransferred = formatSize(totalBytes)

			if totalFiles > 0 {
				ratio := float64(totalDeduped) / float64(totalFiles) * 100
				data.Metrics.DedupRatio = fmt.Sprintf("%.1f%%", ratio)
				data.Metrics.DedupRatioPercent = ratio
			}
		}

		// Count unique hashes from a recent query.
		paths, err := s.repo.GetAllFilePaths(r.Context())
		if err == nil {
			data.Metrics.UniqueFiles = int64(len(paths))
		}
	}

	// Get storage size and disk usage percent.
	var storageDir string
	if s.fullCfg != nil {
		storageDir = s.fullCfg.StorageDir()
	} else if s.configPath != "" {
		cfg, err := config.Load(s.configPath)
		if err == nil {
			storageDir = cfg.StorageDir()
		}
	}
	if storageDir != "" {
		data.Metrics.StorageUsed = formatSize(dirSizeQuick(storageDir))
		data.Metrics.StoragePercent = diskUsagePercent(storageDir)
		data.Metrics.StorageColor = StorageColorScheme(data.Metrics.StoragePercent)
	}

	s.renderFragment(w, r, "metrics", data)
}

// dirSizeQuick estimates directory size by listing top-level prefix dirs.
func dirSizeQuick(path string) int64 {
	var size int64
	entries, err := os.ReadDir(path)
	if err != nil {
		return 0
	}
	for _, entry := range entries {
		if entry.IsDir() {
			subPath := path + "/" + entry.Name()
			subEntries, err := os.ReadDir(subPath)
			if err != nil {
				continue
			}
			for _, f := range subEntries {
				if !f.IsDir() {
					info, err := f.Info()
					if err == nil {
						size += info.Size()
					}
				}
			}
		}
	}
	return size
}
