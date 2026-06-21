package webui

import "syscall"

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

// diskUsagePercent returns the percentage of disk space used on the volume
// containing the given path. Returns 0 if the path is empty or stats cannot be obtained.
func diskUsagePercent(path string) float64 {
	if path == "" {
		return 0
	}
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0
	}
	if stat.Blocks == 0 {
		return 0
	}
	used := stat.Blocks - stat.Bfree
	return float64(used) / float64(stat.Blocks) * 100
}
