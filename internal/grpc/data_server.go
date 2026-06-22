package grpc

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/gcclinux/tergum/internal/db"
	"github.com/gcclinux/tergum/internal/grpc/proto"
	"github.com/gcclinux/tergum/internal/model"
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
	clientsDir string // directory for storing client database copies
	restoreSem *Semaphore
}

// DataServerConfig holds configuration for the DataServer.
type DataServerConfig struct {
	Store       storage.Store
	Repo        db.Repository
	ClientsDir  string // path to clients/ directory for SyncDatabase
	MaxRestores int    // max concurrent restores, default 8
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
		clientsDir: cfg.ClientsDir,
		restoreSem: NewSemaphore(maxRestores),
	}
}

// Upload receives a stream of FileChunks (Header â†’ Data â†’ Trailer),
// reconstructs the file, stores it in the CAS, and inserts a DB entry.
func (s *DataServer) Upload(stream proto.DataService_UploadServer) error {
	var (
		header     *proto.FileHeader
		buf        bytes.Buffer
		filesCount int64
		bytesTotal int64
	)

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
				if err := s.storeFile(stream.Context(), header, buf.Bytes()); err != nil {
					return err
				}
				filesCount++
				bytesTotal += int64(buf.Len())
				buf.Reset()
			}
			header = h
			continue
		}

		if data := chunk.GetData(); data != nil {
			buf.Write(data)
			continue
		}

		if t := chunk.GetTrailer(); t != nil {
			// Trailer marks end of current file.
			if header != nil {
				if err := s.storeFile(stream.Context(), header, buf.Bytes()); err != nil {
					return err
				}
				filesCount++
				bytesTotal += int64(buf.Len())
				buf.Reset()
				header = nil
			}
		}
	}

	// Handle case where stream ends without trailer (last file).
	if header != nil {
		if err := s.storeFile(stream.Context(), header, buf.Bytes()); err != nil {
			return err
		}
		filesCount++
		bytesTotal += int64(buf.Len())
	}

	return stream.SendAndClose(&proto.UploadSummary{
		FilesReceived: filesCount,
		BytesTotal:    bytesTotal,
		DedupCount:    0,
	})
}

// storeFile writes file data to the CAS and inserts a backup entry in the database.
func (s *DataServer) storeFile(ctx context.Context, header *proto.FileHeader, data []byte) error {
	// Store in CAS.
	if err := s.store.Put(ctx, header.Blake3Hash, bytes.NewReader(data)); err != nil {
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

	if err := s.repo.InsertBackupEntry(ctx, entry); err != nil {
		return MapError(err)
	}

	return nil
}

// Download retrieves a file from the CAS by hash and streams it back as FileChunks.
func (s *DataServer) Download(req *proto.RestoreRequest, stream proto.DataService_DownloadServer) error {
	ctx := stream.Context()

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
	return stream.SendAndClose(&proto.SyncResponse{
		Success: true,
		Message: fmt.Sprintf("database synced for client %s: %d bytes received", clientID, bytesReceived),
	})
}

// ExchangeManifest compares the received manifest against the CAS and returns
// a ManifestDiff indicating which hashes the server still needs.
func (s *DataServer) ExchangeManifest(ctx context.Context, manifest *proto.Manifest) (*proto.ManifestDiff, error) {
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
