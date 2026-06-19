package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters for key derivation.
const (
	argon2Time    = 1
	argon2Memory  = 64 * 1024 // 64MB
	argon2Threads = 4
	argon2KeyLen  = 32 // 256-bit key
)

// AES-GCM constants.
const (
	nonceSize = 12 // 12-byte nonce for AES-GCM
	tagSize   = 16 // 16-byte GCM authentication tag
	dekSize   = 32 // 256-bit DEK
)

// AES-KW (RFC 3394) constants.
const (
	aesKWBlockSize = 8
	aesKWIV        = 0xA6A6A6A6A6A6A6A6 // Default Initial Value per RFC 3394
)

// Encryptor defines the interface for encryption operations.
type Encryptor interface {
	// Encrypt encrypts data with a new random DEK, returns encrypted data + wrapped DEK + nonce.
	// The ciphertext has the format: [12-byte nonce][ciphertext][16-byte GCM tag].
	Encrypt(plaintext []byte, masterKey []byte) (ciphertext []byte, wrappedDEK []byte, nonce []byte, err error)
	// Decrypt decrypts data using wrapped DEK.
	Decrypt(ciphertext []byte, wrappedDEK []byte, nonce []byte, masterKey []byte) ([]byte, error)
	// DeriveKey derives master key from passphrase using Argon2id.
	DeriveKey(passphrase string, salt []byte) ([]byte, error)
}

// AESEncryptor implements the Encryptor interface using AES-256-GCM with per-file DEKs.
type AESEncryptor struct{}

// NewEncryptor creates a new AESEncryptor instance.
func NewEncryptor() *AESEncryptor {
	return &AESEncryptor{}
}

// DeriveKey derives a 256-bit master key from a passphrase using Argon2id.
func (e *AESEncryptor) DeriveKey(passphrase string, salt []byte) ([]byte, error) {
	if passphrase == "" {
		return nil, errors.New("passphrase must not be empty")
	}
	if len(salt) == 0 {
		return nil, errors.New("salt must not be empty")
	}

	key := argon2.IDKey([]byte(passphrase), salt, argon2Time, argon2Memory, argon2Threads, argon2KeyLen)
	return key, nil
}

// Encrypt encrypts plaintext with a new random DEK using AES-256-GCM.
// Returns: ciphertext ([nonce][encrypted][tag]), wrapped DEK, and nonce.
func (e *AESEncryptor) Encrypt(plaintext []byte, masterKey []byte) ([]byte, []byte, []byte, error) {
	if len(masterKey) != 32 {
		return nil, nil, nil, fmt.Errorf("master key must be 32 bytes, got %d", len(masterKey))
	}

	// Generate random 256-bit DEK.
	dek := make([]byte, dekSize)
	if _, err := io.ReadFull(rand.Reader, dek); err != nil {
		return nil, nil, nil, fmt.Errorf("failed to generate DEK: %w", err)
	}

	// Generate random 12-byte nonce.
	nonce := make([]byte, nonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, nil, fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Encrypt plaintext with AES-256-GCM using the DEK.
	block, err := aes.NewCipher(dek)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to create AES cipher: %w", err)
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	// Seal appends the ciphertext+tag to the nonce prefix.
	// Result format: [12-byte nonce][ciphertext][16-byte GCM tag]
	sealed := aesGCM.Seal(nil, nonce, plaintext, nil)
	ciphertext := make([]byte, nonceSize+len(sealed))
	copy(ciphertext[:nonceSize], nonce)
	copy(ciphertext[nonceSize:], sealed)

	// Wrap DEK with master key using AES-KW (RFC 3394).
	wrappedDEK, err := aesKeyWrap(masterKey, dek)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to wrap DEK: %w", err)
	}

	return ciphertext, wrappedDEK, nonce, nil
}

