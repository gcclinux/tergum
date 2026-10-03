package webui

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestHandleCLIDocs_Returns200 verifies that the /cli-docs route returns
// a 200 status code when the CLI.md file exists.
// Validates: Requirements 2.4, 2.5
func TestHandleCLIDocs_Returns200(t *testing.T) {
	// Create a temp directory structure with docs/CLI.md
	tmpDir := t.TempDir()
	docsDir := filepath.Join(tmpDir, "docs")
	if err := os.MkdirAll(docsDir, 0755); err != nil {
		t.Fatalf("failed to create docs directory: %v", err)
	}
	cliMdPath := filepath.Join(docsDir, "CLI.md")
	if err := os.WriteFile(cliMdPath, []byte("# Test CLI Docs\n\n## Commands\n\nSome content here."), 0644); err != nil {
		t.Fatalf("failed to write CLI.md: %v", err)
	}

	// Change to temp directory so os.ReadFile("docs/CLI.md") works
	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir to temp dir: %v", err)
	}

	s := newTestServerForFragments(t)

	req := httptest.NewRequest(http.MethodGet, "/cli-docs", nil)
	w := httptest.NewRecorder()

	s.handleCLIDocs(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
}

// TestHandleCLIDocs_HTMXRequest_ReturnsFragmentWithPushUrl verifies that
// an htmx request returns only the fragment content with the HX-Push-Url header set.
// Validates: Requirements 2.4
func TestHandleCLIDocs_HTMXRequest_ReturnsFragmentWithPushUrl(t *testing.T) {
	// Create temp docs/CLI.md
	tmpDir := t.TempDir()
	docsDir := filepath.Join(tmpDir, "docs")
	if err := os.MkdirAll(docsDir, 0755); err != nil {
		t.Fatalf("failed to create docs directory: %v", err)
	}
	cliMdPath := filepath.Join(docsDir, "CLI.md")
	mdContent := "# CLI Reference\n\n## Setup\n\nRun `tergum setup` to configure.\n\n## Backup\n\nUse `tergum backup` to create backups."
	if err := os.WriteFile(cliMdPath, []byte(mdContent), 0644); err != nil {
		t.Fatalf("failed to write CLI.md: %v", err)
	}

	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir to temp dir: %v", err)
	}

	s := newTestServerForFragments(t)

	req := httptest.NewRequest(http.MethodGet, "/cli-docs", nil)
	req.Header.Set("HX-Request", "true")
	w := httptest.NewRecorder()

	s.handleCLIDocs(w, req)

	resp := w.Result()
	body := w.Body.String()

	// Should return 200
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	// Should set HX-Push-Url header to /cli-docs
	if got := resp.Header.Get("HX-Push-Url"); got != "/cli-docs" {
		t.Errorf("HX-Push-Url = %q, want %q", got, "/cli-docs")
	}

	// Should NOT contain full page shell markup (no DOCTYPE, no <html> tag)
	if strings.Contains(body, "<!DOCTYPE html>") {
		t.Error("htmx response should not contain <!DOCTYPE html>")
	}
	if strings.Contains(body, "<html") {
		t.Error("htmx response should not contain <html> tag")
	}

	// Should contain content from the fragment (the rendered markdown or TOC elements)
	if !strings.Contains(body, "cli-docs") {
		t.Error("htmx response should contain cli-docs class for styling")
	}
}

// TestHandleCLIDocs_DirectNavigation_ReturnsFullShell verifies that a direct
// browser navigation (no HX-Request header) returns the full shell template.
// Validates: Requirements 2.5
func TestHandleCLIDocs_DirectNavigation_ReturnsFullShell(t *testing.T) {
	// Create temp docs/CLI.md
	tmpDir := t.TempDir()
	docsDir := filepath.Join(tmpDir, "docs")
	if err := os.MkdirAll(docsDir, 0755); err != nil {
		t.Fatalf("failed to create docs directory: %v", err)
	}
	cliMdPath := filepath.Join(docsDir, "CLI.md")
	mdContent := "# CLI Reference\n\n## Commands\n\nList of commands."
	if err := os.WriteFile(cliMdPath, []byte(mdContent), 0644); err != nil {
		t.Fatalf("failed to write CLI.md: %v", err)
	}

	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir to temp dir: %v", err)
	}

	s := newTestServerForFragments(t)

	req := httptest.NewRequest(http.MethodGet, "/cli-docs", nil)
	// No HX-Request header — this is a direct browser navigation
	w := httptest.NewRecorder()

	s.handleCLIDocs(w, req)

	resp := w.Result()
	body := w.Body.String()

	// Should return 200
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	// Should contain full page shell markup
	if !strings.Contains(body, "<!DOCTYPE html>") {
		t.Error("full page response should contain <!DOCTYPE html>")
	}
	if !strings.Contains(body, "<html") {
		t.Error("full page response should contain <html> tag")
	}
	if !strings.Contains(body, "content-area") {
		t.Error("full page response should contain the #content-area div")
	}

	// Should NOT have HX-Push-Url header
	if got := resp.Header.Get("HX-Push-Url"); got != "" {
		t.Errorf("full page response should not have HX-Push-Url header, got %q", got)
	}
}

