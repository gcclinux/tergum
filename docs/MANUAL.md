# Tergum User Manual

## First-Time Setup

**Linux / macOS:**
```bash
tergum setup
```

**PowerShell (Windows):**
```powershell
.\tergum.exe setup
```

The interactive wizard walks you through:

1. **Node role** — `client`, `server`, or `both` (use `both` for single-machine backups)
2. **Server address** — where the server is (default: `localhost`)
3. **Storage path** — where backup data is stored on disk
4. **TLS certificates** — generates Ed25519 mTLS certs automatically
5. **Encryption passphrase** — used to derive a master key (Argon2id)
6. **Include paths** — directories to back up
7. **Exclude patterns** — glob patterns to skip (defaults: `*.tmp`, `.git/`, `node_modules/`, `__pycache__/`, `.cache/`)
8. **File watcher** — enable/disable real-time file monitoring
9. **Backup schedule** — optional cron expressions for automatic backups

After setup, your config lives at `~/.config/tergum/tergum.toml` (Linux).

To regenerate just the TLS certificates:

**Linux / macOS:**
```bash
tergum setup --generate-certs
```

**PowerShell (Windows):**
```powershell
.\tergum.exe setup --generate-certs
```

---

## Running a Backup

### Start the server first

The server must be running for backups to work (even in `both` mode for scheduled/watcher backups):

**Linux / macOS:**
```bash
tergum server
```

**PowerShell (Windows):**
```powershell
.\tergum.exe server
```

### Manual backup

**Linux / macOS:**
```bash
# Auto level — only new/modified files since last backup
tergum backup

# Full backup — all files in include paths
tergum backup --level full

# Set passphrase via env var (avoids prompt)
TERGUM_PASSPHRASE=mypassphrase tergum backup

# JSON output for scripting
TERGUM_PASSPHRASE=mypassphrase tergum backup --json
```

**PowerShell (Windows):**
```powershell
# Auto level — only new/modified files since last backup
.\tergum.exe backup

# Full backup — all files in include paths
.\tergum.exe backup --level full

# Set passphrase via env var (avoids prompt)
$env:TERGUM_PASSPHRASE="mypassphrase"; .\tergum.exe backup

# JSON output for scripting
$env:TERGUM_PASSPHRASE="mypassphrase"; .\tergum.exe backup --json
```

### Environment variables

| Variable | Description |
|----------|-------------|
| `TERGUM_PASSPHRASE` | Encryption passphrase (avoids interactive prompt) |

#### Setting Environment Variables on Windows

Since Windows shells do not support the Unix `VAR=value command` inline syntax, use the following methods instead:

**PowerShell:**
```powershell
# Set for the current session, then run
$env:TERGUM_PASSPHRASE="mypassphrase"
tergum server

# Or as a one-liner:
$env:TERGUM_PASSPHRASE="mypassphrase"; tergum server
```

**Command Prompt (cmd.exe):**
```cmd
set TERGUM_PASSPHRASE=mypassphrase
tergum server
```

---

## Managing Backup Paths

### Add directories to back up

**Linux / macOS:**
```bash
tergum paths add /home/user/Documents /home/user/Projects
```

**PowerShell (Windows):**
```powershell
.\tergum.exe paths add C:\Users\user\Documents C:\Users\user\Projects
```

### Remove a directory

**Linux / macOS:**
```bash
tergum paths remove /home/user/Downloads
```

**PowerShell (Windows):**
```powershell
.\tergum.exe paths remove C:\Users\user\Downloads
```

### Scan a directory (add all top-level folders)

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

### Add exclude patterns

**Linux / macOS:**
```bash
tergum paths exclude "*.tmp" "*.log" "node_modules/" ".git/"
```

**PowerShell (Windows):**
```powershell
.\tergum.exe paths exclude "*.tmp" "*.log" "node_modules/" ".git/"
```

### Remove an exclude pattern

**Linux / macOS:**
```bash
tergum paths unexclude "*.log"
```

**PowerShell (Windows):**
```powershell
.\tergum.exe paths unexclude "*.log"
```

### View current configuration

**Linux / macOS:**
```bash
tergum paths list
```

**PowerShell (Windows):**
```powershell
.\tergum.exe paths list
```

---

## All Commands

