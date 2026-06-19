package model

import "fmt"

// Exit codes for CLI operations.
const (
	ExitSuccess       = 0
	ExitGeneralError  = 1
	ExitConfigError   = 2
	ExitConnError     = 3
	ExitAuthError     = 4
	ExitStorageError  = 5
	ExitStopped       = 10
	ExitBackupFailed  = 11
)

// ConfigError indicates a configuration validation or loading failure.
type ConfigError struct {
	Message string
}

func (e *ConfigError) Error() string {
	return fmt.Sprintf("config error: %s", e.Message)
}

// ExitCode returns the CLI exit code for this error type.
func (e *ConfigError) ExitCode() int {
	return ExitConfigError
}

// ConnectionError indicates a network or gRPC connection failure.
type ConnectionError struct {
	Message string
}

func (e *ConnectionError) Error() string {
	return fmt.Sprintf("connection error: %s", e.Message)
}

// ExitCode returns the CLI exit code for this error type.
func (e *ConnectionError) ExitCode() int {
	return ExitConnError
}

// AuthError indicates an authentication or authorization failure.
type AuthError struct {
	Message string
}

func (e *AuthError) Error() string {
	return fmt.Sprintf("auth error: %s", e.Message)
}

// ExitCode returns the CLI exit code for this error type.
func (e *AuthError) ExitCode() int {
	return ExitAuthError
}

// StorageError indicates a storage backend failure.
type StorageError struct {
	Message string
}

func (e *StorageError) Error() string {
	return fmt.Sprintf("storage error: %s", e.Message)
}

// ExitCode returns the CLI exit code for this error type.
func (e *StorageError) ExitCode() int {
	return ExitStorageError
}

// StoppedError indicates the operation was stopped by the user.
type StoppedError struct {
	Message string
}

func (e *StoppedError) Error() string {
	if e.Message == "" {
		return "operation stopped by user"
	}
	return fmt.Sprintf("stopped: %s", e.Message)
}

// ExitCode returns the CLI exit code for this error type.
func (e *StoppedError) ExitCode() int {
	return ExitStopped
}

// BackupFailedError indicates that a backup operation failed.
type BackupFailedError struct {
	Message string
}

func (e *BackupFailedError) Error() string {
	return fmt.Sprintf("backup failed: %s", e.Message)
}

// ExitCode returns the CLI exit code for this error type.
func (e *BackupFailedError) ExitCode() int {
	return ExitBackupFailed
}

// ExitCoder is implemented by errors that map to specific CLI exit codes.
type ExitCoder interface {
	error
	ExitCode() int
}

// GetExitCode returns the appropriate exit code for an error.
// Returns ExitSuccess for nil errors, the typed exit code for ExitCoder
// implementations, and ExitGeneralError for all other errors.
func GetExitCode(err error) int {
	if err == nil {
		return ExitSuccess
	}
	if ec, ok := err.(ExitCoder); ok {
		return ec.ExitCode()
	}
	return ExitGeneralError
}
