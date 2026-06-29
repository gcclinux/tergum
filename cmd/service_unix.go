//go:build !windows

package cmd

import (
	"os"
	"os/exec"
	"syscall"
)

// detachProcess configures the command to run in a new session (detached from terminal).
func detachProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}
}

// terminateProcess sends SIGTERM to the process for graceful shutdown.
func terminateProcess(p *os.Process) error {
	return p.Signal(syscall.SIGTERM)
}

// isProcessRunning checks if a process with the given PID exists.
func isProcessRunning(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// On Unix, FindProcess always succeeds. Send signal 0 to check existence.
	err = process.Signal(syscall.Signal(0))
	return err == nil
}
