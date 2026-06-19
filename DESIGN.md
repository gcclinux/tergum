# Tergum v3.0 — Go Redesign Document

**Author:** Ricardo Wagemaker  
**Version:** 3.0  
**Language:** Go 1.22+  
**Status:** Design Phase  
**Date:** June 2026  

---

## 1. Motivation

The current Tergum system (v2.0) is a Java-based client/server backup system using raw TCP sockets, a custom binary protocol, and SQLite for metadata. While functional, it has several limitations that a Go rewrite addresses:

| Concern | Java v2.0 | Go v3.0 |
|---------|-----------|---------|
| Deployment | Requires JRE 21+, classpath management | Single static binary per platform |
| Protocol | Custom binary framing (0x02/0x04 delimiters) | gRPC + Protobuf with streaming |
| Security | Plaintext TCP | Mutual TLS + AES-256-GCM at-rest encryption |
| Concurrency | Thread-per-connection | Goroutines with bounded worker pools |
| Hashing | MD5 (collision-vulnerable) | BLAKE3 (fast, cryptographically strong) |
| Configuration | SQLite CONFIG table only | TOML config file + SQLite for runtime state |
| UI | Swing desktop GUI | Embedded web UI (server-side rendered) |
| Cross-compilation | JAR runs anywhere with JRE | `GOOS/GOARCH` produces native binaries |
| Retention | Manual deletion only | Policy-based automatic expiration |

---

## 2. Architecture Overview

```
┌──────────────────────────────────────────────────────────────────────────────┐
│                         TERGUM v3.0 SYSTEM                                   │
├──────────────────────────────┬───────────────────────────────────────────────┤
│          CLIENT              │                SERVER                         │
│                              │                                               │
│  ┌────────────────────┐      │      ┌──────────────────────────────┐         │
│  │  gRPC Command       │◄────┼──────│  Scheduler / API Trigger     │         │
│  │  Handler (:7400)    │     │      └──────────────────────────────┘         │
│  └────────┬───────────┘      │                                               │
│           │                  │      ┌──────────────────────────────┐         │
│           ▼                  │      │  gRPC Data Receiver (:7401)  │         │
│  ┌────────────────────┐      │      │  (Streaming file upload)     │         │
│  │  Backup Engine      │─────┼─────►└──────────────────────────────┘         │
│  │  - Config validator │     │                                               │
│  │  - BLAKE3 scanner   │     │      ┌──────────────────────────────┐         │
│  │  - Delta detection  │     │      │  gRPC Restore Sender (:7401) │         │
│  │  - Stream sender    │     │      │  (Streaming file download)   │         │
│  └────────────────────┘      │      └──────────────┬───────────────┘         │
│                              │                     │                         │
│  ┌────────────────────┐      │                     │                         │
│  │  Restore Receiver   │◄────┼─────────────────────┘                         │
│  │  (Stream + decrypt) │     │                                               │
│  └────────────────────┘      │      ┌──────────────────────────────┐         │
│                              │      │  Web UI (:7480)              │         │
│  ┌────────────────────┐      │      │  - Dashboard                 │         │
│  │  File Watcher       │     │      │  - Config editor             │         │
│  │  (fsnotify)         │     │      │  - Backup/restore mgmt       │         │
│  └────────────────────┘      │      │  - Activity log              │         │
│                              │      └──────────────────────────────┘         │
└──────────────────────────────┴───────────────────────────────────────────────┘
```

### Design Principles

1. **Single binary** — One compiled executable acts as both client and server based on subcommand.
2. **gRPC streaming** — All file transfers use bidirectional gRPC streams with backpressure.
3. **Content-addressable storage** — Files stored by BLAKE3 hash (replacing MD5).
4. **Encryption by default** — TLS in transit, AES-256-GCM at rest.
5. **Observable** — Structured logging (slog), Prometheus metrics endpoint, health checks.
6. **Backward compatible** — Can import v2.0 SQLite databases and read existing backup storage.

---

## 3. Ports & Connectivity

| Port | Service | Protocol | Purpose |
|------|---------|----------|---------|
| **7400** | Command & Control | gRPC (mTLS) | Backup triggers, stop signals, status queries, scheduling |
| **7401** | Data Transfer | gRPC (mTLS) | Bidirectional streaming for backup upload and restore download |
| **7480** | Web UI | HTTPS | Embedded management dashboard |
| **7490** | Metrics | HTTP | Prometheus `/metrics` endpoint |

### Port Consolidation (vs v2.0)

v2.0 used three separate raw TCP ports (4366, 4355, 4344). v3.0 consolidates into two gRPC ports with service multiplexing:

- **Port 7400** replaces TCPDataComm (4366) — all command/control traffic
- **Port 7401** replaces both TCPDataReceiver (4355) and TCPRestore (4344) — all data streaming in both directions via gRPC service methods

### Connection Model

```
Client ──── mTLS ────► Server:7400  (CommandService)
Client ──── mTLS ────► Server:7401  (DataService.Upload / DataService.Download)
Browser ─── HTTPS ───► Server:7480  (Web UI)
Prometheus ─ HTTP ───► Server:7490  (/metrics)
```

All gRPC connections use mutual TLS. The client and server each hold a certificate signed by a shared CA generated during `tergum setup`.

---

## 4. gRPC & Protobuf Protocol

### Proto Definitions

```protobuf
syntax = "proto3";
package tergum.v3;

option go_package = "github.com/rwagemaker/tergum/proto/v3";

// ─── Command Service (port 7400) ───────────────────────────────────

service CommandService {
  // Server triggers a backup on the client
  rpc TriggerBackup(BackupRequest) returns (BackupResponse);
  
  // Client or server requests to stop a running backup
  rpc StopBackup(StopRequest) returns (StopResponse);
  
  // Query backup status
  rpc GetStatus(StatusRequest) returns (StatusResponse);
  
  // Heartbeat / health check
  rpc Ping(PingRequest) returns (PingResponse);
  
  // List backup sets
  rpc ListBackups(ListBackupsRequest) returns (ListBackupsResponse);
  
  // Delete a backup set
  rpc DeleteBackup(DeleteBackupRequest) returns (DeleteBackupResponse);
  
  // Get retention policy status
  rpc GetRetention(RetentionRequest) returns (RetentionResponse);
}

// ─── Data Service (port 7401) ──────────────────────────────────────

service DataService {
  // Client streams file chunks to server during backup
  rpc Upload(stream FileChunk) returns (UploadSummary);
  
  // Server streams file chunks to client during restore
  rpc Download(RestoreRequest) returns (stream FileChunk);
  
  // Client sends its database to server after backup
  rpc SyncDatabase(stream DatabaseChunk) returns (SyncResponse);
  
  // Manifest exchange: client sends list of hashes, server replies with missing ones
  rpc ExchangeManifest(Manifest) returns (ManifestDiff);
}

// ─── Messages ──────────────────────────────────────────────────────

message BackupRequest {
  string client_id = 1;
  BackupLevel level = 2;
  string initiated_by = 3;   // "scheduler", "api", "cli"
}

enum BackupLevel {
  AUTO = 0;      // Only new/modified files (scheduled)
  FULL = 1;      // All files regardless of change (scheduled)
  ONGOING = 2;   // Watcher-driven continuous backup (event-based)
}

message FileChunk {
  oneof payload {
    FileHeader header = 1;
    bytes data = 2;
    FileTrailer trailer = 3;
  }
}

message FileHeader {
  string blake3_hash = 1;
  string file_name = 2;
  string file_path = 3;
  int64 file_size = 4;
  FileMetadata metadata = 5;
  bytes encrypted_key = 6;  // AES-256-GCM key wrapped with server public key
  bytes nonce = 7;          // GCM nonce for this file
}

message FileMetadata {
  int64 created_at = 1;
  int64 modified_at = 2;
  int64 accessed_at = 3;
  string owner = 4;
  string group = 5;
  uint32 permissions = 6;    // Unix mode bits
  bool hidden = 7;
  bool symlink = 8;
  string symlink_target = 9;
  string os = 10;
  string extension = 11;
}

message FileTrailer {
  string blake3_hash = 1;   // Final hash verification
  int64 bytes_sent = 2;
}

message UploadSummary {
  int64 files_received = 1;
  int64 bytes_received = 2;
  int64 files_deduplicated = 3;
  string backup_id = 4;
}

message RestoreRequest {
  string blake3_hash = 1;
  string destination_path = 2;
}

message Manifest {
  string backup_id = 1;
  repeated ManifestEntry entries = 2;
}

message ManifestEntry {
  string blake3_hash = 1;
  string file_path = 2;
  int64 file_size = 3;
  int64 modified_at = 4;
}

message ManifestDiff {
  repeated string needed_hashes = 1;  // Hashes the server doesn't have
  int64 already_stored = 2;           // Count of deduplicated files
}
```

### Protocol Flow — Backup with Manifest

