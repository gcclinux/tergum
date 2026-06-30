//go:build !windows

package server

import (
	"log/slog"
	"syscall"
)

// raiseFileLimit attempts to increase the process file descriptor limit to at
// least minFDs. On macOS/Linux the default soft limit (often 256 on macOS) is
// too low when the file watcher monitors thousands of directories. This raises
// it toward the hard limit, avoiding "too many open files" errors during backup.
func raiseFileLimit(minFDs uint64) {
	var rlim syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &rlim); err != nil {
		slog.Warn("cannot read file descriptor limit", "error", err)
		return
	}

	if rlim.Cur >= minFDs {
		return // already sufficient
	}

	target := minFDs
	if target > rlim.Max {
		target = rlim.Max
	}

	prev := rlim.Cur
	rlim.Cur = target
	if err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &rlim); err != nil {
		slog.Warn("cannot raise file descriptor limit",
			"current", prev, "target", target, "error", err)
		return
	}

	slog.Info("raised file descriptor limit", "from", prev, "to", target)
}
