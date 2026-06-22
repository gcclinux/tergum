package grpc

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/gcclinux/tergum/internal/grpc/proto"
)

const (
	// syncChunkSize is the chunk size for streaming the database file (64KB).
	syncChunkSize = 64 * 1024
)

// SyncDatabaseToServer reads the local DB file and streams it to the server
// via the DataService SyncDatabase RPC. The first chunk includes the clientID
// so the server can store it at clients/{client_id}.db.
func SyncDatabaseToServer(ctx context.Context, client proto.DataServiceClient, dbPath string, clientID string) error {
	if clientID == "" {
		return fmt.Errorf("clientID is required for database sync")
	}

	f, err := os.Open(dbPath)
	if err != nil {
		return fmt.Errorf("opening database file %s: %w", dbPath, err)
	}
	defer f.Close()

	stream, err := client.SyncDatabase(ctx)
	if err != nil {
		return fmt.Errorf("initiating SyncDatabase stream: %w", err)
	}

	buf := make([]byte, syncChunkSize)
	firstChunk := true

	for {
		n, readErr := f.Read(buf)
		if n > 0 {
			chunk := &proto.DatabaseChunk{
				Data: append([]byte(nil), buf[:n]...),
			}
			// Include client_id in the first chunk.
			if firstChunk {
				chunk.ClientId = clientID
				firstChunk = false
			}
			if sendErr := stream.Send(chunk); sendErr != nil {
				return fmt.Errorf("sending database chunk: %w", sendErr)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return fmt.Errorf("reading database file: %w", readErr)
		}
	}

	resp, err := stream.CloseAndRecv()
	if err != nil {
		return fmt.Errorf("closing SyncDatabase stream: %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("server reported sync failure: %s", resp.Message)
	}

	return nil
}
