package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gcclinux/tergum/internal/config"
	"github.com/gcclinux/tergum/internal/envfile"
)

func newServiceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "service",
		Short: "Manage the Tergum background service",
		Long: `Manage the Tergum daemon as a background service. The appropriate services
and ports are started based on the configured node role (client, server, or hybrid).

The service commands load environment variables from a .env file (default: .env
in the current directory) before starting the process.`,
	}

	cmd.AddCommand(newServiceStartCmd())
	cmd.AddCommand(newServiceStopCmd())
	cmd.AddCommand(newServiceRestartCmd())
	cmd.AddCommand(newServiceStatusCmd())
	cmd.AddCommand(newServiceEnableCmd())
	cmd.AddCommand(newServiceDisableCmd())
	cmd.AddCommand(newServiceLinkCmd())
	cmd.AddCommand(newServiceUnlinkCmd())

	return cmd
}

func newServiceLinkCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "link",
		Short: "Make the 'tergum' command available on your PATH",
		Long: `Installs the Tergum binary so the 'tergum' command can be run from anywhere,
without needing to type the full path to the executable. The mechanism is
platform-specific and requires no administrator/root privileges:

  Linux/macOS - a symlink named 'tergum' in ~/.local/bin
  Windows     - the binary's directory is added to the per-user PATH (HKCU)

If the target directory is not already on your PATH, the command prints
instructions for adding it.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runServiceLink()
		},
	}

	return cmd
}

func newServiceUnlinkCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unlink",
		Short: "Remove the 'tergum' command from your PATH",
		Long:  `Removes the PATH entry created by 'tergum service link'. Safe to run even if link was never used.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runServiceUnlink()
		},
	}

	return cmd
}

func newServiceEnableCmd() *cobra.Command {
	var envPath string

	cmd := &cobra.Command{
		Use:   "enable",
		Short: "Enable autostart of the Tergum service on system boot/login",
		Long: `Registers the Tergum service to start automatically when the machine boots
(or when the current user logs in). The mechanism is platform-specific:

  Linux   - a systemd user service unit (~/.config/systemd/user/tergum.service)
  macOS   - a launchd LaunchAgent (~/Library/LaunchAgents/com.tergum.tergum.plist)
  Windows - a per-user autostart entry (HKCU Run key / Startup)

The service is launched with the current configuration and the given .env file.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runServiceEnable(envPath)
		},
	}

	cmd.Flags().StringVar(&envPath, "env-file", ".env", "path to .env file used at autostart")

	return cmd
}

func newServiceDisableCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "disable",
		Short: "Disable autostart of the Tergum service on system boot/login",
		Long:  `Removes the platform-specific autostart registration created by 'tergum service enable'.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runServiceDisable()
		},
	}

	return cmd
}

func newServiceStartCmd() *cobra.Command {
	var envPath string

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the Tergum service in the background",
		Long: `Starts the Tergum daemon as a background process based on the configured
node role. Environment variables are loaded from the .env file before launch.

The process PID is stored in the config directory for later stop/restart.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runServiceStart(envPath)
		},
	}

	cmd.Flags().StringVar(&envPath, "env-file", ".env", "path to .env file")

	return cmd
}

func newServiceStopCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop the running Tergum service",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runServiceStop()
		},
	}

	return cmd
}

func newServiceRestartCmd() *cobra.Command {
	var envPath string

	cmd := &cobra.Command{
		Use:   "restart",
		Short: "Restart the Tergum service",
		Long:  `Stops the running service (if any) and starts it again with the current configuration.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Stop ignoring errors (process may not be running).
			_ = runServiceStop()
			return runServiceStart(envPath)
		},
	}

	cmd.Flags().StringVar(&envPath, "env-file", ".env", "path to .env file")

	return cmd
}

func newServiceStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Check if the Tergum service is running",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runServiceStatus()
		},
	}

	return cmd
}

