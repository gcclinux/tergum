package grpc

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/gcclinux/tergum/internal/db"
	"github.com/gcclinux/tergum/internal/grpc/proto"
	"github.com/gcclinux/tergum/internal/model"
	"github.com/gcclinux/tergum/internal/registry"
	"github.com/gcclinux/tergum/internal/storage"
)

const (
	// defaultChunkSize is the size of data chunks for streaming (64KB).
	defaultChunkSize = 64 * 1024
)

// DataServer implements the DataServiceServer interface.
type DataServer struct {
	proto.UnimplementedDataServiceServer

	store      storage.Store
	repo       db.Repository
	writeQueue *db.WriteQueue
	clientsDir string // directory for storing client database copies
	restoreSem *Semaphore
	onSync     func(clientID string, dbPath string)
	registry   *registry.Registry // optional; used to reject disabled clients
}

// DataServerConfig holds configuration for the DataServer.
type DataServerConfig struct {
	Store       storage.Store
	Repo        db.Repository
	WriteQueue  *db.WriteQueue                       // serializes DB writes to avoid SQLITE_BUSY
	ClientsDir  string                               // path to clients/ directory for SyncDatabase
	MaxRestores int                                  // max concurrent restores, default 8
	OnSync      func(clientID string, dbPath string) // called after a successful SyncDatabase
	Registry    *registry.Registry                   // optional; enables disabled-client checks
}

// NewDataServer creates a new DataServer with the given configuration.
func NewDataServer(cfg DataServerConfig) *DataServer {
	maxRestores := cfg.MaxRestores
	if maxRestores <= 0 {
		maxRestores = 8
	}

	return &DataServer{
		store:      cfg.Store,
		repo:       cfg.Repo,
		writeQueue: cfg.WriteQueue,
		clientsDir: cfg.ClientsDir,
		restoreSem: NewSemaphore(maxRestores),
		onSync:     cfg.OnSync,
		registry:   cfg.Registry,
	}
}

// Upload receives a stream of FileChunks (Header → Data → Trailer),
// streams data directly to a temp file to avoid memory pressure, then
// stores the file in the CAS and inserts a DB entry.
func (s *DataServer) Upload(stream proto.DataService_UploadServer) error {
	// Reject uploads from disabled clients.
	if s.registry != nil {
		if clientID, err := clientIDFromContext(stream.Context()); err == nil && clientID != "" {
			if ci := s.registry.GetClient(clientID); ci != nil && ci.Disabled {
				return MapError(&model.ConfigError{Message: fmt.Sprintf("client %q is disabled", clientID)})
			}
		}
	}

	var (
		header     *proto.FileHeader
		tmpFile    *os.File
		tmpPath    string
		fileBytes  int64
		filesCount int64
		bytesTotal int64
	)

	// cleanup ensures any open temp file is removed on error.
	cleanup := func() {
		if tmpFile != nil {
			tmpFile.Close()
			os.Remove(tmpPath)
			tmpFile = nil
			tmpPath = ""
		}
	}
	defer cleanup()

	// finishCurrentFile stores the current temp file to CAS and inserts the DB entry.
	finishCurrentFile := func() error {
		if header == nil || tmpFile == nil {
			return nil
		}

		// Close the temp file so it can be read by the store.
		if err := tmpFile.Close(); err != nil {
			os.Remove(tmpPath)
			tmpFile = nil
			return MapError(&model.StorageError{Message: fmt.Sprintf("closing temp file: %v", err)})
		}

		// Open for reading and pass to store.
		reader, err := os.Open(tmpPath)
		if err != nil {
			os.Remove(tmpPath)
			tmpFile = nil
			return MapError(&model.StorageError{Message: fmt.Sprintf("reopening temp file: %v", err)})
		}

		if err := s.storeFileFromReader(stream.Context(), header, reader, fileBytes); err != nil {
			reader.Close()
			os.Remove(tmpPath)
			tmpFile = nil
			return err
		}
		reader.Close()
		os.Remove(tmpPath)

		filesCount++
		bytesTotal += fileBytes
		tmpFile = nil
		tmpPath = ""
		fileBytes = 0
		header = nil
		return nil
	}

	for {
		chunk, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return MapError(err)
		}

		if h := chunk.GetHeader(); h != nil {
			// If we have a pending file from a previous header, store it.
			if header != nil {
				if err := finishCurrentFile(); err != nil {
					return err
				}
			}
			header = h

			// Create a new temp file for this upload.
			tmpFile, err = os.CreateTemp("", "tergum-upload-*")
			if err != nil {
				return MapError(&model.StorageError{Message: fmt.Sprintf("creating temp file: %v", err)})
			}
			tmpPath = tmpFile.Name()
			fileBytes = 0
			continue
		}

		if data := chunk.GetData(); data != nil {
			if tmpFile == nil {
				slog.Warn("received data chunk without header, ignoring")
				continue
			}
			n, err := tmpFile.Write(data)
			if err != nil {
				return MapError(&model.StorageError{Message: fmt.Sprintf("writing to temp file: %v", err)})
			}
			fileBytes += int64(n)
			continue
		}

		if t := chunk.GetTrailer(); t != nil {
			// Trailer marks end of current file.
			if err := finishCurrentFile(); err != nil {
				return err
			}
		}
	}

	// Handle case where stream ends without trailer (last file).
	if header != nil {
		if err := finishCurrentFile(); err != nil {
			return err
		}
	}

	return stream.SendAndClose(&proto.UploadSummary{
		FilesReceived: filesCount,
		BytesTotal:    bytesTotal,
		DedupCount:    0,
	})
}

