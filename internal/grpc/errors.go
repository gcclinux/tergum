// Package grpc implements the gRPC server handlers for CommandService and DataService.
package grpc

import (
	"errors"

	"github.com/ricardopadilha/tergum/internal/model"
	"github.com/ricardopadilha/tergum/internal/storage"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// MapError converts internal Tergum error types to appropriate gRPC status errors.
// The mapping follows the convention:
//   - ConfigError      → codes.InvalidArgument
//   - ConnectionError  → codes.Unavailable
//   - AuthError        → codes.Unauthenticated
//   - StorageError     → codes.ResourceExhausted
//   - StoppedError     → codes.Canceled
//   - BackupFailedError→ codes.Internal
//   - storage.ErrNotFound → codes.NotFound
//   - General errors   → codes.Internal
func MapError(err error) error {
	if err == nil {
		return nil
	}

	var configErr *model.ConfigError
	if errors.As(err, &configErr) {
		return status.Error(codes.InvalidArgument, configErr.Error())
	}

	var connErr *model.ConnectionError
	if errors.As(err, &connErr) {
		return status.Error(codes.Unavailable, connErr.Error())
	}

	var authErr *model.AuthError
	if errors.As(err, &authErr) {
		return status.Error(codes.Unauthenticated, authErr.Error())
	}

	var storageErr *model.StorageError
	if errors.As(err, &storageErr) {
		return status.Error(codes.ResourceExhausted, storageErr.Error())
	}

	var stoppedErr *model.StoppedError
	if errors.As(err, &stoppedErr) {
		return status.Error(codes.Canceled, stoppedErr.Error())
	}

	var backupErr *model.BackupFailedError
	if errors.As(err, &backupErr) {
		return status.Error(codes.Internal, backupErr.Error())
	}

	if errors.Is(err, storage.ErrNotFound) {
		return status.Error(codes.NotFound, err.Error())
	}

	return status.Error(codes.Internal, err.Error())
}
