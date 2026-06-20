# Tergum

Encrypted, deduplicated backup system with gRPC streaming, mutual TLS, policy-based retention, and real-time file watching.

A single statically-linked binary acts as client, server, or both.

## Building

Requires Go 1.24+. No CGO dependencies (pure Go SQLite via modernc.org/sqlite).

```bash
# Standard build
go build -o tergum ./

# Production build with version info
CGO_ENABLED=0 go build -ldflags="-s -w \
  -X 'github.com/ricardopadilha/tergum/cmd.Version=$(git describe --tags --always)' \
  -X 'github.com/ricardopadilha/tergum/cmd.Commit=$(git rev-parse --short HEAD)' \
  -X 'github.com/ricardopadilha/tergum/cmd.BuildDate=$(date -u +%Y-%m-%dT%H:%M:%SZ)'" \
  -o tergum ./

# Using Makefile
make build

# Run tests
make test
```

Cross-compile for any platform Go supports:

```bash
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o tergum-linux ./
GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -o tergum-macos ./
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o tergum.exe ./
```

### Build Scripts

Platform-specific build scripts are provided that handle cross-compilation and version embedding automatically.

| Script | Platform | Usage |
|--------|----------|-------|
| `build.sh` | Linux/macOS | `./build.sh [--prod] [target]` |
| `build.bat` | Windows (cmd) | `build.bat [--prod] [target]` |
| `build.ps1` | Windows (PowerShell) | `.\build.ps1 [-Prod] [target]` |

**Targets:** `linux`, `darwin` (or `macos`), `windows` (or `win`), `all` (default)

**The `--prod` flag:**

By default, the build scripts include a `-dirty` suffix in the version string when there are uncommitted changes in the working tree (via `git describe --tags --always --dirty`). This is useful during development to distinguish clean builds from modified ones.

Pass `--prod` (or `-Prod` in PowerShell) to produce a clean version string without the `-dirty` suffix. If the working tree has uncommitted changes, a warning is printed but the build proceeds.

```bash
# Development build (version may include -dirty suffix)
./build.sh windows

# Production build (clean version string, no -dirty suffix)
./build.sh --prod windows
```

```powershell
# Development build
.\build.ps1 windows

# Production build
.\build.ps1 -Prod windows
```

```batch
REM Development build
build.bat windows

REM Production build
build.bat --prod windows
```

All scripts output binaries to the `dist/` directory and embed version, commit hash, and build timestamp via ldflags.

## Quick Start

```bash
# 1. Run the setup wizard (generates config + TLS certs)
tergum setup

# 2. Start the server
tergum server

# 3. Trigger a backup (from another terminal)
tergum backup

# 4. Check status
tergum status
```

## Global Flags

These flags are available on all commands:

| Flag | Description |
|------|-------------|
| `--config <path>` | Path to TOML config file (default: platform-specific) |
| `--json` | Output machine-readable JSON |
| `--dry-run` | Preview destructive operations without executing |

## Commands

### `tergum setup`

Interactive first-time configuration wizard.

```bash
# Interactive mode — prompts for role, server address, storage, certs, passphrase
tergum setup

# Non-interactive — only generate TLS certificates
tergum setup --generate-certs
```

The wizard:
1. Selects node role (client / server / both)
2. Configures server address (for client role)
3. Sets storage path
4. Generates Ed25519 mTLS certificates (CA + server + client)
5. Sets encryption passphrase (Argon2id key derivation)
6. Writes `tergum.toml` config file

### `tergum server`

Starts all server subsystems. Handles graceful shutdown on SIGTERM/SIGINT.

```bash
tergum server
tergum server --config /etc/tergum/tergum.toml
```

Services started:
- gRPC CommandService (default port 7400)
- gRPC DataService (default port 7401)
- Web UI (default port 7480)
- Prometheus metrics + health endpoint (default port 7490)
- Retention engine (hourly evaluation)
- Cron scheduler (configured backup schedules)

#### Web Management UI

When the server is running with `webui.enabled = true` (the default for server/both roles), the embedded web dashboard is available at:

```
http://localhost:7480
```

Login with the credentials configured in `tergum.toml`:

```toml
[webui]
enabled = true
port = 7480
username = "admin"
password = "admin"
session_timeout_hours = 24
```

The default credentials are `admin` / `admin`. Change these in your config file before exposing the server to a network.