// storeFileFromReader writes file data from a reader to the CAS and inserts a backup entry.
func (s *DataServer) storeFileFromReader(ctx context.Context, header *proto.FileHeader, reader io.Reader, size int64) error {
	// Store in CAS.
	if err := s.store.Put(ctx, header.Blake3Hash, reader); err != nil {
		return MapError(&model.StorageError{Message: fmt.Sprintf("storing file %s: %v", header.Blake3Hash, err)})
	}

	// Insert backup entry in DB.
	entry := model.BackupEntry{
		Blake3Hash:    header.Blake3Hash,
		FileName:      header.FileName,
		FilePath:      header.FilePath,
		FileExt:       header.FileExt,
		FileSize:      header.FileSize,
		Owner:         header.Owner,
		FileGroup:     header.FileGroup,
		Hidden:        header.Hidden,
		Symlink:       header.Symlink,
		SymlinkTarget: header.SymlinkTarget,
		OS:            header.Os,
		EncryptedDEK:  header.EncryptedDek,
		Nonce:         header.Nonce,
	}

	if header.Permissions != 0 {
		perm := header.Permissions
		entry.Permissions = &perm
	}

	// Serialize the DB write through the write queue to avoid SQLITE_BUSY
	// when multiple upload streams run concurrently.
	if s.writeQueue != nil {
		if err := s.writeQueue.Submit(ctx, func() error {
			return s.repo.InsertBackupEntry(ctx, entry)
		}); err != nil {
			return MapError(err)
		}
		return nil
	}

	if err := s.repo.InsertBackupEntry(ctx, entry); err != nil {
		return MapError(err)
	}

	return nil
}

// Download retrieves a file from the CAS by hash and streams it back as FileChunks.
func (s *DataServer) Download(req *proto.RestoreRequest, stream proto.DataService_DownloadServer) error {
	ctx := stream.Context()

	// Reject downloads from disabled clients.
	if s.registry != nil {
		if clientID, err := clientIDFromContext(ctx); err == nil && clientID != "" {
			if ci := s.registry.GetClient(clientID); ci != nil && ci.Disabled {
				return MapError(&model.ConfigError{Message: fmt.Sprintf("client %q is disabled", clientID)})
			}
		}
	}

	// Acquire restore semaphore.
	if err := s.restoreSem.Acquire(ctx); err != nil {
		return MapError(&model.ConnectionError{Message: "server at maximum restore capacity"})
	}
	defer s.restoreSem.Release()

	if req.Blake3Hash == "" {
		return MapError(&model.ConfigError{Message: "blake3_hash is required"})
	}

	// Get file from CAS.
	reader, err := s.store.Get(ctx, req.Blake3Hash)
	if err != nil {
		return MapError(err)
	}
	defer reader.Close()

	// Send header chunk.
	headerChunk := &proto.FileChunk{
		Payload: &proto.FileChunk_Header{
			Header: &proto.FileHeader{
				Blake3Hash: req.Blake3Hash,
			},
		},
	}
	if err := stream.Send(headerChunk); err != nil {
		return MapError(err)
	}

	// Stream data in chunks.
	buf := make([]byte, defaultChunkSize)
	var totalBytes int64
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			dataChunk := &proto.FileChunk{
				Payload: &proto.FileChunk_Data{
					Data: append([]byte(nil), buf[:n]...),
				},
			}
			if sendErr := stream.Send(dataChunk); sendErr != nil {
				return MapError(sendErr)
			}
			totalBytes += int64(n)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return MapError(&model.StorageError{Message: fmt.Sprintf("reading file: %v", err)})
		}
	}

	// Send trailer.
	trailerChunk := &proto.FileChunk{
		Payload: &proto.FileChunk_Trailer{
			Trailer: &proto.FileTrailer{
				Blake3Hash: req.Blake3Hash,
				BytesTotal: totalBytes,
			},
		},
	}
	if err := stream.Send(trailerChunk); err != nil {
		return MapError(err)
	}

	return nil
}

