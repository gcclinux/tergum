package crypto

import (
	"encoding/hex"
	"io"
	"os"

	"github.com/zeebo/blake3"
)

// HashFile computes the BLAKE3 hash of the file at the given path.
// Returns the hex-encoded 256-bit hash string (64 lowercase hex characters).
// Uses streaming to handle large files without loading the entire file into memory.
func HashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := blake3.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}

	sum := h.Sum(nil)
	return hex.EncodeToString(sum), nil
}

// HashBytes computes the BLAKE3 hash of the given byte slice.
// Returns the hex-encoded 256-bit hash string (64 lowercase hex characters).
func HashBytes(data []byte) string {
	sum := blake3.Sum256(data)
	return hex.EncodeToString(sum[:])
}
