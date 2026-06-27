// Package proto — tunnel messages for NAT traversal.
//
// The CommandTunnel RPC allows clients behind NAT to receive commands from
// the server without requiring inbound connectivity. The client opens a
// bidirectional stream to the server; the server pushes commands down the
// stream and the client responds on the same stream.
package proto

// TunnelCommand is sent from the server to the client over the tunnel stream.
// Exactly one of the command fields will be set.
type TunnelCommand struct {
	// RequestId is a unique identifier for this request. The client MUST echo
	// it back in the corresponding TunnelResponse so the server can correlate.
	RequestId string `json:"request_id"`

	// Exactly one of the following will be set:
	PingRequest         *PingRequest    `json:"ping_request,omitempty"`
	TriggerBackup       *BackupRequest  `json:"trigger_backup,omitempty"`
	StopBackup          *StopRequest    `json:"stop_backup,omitempty"`
	GetStatus           *StatusRequest  `json:"get_status,omitempty"`
	StartWatcher        *WatcherRequest `json:"start_watcher,omitempty"`
	StopWatcher         *WatcherRequest `json:"stop_watcher,omitempty"`
	PushRestoreInitiate *PushRestoreInitiateRequest `json:"push_restore_initiate,omitempty"`
}

// TunnelResponse is sent from the client back to the server over the tunnel stream.
type TunnelResponse struct {
	// RequestId echoes the TunnelCommand.RequestId this is responding to.
	RequestId string `json:"request_id"`

	// Error is set if the command could not be executed.
	Error string `json:"error,omitempty"`

	// Exactly one of the following will be set (matching the original command):
	PingResponse    *PingResponse    `json:"ping_response,omitempty"`
	BackupResponse  *BackupResponse  `json:"backup_response,omitempty"`
	StopResponse    *StopResponse    `json:"stop_response,omitempty"`
	StatusResponse  *StatusResponse  `json:"status_response,omitempty"`
	WatcherResponse *WatcherResponse `json:"watcher_response,omitempty"`
	PushRestoreResponse *PushRestoreResponse `json:"push_restore_response,omitempty"`
}

// TunnelRegistration is the first message sent by the client when opening
// a tunnel. It identifies the client so the server can route commands.
type TunnelRegistration struct {
	ClientId string `json:"client_id"`
}

// PushRestoreInitiateRequest tells the client that the server wants to push
// files to it. The actual file data is sent as subsequent TunnelPushData messages.
type PushRestoreInitiateRequest struct {
	DestPath     string `json:"dest_path"`
	SourceClient string `json:"source_client"`
	TotalFiles   int64  `json:"total_files"`
}