| Command | Status | Description |
|---------|--------|-------------|
| `tergum setup` | ✅ Working | Interactive configuration wizard |
| `tergum server` | ✅ Working | Start server or client daemon (role-aware) |
| `tergum admin` | ✅ Working | Start Web UI only (lightweight) |
| `tergum node` | ✅ Working | Manage node role and hostname settings |
| `tergum backup` | ✅ Working | Run a manual backup (local or remote) |
| `tergum paths` | ✅ Working | Manage include/exclude paths |
| `tergum list` | ✅ Working | List backup sets and files |
| `tergum delete` | ✅ Working | Delete backup entries |
| `tergum retention` | ✅ Working | Manage retention policies |
| `tergum restore` | ✅ Working | Restore files from backup (local or remote) |
| `tergum stop` | ✅ Working | Stop an in-progress backup |
| `tergum status` | ✅ Working | Show system status |
| `tergum watch` | ✅ Working | File watcher for ongoing backup (local or remote) |
| `tergum version` | ✅ Working | Print version info |
| `tergum migrate` | 🚧 Planned | Migrate from v2.0 to v3.0 |

---

## Command Reference

### tergum setup

```
Usage: tergum setup [flags]

Flags:
  --generate-certs   Generate TLS certificates without interactive prompts
```

### tergum server

```
Usage: tergum server [flags]

Starts in role-aware mode based on [node].role in config:
  - role "server": gRPC services (7400, 7401), web UI (7480), metrics (7490),
    retention engine, scheduler, client registry
  - role "client": client CommandService (7400), heartbeat to server,
    file watcher (if enabled), accepts remote triggers
  - role "hybrid": full local server (same as "server" without client registry)

Graceful shutdown on SIGTERM/SIGINT.
```

### tergum admin

```
Usage: tergum admin [flags]

Flags:
  -p, --port int   Override web UI port (default: from config, typically 7480)
```

Starts just the Web UI without gRPC services, scheduler, or watcher. Use this for
lightweight browser-based management.

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

The admin panel provides the same Web UI as the full server:
- Dashboard with storage stats and backup history
- Backup job management (view, delete)
- Include/exclude path management
- Node settings — change role (server/hybrid) and hostname (network interface)
- Retention policy management
- Restore file browser (search backed-up files)
- Watcher path management

### tergum node

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

Change the node role between **server** (serves remote clients only, no local backups or file watcher) and **hybrid** (full server + local backup, file watcher, and scheduling). A server restart is required for role changes to take effect.

The hostname setting identifies which network interface address to advertise to remote clients, useful when the node has multiple interfaces.

**Linux / macOS:**
```bash
# Show current settings
tergum node show

# Switch to hybrid mode (enables local backups)
tergum node role set hybrid

# Switch back to server-only
tergum node role set server

# Set hostname to a specific interface
tergum node hostname set 192.168.1.10

# Clear hostname (use system default)
tergum node hostname clear
```

**PowerShell (Windows):**
```powershell
# Show current settings
.\tergum.exe node show

# Switch to hybrid mode (enables local backups)
.\tergum.exe node role set hybrid

# Switch back to server-only
.\tergum.exe node role set server

# Set hostname to a specific interface
.\tergum.exe node hostname set 192.168.1.10

# Clear hostname (use system default)
.\tergum.exe node hostname clear
```

These settings can also be changed from the Web UI under **Config → Node Settings**.

### tergum backup

```
Usage: tergum backup [flags]

Flags:
  -l, --level string   Backup level: auto, full (default "auto")
      --json           Output result as JSON
```

**Auto** mode backs up files that are new or modified since the last backup.
**Full** mode backs up everything in the include paths regardless of changes.

**Linux / macOS:**
```bash
# JSON output for scripting
TERGUM_PASSPHRASE=mypassphrase tergum backup --json
```

**PowerShell (Windows):**
```powershell
# JSON output for scripting
$env:TERGUM_PASSPHRASE="mypassphrase"; .\tergum.exe backup --json
```

### tergum paths

```
Usage: tergum paths <subcommand>

Subcommands:
  scan [directory]       Scan a directory and add top-level folders
  add [path...]          Add one or more include paths
  remove [path...]       Remove one or more include paths
  exclude [pattern...]   Add exclude patterns (glob syntax)
  unexclude [pattern...] Remove exclude patterns
  list                   Show all include paths and exclude patterns

Flags for scan:
  --include-hidden   Include hidden directories (starting with '.')

Global flags apply:
  --json             Output as JSON (works with all subcommands)
```

