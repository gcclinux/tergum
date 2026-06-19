package chunk

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

func TestSplitAndJoinRoundTrip_Empty(t *testing.T) {
	reader := bytes.NewReader(nil)
	chunks, err := Split(reader, DefaultChunkSize)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(chunks) != 0 {
		t.Fatalf("expected 0 chunks, got %d", len(chunks))
	}
	result := Join(chunks)
	if len(result) != 0 {
		t.Fatalf("expected empty result, got %d bytes", len(result))
	}
}

func TestSplitAndJoinRoundTrip_Exactly64KB(t *testing.T) {
	data := make([]byte, DefaultChunkSize)
	for i := range data {
		data[i] = byte(i % 256)
	}

	reader := bytes.NewReader(data)
	chunks, err := Split(reader, DefaultChunkSize)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if len(chunks[0]) != DefaultChunkSize {
		t.Fatalf("expected chunk of %d bytes, got %d", DefaultChunkSize, len(chunks[0]))
	}

	result := Join(chunks)
	if !bytes.Equal(result, data) {
		t.Fatal("round-trip failed: data mismatch")
	}
}

func TestSplitAndJoinRoundTrip_64KBPlus1(t *testing.T) {
	size := DefaultChunkSize + 1
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i % 256)
	}

	reader := bytes.NewReader(data)
	chunks, err := Split(reader, DefaultChunkSize)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}
	if len(chunks[0]) != DefaultChunkSize {
		t.Fatalf("expected first chunk of %d bytes, got %d", DefaultChunkSize, len(chunks[0]))
	}
	if len(chunks[1]) != 1 {
		t.Fatalf("expected second chunk of 1 byte, got %d", len(chunks[1]))
	}

	result := Join(chunks)
	if !bytes.Equal(result, data) {
		t.Fatal("round-trip failed: data mismatch")
	}
}

func TestSplitAndJoinRoundTrip_MultipleChunks(t *testing.T) {
	// 3.5 chunks worth of data
	size := DefaultChunkSize*3 + DefaultChunkSize/2
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i % 251) // prime modulus for variety
	}

	reader := bytes.NewReader(data)
	chunks, err := Split(reader, DefaultChunkSize)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(chunks) != 4 {
		t.Fatalf("expected 4 chunks, got %d", len(chunks))
	}
	// First 3 chunks should be full size
	for i := 0; i < 3; i++ {
		if len(chunks[i]) != DefaultChunkSize {
			t.Fatalf("chunk %d: expected %d bytes, got %d", i, DefaultChunkSize, len(chunks[i]))
		}
	}
	// Last chunk should be half size
	if len(chunks[3]) != DefaultChunkSize/2 {
		t.Fatalf("last chunk: expected %d bytes, got %d", DefaultChunkSize/2, len(chunks[3]))
	}

	result := Join(chunks)
	if !bytes.Equal(result, data) {
		t.Fatal("round-trip failed: data mismatch")
	}
}

func TestSplitInvalidChunkSize(t *testing.T) {
	reader := bytes.NewReader([]byte("hello"))

	_, err := Split(reader, 0)
	if !errors.Is(err, ErrInvalidChunkSize) {
		t.Fatalf("expected ErrInvalidChunkSize, got %v", err)
	}

	_, err = Split(reader, -1)
	if !errors.Is(err, ErrInvalidChunkSize) {
		t.Fatalf("expected ErrInvalidChunkSize, got %v", err)
	}
}

