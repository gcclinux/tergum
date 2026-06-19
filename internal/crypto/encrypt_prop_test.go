package crypto

import (
	"bytes"
	"testing"

	"pgregory.net/rapid"
)

// **Validates: Requirements 4.1, 4.2, 4.3, 4.4, 4.5**

// TestProperty_EncryptDecryptRoundTrip verifies that for any random plaintext
// (0 to 1MB) and any random 32-byte master key, Encrypt followed by Decrypt
// produces the original plaintext.
func TestProperty_EncryptDecryptRoundTrip(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		enc := NewEncryptor()

		// Generate random plaintext (0 to 1MB)
		size := rapid.IntRange(0, 1<<20).Draw(rt, "plaintextSize")
		plaintext := rapid.SliceOfN(rapid.Byte(), size, size).Draw(rt, "plaintext")

		// Generate random 32-byte master key
		masterKey := rapid.SliceOfN(rapid.Byte(), 32, 32).Draw(rt, "masterKey")

		// Encrypt
		ciphertext, wrappedDEK, nonce, err := enc.Encrypt(plaintext, masterKey)
		if err != nil {
			rt.Fatalf("Encrypt failed: %v", err)
		}

		// Decrypt
		decrypted, err := enc.Decrypt(ciphertext, wrappedDEK, nonce, masterKey)
		if err != nil {
			rt.Fatalf("Decrypt failed: %v", err)
		}

		// Verify round-trip produces original plaintext
		if !bytes.Equal(plaintext, decrypted) {
			rt.Fatalf("round-trip failed: plaintext length %d, decrypted length %d", len(plaintext), len(decrypted))
		}
	})
}

// TestProperty_CiphertextLength verifies that the ciphertext length is always
// 12 (nonce) + len(plaintext) + 16 (GCM tag).
func TestProperty_CiphertextLength(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		enc := NewEncryptor()

		// Generate random plaintext (0 to 1MB)
		size := rapid.IntRange(0, 1<<20).Draw(rt, "plaintextSize")
		plaintext := rapid.SliceOfN(rapid.Byte(), size, size).Draw(rt, "plaintext")

		// Generate random 32-byte master key
		masterKey := rapid.SliceOfN(rapid.Byte(), 32, 32).Draw(rt, "masterKey")

		// Encrypt
		ciphertext, _, _, err := enc.Encrypt(plaintext, masterKey)
		if err != nil {
			rt.Fatalf("Encrypt failed: %v", err)
		}

		// Verify ciphertext length == 12 (nonce) + len(plaintext) + 16 (tag)
		expectedLen := nonceSize + len(plaintext) + tagSize
		if len(ciphertext) != expectedLen {
			rt.Fatalf("ciphertext length = %d, want %d (nonce=%d + plaintext=%d + tag=%d)",
				len(ciphertext), expectedLen, nonceSize, len(plaintext), tagSize)
		}
	})
}

// TestProperty_CiphertextNoncePrefix verifies that the first 12 bytes of the
// ciphertext are equal to the returned nonce.
func TestProperty_CiphertextNoncePrefix(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		enc := NewEncryptor()

		// Generate random plaintext (0 to 1MB)
		size := rapid.IntRange(0, 1<<20).Draw(rt, "plaintextSize")
		plaintext := rapid.SliceOfN(rapid.Byte(), size, size).Draw(rt, "plaintext")

		// Generate random 32-byte master key
		masterKey := rapid.SliceOfN(rapid.Byte(), 32, 32).Draw(rt, "masterKey")

		// Encrypt
		ciphertext, _, nonce, err := enc.Encrypt(plaintext, masterKey)
		if err != nil {
			rt.Fatalf("Encrypt failed: %v", err)
		}

		// Verify first 12 bytes of ciphertext == returned nonce
		if !bytes.Equal(ciphertext[:nonceSize], nonce) {
			rt.Fatalf("ciphertext prefix does not match returned nonce")
		}
	})
}

// TestProperty_DeriveKeyDeterministic verifies that for any random passphrase
// and any random salt, calling DeriveKey twice produces the same result.
func TestProperty_DeriveKeyDeterministic(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		enc := NewEncryptor()

		// Generate random non-empty passphrase (1 to 64 bytes)
		passLen := rapid.IntRange(1, 64).Draw(rt, "passLen")
		passphrase := string(rapid.SliceOfN(rapid.Byte(), passLen, passLen).Draw(rt, "passphrase"))

		// Generate random non-empty salt (1 to 32 bytes)
		saltLen := rapid.IntRange(1, 32).Draw(rt, "saltLen")
		salt := rapid.SliceOfN(rapid.Byte(), saltLen, saltLen).Draw(rt, "salt")

		// Derive key twice with same inputs
		key1, err := enc.DeriveKey(passphrase, salt)
		if err != nil {
			rt.Fatalf("DeriveKey (first call) failed: %v", err)
		}

		key2, err := enc.DeriveKey(passphrase, salt)
		if err != nil {
			rt.Fatalf("DeriveKey (second call) failed: %v", err)
		}

		// Verify same passphrase+salt always derives same master key
		if !bytes.Equal(key1, key2) {
			rt.Fatalf("same passphrase+salt produced different keys")
		}
	})
}

// TestProperty_WrappedDEKSize verifies that the wrapped DEK is always 40 bytes
// (32-byte DEK + 8-byte AES-KW IV).
func TestProperty_WrappedDEKSize(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		enc := NewEncryptor()

		// Generate random plaintext (0 to 1MB)
		size := rapid.IntRange(0, 1<<20).Draw(rt, "plaintextSize")
		plaintext := rapid.SliceOfN(rapid.Byte(), size, size).Draw(rt, "plaintext")

		// Generate random 32-byte master key
		masterKey := rapid.SliceOfN(rapid.Byte(), 32, 32).Draw(rt, "masterKey")

		// Encrypt
		_, wrappedDEK, _, err := enc.Encrypt(plaintext, masterKey)
		if err != nil {
			rt.Fatalf("Encrypt failed: %v", err)
		}

		// Verify wrapped DEK is always 40 bytes (32 + 8 byte AES-KW IV)
		if len(wrappedDEK) != 40 {
			rt.Fatalf("wrapped DEK length = %d, want 40", len(wrappedDEK))
		}
	})
}
