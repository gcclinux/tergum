package webui

// StorageColorScheme returns the Tailwind color class for a storage usage percentage.
// Default (blue) when <80%, warning (amber) when 80-94%, error (red) when ≥95%.
func StorageColorScheme(percent float64) string {
	switch {
	case percent >= 95:
		return "red"
	case percent >= 80:
		return "amber"
	default:
		return "blue"
	}
}
