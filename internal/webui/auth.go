// Package webui provides an embedded web management interface for Tergum.
package webui

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net/http"
	"sync"
	"time"

	"golang.org/x/crypto/argon2"
)

// Argon2idParams holds the parameters for Argon2id password hashing.
type Argon2idParams struct {
	Memory      uint32
	Iterations  uint32
	Parallelism uint8
	SaltLength  uint32
	KeyLength   uint32
}

// DefaultArgon2idParams returns the recommended Argon2id parameters.
func DefaultArgon2idParams() *Argon2idParams {
	return &Argon2idParams{
		Memory:      64 * 1024, // 64 MB
		Iterations:  3,
		Parallelism: 2,
		SaltLength:  16,
		KeyLength:   32,
	}
}

// HashedPassword stores a password hash along with its parameters and salt.
type HashedPassword struct {
	Hash        []byte
	Salt        []byte
	Params      *Argon2idParams
}

// HashPassword creates an Argon2id hash of the given password.
func HashPassword(password string, params *Argon2idParams) (*HashedPassword, error) {
	salt := make([]byte, params.SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("generating salt: %w", err)
	}

	hash := argon2.IDKey([]byte(password), salt, params.Iterations, params.Memory, params.Parallelism, params.KeyLength)

	return &HashedPassword{
		Hash:   hash,
		Salt:   salt,
		Params: params,
	}, nil
}

// VerifyPassword checks whether the provided password matches the stored hash.
func VerifyPassword(password string, hashed *HashedPassword) bool {
	hash := argon2.IDKey(
		[]byte(password),
		hashed.Salt,
		hashed.Params.Iterations,
		hashed.Params.Memory,
		hashed.Params.Parallelism,
		hashed.Params.KeyLength,
	)
	return subtle.ConstantTimeCompare(hash, hashed.Hash) == 1
}

// Session represents an authenticated user session.
type Session struct {
	ID        string
	Username  string
	CreatedAt time.Time
	ExpiresAt time.Time
}

// SessionStore manages authenticated sessions with configurable timeout.
type SessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	timeout  time.Duration
}

// NewSessionStore creates a new session store with the given timeout duration.
func NewSessionStore(timeout time.Duration) *SessionStore {
	return &SessionStore{
		sessions: make(map[string]*Session),
		timeout:  timeout,
	}
}

// Create generates a new session for the given username.
func (s *SessionStore) Create(username string) (*Session, error) {
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, fmt.Errorf("generating session token: %w", err)
	}

	token := base64.URLEncoding.EncodeToString(tokenBytes)
	now := time.Now()

	session := &Session{
		ID:        token,
		Username:  username,
		CreatedAt: now,
		ExpiresAt: now.Add(s.timeout),
	}

	s.mu.Lock()
	s.sessions[token] = session
	s.mu.Unlock()

	return session, nil
}

// Get retrieves a session by token, returning nil if expired or not found.
func (s *SessionStore) Get(token string) *Session {
	s.mu.RLock()
	session, ok := s.sessions[token]
	s.mu.RUnlock()

	if !ok {
		return nil
	}

	if time.Now().After(session.ExpiresAt) {
		s.Delete(token)
		return nil
	}

	return session
}

// Delete removes a session by token.
func (s *SessionStore) Delete(token string) {
	s.mu.Lock()
	delete(s.sessions, token)
	s.mu.Unlock()
}

// Cleanup removes expired sessions. Should be called periodically.
func (s *SessionStore) Cleanup() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for token, session := range s.sessions {
		if now.After(session.ExpiresAt) {
			delete(s.sessions, token)
		}
	}
}

// AuthMiddleware provides HTTP Basic Auth middleware backed by Argon2id-hashed credentials.
type AuthMiddleware struct {
	Username       string
	HashedPassword *HashedPassword
	Sessions       *SessionStore
}

// NewAuthMiddleware creates an auth middleware with the given credentials.
// The password should be the plaintext password which will be verified against the hash.
func NewAuthMiddleware(username string, hashedPwd *HashedPassword, sessions *SessionStore) *AuthMiddleware {
	return &AuthMiddleware{
		Username:       username,
		HashedPassword: hashedPwd,
		Sessions:       sessions,
	}
}

// Wrap returns an HTTP handler that enforces authentication before calling the next handler.
// Authentication is checked via session cookie first, then HTTP Basic Auth as fallback.
func (a *AuthMiddleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check session cookie first.
		if cookie, err := r.Cookie("tergum_session"); err == nil {
			if session := a.Sessions.Get(cookie.Value); session != nil {
				next.ServeHTTP(w, r)
				return
			}
		}

		// Fall back to HTTP Basic Auth.
		user, pass, ok := r.BasicAuth()
		if !ok {
			w.Header().Set("WWW-Authenticate", `Basic realm="Tergum"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		if subtle.ConstantTimeCompare([]byte(user), []byte(a.Username)) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="Tergum"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		if !VerifyPassword(pass, a.HashedPassword) {
			w.Header().Set("WWW-Authenticate", `Basic realm="Tergum"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Successful auth — create a session.
		session, err := a.Sessions.Create(user)
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		http.SetCookie(w, &http.Cookie{
			Name:     "tergum_session",
			Value:    session.ID,
			Path:     "/",
			HttpOnly: true,
			Secure:   true,
			SameSite: http.SameSiteStrictMode,
			Expires:  session.ExpiresAt,
		})

		next.ServeHTTP(w, r)
	})
}