The dashboard provides:
- Real-time backup activity log (SSE)
- Backup job history and file browser
- Restore interface
- Retention policy management
- File watcher status
- Connected clients overview
- Metrics visualization
- Configuration editor

### `tergum backup`

Triggers a backup operation.

```bash
tergum backup                # auto-level (new/modified files)
tergum backup --level full   # full backup (all files)
tergum backup --json         # machine-readable output
```

| Flag | Default | Description |
|------|---------|-------------|
| `-l, --level` | `auto` | Backup level: `auto` or `full` |

### `tergum restore`

Search for and restore files from backup.

```bash
tergum restore "*.go"                     # restore by pattern
tergum restore --dest /tmp/restored       # custom destination
tergum restore --backup-id abc123         # from specific backup set
tergum restore --concurrency 8            # parallel streams
```

| Flag | Default | Description |
|------|---------|-------------|
| `-d, --dest` | `.` | Destination directory |
| `-c, --concurrency` | `4` | Parallel restore streams |
| `--backup-id` | | Restore from specific backup set |

### `tergum list`

List backup jobs and files.

```bash
tergum list                          # list all backup jobs
tergum list --backup-id abc123       # list files in a backup
tergum list --pattern "*.go"         # filter by glob
tergum list --json                   # JSON output
```

| Flag | Description |
|------|-------------|
| `--backup-id` | Show files within a specific backup |
| `-p, --pattern` | Filter files by glob pattern |

### `tergum delete`

Delete backup entries with optional dry-run preview.

```bash
tergum delete /path/to/file.txt --backup-id abc123
tergum delete /path/to/folder/ --all-backups
tergum delete --backup-id abc123                       # delete entire backup set
tergum delete /path/to/file.txt --all-backups --dry-run
```

| Flag | Description |
|------|-------------|
| `--backup-id` | Scope deletion to a specific backup set |
| `--all-backups` | Delete across all backup sets |
| `--dry-run` | Preview what would be deleted |

Physical storage files are only removed when no database entries reference them (refcount = 0).

### `tergum stop`

Gracefully stop an in-progress backup.

```bash
tergum stop
```

Completes the current file transfer, updates the job status to "stopped", and returns.

### `tergum watch`

Start the file watcher for ongoing (continuous) backup.

```bash
tergum watch              # foreground mode
tergum watch --daemon     # background daemon
```

Pipeline: filesystem event → exclude filter → 500ms debounce → 60s stability gate → BLAKE3 hash → upload.

| Flag | Description |
|------|-------------|
| `-d, --daemon` | Run as background daemon |

### `tergum status`

Show current system status.

```bash
tergum status
tergum status --json
```

### `tergum retention`

Manage retention policies.

```bash
# List policies
tergum retention list

# Add a policy
tergum retention add my-policy --keep-days 30 --keep-versions 3 --pattern "*.log" --priority 10

# Remove a policy
tergum retention remove my-policy

# Run evaluation manually
tergum retention run
tergum retention run --dry-run
```

Safety rules:
- The most recent version of any file is **never** deleted
- Files with only one version are **never** touched
- Policies evaluated in priority order (highest first, first-match-wins)

### `tergum migrate`

Migrate from Tergum v2.0 to v3.0.

```bash
tergum migrate --from-db /path/to/v2/tergum.db --rehash --encrypt --verify
```

| Flag | Description |
|------|---------|
| `--from-db` | Path to v2.0 SQLite database (required) |
| `--rehash` | Compute BLAKE3 hashes replacing MD5, rename storage files |
| `--encrypt` | Encrypt existing storage with AES-256-GCM |
| `--verify` | Post-migration integrity check |

The original v2.0 database is never modified. A rollback script is generated for file renames.

### `tergum version`

Print version information.

```bash
tergum version
tergum version --json
```

## Configuration

Tergum uses a TOML configuration file. Default locations:

| Platform | Path |
|----------|------|
| Linux | `~/.config/tergum/tergum.toml` |
| macOS | `~/Library/Application Support/tergum/tergum.toml` |
| Windows | `%APPDATA%\tergum\tergum.toml` |

### Full Configuration Reference

