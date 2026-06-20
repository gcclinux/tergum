# Tergum User Manual

## First-Time Setup

```bash
tergum setup
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

```bash
tergum setup --generate-certs
```

---

## Running a Backup

### Start the server first

The server must be running for backups to work (even in `both` mode for scheduled/watcher backups):

```bash
tergum server
```

### Manual backup

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

### Environment variables

| Variable | Description |
|----------|-------------|
| `TERGUM_PASSPHRASE` | Encryption passphrase (avoids interactive prompt) |

---

## Managing Backup Paths

### Add directories to back up

```bash
tergum paths add /home/user/Documents /home/user/Projects
```

### Remove a directory

```bash
tergum paths remove /home/user/Downloads
```

### Scan a directory (add all top-level folders)

```bash
# Scan home directory (skips hidden folders)
tergum paths scan

# Scan specific directory, include hidden folders
tergum paths scan /mnt/data --include-hidden
```

### Add exclude patterns

```bash
tergum paths exclude "*.tmp" "*.log" "node_modules/" ".git/"
```

### Remove an exclude pattern

```bash
tergum paths unexclude "*.log"
```

### View current configuration

```bash
tergum paths list
```

---

## All Commands

| Command | Status | Description |
|---------|--------|-------------|
| `tergum setup` | ✅ Working | Interactive configuration wizard |
| `tergum server` | ✅ Working | Start all server subsystems |
| `tergum admin` | ✅ Working | Start Web UI only (lightweight) |
| `tergum backup` | ✅ Working | Run a manual backup |
| `tergum paths` | ✅ Working | Manage include/exclude paths |
| `tergum list` | ✅ Working | List backup sets and files |
| `tergum delete` | ✅ Working | Delete backup entries |
| `tergum retention` | ✅ Working | Manage retention policies |
| `tergum restore` | ✅ Working | Restore files from backup |
| `tergum stop` | ✅ Working | Stop an in-progress backup |
| `tergum status` | ✅ Working | Show system status |
| `tergum watch` | ✅ Working | File watcher for ongoing backup |
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

Starts gRPC services (ports 7400, 7401), web UI (7480), metrics (7490),
retention engine, and scheduler. Graceful shutdown on SIGTERM/SIGINT.
```

### tergum admin

```
Usage: tergum admin [flags]

Flags:
  -p, --port int   Override web UI port (default: from config, typically 7480)
```

Starts just the Web UI without gRPC services, scheduler, or watcher. Use this for
lightweight browser-based management.

```bash
# Start on default port
tergum admin

# Start on a custom port
tergum admin --port 8080
```

The admin panel provides the same Web UI as the full server:
- Dashboard with storage stats and backup history
- Backup job management (view, delete)
- Include/exclude path management
- Retention policy management
- Restore file browser (search backed-up files)
- Watcher path management

### tergum backup

```
Usage: tergum backup [flags]

Flags:
  -l, --level string   Backup level: auto, full (default "auto")
      --json           Output result as JSON
```

**Auto** mode backs up files that are new or modified since the last backup.
**Full** mode backs up everything in the include paths regardless of changes.

```bash
# JSON output for scripting
TERGUM_PASSPHRASE=mypassphrase tergum backup --json
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

```bash
# JSON output for paths list
tergum paths list --json
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

### tergum delete

```
Usage: tergum delete [target] [flags]

Flags:
      --backup-id string   Target a specific backup set
      --all-backups        Delete across all backup sets
      --dry-run            Preview what would be deleted
      --json               Output as JSON
```

Must specify either `--backup-id` or `--all-backups`.

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
```

Output shows: entries deleted, bytes freed, physical storage files removed, and orphan jobs cleaned up.

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
  --keep-versions int    Minimum versions to always keep (default 1)
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
- The **latest version** of any file is NEVER deleted regardless of policies
- Files with only **one version** are never touched
- Higher priority policies are evaluated first (first-match-wins)

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

### tergum stop

```
Usage: tergum stop [--json]
```

Sends a stop signal to a running backup. The backup will complete its current file, update the job status to "stopped", and exit cleanly.

```bash
# In one terminal: start a backup
TERGUM_PASSPHRASE=mypass tergum backup --level full

# In another terminal: stop it
tergum stop
```

### tergum status

```
Usage: tergum status [--json]
```

Shows a full system overview: configuration, paths, backup history, storage usage, and last backup details.

```bash
tergum status
tergum status --json
```

### tergum watch

```
Usage: tergum watch [--json]
```

Starts the file watcher for continuous backup. Monitors all configured include paths
for changes and automatically backs up files that pass the stability gate.

```bash
# Start watcher (runs in foreground, Ctrl+C to stop)
TERGUM_PASSPHRASE=mypass tergum watch

# Or set passphrase via env for unattended operation
export TERGUM_PASSPHRASE=mypass
tergum watch
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

```bash
tergum list --json
tergum backup --json
tergum paths list --json
tergum version --json
tergum list --backup-id <id> --json
```

---

## Web UI

When the server or admin panel is running, the management dashboard is at:

```
http://localhost:7480
```

Default login: `admin` / `admin` (change in `tergum.toml`).

Start the Web UI with either:
```bash
tergum server    # full server with Web UI included
tergum admin     # Web UI only (lightweight, no gRPC/scheduler)
```

Pages and capabilities:
- **Dashboard** — storage stats, total files, total size
- **Backups** — job history with status, file count, and delete functionality
- **Restore** — search backed-up files by name/path (restore via CLI)
- **Config** — add/remove include paths and exclude patterns (syncs to TOML)
- **Retention** — add/remove retention policies with pattern matching
- **Watchers** — add/remove watch paths for file monitoring
- **Activity** — real-time event log (SSE)
- **Clients** — connected client nodes
- **Metrics** — backup and storage metrics

---

## Typical Workflow

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

---

## Configuration File

Location: `~/.config/tergum/tergum.toml`

```toml
[node]
role = "both"

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