**Linux / macOS:**
```bash
# JSON output for paths list
tergum paths list --json
```

**PowerShell (Windows):**
```powershell
# JSON output for paths list
.\tergum.exe paths list --json
```

### tergum restore

```
Usage: tergum restore [query] [flags]

Flags:
  -d, --dest string        Destination directory (default: original paths)
  -c, --concurrency int    Parallel restore streams (default 4)
      --backup-id string   Restore from a specific backup set
      --list               Search and list matching files without restoring
      --json               Output as JSON
```

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

Search behavior:
- Path containing `/` → path prefix match
- Pattern with `*` or `?` → glob match on file name
- Plain text → exact file name match

Restore behavior:
- Files are decrypted and BLAKE3-verified before writing
- Original permissions and timestamps are restored
- Symlinks are recreated
- Parallel downloads with configurable concurrency

### tergum list

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

### tergum delete

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

Output shows: entries deleted, bytes freed, physical storage files removed, and orphan jobs cleaned up.

Notes:
- `--all-activity` clears restore history and completed/failed/stopped job records (keeps running jobs)
- Physical storage files are only removed when no other backup entry references the same content hash

### tergum retention

```
Usage: tergum retention <subcommand>

Subcommands:
  list                   List configured retention policies
  add [name]             Add a retention policy
  remove [name]          Remove a retention policy
  run                    Manually trigger retention evaluation

Flags for add:
  --keep-days int        Days to retain versions (0 = forever)
  --keep-versions int    Minimum versions to keep; 0 = purge all after keep-days (default 1)
  --pattern string       Glob pattern to match file paths (default "*")
  --priority int         Evaluation priority (higher = evaluated first)

Flags for run:
  --dry-run              Preview without deleting

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

**Linux / macOS:**
```bash
# Keep logs for only 7 days, minimum 2 versions
tergum retention add cleanup-logs --pattern "*.log" --keep-days 7 --keep-versions 2 --priority 10

# Keep Downloads for 30 days
tergum retention add cleanup-downloads --pattern "/home/user/Downloads/*" --keep-days 30

# Keep tmp files for 3 days
tergum retention add cleanup-tmp --pattern "*.tmp" --keep-days 3 --priority 5

# Everything else (Documents, Projects, etc.) is kept FOREVER automatically

# Preview what would be cleaned up
tergum retention run --dry-run

# Actually run retention cleanup
tergum retention run

# List current policies
tergum retention list

# Remove a policy (files revert to "keep forever")
tergum retention remove cleanup-logs
```

**PowerShell (Windows):**
```powershell
# Keep logs for only 7 days, minimum 2 versions
.\tergum.exe retention add cleanup-logs --pattern "*.log" --keep-days 7 --keep-versions 2 --priority 10

# Keep Downloads for 30 days
.\tergum.exe retention add cleanup-downloads --pattern "C:\Users\user\Downloads\*" --keep-days 30

# Keep tmp files for 3 days
.\tergum.exe retention add cleanup-tmp --pattern "*.tmp" --keep-days 3 --priority 5

# Everything else (Documents, Projects, etc.) is kept FOREVER automatically

# Preview what would be cleaned up
.\tergum.exe retention run --dry-run

# Actually run retention cleanup
.\tergum.exe retention run

# List current policies
.\tergum.exe retention list

# Remove a policy (files revert to "keep forever")
.\tergum.exe retention remove cleanup-logs
```

#### Purge Mode (keep-versions 0)

Setting `--keep-versions 0` enables purge mode: ALL versions of matching files are
deleted after `keep-days` expires, including the latest version and single-version files.
Physical storage is freed when no other backup entry references the same content hash.

This is useful when you want a folder's contents to be completely removed from backups
after a certain age — no trace left in the database or on disk.

**Linux / macOS:**
```bash
# Delete everything in .cache after 7 days (nothing preserved)
tergum retention add purge-cache --pattern "/home/user/.cache/*" --keep-days 7 --keep-versions 0

# Purge Downloads after 14 days
tergum retention add purge-downloads --pattern "/home/user/Downloads/*" --keep-days 14 --keep-versions 0