```toml
[node]
role = "client"           # "client", "server", or "both"
hostname = ""             # auto-detected if empty

[server]
address = "192.168.1.5"   # server address (required for client role)
command_port = 7400       # gRPC command service port
data_port = 7401          # gRPC data streaming port

[client]
include_paths = ["~/Documents", "~/Projects"]
exclude_patterns = ["*.tmp", "*.log", "node_modules/", ".git/"]
max_file_size = "10GB"    # skip files larger than this
```

### Default Exclude Patterns

When running `tergum setup`, the wizard offers a default set of exclude patterns that skip build artifacts, caches, and version control directories:

| Pattern | What it skips |
|---------|---------------|
| `*.tmp` | Temporary files |
| `*.log` | Log files |
| `*.o` | C/C++ object files |
| `*.class` | Java compiled classes |
| `.git/` | Git repositories |
| `.cache/` | General caches |
| `.nuget/` | .NET package cache |
| `.npm/` | NPM cache |
| `.gradle/` | Gradle cache |
| `node_modules/` | Node.js dependencies |
| `__pycache__/` | Python bytecode cache |
| `bin/Debug/` | .NET debug builds |
| `bin/Release/` | .NET release builds |
| `obj/` | .NET intermediate output |
| `target/` | Rust/Java/Maven build output |
| `dist/` | Build distribution folders |

You can add or remove patterns at any time with:

```bash
tergum paths exclude "*.bak" "vendor/"
tergum paths unexclude "dist/"
```

[tls]
ca_cert = "~/.config/tergum/certs/ca.crt"
cert = "~/.config/tergum/certs/client.crt"
key = "~/.config/tergum/certs/client.key"

[encryption]
enabled = true

[database]
path = "~/.config/tergum/tergum.db"
wal_mode = true

[backup]
chunk_size = 65536              # 64KB streaming chunks
max_concurrent_uploads = 4
max_concurrent_downloads = 8

[watcher]
enabled = true
debounce_ms = 500               # collapse rapid events within window
stability_seconds = 60          # file must be unchanged for this duration
ongoing_backup = true           # auto-backup stable files
batch_interval_minutes = 5      # group ongoing uploads into jobs

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
level = "info"            # debug, info, warn, error
format = "text"           # text or json

[scheduler]
full_backup_cron = "0 2 * * 0"    # weekly full backup (Sunday 2am)
auto_backup_cron = "0 3 * * *"    # daily incremental (3am)
```

## Architecture

```
┌─────────────────────────────────────────────────────┐
│                    Client                            │
│  Scanner → Manifest → Dedup → Encrypt → Stream      │
│  Watcher → Debounce → Stability → Ongoing Backup    │
└──────────────┬──────────────────────────────────────┘
               │ gRPC + mTLS (Ed25519)
               │ Port 7400 (commands) / 7401 (data)
┌──────────────▼──────────────────────────────────────┐
│                    Server                            │
│  CAS Storage (/hash[:2]/hash)                       │
│  SQLite DB (WAL mode)                               │
│  Retention Engine (hourly)                          │
│  Scheduler (cron)                                   │
│  Web UI (:7480) / Metrics (:7490)                   │
└─────────────────────────────────────────────────────┘
```

## Key Features

- **Encryption**: AES-256-GCM with per-file DEK, Argon2id master key derivation
- **Deduplication**: BLAKE3 content-addressable storage, manifest-based diff
- **Transport**: gRPC streaming with mutual TLS (Ed25519 certificates)
- **Retention**: Policy-based expiration with safety guarantees
- **File Watching**: fsnotify with debouncing and stability gate
- **Scheduling**: Cron-based full/auto backups
- **Observability**: Prometheus metrics, structured logging, health endpoint
- **Web UI**: Embedded HTMX/Alpine.js dashboard with SSE activity log
- **Migration**: v2.0 → v3.0 with rehash, encrypt, and rollback support
- **Cross-platform**: Linux, macOS, Windows with platform-specific metadata

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | General error |
| 2 | Configuration error |
| 3 | Connection error |
| 4 | Authentication error |
| 5 | Storage error |
| 10 | Stopped by user (graceful shutdown) |
| 11 | Backup failed |

## Network Ports

| Port | Service |
|------|---------|
| 7400 | gRPC CommandService (backup, stop, status, list, delete, retention) |
| 7401 | gRPC DataService (upload, download, sync, manifest exchange) |
| 7480 | Web management UI (HTTPS) |
| 7490 | Prometheus metrics + `/health` endpoint |

## License

See LICENSE file.
