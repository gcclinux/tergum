package crypto

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"testing"
)

func TestDeriveKey_Produces32Bytes(t *testing.T) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		t.Fatal(err)
	}

	enc := NewEncryptor()
	key, err := enc.DeriveKey("my-strong-passphrase", salt)
	if err != nil {
		t.Fatalf("DeriveKey returned error: %v", err)
	}
	if len(key) != 32 {
		t.Errorf("expected 32 bytes, got %d", len(key))
	}
}

func TestDeriveKey_SamePassphraseSameSalt_SameKey(t *testing.T) {
	salt := []byte("fixed-salt-value")

	enc := NewEncryptor()
	key1, err := enc.DeriveKey("test-passphrase", salt)
	if err != nil {
		t.Fatal(err)
	}
	key2, err := enc.DeriveKey("test-passphrase", salt)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(key1, key2) {
		t.Error("same passphrase+salt produced different keys")
	}
}

func TestDeriveKey_DifferentPassphrase_DifferentKey(t *testing.T) {
	salt := []byte("shared-salt")

	enc := NewEncryptor()
	key1, err := enc.DeriveKey("passphrase-one", salt)
	if err != nil {
		t.Fatal(err)
	}
	key2, err := enc.DeriveKey("passphrase-two", salt)
	if err != nil {
		t.Fatal(err)
	}

	if bytes.Equal(key1, key2) {
		t.Error("different passphrases produced the same key")
	}
}

func TestDeriveKey_DifferentSalt_DifferentKey(t *testing.T) {
	enc := NewEncryptor()
	key1, err := enc.DeriveKey("same-passphrase", []byte("salt-alpha"))
	if err != nil {
		t.Fatal(err)
	}
	key2, err := enc.DeriveKey("same-passphrase", []byte("salt-bravo"))
	if err != nil {
		t.Fatal(err)
	}

	if bytes.Equal(key1, key2) {
		t.Error("different salts produced the same key")
	}
}

func TestDeriveKey_EmptyPassphrase_Error(t *testing.T) {
	enc := NewEncryptor()
	_, err := enc.DeriveKey("", []byte("some-salt"))
	if err == nil {
		t.Error("expected error for empty passphrase")
	}
}

func TestDeriveKey_EmptySalt_Error(t *testing.T) {
	enc := NewEncryptor()
	_, err := enc.DeriveKey("passphrase", []byte{})
	if err == nil {
		t.Error("expected error for empty salt")
	}
}

func TestEncryptDecrypt_RoundTrip(t *testing.T) {
	enc := NewEncryptor()
	masterKey := make([]byte, 32)
	if _, err := rand.Read(masterKey); err != nil {
		t.Fatal(err)
	}

	plaintext := []byte("hello, tergum encryption test!")

	ciphertext, wrappedDEK, nonce, err := enc.Encrypt(plaintext, masterKey)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	// Verify ciphertext structure: [12-byte nonce][encrypted data][16-byte tag]
	if len(ciphertext) < nonceSize+tagSize {
		t.Fatalf("ciphertext too short: %d bytes", len(ciphertext))
	}

	// Verify nonce matches the prefix of ciphertext
	if !bytes.Equal(nonce, ciphertext[:nonceSize]) {
		t.Error("nonce does not match ciphertext prefix")
	}

	// Verify wrapped DEK is 40 bytes (32-byte DEK + 8-byte AES-KW IV)
	if len(wrappedDEK) != 40 {
		t.Errorf("expected wrapped DEK of 40 bytes, got %d", len(wrappedDEK))
	}

	// Decrypt and verify round-trip
	decrypted, err := enc.Decrypt(ciphertext, wrappedDEK, nonce, masterKey)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}

	if !bytes.Equal(plaintext, decrypted) {
		t.Errorf("decrypted text does not match original\ngot:  %q\nwant: %q", decrypted, plaintext)
	}
}