```
Client                                      Server
  │                                            │
  │  1. ExchangeManifest(full file list)       │
  │───────────────────────────────────────────►│
  │                                            │
  │  2. ManifestDiff(needed_hashes[])          │
  │◄───────────────────────────────────────────│
  │                                            │
  │  3. Upload(stream: header→chunks→trailer)  │
  │    (only files in needed_hashes)           │
  │───────────────────────────────────────────►│
  │                                            │
  │  4. UploadSummary                          │
  │◄───────────────────────────────────────────│
  │                                            │
  │  5. SyncDatabase(stream: db chunks)        │
  │───────────────────────────────────────────►│
  │                                            │
```

This replaces the v2.0 approach where each file was individually checked against in-memory databases. The manifest exchange deduplicates in a single round-trip.

---

## 5. Encryption

### 5.1 In-Transit: Mutual TLS (mTLS)

All gRPC connections require mutual TLS authentication:

```
tergum setup --generate-certs
```

This generates:
- `ca.crt` / `ca.key` — Certificate Authority (stays on the server)
- `server.crt` / `server.key` — Server identity
- `client.crt` / `client.key` — Client identity (signed by CA)

Certificates use Ed25519 keys (small, fast). The CA signs both server and client certificates, enabling mutual authentication without an external PKI.

### 5.2 At-Rest: AES-256-GCM

Files stored on the server are encrypted using AES-256-GCM:

```
┌─────────────────────────────────────────────────────────┐
│  Storage Format (per file)                              │
├─────────────────────────────────────────────────────────┤
│  [12-byte nonce] [encrypted payload] [16-byte GCM tag] │
└─────────────────────────────────────────────────────────┘
```

**Key management:**
- A master encryption key is derived during `tergum setup` using Argon2id from a user passphrase.
- Each file gets a unique random 256-bit data encryption key (DEK).
- The DEK is wrapped (encrypted) with the master key and stored alongside the file metadata in SQLite.
- The master key never leaves the server memory; only the wrapped DEK is persisted.

### 5.3 Hash Upgrade: MD5 → BLAKE3

| Property | MD5 (v2.0) | BLAKE3 (v3.0) |
|----------|------------|---------------|
| Output size | 128-bit | 256-bit |
| Collision resistance | Broken | Cryptographically strong |
| Speed (large files) | ~500 MB/s | ~3+ GB/s (with SIMD) |
| Incremental hashing | No | Yes (tree structure) |

BLAKE3 hashes serve dual purpose: content addressing for deduplication and integrity verification during transfer.

---

## 6. SQLite Schema

v3.0 retains SQLite as the metadata store but restructures the schema for normalization, retention support, and encryption metadata.

### 6.1 `backups` Table

```sql
CREATE TABLE backups (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    backup_id       TEXT NOT NULL,           -- Job ID (timestamp-based: 20260619143022)
    blake3_hash     TEXT NOT NULL,           -- BLAKE3 hash of file content
    file_name       TEXT NOT NULL,           -- Original file name
    file_path       TEXT NOT NULL,           -- Original absolute path
    file_ext        TEXT,                    -- File extension (lowercase)
    file_size       INTEGER NOT NULL,        -- Size in bytes
    created_at      INTEGER,                -- Creation timestamp (Unix ms)
    modified_at     INTEGER,                -- Modification timestamp (Unix ms)
    accessed_at     INTEGER,                -- Access timestamp (Unix ms)
    permissions     INTEGER,                -- Unix permission bits
    owner           TEXT,                    -- File owner
    file_group      TEXT,                    -- File group
    hidden          INTEGER DEFAULT 0,      -- Boolean: hidden file
    symlink         INTEGER DEFAULT 0,      -- Boolean: is symlink
    symlink_target  TEXT,                    -- Symlink destination
    os              TEXT NOT NULL,           -- Originating OS
    encrypted_dek   BLOB,                   -- Wrapped AES-256 data encryption key
    nonce           BLOB,                   -- 12-byte GCM nonce
    backup_date     TEXT NOT NULL DEFAULT (datetime('now')),
    expires_at      TEXT,                    -- Retention expiry (NULL = keep forever)
    
    FOREIGN KEY (backup_id) REFERENCES backup_jobs(backup_id)
);

CREATE INDEX idx_backups_hash ON backups(blake3_hash);
CREATE INDEX idx_backups_job ON backups(backup_id);
CREATE INDEX idx_backups_path ON backups(file_path);
CREATE INDEX idx_backups_name ON backups(file_name);
CREATE INDEX idx_backups_ext ON backups(file_ext);
CREATE INDEX idx_backups_expires ON backups(expires_at);
```

### 6.2 `backup_jobs` Table

```sql
CREATE TABLE backup_jobs (
    backup_id       TEXT PRIMARY KEY,        -- Timestamp-based ID
    level           TEXT NOT NULL CHECK(level IN ('AUTO','FULL')),
    client_id       TEXT NOT NULL,           -- Client hostname
    client_ip       TEXT,                    -- Client IP address
    initiated_by    TEXT DEFAULT 'cli',      -- 'cli', 'scheduler', 'api', 'watcher'
    started_at      TEXT NOT NULL DEFAULT (datetime('now')),
    finished_at     TEXT,
    status          TEXT NOT NULL DEFAULT 'running'
                    CHECK(status IN ('running','completed','failed','stopped','expired')),
    file_count      INTEGER DEFAULT 0,
    bytes_total     INTEGER DEFAULT 0,
    bytes_new       INTEGER DEFAULT 0,      -- Bytes actually transferred (not deduplicated)
    files_deduped   INTEGER DEFAULT 0,      -- Files skipped due to deduplication
    error_message   TEXT
);

CREATE INDEX idx_jobs_client ON backup_jobs(client_id);
CREATE INDEX idx_jobs_status ON backup_jobs(status);
CREATE INDEX idx_jobs_started ON backup_jobs(started_at);
```

### 6.3 `restore_history` Table

```sql
CREATE TABLE restore_history (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    blake3_hash     TEXT NOT NULL,
    file_name       TEXT NOT NULL,
    source_backup   TEXT NOT NULL,           -- backup_id it was restored from
    restored_to     TEXT NOT NULL,           -- Destination path
    restored_at     TEXT NOT NULL DEFAULT (datetime('now')),
    restored_by     TEXT DEFAULT 'cli',      -- 'cli', 'api', 'webui'
    success         INTEGER DEFAULT 1
);
```

### 6.4 `retention_policies` Table

```sql
CREATE TABLE retention_policies (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    name            TEXT NOT NULL UNIQUE,
    keep_days       INTEGER,                 -- Delete older versions after N days (NULL = forever)
    keep_versions   INTEGER DEFAULT 1,       -- Keep at least N versions per file path (min: 1)
    pattern         TEXT,                    -- Glob pattern to match (e.g. '*.log')
    priority        INTEGER DEFAULT 0,       -- Higher = evaluated first
    enabled         INTEGER DEFAULT 1,
    created_at      TEXT NOT NULL DEFAULT (datetime('now'))
);
-- NOTE: The latest version of any file is ALWAYS protected regardless of policy.
-- keep_versions = 1 means "keep only the latest" (delete all older expired versions).
-- keep_versions = 7 means "keep the 7 most recent versions" (delete older expired ones).
```

### 6.5 `watched_paths` Table

```sql
CREATE TABLE watched_paths (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    path            TEXT NOT NULL UNIQUE,
    recursive       INTEGER DEFAULT 1,
    enabled         INTEGER DEFAULT 1,
    last_event      TEXT,
    event_count     INTEGER DEFAULT 0,
    added_at        TEXT NOT NULL DEFAULT (datetime('now'))
);
```

### 6.6 `config` Table (Runtime State Only)

```sql
CREATE TABLE config (
    key             TEXT PRIMARY KEY,
    value           TEXT NOT NULL,
    updated_at      TEXT NOT NULL DEFAULT (datetime('now'))
);

-- Default entries:
-- ('node_role', 'client')           -- 'client', 'server', 'both'
-- ('server_address', '')
-- ('grpc_command_port', '7400')
-- ('grpc_data_port', '7401')
-- ('web_ui_port', '7480')
-- ('metrics_port', '7490')
-- ('storage_path', '')
-- ('encryption_enabled', 'true')
-- ('last_backup_id', '')
```

### 6.7 Database Placement & Sync Flow

Both the client and the server maintain their own SQLite database. After each backup, the client sends a copy of its database to the server — ensuring the server can perform restores and searches even if the client is destroyed.

