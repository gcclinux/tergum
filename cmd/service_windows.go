//go:build windows

package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
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

// --- Autostart (Windows: per-user HKCU Run registry key) ---

const (
	autostartRunKey    = `Software\Microsoft\Windows\CurrentVersion\Run`
	autostartValueName = "Tergum"
)

// runServiceEnable registers Tergum to start automatically when the current user logs in.
// It uses the per-user HKCU Run key, which requires no administrator privileges.
func runServiceEnable(envPath string) error {
	p, err := resolveAutostartParams(envPath)
	if err != nil {
		return err
	}

	// Build the command line. The service is launched via the same "service start"
	// pathway used for manual starts so it detaches and writes a PID file.
	var b strings.Builder
	b.WriteString(quoteWin(p.Binary))
	b.WriteString(" service start")
	if p.EnvFile != "" {
		b.WriteString(" --env-file ")
		b.WriteString(quoteWin(p.EnvFile))
	}
	if p.ConfigTo != "" {
		b.WriteString(" --config ")
		b.WriteString(quoteWin(p.ConfigTo))
	}
	command := b.String()

	key, _, err := registry.CreateKey(registry.CURRENT_USER, autostartRunKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("opening HKCU Run key: %w", err)
	}
	defer key.Close()

	if err := key.SetStringValue(autostartValueName, command); err != nil {
		return fmt.Errorf("writing autostart registry value: %w", err)
	}

	printOutput(
		map[string]interface{}{
			"status":    "enabled",
			"mechanism": "hkcu-run",
			"key":       `HKCU\` + autostartRunKey,
			"value":     autostartValueName,
			"command":   command,
		},
		fmt.Sprintf("Tergum autostart enabled (runs at user login).\nRegistry: HKCU\\%s\\%s\nCommand: %s",
			autostartRunKey, autostartValueName, command),
	)
	return nil
}

// runServiceDisable removes the Windows autostart registry entry.
func runServiceDisable() error {
	key, err := registry.OpenKey(registry.CURRENT_USER, autostartRunKey, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		// Key doesn't exist -> nothing to disable.
		printOutput(
			map[string]interface{}{"status": "disabled", "mechanism": "hkcu-run", "removed": false},
			"Tergum autostart was not enabled (no registry entry found).",
		)
		return nil
	}
	defer key.Close()

	removed := true
	if err := key.DeleteValue(autostartValueName); err != nil {
		if err == registry.ErrNotExist {
			removed = false
		} else {
			return fmt.Errorf("deleting autostart registry value: %w", err)
		}
	}

	msg := "Tergum autostart disabled (registry entry removed)."
	if !removed {
		msg = "Tergum autostart was not enabled (no registry entry found)."
	}
	printOutput(
		map[string]interface{}{
			"status":    "disabled",
			"mechanism": "hkcu-run",
			"value":     autostartValueName,
			"removed":   removed,
		},
		msg,
	)
	return nil
}

// quoteWin wraps a path in double quotes if it contains spaces.
func quoteWin(s string) string {
	if strings.ContainsAny(s, " \t") {
		return "\"" + s + "\""
	}
	return s
}
