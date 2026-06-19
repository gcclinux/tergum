package webui

import (
	"testing"
	"time"
)

func TestHashPassword(t *testing.T) {
	params := DefaultArgon2idParams()
	hashed, err := HashPassword("testpassword", params)
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}

	if len(hashed.Hash) != int(params.KeyLength) {
		t.Errorf("hash length = %d, want %d", len(hashed.Hash), params.KeyLength)
	}
	if len(hashed.Salt) != int(params.SaltLength) {
		t.Errorf("salt length = %d, want %d", len(hashed.Salt), params.SaltLength)
	}
}

func TestHashPassword_UniqueSalts(t *testing.T) {
	params := DefaultArgon2idParams()
	h1, _ := HashPassword("same", params)
	h2, _ := HashPassword("same", params)

	// Different salts mean different hashes even for same password.
	if string(h1.Salt) == string(h2.Salt) {
		t.Error("expected different salts for separate calls")
	}
}

func TestVerifyPassword_Correct(t *testing.T) {
	params := DefaultArgon2idParams()
	hashed, _ := HashPassword("mypassword", params)

	if !VerifyPassword("mypassword", hashed) {
		t.Error("VerifyPassword should return true for correct password")
	}
}

func TestVerifyPassword_Wrong(t *testing.T) {
	params := DefaultArgon2idParams()
	hashed, _ := HashPassword("mypassword", params)

	if VerifyPassword("wrongpassword", hashed) {
		t.Error("VerifyPassword should return false for wrong password")
	}
}

func TestVerifyPassword_EmptyPassword(t *testing.T) {
	params := DefaultArgon2idParams()
	hashed, _ := HashPassword("", params)

	if !VerifyPassword("", hashed) {
		t.Error("VerifyPassword should verify empty password")
	}
	if VerifyPassword("notempty", hashed) {
		t.Error("VerifyPassword should reject non-empty when empty was hashed")
	}
}

func TestSessionStore_Create(t *testing.T) {
	store := NewSessionStore(1 * time.Hour)
	session, err := store.Create("admin")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if session.Username != "admin" {
		t.Errorf("username = %q, want %q", session.Username, "admin")
	}
	if session.ID == "" {
		t.Error("session ID should not be empty")
	}
	if session.ExpiresAt.Before(time.Now()) {
		t.Error("session should not be expired immediately")
	}
}

func TestSessionStore_Get(t *testing.T) {
	store := NewSessionStore(1 * time.Hour)
	session, _ := store.Create("admin")

	got := store.Get(session.ID)
	if got == nil {
		t.Fatal("Get() returned nil for valid session")
	}
	if got.Username != "admin" {
		t.Errorf("username = %q, want %q", got.Username, "admin")
	}
}

func TestSessionStore_GetExpired(t *testing.T) {
	store := NewSessionStore(1 * time.Millisecond)
	session, _ := store.Create("admin")

	// Wait for expiry.
	time.Sleep(5 * time.Millisecond)

	got := store.Get(session.ID)
	if got != nil {
		t.Error("Get() should return nil for expired session")
	}
}

func TestSessionStore_GetInvalid(t *testing.T) {
	store := NewSessionStore(1 * time.Hour)
	got := store.Get("nonexistent-token")
	if got != nil {
		t.Error("Get() should return nil for invalid token")
	}
}

func TestSessionStore_Delete(t *testing.T) {
	store := NewSessionStore(1 * time.Hour)
	session, _ := store.Create("admin")

	store.Delete(session.ID)

	got := store.Get(session.ID)
	if got != nil {
		t.Error("Get() should return nil after Delete()")
	}
}

func TestSessionStore_Cleanup(t *testing.T) {
	store := NewSessionStore(1 * time.Millisecond)
	store.Create("user1")
	store.Create("user2")

	time.Sleep(5 * time.Millisecond)
	store.Cleanup()

	store.mu.RLock()
	count := len(store.sessions)
	store.mu.RUnlock()

	if count != 0 {
		t.Errorf("expected 0 sessions after cleanup, got %d", count)
	}
}
