//go:build windows

package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

	// Make the 'tergum' command available on PATH (non-fatal on failure).
	maybeLinkOnEnable()

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

// --- PATH linking (Windows: add binary dir to per-user PATH in HKCU\Environment) ---

const userEnvKey = `Environment`

// binaryDir returns the directory containing the running executable.
func binaryDir() (string, error) {
	binary, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolving executable path: %w", err)
	}
	return filepath.Dir(binary), nil
}

// readUserPath reads the current per-user PATH value from HKCU\Environment.
// It returns the value and whether it was stored as an expandable string.
func readUserPath() (string, bool, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, userEnvKey, registry.QUERY_VALUE)
	if err != nil {
		return "", false, err
	}
	defer key.Close()

	val, valType, err := key.GetStringValue("Path")
	if err != nil {
		if err == registry.ErrNotExist {
			return "", false, nil
		}
		return "", false, err
	}
	return val, valType == registry.EXPAND_SZ, nil
}

// pathContainsDir reports whether the semicolon-separated PATH contains dir.
func pathContainsDir(pathVal, dir string) bool {
	clean := strings.ToLower(strings.TrimRight(filepath.Clean(dir), `\`))
	for _, p := range strings.Split(pathVal, ";") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if strings.ToLower(strings.TrimRight(filepath.Clean(p), `\`)) == clean {
			return true
		}
	}
	return false
}

// writeUserPath writes the per-user PATH value to HKCU\Environment.
func writeUserPath(value string, expandable bool) error {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, userEnvKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()

	if expandable {
		return key.SetExpandStringValue("Path", value)
	}
	return key.SetStringValue("Path", value)
}

// runServiceLink adds the Tergum binary's directory to the per-user PATH.
func runServiceLink() error {
	dir, err := binaryDir()
	if err != nil {
		return err
	}

	current, expandable, err := readUserPath()
	if err != nil {
		return fmt.Errorf("reading user PATH: %w", err)
	}

	if pathContainsDir(current, dir) {
		printOutput(
			map[string]interface{}{"status": "linked", "dir": dir, "already": true},
			fmt.Sprintf("'tergum' is already on your PATH (%s).", dir),
		)
		return nil
	}

	newVal := dir
	if current != "" {
		newVal = strings.TrimRight(current, ";") + ";" + dir
	}
	if err := writeUserPath(newVal, expandable); err != nil {
		return fmt.Errorf("updating user PATH: %w", err)
	}

	printOutput(
		map[string]interface{}{"status": "linked", "dir": dir, "already": false},
		fmt.Sprintf("Added %s to your user PATH.\nOpen a new terminal (or log out and back in) to run 'tergum' from anywhere.", dir),
	)
	return nil
}

// runServiceUnlink removes the Tergum binary's directory from the per-user PATH.
func runServiceUnlink() error {
	dir, err := binaryDir()
	if err != nil {
		return err
	}

	current, expandable, err := readUserPath()
	if err != nil {
		return fmt.Errorf("reading user PATH: %w", err)
	}

	if current == "" || !pathContainsDir(current, dir) {
		printOutput(
			map[string]interface{}{"status": "unlinked", "dir": dir, "removed": false},
			"Tergum's directory was not on your PATH (nothing to remove).",
		)
		return nil
	}

	clean := strings.ToLower(strings.TrimRight(filepath.Clean(dir), `\`))
	kept := make([]string, 0)
	for _, p := range strings.Split(current, ";") {
		trimmed := strings.TrimSpace(p)
		if trimmed == "" {
			continue
		}
		if strings.ToLower(strings.TrimRight(filepath.Clean(trimmed), `\`)) == clean {
			continue
		}
		kept = append(kept, trimmed)
	}
	if err := writeUserPath(strings.Join(kept, ";"), expandable); err != nil {
		return fmt.Errorf("updating user PATH: %w", err)
	}

	printOutput(
		map[string]interface{}{"status": "unlinked", "dir": dir, "removed": true},
		fmt.Sprintf("Removed %s from your user PATH.\nOpen a new terminal for the change to take effect.", dir),
	)
	return nil
}

// maybeLinkOnEnable attempts to add the binary dir to PATH during 'service enable'.
// Failures are non-fatal and only warned about.
func maybeLinkOnEnable() {
	dir, err := binaryDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not resolve binary directory for PATH: %v\n", err)
		return
	}
	current, expandable, err := readUserPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not read user PATH: %v\n", err)
		return
	}
	if pathContainsDir(current, dir) {
		return
	}
	newVal := dir
	if current != "" {
		newVal = strings.TrimRight(current, ";") + ";" + dir
	}
	if err := writeUserPath(newVal, expandable); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not add %s to user PATH: %v\n", dir, err)
		return
	}
	fmt.Printf("Added %s to your user PATH. Open a new terminal to run 'tergum' from anywhere.\n", dir)
}
