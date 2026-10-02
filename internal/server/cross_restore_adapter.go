package server

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gcclinux/tergum/internal/crypto"
	"github.com/gcclinux/tergum/internal/db"
	grpcpkg "github.com/gcclinux/tergum/internal/grpc"
	"github.com/gcclinux/tergum/internal/model"
	"github.com/gcclinux/tergum/internal/restore"
	"github.com/gcclinux/tergum/internal/webui"
)

// crossRestoreAdapter satisfies grpcpkg.CrossRestorer. For each request it opens
// the source client's server-side database copy, reads file bytes from the
// shared CAS, decrypts them with the server-side master key, and streams them to
// the target client via the RemoteClientConnector's PushRestore stream. It
// reuses the extracted internal/restore.CrossClientRestorer orchestrator, so the
// push/decrypt logic lives in exactly one place (design approach 1 for the
// orchestrator; this adapter supplies the server-specific I/O wiring).
type crossRestoreAdapter struct {
	clientsDir string // directory holding <clientID>.db copies
	storageDir string // shared CAS storage directory
	masterKey  []byte // server-side master key (nil if unavailable)
	encEnabled bool
	connector  *webui.RemoteClientConnector
}

// RestoreToTarget implements grpcpkg.CrossRestorer.
func (a *crossRestoreAdapter) RestoreToTarget(ctx context.Context, req grpcpkg.CrossRestoreRequest) (grpcpkg.CrossRestoreResult, error) {
	if a.connector == nil {
		return grpcpkg.CrossRestoreResult{}, &model.ConfigError{Message: "remote client connector unavailable on server"}
	}

	// Open the source client's database copy held on the server.
	srcDB := filepath.Join(a.clientsDir, req.SourceClientID+".db")
	if _, err := os.Stat(srcDB); err != nil {
		return grpcpkg.CrossRestoreResult{}, &model.ConfigError{Message: fmt.Sprintf("source client %q database copy not found on server", req.SourceClientID)}
	}
	repo, err := db.NewRepository(srcDB, false)
	if err != nil {
		return grpcpkg.CrossRestoreResult{}, fmt.Errorf("open source client database: %w", err)
	}
	defer repo.Close()

	var encryptor *crypto.AESEncryptor
	if a.encEnabled {
		encryptor = crypto.NewEncryptor()
	}

	source := &restore.LocalDataSource{StorageDir: a.storageDir}

	opener := func(ctx context.Context, targetClientID string) (restore.PushRestoreStream, error) {
		stream, err := a.connector.OpenPushRestoreStream(ctx, targetClientID)
		if err != nil {
			return nil, err
		}
		return stream, nil
	}

	orch := restore.NewCrossClientRestorer(source, repo, encryptor, a.masterKey, opener)
	res, err := orch.RestoreToTarget(ctx, restore.CrossRestoreRequest{
		SourceClientID: req.SourceClientID,
		TargetClientID: req.TargetClientID,
		Query:          req.Query,
		BackupID:       req.BackupID,
		File:           req.File,
		Dest:           req.Dest,
	})
	if err != nil {
		return grpcpkg.CrossRestoreResult{}, err
	}

	return grpcpkg.CrossRestoreResult{
		FilesSent:     res.FilesSent,
		FilesReceived: res.FilesReceived,
		FilesFailed:   res.FilesFailed,
	}, nil
}

// Ensure the adapter satisfies the interface at compile time.
var _ grpcpkg.CrossRestorer = (*crossRestoreAdapter)(nil)
