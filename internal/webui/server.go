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
	"time"

	"github.com/ricardopadilha/tergum/internal/config"
	"github.com/ricardopadilha/tergum/internal/db"
)

//go:embed assets
var assetsFS embed.FS

//go:embed templates
var templatesFS embed.FS

// Server is the embedded HTTPS web management UI server.
type Server struct {
	httpServer *http.Server
	templates  map[string]*template.Template
	auth       *AuthMiddleware
	sessions   *SessionStore
	broker     *SSEBroker
	logger     *slog.Logger
	cfg        config.WebUIConfig
	repo       db.Repository
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
		templates: tmpl,
		auth:      auth,
		sessions:  sessions,
		broker:    broker,
		logger:    slog.Default().With(slog.String("component", "webui")),
		cfg:       cfg,
	}

	// Apply options.
	for _, opt := range opts {
		opt(s)
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
	mux.Handle("/assets/", http.StripPrefix("/assets/", http.FileServer(http.FS(assetsSubFS))))

	// Authenticated routes.
	authed := http.NewServeMux()
	authed.HandleFunc("/", s.handleDashboard)
	authed.HandleFunc("/backups", s.handleBackups)
	authed.HandleFunc("/restore", s.handleRestore)
	authed.HandleFunc("/config", s.handleConfig)
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

	mux.Handle("/", s.auth.Wrap(authed))

	return mux
}

// Start begins listening and serving HTTPS connections.
// If TLS is not configured, it falls back to plain HTTP (for development).
func (s *Server) Start() error {
	s.logger.Info("starting web UI server", "addr", s.httpServer.Addr)

	// Start session cleanup goroutine.
	go s.cleanupSessions()

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

// parseTemplates parses each page template together with the shared layout.
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
		t, err := template.ParseFS(templatesFS, "templates/layout.html", "templates/"+page)
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

// Page data types.

type dashboardData struct {
	Title         string
	Uptime        string
	Version       string
	TotalFiles    int64
	TotalSize     string
	ActiveClients int
}

type backupsData struct {
	Title string
	Jobs  []backupJobView
}

type backupJobView struct {
	BackupID  string
	ClientID  string
	Level     string
	Status    string
	FileCount int64
	StartedAt string
}

type restoreData struct {
	Title string
}

type configData struct {
	Title  string
	Config config.Config
}

type retentionData struct {
	Title    string
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
	Title      string
	WatchPaths []watchPathView
}

type watchPathView struct {
	Path       string
	Recursive  bool
	Enabled    bool
	LastEvent  string
	EventCount int64
}

type activityData struct {
	Title string
}

type clientsData struct {
	Title   string
	Clients []clientView
}

type clientView struct {
	ClientID   string
	LastSeen   string
	LastBackup string
	Status     string
	FileCount  int64
}

type metricsData struct {
	Title   string
	Metrics metricsView
}

type metricsView struct {
	FilesBackedUp    int64
	BytesTransferred string
	DedupRatio       string
	StorageUsed      string
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
		Uptime:        "0h 0m",
		Version:       "3.0.0",
		TotalFiles:    0,
		TotalSize:     "0 B",
		ActiveClients: 0,
	}
	s.renderTemplate(w, "dashboard.html", data)
}

func (s *Server) handleBackups(w http.ResponseWriter, r *http.Request) {
	data := backupsData{
		Title: "Backups",
		Jobs:  []backupJobView{},
	}
	s.renderTemplate(w, "backups.html", data)
}

func (s *Server) handleRestore(w http.ResponseWriter, r *http.Request) {
	data := restoreData{Title: "Restore"}
	s.renderTemplate(w, "restore.html", data)
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	data := configData{Title: "Configuration"}
	s.renderTemplate(w, "config.html", data)
}

func (s *Server) handleRetention(w http.ResponseWriter, r *http.Request) {
	data := retentionData{
		Title:    "Retention Policies",
		Policies: []retentionPolicyView{},
	}
	s.renderTemplate(w, "retention.html", data)
}

func (s *Server) handleWatchers(w http.ResponseWriter, r *http.Request) {
	data := watchersData{
		Title:      "Watchers",
		WatchPaths: []watchPathView{},
	}
	s.renderTemplate(w, "watchers.html", data)
}

func (s *Server) handleActivity(w http.ResponseWriter, r *http.Request) {
	data := activityData{Title: "Activity Log"}
	s.renderTemplate(w, "activity.html", data)
}

func (s *Server) handleClients(w http.ResponseWriter, r *http.Request) {
	data := clientsData{
		Title:   "Clients",
		Clients: []clientView{},
	}
	s.renderTemplate(w, "clients.html", data)
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	data := metricsData{
		Title: "Metrics",
		Metrics: metricsView{
			FilesBackedUp:    0,
			BytesTransferred: "0 B",
			DedupRatio:       "0.0",
			StorageUsed:      "0 B",
			UniqueFiles:      0,
			GRPCRequests:     0,
			GRPCErrors:       0,
			ConnectedClients: 0,
		},
	}
	s.renderTemplate(w, "metrics.html", data)
}