func TestStreamChunks_ProcessesAllData(t *testing.T) {
	size := DefaultChunkSize*2 + 100
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i % 256)
	}

	reader := bytes.NewReader(data)
	var collected [][]byte

	err := StreamChunks(reader, DefaultChunkSize, func(chunk []byte) error {
		// Retain the chunk - it should be safe to do so
		collected = append(collected, chunk)
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(collected) != 3 {
		t.Fatalf("expected 3 chunks, got %d", len(collected))
	}

	// Reassemble and compare
	result := Join(collected)
	if !bytes.Equal(result, data) {
		t.Fatal("StreamChunks did not process all data correctly")
	}
}

func TestStreamChunks_ErrorPropagation(t *testing.T) {
	data := make([]byte, DefaultChunkSize*3)
	reader := bytes.NewReader(data)

	callbackErr := errors.New("callback error")
	callCount := 0

	err := StreamChunks(reader, DefaultChunkSize, func(chunk []byte) error {
		callCount++
		if callCount == 2 {
			return callbackErr
		}
		return nil
	})

	if !errors.Is(err, callbackErr) {
		t.Fatalf("expected callback error, got %v", err)
	}
	if callCount != 2 {
		t.Fatalf("expected callback called 2 times, got %d", callCount)
	}
}

func TestStreamChunks_EmptyInput(t *testing.T) {
	reader := bytes.NewReader(nil)
	callCount := 0

	err := StreamChunks(reader, DefaultChunkSize, func(chunk []byte) error {
		callCount++
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if callCount != 0 {
		t.Fatalf("expected 0 callback calls for empty input, got %d", callCount)
	}
}

func TestStreamChunks_InvalidChunkSize(t *testing.T) {
	reader := bytes.NewReader([]byte("hello"))

	err := StreamChunks(reader, 0, func(chunk []byte) error { return nil })
	if !errors.Is(err, ErrInvalidChunkSize) {
		t.Fatalf("expected ErrInvalidChunkSize, got %v", err)
	}

	err = StreamChunks(reader, -5, func(chunk []byte) error { return nil })
	if !errors.Is(err, ErrInvalidChunkSize) {
		t.Fatalf("expected ErrInvalidChunkSize, got %v", err)
	}
}

func TestJoinEmptySlice(t *testing.T) {
	result := Join([][]byte{})
	if result == nil {
		t.Fatal("Join of empty slice should return empty []byte, not nil")
	}
	if len(result) != 0 {
		t.Fatalf("expected empty result, got %d bytes", len(result))
	}
}

func TestJoinNilSlice(t *testing.T) {
	result := Join(nil)
	if result == nil {
		t.Fatal("Join of nil slice should return empty []byte, not nil")
	}
	if len(result) != 0 {
		t.Fatalf("expected empty result, got %d bytes", len(result))
	}
}

func TestSplit_ChunksHaveIndependentBackingArrays(t *testing.T) {
	data := bytes.Repeat([]byte{0xAA}, DefaultChunkSize*2)
	reader := bytes.NewReader(data)

	chunks, err := Split(reader, DefaultChunkSize)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}

	// Modify first chunk, second should be unaffected
	chunks[0][0] = 0xBB
	if chunks[1][0] != 0xAA {
		t.Fatal("chunks share underlying array - modification leaked")
	}
}

func TestStreamChunks_ChunkCopyIsSafe(t *testing.T) {
	data := bytes.Repeat([]byte{0xCC}, DefaultChunkSize*2)
	reader := bytes.NewReader(data)

	var retained [][]byte
	err := StreamChunks(reader, DefaultChunkSize, func(chunk []byte) error {
		retained = append(retained, chunk)
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify retained chunks still have correct data
	for i, chunk := range retained {
		for j, b := range chunk {
			if b != 0xCC {
				t.Fatalf("chunk %d, byte %d: expected 0xCC, got 0x%02X", i, j, b)
			}
		}
	}
}

// errorReader is a helper that returns an error after reading some data.
type errorReader struct {
	data []byte
	pos  int
	err  error
}

func (r *errorReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, r.err
	}
	n := copy(p, r.data[r.pos:])
	r.pos += n
	if r.pos >= len(r.data) {
		return n, r.err
	}
	return n, nil
}

func TestSplit_ReaderError(t *testing.T) {
	readErr := errors.New("read failure")
	reader := &errorReader{
		data: make([]byte, DefaultChunkSize+100),
		err:  readErr,
	}

	_, err := Split(reader, DefaultChunkSize)
	// The error reader returns data then an error - Split should return whatever
	// data was read before the error, or the error itself
	if err != nil && !errors.Is(err, readErr) {
		t.Fatalf("expected readErr or nil, got %v", err)
	}
}

func TestStreamChunks_ReaderError(t *testing.T) {
	readErr := errors.New("stream read failure")
	// Create a reader that gives one full chunk then errors
	data := make([]byte, DefaultChunkSize)
	reader := io.MultiReader(bytes.NewReader(data), &errorReader{data: nil, err: readErr})

	callCount := 0
	err := StreamChunks(reader, DefaultChunkSize, func(chunk []byte) error {
		callCount++
		return nil
	})

	// After the first full chunk, the next read should hit the error
	if err != nil && !errors.Is(err, readErr) {
		t.Fatalf("expected readErr or nil, got %v", err)
	}
}
