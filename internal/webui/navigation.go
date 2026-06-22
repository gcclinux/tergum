package webui

// NavItem represents a single navigation entry in the sidebar.
type NavItem struct {
	Path  string   // URL path (e.g., "/backups")
	Label string   // Display text (e.g., "Backups")
	Icon  string   // SVG path data for the icon
	Roles []string // Which roles can see this item (e.g., []string{"client", "server", "both"})
}

// allNavItems defines the complete navigation structure with role visibility.
var allNavItems = []NavItem{
	{
		Path:  "/",
		Label: "Dashboard",
		Icon:  "M3 12l2-2m0 0l7-7 7 7M5 10v10a1 1 0 001 1h3m10-11l2 2m-2-2v10a1 1 0 01-1 1h-3m-6 0a1 1 0 001-1v-4a1 1 0 011-1h2a1 1 0 011 1v4a1 1 0 001 1m-6 0h6",
		Roles: []string{"client", "server", "both"},
	},
	{
		Path:  "/backups",
		Label: "Backups",
		Icon:  "M7 16a4 4 0 01-.88-7.903A5 5 0 1115.9 6L16 6a5 5 0 011 9.9M15 13l-3-3m0 0l-3 3m3-3v12",
		Roles: []string{"client", "both"},
	},
	{
		Path:  "/restore",
		Label: "Restore",
		Icon:  "M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1m-4-4l-4 4m0 0l-4-4m4 4V4",
		Roles: []string{"client", "both"},
	},
	{
		Path:  "/config",
		Label: "Config",
		Icon:  "M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.066 2.573c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.573 1.066c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.066-2.573c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z M15 12a3 3 0 11-6 0 3 3 0 016 0z",
		Roles: []string{"client", "server", "both"},
	},
	{
		Path:  "/paths",
		Label: "Paths",
		Icon:  "M3 7a2 2 0 012-2h4l2 2h6a2 2 0 012 2v8a2 2 0 01-2 2H5a2 2 0 01-2-2V7z",
		Roles: []string{"client", "both"},
	},
	{
		Path:  "/retention",
		Label: "Retention",
		Icon:  "M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z",
		Roles: []string{"client", "server", "both"},
	},
	{
		Path:  "/watchers",
		Label: "Watchers",
		Icon:  "M15 12a3 3 0 11-6 0 3 3 0 016 0z M2.458 12C3.732 7.943 7.523 5 12 5c4.478 0 8.268 2.943 9.542 7-1.274 4.057-5.064 7-9.542 7-4.477 0-8.268-2.943-9.542-7z",
		Roles: []string{"client", "both"},
	},
	{
		Path:  "/activity",
		Label: "Activity",
		Icon:  "M13 10V3L4 14h7v7l9-11h-7z",
		Roles: []string{"client", "server", "both"},
	},
	{
		Path:  "/clients",
		Label: "Clients",
		Icon:  "M17 20h5v-2a3 3 0 00-5.356-1.857M17 20H7m10 0v-2c0-.656-.126-1.283-.356-1.857M7 20H2v-2a3 3 0 015.356-1.857M7 20v-2c0-.656.126-1.283.356-1.857m0 0a5.002 5.002 0 019.288 0M15 7a3 3 0 11-6 0 3 3 0 016 0zm6 3a2 2 0 11-4 0 2 2 0 014 0zM7 10a2 2 0 11-4 0 2 2 0 014 0z",
		Roles: []string{"server", "both"},
	},
	{
		Path:  "/metrics",
		Label: "Metrics",
		Icon:  "M9 19v-6a2 2 0 00-2-2H5a2 2 0 00-2 2v6a2 2 0 002 2h2a2 2 0 002-2zm0 0V9a2 2 0 012-2h2a2 2 0 012 2v10m-6 0a2 2 0 002 2h2a2 2 0 002-2m0 0V5a2 2 0 012-2h2a2 2 0 012 2v14a2 2 0 01-2 2h-2a2 2 0 01-2-2z",
		Roles: []string{"client", "server", "both"},
	},
}

// FilterNavItems returns the navigation items visible for the given role.
// An item is visible if its Roles slice contains the given role.
func FilterNavItems(role string) []NavItem {
	var result []NavItem
	for _, item := range allNavItems {
		if itemVisibleForRole(item, role) {
			result = append(result, item)
		}
	}
	return result
}

// itemVisibleForRole reports whether a navigation item is visible for the given role.
func itemVisibleForRole(item NavItem, role string) bool {
	for _, r := range item.Roles {
		if r == role {
			return true
		}
	}
	return false
}
