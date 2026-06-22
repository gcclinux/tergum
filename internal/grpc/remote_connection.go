package grpc

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/gcclinux/tergum/internal/backup"
	"github.com/gcclinux/tergum/internal/grpc/proto"
	"github.com/gcclinux/tergum/internal/model"
)

// RemoteServerConnection implements backup.ServerConnection using gRPC calls
// to a remote Tergum server. It wraps the DataServiceClient and streams
// files/database to the server.
type RemoteServerConnection struct {
	client   proto.DataServiceClient
	clientID string
}

// NewRemoteServerConnection creates a RemoteServerConnection for the given client.
func NewRemoteServerConnection(client proto.DataServiceClient, clientID string) *RemoteServerConnection {
	return &RemoteServerConnection{
		client:   client,
		clientID: clientID,
	}
}

// ExchangeManifest sends the client manifest to the server and returns the diff.
func (r *RemoteServerConnection) ExchangeManifest(ctx context.Context, manifest []model.ManifestEntry) (backup.ManifestDiff, error) {
	entries := make([]*proto.ManifestEntryProto, len(manifest))
	for i, e := range manifest {
		entries[i] = &proto.ManifestEntryProto{
			Blake3Hash: e.Blake3Hash,
			FilePath:   e.FilePath,
			FileSize:   e.FileSize,
			ModifiedAt: e.ModifiedAt,
		}
	}

	resp, err := r.client.ExchangeManifest(ctx, &proto.Manifest{
		Entries:  entries,
		ClientId: r.clientID,
	})
	if err != nil {
		return backup.ManifestDiff{}, fmt.Errorf("exchange manifest: %w", err)
	}

	return backup.ManifestDiff{
		NeededHashes: resp.NeededHashes,
		DedupCount:   int(resp.DedupCount),
	}, nil
}

// UploadFile streams an encrypted file to the server via the Upload RPC.
func (r *RemoteServerConnection) UploadFile(ctx context.Context, hash string, data []byte, wrappedDEK []byte, nonce []byte, entry model.BackupEntry) error {
	stream, err := r.client.Upload(ctx)
	if err != nil {
		return fmt.Errorf("opening upload stream: %w", err)
	}

	// Send file header.
	header := &proto.FileHeader{
		Blake3Hash:    hash,
		FileName:      entry.FileName,
		FilePath:      entry.FilePath,
		FileExt:       entry.FileExt,
		FileSize:      entry.FileSize,
		Owner:         entry.Owner,
		FileGroup:     entry.FileGroup,
		Hidden:        entry.Hidden,
		Symlink:       entry.Symlink,
		SymlinkTarget: entry.SymlinkTarget,
		Os:            entry.OS,
		EncryptedDek:  wrappedDEK,
		Nonce:         nonce,
	}
	if entry.Permissions != nil {
		header.Permissions = *entry.Permissions
	}

	if err := stream.Send(&proto.FileChunk{
		Payload: &proto.FileChunk_Header{Header: header},
	}); err != nil {
		return fmt.Errorf("sending file header: %w", err)
	}

	// Stream data in 64KB chunks.
	reader := bytes.NewReader(data)
	buf := make([]byte, defaultChunkSize)
	for {
		n, readErr := reader.Read(buf)
		if n > 0 {
			if err := stream.Send(&proto.FileChunk{
				Payload: &proto.FileChunk_Data{Data: append([]byte(nil), buf[:n]...)},
			}); err != nil {
				return fmt.Errorf("sending data chunk: %w", err)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return fmt.Errorf("reading data: %w", readErr)
		}
	}

	// Send trailer.
	if err := stream.Send(&proto.FileChunk{
		Payload: &proto.FileChunk_Trailer{Trailer: &proto.FileTrailer{
			Blake3Hash: hash,
			BytesTotal: int64(len(data)),
		}},
	}); err != nil {
		return fmt.Errorf("sending trailer: %w", err)
	}

	// Close stream and receive summary.
	_, err = stream.CloseAndRecv()
	if err != nil {
		return fmt.Errorf("closing upload stream: %w", err)
	}

	return nil
}

// SyncDatabase streams the local database file to the server using SyncDatabaseToServer.
func (r *RemoteServerConnection) SyncDatabase(ctx context.Context, dbPath string) error {
	return SyncDatabaseToServer(ctx, r.client, dbPath, r.clientID)
}

// Ensure RemoteServerConnection satisfies the backup.ServerConnection interface at compile time.
var _ backup.ServerConnection = (*RemoteServerConnection)(nil)
