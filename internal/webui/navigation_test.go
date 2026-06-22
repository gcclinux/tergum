package webui

import (
	"testing"
)

func TestFilterNavItems_ClientRole(t *testing.T) {
	items := FilterNavItems("client")

	// Common items should be present
	assertHasItem(t, items, "/")
	assertHasItem(t, items, "/config")
	assertHasItem(t, items, "/retention")
	assertHasItem(t, items, "/activity")
	assertHasItem(t, items, "/metrics")

	// Client-specific items should be present
	assertHasItem(t, items, "/backups")
	assertHasItem(t, items, "/restore")
	assertHasItem(t, items, "/paths")
	assertHasItem(t, items, "/watchers")

	// Server-only items should be absent
	assertNoItem(t, items, "/clients")
}

func TestFilterNavItems_ServerRole(t *testing.T) {
	items := FilterNavItems("server")

	// Common items should be present
	assertHasItem(t, items, "/")
	assertHasItem(t, items, "/config")
	assertHasItem(t, items, "/retention")
	assertHasItem(t, items, "/activity")
	assertHasItem(t, items, "/metrics")

	// Server-specific items should be present
	assertHasItem(t, items, "/clients")

	// Client-only items should be absent
	assertNoItem(t, items, "/backups")
	assertNoItem(t, items, "/restore")
	assertNoItem(t, items, "/paths")
	assertNoItem(t, items, "/watchers")
}

func TestFilterNavItems_BothRole(t *testing.T) {
	items := FilterNavItems("both")

	// All items should be present
	assertHasItem(t, items, "/")
	assertHasItem(t, items, "/backups")
	assertHasItem(t, items, "/restore")
	assertHasItem(t, items, "/config")
	assertHasItem(t, items, "/paths")
	assertHasItem(t, items, "/retention")
	assertHasItem(t, items, "/watchers")
	assertHasItem(t, items, "/activity")
	assertHasItem(t, items, "/clients")
	assertHasItem(t, items, "/metrics")

	// Should have all 10 items
	if len(items) != 10 {
		t.Errorf("expected 10 items for role 'both', got %d", len(items))
	}
}

func TestFilterNavItems_UnknownRole(t *testing.T) {
	items := FilterNavItems("unknown")

	// No items should be visible for an unknown role
	if len(items) != 0 {
		t.Errorf("expected 0 items for unknown role, got %d", len(items))
	}
}

func TestFilterNavItems_EmptyRole(t *testing.T) {
	items := FilterNavItems("")

	// No items should be visible for an empty role
	if len(items) != 0 {
		t.Errorf("expected 0 items for empty role, got %d", len(items))
	}
}

func TestFilterNavItems_PreservesOrder(t *testing.T) {
	items := FilterNavItems("both")

	expected := []string{"/", "/backups", "/restore", "/config", "/paths", "/retention", "/watchers", "/activity", "/clients", "/metrics"}
	if len(items) != len(expected) {
		t.Fatalf("expected %d items, got %d", len(expected), len(items))
	}
	for i, item := range items {
		if item.Path != expected[i] {
			t.Errorf("item[%d]: expected path %q, got %q", i, expected[i], item.Path)
		}
	}
}

func TestFilterNavItems_ItemsHaveLabelsAndIcons(t *testing.T) {
	items := FilterNavItems("both")
	for _, item := range items {
		if item.Label == "" {
			t.Errorf("item %q has empty label", item.Path)
		}
		if item.Icon == "" {
			t.Errorf("item %q has empty icon", item.Path)
		}
	}
}

// assertHasItem checks that an item with the given path exists in the slice.
func assertHasItem(t *testing.T, items []NavItem, path string) {
	t.Helper()
	for _, item := range items {
		if item.Path == path {
			return
		}
	}
	t.Errorf("expected item with path %q to be present", path)
}

// assertNoItem checks that no item with the given path exists in the slice.
func assertNoItem(t *testing.T, items []NavItem, path string) {
	t.Helper()
	for _, item := range items {
		if item.Path == path {
			t.Errorf("expected item with path %q to be absent", path)
			return
		}
	}
}
