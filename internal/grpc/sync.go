package grpc

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

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

	// Retry os.Open if FD limit is temporarily exhausted (e.g. file watcher
	// holding many directory handles on macOS with a low ulimit).
	var f *os.File
	var err error
	backoff := 500 * time.Millisecond
	for attempt := 0; attempt < 5; attempt++ {
		f, err = os.Open(dbPath)
		if err == nil {
			break
		}
		if !isEMFILEError(err) {
			return fmt.Errorf("opening database file %s: %w", dbPath, err)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("opening database file %s: %w", dbPath, err)
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > 10*time.Second {
			backoff = 10 * time.Second
		}
	}
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

// isEMFILEError returns true if the error is caused by file descriptor exhaustion.
func isEMFILEError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "too many open files")
}

// DownloadDatabaseFromServer streams the client's backup database file from the server
// and saves it locally at destDBPath. It writes to a temporary file first and atomically
// moves it into place on completion.
func DownloadDatabaseFromServer(ctx context.Context, client proto.DataServiceClient, clientID string, destDBPath string) error {
	if clientID == "" {
		return fmt.Errorf("clientID is required for database download")
	}

	stream, err := client.DownloadDatabase(ctx, &proto.DownloadDatabaseRequest{
		ClientId: clientID,
	})
	if err != nil {
		return fmt.Errorf("initiating DownloadDatabase stream: %w", err)
	}

	// Ensure destination directory exists.
	destDir := filepath.Dir(destDBPath)
	if err := os.MkdirAll(destDir, 0700); err != nil {
		return fmt.Errorf("creating database directory: %w", err)
	}

	tmp, err := os.CreateTemp(destDir, ".restore-db-tmp-*")
	if err != nil {
		return fmt.Errorf("creating temp file for database download: %w", err)
	}
	tmpName := tmp.Name()

	success := false
	defer func() {
		if !success {
			tmp.Close()
			os.Remove(tmpName)
		}
	}()

	var bytesReceived int64
	for {
		chunk, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("receiving database chunk: %w", err)
		}

		if len(chunk.Data) > 0 {
			n, err := tmp.Write(chunk.Data)
			if err != nil {
				return fmt.Errorf("writing database chunk: %w", err)
			}
			bytesReceived += int64(n)
		}
	}

	if bytesReceived == 0 {
		return fmt.Errorf("empty database received from server for client %q", clientID)
	}

	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing temp database file: %w", err)
	}

	if err := os.Rename(tmpName, destDBPath); err != nil {
		return fmt.Errorf("installing downloaded database: %w", err)
	}

	success = true
	return nil
}

