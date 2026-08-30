# Tergum CLI Reference

## Command Index

| Command | Status | Description |
|---------|--------|-------------|
| [`tergum setup`](#tergum-setup) | ✅ Working | Interactive configuration wizard |
| [`tergum server`](#tergum-server) | ✅ Working | Start the Tergum daemon (role-aware: server, client, or hybrid) |
| [`tergum client`](#tergum-client) | ✅ Working | Manage and view remote clients (server-side) |
| [`tergum client list`](#tergum-client-list) | ✅ Working | List all registered clients and their status |
| [`tergum client status`](#tergum-client-status) | ✅ Working | Show detailed status for a specific client |
| [`tergum client disable`](#tergum-client-disable) | ✅ Working | Disable a client (no backups, restores, or monitoring) |
| [`tergum client enable`](#tergum-client-enable) | ✅ Working | Re-enable a previously disabled client |
| [`tergum admin`](#tergum-admin) | ✅ Working | Start Web UI only (lightweight) |
| [`tergum node`](#tergum-node) | ✅ Working | Manage node role and hostname settings |
| [`tergum node show`](#tergum-node-show) | ✅ Working | Show current node role and hostname |
| [`tergum node role set`](#tergum-node-role-set) | ✅ Working | Change node role (server/hybrid) |
| [`tergum node hostname set`](#tergum-node-hostname-set) | ✅ Working | Set the hostname (network interface) |
| [`tergum node hostname clear`](#tergum-node-hostname-clear) | ✅ Working | Clear the hostname setting |
| [`tergum service`](#tergum-service) | ✅ Working | Manage the Tergum background service |
| [`tergum service start`](#tergum-service-start) | ✅ Working | Start the service in the background |
| [`tergum service stop`](#tergum-service-stop) | ✅ Working | Stop the running service |
| [`tergum service restart`](#tergum-service-restart) | ✅ Working | Restart the service |
| [`tergum service status`](#tergum-service-status) | ✅ Working | Check if the service is running |
| [`tergum service enable`](#tergum-service-enable) | ✅ Working | Enable autostart on boot/login (all OSes) |
| [`tergum service disable`](#tergum-service-disable) | ✅ Working | Disable autostart on boot/login |
| [`tergum service link`](#tergum-service-link) | ✅ Working | Make the `tergum` command available on PATH |
| [`tergum service unlink`](#tergum-service-unlink) | ✅ Working | Remove the `tergum` command from PATH |
| [`tergum backup`](#tergum-backup) | ✅ Working | Run a manual backup (local or remote) |
| [`tergum paths`](#tergum-paths) | ✅ Working | Manage include/exclude paths |
| [`tergum paths scan`](#tergum-paths-scan) | ✅ Working | Scan a directory and add top-level folders |
| [`tergum paths add`](#tergum-paths-add) | ✅ Working | Add one or more include paths |
| [`tergum paths remove`](#tergum-paths-remove) | ✅ Working | Remove one or more include paths |
| [`tergum paths exclude`](#tergum-paths-exclude) | ✅ Working | Add exclude patterns (glob syntax) |
| [`tergum paths unexclude`](#tergum-paths-unexclude) | ✅ Working | Remove exclude patterns |
| [`tergum paths list`](#tergum-paths-list) | ✅ Working | Show all include paths and exclude patterns |
| [`tergum list`](#tergum-list) | ✅ Working | List backup sets and files |
| [`tergum delete`](#tergum-delete) | ✅ Working | Delete backup entries |
| [`tergum retention`](#tergum-retention) | ✅ Working | Manage retention policies |
| [`tergum retention list`](#tergum-retention-list) | ✅ Working | List configured retention policies |
| [`tergum retention add`](#tergum-retention-add) | ✅ Working | Add a retention policy |
| [`tergum retention remove`](#tergum-retention-remove) | ✅ Working | Remove a retention policy |
| [`tergum retention run`](#tergum-retention-run) | ✅ Working | Manually trigger retention evaluation |
| [`tergum restore`](#tergum-restore) | ✅ Working | Restore files from backup (local or remote) |
| [`tergum stop`](#tergum-stop) | ✅ Working | Stop an in-progress backup |
| [`tergum status`](#tergum-status) | ✅ Working | Show system status |
| [`tergum watch`](#tergum-watch) | ✅ Working | File watcher for ongoing backup (local or remote) |
| [`tergum watch run`](#tergum-watch-run) | ✅ Working | Start file watcher in the foreground |
| [`tergum watch enable`](#tergum-watch-enable) | ✅ Working | Enable the file watcher in configuration |
| [`tergum watch disable`](#tergum-watch-disable) | ✅ Working | Disable the file watcher in configuration |
| [`tergum watch start`](#tergum-watch-start) | ✅ Working | Start the file watcher on a remote client (server-side only) |
| [`tergum watch stop`](#tergum-watch-stop) | ✅ Working | Stop the file watcher on a remote client (server-side only) |
| [`tergum version`](#tergum-version) | ✅ Working | Print version info |

---

## Global Flags

Available on every command:

```
--config string   Config file path (default: ~/.config/tergum/tergum.toml)
--json            Output machine-readable JSON
--dry-run         Preview destructive operations without executing
```

---

## Environment Variables

| Variable | Description |
|----------|-------------|
| `TERGUM_PASSPHRASE` | Encryption passphrase (avoids interactive prompt) |
| `TERGUM_CONFIG` | Path to configuration file (used by `service start`) |
| `TERGUM_LOG_LEVEL` | Log level: debug, info, warn, error (default: info) |

---

## Commands

### tergum setup

Interactive configuration wizard for first-time setup or reconfiguration.

```
Usage: tergum setup [flags]

Flags:
  --generate-certs   Generate TLS certificates without interactive prompts
```

The wizard walks you through: node role, server address, storage path, TLS certificates, encryption passphrase, include paths, exclude patterns, file watcher, and backup schedule.

**Linux / macOS:**
```bash
# Full interactive setup
tergum setup

# Regenerate TLS certificates only
tergum setup --generate-certs
```

**PowerShell (Windows):**
```powershell
# Full interactive setup
.\tergum.exe setup

# Regenerate TLS certificates only
.\tergum.exe setup --generate-certs
```

---

### tergum server

Start the daemon in role-aware mode based on `[node].role` in config.

```
Usage: tergum server [flags]

Starts in role-aware mode based on [node].role in config:
  - role "server": gRPC services (7400, 7401), web UI (7480), metrics (7490),
    retention engine, scheduler, client registry
  - role "client": client CommandService (7400), heartbeat to server,
    file watcher (if enabled), accepts remote triggers
  - role "both": full local server (same as "server" without client registry)

Graceful shutdown on SIGTERM/SIGINT.
```

**Linux / macOS:**
```bash
# Start in the foreground
tergum server

# Start with encryption passphrase (needed for scheduled/watcher backups)
TERGUM_PASSPHRASE=mypassphrase tergum server

# Start in the background
tergum server &
```

**PowerShell (Windows):**
```powershell
# Start in the foreground
.\tergum.exe server

# Start with encryption passphrase (needed for scheduled/watcher backups)
$env:TERGUM_PASSPHRASE="mypassphrase"; .\tergum.exe server

# Start in the background
Start-Process -FilePath ".\tergum.exe" -ArgumentList "server" -NoNewWindow
```

---

### tergum client

Manage and view remote backup clients registered with this server. Only available on nodes with role `server` or `hybrid`.

```
Usage: tergum client <subcommand>

Subcommands:
  list                   List all registered clients and their online/offline status
  status <client-name>   Show detailed status for a specific client
  disable <client-name>  Disable a client (no backups, restores, or monitoring from server)
  enable <client-name>   Re-enable a previously disabled client
```

---

#### tergum client list

List all registered clients with their address, online/offline status, and last seen time.

```
Usage: tergum client list [flags]

Flags:
  --json   Output as JSON
```

**Linux / macOS:**
```bash
tergum client list
tergum client list --json
```

**PowerShell (Windows):**
```powershell
.\tergum.exe client list
.\tergum.exe client list --json
```

**Example output:**
```
CLIENT          ADDRESS           STATUS    LAST SEEN
------          -------           ------    ---------
fedora-laptop   192.168.1.100     online    2 minutes ago
ubuntu-server   192.168.1.214     offline   3 hours ago
win-desktop     192.168.1.50      online    just now
```

---

#### tergum client status

Show detailed status for a specific client, including last backup time, watcher state, schedule configuration, and any missed backups.

```
Usage: tergum client status <client-name> [flags]

Flags:
  --json   Output as JSON
```

**Linux / macOS:**
```bash
tergum client status fedora-laptop
tergum client status fedora-laptop --json
```

**PowerShell (Windows):**
```powershell
.\tergum.exe client status fedora-laptop
.\tergum.exe client status fedora-laptop --json
```

**Example output:**
```
Client:         fedora-laptop
Address:        192.168.1.100
Status:         online
Last Seen:      2026-06-29 14:32:10 (2 minutes ago)
Last Backup:    2026-06-29 03:00:05 (11 hours ago)
Watcher Active: true
Registered:     2026-06-15 09:20:00
Schedule:
  Full Backup:  0 3 * * 0
  Auto Backup:  0 */6 * * *
```

---

#### tergum client disable

Disable a client on the server side. A disabled client is fully ignored by the server:
- No scheduled backups will be triggered
- No heartbeat/status polling (client appears frozen in time)
- Incoming backup uploads and database syncs from the client are rejected
- Cannot be triggered for backup, watcher start/stop, or restore from the server
- The client record is preserved and can be re-enabled at any time

```
Usage: tergum client disable <client-name> [flags]

Flags:
  --json   Output as JSON
```

**Linux / macOS:**
```bash
tergum client disable fedora-laptop
tergum client disable fedora-laptop --json
```

**PowerShell (Windows):**
```powershell
.\tergum.exe client disable fedora-laptop
.\tergum.exe client disable fedora-laptop --json
```

**Example output:**
```
Client "fedora-laptop" disabled.
```

---

#### tergum client enable

Re-enable a previously disabled client. The client will resume normal operation on the next heartbeat cycle (within 30 seconds).

```
Usage: tergum client enable <client-name> [flags]

Flags:
  --json   Output as JSON
```

**Linux / macOS:**
```bash
tergum client enable fedora-laptop
tergum client enable fedora-laptop --json
```

**PowerShell (Windows):**
```powershell
.\tergum.exe client enable fedora-laptop
.\tergum.exe client enable fedora-laptop --json
```

**Example output:**
```
Client "fedora-laptop" enabled.
```

---

### tergum admin

Start the Web UI only, without gRPC services, scheduler, or watcher. Use this for lightweight browser-based management.

```
Usage: tergum admin [flags]

Flags:
  -p, --port int   Override web UI port (default: from config, typically 7480)
```

**Linux / macOS:**
```bash
# Start on default port
tergum admin

# Start on a custom port
tergum admin --port 8080
```

**PowerShell (Windows):**
```powershell
# Start on default port
.\tergum.exe admin

# Start on a custom port
.\tergum.exe admin --port 8080
```

---

### tergum node

Manage the node's role and hostname identity settings.

```
Usage: tergum node <subcommand>

Subcommands:
  show                   Show current node role and hostname
  role set <role>        Change node role (server or hybrid)
  hostname set <host>    Set the hostname (network interface address)
  hostname clear         Clear the hostname setting (use system default)

Global flags apply:
  --json                 Output as JSON
```

Changing the role controls behavior at next server restart:
- **server** — serves remote clients only; no local backups, no local file watcher.
- **hybrid** — full server capabilities PLUS local backup, file watcher, and scheduling.

The hostname identifies which network interface address to advertise to remote clients. Useful when the node has multiple network interfaces.

---

#### tergum node show

Show the current node role and hostname.

```
Usage: tergum node show [flags]

Flags:
  --json   Output as JSON
```

**Linux / macOS:**
```bash
tergum node show
tergum node show --json
```

**PowerShell (Windows):**
```powershell
.\tergum.exe node show
.\tergum.exe node show --json
```

---

#### tergum node role set

Change the node role between "server" and "hybrid". Requires a server restart to take effect.

```
Usage: tergum node role set <server|hybrid> [flags]

Flags:
  --json   Output as JSON
```

**Linux / macOS:**
```bash
# Switch to hybrid (enables local backups and watcher)
tergum node role set hybrid

# Switch back to server-only mode
tergum node role set server
```

**PowerShell (Windows):**
```powershell
# Switch to hybrid (enables local backups and watcher)
.\tergum.exe node role set hybrid

# Switch back to server-only mode
.\tergum.exe node role set server
```

---

#### tergum node hostname set

Set the hostname used to identify the network interface for backup traffic. Requires a server restart to take effect.

```
Usage: tergum node hostname set <hostname> [flags]

Flags:
  --json   Output as JSON
```

**Linux / macOS:**
```bash
# Set to a specific IP address
tergum node hostname set 192.168.1.10

# Set to a DNS name
tergum node hostname set backup.internal.example.com
```

**PowerShell (Windows):**
```powershell
# Set to a specific IP address
.\tergum.exe node hostname set 192.168.1.10

# Set to a DNS name
.\tergum.exe node hostname set backup.internal.example.com
```

---

#### tergum node hostname clear

Clear the hostname setting so the system default is used. Requires a server restart to take effect.

```
Usage: tergum node hostname clear [flags]

Flags:
  --json   Output as JSON
```

**Linux / macOS:**
```bash
tergum node hostname clear
```

**PowerShell (Windows):**
```powershell
.\tergum.exe node hostname clear
```

---

### tergum service

Manage the Tergum daemon as a background service. The appropriate services and ports are started based on the configured node role (client, server, or hybrid).

```
Usage: tergum service <subcommand>

Subcommands:
  start     Start the service in the background
  stop      Stop the running service
  restart   Restart the service
  status    Check if the service is running
  enable    Enable autostart of the service on system boot/login
  disable   Disable autostart of the service on system boot/login
  link      Make the 'tergum' command available on your PATH
  unlink    Remove the 'tergum' command from your PATH
```

The service commands load environment variables from a `.env` file before launching, eliminating the need for manual `nohup`, `env`, or `&` background tricks. A PID file is stored in the config directory for process tracking.

**`.env` file format:**
```bash
# Required: Passphrase for encryption key derivation
TERGUM_PASSPHRASE=your-secure-passphrase-here

# Optional: Path to configuration file
# TERGUM_CONFIG=/path/to/tergum.toml

# Optional: Log level (debug, info, warn, error)
# TERGUM_LOG_LEVEL=info
```

---

#### tergum service start

Start the Tergum daemon as a background process. Loads the `.env` file, determines the node role from config, and spawns the appropriate daemon (`server` or `client`).

```
Usage: tergum service start [flags]

Flags:
  --env-file string   Path to .env file (default ".env")
```

The PID is stored in the platform config directory for later stop/restart. Logs are written to `tergum-service.log` in the same directory.

**Linux / macOS:**
```bash
# Create .env from the template
cp .env.example .env
# Edit .env with your passphrase, then:

# Start the service (reads .env from current directory)
tergum service start

# Start with a custom .env location
tergum service start --env-file /etc/tergum/.env

# Start with a specific config file
tergum service start --config /path/to/tergum.toml
```

**PowerShell (Windows):**
```powershell
# Create .env from the template
Copy-Item .env.example .env
# Edit .env with your passphrase, then:

# Start the service (reads .env from current directory)
.\tergum.exe service start

# Start with a custom .env location
.\tergum.exe service start --env-file C:\tergum\.env

# Start with a specific config file
.\tergum.exe service start --config C:\tergum\tergum.toml
```

---

#### tergum service stop

Stop the running Tergum service by sending a termination signal to the tracked PID.

```
Usage: tergum service stop [flags]
```

On Linux/macOS, sends SIGTERM for graceful shutdown. On Windows, terminates the process.

**Linux / macOS:**
```bash
tergum service stop
```

**PowerShell (Windows):**
```powershell
.\tergum.exe service stop
```

---

#### tergum service restart

Stop the running service (if any) and start it again with the current configuration and `.env` file.

```
Usage: tergum service restart [flags]

Flags:
  --env-file string   Path to .env file (default ".env")
```

**Linux / macOS:**
```bash
# Restart with default .env
tergum service restart

# Restart with updated env file
tergum service restart --env-file /etc/tergum/.env
```

**PowerShell (Windows):**
```powershell
# Restart with default .env
.\tergum.exe service restart

# Restart with updated env file
.\tergum.exe service restart --env-file C:\tergum\.env
```

---

#### tergum service status

Check whether the Tergum service is currently running.

```
Usage: tergum service status [flags]

Flags:
  --json   Output as JSON
```

> Note: This command only checks the PID file written by `tergum service start`. If you enabled autostart with `tergum service enable` on Linux/macOS, the daemon runs under systemd/launchd and **will not** show up here — it will report "not running" even when the daemon is active. Use `systemctl --user status tergum.service` (Linux) or `launchctl list | grep com.tergum.tergum` (macOS) instead. See [Two supervision mechanisms](#two-supervision-mechanisms-important).

**Linux / macOS:**
```bash
tergum service status
tergum service status --json
```

**PowerShell (Windows):**
```powershell
.\tergum.exe service status
.\tergum.exe service status --json
```

---

#### tergum service enable

Register the Tergum service to start automatically when the machine boots or the current user logs in. The mechanism is platform-specific and requires **no administrator/root privileges** — everything is installed under the current user's account:

| OS | Mechanism | Location |
|----|-----------|----------|
| Linux | systemd user service | `~/.config/systemd/user/tergum.service` |
| macOS | launchd LaunchAgent | `~/Library/LaunchAgents/com.tergum.tergum.plist` |
| Windows | per-user autostart (HKCU `Run` key) | `HKCU\Software\Microsoft\Windows\CurrentVersion\Run` → `Tergum` |

The autostart entry captures the current executable path, the resolved config path (if `--config` or `TERGUM_CONFIG` is set), and the `.env` file location.

```
Usage: tergum service enable [flags]

Flags:
  --env-file string   path to .env file used at autostart (default ".env")
```

**Linux:**
```bash
# From the directory containing your .env
tergum service enable

# The command writes the systemd unit and, if systemctl is available,
# runs: systemctl --user enable --now tergum.service
#
# To keep the service running on boot without an active login session:
sudo loginctl enable-linger $USER
```

**macOS:**
```bash
tergum service enable
# Writes the LaunchAgent plist and loads it with:
#   launchctl load -w ~/Library/LaunchAgents/com.tergum.tergum.plist
```

**PowerShell (Windows):**
```powershell
# Runs tergum at user login via the HKCU Run key
.\tergum.exe service enable

# With a specific .env file
.\tergum.exe service enable --env-file C:\tergum\.env
```

> Note: On Linux/macOS the service is supervised directly by systemd/launchd (with automatic restart on failure). On Windows the entry runs `tergum service start`, which detaches the daemon and tracks it via the PID file.

> Tip: `service enable` also makes the `tergum` command available on your PATH automatically (same as running [`tergum service link`](#tergum-service-link)), so you can call `tergum` from anywhere afterwards. If the target location isn't already on your PATH, the command prints instructions for adding it. This step is best-effort — if it fails, autostart is still enabled and a warning is printed.

##### Two supervision mechanisms (important)

Tergum has **two independent ways** of running the daemon, and they do not share state:

1. **`service enable`** installs a supervisor (systemd on Linux, launchd on macOS) that runs `tergum server` directly. The supervisor owns the process and restarts it on failure or boot.
2. **`service start` / `stop` / `status` / `restart`** use a separate PID file (`~/.config/tergum/tergum.pid`) to track a process they spawn themselves.

Because these mechanisms are disconnected, mixing them causes confusion:

- After `service enable`, the daemon runs under systemd/launchd. It **will not appear** in `tergum service status`, which only checks the PID file — so `status` reports "not running" even though the daemon is active.
- Running `tergum service start` while the enabled service is already active makes the new process fail to bind its port (e.g. `bind: address already in use` on port 7400). It exits immediately, leaving a stale "started" message followed by `status` reporting "not running".

**On Linux/macOS, once you have run `service enable`, manage the daemon through the supervisor — not through `service start/stop/status`:**

Linux (systemd user service):
```bash
systemctl --user status tergum.service     # check if running
systemctl --user restart tergum.service     # restart
systemctl --user stop tergum.service         # stop
systemctl --user start tergum.service        # start
journalctl --user -u tergum.service -f       # follow logs

# Verify it starts on boot without an active login session:
sudo loginctl enable-linger $USER
```

macOS (launchd LaunchAgent):
```bash
launchctl list | grep com.tergum.tergum                                  # check if loaded
launchctl unload ~/Library/LaunchAgents/com.tergum.tergum.plist          # stop
launchctl load -w ~/Library/LaunchAgents/com.tergum.tergum.plist         # start
```

To go back to using `service start/stop/status`, first run `tergum service disable` so the supervisor releases the port.

On **Windows**, both mechanisms use the same PID-file path (`service enable` runs `service start` at login), so `service status` works as expected there.

---

#### tergum service disable

Remove the autostart registration created by `tergum service enable`. Safe to run even if autostart was never enabled.

```
Usage: tergum service disable [flags]
```

**Linux / macOS:**
```bash
tergum service disable
```

**PowerShell (Windows):**
```powershell
.\tergum.exe service disable
```

---

#### tergum service link

Make the `tergum` command runnable from anywhere, without typing the full path to the binary. Requires **no administrator/root privileges** — everything is installed under the current user's account. This runs automatically as part of [`tergum service enable`](#tergum-service-enable), but you can also run it on its own.

| OS | Mechanism | Location |
|----|-----------|----------|
| Linux / macOS | symlink named `tergum` | `~/.local/bin/tergum` → the binary |
| Windows | binary's directory added to the per-user PATH (HKCU) | `HKCU\Environment` → `Path` |

```
Usage: tergum service link
```

If the target location is not already on your PATH, the command prints instructions for adding it.

**Linux / macOS:**
```bash
# Run from the binary you want linked
./dist/tergum-amd64-linux service link
# Symlink: ~/.local/bin/tergum

# If ~/.local/bin is not on your PATH, add it to your shell profile:
export PATH="$HOME/.local/bin:$PATH"
```

**PowerShell (Windows):**
```powershell
.\tergum.exe service link
# Adds the binary's folder to your user PATH.
# Open a new terminal (or log out/in) for the change to take effect.
```

> Note: On Linux/macOS the symlink points at the exact binary you ran `link` from. If you move or replace that binary, re-run `service link`. On Windows the whole folder containing the binary is added to PATH, so keep the executable in that folder.

---

#### tergum service unlink

Remove the PATH entry created by `tergum service link`. Safe to run even if `link` was never used. It only removes entries Tergum manages (the `~/.local/bin/tergum` symlink on Unix, or the binary's directory from the user PATH on Windows) and leaves anything else untouched.

```
Usage: tergum service unlink
```

**Linux / macOS:**
```bash
tergum service unlink
```

**PowerShell (Windows):**
```powershell
.\tergum.exe service unlink
```

---

### tergum backup

Run a manual backup (local or remote depending on node role).

```
Usage: tergum backup [flags]

Flags:
  -l, --level string   Backup level: auto, full (default "auto")
      --json           Output result as JSON
```

- **Auto** mode backs up files that are new or modified since the last backup.
- **Full** mode backs up everything in the include paths regardless of changes.

**Linux / macOS:**
```bash
# Auto level — only new/modified files since last backup
tergum backup

# Full backup — all files in include paths
tergum backup --level full

# Set passphrase via env var (avoids prompt)
TERGUM_PASSPHRASE=mypassphrase tergum backup

# JSON output for scripting (with encryption)
TERGUM_PASSPHRASE=mypassphrase tergum backup --json

# JSON output for scripting (without encryption)
tergum backup --json
```

**PowerShell (Windows):**
```powershell
# Auto level — only new/modified files since last backup
.\tergum.exe backup

# Full backup — all files in include paths
.\tergum.exe backup --level full

# Set passphrase via env var (avoids prompt)
$env:TERGUM_PASSPHRASE="mypassphrase"; .\tergum.exe backup

# JSON output for scripting (with encryption)
$env:TERGUM_PASSPHRASE="mypassphrase"; .\tergum.exe backup --json

# JSON output for scripting (without encryption)
.\tergum.exe backup --json
```

---

### tergum paths

Manage include and exclude paths for backups.

```
Usage: tergum paths <subcommand>

Subcommands:
  scan [directory]       Scan a directory and add top-level folders
  add [path...]          Add one or more include paths
  remove [path...]       Remove one or more include paths
  exclude [pattern...]   Add exclude patterns (glob syntax)
  unexclude [pattern...] Remove exclude patterns
  list                   Show all include paths and exclude patterns

Global flags apply:
  --json                 Output as JSON (works with all subcommands)
```

#### tergum paths scan

Scan a directory and add all its top-level folders as include paths.

```
Usage: tergum paths scan [directory] [flags]

Flags:
  --include-hidden   Include hidden directories (starting with '.')
  --json             Output as JSON
```

**Linux / macOS:**
```bash
# Scan home directory (skips hidden folders)
tergum paths scan

# Scan specific directory, include hidden folders
tergum paths scan /mnt/data --include-hidden
```

**PowerShell (Windows):**
```powershell
# Scan home directory (skips hidden folders)
.\tergum.exe paths scan

# Scan specific directory, include hidden folders
.\tergum.exe paths scan D:\data --include-hidden
```

#### tergum paths add

Add one or more directories to the include list.

```
Usage: tergum paths add [path...] [flags]

Flags:
  --json   Output as JSON
```

**Linux / macOS:**
```bash
tergum paths add /home/user/Documents /home/user/Projects
```

**PowerShell (Windows):**
```powershell
.\tergum.exe paths add C:\Users\user\Documents C:\Users\user\Projects
```

#### tergum paths remove

Remove one or more directories from the include list.

```
Usage: tergum paths remove [path...] [flags]

Flags:
  --json   Output as JSON
```

**Linux / macOS:**
```bash
tergum paths remove /home/user/Downloads
```

**PowerShell (Windows):**
```powershell
.\tergum.exe paths remove C:\Users\user\Downloads
```

#### tergum paths exclude

Add glob patterns to the exclude list.

```
Usage: tergum paths exclude [pattern...] [flags]

Flags:
  --json   Output as JSON
```

**Linux / macOS:**
```bash
tergum paths exclude "*.tmp" "*.log" "node_modules/" ".git/"
```

**PowerShell (Windows):**
```powershell
.\tergum.exe paths exclude "*.tmp" "*.log" "node_modules/" ".git/"
```

#### tergum paths unexclude

Remove glob patterns from the exclude list.

```
Usage: tergum paths unexclude [pattern...] [flags]

Flags:
  --json   Output as JSON
```

**Linux / macOS:**
```bash
tergum paths unexclude "*.log"
```

**PowerShell (Windows):**
```powershell
.\tergum.exe paths unexclude "*.log"
```

#### tergum paths list

Show all configured include paths and exclude patterns.

```
Usage: tergum paths list [flags]

Flags:
  --json   Output as JSON
```

**Linux / macOS:**
```bash
tergum paths list
tergum paths list --json
```

**PowerShell (Windows):**
```powershell
.\tergum.exe paths list
.\tergum.exe paths list --json
```

---

### tergum list

List backup sets and browse files within them.

```
Usage: tergum list [flags]

Flags:
      --backup-id string   List files within a specific backup
  -p, --pattern string     Filter files by glob pattern
      --json               Output as JSON
```

**Linux / macOS:**
```bash
# List all backup jobs
tergum list

# List with JSON output
tergum list --json

# List files in a specific backup
tergum list --backup-id 099fc690-f77a-4e55-b606-3fc9c1b0512c

# Search for files by pattern
tergum list --pattern "*.go"
```

**PowerShell (Windows):**
```powershell
# List all backup jobs
.\tergum.exe list

# List with JSON output
.\tergum.exe list --json

# List files in a specific backup
.\tergum.exe list --backup-id 099fc690-f77a-4e55-b606-3fc9c1b0512c

# Search for files by pattern
.\tergum.exe list --pattern "*.go"
```

---

### tergum delete

Delete backup entries, specific files from backups, or clear activity history.

```
Usage: tergum delete [target] [flags]

Flags:
      --backup-id string   Target a specific backup set
      --all-backups        Delete across all backup sets
      --all-activity       Clear all activity history (restore history and job records)
      --dry-run            Preview what would be deleted
      --json               Output as JSON
```

Must specify either `--backup-id`, `--all-backups`, or `--all-activity`.

Output shows: entries deleted, bytes freed, physical storage files removed, and orphan jobs cleaned up.

**Notes:**
- `--all-activity` clears restore history and completed/failed/stopped job records (keeps running jobs)
- Physical storage files are only removed when no other backup entry references the same content hash

**Linux / macOS:**
```bash
# Delete a specific backup set
tergum delete --backup-id 099fc690-f77a-4e55-b606-3fc9c1b0512c

# Delete ALL backups (dangerous)
tergum delete --all-backups

# Preview what would be deleted
tergum delete --all-backups --dry-run

# Delete a specific file from a backup
tergum delete /home/user/Documents/secret.txt --backup-id abc123

# Delete a folder from all backups
tergum delete /home/user/Downloads/ --all-backups

# Preview folder deletion
tergum delete /home/user/Downloads/ --all-backups --dry-run

# Clear activity history (restore records + completed job records)
tergum delete --all-activity

# Full clean slate (delete all backups AND clear activity)
tergum delete --all-backups
tergum delete --all-activity
```

**PowerShell (Windows):**
```powershell
# Delete a specific backup set
.\tergum.exe delete --backup-id 099fc690-f77a-4e55-b606-3fc9c1b0512c

# Delete ALL backups (dangerous)
.\tergum.exe delete --all-backups

# Preview what would be deleted
.\tergum.exe delete --all-backups --dry-run

# Delete a specific file from a backup
.\tergum.exe delete C:\Users\user\Documents\secret.txt --backup-id abc123

# Delete a folder from all backups
.\tergum.exe delete C:\Users\user\Downloads\ --all-backups

# Preview folder deletion
.\tergum.exe delete C:\Users\user\Downloads\ --all-backups --dry-run

# Clear activity history (restore records + completed job records)
.\tergum.exe delete --all-activity

# Full clean slate (delete all backups AND clear activity)
.\tergum.exe delete --all-backups
.\tergum.exe delete --all-activity
```

---

### tergum retention

Manage retention policies that control how long backup versions are kept.

```
Usage: tergum retention <subcommand>

Subcommands:
  list                   List configured retention policies
  add [name]             Add a retention policy
  remove [name]          Remove a retention policy
  run                    Manually trigger retention evaluation

Global flags apply:
  --json                 Output as JSON (works with all subcommands)
```

**Key behavior:**
- Files with NO matching policy are kept **forever**
- `--keep-days 0` (or omitting it) means keep forever
- The **latest version** of any file is NEVER deleted unless `--keep-versions 0` (purge mode)
- Files with only **one version** are never touched unless `--keep-versions 0` (purge mode)
- `--keep-versions 0` = purge mode: all versions deleted after `keep-days`, including the latest
- Higher priority policies are evaluated first (first-match-wins)

#### tergum retention list

List all configured retention policies.

```
Usage: tergum retention list [flags]

Flags:
  --json   Output as JSON
```

**Linux / macOS:**
```bash
tergum retention list
tergum retention list --json
```

**PowerShell (Windows):**
```powershell
.\tergum.exe retention list
.\tergum.exe retention list --json
```

#### tergum retention add

Add a new retention policy.

```
Usage: tergum retention add [name] [flags]

Flags:
  --keep-days int        Days to retain versions (0 = forever)
  --keep-versions int    Minimum versions to keep; 0 = purge all after keep-days (default 1)
  --pattern string       Glob pattern to match file paths (default "*")
  --priority int         Evaluation priority (higher = evaluated first)
  --json                 Output as JSON
```

**Linux / macOS:**
```bash
# Keep logs for only 7 days, minimum 2 versions
tergum retention add cleanup-logs --pattern "*.log" --keep-days 7 --keep-versions 2 --priority 10

# Keep Downloads for 30 days
tergum retention add cleanup-downloads --pattern "/home/user/Downloads/*" --keep-days 30

# Keep tmp files for 3 days
tergum retention add cleanup-tmp --pattern "*.tmp" --keep-days 3 --priority 5

# Purge mode: delete everything in .cache after 7 days (nothing preserved)
tergum retention add purge-cache --pattern "/home/user/.cache/*" --keep-days 7 --keep-versions 0
```

**PowerShell (Windows):**
```powershell
# Keep logs for only 7 days, minimum 2 versions
.\tergum.exe retention add cleanup-logs --pattern "*.log" --keep-days 7 --keep-versions 2 --priority 10

# Keep Downloads for 30 days
.\tergum.exe retention add cleanup-downloads --pattern "C:\Users\user\Downloads\*" --keep-days 30

# Keep tmp files for 3 days
.\tergum.exe retention add cleanup-tmp --pattern "*.tmp" --keep-days 3 --priority 5

# Purge mode: delete everything in .cache after 7 days (nothing preserved)
.\tergum.exe retention add purge-cache --pattern "C:\Users\user\.cache\*" --keep-days 7 --keep-versions 0
```

#### tergum retention remove

Remove a retention policy by name. Files previously matched revert to "keep forever".

```
Usage: tergum retention remove [name] [flags]

Flags:
  --json   Output as JSON
```

**Linux / macOS:**
```bash
tergum retention remove cleanup-logs
```

**PowerShell (Windows):**
```powershell
.\tergum.exe retention remove cleanup-logs
```

#### tergum retention run

Manually trigger retention evaluation to clean up old backup versions.

```
Usage: tergum retention run [flags]

Flags:
  --dry-run   Preview without deleting
  --json      Output as JSON
```

**Linux / macOS:**
```bash
# Preview what would be cleaned up
tergum retention run --dry-run

# Actually run retention cleanup
tergum retention run
```

**PowerShell (Windows):**
```powershell
# Preview what would be cleaned up
.\tergum.exe retention run --dry-run

# Actually run retention cleanup
.\tergum.exe retention run
```

---

### tergum restore

Restore files from backup (local or remote).

```
Usage: tergum restore [query] [flags]

Flags:
  -d, --dest string        Destination directory (default: original paths)
  -c, --concurrency int    Parallel restore streams (default 4)
      --backup-id string   Restore from a specific backup set
      --list               Search and list matching files without restoring
      --json               Output as JSON
      --client string      Client ID to restore from (server-side only)
  -p, --path string        Specific file path, name, or pattern to restore (alternative to query argument)
      --target string      Target client ID to push restored files to (requires --client)
```

**Search behavior:**
- Path containing `/` → path prefix match
- Pattern with `*` or `?` → glob match on file name
- Plain text → exact file name match

**Restore behavior:**
- Files are decrypted and BLAKE3-verified before writing
- Original permissions and timestamps are restored
- Symlinks are recreated
- Parallel downloads with configurable concurrency

**Linux / macOS:**
```bash
# Search for files without restoring
tergum restore "*.go" --list

# Restore all .go files to a directory
TERGUM_PASSPHRASE=mypass tergum restore "*.go" --dest /tmp/restored

# Restore a specific file by name
TERGUM_PASSPHRASE=mypass tergum restore "report.pdf" --dest /tmp/restored

# Restore an entire folder
TERGUM_PASSPHRASE=mypass tergum restore "/home/user/Documents/" --dest /tmp/restored

# Restore from a specific backup set
TERGUM_PASSPHRASE=mypass tergum restore --backup-id abc123 --dest /tmp/restored

# Restore to original paths (in-place)
TERGUM_PASSPHRASE=mypass tergum restore "important.docx"
```

**PowerShell (Windows):**
```powershell
# Search for files without restoring
.\tergum.exe restore "*.go" --list

# Restore all .go files to a directory
$env:TERGUM_PASSPHRASE="mypass"; .\tergum.exe restore "*.go" --dest C:\temp\restored

# Restore a specific file by name
$env:TERGUM_PASSPHRASE="mypass"; .\tergum.exe restore "report.pdf" --dest C:\temp\restored

# Restore an entire folder
$env:TERGUM_PASSPHRASE="mypass"; .\tergum.exe restore "C:\Users\user\Documents\" --dest C:\temp\restored

# Restore from a specific backup set
$env:TERGUM_PASSPHRASE="mypass"; .\tergum.exe restore --backup-id abc123 --dest C:\temp\restored

# Restore to original paths (in-place)
$env:TERGUM_PASSPHRASE="mypass"; .\tergum.exe restore "important.docx"
```

**Restoring Remote Client Data:**
Because Tergum uses secure client-side encryption, the master decryption keys are derived from the client's own `TERGUM_PASSPHRASE` and are never stored on the server. There are three ways to restore remote client data:

#### Method 1: Client-Side Restore (Recommended)
Log in to the remote client machine and run the restore command. The client daemon connects to the server to download backup blobs and decrypts them locally. **Files will be restored directly onto the remote client's filesystem.**

**Example (Linux / macOS Client):**
```bash
# Executed on the client machine:
TERGUM_PASSPHRASE=clientpassphrase tergum restore "*.docx" --dest /path/to/restore
```

**Example (PowerShell Windows Client):**
```powershell
# Executed on the client machine:
$env:TERGUM_PASSPHRASE="clientpassphrase"; .\tergum.exe restore "*.docx" --dest C:\path\to\restore
```

#### Method 2: Server-Side Restore (to server filesystem)
Run the restore command on the server using `--client`. You must provide the remote client's `TERGUM_PASSPHRASE`. **Files will be restored onto the server's local filesystem** (useful if the client is offline or destroyed).

**Example (Linux / macOS Server):**
```bash
# Executed on the server machine — files land on the server:
TERGUM_PASSPHRASE=clientpassphrase tergum restore --client my-client --path "*.docx" --dest /path/to/restore
```

**Example (PowerShell Windows Server):**
```powershell
# Executed on the server machine — files land on the server:
$env:TERGUM_PASSPHRASE="clientpassphrase"; .\tergum.exe restore --client my-client --path "*.docx" --dest C:\path\to\restore
```

#### Method 3: Cross-Client Restore (push to target client)
Use `--target` to push restored files directly to another online client over gRPC. The server decrypts the source client's backup and streams the files to the target client, which writes them to disk. **The target client must be online and registered.**

**Example (Linux / macOS Server):**
```bash
# Restore from client "fedora" and push files to client "ubuntu":
TERGUM_PASSPHRASE=fedora_passphrase tergum restore --client fedora --target ubuntu --path "/home/user/Documents/" --dest /tmp/restored

# Restore back to the same client (disaster recovery — client re-imaged):
TERGUM_PASSPHRASE=fedora_passphrase tergum restore --client fedora --target fedora --path "report.pdf" --dest /tmp/restored
```

**Example (PowerShell Windows Server):**
```powershell
# Restore from client "fedora" and push files to client "ubuntu":
$env:TERGUM_PASSPHRASE="fedora_passphrase"; .\tergum.exe restore --client fedora --target ubuntu --path "/home/user/Documents/" --dest /tmp/restored

# Restore back to the same client:
$env:TERGUM_PASSPHRASE="fedora_passphrase"; .\tergum.exe restore --client fedora --target fedora --path "report.pdf" --dest /tmp/restored
```

**Requirements for `--target`:**
- The source client must have `--client` specified
- The target client must be online (registered in the server's client registry)
- The server must have TLS configured to connect to the target client
- The `TERGUM_PASSPHRASE` is the source client's passphrase (for decryption)

---

### tergum stop

Stop an in-progress backup. The backup will complete its current file, update the job status to "stopped", and exit cleanly.

```
Usage: tergum stop [flags]

Flags:
  --json   Output as JSON
```

**Linux / macOS:**
```bash
# In one terminal: start a backup
TERGUM_PASSPHRASE=mypass tergum backup --level full

# In another terminal: stop it
tergum stop
```

**PowerShell (Windows):**
```powershell
# In one terminal: start a backup
$env:TERGUM_PASSPHRASE="mypass"; .\tergum.exe backup --level full

# In another terminal: stop it
.\tergum.exe stop
```

---

### tergum status

Show a full system overview: configuration, paths, backup history, storage usage, and last backup details.

```
Usage: tergum status [flags]

Flags:
  --json   Output as JSON
```

**Linux / macOS:**
```bash
tergum status
tergum status --json
```

**PowerShell (Windows):**
```powershell
.\tergum.exe status
.\tergum.exe status --json
```

---

### tergum watch

Manage the file watcher for continuous backup.

```
Usage: tergum watch [command] [flags]

Available Commands:
  run       Start file watcher in the foreground (default)
  enable    Enable the file watcher in configuration
  disable   Disable the file watcher in configuration

Flags:
  --json   Output as JSON
```

**Pipeline:** filesystem event → exclude filter → debounce (500ms) → stability gate (60s) → BLAKE3 hash → encrypt → upload

#### tergum watch run

Start the file watcher in the foreground. Monitors all configured include paths for changes and automatically backs up files that pass the stability gate.

```
Usage: tergum watch run [flags]

Flags:
  --json   Output as JSON
```

**How it works:**
- Recursively watches all configured include paths
- Ignores files matching exclude patterns
- Debounces rapid events (e.g., file being actively written)
- Waits for stability (file unchanged for 60s) before backing up
- Batches stable files into backup jobs (default: every 5 minutes)
- Prints status every 30 seconds when events are occurring
- Graceful shutdown on Ctrl+C or SIGTERM

**Linux / macOS:**
```bash
# Start watcher (runs in foreground, Ctrl+C to stop)
TERGUM_PASSPHRASE=mypass tergum watch
# Or explicitly:
TERGUM_PASSPHRASE=mypass tergum watch run
```

**PowerShell (Windows):**
```powershell
# Start watcher (runs in foreground, Ctrl+C to stop)
$env:TERGUM_PASSPHRASE="mypass"; .\tergum.exe watch
# Or explicitly:
$env:TERGUM_PASSPHRASE="mypass"; .\tergum.exe watch run
```

#### tergum watch enable

Enable the file watcher in the configuration file.

```
Usage: tergum watch enable [flags]

Flags:
  --json   Output as JSON
```

**Linux / macOS:**
```bash
tergum watch enable
```

**PowerShell (Windows):**
```powershell
.\tergum.exe watch enable
```

#### tergum watch disable

Disable the file watcher in the configuration file.

```
Usage: tergum watch disable [flags]

Flags:
  --json   Output as JSON
```

**Linux / macOS:**
```bash
tergum watch disable
```

**PowerShell (Windows):**
```powershell
.\tergum.exe watch disable
```

#### tergum watch start

Start the file watcher and ongoing backup loop on a remote client from the server.

```
Usage: tergum watch start [client_id] [flags]

Flags:
  --json   Output as JSON
```

**Linux / macOS:**
```bash
tergum watch start 192.168.1.214
```

**PowerShell (Windows):**
```powershell
.\tergum.exe watch start 192.168.1.214
```

#### tergum watch stop

Stop the file watcher and ongoing backup loop on a remote client from the server.

```
Usage: tergum watch stop [client_id] [flags]

Flags:
  --json   Output as JSON
```

**Linux / macOS:**
```bash
tergum watch stop 192.168.1.214
```

**PowerShell (Windows):**
```powershell
.\tergum.exe watch stop 192.168.1.214
```

---

### tergum version

Print version information.

```
Usage: tergum version [flags]

Flags:
  --json   Output as JSON
```

**Linux / macOS:**
```bash
tergum version
tergum version --json
```

**PowerShell (Windows):**
```powershell
.\tergum.exe version
.\tergum.exe version --json
```

---

### tergum migrate

> 🚧 **Planned** — Not yet implemented.

Migrate from v2.0 to v3.0.

```
Usage: tergum migrate [flags]

Flags:
  --from-db string   Path to v2.0 SQLite database (required)
  --rehash           Compute BLAKE3 hashes replacing MD5
  --encrypt          Encrypt existing storage files
  --verify           Verify migration integrity
```
