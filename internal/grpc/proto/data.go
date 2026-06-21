package proto

import "encoding/json"

// FileChunk represents a chunk of file data in the upload/download stream.
// The stream follows the pattern: FileHeader -> data chunks -> FileTrailer.
type FileChunk struct {
	// Payload is one of: *FileChunk_Header, *FileChunk_Data, *FileChunk_Trailer
	Payload isFileChunkPayload
}

// fileChunkJSON is the JSON wire format for FileChunk.
type fileChunkJSON struct {
	Header  *FileHeader  `json:"header,omitempty"`
	Data    []byte       `json:"data,omitempty"`
	Trailer *FileTrailer `json:"trailer,omitempty"`
}

// MarshalJSON implements json.Marshaler for FileChunk.
func (fc FileChunk) MarshalJSON() ([]byte, error) {
	var j fileChunkJSON
	switch p := fc.Payload.(type) {
	case *FileChunk_Header:
		j.Header = p.Header
	case *FileChunk_Data:
		j.Data = p.Data
	case *FileChunk_Trailer:
		j.Trailer = p.Trailer
	}
	return json.Marshal(j)
}

// UnmarshalJSON implements json.Unmarshaler for FileChunk.
func (fc *FileChunk) UnmarshalJSON(data []byte) error {
	var j fileChunkJSON
	if err := json.Unmarshal(data, &j); err != nil {
		return err
	}
	switch {
	case j.Header != nil:
		fc.Payload = &FileChunk_Header{Header: j.Header}
	case j.Trailer != nil:
		fc.Payload = &FileChunk_Trailer{Trailer: j.Trailer}
	case j.Data != nil:
		fc.Payload = &FileChunk_Data{Data: j.Data}
	}
	return nil
}

// GetHeader returns the FileHeader if this chunk contains one.
func (fc *FileChunk) GetHeader() *FileHeader {
	if p, ok := fc.Payload.(*FileChunk_Header); ok {
		return p.Header
	}
	return nil
}

// GetData returns the data bytes if this chunk contains data.
func (fc *FileChunk) GetData() []byte {
	if p, ok := fc.Payload.(*FileChunk_Data); ok {
		return p.Data
	}
	return nil
}

// GetTrailer returns the FileTrailer if this chunk contains one.
func (fc *FileChunk) GetTrailer() *FileTrailer {
	if p, ok := fc.Payload.(*FileChunk_Trailer); ok {
		return p.Trailer
	}
	return nil
}

// isFileChunkPayload is the interface for the FileChunk oneof field.
type isFileChunkPayload interface {
	isFileChunkPayload()
}

// FileChunk_Header wraps a FileHeader in the oneof.
type FileChunk_Header struct {
	Header *FileHeader
}

func (*FileChunk_Header) isFileChunkPayload() {}

// FileChunk_Data wraps raw bytes in the oneof.
type FileChunk_Data struct {
	Data []byte
}

func (*FileChunk_Data) isFileChunkPayload() {}

// FileChunk_Trailer wraps a FileTrailer in the oneof.
type FileChunk_Trailer struct {
	Trailer *FileTrailer
}

func (*FileChunk_Trailer) isFileChunkPayload() {}

// FileHeader contains file metadata sent at the beginning of a file stream.
type FileHeader struct {
	Blake3Hash    string `json:"blake3_hash"`
	FileName      string `json:"file_name"`
	FilePath      string `json:"file_path"`
	FileExt       string `json:"file_ext"`
	FileSize      int64  `json:"file_size"`
	CreatedAt     int64  `json:"created_at"`
	ModifiedAt    int64  `json:"modified_at"`
	AccessedAt    int64  `json:"accessed_at"`
	Permissions   uint32 `json:"permissions"`
	Owner         string `json:"owner"`
	FileGroup     string `json:"file_group"`
	Hidden        bool   `json:"hidden"`
	Symlink       bool   `json:"symlink"`
	SymlinkTarget string `json:"symlink_target"`
	Os            string `json:"os"`
	EncryptedDek  []byte `json:"encrypted_dek"`
	Nonce         []byte `json:"nonce"`
}

// FileTrailer is sent at the end of a file stream for verification.
type FileTrailer struct {
	Blake3Hash string `json:"blake3_hash"`
	BytesTotal int64  `json:"bytes_total"`
}

// Manifest represents the client's file manifest for deduplication.
type Manifest struct {
	Entries  []*ManifestEntryProto `json:"entries"`
	ClientId string                `json:"client_id"`
	BackupId string                `json:"backup_id"`
}

// ManifestEntryProto represents a single file in the manifest protocol message.
type ManifestEntryProto struct {
	Blake3Hash string `json:"blake3_hash"`
	FilePath   string `json:"file_path"`
	FileSize   int64  `json:"file_size"`
	ModifiedAt int64  `json:"modified_at"`
}

// ManifestDiff is the server's response indicating which files it still needs.
type ManifestDiff struct {
	NeededHashes []string `json:"needed_hashes"`
	DedupCount   int32    `json:"dedup_count"`
	TotalFiles   int32    `json:"total_files"`
}

// UploadSummary is returned after a complete upload stream.
type UploadSummary struct {
	FilesReceived int64 `json:"files_received"`
	BytesTotal    int64 `json:"bytes_total"`
	DedupCount    int64 `json:"dedup_count"`
}

// RestoreRequest is sent to initiate a file download.
type RestoreRequest struct {
	Blake3Hash string `json:"blake3_hash"`
	ClientId   string `json:"client_id"`
}

// DatabaseChunk is used for streaming client database sync.
// The first chunk in the stream MUST include ClientId to identify the client.
// Subsequent chunks only need to carry Data.
type DatabaseChunk struct {
	Data     []byte `json:"data"`
	ClientId string `json:"client_id,omitempty"` // required in the first chunk
}

// SyncResponse is returned after database sync completes.
type SyncResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}