# Purge temp build artifacts after 3 days
tergum retention add purge-builds --pattern "/tmp/builds/*" --keep-days 3 --keep-versions 0 --priority 20

# Always preview first!
tergum retention run --dry-run
```

**PowerShell (Windows):**
```powershell
# Delete everything in .cache after 7 days (nothing preserved)
.\tergum.exe retention add purge-cache --pattern "C:\Users\user\.cache\*" --keep-days 7 --keep-versions 0

# Purge Downloads after 14 days
.\tergum.exe retention add purge-downloads --pattern "C:\Users\user\Downloads\*" --keep-days 14 --keep-versions 0

# Purge temp build artifacts after 3 days
.\tergum.exe retention add purge-builds --pattern "C:\temp\builds\*" --keep-days 3 --keep-versions 0 --priority 20

# Always preview first!
.\tergum.exe retention run --dry-run
```

**Safety notes for purge mode:**
- There is no "latest version protection" — if the file is older than `keep-days`, it's gone
- Single-version files ARE deleted (unlike standard mode where they're always kept)
- The `keep-days` time condition still applies: files newer than `keep-days` are safe
- Physical storage files are only removed when their reference count drops to zero
- Use `--dry-run` before running retention to see what would be purged

### tergum stop

```
Usage: tergum stop [--json]
```

Sends a stop signal to a running backup. The backup will complete its current file, update the job status to "stopped", and exit cleanly.

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

### tergum status

```
Usage: tergum status [--json]
```

Shows a full system overview: configuration, paths, backup history, storage usage, and last backup details.

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

### tergum watch

```
Usage: tergum watch [command] [--json]

Available Commands:
  run       Start file watcher in the foreground (default)
  enable    Enable the file watcher in configuration
  disable   Disable the file watcher in configuration
```

Starts the file watcher for continuous backup or manages its configuration. When running, it monitors all configured include paths for changes and automatically backs up files that pass the stability gate.

**Linux / macOS:**
```bash
# Start watcher (runs in foreground, Ctrl+C to stop)
TERGUM_PASSPHRASE=mypass tergum watch
# Or explicitly:
TERGUM_PASSPHRASE=mypass tergum watch run

# Enable the watcher in the configuration file
tergum watch enable

# Disable the watcher in the configuration file
tergum watch disable
```

**PowerShell (Windows):**
```powershell
# Start watcher (runs in foreground, Ctrl+C to stop)
$env:TERGUM_PASSPHRASE="mypass"; .\tergum.exe watch
# Or explicitly:
$env:TERGUM_PASSPHRASE="mypass"; .\tergum.exe watch run

# Enable the watcher in the configuration file
.\tergum.exe watch enable

# Disable the watcher in the configuration file
.\tergum.exe watch disable
```

**Pipeline:** filesystem event → exclude filter → debounce (500ms) → stability gate (60s) → BLAKE3 hash → encrypt → upload

**How it works:**
- Recursively watches all configured include paths
- Ignores files matching exclude patterns
- Debounces rapid events (e.g., file being actively written)
- Waits for stability (file unchanged for 60s) before backing up
- Batches stable files into backup jobs (default: every 5 minutes)
- Prints status every 30 seconds when events are occurring
- Graceful shutdown on Ctrl+C or SIGTERM

**Configuration** (in `tergum.toml`):

```toml
[watcher]
enabled = true
debounce_ms = 500               # collapse rapid events within window
stability_seconds = 60          # file must be unchanged for this long
ongoing_backup = true           # auto-backup stable files
batch_interval_minutes = 5      # group uploads into jobs
```

### tergum migrate

```
Usage: tergum migrate [flags]

Flags:
  --from-db string   Path to v2.0 SQLite database (required)
  --rehash           Compute BLAKE3 hashes replacing MD5
  --encrypt          Encrypt existing storage files
  --verify           Verify migration integrity
