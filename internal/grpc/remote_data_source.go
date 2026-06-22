package grpc

import (
	"context"
	"fmt"
	"io"

	"github.com/gcclinux/tergum/internal/grpc/proto"
	"github.com/gcclinux/tergum/internal/restore"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// RemoteDataSource implements restore.DataSource for client-mode restores.
// It downloads file content from the remote server via the DataService.Download RPC.
type RemoteDataSource struct {
	client   proto.DataServiceClient
	clientID string
}

// NewRemoteDataSource creates a RemoteDataSource for the given client.
func NewRemoteDataSource(client proto.DataServiceClient, clientID string) *RemoteDataSource {
	return &RemoteDataSource{
		client:   client,
		clientID: clientID,
	}
}

// DownloadFile retrieves the encrypted file content from the remote server by BLAKE3 hash.
// It calls the DataService.Download RPC and reassembles the streaming FileChunk messages
// (Header → Data → Trailer) into a single byte slice.
// Decryption is handled by the restore engine after this method returns.
func (r *RemoteDataSource) DownloadFile(ctx context.Context, hash string) ([]byte, error) {
	stream, err := r.client.Download(ctx, &proto.RestoreRequest{
		Blake3Hash: hash,
		ClientId:   r.clientID,
	})
	if err != nil {
		if s, ok := status.FromError(err); ok && s.Code() == codes.NotFound {
			return nil, fmt.Errorf("file not found on server: hash %s", hash)
		}
		return nil, fmt.Errorf("opening download stream for hash %s: %w", hash, err)
	}

	var data []byte
	for {
		chunk, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				break
			}
			if s, ok := status.FromError(err); ok && s.Code() == codes.NotFound {
				return nil, fmt.Errorf("file not found on server: hash %s", hash)
			}
			return nil, fmt.Errorf("receiving download chunk for hash %s: %w", hash, err)
		}

		switch {
		case chunk.GetHeader() != nil:
			// Header chunk — nothing to accumulate, metadata only.
		case chunk.GetData() != nil:
			data = append(data, chunk.GetData()...)
		case chunk.GetTrailer() != nil:
			// Trailer chunk — stream should end after this.
		}
	}

	return data, nil
}

// Ensure RemoteDataSource satisfies the restore.DataSource interface at compile time.
var _ restore.DataSource = (*RemoteDataSource)(nil)
