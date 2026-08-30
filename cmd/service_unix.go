//go:build !windows

package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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

// --- Autostart (Linux systemd user unit / macOS launchd LaunchAgent) ---

// runServiceEnable registers Tergum to start automatically on login/boot.
func runServiceEnable(envPath string) error {
	p, err := resolveAutostartParams(envPath)
	if err != nil {
		return err
	}

	// Make the 'tergum' command available on PATH (non-fatal on failure).
	maybeLinkOnEnable()

	if runtime.GOOS == "darwin" {
		return enableLaunchd(p)
	}
	return enableSystemd(p)
}

// runServiceDisable removes the autostart registration.
func runServiceDisable() error {
	if runtime.GOOS == "darwin" {
		return disableLaunchd()
	}
	return disableSystemd()
}

// --- Linux: systemd user service ---

func systemdUnitPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "systemd", "user", "tergum.service"), nil
}

func enableSystemd(p autostartParams) error {
	unitPath, err := systemdUnitPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(unitPath), 0755); err != nil {
		return fmt.Errorf("creating systemd user dir: %w", err)
	}

	// Build ExecStart. We use the "service start" path via the server subcommand
	// directly so systemd owns the process lifecycle (no double-forking).
	execArgs := []string{p.Binary, "server"}
	if p.ConfigTo != "" {
		execArgs = append(execArgs, "--config", p.ConfigTo)
	}

	var b strings.Builder
	b.WriteString("[Unit]\n")
	b.WriteString("Description=Tergum encrypted backup service\n")
	b.WriteString("After=network-online.target\n")
	b.WriteString("Wants=network-online.target\n\n")
	b.WriteString("[Service]\n")
	b.WriteString("Type=simple\n")
	if p.WorkDir != "" {
		fmt.Fprintf(&b, "WorkingDirectory=%s\n", p.WorkDir)
	}
	if p.EnvFile != "" {
		fmt.Fprintf(&b, "EnvironmentFile=%s\n", p.EnvFile)
	}
	fmt.Fprintf(&b, "ExecStart=%s\n", quoteExecArgs(execArgs))
	b.WriteString("Restart=on-failure\n")
	b.WriteString("RestartSec=5\n\n")
	b.WriteString("[Install]\n")
	b.WriteString("WantedBy=default.target\n")

	if err := os.WriteFile(unitPath, []byte(b.String()), 0644); err != nil {
		return fmt.Errorf("writing systemd unit: %w", err)
	}

	// Try to enable via systemctl --user. Non-fatal if unavailable.
	systemctlAvailable := false
	if _, lookErr := exec.LookPath("systemctl"); lookErr == nil {
		systemctlAvailable = true
		_ = runQuiet("systemctl", "--user", "daemon-reload")
		if out, enErr := exec.Command("systemctl", "--user", "enable", "--now", "tergum.service").CombinedOutput(); enErr != nil {
			fmt.Fprintf(os.Stderr, "Warning: 'systemctl --user enable' failed: %v\n%s\n", enErr, strings.TrimSpace(string(out)))
			systemctlAvailable = false
		}
	}

	msg := fmt.Sprintf("Tergum autostart enabled (systemd user unit).\nUnit: %s", unitPath)
	if !systemctlAvailable {
		msg += "\nNote: systemctl was not available or failed. To finish enabling manually, run:\n" +
			"  systemctl --user daemon-reload\n" +
			"  systemctl --user enable --now tergum.service\n" +
			"To start on boot without an active login session, run: sudo loginctl enable-linger $USER"
	} else {
		msg += "\nThe service will start on login. For boot without login, run: sudo loginctl enable-linger $USER"
	}

	printOutput(
		map[string]interface{}{
			"status":    "enabled",
			"mechanism": "systemd-user",
			"unit":      unitPath,
		},
		msg,
	)
	return nil
}

func disableSystemd() error {
	unitPath, err := systemdUnitPath()
	if err != nil {
		return err
	}

	if _, lookErr := exec.LookPath("systemctl"); lookErr == nil {
		_ = runQuiet("systemctl", "--user", "disable", "--now", "tergum.service")
	}

	removed := false
	if err := os.Remove(unitPath); err == nil {
		removed = true
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("removing systemd unit: %w", err)
	}

	if _, lookErr := exec.LookPath("systemctl"); lookErr == nil {
		_ = runQuiet("systemctl", "--user", "daemon-reload")
	}

	msg := "Tergum autostart disabled (systemd user unit removed)."
	if !removed {
		msg = "Tergum autostart was not enabled (no systemd user unit found)."
	}
	printOutput(
		map[string]interface{}{
			"status":    "disabled",
			"mechanism": "systemd-user",
			"unit":      unitPath,
			"removed":   removed,
		},
		msg,
	)
	return nil
}