```

### tergum version

```
Usage: tergum version [--json]
```

---

## Global Flags

Available on every command:

```
--config string   Config file path (default: ~/.config/tergum/tergum.toml)
--json            Output machine-readable JSON
--dry-run         Preview destructive operations without executing
```

The `--json` flag works on all commands and is useful for scripting:

**Linux / macOS:**
```bash
tergum list --json
tergum backup --json
tergum paths list --json
tergum version --json
tergum list --backup-id <id> --json
```

**PowerShell (Windows):**
```powershell
.\tergum.exe list --json
.\tergum.exe backup --json
.\tergum.exe paths list --json
.\tergum.exe version --json
.\tergum.exe list --backup-id <id> --json
```

---

## Web UI

When the server or admin panel is running, the management dashboard is at:

```
http://localhost:7480
```

Default login: `admin` / `admin` (change in `tergum.toml`).

Start the Web UI with either:
**Linux / macOS:**
```bash
tergum server    # full server with Web UI included
tergum admin     # Web UI only (lightweight, no gRPC/scheduler)
```

**PowerShell (Windows):**
```powershell
.\tergum.exe server    # full server with Web UI included
.\tergum.exe admin     # Web UI only (lightweight, no gRPC/scheduler)
```

Pages and capabilities:
- **Dashboard** — storage stats, total files, total size
- **Backups** — job history with status, file count, and delete functionality
- **Restore** — search backed-up files by name/path (restore via CLI)
- **Config** — node settings (role, hostname), include/exclude paths (syncs to TOML)
- **Retention** — add/remove retention policies with pattern matching
- **Watchers** — add/remove watch paths for file monitoring
- **Activity** — real-time event log (SSE)
- **Clients** — connected client nodes with trigger backup, start/stop watcher, schedule config (server role only)
- **Metrics** — backup and storage metrics

---

## Typical Workflow

**Linux / macOS:**
```bash
# 1. Initial setup (one-time)
tergum setup

# 2. Start server (keep running, or use systemd)
tergum server &

# 3. Run first full backup
TERGUM_PASSPHRASE=mypassphrase tergum backup --level full

# 4. Subsequent incremental backups
TERGUM_PASSPHRASE=mypassphrase tergum backup

# 5. Add more paths later
tergum paths add /home/user/NewProject

# 6. Check what's configured
tergum paths list
```

**PowerShell (Windows):**
```powershell
# 1. Initial setup (one-time)
.\tergum.exe setup

# 2. Start server (keep running in background)
Start-Process -FilePath ".\tergum.exe" -ArgumentList "server" -NoNewWindow

# 3. Run first full backup
$env:TERGUM_PASSPHRASE="mypassphrase"; .\tergum.exe backup --level full

# 4. Subsequent incremental backups
$env:TERGUM_PASSPHRASE="mypassphrase"; .\tergum.exe backup

# 5. Add more paths later
.\tergum.exe paths add C:\Users\user\NewProject

# 6. Check what's configured
.\tergum.exe paths list
```

---

## Remote Backup (Client/Server)

Tergum supports multi-machine backup where client nodes stream data to a central server. The server stores all backup data, manages schedules, and provides a single dashboard for the entire infrastructure.

### Architecture Overview

```
┌──────────────────────┐         mTLS          ┌──────────────────────┐
│   CLIENT NODE        │ ─────────────────────► │   SERVER NODE        │
│                      │  Upload / Manifest     │                      │
│  • Backup Engine     │ ◄───────────────────── │  • DataService :7401 │
│  • File Watcher      │  Download / Diff       │  • CommandService    │
│  • CommandService    │                        │    :7400             │
│    (accepts triggers)│ ◄───────────────────── │  • Client Registry   │
│  • Heartbeat (30s)   │  TriggerBackup /       │  • Per-Client Sched. │
│                      │  Start-Stop Watcher    │  • Web UI :7480      │
└──────────────────────┘                        └──────────────────────┘
```

Both directions are authenticated with mutual TLS. The client identifies itself using the Common Name (CN) from its certificate.

### Setup Flow

#### 1. Generate certificates on the server

Run the setup wizard on the server machine. This generates a CA and server certificate:

**Linux / macOS:**
```bash
# On the server
tergum setup
# Choose role: server
# This generates: ca.crt, server.crt, server.key in ~/.config/tergum/certs/
```

**PowerShell (Windows):**
```powershell
# On the server
.\tergum.exe setup
# Choose role: server
# This generates: ca.crt, server.crt, server.key in C:\Users\user\AppData\Roaming\tergum\certs\
```

Or regenerate certificates only:

**Linux / macOS:**
```bash
tergum setup --generate-certs
```

**PowerShell (Windows):**
```powershell
.\tergum.exe setup --generate-certs
```

#### 2. Distribute certificates to clients

Copy the CA certificate and generate client certificates for each client machine. The CA (`ca.crt`) must be the same on all nodes:

```bash
# Copy to each client machine:
#   ca.crt          → shared CA (same across all nodes)
#   client.crt      → unique per client (CN = client hostname)
#   client.key      → unique per client
```

Alternatively, run `tergum setup` on each client — it will prompt for the server address and generate client certs signed by the same CA (requires the CA key to be present during generation).

#### 3. Configure the client

On each client machine, the config should have:

```toml
[node]
role = "client"