```
┌────────────────────────────────────┐     ┌────────────────────────────────────┐
│           CLIENT                   │     │             SERVER                 │
│                                    │     │                                    │
│  ~/.config/tergum/tergum.db        │     │  /var/lib/tergum/storage/          │
│  ┌──────────────────────────────┐  │     │  ┌──────────────────────────────┐  │
│  │ backups          (own files) │  │     │  │ tergum.db (server master)    │  │
│  │ backup_jobs      (own jobs)  │  │     │  │  ├─ backup_jobs (all clients)│  │
│  │ config           (local cfg) │  │     │  │  ├─ retention_policies       │  │
│  │ watched_paths    (watchers)  │  │     │  │  └─ config                   │  │
│  │ restore_history  (restores)  │  │     │  ├──────────────────────────────┤  │
│  └──────────────────────────────┘  │     │  │ clients/                     │  │
│                                    │     │  │  ├─ workstation1.db (copy)   │  │
│  After each backup:                │     │  │  ├─ workstation2.db (copy)   │  │
│  SyncDatabase ─────────────────────┼────►│  │  └─ laptop1.db (copy)       │  │
│  (streams tergum.db to server)     │     │  └──────────────────────────────┘  │
│                                    │     │                                    │
└────────────────────────────────────┘     └────────────────────────────────────┘
```

**Client database contains:**
- `backups` — Metadata for every file this client has backed up (hashes, paths, permissions, timestamps)
- `backup_jobs` — History of backup jobs initiated on this client
- `config` — Local runtime configuration
- `watched_paths` — File watcher registrations
- `restore_history` — Log of files restored to this client

**Server database contains:**
- `backup_jobs` — Unified view of all client backup sessions (who, when, status, size)
- `retention_policies` — Server-wide retention rules
- `config` — Server runtime configuration

**Server also stores copies of each client's database** in a `clients/` subdirectory. These copies enable:
- Restore operations when the client is offline or destroyed
- Server-side search across all clients from the Web UI
- Audit and reporting across the entire backup fleet

**Sync flow after each backup:**

```
1. Backup completes (all files uploaded)
        │
        ▼
2. Client updates local DB
   (backup_jobs status → completed, file_count, bytes)
        │
        ▼
3. Client calls SyncDatabase (gRPC :7401)
   Streams its entire tergum.db to the server
        │
        ▼
4. Server saves as clients/{hostname}.db
   Updates its own backup_jobs table with final status
        │
        ▼
5. Server applies retention policies to new entries
```

**Why both sides have a database:**

