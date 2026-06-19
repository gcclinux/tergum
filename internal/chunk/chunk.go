// Package chunk provides fixed-size chunking for gRPC streaming upload/download.
package chunk

import (
	"errors"
	"io"
)

// DefaultChunkSize is 64KB.
const DefaultChunkSize = 65536

// ErrInvalidChunkSize is returned when chunkSize <= 0.
var ErrInvalidChunkSize = errors.New("chunk: chunkSize must be greater than zero")

// Split reads all data from reader, splitting into chunks of chunkSize bytes.
// The last chunk may be smaller than chunkSize.
// Returns all chunks in order.
func Split(reader io.Reader, chunkSize int) ([][]byte, error) {
	if chunkSize <= 0 {
		return nil, ErrInvalidChunkSize
	}

	var chunks [][]byte
	buf := make([]byte, chunkSize)

	for {
		n, err := io.ReadFull(reader, buf)
		if n > 0 {
			// Allocate a new slice for each chunk (don't share underlying arrays)
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			chunks = append(chunks, chunk)
		}
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			break
		}
		if err != nil {
			return nil, err
		}
	}

	return chunks, nil
}

// Join concatenates all chunks in order, returning the reassembled data.
// Join of an empty slice returns an empty []byte (not nil).
func Join(chunks [][]byte) []byte {
	totalLen := 0
	for _, c := range chunks {
		totalLen += len(c)
	}

	result := make([]byte, 0, totalLen)
	for _, c := range chunks {
		result = append(result, c...)
	}
	return result
}

// StreamChunks reads from reader in chunks of chunkSize bytes, calling fn for each chunk.
// This is memory-efficient as it processes one chunk at a time.
// The last chunk may be smaller than chunkSize.
// If fn returns an error, streaming stops and the error is propagated.
// The fn callback receives a copy of the chunk data that is safe to retain.
func StreamChunks(reader io.Reader, chunkSize int, fn func(chunk []byte) error) error {
	if chunkSize <= 0 {
		return ErrInvalidChunkSize
	}

	buf := make([]byte, chunkSize)

	for {
		n, err := io.ReadFull(reader, buf)
		if n > 0 {
			// Give the callback a copy so it can safely retain it
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			if fnErr := fn(chunk); fnErr != nil {
				return fnErr
			}
		}
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}