[server]
address = "192.168.1.5"    # server's IP or hostname
command_port = 7400
data_port = 7401

[tls]
ca_cert = "~/.config/tergum/certs/ca.crt"
cert = "~/.config/tergum/certs/client.crt"
key = "~/.config/tergum/certs/client.key"

[client]
include_paths = ["/home/user/Documents", "/home/user/Projects"]
exclude_patterns = ["*.tmp", ".git/", "node_modules/"]
```

#### 4. Configure the server

```toml
[node]
role = "server"

[server]
command_port = 7400
data_port = 7401

[tls]
ca_cert = "~/.config/tergum/certs/ca.crt"
cert = "~/.config/tergum/certs/server.crt"
key = "~/.config/tergum/certs/server.key"

[backup]
storage_path = "/var/lib/tergum/storage"

[webui]
enabled = true
port = 7480
```

#### 5. Start both daemons

**Linux / macOS:**
```bash
# On the server
tergum server
# Starts: gRPC services, Web UI, scheduler, registry

# On each client
tergum server
# Detects role = "client", starts: client CommandService, heartbeat, file watcher (if enabled)
```

**PowerShell (Windows):**
```powershell
# On the server
.\tergum.exe server
# Starts: gRPC services, Web UI, scheduler, registry

# On each client
.\tergum.exe server
# Detects role = "client", starts: client CommandService, heartbeat, file watcher (if enabled)
```

The `tergum server` command detects the node role from the config and starts the appropriate subsystems.

### Client Operations

Once the client daemon is running, backups work the same as local mode:

**Linux / macOS:**
```bash
# Manual backup (streams to remote server)
TERGUM_PASSPHRASE=mypass tergum backup

# Full backup
TERGUM_PASSPHRASE=mypass tergum backup --level full

# Restore from server
TERGUM_PASSPHRASE=mypass tergum restore "*.go" --dest /tmp/restored

# File watcher (continuous backup to server)
TERGUM_PASSPHRASE=mypass tergum watch
```

**PowerShell (Windows):**
```powershell
# Manual backup (streams to remote server)
$env:TERGUM_PASSPHRASE="mypass"; .\tergum.exe backup

# Full backup
$env:TERGUM_PASSPHRASE="mypass"; .\tergum.exe backup --level full

# Restore from server
$env:TERGUM_PASSPHRASE="mypass"; .\tergum.exe restore "*.go" --dest C:\temp\restored

# File watcher (continuous backup to server)
$env:TERGUM_PASSPHRASE="mypass"; .\tergum.exe watch
```

Key differences in remote mode:
- Files are encrypted locally before upload (passphrase never leaves the client)
- A manifest exchange determines which files the server already has (deduplication)
- After backup, the client syncs its database to the server
- If the server is unreachable, the client retries with exponential backoff (1s → 30s, 5 attempts max)

### Server-Initiated Operations

The server can trigger operations on any connected client:

**Trigger a backup remotely:**
The server sends a `TriggerBackup` RPC to the client's CommandService. This can be done from the Web UI or by the scheduler.

**Start/stop file watcher remotely:**
The server can start or stop the file watcher on any client without needing to SSH into the machine.

**Scheduled backups:**
The server manages per-client cron schedules. When a schedule fires:
- If the client is online → backup is triggered immediately
- If the client is offline → the missed backup is queued and triggered within 60 seconds of reconnection

### Web UI Client Management

Access the client management dashboard at `http://server:7480` → **Clients** page.

The clients page shows:
- **Hostname** — client identity (from certificate CN)
- **Status** — online/offline with colored indicator
- **Last Seen** — last heartbeat timestamp
- **Last Backup** — most recent backup completion time
- **Watcher** — whether the file watcher is active