// --- macOS: launchd LaunchAgent ---

const launchdLabel = "com.tergum.tergum"

func launchdPlistPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", launchdLabel+".plist"), nil
}

func enableLaunchd(p autostartParams) error {
	plistPath, err := launchdPlistPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(plistPath), 0755); err != nil {
		return fmt.Errorf("creating LaunchAgents dir: %w", err)
	}

	logPath := filepath.Join(p.WorkDir, "tergum-autostart.log")

	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")
	b.WriteString(`<plist version="1.0">` + "\n<dict>\n")
	fmt.Fprintf(&b, "  <key>Label</key>\n  <string>%s</string>\n", launchdLabel)
	b.WriteString("  <key>ProgramArguments</key>\n  <array>\n")
	fmt.Fprintf(&b, "    <string>%s</string>\n", plistEscape(p.Binary))
	b.WriteString("    <string>server</string>\n")
	if p.ConfigTo != "" {
		b.WriteString("    <string>--config</string>\n")
		fmt.Fprintf(&b, "    <string>%s</string>\n", plistEscape(p.ConfigTo))
	}
	b.WriteString("  </array>\n")
	if p.WorkDir != "" {
		fmt.Fprintf(&b, "  <key>WorkingDirectory</key>\n  <string>%s</string>\n", plistEscape(p.WorkDir))
	}
	if p.EnvFile != "" {
		// launchd cannot read an env file directly; document the location.
		// We rely on the process loading .env from WorkingDirectory at startup.
		_ = p.EnvFile
	}
	b.WriteString("  <key>RunAtLoad</key>\n  <true/>\n")
	b.WriteString("  <key>KeepAlive</key>\n  <true/>\n")
	fmt.Fprintf(&b, "  <key>StandardOutPath</key>\n  <string>%s</string>\n", plistEscape(logPath))
	fmt.Fprintf(&b, "  <key>StandardErrorPath</key>\n  <string>%s</string>\n", plistEscape(logPath))
	b.WriteString("</dict>\n</plist>\n")

	if err := os.WriteFile(plistPath, []byte(b.String()), 0644); err != nil {
		return fmt.Errorf("writing LaunchAgent plist: %w", err)
	}

	loaded := false
	if _, lookErr := exec.LookPath("launchctl"); lookErr == nil {
		// Unload any prior version, then load.
		_ = runQuiet("launchctl", "unload", plistPath)
		if out, ldErr := exec.Command("launchctl", "load", "-w", plistPath).CombinedOutput(); ldErr != nil {
			fmt.Fprintf(os.Stderr, "Warning: 'launchctl load' failed: %v\n%s\n", ldErr, strings.TrimSpace(string(out)))
		} else {
			loaded = true
		}
	}

	msg := fmt.Sprintf("Tergum autostart enabled (launchd LaunchAgent).\nPlist: %s", plistPath)
	if !loaded {
		msg += "\nNote: launchctl was not available or failed. To finish enabling manually, run:\n" +
			"  launchctl load -w " + plistPath
	}
	printOutput(
		map[string]interface{}{
			"status":    "enabled",
			"mechanism": "launchd",
			"plist":     plistPath,
		},
		msg,
	)
	return nil
}

func disableLaunchd() error {
	plistPath, err := launchdPlistPath()
	if err != nil {
		return err
	}

	if _, lookErr := exec.LookPath("launchctl"); lookErr == nil {
		_ = runQuiet("launchctl", "unload", "-w", plistPath)
	}

	removed := false
	if err := os.Remove(plistPath); err == nil {
		removed = true
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("removing LaunchAgent plist: %w", err)
	}

	msg := "Tergum autostart disabled (LaunchAgent removed)."
	if !removed {
		msg = "Tergum autostart was not enabled (no LaunchAgent found)."
	}
	printOutput(
		map[string]interface{}{
			"status":    "disabled",
			"mechanism": "launchd",
			"plist":     plistPath,
			"removed":   removed,
		},
		msg,
	)
	return nil
}

// --- PATH linking (Unix: symlink in ~/.local/bin) ---

// linkTargetDir returns the directory where the 'tergum' symlink is installed.
func linkTargetDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "bin"), nil
}

// linkPath returns the full path to the installed 'tergum' symlink.
func linkPath() (string, error) {
	dir, err := linkTargetDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "tergum"), nil
}

// resolveBinary returns the absolute, symlink-resolved path to the running executable.
func resolveBinary() (string, error) {
	binary, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolving executable path: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(binary); err == nil {
		binary = resolved
	}
	return binary, nil
}

// dirOnPath reports whether dir is present in the PATH environment variable.
func dirOnPath(dir string) bool {
	clean := filepath.Clean(dir)
	for _, p := range filepath.SplitList(os.Getenv("PATH")) {
		if filepath.Clean(p) == clean {
			return true
		}
	}
	return false
}

