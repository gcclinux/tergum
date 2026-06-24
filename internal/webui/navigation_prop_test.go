package webui

import (
	"testing"

	"pgregory.net/rapid"
)

// **Validates: Requirements 1.4, 1.5, 1.6, 1.7**

// TestProperty_NavigationRoleFiltering verifies that for any valid role value,
// the filtered navigation items always include common items and conditionally
// include role-specific items per the role visibility rules.
// Feature: webui-redesign, Property 1: Navigation role filtering
func TestProperty_NavigationRoleFiltering(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate a random role from the valid set.
		role := rapid.SampledFrom([]string{"client", "server", "hybrid"}).Draw(rt, "role")

		// Call the function under test.
		items := FilterNavItems(role)

		// Build a set of paths for quick lookup.
		paths := make(map[string]bool)
		for _, item := range items {
			paths[item.Path] = true
		}

		// Common items should always be present regardless of role.
		commonPaths := []string{"/", "/config", "/retention", "/activity", "/metrics"}
		for _, p := range commonPaths {
			if !paths[p] {
				rt.Fatalf("role %q: expected common item %q to be present", role, p)
			}
		}

		// Role-specific assertions.
		switch role {
		case "client":
			// Client-specific items should be present.
			clientPaths := []string{"/backups", "/restore", "/paths", "/watchers"}
			for _, p := range clientPaths {
				if !paths[p] {
					rt.Fatalf("role %q: expected client item %q to be present", role, p)
				}
			}
			// Server-only items should be absent.
			if paths["/clients"] {
				rt.Fatalf("role %q: expected /clients to be absent", role)
			}

		case "server":
			// Server-specific items should be present.
			if !paths["/clients"] {
				rt.Fatalf("role %q: expected /clients to be present", role)
			}
			// Client-only items should be absent.
			clientOnlyPaths := []string{"/backups", "/restore", "/paths", "/watchers"}
			for _, p := range clientOnlyPaths {
				if paths[p] {
					rt.Fatalf("role %q: expected client-only item %q to be absent", role, p)
				}
			}

		case "hybrid":
			// All items should be present (10 total).
			allPaths := []string{"/", "/backups", "/restore", "/config", "/paths", "/retention", "/watchers", "/activity", "/clients", "/metrics"}
			for _, p := range allPaths {
				if !paths[p] {
					rt.Fatalf("role %q: expected item %q to be present", role, p)
				}
			}
			if len(items) != 10 {
				rt.Fatalf("role %q: expected 10 items, got %d", role, len(items))
			}
		}
	})
}
