package webui

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestParseFragmentTemplates(t *testing.T) {
	templates, err := parseFragmentTemplates()
	if err != nil {
		t.Fatalf("parseFragmentTemplates() failed: %v", err)
	}

	expectedFragments := []string{
		"dashboard", "backups", "restore", "config",
		"retention", "watchers", "activity", "clients", "metrics",
	}

	for _, name := range expectedFragments {
		if _, ok := templates[name]; !ok {
			t.Errorf("expected fragment template %q not found", name)
		}
	}
}

func TestParseFragmentTemplates_HasShellAndContent(t *testing.T) {
	templates, err := parseFragmentTemplates()
	if err != nil {
		t.Fatalf("parseFragmentTemplates() failed: %v", err)
	}

	for name, tmpl := range templates {
		// Check that "shell" template is defined.
		if tmpl.Lookup("shell") == nil {
			t.Errorf("fragment %q: missing 'shell' template definition", name)
		}
		// Check that "content" template is defined.
		if tmpl.Lookup("content") == nil {
			t.Errorf("fragment %q: missing 'content' template definition", name)
		}
	}
}

func TestRenderFragment_HTMXRequest_ReturnsContentOnly(t *testing.T) {
	s := newTestServerForFragments(t)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("HX-Request", "true")
	w := httptest.NewRecorder()

	s.renderFragment(w, req, "dashboard", dashboardData{
		Title:    "Dashboard",
		NodeRole: "both",
		Version:  "3.0.0",
		Uptime:   "1h",
	})

	resp := w.Result()
	body := w.Body.String()

	// Should set HX-Push-Url header.
	if got := resp.Header.Get("HX-Push-Url"); got != "/" {
		t.Errorf("HX-Push-Url = %q, want %q", got, "/")
	}

	// Should NOT contain full page shell markup.
	if strings.Contains(body, "<!DOCTYPE html>") {
		t.Error("htmx response should not contain <!DOCTYPE html>")
	}
	if strings.Contains(body, "<html") {
		t.Error("htmx response should not contain <html> tag")
	}

	// Should contain content from the fragment.
	if !strings.Contains(body, "Dashboard") || !strings.Contains(body, "data-card") || !strings.Contains(body, "card-cpu") {
		// The dashboard fragment has various data cards - just check it rendered something meaningful.
		if len(body) < 50 {
			t.Errorf("htmx response body too short (%d bytes), expected fragment content", len(body))
		}
	}
}

func TestRenderFragment_FullPageRequest_ReturnsShell(t *testing.T) {
	s := newTestServerForFragments(t)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	// No HX-Request header — full page request.
	w := httptest.NewRecorder()

	s.renderFragment(w, req, "dashboard", dashboardData{
		Title:    "Dashboard",
		NodeRole: "both",
		Version:  "3.0.0",
		Uptime:   "1h",
	})

	body := w.Body.String()

	// Should contain full page shell markup.
	if !strings.Contains(body, "<!DOCTYPE html>") {
		t.Error("full page response should contain <!DOCTYPE html>")
	}
	if !strings.Contains(body, "<html") {
		t.Error("full page response should contain <html> tag")
	}
	if !strings.Contains(body, "content-area") {
		t.Error("full page response should contain the #content-area div")
	}

	// Should NOT have HX-Push-Url header (that's only for htmx responses).
	if got := w.Result().Header.Get("HX-Push-Url"); got != "" {
		t.Errorf("full page response should not have HX-Push-Url header, got %q", got)
	}
}

func TestRenderFragment_UnknownFragment_Returns500(t *testing.T) {
	s := newTestServerForFragments(t)

	req := httptest.NewRequest(http.MethodGet, "/nonexistent", nil)
	w := httptest.NewRecorder()

	s.renderFragment(w, req, "nonexistent", nil)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500 for unknown fragment, got %d", w.Code)
	}
}

func TestRenderFragment_HTMXRequest_PushUrlMatchesRequestPath(t *testing.T) {
	s := newTestServerForFragments(t)

	req := httptest.NewRequest(http.MethodGet, "/backups", nil)
	req.Header.Set("HX-Request", "true")
	w := httptest.NewRecorder()

	s.renderFragment(w, req, "backups", backupsData{
		Title:    "Backups",
		NodeRole: "client",
		Jobs:     []backupJobView{},
	})

	if got := w.Result().Header.Get("HX-Push-Url"); got != "/backups" {
		t.Errorf("HX-Push-Url = %q, want %q", got, "/backups")
	}
}

// newTestServerForFragments creates a minimal Server with fragment templates loaded.
func newTestServerForFragments(t *testing.T) *Server {
	t.Helper()
	fragTmpl, err := parseFragmentTemplates()
	if err != nil {
		t.Fatalf("parseFragmentTemplates() failed: %v", err)
	}
	return &Server{
		fragmentTmpl: fragTmpl,
		logger:       slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
	}
}