// TestHandleCLIDocs_MissingFile_ShowsError verifies that when the CLI.md file
// is missing, the handler displays an error message rather than crashing.
// Validates: Requirements 3.7 (error handling when file cannot be read)
func TestHandleCLIDocs_MissingFile_ShowsError(t *testing.T) {
	// Create a temp directory WITHOUT docs/CLI.md
	tmpDir := t.TempDir()

	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir to temp dir: %v", err)
	}

	s := newTestServerForFragments(t)

	req := httptest.NewRequest(http.MethodGet, "/cli-docs", nil)
	w := httptest.NewRecorder()

	s.handleCLIDocs(w, req)

	resp := w.Result()
	body := w.Body.String()

	// Should still return 200 (error is displayed in the page, not as HTTP error)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200 even with missing file, got %d", resp.StatusCode)
	}

	// Should contain the error message in the response body
	if !strings.Contains(body, "Documentation unavailable") {
		t.Error("response should contain 'Documentation unavailable' error message")
	}

	// The error block should be displayed
	if !strings.Contains(body, "bg-red-900") || !strings.Contains(body, "text-red-400") {
		t.Error("response should contain error styling classes")
	}
}

// TestHandleCLIDocs_HTMXRequest_MissingFile_ShowsError verifies that an htmx request
// with a missing CLI.md file returns the error message in fragment form.
func TestHandleCLIDocs_HTMXRequest_MissingFile_ShowsError(t *testing.T) {
	// Create a temp directory WITHOUT docs/CLI.md
	tmpDir := t.TempDir()

	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir to temp dir: %v", err)
	}

	s := newTestServerForFragments(t)

	req := httptest.NewRequest(http.MethodGet, "/cli-docs", nil)
	req.Header.Set("HX-Request", "true")
	w := httptest.NewRecorder()

	s.handleCLIDocs(w, req)

	resp := w.Result()
	body := w.Body.String()

	// Should return 200
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	// Should set HX-Push-Url header even on error
	if got := resp.Header.Get("HX-Push-Url"); got != "/cli-docs" {
		t.Errorf("HX-Push-Url = %q, want %q", got, "/cli-docs")
	}

	// Should NOT contain full page shell markup
	if strings.Contains(body, "<!DOCTYPE html>") {
		t.Error("htmx error response should not contain <!DOCTYPE html>")
	}

	// Should contain the error message
	if !strings.Contains(body, "Documentation unavailable") {
		t.Error("htmx error response should contain 'Documentation unavailable' error message")
	}
}

// TestHandleCLIDocs_RendersMarkdownContent verifies that the handler correctly
// renders markdown content into HTML.
func TestHandleCLIDocs_RendersMarkdownContent(t *testing.T) {
	tmpDir := t.TempDir()
	docsDir := filepath.Join(tmpDir, "docs")
	if err := os.MkdirAll(docsDir, 0755); err != nil {
		t.Fatalf("failed to create docs directory: %v", err)
	}
	cliMdPath := filepath.Join(docsDir, "CLI.md")
	mdContent := `# Tergum CLI

## Setup Command

Run the setup wizard:

` + "```bash\ntergum setup\n```" + `

## Backup Command

Create a backup:

| Option | Description |
|--------|-------------|
| --full | Full backup |
| --auto | Auto backup |
`
	if err := os.WriteFile(cliMdPath, []byte(mdContent), 0644); err != nil {
		t.Fatalf("failed to write CLI.md: %v", err)
	}

	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir to temp dir: %v", err)
	}

	s := newTestServerForFragments(t)

	req := httptest.NewRequest(http.MethodGet, "/cli-docs", nil)
	req.Header.Set("HX-Request", "true")
	w := httptest.NewRecorder()

	s.handleCLIDocs(w, req)

	body := w.Body.String()

	// Check that markdown was rendered to HTML
	// Should have heading IDs for navigation
	if !strings.Contains(body, "setup-command") {
		t.Error("response should contain heading ID 'setup-command'")
	}

	// Should render tables
	if !strings.Contains(body, "<table") || !strings.Contains(body, "</table>") {
		t.Error("response should contain rendered HTML table")
	}

	// Should render code blocks
	if !strings.Contains(body, "<pre") || !strings.Contains(body, "</pre>") {
		t.Error("response should contain rendered code block")
	}
}

// TestHandleCLIDocs_ExtractsTOC verifies that the handler extracts table of contents
// entries from h2/h3 headings.
func TestHandleCLIDocs_ExtractsTOC(t *testing.T) {
	tmpDir := t.TempDir()
	docsDir := filepath.Join(tmpDir, "docs")
	if err := os.MkdirAll(docsDir, 0755); err != nil {
		t.Fatalf("failed to create docs directory: %v", err)
	}
	cliMdPath := filepath.Join(docsDir, "CLI.md")
	mdContent := `# CLI Reference

## Getting Started

Introduction text.

### Prerequisites

You need Go installed.

### Installation

Run the installer.

## Commands

List of commands.

### tergum backup

Backup command details.
`
	if err := os.WriteFile(cliMdPath, []byte(mdContent), 0644); err != nil {
		t.Fatalf("failed to write CLI.md: %v", err)
	}

	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir to temp dir: %v", err)
	}

	s := newTestServerForFragments(t)

	req := httptest.NewRequest(http.MethodGet, "/cli-docs", nil)
	req.Header.Set("HX-Request", "true")
	w := httptest.NewRecorder()

	s.handleCLIDocs(w, req)

	body := w.Body.String()

	// The TOC should be rendered with links to h2/h3 sections
	// TOC entries should contain anchor links
	if !strings.Contains(body, "getting-started") {
		t.Error("TOC should contain link to 'getting-started' section")
	}
	if !strings.Contains(body, "commands") {
		t.Error("TOC should contain link to 'commands' section")
	}
}

// newTestServerForCLIDocs creates a Server specifically for CLI docs testing
// with fragment templates and a silent logger.
func newTestServerForCLIDocs(t *testing.T) *Server {
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