// installLink creates (or refreshes) the 'tergum' symlink pointing at the current
// binary. It returns the symlink path, its target directory, and whether that
// directory is currently on PATH.
func installLink() (link string, dir string, onPath bool, err error) {
	binary, err := resolveBinary()
	if err != nil {
		return "", "", false, err
	}

	dir, err = linkTargetDir()
	if err != nil {
		return "", "", false, err
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", "", false, fmt.Errorf("creating %s: %w", dir, err)
	}

	link, err = linkPath()
	if err != nil {
		return "", "", false, err
	}

	// If the link already points at this binary, leave it alone.
	if existing, lerr := os.Readlink(link); lerr == nil && existing == binary {
		return link, dir, dirOnPath(dir), nil
	}

	// Don't clobber a real file that isn't our symlink.
	if info, serr := os.Lstat(link); serr == nil {
		if info.Mode()&os.ModeSymlink == 0 {
			return "", "", false, fmt.Errorf("%s already exists and is not a symlink; remove it first", link)
		}
		if rerr := os.Remove(link); rerr != nil {
			return "", "", false, fmt.Errorf("replacing existing symlink %s: %w", link, rerr)
		}
	}

	if err := os.Symlink(binary, link); err != nil {
		return "", "", false, fmt.Errorf("creating symlink %s -> %s: %w", link, binary, err)
	}

	return link, dir, dirOnPath(dir), nil
}

// pathHint returns a shell instruction for adding dir to PATH, or "" if already present.
func pathHint(dir string, onPath bool) string {
	if onPath {
		return ""
	}
	return fmt.Sprintf("Note: %s is not on your PATH. Add it by appending this line to your shell profile\n"+
		"(~/.bashrc, ~/.zshrc, or ~/.profile) and restarting your shell:\n"+
		"  export PATH=\"%s:$PATH\"", dir, dir)
}

// runServiceLink installs the 'tergum' symlink so the command is available anywhere.
func runServiceLink() error {
	link, dir, onPath, err := installLink()
	if err != nil {
		return err
	}

	msg := fmt.Sprintf("Linked 'tergum' command.\nSymlink: %s", link)
	if hint := pathHint(dir, onPath); hint != "" {
		msg += "\n" + hint
	} else {
		msg += "\nYou can now run 'tergum' from anywhere."
	}

	printOutput(
		map[string]interface{}{
			"status":  "linked",
			"symlink": link,
			"dir":     dir,
			"on_path": onPath,
		},
		msg,
	)
	return nil
}

// runServiceUnlink removes the 'tergum' symlink created by runServiceLink.
func runServiceUnlink() error {
	link, err := linkPath()
	if err != nil {
		return err
	}

	removed := false
	// Only remove it if it's a symlink we manage.
	if info, serr := os.Lstat(link); serr == nil {
		if info.Mode()&os.ModeSymlink == 0 {
			return fmt.Errorf("%s is not a symlink managed by tergum; leaving it untouched", link)
		}
		if rerr := os.Remove(link); rerr != nil {
			return fmt.Errorf("removing symlink %s: %w", link, rerr)
		}
		removed = true
	}

	msg := "Removed 'tergum' command symlink."
	if !removed {
		msg = "No 'tergum' symlink found (nothing to remove)."
	}
	printOutput(
		map[string]interface{}{
			"status":  "unlinked",
			"symlink": link,
			"removed": removed,
		},
		msg,
	)
	return nil
}

// maybeLinkOnEnable attempts to install the PATH link during 'service enable',
// printing a short summary. Failures are non-fatal and only warned about.
func maybeLinkOnEnable() {
	link, dir, onPath, err := installLink()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not link 'tergum' onto PATH: %v\n", err)
		return
	}
	fmt.Printf("Linked 'tergum' command: %s\n", link)
	if hint := pathHint(dir, onPath); hint != "" {
		fmt.Println(hint)
	}
}

// --- helpers ---

// runQuiet runs a command discarding output, returning any error.
func runQuiet(name string, args ...string) error {
	return exec.Command(name, args...).Run()
}

// quoteExecArgs joins exec args for a systemd ExecStart line, quoting any
// argument that contains whitespace.
func quoteExecArgs(args []string) string {
	quoted := make([]string, len(args))
	for i, a := range args {
		if strings.ContainsAny(a, " \t") {
			quoted[i] = "\"" + a + "\""
		} else {
			quoted[i] = a
		}
	}
	return strings.Join(quoted, " ")
}

// plistEscape escapes XML special characters for use inside a plist <string>.
func plistEscape(s string) string {
	r := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
	)
	return r.Replace(s)
}
