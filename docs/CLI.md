# Tergum CLI Reference

## Command Index

| Command | Status | Description |
|---------|--------|-------------|
| [`tergum setup`](#tergum-setup) | ✅ Working | Interactive configuration wizard |
| [`tergum server`](#tergum-server) | ✅ Working | Start server or client daemon (role-aware) |
| [`tergum admin`](#tergum-admin) | ✅ Working | Start Web UI only (lightweight) |
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