**Actions per client:**
| Action | Description |
|--------|-------------|
| Trigger Backup | Sends a backup command to the client |
| Start Watcher | Starts the file watcher on the client |
| Stop Watcher | Stops the file watcher on the client |
| Configure Schedule | Set full/auto backup cron expressions |

**Client detail view** shows:
- Backup history for that specific client
- Current watcher status and monitored paths
- Schedule configuration with inline editing
- Live backup progress (polled every 5 seconds when a backup is running)

### Connection & Heartbeat

- Clients send a `Ping` RPC to the server every **30 seconds**
- If 3 consecutive pings are missed (90 seconds), the server marks the client as **offline**
- When the client reconnects, it's automatically marked **online** and any missed scheduled backups are triggered

### Security

- All communication uses mutual TLS (mTLS) — both sides verify certificates
- Only certificates signed by the shared CA are accepted
- The client's certificate CN is used as its identity — no passwords for node-to-node auth
- Encryption passphrases and master keys remain on the client — data is encrypted before transmission
- The client's CommandService also requires mTLS, so only the legitimate server can issue commands

### Example: Two-Machine Setup

**Linux / macOS:**
```bash
# === SERVER (192.168.1.5) ===
tergum setup
# role: server, storage: /var/lib/tergum/storage
tergum server

# === CLIENT (laptop) ===
tergum setup
# role: client, server address: 192.168.1.5
# Copy ca.crt from server, generate client cert
export TERGUM_PASSPHRASE=mypassphrase
tergum server     # starts client daemon (heartbeat + watcher)

# Or run a one-off backup without the daemon:
tergum backup --level full
```

**PowerShell (Windows):**
```powershell
# === SERVER (192.168.1.5) ===
.\tergum.exe setup
# role: server, storage: D:\tergum\storage
.\tergum.exe server

# === CLIENT (laptop) ===
.\tergum.exe setup
# role: client, server address: 192.168.1.5
# Copy ca.crt from server, generate client cert
$env:TERGUM_PASSPHRASE="mypassphrase"
.\tergum.exe server     # starts client daemon (heartbeat + watcher)

# Or run a one-off backup without the daemon:
$env:TERGUM_PASSPHRASE="mypassphrase"; .\tergum.exe backup --level full
```

### Troubleshooting

| Symptom | Likely Cause |
|---------|-------------|
| "server.address is required" | Config has `role = "client"` but no `[server] address` |
| "certificate signed by unknown authority" | CA cert mismatch between client and server |
| Client shows "offline" in Web UI | Firewall blocking port 7400 from server → client |
| "server nodes do not run local backups" | Ran `tergum backup` on a server-role node (use Web UI to trigger clients instead) |
| Backup fails with connection error | Server unreachable — check network, ports 7400/7401 |

---

## Configuration File

Location: `~/.config/tergum/tergum.toml`

```toml
[node]
role = "hybrid"

[server]
address = "localhost"
command_port = 7400
data_port = 7401

[client]
include_paths = ["/home/user/Documents", "/home/user/Projects"]
exclude_patterns = ["*.tmp", ".git/", "node_modules/"]
max_file_size = "10GB"

[tls]
ca_cert = "~/.config/tergum/certs/ca.crt"
cert = "~/.config/tergum/certs/server.crt"
key = "~/.config/tergum/certs/server.key"

[encryption]
enabled = true

[database]
path = "~/.config/tergum/tergum.db"
wal_mode = true

[backup]
chunk_size = 65536
max_concurrent_uploads = 4
max_concurrent_downloads = 8

[watcher]
enabled = true
debounce_ms = 500
stability_seconds = 60
ongoing_backup = true
batch_interval_minutes = 5

[webui]
enabled = true
port = 7480
username = "admin"
password = "admin"
session_timeout_hours = 24

[metrics]
enabled = true
port = 7490

[logging]
level = "info"
format = "text"

[scheduler]
full_backup_cron = ""
auto_backup_cron = ""
```

---

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | General error |
| 2 | Configuration error |
| 3 | Connection error |
| 4 | Authentication error |
| 5 | Storage error |
| 10 | Stopped by user |
| 11 | Backup failed |

---

## Network Ports

| Port | Service |
|------|---------|
| 7400 | gRPC CommandService |
| 7401 | gRPC DataService |
| 7480 | Web management UI |
| 7490 | Prometheus metrics + `/health` |
