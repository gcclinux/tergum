// Package proto contains the gRPC service definitions and message types
// for Tergum v3 CommandService and DataService protocols.
//
// These types mirror what protoc-gen-go would generate from the .proto files
// in proto/v3/. They use the google.golang.org/grpc interfaces directly.
package proto

// BackupLevel represents the type of backup operation.
type BackupLevel int32

const (
	BackupLevel_AUTO    BackupLevel = 0
	BackupLevel_FULL    BackupLevel = 1
	BackupLevel_ONGOING BackupLevel = 2
)

// BackupRequest is sent to trigger a backup operation.
type BackupRequest struct {
	Level       BackupLevel `json:"level"`
	ClientId    string      `json:"client_id"`
	InitiatedBy string      `json:"initiated_by"`
}

// BackupResponse is returned after a backup is triggered.
type BackupResponse struct {
	BackupId string `json:"backup_id"`
	Status   string `json:"status"`
	Message  string `json:"message"`
}

// StopRequest is sent to stop an in-progress backup.
type StopRequest struct {
	BackupId string `json:"backup_id"`
	ClientId string `json:"client_id"`
}

// StopResponse is returned after a stop attempt.
type StopResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// StatusRequest queries the status of current operations.
type StatusRequest struct {
	ClientId string `json:"client_id"`
}

// StatusResponse contains the current operation status.
type StatusResponse struct {
	Status           string `json:"status"`
	BackupId         string `json:"backup_id"`
	FilesProcessed   int64  `json:"files_processed"`
	BytesTransferred int64  `json:"bytes_transferred"`
	StartedAt        string `json:"started_at"`
	Message          string `json:"message"`
	WatcherActive    bool   `json:"watcher_active"`
}

// PingRequest is a health check request that also carries client state for
// bidirectional sync. The server uses these fields to keep the registry
// consistent without requiring separate status queries.
type PingRequest struct {
	WatcherActive bool   `json:"watcher_active,omitempty"` // true if the client's file watcher is running
	LastBackupAt  string `json:"last_backup_at,omitempty"` // RFC3339 timestamp of most recent completed backup
	BackupActive  bool   `json:"backup_active,omitempty"`  // true if a backup is currently in progress
}

// PingResponse contains server health information.
type PingResponse struct {
	Version        string `json:"version"`
	Commit         string `json:"commit,omitempty"`
	BuildDate      string `json:"build_date,omitempty"`
	Uptime         string `json:"uptime"`
	ClientDisabled bool   `json:"client_disabled,omitempty"` // true when the server has disabled this client
}

// ListBackupsRequest queries backup history.
type ListBackupsRequest struct {
	ClientId string `json:"client_id"`
	Limit    int32  `json:"limit"`
	Offset   int32  `json:"offset"`
}

// BackupJobInfo contains information about a single backup job.
type BackupJobInfo struct {
	BackupId     string `json:"backup_id"`
	Level        string `json:"level"`
	ClientId     string `json:"client_id"`
	InitiatedBy  string `json:"initiated_by"`
	StartedAt    string `json:"started_at"`
	FinishedAt   string `json:"finished_at"`
	Status       string `json:"status"`
	FileCount    int64  `json:"file_count"`
	BytesTotal   int64  `json:"bytes_total"`
	BytesNew     int64  `json:"bytes_new"`
	FilesDeduped int64  `json:"files_deduped"`
	ErrorMessage string `json:"error_message"`
}

// ListBackupsResponse contains a list of backup jobs.
type ListBackupsResponse struct {
	Backups []*BackupJobInfo `json:"backups"`
	Total   int32            `json:"total"`
}

// DeleteRequest is sent to delete backup entries.
type DeleteRequest struct {
	BackupId   string `json:"backup_id"`
	FilePath   string `json:"file_path"`
	FolderPath string `json:"folder_path"`
	AllBackups bool   `json:"all_backups"`
	DryRun     bool   `json:"dry_run"`
}

// DeleteResponse is returned after a delete operation.
type DeleteResponse struct {
	Success        bool   `json:"success"`
	EntriesDeleted int64  `json:"entries_deleted"`
	BytesFreed     int64  `json:"bytes_freed"`
	Message        string `json:"message"`
}

// RetentionRequest queries retention policies.
type RetentionRequest struct {
	ClientId string `json:"client_id"`
}

// RetentionPolicyProto represents a retention policy in the protocol.
type RetentionPolicyProto struct {
	Name         string `json:"name"`
	KeepDays     int32  `json:"keep_days"`
	KeepVersions int32  `json:"keep_versions"`
	Pattern      string `json:"pattern"`
	Priority     int32  `json:"priority"`
	Enabled      bool   `json:"enabled"`
}

// RetentionResponse contains a list of retention policies.
type RetentionResponse struct {
	Policies []*RetentionPolicyProto `json:"policies"`
}

// WatcherRequest is sent to start or stop the file watcher on a client node.
type WatcherRequest struct {
	ClientId string `json:"client_id"`
}

// WatcherResponse is returned after a watcher start/stop operation.
type WatcherResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// RegisterRequest is sent by a client to register itself with the server.
type RegisterRequest struct {
	ClientId string `json:"client_id"`
	Address  string `json:"address"`
}

// RegisterResponse is returned after a client registration attempt.
type RegisterResponse struct {
	Success       bool   `json:"success"`
	ServerVersion string `json:"server_version"`
}

// PushRestoreRequest is sent as the first message in a PushRestore stream
// to configure the restore destination on the target client.
type PushRestoreRequest struct {
	DestPath     string `json:"dest_path"`     // Base destination directory on the target client
	SourceClient string `json:"source_client"` // ID of the source client whose backup is being restored
	TotalFiles   int64  `json:"total_files"`   // Expected total number of files (for progress)
}

// PushRestoreResponse is returned after a PushRestore stream completes.
type PushRestoreResponse struct {
	Success       bool   `json:"success"`
	FilesReceived int64  `json:"files_received"`
	BytesTotal    int64  `json:"bytes_total"`
	FilesFailed   int64  `json:"files_failed"`
	Message       string `json:"message"`
}
