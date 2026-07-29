// Package model defines shared types used across Tergum components.
package model

import "time"

// BackupLevel represents the type of backup operation.
type BackupLevel int

const (
	// BackupLevelAuto selects files that are new or modified since the last backup.
	BackupLevelAuto BackupLevel = iota
	// BackupLevelFull includes all files matching include paths.
	BackupLevelFull
	// BackupLevelOngoing is used for watcher-triggered incremental backups.
	BackupLevelOngoing
)

// String returns the string representation of a BackupLevel.
func (l BackupLevel) String() string {
	switch l {
	case BackupLevelAuto:
		return "AUTO"
	case BackupLevelFull:
		return "FULL"
	case BackupLevelOngoing:
		return "ONGOING"
	default:
		return "UNKNOWN"
	}
}

// JobStatus represents the current state of a backup job.
type JobStatus string

const (
	JobRunning   JobStatus = "running"
	JobCompleted JobStatus = "completed"
	JobFailed    JobStatus = "failed"
	JobStopped   JobStatus = "stopped"
	JobExpired   JobStatus = "expired"
)

// BackupEntry represents a single file record in the backup database.
type BackupEntry struct {
	ID            int64
	BackupID      string
	Blake3Hash    string
	FileName      string
	FilePath      string
	FileExt       string
	FileSize      int64
	CreatedAt     *time.Time
	ModifiedAt    *time.Time
	AccessedAt    *time.Time
	Permissions   *uint32
	Owner         string
	FileGroup     string
	Hidden        bool
	Symlink       bool
	SymlinkTarget string
	OS            string
	EncryptedDEK  []byte
	Nonce         []byte
	BackupDate    time.Time
	ExpiresAt     *time.Time
}

// BackupJob represents a backup operation record.
type BackupJob struct {
	BackupID     string
	Level        string
	ClientID     string
	ClientIP     string
	InitiatedBy  string
	StartedAt    time.Time
	FinishedAt   *time.Time
	Status       JobStatus
	FileCount    int64
	BytesTotal   int64
	BytesNew     int64
	FilesDeduped int64
	ErrorMessage string
}

// RetentionPolicy defines rules for automatic backup expiration.
type RetentionPolicy struct {
	ID           int64
	Name         string
	KeepDays     *int
	KeepVersions int
	Pattern      string
	Priority     int
	Enabled      bool
	CreatedAt    time.Time
}

// ManifestEntry represents a file in the deduplication manifest.
type ManifestEntry struct {
	Blake3Hash string
	FilePath   string
	FileSize   int64
	ModifiedAt int64
}