func TestEncryptDecrypt_EmptyPlaintext(t *testing.T) {
	enc := NewEncryptor()
	masterKey := make([]byte, 32)
	if _, err := rand.Read(masterKey); err != nil {
		t.Fatal(err)
	}

	plaintext := []byte{}

	ciphertext, wrappedDEK, nonce, err := enc.Encrypt(plaintext, masterKey)
	if err != nil {
		t.Fatalf("Encrypt failed for empty plaintext: %v", err)
	}

	decrypted, err := enc.Decrypt(ciphertext, wrappedDEK, nonce, masterKey)
	if err != nil {
		t.Fatalf("Decrypt failed for empty plaintext: %v", err)
	}

	if !bytes.Equal(plaintext, decrypted) {
		t.Error("round-trip failed for empty plaintext")
	}
}

func TestEncryptDecrypt_LargePlaintext(t *testing.T) {
	enc := NewEncryptor()
	masterKey := make([]byte, 32)
	if _, err := rand.Read(masterKey); err != nil {
		t.Fatal(err)
	}

	// 1MB plaintext
	plaintext := make([]byte, 1<<20)
	if _, err := rand.Read(plaintext); err != nil {
		t.Fatal(err)
	}

	ciphertext, wrappedDEK, nonce, err := enc.Encrypt(plaintext, masterKey)
	if err != nil {
		t.Fatalf("Encrypt failed for large plaintext: %v", err)
	}

	decrypted, err := enc.Decrypt(ciphertext, wrappedDEK, nonce, masterKey)
	if err != nil {
		t.Fatalf("Decrypt failed for large plaintext: %v", err)
	}

	if !bytes.Equal(plaintext, decrypted) {
		t.Error("round-trip failed for large plaintext")
	}
}

func TestDecrypt_WrongMasterKey_Fails(t *testing.T) {
	enc := NewEncryptor()
	masterKey := make([]byte, 32)
	if _, err := rand.Read(masterKey); err != nil {
		t.Fatal(err)
	}

	plaintext := []byte("secret data")

	ciphertext, wrappedDEK, nonce, err := enc.Encrypt(plaintext, masterKey)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	// Try decrypting with a different master key
	wrongKey := make([]byte, 32)
	if _, err := rand.Read(wrongKey); err != nil {
		t.Fatal(err)
	}

	_, err = enc.Decrypt(ciphertext, wrappedDEK, nonce, wrongKey)
	if err == nil {
		t.Error("expected error when decrypting with wrong master key")
	}
}

func TestDecrypt_CorruptedCiphertext_Fails(t *testing.T) {
	enc := NewEncryptor()
	masterKey := make([]byte, 32)
	if _, err := rand.Read(masterKey); err != nil {
		t.Fatal(err)
	}

	plaintext := []byte("data to corrupt")

	ciphertext, wrappedDEK, nonce, err := enc.Encrypt(plaintext, masterKey)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	// Corrupt ciphertext by flipping a byte in the encrypted data portion
	corrupted := make([]byte, len(ciphertext))
	copy(corrupted, ciphertext)
	if len(corrupted) > nonceSize+1 {
		corrupted[nonceSize+1] ^= 0xFF
	}

	_, err = enc.Decrypt(corrupted, wrappedDEK, nonce, masterKey)
	if err == nil {
		t.Error("expected error when decrypting corrupted ciphertext")
	}
}

func TestEncrypt_InvalidMasterKeyLength(t *testing.T) {
	enc := NewEncryptor()

	_, _, _, err := enc.Encrypt([]byte("test"), make([]byte, 16))
	if err == nil {
		t.Error("expected error for 16-byte master key")
	}

	_, _, _, err = enc.Encrypt([]byte("test"), make([]byte, 64))
	if err == nil {
		t.Error("expected error for 64-byte master key")
	}
}

func TestDecrypt_InvalidMasterKeyLength(t *testing.T) {
	enc := NewEncryptor()

	_, err := enc.Decrypt(make([]byte, 50), make([]byte, 40), make([]byte, 12), make([]byte, 16))
	if err == nil {
		t.Error("expected error for 16-byte master key")
	}
}

