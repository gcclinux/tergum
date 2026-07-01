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

	// Determine a safe target: the minimum of our request and the hard limit.
	target := minFDs
	if rlim.Max > 0 && target > rlim.Max {
		target = rlim.Max
	}

	// On macOS, kern.maxfilesperproc caps what we can actually set.
	// If the hard limit seems unreasonably high (>1M), cap at a safe value.
	const macOSSafeMax = 1048576 // 1M — beyond this macOS may reject or misbehave
	if target > macOSSafeMax {
		target = macOSSafeMax
	}

	prev := rlim.Cur
	rlim.Cur = target
	if err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &rlim); err != nil {
		// If the full target fails, try a more modest increase.
		rlim.Cur = 10240
		if rlim.Cur <= prev {
			slog.Warn("cannot raise file descriptor limit",
				"current", prev, "target", target, "error", err)
			return
		}
		if err2 := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &rlim); err2 != nil {
			slog.Warn("cannot raise file descriptor limit",
				"current", prev, "target", target, "error", err)
			return
		}
		target = 10240
	}

	slog.Info("raised file descriptor limit", "from", prev, "to", target)
}
