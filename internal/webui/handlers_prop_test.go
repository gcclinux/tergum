package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ricardopadilha/tergum/internal/config"
	"pgregory.net/rapid"
)

// Feature: webui-redesign, Property 2: Fragment vs full response correctness
// Validates: Requirements 1.2

func TestProperty_FragmentVsFullResponse(t *testing.T) {
	s := newTestServerForFragments(t)

	fragments := []string{
		"dashboard", "backups", "restore", "config", "paths",
		"retention", "watchers", "activity", "clients", "metrics",
	}

	// Map fragment names to their corresponding URL paths.
	fragmentPaths := map[string]string{
		"dashboard": "/",
		"backups":   "/backups",
		"restore":   "/restore",
		"config":    "/config",
		"paths":     "/paths",
		"retention": "/retention",
		"watchers":  "/watchers",
		"activity":  "/activity",
		"clients":   "/clients",
		"metrics":   "/metrics",
	}

	rapid.Check(t, func(t *rapid.T) {
		// Pick a random fragment.
		fragIdx := rapid.IntRange(0, len(fragments)-1).Draw(t, "fragmentIndex")
		fragment := fragments[fragIdx]
		path := fragmentPaths[fragment]

		// Randomly decide whether to include HX-Request header.
		isHTMX := rapid.Bool().Draw(t, "isHTMXRequest")

		// Build fragment-specific test data.
		var data any
		switch fragment {
		case "dashboard":
			data = dashboardData{
				Title:    "Dashboard",
				NodeRole: "both",
				NavItems: FilterNavItems("both"),
				Version:  "3.0.0",
				Uptime:   "1h",
			}
		case "backups":
			data = backupsData{
				Title:    "Backups",
				NodeRole: "both",
				NavItems: FilterNavItems("both"),
				Jobs:     []backupJobView{},
			}
		case "restore":
			data = restoreData{
				Title:    "Restore",
				NodeRole: "both",
				NavItems: FilterNavItems("both"),
			}
		case "config":
			data = configData{
				Title:    "Config",
				NodeRole: "both",
				NavItems: FilterNavItems("both"),
				Config:   &config.Config{},
			}
		case "paths":
			data = pathsData{
				Title:    "Paths",
				NodeRole: "both",
				NavItems: FilterNavItems("both"),
			}
		case "retention":
			data = retentionData{
				Title:    "Retention",
				NodeRole: "both",
				NavItems: FilterNavItems("both"),
				Policies: []retentionPolicyView{},
			}
		case "watchers":
			data = watchersData{
				Title:          "Watchers",
				NodeRole:       "both",
				NavItems:       FilterNavItems("both"),
				WatcherEnabled: true,
				WatcherRunning: true,
			}
		case "activity":
			data = activityData{
				Title:    "Activity",
				NodeRole: "both",
				NavItems: FilterNavItems("both"),
			}
		case "clients":
			data = clientsData{
				Title:    "Clients",
				NodeRole: "both",
				NavItems: FilterNavItems("both"),
			}
		case "metrics":
			data = metricsData{
				Title:    "Metrics",
				NodeRole: "both",
				NavItems: FilterNavItems("both"),
			}
		default:
			data = dashboardData{
				Title:    strings.Title(fragment),
				NodeRole: "both",
				NavItems: FilterNavItems("both"),
			}
		}

		req := httptest.NewRequest(http.MethodGet, path, nil)
		if isHTMX {
			req.Header.Set("HX-Request", "true")
		}
		w := httptest.NewRecorder()

		s.renderFragment(w, req, fragment, data)

		resp := w.Result()
		body := w.Body.String()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("unexpected status %d for fragment %q (htmx=%v)", resp.StatusCode, fragment, isHTMX)
		}

		if isHTMX {
			// Fragment-only response: must NOT contain shell markup.
			if strings.Contains(body, "<!DOCTYPE html>") {
				t.Fatalf("htmx response for %q should not contain <!DOCTYPE html>", fragment)
			}
			if strings.Contains(body, "<html") {
				t.Fatalf("htmx response for %q should not contain <html tag", fragment)
			}
			if strings.Contains(body, "<head") {
				t.Fatalf("htmx response for %q should not contain <head tag", fragment)
			}
			if strings.Contains(body, "<nav") {
				t.Fatalf("htmx response for %q should not contain <nav tag", fragment)
			}
			// HX-Push-Url must be set to the request path.
			pushURL := resp.Header.Get("HX-Push-Url")
			if pushURL == "" {
				t.Fatalf("htmx response for %q should have HX-Push-Url header set", fragment)
			}
			if pushURL != path {
				t.Fatalf("HX-Push-Url = %q, want %q", pushURL, path)
			}
		} else {
			// Full page response: must contain shell markup.
			if !strings.Contains(body, "<!DOCTYPE html>") {
				t.Fatalf("full page response for %q should contain <!DOCTYPE html>", fragment)
			}
			if !strings.Contains(body, "<html") {
				t.Fatalf("full page response for %q should contain <html tag", fragment)
			}
			// HX-Push-Url must NOT be set for full page responses.
			pushURL := resp.Header.Get("HX-Push-Url")
			if pushURL != "" {
				t.Fatalf("full page response for %q should not have HX-Push-Url header, got %q", fragment, pushURL)
			}
		}
	})
}