func TestEncrypt_CiphertextFormat(t *testing.T) {
	enc := NewEncryptor()
	masterKey := make([]byte, 32)
	if _, err := rand.Read(masterKey); err != nil {
		t.Fatal(err)
	}

	plaintext := []byte("format check")

	ciphertext, _, nonce, err := enc.Encrypt(plaintext, masterKey)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	// Verify format: [12-byte nonce][ciphertext][16-byte GCM tag]
	// Total length = 12 (nonce) + len(plaintext) + 16 (tag)
	expectedLen := nonceSize + len(plaintext) + tagSize
	if len(ciphertext) != expectedLen {
		t.Errorf("ciphertext length = %d, want %d", len(ciphertext), expectedLen)
	}

	// First 12 bytes should be the nonce
	if !bytes.Equal(ciphertext[:nonceSize], nonce) {
		t.Error("ciphertext does not start with the nonce")
	}
}

func TestAESKeyWrapUnwrap_RoundTrip(t *testing.T) {
	kek := make([]byte, 32)
	if _, err := rand.Read(kek); err != nil {
		t.Fatal(err)
	}

	plaintext := make([]byte, 32) // 256-bit key to wrap
	if _, err := rand.Read(plaintext); err != nil {
		t.Fatal(err)
	}

	wrapped, err := aesKeyWrap(kek, plaintext)
	if err != nil {
		t.Fatalf("aesKeyWrap failed: %v", err)
	}

	// Wrapped output should be 40 bytes for a 32-byte input
	if len(wrapped) != 40 {
		t.Errorf("wrapped length = %d, want 40", len(wrapped))
	}

	unwrapped, err := aesKeyUnwrap(kek, wrapped)
	if err != nil {
		t.Fatalf("aesKeyUnwrap failed: %v", err)
	}

	if !bytes.Equal(plaintext, unwrapped) {
		t.Error("key wrap/unwrap round-trip failed")
	}
}

func TestAESKeyUnwrap_WrongKey_Fails(t *testing.T) {
	kek := make([]byte, 32)
	if _, err := rand.Read(kek); err != nil {
		t.Fatal(err)
	}

	plaintext := make([]byte, 32)
	if _, err := rand.Read(plaintext); err != nil {
		t.Fatal(err)
	}

	wrapped, err := aesKeyWrap(kek, plaintext)
	if err != nil {
		t.Fatal(err)
	}

	// Try unwrapping with a different key
	wrongKEK := make([]byte, 32)
	if _, err := rand.Read(wrongKEK); err != nil {
		t.Fatal(err)
	}

	_, err = aesKeyUnwrap(wrongKEK, wrapped)
	if err == nil {
		t.Error("expected error when unwrapping with wrong key")
	}
}

func TestVerifyMasterKey(t *testing.T) {
	enc := NewEncryptor()
	masterKey := make([]byte, 32)
	if _, err := rand.Read(masterKey); err != nil {
		t.Fatal(err)
	}

	// Encrypt verification token
	verificationPlaintext := []byte("tergum-key-verification")
	ciphertext, wrappedDEK, nonce, err := enc.Encrypt(verificationPlaintext, masterKey)
	if err != nil {
		t.Fatal(err)
	}

	// format: hex(ciphertext):hex(wrappedDEK):hex(nonce)
	verifyData := fmt.Sprintf("%s:%s:%s",
		hex.EncodeToString(ciphertext),
		hex.EncodeToString(wrappedDEK),
		hex.EncodeToString(nonce),
	)

	// Verify with correct master key
	ok, err := enc.VerifyMasterKey(masterKey, verifyData)
	if err != nil {
		t.Errorf("VerifyMasterKey failed with correct key: %v", err)
	}
	if !ok {
		t.Error("VerifyMasterKey returned false with correct key")
	}

	// Verify with wrong master key
	wrongKey := make([]byte, 32)
	if _, err := rand.Read(wrongKey); err != nil {
		t.Fatal(err)
	}
	ok, err = enc.VerifyMasterKey(wrongKey, verifyData)
	if err == nil {
		t.Error("expected error with wrong master key")
	}
	if ok {
		t.Error("VerifyMasterKey returned true with wrong master key")
	}

	// Verify with invalid token format
	ok, err = enc.VerifyMasterKey(masterKey, "invalid:format")
	if err == nil {
		t.Error("expected error for invalid token format")
	}
	if ok {
		t.Error("VerifyMasterKey returned true for invalid format")
	}
}