// runServiceStart loads .env, determines the role, and spawns tergum in the background.
func runServiceStart(envPath string) error {
	// Check if already running.
	if pid, running := readPID(); running {
		return fmt.Errorf("tergum service is already running (PID %d). Use 'tergum service restart' to restart", pid)
	}

	// Load .env file (non-fatal if missing).
	if err := envfile.Load(envPath, false); err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("loading env file: %w", err)
		}
		fmt.Printf("Note: no .env file found at %q, using existing environment.\n", envPath)
	}

	// Load config to determine role.
	cfgPath := os.Getenv("TERGUM_CONFIG")
	if cfgFile != "" {
		cfgPath = cfgFile
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// Determine which subcommand to run based on role.
	// The "server" subcommand handles all roles internally (server, hybrid, client).
	var subCmd string
	switch cfg.Node.Role {
	case "server", "hybrid", "client":
		subCmd = "server"
	default:
		return fmt.Errorf("unknown node role %q; expected client, server, or hybrid", cfg.Node.Role)
	}

	// Find the tergum binary path.
	binary, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolving executable path: %w", err)
	}

	// Build arguments for the background process.
	cmdArgs := []string{subCmd}
	if cfgPath != "" {
		cmdArgs = append(cmdArgs, "--config", cfgPath)
	}

	// Spawn the background process.
	proc := exec.Command(binary, cmdArgs...)
	proc.Env = os.Environ()

	// Detach from terminal (platform-specific).
	detachProcess(proc)

	// Redirect output to log file in config dir.
	logPath := filepath.Join(config.DefaultConfigDir(), "tergum-service.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("opening log file: %w", err)
	}

	proc.Stdout = logFile
	proc.Stderr = logFile

	if err := proc.Start(); err != nil {
		logFile.Close()
		return fmt.Errorf("starting service: %w", err)
	}

	// Write PID file.
	if err := writePID(proc.Process.Pid); err != nil {
		// Non-fatal: the process is running, just can't track it.
		fmt.Fprintf(os.Stderr, "Warning: could not write PID file: %v\n", err)
	}

	printOutput(
		map[string]interface{}{
			"status": "started",
			"pid":    proc.Process.Pid,
			"role":   cfg.Node.Role,
			"log":    logPath,
		},
		fmt.Sprintf("Tergum service started (role: %s, PID: %d).\nLogs: %s", cfg.Node.Role, proc.Process.Pid, logPath),
	)

	logFile.Close()
	return nil
}

// runServiceStop sends a termination signal to the running service.
func runServiceStop() error {
	pid, running := readPID()
	if !running {
		printOutput(
			map[string]interface{}{"status": "not_running"},
			"Tergum service is not running.",
		)
		return nil
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		removePIDFile()
		return fmt.Errorf("finding process %d: %w", pid, err)
	}

	// Send termination signal and wait for process to exit.
	if err := terminateProcess(process); err != nil {
		removePIDFile()
		return fmt.Errorf("stopping process %d: %w", pid, err)
	}

	// Verify the process is actually gone.
	if isProcessRunning(pid) {
		return fmt.Errorf("process %d is still running after stop attempt", pid)
	}

	removePIDFile()

	printOutput(
		map[string]interface{}{
			"status": "stopped",
			"pid":    pid,
		},
		fmt.Sprintf("Tergum service stopped (PID: %d).", pid),
	)

	return nil
}

// runServiceStatus reports whether the service is running.
func runServiceStatus() error {
	pid, running := readPID()
	if !running {
		printOutput(
			map[string]interface{}{"status": "stopped"},
			"Tergum service is not running.",
		)
		return nil
	}

	printOutput(
		map[string]interface{}{
			"status": "running",
			"pid":    pid,
		},
		fmt.Sprintf("Tergum service is running (PID: %d).", pid),
	)

	return nil
}

// autostartParams holds the resolved values needed to register an autostart entry.
type autostartParams struct {
	Binary   string // absolute path to the tergum executable
	ConfigTo string // config file path to pass (may be empty)
	EnvFile  string // absolute path to the .env file (may be empty if not found)
	WorkDir  string // working directory to run the service from
}

// resolveAutostartParams gathers the values used to build a platform autostart entry.
func resolveAutostartParams(envPath string) (autostartParams, error) {
	var p autostartParams

	binary, err := os.Executable()
	if err != nil {
		return p, fmt.Errorf("resolving executable path: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(binary); err == nil {
		binary = resolved
	}
	p.Binary = binary

	// Resolve config path the same way runServiceStart does.
	cfgPath := os.Getenv("TERGUM_CONFIG")
	if cfgFile != "" {
		cfgPath = cfgFile
	}
	p.ConfigTo = cfgPath

	// Resolve the env file to an absolute path if it exists.
	if envPath != "" {
		if abs, err := filepath.Abs(envPath); err == nil {
			if _, statErr := os.Stat(abs); statErr == nil {
				p.EnvFile = abs
			}
		}
	}

	// Working directory: prefer the directory containing the env file, else cwd.
	if p.EnvFile != "" {
		p.WorkDir = filepath.Dir(p.EnvFile)
	} else if cwd, err := os.Getwd(); err == nil {
		p.WorkDir = cwd
	}

	return p, nil
}

// --- PID file helpers ---

func pidFilePath() string {
	return filepath.Join(config.DefaultConfigDir(), "tergum.pid")
}

func writePID(pid int) error {
	dir := config.DefaultConfigDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return os.WriteFile(pidFilePath(), []byte(strconv.Itoa(pid)), 0600)
}

func readPID() (int, bool) {
	data, err := os.ReadFile(pidFilePath())
	if err != nil {
		return 0, false
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, false
	}

	// Verify the process is actually running.
	if !isProcessRunning(pid) {
		removePIDFile()
		return 0, false
	}

	return pid, true
}

func removePIDFile() {
	os.Remove(pidFilePath())
}
