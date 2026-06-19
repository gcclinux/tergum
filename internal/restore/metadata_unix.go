//go:build !windows

package restore

// setHiddenAttribute is a no-op on non-Windows platforms.
// On Unix, hidden files are denoted by a "." prefix rather than a file attribute.
func setHiddenAttribute(path string) {
	// No-op on Unix.
}