// SyncDatabase receives a streamed client database and writes it to the clients/ directory.
// The first chunk in the stream MUST contain a ClientId to determine the storage filename.
// The database is stored at clients/{client_id}.db.
func (s *DataServer) SyncDatabase(stream proto.DataService_SyncDatabaseServer) error {
	if s.clientsDir == "" {
		return MapError(&model.ConfigError{Message: "clients directory not configured"})
	}

	// Ensure clients directory exists.
	if err := os.MkdirAll(s.clientsDir, 0o755); err != nil {
		return MapError(&model.StorageError{Message: fmt.Sprintf("creating clients directory: %v", err)})
	}

	// Write to a temp file for atomic replacement.
	tmp, err := os.CreateTemp(s.clientsDir, ".sync-tmp-*")
	if err != nil {
		return MapError(&model.StorageError{Message: fmt.Sprintf("creating temp file: %v", err)})
	}
	tmpName := tmp.Name()

	success := false
	defer func() {
		if !success {
			tmp.Close()
			os.Remove(tmpName)
		}
	}()

	var clientID string
	var bytesReceived int64
	firstChunk := true

	for {
		chunk, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return MapError(err)
		}

		// Extract client_id from the first chunk.
		if firstChunk {
			clientID = chunk.ClientId
			if clientID == "" {
				return MapError(&model.ConfigError{Message: "client_id is required in the first database chunk"})
			}
			// Reject sync from disabled clients.
			if s.registry != nil {
				if ci := s.registry.GetClient(clientID); ci != nil && ci.Disabled {
					return MapError(&model.ConfigError{Message: fmt.Sprintf("client %q is disabled", clientID)})
				}
			}
			firstChunk = false
		}

		if len(chunk.Data) > 0 {
			n, err := tmp.Write(chunk.Data)
			if err != nil {
				return MapError(&model.StorageError{Message: fmt.Sprintf("writing database chunk: %v", err)})
			}
			bytesReceived += int64(n)
		}
	}

	// Handle empty stream (no chunks received).
	if firstChunk {
		return MapError(&model.ConfigError{Message: "empty database sync stream: no chunks received"})
	}

	if err := tmp.Close(); err != nil {
		return MapError(&model.StorageError{Message: fmt.Sprintf("closing temp file: %v", err)})
	}

	// Move to final location: clients/{client_id}.db
	destPath := filepath.Join(s.clientsDir, clientID+".db")
	if err := os.Rename(tmpName, destPath); err != nil {
		return MapError(&model.StorageError{Message: fmt.Sprintf("renaming database file: %v", err)})
	}

	success = true

	// Notify the server to update last backup time from the synced DB.
	if s.onSync != nil {
		s.onSync(clientID, destPath)
	}

	return stream.SendAndClose(&proto.SyncResponse{
		Success: true,
		Message: fmt.Sprintf("database synced for client %s: %d bytes received", clientID, bytesReceived),
	})
}

// ExchangeManifest compares the received manifest against the CAS and returns
// a ManifestDiff indicating which hashes the server still needs.
func (s *DataServer) ExchangeManifest(ctx context.Context, manifest *proto.Manifest) (*proto.ManifestDiff, error) {
	// Reject manifest exchange from disabled clients.
	if s.registry != nil {
		if clientID, err := clientIDFromContext(ctx); err == nil && clientID != "" {
			if ci := s.registry.GetClient(clientID); ci != nil && ci.Disabled {
				return nil, MapError(&model.ConfigError{Message: fmt.Sprintf("client %q is disabled", clientID)})
			}
		}
	}

	if manifest == nil || len(manifest.Entries) == 0 {
		return &proto.ManifestDiff{
			NeededHashes: nil,
			DedupCount:   0,
			TotalFiles:   0,
		}, nil
	}

	var neededHashes []string
	var dedupCount int32

	for _, entry := range manifest.Entries {
		exists, err := s.store.Exists(ctx, entry.Blake3Hash)
		if err != nil {
			// If we can't check, assume we need it.
			neededHashes = append(neededHashes, entry.Blake3Hash)
			continue
		}
		if exists {
			dedupCount++
		} else {
			neededHashes = append(neededHashes, entry.Blake3Hash)
		}
	}

	return &proto.ManifestDiff{
		NeededHashes: neededHashes,
		DedupCount:   dedupCount,
		TotalFiles:   int32(len(manifest.Entries)),
	}, nil
}

// Ensure DataServer satisfies the interface at compile time.
var _ proto.DataServiceServer = (*DataServer)(nil)