| Scenario | Client DB handles it | Server DB copy handles it |
|----------|---------------------|--------------------------|
| List my backed-up files | Yes (local query, no network) | — |
| Restore after client disk failure | — | Yes (server has metadata + files) |
| Search across all clients | — | Yes (Web UI queries all client DBs) |
| Determine what needs backup (manifest) | Yes (local hash lookup) | — |
| Ongoing backup deduplication | Yes (compare local hashes) | — |
| Server-side retention enforcement | — | Yes (knows what's expired) |
| Client offline/destroyed recovery | — | Yes (full metadata available) |

---

## 7. Retention Policies

v2.0 had no automatic retention — backups accumulated until manually deleted. v3.0 introduces policy-based retention with one fundamental safety rule:

> **Core Rule:** Retention will NEVER delete the last remaining copy of a file. Only older versions of a file that has multiple versions can be automatically removed. A file can only be fully removed through explicit manual deletion.

### How Versioning Works

Every time a file at the same path is backed up with a different BLAKE3 hash, it creates a new **version**. Retention operates on versions, not files:

```
FILE: ~/Documents/budget.xlsx

Version 1 │ hash: abc123 │ 2026-01-15 │ ← oldest (candidate for expiry)
Version 2 │ hash: def456 │ 2026-03-20 │ ← older   (candidate for expiry)
Version 3 │ hash: ghi789 │ 2026-06-10 │ ← latest  (ALWAYS kept)
```

Retention can delete versions 1 and 2 based on policy, but version 3 (the latest) is **never touched** regardless of its age or any policy rule.

### Default Policies

| Policy | Rule | Description |
|--------|------|-------------|
| `daily` | keep_days: 30, keep_versions: 7 | Keep last 7 versions or 30 days, whichever retains more |
| `weekly` | keep_days: 90, keep_versions: 4 | Weekly granularity retained for 90 days |
| `monthly` | keep_days: 365, keep_versions: 12 | Monthly granularity retained for 1 year |
| `forever` | keep_days: NULL | All versions kept indefinitely |
| `logs` | keep_days: 7, keep_versions: 1, pattern: `*.log` | Only latest version after 7 days |

### Retention Engine

The retention engine runs as a background goroutine on the server:

```
┌──────────────────────────────────────────────────────────────────────────┐
│  Retention Engine (runs every hour)                                       │
├──────────────────────────────────────────────────────────────────────────┤
│                                                                          │
│  For each unique file_path in backups:                                   │
│                                                                          │
│    1. Count versions (entries with same file_path, different hash)        │
│                                                                          │
│    2. If only 1 version exists → SKIP (never delete last copy)           │
│                                                                          │
│    3. If multiple versions exist:                                        │
│       a. Identify the latest version (most recent backup_date)           │
│       b. Mark latest as PROTECTED (never expires)                        │
│       c. For each older version:                                         │
│          - Match against retention policies (by pattern, priority)       │
│          - If expired (backup_date + keep_days < now) → candidate        │
│          - If exceeds keep_versions → candidate                          │
│          - Check: would deleting this leave ≥ 1 version? → DELETE        │
│                                                                          │
│    4. For deleted entries:                                                │
│       - Remove row from backups table                                    │
│       - If no other entry references same blake3_hash → delete file      │
│         from storage (content-addressable: other files may share hash)   │
│                                                                          │
│    5. Log summary to activity log                                        │
│    6. Update Prometheus metrics                                          │
│                                                                          │
└──────────────────────────────────────────────────────────────────────────┘
```

### Safety Guarantees

| Rule | Guarantee |
|------|-----------|
| Last version protection | The most recent version of any file is NEVER deleted by retention |
| Single-version files | Files that have never been modified (only 1 version) are never touched |
| Storage deduplication | Physical file on disk only deleted when zero DB entries reference its hash |
| Manual override only | To fully remove a file from backup, user must explicitly `tergum delete` |
| Dry-run verification | `--dry-run` always available to preview before committing |

### Example Scenarios

```
Scenario 1: budget.xlsx — backed up 20 times over 6 months
┌────────────────────────────────────────────────────────────────┐
│  Versions: 20                                                  │
│  Policy: daily (keep_days: 30, keep_versions: 7)               │
│                                                                │
│  Result:                                                       │
│    - Latest version: KEPT (always protected)                   │
│    - 6 most recent older versions: KEPT (within keep_versions) │
│    - 13 oldest versions: DELETED (older than 30 days AND       │
│      exceeds keep_versions limit)                              │
│    - Remaining: 7 versions                                     │
└────────────────────────────────────────────────────────────────┘

Scenario 2: family-photo.jpg — backed up once, never changed
┌────────────────────────────────────────────────────────────────┐
│  Versions: 1                                                   │
│  Policy: any                                                   │
│                                                                │
│  Result:                                                       │
│    - KEPT forever (only 1 version — retention never touches it)│
│    - Can only be removed via: tergum delete --file family.jpg  │
└────────────────────────────────────────────────────────────────┘

Scenario 3: app.log — backed up daily, grows constantly
┌────────────────────────────────────────────────────────────────┐
│  Versions: 30                                                  │
│  Policy: logs (keep_days: 7, keep_versions: 1)                 │
│                                                                │
│  Result:                                                       │
│    - Latest version: KEPT (always protected)                   │
│    - Older than 7 days AND beyond 1 version: DELETED           │
│    - Remaining: 1 version (the latest)                         │
└────────────────────────────────────────────────────────────────┘
```

### Policy Assignment

When a backup completes, the retention engine evaluates policies by priority:

1. Match file against `pattern` globs (highest priority first)
2. First matching policy wins
3. Compute `expires_at = backup_date + keep_days` for all versions except the latest
4. If no policy matches, the file has `expires_at = NULL` (all versions kept forever)
5. Latest version always has `expires_at = NULL` regardless of policy

### CLI Commands

```bash
tergum retention list                          # Show all policies
tergum retention add --name temp --days 7 --versions 1 --pattern "*.tmp"
tergum retention remove --name temp
tergum retention run --dry-run                 # Preview what would be deleted
tergum retention run                           # Execute retention now
tergum retention status                        # Show version counts per file
tergum delete --file budget.xlsx               # Manually delete ALL versions (requires confirmation)
tergum delete --file budget.xlsx --version 2   # Delete a specific version (if not the last one)
```

---

## 7.1 Deletion & Backup Set Management

Retention handles automatic cleanup of old versions. But you also need manual control to delete entire backup sets, specific folders, or individual files within a backup — just like v2.0's "Delete backup set" feature, but with finer granularity.

### Deletion Levels

```
┌──────────────────────────────────────────────────────────────────────────┐
│  Deletion Granularity                                                     │
├──────────────────────────────────────────────────────────────────────────┤
│                                                                          │
│  Level 1: DELETE ENTIRE BACKUP SET                                       │
│  ─────────────────────────────────                                       │
│  Remove every file associated with a backup job ID.                      │
│  Equivalent to v2.0 "Delete backup set" in the GUI.                      │
│                                                                          │
│  Level 2: DELETE A FOLDER FROM A BACKUP SET                              │
│  ──────────────────────────────────────────                              │
│  Remove all files under a specific path within a backup.                 │
│  The rest of the backup remains intact.                                  │
│                                                                          │
│  Level 3: DELETE A SINGLE FILE FROM A BACKUP SET                         │
│  ────────────────────────────────────────────────                        │
│  Remove one specific file entry from a backup.                           │
│  Everything else in the backup remains intact.                           │
│                                                                          │
└──────────────────────────────────────────────────────────────────────────┘
```

### CLI Usage

```bash
# ─── Level 1: Delete entire backup set ──────────────────────────────────
tergum delete --backup-id 20260619143022              # Delete all files from this backup job
tergum delete --backup-id 20260619143022 --dry-run    # Preview what would be removed

# ─── Level 2: Delete a folder from a backup ─────────────────────────────
tergum delete --backup-id 20260619143022 --folder "/home/ricardo/Documents/old-project"
tergum delete --backup-id 20260619143022 --folder "C:\Users\ricardo\Downloads"

# ─── Level 3: Delete a single file from a backup ────────────────────────
tergum delete --backup-id 20260619143022 --file "/home/ricardo/Documents/secret.pdf"
tergum delete --backup-id 20260619143022 --file "budget.xlsx"    # Match by name

# ─── Delete across ALL backups (all versions of a file/folder) ──────────
tergum delete --file secret.pdf --all-backups         # Remove from every backup set
tergum delete --folder "old-project" --all-backups    # Remove folder from all backups

# ─── Interactive mode (browse and select) ────────────────────────────────
tergum delete --backup-id 20260619143022 --interactive
```

### Interactive Delete Workflow

```
$ tergum delete --backup-id 20260619143022 --interactive

Backup: 20260619143022 | 2026-06-19 14:30 | 4,521 files | 2.3 GB

Browse backup contents:
┌─────────────────────────────────────────────────────┐
│  /home/ricardo/                                     │
│  ├── Documents/          (1,204 files, 890 MB)     │
│  │   ├── work/           (342 files, 210 MB)       │
│  │   ├── personal/       (156 files, 95 MB)        │
│  │   └── old-project/    (706 files, 585 MB)  ← ☒  │
│  ├── Projects/           (2,891 files, 1.2 GB)     │
│  └── Pictures/           (426 files, 245 MB)       │
└─────────────────────────────────────────────────────┘

Selected for deletion: old-project/ (706 files, 585 MB)

Confirm deletion? This will:
  - Remove 706 file entries from backup 20260619143022
  - Delete storage files not referenced by other backups
  - Free approximately 585 MB of disk space

[y/N]: y

Deleted: 706 entries removed, 584 MB freed (12 files shared with other backups, kept)
```

### Web UI Delete

The Web UI provides the same functionality with a visual file browser:

1. Navigate to **Backups** page
2. Select a backup set from the list
3. Click **Browse** to see the full file/folder tree
4. Check individual files or entire folders
5. Click **Delete Selected** → confirmation dialog shows impact
6. Deletion executes, page refreshes with updated totals

### Deletion Engine Logic

```
┌──────────────────────────────────────────────────────────────────────────┐
│  Delete Operation                                                         │
├──────────────────────────────────────────────────────────────────────────┤
│                                                                          │
│  Input: backup_id + (file_path | folder_path | entire set)               │
│                                                                          │
│  1. Identify target entries in backups table:                            │
│     - Entire set: WHERE backup_id = ?                                    │
│     - Folder:     WHERE backup_id = ? AND file_path LIKE '/folder/%'     │
│     - File:       WHERE backup_id = ? AND file_path = ?                  │
│                                                                          │
│  2. For each entry to delete:                                            │
│     a. Record the blake3_hash                                            │
│     b. Delete the row from backups table                                 │
│                                                                          │
│  3. For each unique blake3_hash from deleted entries:                     │
│     a. Check: do OTHER entries in backups still reference this hash?      │
│        (Same file content may exist in other backup sets or paths)        │
│     b. If YES → keep the storage file (still needed)                     │
│     c. If NO  → delete the physical file from storage                    │
│                                                                          │
│  4. Update backup_jobs table:                                            │
│     - Recalculate file_count and bytes_total for affected backup_id      │
│     - If file_count = 0 → remove the backup_jobs entry entirely          │
│                                                                          │
│  5. Log the operation to activity log                                    │
│  6. Sync updated database to server (if executed from client)            │
│                                                                          │
└──────────────────────────────────────────────────────────────────────────┘
```

### Safety: Storage Deduplication Awareness

Because files are stored by BLAKE3 hash (content-addressable), the same physical file may be referenced by multiple backup entries:

```
Storage file: abc123def456...   (the physical file on disk)
    ├── Referenced by: backup 20260601 → /Documents/report.docx
    ├── Referenced by: backup 20260615 → /Documents/report.docx  (same version)
    └── Referenced by: backup 20260619 → /Archive/report-copy.docx (same content, different path)
```

Deleting `report.docx` from backup `20260601` removes that DB entry, but the storage file stays because backups `20260615` and `20260619` still reference the same hash. The physical file is only deleted when **zero** database entries reference it.

### gRPC Support

```protobuf
// Added to CommandService (port 7400)
service CommandService {
  // ... existing RPCs ...
  
  // Delete entries from a backup set
  rpc DeleteFromBackup(DeleteRequest) returns (DeleteResponse);
}

message DeleteRequest {
  string backup_id = 1;
  oneof target {
    bool entire_set = 2;              // Delete all entries in the backup
    string folder_path = 3;           // Delete all entries under this folder
    string file_path = 4;             // Delete a specific file entry
  }
  bool dry_run = 5;                   // Preview only, don't execute
}

message DeleteResponse {
  int64 entries_removed = 1;
  int64 bytes_freed = 2;              // Actual storage freed (after dedup check)
  int64 files_kept_shared = 3;        // Storage files kept because still referenced
  repeated string deleted_paths = 4;  // Paths that were removed (for confirmation)
}
```

---

## 8. Backup Flow (Detailed)

```
┌─────────┐                                         ┌─────────┐
│ SERVER  │                                         │ CLIENT  │
└────┬────┘                                         └────┬────┘
     │                                                   │
     │  1. TriggerBackup(level=AUTO, client=hostname)    │
     │──────────────────────────────────────────────────►│
     │                  (gRPC :7400)                      │
     │                                                   │
     │                                    2. Client validates:
     │                                       - Config file exists
     │                                       - Include paths accessible
     │                                       - Exclude patterns loaded
     │                                       - Certificate valid
     │                                                   │
     │                                    3. Client scans file tree:
     │                                       - Walk include paths
     │                                       - Apply exclude patterns
     │                                       - Compute BLAKE3 per file
     │                                       - Build manifest
     │                                                   │
     │  4. ExchangeManifest(entries[])                   │
     │◄──────────────────────────────────────────────────│
     │                  (gRPC :7401)                      │
     │                                                   │
     │  5. Server compares manifest against stored hashes│
     │     Returns ManifestDiff(needed_hashes[])         │
     │──────────────────────────────────────────────────►│
     │                                                   │
     │                                    6. For each needed file:
     │                                       - Generate random DEK
     │                                       - Encrypt file with AES-256-GCM
     │                                       - Stream chunks (64KB each)
     │                                                   │
     │  7. Upload(stream: header→data→trailer per file)  │
     │◄──────────────────────────────────────────────────│
     │                  (gRPC :7401)                      │
     │                                                   │
     │  8. Server receives, verifies BLAKE3, stores      │
     │     Returns UploadSummary                         │
     │──────────────────────────────────────────────────►│
     │                                                   │
     │  9. SyncDatabase (client sends updated .db)       │
     │◄──────────────────────────────────────────────────│
     │                                                   │
     │  10. Server updates backup_jobs status=completed  │
     │      Applies retention policies to new entries    │
     │                                                   │
```

### Key Improvements over v2.0

| Aspect | v2.0 Behavior | v3.0 Behavior |
|--------|---------------|---------------|
| Deduplication check | Per-file during scan (2x in-memory DB lookups) | Single manifest exchange before transfer |
| Chunk size | 20 KB with per-chunk ACK | 64 KB with gRPC flow control (no manual ACK) |
| Transfer security | Plaintext TCP | mTLS + per-file AES-256-GCM encryption |
| Completion signal | Magic string `tergum_DONE_tergum` | gRPC UploadSummary response |
| Stop signal | Magic string `tergum_STOPPED_tergum` | gRPC StopBackup RPC with graceful drain |
| Error handling | `System.exit(5)` on failure | Structured error propagation, retry logic |

---

## 9. Restore Flow (Detailed)

```
┌─────────┐                                         ┌─────────┐
│ CLIENT  │                                         │ SERVER  │
└────┬────┘                                         └────┬────┘
     │                                                   │
     │  1. User searches (CLI / Web UI):                 │
     │     tergum restore --file budget.xlsx             │
     │                                                   │
     │  2. Query local DB for matching entries           │
     │     Display results with backup date, size, path  │
     │                                                   │
     │  3. User selects entry, confirms destination      │
     │                                                   │
     │  4. Download(blake3_hash, destination_path)        │
     │──────────────────────────────────────────────────►│
     │                  (gRPC :7401)                      │
     │                                                   │
     │                                    5. Server locates file by hash
     │                                       in content-addressable store
     │                                                   │
     │  6. Stream FileChunks (encrypted)                 │
     │◄──────────────────────────────────────────────────│
     │                                                   │
     │  7. Client decrypts chunks with DEK from header   │
     │     Verifies BLAKE3 hash matches                  │
     │     Writes to destination path                    │
     │     Restores file metadata:                       │
     │       - Owner / group                             │
     │       - Permissions (mode bits)                   │
     │       - Timestamps (create/modify/access)         │
     │       - Hidden attribute (Windows)                │
     │       - Symlink recreation                        │
     │                                                   │
     │  8. Record in restore_history table               │
     │                                                   │
```

### Batch Restore

```bash
# Restore an entire folder structure
tergum restore --folder Documents --dest ~/Recovered/

# Restore all files from a specific backup job
tergum restore --backup-id 20260619143022 --dest /tmp/restore/

# Restore by pattern with date filter
tergum restore --file "*.xlsx" --after 2026-01-01 --dest ~/Spreadsheets/
```

Batch restores use parallel gRPC streams (configurable concurrency, default 4) for throughput.

---

## 10. File Watcher

v2.0 used Java NIO `WatchService` to monitor a single directory. v3.0 uses [fsnotify](https://github.com/fsnotify/fsnotify) with debouncing and multi-folder support.

### Architecture

```
┌───────────────────────────────────────────────────────────────────────┐
│  File Watcher Subsystem                                                │
├───────────────────────────────────────────────────────────────────────┤
│                                                                       │
│  ┌─────────────┐     ┌──────────────┐     ┌───────────────┐          │
│  │  fsnotify   │────►│  Debouncer   │────►│  Stability    │          │
│  │  (inotify/  │     │  (500ms      │     │  Gate (60s)   │          │
│  │   kqueue/   │     │   window)    │     │               │          │
│  │   ReadDir)  │     └──────────────┘     └───────┬───────┘          │
│  └─────────────┘                                  │                  │
│                                                   ▼                  │
│                                     ┌──────────────────────┐         │
│                                     │  File still exists?  │         │
│                                     │  Hash unchanged?     │         │
│                                     └──────────┬───────────┘         │
│                                                │                     │
│                                          YES   │   NO → discard      │
│                                                ▼                     │
│                                     ┌──────────────────────┐         │
│                                     │  Incremental Backup  │         │
│                                     │  (batched upload)    │         │
│                                     └──────────────────────┘         │
│                                                                       │
└───────────────────────────────────────────────────────────────────────┘
```

### Behavior

1. **Multi-folder** — Watch all paths defined in `include_paths` configuration.
2. **Debouncing** — File events are batched in a 500ms sliding window (prevents rapid re-triggers from editors that write-rename-delete).
3. **Stability gate** — After debounce, each file enters a stability hold (default 60s). When the timer expires, the watcher checks: does the file still exist? Has its content changed again since the event? Only files that pass both checks proceed to backup. This filters out temp files, partial downloads, build artifacts, and anything transient.
4. **Exclude-aware** — Events for excluded paths are immediately discarded before debouncing.
5. **Auto-backup** — Stable files are queued for incremental backup (hash + upload only the changed ones).
6. **Persistent state** — Watch registrations stored in `watched_paths` table; survive restarts.

### Stability Gate — How It Works

```
Time ─────────────────────────────────────────────────────────────►

Event: file.txt MODIFIED
  │
  ├── Debounce window (500ms) ──┐
  │                             │
  │   (more events for same     │
  │    file collapse here)      │
  │                             ▼
  │                    Debounce closes
  │                             │
  │                    Start 60s stability timer
  │                             │
  │                             ▼  (60 seconds later)
  │                    ┌─────────────────────┐
  │                    │ file.txt exists?    │── NO ──► discard (was temp)
  │                    └────────┬────────────┘
  │                             │ YES
  │                             ▼
  │                    ┌─────────────────────┐
  │                    │ Modified again      │── YES ─► reset timer (file still changing)
  │                    │ since event?        │
  │                    └────────┬────────────┘
  │                             │ NO
  │                             ▼
  │                    Queue for backup ✓
```

**What this catches:**

| Scenario | Without stability gate | With stability gate |
|----------|----------------------|---------------------|
| Editor saves temp file, renames over target | Backs up temp file | Only backs up final file |
| `npm install` creates thousands of files | Backs up every intermediate state | Backs up final state once settled |
| Browser downloads `file.part` → `file.zip` | Backs up partial download | Only backs up completed download |
| Build creates `.o` files then deletes them | Wastes bandwidth on build artifacts | Discards — files don't survive 60s |
| Compiler writes output, immediately stable | Waits 60s then backs up | Same — backs up after 60s |

If a file is modified again during the stability window, the timer resets. This means a file being continuously written (like an active log) won't trigger backup until it goes quiet for the full stability duration.

### CLI

```bash
tergum watch start                   # Start watching all configured include paths
tergum watch start --path ~/Documents --path ~/Projects
tergum watch stop                    # Stop all watchers
tergum watch status                  # Show active watchers and event counts
tergum watch add --path /data/important
tergum watch remove --path /tmp/scratch
```

### Watcher Configuration (in `tergum.toml`)

```toml
[watcher]
enabled = true
debounce_ms = 500
stability_seconds = 60     # File must exist unchanged for this long before backup
ongoing_backup = true      # Automatically back up stable files (continuous protection)
batch_size = 100           # Max files per incremental backup batch
auto_backup = true         # Automatically trigger backup on changes
cooldown_seconds = 300     # Minimum time between scheduled auto-backups (0 for ongoing)
reset_on_modify = true     # Reset stability timer if file changes again
```

### Ongoing Backup Mode

Traditional backups (AUTO/FULL) are triggered by a schedule or manual command. **Ongoing backup** is a continuous protection mode — the watcher and backup engine work together as a single always-on process. Files that pass the stability gate are automatically backed up to the server without any explicit trigger.

```
┌─────────────────────────────────────────────────────────────────────┐
│  Backup Modes                                                        │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  SCHEDULED (AUTO/FULL)         │  ONGOING (continuous)              │
│  ─────────────────────         │  ────────────────────              │
│  Cron or manual trigger        │  Always running                    │
│  Scans entire file tree        │  Never scans — reacts to events   │
│  Transfers all new/changed     │  Transfers only stable changes    │
│  Runs periodically             │  Backs up within seconds of change │
│  Best for: full coverage       │  Best for: active work protection │
│                                │                                    │
└─────────────────────────────────────────────────────────────────────┘
```

**How it works:**

1. Watcher detects file change → debounce → stability gate (60s)
2. File survives stability check (still exists, no further modifications)
3. Ongoing backup engine computes BLAKE3 hash
4. If hash is new or different from last known → upload to server immediately
5. Metadata recorded in `backups` table with `initiated_by = 'watcher'`

**This means:** if you save a document, within ~60 seconds it's protected on the server. No waiting for the next scheduled backup. No manual trigger needed.

#### Ongoing Backup vs Scheduled Backup

| Aspect | Scheduled (AUTO) | Ongoing |
|--------|-------------------|---------|
| Trigger | Cron / manual / API | Automatic (file change event) |
| Scope | All include paths (full scan) | Only changed files |
| Latency | Minutes to hours | ~60 seconds after last modification |
| CPU usage | Spike during scan | Minimal (event-driven) |
| Network usage | Burst | Trickle (one file at a time) |
| Coverage guarantee | Complete (catches everything) | Only watched paths with events |
| Best used | Nightly/weekly full sweep | Real-time protection during work |

#### Recommended Setup

Use **both** together:

```toml
[watcher]
enabled = true
ongoing_backup = true       # Enable continuous backup from watcher events
stability_seconds = 60
cooldown_seconds = 0        # No cooldown for ongoing mode (each file independent)

[scheduler]
# Nightly full backup catches anything the watcher might miss
# (files modified before watcher started, permission-only changes, etc.)
full_backup_cron = "0 2 * * 0"    # Weekly FULL at 2 AM Sunday
auto_backup_cron = "0 3 * * *"    # Daily AUTO at 3 AM
```

With this combination:
- Active files are protected within a minute of saving
- The nightly/weekly scheduled backup acts as a safety net for completeness

#### CLI

```bash
tergum backup --ongoing                  # Start ongoing backup (foreground)
tergum backup --ongoing --daemon         # Start as background service
tergum backup --ongoing --status         # Check if ongoing backup is active
tergum backup --ongoing --stop           # Stop ongoing backup
```

#### Backup Job Tracking

Ongoing backup batches file uploads into logical jobs every 5 minutes (configurable). This groups watcher-triggered backups into manageable units in the `backup_jobs` table:

```
backup_id: 20260619143500
level: AUTO
initiated_by: watcher
status: completed
file_count: 7
bytes_new: 245760
```

This keeps the job history readable rather than creating a separate job per file.

---

## 11. CLI Commands

The single `tergum` binary exposes all functionality through subcommands:

```
tergum <command> [subcommand] [flags]
```

### Command Reference

| Command | Description |
|---------|-------------|
| `tergum setup` | Interactive first-time configuration wizard |
| `tergum server` | Start the server (gRPC listeners + web UI) |
| `tergum backup` | Trigger a backup |
| `tergum restore` | Restore files |
| `tergum delete` | Delete backup sets, folders, or files from backups |
| `tergum list` | Browse and search backed-up files |
| `tergum stop` | Stop a running backup |
| `tergum watch` | File system watcher management |
| `tergum retention` | Retention policy management |
| `tergum status` | Show node status and connectivity |
| `tergum migrate` | Import v2.0 database and storage |
| `tergum version` | Show version and build info |

### Detailed Usage

```bash
# ─── Setup ──────────────────────────────────────────────────────────
tergum setup                          # Interactive wizard
tergum setup --role server --storage /mnt/backups --generate-certs
tergum setup --role client --server 192.168.1.5:7400

# ─── Server ─────────────────────────────────────────────────────────
tergum server                         # Start all services (foreground)
tergum server --daemon                # Start as background daemon
tergum server --no-webui              # Disable web UI

# ─── Backup ─────────────────────────────────────────────────────────
tergum backup --level auto --client workstation1
tergum backup --level full --client 192.168.1.10
tergum backup --level auto --all-clients    # Backup all registered clients
tergum backup --ongoing                     # Start continuous watcher-driven backup
tergum backup --ongoing --daemon            # Run ongoing backup as background service

# ─── Stop ────────────────────────────────────────────────────────────
tergum stop --client workstation1
tergum stop --all                     # Stop all running backups

# ─── Restore ────────────────────────────────────────────────────────
tergum restore --file budget.xlsx
tergum restore --file "*.docx" --after 2026-01-01 --dest ~/Recovered
tergum restore --folder Documents --dest /tmp/restore
tergum restore --backup-id 20260619143022 --dest /tmp/full-restore
tergum restore --interactive          # TUI-based file browser

# ─── List / Search ──────────────────────────────────────────────────
tergum list --all --limit 100
tergum list --file report
tergum list --ext pdf
tergum list --folder Documents
tergum list --stats                   # Summary statistics
tergum list --db /path/to/other.db    # Query external database

# ─── Watch ───────────────────────────────────────────────────────────
tergum watch start
tergum watch status
tergum watch add --path ~/NewProject

# ─── Retention ───────────────────────────────────────────────────────
tergum retention list
tergum retention add --name cleanup --days 14 --pattern "*.tmp"
tergum retention run --dry-run
tergum retention run

# ─── Delete ──────────────────────────────────────────────────────────
tergum delete --backup-id 20260619143022              # Delete entire backup set
tergum delete --backup-id 20260619143022 --folder Documents/old-project
tergum delete --backup-id 20260619143022 --file secret.pdf
tergum delete --file secret.pdf --all-backups         # Remove from every backup
tergum delete --backup-id 20260619143022 --interactive # Browse and select
tergum delete --backup-id 20260619143022 --dry-run    # Preview only

# ─── Status ──────────────────────────────────────────────────────────
tergum status                         # Node info, connectivity, last backup
tergum status --json                  # Machine-readable output

# ─── Migration ───────────────────────────────────────────────────────
tergum migrate --from-db ~/.tergum/db/hostname.db --rehash
tergum migrate --from-storage /mnt/backups/tergum --verify
```

### Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | General error |
| 2 | Configuration error |
| 3 | Connection error (server unreachable) |
| 4 | Authentication error (certificate rejected) |
| 5 | Storage error (disk full, permission denied) |
| 10 | Backup stopped by user |
| 11 | Backup failed (partial transfer) |

---

## 12. Embedded Web UI

v2.0 used a Java Swing desktop GUI. v3.0 replaces it with an embedded web UI served from the server binary using Go's `embed` package — no external dependencies, no Node.js build step.

### Technology Stack

| Layer | Technology |
|-------|-----------|
| Server | Go `net/http` + `html/template` |
| Frontend | HTMX + Alpine.js + Tailwind CSS (CDN-free, embedded) |
| Real-time | Server-Sent Events (SSE) for live updates |
| Assets | Embedded via `//go:embed` directive |

### Pages

| Route | Description |
|-------|-------------|
| `/` | Dashboard — file count, last backup, storage usage, active jobs |
| `/backups` | Backup management — trigger, view history, delete sets |
| `/restore` | File browser — search, select, restore with progress |
| `/config` | Configuration editor — paths, ports, includes/excludes |
| `/retention` | Retention policy management |
| `/watchers` | File watcher status and management |
| `/activity` | Live activity log (SSE-powered auto-scroll) |
| `/clients` | Registered clients and their status |
| `/metrics` | Visual metrics (storage growth, backup durations) |

### Dashboard Widgets

```
┌──────────────────────────────────────────────────────────┐
│  TERGUM DASHBOARD                              [v3.0.1]  │
├──────────────┬──────────────┬──────────────┬─────────────┤
│  Files       │  Storage     │  Last Backup │  Clients    │
│  142,847     │  48.3 GB     │  2 hours ago │  3 online   │
├──────────────┴──────────────┴──────────────┴─────────────┤
│                                                          │
│  Active Jobs                                             │
│  ┌─────────────────────────────────────────────────────┐ │
│  │ workstation1 │ AUTO │ 67% │ 2,341 files │ running  │ │
│  └─────────────────────────────────────────────────────┘ │
│                                                          │
│  Recent Activity (live)                                   │
│  ┌─────────────────────────────────────────────────────┐ │
│  │ 14:30:22 │ Backup completed: workstation2 (1,204)   │ │
│  │ 14:28:01 │ Retention: removed 23 expired files      │ │
│  │ 14:15:44 │ Watcher: 12 files changed in ~/Projects  │ │
│  └─────────────────────────────────────────────────────┘ │
│                                                          │
│  Storage Trend (7 days)                                   │
│  ████████████████████████░░░░░░  48.3 / 100 GB           │
│                                                          │
└──────────────────────────────────────────────────────────┘
```

### Authentication

The web UI uses HTTP Basic Auth over HTTPS with credentials set during `tergum setup`:

```toml
[webui]
enabled = true
listen = "0.0.0.0:7480"
username = "admin"
password_hash = "$argon2id$..."   # Argon2id hash
session_timeout = "24h"
```

---

## 13. Configuration

v2.0 stored all configuration in a SQLite `CONFIG` table. v3.0 uses a TOML configuration file for static settings and SQLite for runtime state.

### Configuration File: `tergum.toml`

```toml
# Tergum v3.0 Configuration
# Location: ~/.config/tergum/tergum.toml (Linux)
#            ~/Library/Application Support/tergum/tergum.toml (macOS)
#            %APPDATA%\tergum\tergum.toml (Windows)

[node]
role = "both"                    # "client", "server", "both"
hostname = "workstation1"        # Override auto-detected hostname

[server]
grpc_command_port = 7400
grpc_data_port = 7401
storage_path = "/mnt/backups/tergum"
max_concurrent_backups = 4
max_concurrent_restores = 8

[client]
server_address = "192.168.1.5"
server_command_port = 7400
server_data_port = 7401

[tls]
ca_cert = "certs/ca.crt"
server_cert = "certs/server.crt"
server_key = "certs/server.key"
client_cert = "certs/client.crt"
client_key = "certs/client.key"

[encryption]
enabled = true
# Master key derived from passphrase during setup
# Wrapped key stored in: ~/.config/tergum/master.key (encrypted with OS keyring)

[database]
path = "tergum.db"              # Relative to config directory

[backup]
chunk_size = 65536              # 64 KB transfer chunks
include_paths = [
    "~/Documents",
    "~/Projects",
    "~/Pictures"
]
exclude_patterns = [
    "**/node_modules/**",
    "**/.git/objects/**",
    "**/*.class",
    "**/target/**",
    "**/__pycache__/**",
    "**/.DS_Store",
    "**/Thumbs.db"
]
max_file_size = "10GB"          # Skip files larger than this

[watcher]
enabled = true
debounce_ms = 500
stability_seconds = 60
ongoing_backup = true
batch_size = 100
auto_backup = true
cooldown_seconds = 300
reset_on_modify = true

[webui]
enabled = true
listen = "0.0.0.0:7480"
username = "admin"
password_hash = ""              # Set during tergum setup
session_timeout = "24h"

[metrics]
enabled = true
listen = "0.0.0.0:7490"

[logging]
level = "info"                  # debug, info, warn, error
format = "text"                 # text, json
file = "tergum.log"            # Relative to config directory
max_size_mb = 100
max_backups = 5
```

### Configuration Directory Layout

```
~/.config/tergum/              # Linux (XDG_CONFIG_HOME)
├── tergum.toml                # Main configuration
├── tergum.db                  # SQLite database
├── tergum.log                 # Application log
├── master.key                 # Encrypted master key
├── certs/
│   ├── ca.crt                 # Certificate Authority
│   ├── ca.key                 # CA private key (server only)
│   ├── server.crt             # Server certificate
│   ├── server.key             # Server private key
│   ├── client.crt             # Client certificate
│   └── client.key             # Client private key
└── include.txt                # Optional: one path per line (alternative to TOML array)
```

### Platform Paths

| OS | Config Directory | Storage Default |
|----|-----------------|-----------------|
| Linux | `~/.config/tergum/` | `/var/lib/tergum/storage/` |
| macOS | `~/Library/Application Support/tergum/` | `~/Library/Application Support/tergum/storage/` |
| Windows | `%APPDATA%\tergum\` | `%APPDATA%\tergum\storage\` |

---

## 14. Package Structure (Go)

```
tergum/
├── cmd/
│   └── tergum/
│       └── main.go              # Entry point, CLI root command
│
├── internal/
│   ├── backup/
│   │   ├── engine.go            # Backup orchestrator (replaces CLTRunBackup)
│   │   ├── scanner.go           # File tree scanner (replaces ScanFileTree)
│   │   ├── manifest.go          # Manifest builder and diff logic
│   │   └── dedup.go             # BLAKE3 deduplication logic
│   │
│   ├── restore/
│   │   ├── engine.go            # Restore orchestrator
│   │   ├── permissions.go       # File metadata restoration (replaces SetFilePermission)
│   │   └── selector.go          # Interactive file selection
│   │
│   ├── server/
│   │   ├── grpc.go              # gRPC server setup and handlers
│   │   ├── command_service.go   # CommandService implementation
│   │   ├── data_service.go      # DataService implementation
│   │   └── scheduler.go         # Cron-based backup scheduling
│   │
│   ├── client/
│   │   ├── grpc.go              # gRPC client connections
│   │   ├── uploader.go          # Stream upload logic (replaces CLTDataSender)
│   │   └── downloader.go        # Stream download logic
│   │
│   ├── storage/
│   │   ├── cas.go               # Content-addressable storage engine
│   │   ├── writer.go            # Encrypted file writer
│   │   └── reader.go            # Encrypted file reader
│   │
│   ├── crypto/
│   │   ├── tls.go               # Certificate generation and loading
│   │   ├── aes.go               # AES-256-GCM encrypt/decrypt
│   │   ├── keys.go              # Key derivation (Argon2id) and wrapping
│   │   └── blake3.go            # BLAKE3 hashing utilities
│   │
│   ├── database/
│   │   ├── sqlite.go            # Database connection and migrations
│   │   ├── backups.go           # Backup queries
│   │   ├── jobs.go              # Job tracking queries
│   │   ├── config.go            # Config table operations
│   │   └── migrate_v2.go        # v2.0 schema migration
│   │
│   ├── retention/
│   │   ├── engine.go            # Retention policy engine
│   │   ├── policy.go            # Policy evaluation logic
│   │   └── cleanup.go           # File and DB cleanup
│   │
│   ├── watcher/
│   │   ├── watcher.go           # fsnotify wrapper with debouncing
│   │   └── queue.go             # Change event queue
│   │
│   ├── webui/
│   │   ├── server.go            # HTTP server and routes
│   │   ├── handlers.go          # Page handlers
│   │   ├── sse.go               # Server-Sent Events for live updates
│   │   ├── auth.go              # Basic auth middleware
│   │   └── static/              # Embedded assets (HTML, CSS, JS)
│   │       ├── templates/
│   │       └── assets/
│   │
│   ├── config/
│   │   ├── config.go            # TOML config parsing and defaults
│   │   └── paths.go             # Platform-specific path resolution
│   │
│   ├── monitor/
│   │   ├── logger.go            # Structured logging (replaces addToMonitor)
│   │   └── metrics.go           # Prometheus metrics
│   │
│   └── platform/
│       ├── permissions_unix.go   # Unix file permission handling
│       ├── permissions_windows.go# Windows ACL handling
│       └── symlink.go           # Cross-platform symlink support
│
├── proto/
│   └── v3/
│       ├── command.proto         # CommandService definition
│       ├── data.proto            # DataService definition
│       └── gen/                  # Generated Go code (protoc output)
│
├── go.mod
├── go.sum
├── Makefile
├── Dockerfile
└── README.md
```

### Mapping: Java Classes → Go Packages

| Java Class (v2.0) | Go Package (v3.0) | Notes |
|-------------------|-------------------|-------|
| `TGMprop` | `internal/config` | TOML config + platform paths |
| `CLTRunBackup` | `internal/backup/engine` | Backup orchestrator |
| `ScanFileTree` | `internal/backup/scanner` | File tree walker |
| `CLTDataSender` | `internal/client/uploader` | gRPC stream upload |
| `CLTDBSend` | `internal/client/uploader` | Database sync via gRPC |
| `TCPDataComm` | `internal/server/command_service` | gRPC CommandService |
| `TCPDataReceiver` | `internal/server/data_service` | gRPC DataService.Upload |
| `TCPRestore` | `internal/client/downloader` | gRPC DataService.Download |
| `SRVDataSender` | `internal/server/data_service` | gRPC Download stream |
| `CHKConfig` | `internal/config` | Config validation |
| `initiateMemDB` | `internal/database/sqlite` | Direct SQLite (no in-memory hack) |
| `SQLiteCreate` | `internal/database/sqlite` | Schema migrations |
| `getFilePermission` | `internal/backup/scanner` | Collected during scan |
| `SetFilePermission` | `internal/restore/permissions` | Applied during restore |
| `getExcludeList` | `internal/config` | Loaded from TOML |
| `getIncludesList` | `internal/config` | Loaded from TOML |
| `addToMonitor` | `internal/monitor/logger` | Structured slog logging |
| `ExecAdmin` (Swing) | `internal/webui` | Embedded web UI |
| `run_watcher` | `internal/watcher` | fsnotify-based |
| `getFileMD5` | `internal/crypto/blake3` | BLAKE3 replaces MD5 |

---

## 15. Build Strategy

### Makefile Targets

```makefile
VERSION := $(shell git describe --tags --always --dirty)
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.buildDate=$(shell date -u +%Y-%m-%dT%H:%M:%SZ)

.PHONY: all build proto test lint clean release

all: proto build

proto:
	protoc --go_out=. --go-grpc_out=. proto/v3/*.proto

build:
	CGO_ENABLED=1 go build -ldflags="$(LDFLAGS)" -o bin/tergum ./cmd/tergum

test:
	go test -race -cover ./...

lint:
	golangci-lint run ./...

clean:
	rm -rf bin/ proto/v3/gen/

# Cross-compilation (CGO required for SQLite)
release:
	GOOS=linux   GOARCH=amd64 CGO_ENABLED=1 go build -ldflags="$(LDFLAGS)" -o dist/tergum-linux-amd64 ./cmd/tergum
	GOOS=linux   GOARCH=arm64 CGO_ENABLED=1 CC=aarch64-linux-gnu-gcc go build -ldflags="$(LDFLAGS)" -o dist/tergum-linux-arm64 ./cmd/tergum
	GOOS=darwin  GOARCH=amd64 CGO_ENABLED=1 go build -ldflags="$(LDFLAGS)" -o dist/tergum-darwin-amd64 ./cmd/tergum
	GOOS=darwin  GOARCH=arm64 CGO_ENABLED=1 go build -ldflags="$(LDFLAGS)" -o dist/tergum-darwin-arm64 ./cmd/tergum
	GOOS=windows GOARCH=amd64 CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc go build -ldflags="$(LDFLAGS)" -o dist/tergum-windows-amd64.exe ./cmd/tergum
```

### Dependencies

| Module | Purpose |
|--------|---------|
| `google.golang.org/grpc` | gRPC framework |
| `google.golang.org/protobuf` | Protobuf runtime |
| `github.com/mattn/go-sqlite3` | SQLite driver (CGO) |
| `github.com/zeebo/blake3` | BLAKE3 hashing |
| `github.com/fsnotify/fsnotify` | Cross-platform file watching |
| `github.com/spf13/cobra` | CLI framework |
| `github.com/BurntSushi/toml` | TOML configuration |
| `github.com/prometheus/client_golang` | Metrics |
| `golang.org/x/crypto` | Argon2id key derivation |
| `log/slog` (stdlib) | Structured logging |

### Docker

```dockerfile
FROM golang:1.22-alpine AS builder
RUN apk add --no-cache gcc musl-dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 go build -ldflags="-s -w" -o /tergum ./cmd/tergum

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=builder /tergum /usr/local/bin/tergum
EXPOSE 7400 7401 7480 7490
ENTRYPOINT ["tergum"]
CMD ["server"]
```

---

## 16. Migration from v2.0

### Migration Command

```bash
tergum migrate --from-db ~/.tergum/db/hostname.db --rehash --verify
```

### Migration Steps

```
┌───────────────────────────────────────────────────────────────┐
│  Migration Process                                            │
├───────────────────────────────────────────────────────────────┤
│                                                               │
│  1. Read v2.0 SQLite database                                 │
│     - Parse BACKUPS, CONFIG, BKPINDEX, BKPSERVER tables       │
│                                                               │
│  2. Transform schema                                          │
│     - BACKUPS → backups (normalize columns, add new fields)   │
│     - CONFIG → tergum.toml (extract to file)                  │
│     - BKPINDEX → backup_jobs (restructure)                    │
│     - BKPSERVER → backup_jobs (merge client tracking)         │
│                                                               │
│  3. Rehash files (--rehash flag)                              │
│     - Walk storage directory                                  │
│     - Compute BLAKE3 for each file (stored by MD5 name)       │
│     - Rename file: {md5} → {blake3}                           │
│     - Update database references                              │
│     - Keep MD5 → BLAKE3 mapping for verification              │
│                                                               │
│  4. Encrypt storage (if encryption enabled)                   │
│     - Generate DEK per file                                   │
│     - Encrypt in-place with AES-256-GCM                       │
│     - Store wrapped DEK in database                           │
│                                                               │
│  5. Verify (--verify flag)                                    │
│     - Confirm all files in DB exist on disk                   │
│     - Confirm all files on disk have DB entries               │
│     - Verify BLAKE3 hashes match                              │
│     - Report orphaned files                                   │
│                                                               │
│  6. Generate certificates                                     │
│     - Create CA, server, and client certificates              │
│     - Write to config directory                               │
│                                                               │
│  7. Write tergum.toml                                         │
│     - Port mappings (preserve custom ports if set)            │
│     - Server name                                             │
│     - Storage path                                            │
│     - Include/exclude lists from saveFile/skipFile.tergum     │
│                                                               │
└───────────────────────────────────────────────────────────────┘
```

### Schema Mapping

| v2.0 Column | v3.0 Column | Transformation |
|-------------|-------------|----------------|
| `BACKUP_ID` (INT) | `backup_id` (TEXT) | String representation preserved |
| `MD5` | `blake3_hash` | Rehashed from file content |
| `BKP_DATE` | `backup_date` | Format normalized to ISO 8601 |
| `FILE_NAME` | `file_name` | Direct copy |
| `FILE_PATH` | `file_path` | Direct copy |
| `FILE_SIZE` | `file_size` | Direct copy |
| `FILE_CREATE` | `created_at` | Already Unix ms |
| `FILE_ACCESS` | `accessed_at` | Already Unix ms |
| `FILE_MODIFY` | `modified_at` | Already Unix ms |
| `FILE_READ/WRITE/EXEC` | `permissions` | Combined into Unix mode bits |
| `FILE_HIDDEN` | `hidden` | Boolean normalization |
| `FILE_PERM` | `permissions` | Parsed to numeric |
| `FILE_SYM` | `symlink_target` | Direct copy |
| `FILE_OWNER` | `owner` | Direct copy |
| `FILE_GROUP` | `file_group` | Direct copy |
| `FILE_OS` | `os` | Direct copy |
| `FILE_EXT` | `file_ext` | Direct copy |
| `REC_COUNT`/`REC_DATE`/`REC_PATH` | `restore_history` table | Normalized to separate table |
| `BKP_STATUS` (BKPSERVER) | `status` (backup_jobs) | I→running, R→running, D→completed, F→failed, S→stopped, P→stopped |

### Backward Compatibility

During migration, the v2.0 database is **not modified** — a new v3.0 database is created alongside it. The original storage files are renamed (not copied) during rehashing to save disk space. A rollback script is generated that can undo the rename operation if needed.

---

## 17. Observability

### Structured Logging

All log output uses Go's `slog` package with consistent fields:

```json
{
  "time": "2026-06-19T14:30:22Z",
  "level": "INFO",
  "msg": "backup completed",
  "component": "backup.engine",
  "client": "workstation1",
  "backup_id": "20260619143022",
  "files": 2341,
  "bytes": 1073741824,
  "duration_ms": 45200,
  "deduped": 18420
}
```

### Prometheus Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `tergum_backup_files_total` | Counter | Total files backed up |
| `tergum_backup_bytes_total` | Counter | Total bytes transferred |
| `tergum_backup_duration_seconds` | Histogram | Backup job duration |
| `tergum_backup_dedup_ratio` | Gauge | Current deduplication ratio |
| `tergum_restore_files_total` | Counter | Total files restored |
| `tergum_storage_bytes_used` | Gauge | Current storage usage |
| `tergum_storage_files_count` | Gauge | Files in storage |
| `tergum_retention_deleted_total` | Counter | Files removed by retention |
| `tergum_grpc_requests_total` | Counter | gRPC requests by method |
| `tergum_grpc_errors_total` | Counter | gRPC errors by method |
| `tergum_watcher_events_total` | Counter | File watcher events |
| `tergum_clients_connected` | Gauge | Currently connected clients |

### Health Endpoint

```
GET /health → {"status": "healthy", "version": "3.0.1", "uptime": "4d 2h 15m"}
```

---

## 18. Security Model

| Layer | Mechanism | Purpose |
|-------|-----------|---------|
| Transport | mTLS (Ed25519) | Identity verification + encryption in transit |
| Storage | AES-256-GCM | Confidentiality at rest |
| Key derivation | Argon2id | Passphrase → master key |
| Hashing | BLAKE3 | Content integrity + deduplication |
| Web UI | HTTPS + Argon2id password hash | Admin authentication |
| API | Client certificate required | No anonymous access |

### Threat Mitigation

| Threat | Mitigation |
|--------|-----------|
| Man-in-the-middle | Mutual TLS with pinned CA |
| Stolen backup disk | AES-256-GCM encryption at rest |
| Hash collision (dedup bypass) | BLAKE3 (256-bit, collision-resistant) |
| Brute-force web login | Argon2id + rate limiting |
| Unauthorized backup trigger | Client certificate validation |
| Replay attacks | gRPC deadline + nonce in file headers |

---

## 19. Future Considerations (Post-v3.0)

These are explicitly **out of scope** for the initial v3.0 release but inform architectural decisions:

- **Multi-server replication** — Storage can be replicated between servers (architecture supports it via gRPC)
- **S3-compatible backend** — Pluggable storage interface allows future cloud backends
- **Differential/incremental at block level** — Current design operates at file level; block-level dedup possible later
- **Agent mode** — Client runs as a system service with scheduled self-backup (no server trigger needed)
- **End-to-end encryption** — Client holds the only decryption key (server never sees plaintext)
- **Web UI SSO** — OIDC/SAML integration for enterprise environments

---

## 20. Summary: v2.0 → v3.0 at a Glance

| Dimension | v2.0 (Java) | v3.0 (Go) |
|-----------|-------------|-----------|
| Language | Java 21 | Go 1.22+ |
| Binary | JAR + JRE + classpath | Single static binary |
| Protocol | Custom TCP framing | gRPC + Protobuf |
| Ports | 3 (4366, 4355, 4344) | 2 (7400, 7401) + Web (7480) |
| Security | None (plaintext) | mTLS + AES-256-GCM |
| Hashing | MD5 | BLAKE3 |
| Config | SQLite table | TOML file |
| UI | Java Swing | Embedded web (HTMX) |
| Dedup strategy | Per-file in-memory DB check | Manifest exchange (single round-trip) |
| Retention | Manual only | Policy-based automatic |
| Observability | Log files | Structured logs + Prometheus |
| File watching | Single folder (NIO) | Multi-folder (fsnotify) + auto-backup |
| Restore | Sequential, one file at a time | Parallel streams |
| Error handling | `System.exit()` | Structured errors, retries, graceful shutdown |
| Cross-compile | N/A (JVM) | `GOOS/GOARCH` native binaries |
| Container | Not supported | Dockerfile included |