// Decrypt decrypts ciphertext using a wrapped DEK and master key.
// The ciphertext must have the format: [12-byte nonce][ciphertext][16-byte GCM tag].
func (e *AESEncryptor) Decrypt(ciphertext []byte, wrappedDEK []byte, nonce []byte, masterKey []byte) ([]byte, error) {
	if len(masterKey) != 32 {
		return nil, fmt.Errorf("master key must be 32 bytes, got %d", len(masterKey))
	}
	if len(ciphertext) < nonceSize+tagSize {
		return nil, errors.New("ciphertext too short")
	}

	// Unwrap DEK using master key.
	dek, err := aesKeyUnwrap(masterKey, wrappedDEK)
	if err != nil {
		return nil, fmt.Errorf("failed to unwrap DEK: %w", err)
	}

	// Extract nonce from ciphertext prefix.
	extractedNonce := ciphertext[:nonceSize]
	sealed := ciphertext[nonceSize:]

	// Decrypt with AES-256-GCM.
	block, err := aes.NewCipher(dek)
	if err != nil {
		return nil, fmt.Errorf("failed to create AES cipher: %w", err)
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	plaintext, err := aesGCM.Open(nil, extractedNonce, sealed, nil)
	if err != nil {
		return nil, fmt.Errorf("decryption failed (authentication error): %w", err)
	}

	return plaintext, nil
}

// aesKeyWrap implements AES Key Wrap (RFC 3394).
// Wraps plaintext key data using the provided KEK (Key Encryption Key).
// Input plaintext must be a multiple of 8 bytes and at least 16 bytes.
// Output is 8 bytes longer than input (includes integrity check value).
func aesKeyWrap(kek, plaintext []byte) ([]byte, error) {
	n := len(plaintext) / aesKWBlockSize
	if len(plaintext)%aesKWBlockSize != 0 || n < 2 {
		return nil, errors.New("plaintext must be a multiple of 8 bytes and at least 16 bytes")
	}

	block, err := aes.NewCipher(kek)
	if err != nil {
		return nil, err
	}

	// Initialize: Set A to the IV, split plaintext into 8-byte blocks R[1..n].
	a := make([]byte, 8)
	binary.BigEndian.PutUint64(a, aesKWIV)

	r := make([][]byte, n)
	for i := 0; i < n; i++ {
		r[i] = make([]byte, 8)
		copy(r[i], plaintext[i*8:(i+1)*8])
	}

	// Wrap: 6 rounds of n iterations.
	buf := make([]byte, 16)
	for j := 0; j < 6; j++ {
		for i := 0; i < n; i++ {
			// B = AES(K, A || R[i])
			copy(buf[:8], a)
			copy(buf[8:], r[i])
			block.Encrypt(buf, buf)

			// A = MSB(64, B) ^ t where t = (n*j)+i+1
			copy(a, buf[:8])
			t := uint64(n*j + i + 1)
			tBytes := make([]byte, 8)
			binary.BigEndian.PutUint64(tBytes, t)
			for k := 0; k < 8; k++ {
				a[k] ^= tBytes[k]
			}

			// R[i] = LSB(64, B)
			copy(r[i], buf[8:])
		}
	}

	// Output: A || R[1] || R[2] || ... || R[n]
	output := make([]byte, (n+1)*8)
	copy(output[:8], a)
	for i := 0; i < n; i++ {
		copy(output[(i+1)*8:(i+2)*8], r[i])
	}

	return output, nil
}

// aesKeyUnwrap implements AES Key Unwrap (RFC 3394).
// Unwraps ciphertext using the provided KEK.
// Input must be a multiple of 8 bytes and at least 24 bytes.
// Output is 8 bytes shorter than input.
func aesKeyUnwrap(kek, ciphertext []byte) ([]byte, error) {
	n := len(ciphertext)/aesKWBlockSize - 1
	if len(ciphertext)%aesKWBlockSize != 0 || n < 2 {
		return nil, errors.New("ciphertext must be a multiple of 8 bytes and at least 24 bytes")
	}

	block, err := aes.NewCipher(kek)
	if err != nil {
		return nil, err
	}

	// Initialize: A = C[0], R[i] = C[i] for i=1..n
	a := make([]byte, 8)
	copy(a, ciphertext[:8])

	r := make([][]byte, n)
	for i := 0; i < n; i++ {
		r[i] = make([]byte, 8)
		copy(r[i], ciphertext[(i+1)*8:(i+2)*8])
	}

	// Unwrap: 6 rounds of n iterations (reverse order).
	buf := make([]byte, 16)
	for j := 5; j >= 0; j-- {
		for i := n - 1; i >= 0; i-- {
			// A ^ t
			t := uint64(n*j + i + 1)
			tBytes := make([]byte, 8)
			binary.BigEndian.PutUint64(tBytes, t)
			for k := 0; k < 8; k++ {
				a[k] ^= tBytes[k]
			}

			// B = AES-1(K, (A ^ t) || R[i])
			copy(buf[:8], a)
			copy(buf[8:], r[i])
			block.Decrypt(buf, buf)

			// A = MSB(64, B)
			copy(a, buf[:8])
			// R[i] = LSB(64, B)
			copy(r[i], buf[8:])
		}
	}

	// Verify integrity check value.
	expectedIV := make([]byte, 8)
	binary.BigEndian.PutUint64(expectedIV, aesKWIV)
	for i := 0; i < 8; i++ {
		if a[i] != expectedIV[i] {
			return nil, errors.New("key unwrap integrity check failed")
		}
	}

	// Output: R[1] || R[2] || ... || R[n]
	output := make([]byte, n*8)
	for i := 0; i < n; i++ {
		copy(output[i*8:(i+1)*8], r[i])
	}

	return output, nil
}
