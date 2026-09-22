package cmd

import (
	"bytes"
	"context"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gcclinux/tergum/internal/config"
	"github.com/gcclinux/tergum/internal/crypto"
	"github.com/gcclinux/tergum/internal/db"
	"github.com/gcclinux/tergum/internal/identity"
)

func TestRecoverCommandRegistration(t *testing.T) {
	cmd := newRecoverCmd()
	if cmd.Use != "recover" {
		t.Errorf("expected command Use 'recover', got %q", cmd.Use)
	}

	flags := cmd.Flags()
	if flags.Lookup("server") == nil {
		t.Error("missing --server flag")
	}
	if flags.Lookup("client-id") == nil {
		t.Error("missing --client-id flag")
	}
	if flags.Lookup("force") == nil {
		t.Error("missing --force flag")
	}
}

func TestRecoverOSCompatibilityGuards(t *testing.T) {
	tests := []struct {
		name        string
		storedOS    string
		currentOS   string
		shouldError bool
	}{
		{"LinuxToLinux", "linux", "linux", false},
		{"WindowsToWindows", "windows", "windows", false},
		{"DarwinToDarwin", "darwin", "darwin", false},
		{"WindowsToLinux", "windows", "linux", true},
		{"LinuxToWindows", "linux", "windows", true},
		{"DarwinToLinux", "darwin", "linux", true},
		{"LinuxToDarwin", "linux", "darwin", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := identity.CheckOSCompatibility(tt.storedOS, tt.currentOS)
			if tt.shouldError && err == nil {
				t.Errorf("expected error for %s -> %s, got nil", tt.storedOS, tt.currentOS)
			}
			if !tt.shouldError && err != nil {
				t.Errorf("expected no error for %s -> %s, got %v", tt.storedOS, tt.currentOS, err)
			}
		})
	}
}

func TestRecoverWizard_PassphraseAndConfigReconstruction(t *testing.T) {
	tempDir := t.TempDir()
	origAppData := os.Getenv("APPDATA")
	origHome := os.Getenv("HOME")
	os.Setenv("APPDATA", tempDir)
	os.Setenv("HOME", tempDir)
	defer func() {
		os.Setenv("APPDATA", origAppData)
		os.Setenv("HOME", origHome)
	}()

	configDir := config.DefaultConfigDir()
	_ = os.MkdirAll(configDir, 0700)

	// Simulate restored database
	dbPath := filepath.Join(configDir, "tergum.db")
	repo, err := db.NewRepository(dbPath, true)
	if err != nil {
		t.Fatalf("creating test db: %v", err)
	}

	ctx := context.Background()
	_ = repo.AddIncludePath(ctx, "/home/user/documents")
	_ = repo.AddIncludePath(ctx, "/home/user/code")
	_ = repo.AddExcludePattern(ctx, "*.tmp")
	_ = repo.AddExcludePattern(ctx, "node_modules/")

	// Setup encryption verification
	enc := crypto.NewEncryptor()
	salt := []byte("0123456789abcdef")
	saltHex := hex.EncodeToString(salt)
	_ = repo.SetConfig(ctx, "encryption_salt", saltHex)

	masterKey, err := enc.DeriveKey("mysecretpassphrase", salt)
	if err != nil {
		t.Fatalf("DeriveKey failed: %v", err)
	}

	ciphertext, wrappedDEK, nonce, err := enc.Encrypt([]byte("tergum-key-verification"), masterKey)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}
	verifyData := hex.EncodeToString(ciphertext) + ":" + hex.EncodeToString(wrappedDEK) + ":" + hex.EncodeToString(nonce)
	_ = repo.SetConfig(ctx, "key_verify", verifyData)
	repo.Close()

	// Verify database content
	repoCheck, err := db.NewRepository(dbPath, true)
	if err != nil {
		t.Fatalf("reopening db: %v", err)
	}
	defer repoCheck.Close()

	includes, err := repoCheck.ListIncludePaths(ctx)
	if err != nil {
		t.Fatalf("ListIncludePaths error: %v", err)
	}
	if len(includes) != 2 {
		t.Errorf("expected 2 include paths, got %d", len(includes))
	}

	excludes, err := repoCheck.ListExcludePatterns(ctx)
	if err != nil {
		t.Fatalf("ListExcludePatterns error: %v", err)
	}
	if len(excludes) != 2 {
		t.Errorf("expected 2 exclude patterns, got %d", len(excludes))
	}

	storedSalt, _ := repoCheck.GetConfig(ctx, "encryption_salt")
	if storedSalt != saltHex {
		t.Errorf("salt mismatch: got %q, want %q", storedSalt, saltHex)
	}

	storedVerify, _ := repoCheck.GetConfig(ctx, "key_verify")
	if storedVerify != verifyData {
		t.Errorf("key_verify mismatch: got %q, want %q", storedVerify, verifyData)
	}

	// Verify that the passphrase works with the stored verify token
	derivedCheck, err := enc.DeriveKey("mysecretpassphrase", salt)
	if err != nil {
		t.Fatalf("DeriveKey failed: %v", err)
	}
	ok, _ := enc.VerifyMasterKey(derivedCheck, storedVerify)
	if !ok {
		t.Errorf("expected passphrase verification to succeed")
	}

	// Verify bad passphrase fails
	badKey, _ := enc.DeriveKey("wrongpassphrase", salt)
	okBad, _ := enc.VerifyMasterKey(badKey, storedVerify)
	if okBad {
		t.Errorf("expected wrong passphrase verification to fail")
	}
}

func TestRecoverWizard_PromptInteraction(t *testing.T) {
	input := "192.168.1.100\n1\nmysecretpass\n"
	var output bytes.Buffer

	wiz := newSetupWizard(strings.NewReader(input), &output)
	server := wiz.prompt("Server address", "")
	if server != "192.168.1.100" {
		t.Errorf("got %q, want 192.168.1.100", server)
	}

	clientChoice := wiz.prompt("Select client", "1")
	if clientChoice != "1" {
		t.Errorf("got %q, want 1", clientChoice)
	}

	pass := wiz.prompt("Enter passphrase", "")
	if pass != "mysecretpass" {
		t.Errorf("got %q, want mysecretpass", pass)
	}
}
