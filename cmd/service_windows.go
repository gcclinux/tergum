//go:build windows

package cmd

import (
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// detachProcess configures the command to run detached on Windows (CREATE_NEW_PROCESS_GROUP).
func detachProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	}
}

// terminateProcess terminates the process on Windows.
func terminateProcess(p *os.Process) error {
	return p.Kill()
}

// isProcessRunning checks if a process with the given PID is still alive on Windows.
func isProcessRunning(pid int) bool {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(handle)

	var exitCode uint32
	err = windows.GetExitCodeProcess(handle, &exitCode)
	if err != nil {
		return false
	}

	// STILL_ACTIVE (259) means the process is still running.
	return exitCode == 259
}
