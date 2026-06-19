// Package storage implements a content-addressable store with a two-level directory layout.
package storage

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
)

// hashPattern validates a BLAKE3 hash: exactly 64 lowercase hex characters.
var hashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// ErrInvalidHash is returned when a hash doesn't match the expected format.
var ErrInvalidHash = errors.New("invalid hash: must be 64 lowercase hex characters")

// ErrNotFound is returned when a requested hash does not exist in the store.
var ErrNotFound = errors.New("hash not found in store")

// Store defines operations on the content-addressable store.
type Store interface {
	// Put stores encrypted content by hash.
	Put(ctx context.Context, hash string, reader io.Reader) error
	// Get retrieves content by hash.
	Get(ctx context.Context, hash string) (io.ReadCloser, error)
	// Exists checks if a hash exists in storage.
	Exists(ctx context.Context, hash string) (bool, error)
	// Delete removes a file by hash.
	Delete(ctx context.Context, hash string) error
	// RefCount returns number of DB entries referencing this hash.
	RefCount(ctx context.Context, hash string) (int64, error)
}

// RefCounter provides reference counting for stored hashes via a database query.
type RefCounter interface {
	CountHashReferences(ctx context.Context, hash string) (int64, error)
}

// CAS implements the Store interface using a two-level directory layout.
type CAS struct {
	baseDir    string
	refCounter RefCounter
}

// NewCAS creates a new content-addressable store rooted at baseDir.
// If refCounter is nil, RefCount will always return 0.
func NewCAS(baseDir string, refCounter RefCounter) *CAS {
	return &CAS{
		baseDir:    baseDir,
		refCounter: refCounter,
	}
}

// validateHash checks that the hash is exactly 64 lowercase hex characters.
func validateHash(hash string) error {
	if len(hash) != 64 {
		return ErrInvalidHash
	}
	if _, err := hex.DecodeString(hash); err != nil {
		return ErrInvalidHash
	}
	// Ensure lowercase
	if !hashPattern.MatchString(hash) {
		return ErrInvalidHash
	}
	return nil
}

// hashDir returns the two-level directory path for a hash: baseDir/hash[:2]
func (c *CAS) hashDir(hash string) string {
	return filepath.Join(c.baseDir, hash[:2])
}

// hashPath returns the full file path for a hash: baseDir/hash[:2]/hash
func (c *CAS) hashPath(hash string) string {
	return filepath.Join(c.baseDir, hash[:2], hash)
}

// Put stores content from reader under the given hash.
// It creates the subdirectory if needed and uses an atomic write (temp file + rename).
func (c *CAS) Put(ctx context.Context, hash string, reader io.Reader) error {
	if err := validateHash(hash); err != nil {
		return err
	}

	dir := c.hashDir(hash)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating storage directory: %w", err)
	}

	// Write to a temp file in the same directory for atomic rename.
	tmp, err := os.CreateTemp(dir, ".cas-tmp-*")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpName := tmp.Name()

	// Ensure cleanup on failure.
	success := false
	defer func() {
		if !success {
			tmp.Close()
			os.Remove(tmpName)
		}
	}()

	if _, err := io.Copy(tmp, reader); err != nil {
		return fmt.Errorf("writing content: %w", err)
	}

	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing temp file: %w", err)
	}

	dest := c.hashPath(hash)
	if err := os.Rename(tmpName, dest); err != nil {
		return fmt.Errorf("renaming temp file: %w", err)
	}

	success = true
	return nil
}

// Get retrieves content by hash. The caller must close the returned ReadCloser.
func (c *CAS) Get(ctx context.Context, hash string) (io.ReadCloser, error) {
	if err := validateHash(hash); err != nil {
		return nil, err
	}

	path := c.hashPath(hash)
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("opening file: %w", err)
	}
	return f, nil
}

// Exists checks whether a hash exists in the store.
func (c *CAS) Exists(ctx context.Context, hash string) (bool, error) {
	if err := validateHash(hash); err != nil {
		return false, err
	}

	path := c.hashPath(hash)
	_, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("checking file: %w", err)
	}
	return true, nil
}

// Delete removes a file by hash and cleans up the parent directory if empty.
func (c *CAS) Delete(ctx context.Context, hash string) error {
	if err := validateHash(hash); err != nil {
		return err
	}

	path := c.hashPath(hash)
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return ErrNotFound
		}
		return fmt.Errorf("removing file: %w", err)
	}

	// Try to remove the parent directory if empty. Ignore errors (may not be empty).
	dir := c.hashDir(hash)
	_ = os.Remove(dir)

	return nil
}

// RefCount returns the number of database entries referencing this hash.
// If no RefCounter is configured, returns 0.
func (c *CAS) RefCount(ctx context.Context, hash string) (int64, error) {
	if err := validateHash(hash); err != nil {
		return 0, err
	}

	if c.refCounter == nil {
		return 0, nil
	}
	return c.refCounter.CountHashReferences(ctx, hash)
}
