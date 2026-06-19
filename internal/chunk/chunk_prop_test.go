package chunk

import (
	"bytes"
	"testing"

	"pgregory.net/rapid"
)

// **Validates: Requirements 2.3, 2.4**

// TestProperty_ChunkingRoundTrip verifies that for any byte sequence of arbitrary
// length, splitting it into 64KB chunks and then concatenating all chunks in order
// produces the original byte sequence with identical content and length.
func TestProperty_ChunkingRoundTrip(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate a random length between 0 and 1MB for most iterations.
		// This keeps test execution time reasonable while covering edge cases.
		size := rapid.IntRange(0, 1*1024*1024).Draw(rt, "size")

		// Generate random byte data of that length
		data := rapid.SliceOfN(rapid.Byte(), size, size).Draw(rt, "data")

		// Split the data into chunks using the default 64KB chunk size
		reader := bytes.NewReader(data)
		chunks, err := Split(reader, DefaultChunkSize)
		if err != nil {
			rt.Fatalf("Split failed: %v", err)
		}

		// Join the chunks back together
		result := Join(chunks)

		// Verify length is identical
		if len(result) != len(data) {
			rt.Fatalf("round-trip length mismatch: input %d bytes, output %d bytes", len(data), len(result))
		}

		// Verify content is identical
		if !bytes.Equal(result, data) {
			rt.Fatalf("round-trip content mismatch for input of %d bytes", len(data))
		}
	})
}
