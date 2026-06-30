//go:build windows

package server

// raiseFileLimit is a no-op on Windows. Windows does not have a per-process
// file descriptor limit in the same way as Unix systems.
func raiseFileLimit(_ uint64) {}
