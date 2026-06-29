//go:build !windows

package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// detachProcess configures the command to run in a new session (detached from terminal).
func detachProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}
}

// terminateProcess sends SIGTERM to the process and waits for it to exit.
// If the process doesn't exit within 10 seconds, it sends SIGKILL.
func terminateProcess(p *os.Process) error {
	// Send SIGTERM for graceful shutdown.
	if err := p.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("sending SIGTERM: %w", err)
	}

	// Wait for the process to exit, polling every 250ms for up to 10 seconds.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if !isProcessRunning(p.Pid) {
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}

	// Process didn't exit in time — force kill.
	fmt.Fprintf(os.Stderr, "Process %d did not exit after SIGTERM, sending SIGKILL...\n", p.Pid)
	if err := p.Signal(syscall.SIGKILL); err != nil {
		// If signal fails, process may have exited between our check and kill.
		if !isProcessRunning(p.Pid) {
			return nil
		}
		return fmt.Errorf("sending SIGKILL: %w", err)
	}

	// Wait briefly for SIGKILL to take effect.
	time.Sleep(500 * time.Millisecond)
	return nil
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
