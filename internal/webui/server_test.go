package webui

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gcclinux/tergum/internal/config"
	"github.com/gcclinux/tergum/internal/observe"
)

func testConfig() config.WebUIConfig {
	return config.WebUIConfig{
		Enabled:             true,
		Port:                7480,
		SessionTimeoutHours: 24,
	}
}

func TestNewServer(t *testing.T) {
	cfg := testConfig()
	srv, err := NewServer(cfg, "admin", "secret123")
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	if srv == nil {
		t.Fatal("NewServer() returned nil")
	}
	if srv.broker == nil {
		t.Error("broker should not be nil")
	}
	if srv.sessions == nil {
		t.Error("sessions should not be nil")
	}
	if srv.auth == nil {
		t.Error("auth should not be nil")
	}
	if srv.templates == nil {
		t.Error("templates should not be nil")
	}
}

func TestNewServer_DefaultTimeout(t *testing.T) {
	cfg := config.WebUIConfig{
		Enabled:             true,
		Port:                7480,
		SessionTimeoutHours: 0, // should default to 24h
	}
	srv, err := NewServer(cfg, "admin", "secret123")
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	if srv.sessions.timeout != 24*time.Hour {
		t.Errorf("expected 24h timeout, got %v", srv.sessions.timeout)
	}
}

func TestServer_Shutdown(t *testing.T) {
	cfg := testConfig()
	srv, err := NewServer(cfg, "admin", "secret123")
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = srv.Shutdown(ctx)
	if err != nil {
		t.Errorf("Shutdown() error = %v", err)
	}
}

func TestServer_UnauthenticatedAccess(t *testing.T) {
	cfg := testConfig()
	srv, err := NewServer(cfg, "admin", "secret123")
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	handler := srv.routes()

	// Request without credentials should return 401.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}

	if w.Header().Get("WWW-Authenticate") == "" {
		t.Error("expected WWW-Authenticate header")
	}
}

func TestServer_AuthenticatedAccess(t *testing.T) {
	cfg := testConfig()
	srv, err := NewServer(cfg, "admin", "secret123")
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	handler := srv.routes()

	// Request with valid credentials should return 200.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.SetBasicAuth("admin", "secret123")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	// Should set session cookie.
	cookies := w.Result().Cookies()
	found := false
	for _, c := range cookies {
		if c.Name == "tergum_session" {
			found = true
			if !c.HttpOnly {
				t.Error("session cookie should be HttpOnly")
			}
			if !c.Secure {
				t.Error("session cookie should be Secure")
			}
			break
		}
	}
	if !found {
		t.Error("expected tergum_session cookie")
	}
}

func TestServer_WrongCredentials(t *testing.T) {
	cfg := testConfig()
	srv, err := NewServer(cfg, "admin", "secret123")
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	handler := srv.routes()

	tests := []struct {
		name     string
		user     string
		password string
	}{
		{"wrong password", "admin", "wrongpass"},
		{"wrong username", "notadmin", "secret123"},
		{"wrong both", "notadmin", "wrongpass"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.SetBasicAuth(tt.user, tt.password)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			if w.Code != http.StatusUnauthorized {
				t.Errorf("expected 401, got %d", w.Code)
			}
		})
	}
}

func TestServer_SessionAuth(t *testing.T) {
	cfg := testConfig()
	srv, err := NewServer(cfg, "admin", "secret123")
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	handler := srv.routes()

	// First, authenticate to get a session.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.SetBasicAuth("admin", "secret123")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("initial auth failed: %d", w.Code)
	}

	// Extract session cookie.
	var sessionCookie *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == "tergum_session" {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil {
		t.Fatal("no session cookie set")
	}

	// Use session cookie without basic auth.
	req2 := httptest.NewRequest(http.MethodGet, "/backups", nil)
	req2.AddCookie(sessionCookie)
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Errorf("session auth failed: expected 200, got %d", w2.Code)
	}
}

func TestServer_StaticAssetsNoAuth(t *testing.T) {
	cfg := testConfig()
	srv, err := NewServer(cfg, "admin", "secret123")
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	handler := srv.routes()

	// Static assets should be accessible without authentication.
	req := httptest.NewRequest(http.MethodGet, "/assets/htmx.min.js", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for static assets, got %d", w.Code)
	}
}

func TestServer_AllPagesAccessible(t *testing.T) {
	cfg := testConfig()
	srv, err := NewServer(cfg, "admin", "secret123")
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	handler := srv.routes()

	pages := []string{
		"/",
		"/backups",
		"/restore",
		"/config",
		"/retention",
		"/watchers",
		"/activity",
		"/clients",
		"/metrics",
	}

	for _, page := range pages {
		t.Run(page, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, page, nil)
			req.SetBasicAuth("admin", "secret123")
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Errorf("page %s: expected 200, got %d", page, w.Code)
			}
		})
	}
}

func TestServer_NotFound(t *testing.T) {
	cfg := testConfig()
	srv, err := NewServer(cfg, "admin", "secret123")
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	handler := srv.routes()

	req := httptest.NewRequest(http.MethodGet, "/nonexistent", nil)
	req.SetBasicAuth("admin", "secret123")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestServer_ActivityLogJSON_Warning(t *testing.T) {
	cfg := testConfig()
	srv, err := NewServer(cfg, "admin", "secret123")
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	handler := srv.routes()

	_ = observe.SetupLogging("info", "text")

	testWarnMsg := "failed to hash file, skipping C:/Users/ricardo/Documents/My Music"
	slog.Warn(testWarnMsg)

	req := httptest.NewRequest(http.MethodGet, "/api/activity/recent?format=json", nil)
	req.SetBasicAuth("admin", "secret123")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var res []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("failed to parse json: %v, body: %s", err, w.Body.String())
	}

	found := false
	for _, item := range res {
		msg, _ := item["message"].(string)
		level, _ := item["level"].(string)
		if strings.Contains(msg, testWarnMsg) {
			found = true
			if level != "WARN" {
				t.Errorf("expected level to be WARN, got %s", level)
			}
		}
	}
	if !found {
		t.Errorf("expected to find warning msg %q in JSON api response, response: %s", testWarnMsg, w.Body.String())
	}
}
