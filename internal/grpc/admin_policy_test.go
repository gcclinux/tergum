package grpc

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gcclinux/tergum/internal/config"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
)

// writeAdminConfig writes a minimal valid tergum.toml with the given admin
// clients and returns its path.
func writeAdminConfig(t *testing.T, clients []config.AdminClient) string {
	t.Helper()
	cfg := &config.Config{}
	// Load defaults onto an empty config by round-tripping through Load after
	// writing a near-empty file, but simpler: construct and Save.
	cfg.Admin.Clients = clients

	dir := t.TempDir()
	path := filepath.Join(dir, "tergum.toml")
	if err := config.Save(path, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	return path
}

func TestConfigAdminPolicy_IsAdmin(t *testing.T) {
	fp := "aa" + repeatHex(62) // 64 hex chars
	path := writeAdminConfig(t, []config.AdminClient{{Name: "ops", Fingerprint: fp}})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := NewConfigAdminPolicy(ctx, path, time.Hour)

	if !p.IsAdmin(fp) {
		t.Errorf("expected %s to be admin", fp)
	}
	if p.IsAdmin("bb" + repeatHex(62)) {
		t.Error("unexpected admin for unknown fingerprint")
	}
	if p.IsAdmin("") {
		t.Error("empty fingerprint must never be admin")
	}
}

func TestConfigAdminPolicy_ReloadPicksUpChanges(t *testing.T) {
	fp1 := "11" + repeatHex(62)
	fp2 := "22" + repeatHex(62)
	path := writeAdminConfig(t, []config.AdminClient{{Name: "a", Fingerprint: fp1}})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := NewConfigAdminPolicy(ctx, path, time.Hour) // long ticker; drive via Reload()

	if !p.IsAdmin(fp1) {
		t.Fatal("fp1 should be admin after initial load")
	}
	if p.IsAdmin(fp2) {
		t.Fatal("fp2 should not be admin yet")
	}

	// Rewrite the config with a different admin and reload synchronously.
	cfg := &config.Config{}
	cfg.Admin.Clients = []config.AdminClient{{Name: "b", Fingerprint: fp2}}
	if err := config.Save(path, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	if err := p.Reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}

	if p.IsAdmin(fp1) {
		t.Error("fp1 should no longer be admin after reload")
	}
	if !p.IsAdmin(fp2) {
		t.Error("fp2 should be admin after reload")
	}
}

func TestConfigAdminPolicy_LastKnownGoodOnReloadFailure(t *testing.T) {
	fp := "33" + repeatHex(62)
	path := writeAdminConfig(t, []config.AdminClient{{Name: "a", Fingerprint: fp}})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := NewConfigAdminPolicy(ctx, path, time.Hour)

	if !p.IsAdmin(fp) {
		t.Fatal("fp should be admin after initial load")
	}

	// Make the config unreadable/invalid by removing it; Reload should error but
	// the last-known-good admin set must be retained.
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove config: %v", err)
	}
	if err := p.Reload(); err == nil {
		t.Fatal("expected reload to fail on missing config")
	}
	if !p.IsAdmin(fp) {
		t.Error("last-known-good admin set should be retained after a failed reload")
	}
}

func TestConfigAdminPolicy_InitialFailureFailsClosed(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Point at a non-existent file: initial load fails -> empty set (fail-closed).
	p := NewConfigAdminPolicy(ctx, filepath.Join(t.TempDir(), "nope.toml"), time.Hour)
	if p.IsAdmin("44" + repeatHex(62)) {
		t.Error("fail-closed policy must not grant admin on initial load failure")
	}
}

// TestClientSPKIFromContext_DistinctFingerprintsSharedCN verifies that two
// client certificates sharing the generic CN "Tergum Client" still yield
// distinct SPKI fingerprints — proving admin identity derives from the key, not
// the shared CN.
func TestClientSPKIFromContext_DistinctFingerprintsSharedCN(t *testing.T) {
	certA := makeSharedCNCert(t)
	certB := makeSharedCNCert(t)

	fpA := spkiCtxFingerprint(t, certA)
	fpB := spkiCtxFingerprint(t, certB)

	if fpA == "" || fpB == "" {
		t.Fatal("fingerprints should be non-empty for verified peer certs")
	}
	if fpA == fpB {
		t.Error("two distinct Ed25519 certs sharing a CN must produce distinct SPKI fingerprints")
	}

	// Verify the fingerprint equals the SHA-256 of the leaf's SPKI.
	want := sha256.Sum256(certA.RawSubjectPublicKeyInfo)
	if fpA != hex.EncodeToString(want[:]) {
		t.Errorf("fingerprint mismatch: got %s", fpA)
	}
}

func TestClientSPKIFromContext_NoPeer(t *testing.T) {
	if fp := clientSPKIFromContext(context.Background()); fp != "" {
		t.Errorf("expected empty fingerprint with no peer, got %q", fp)
	}
}

// spkiCtxFingerprint builds a context carrying the given cert as a verified peer
// and runs clientSPKIFromContext over it.
func spkiCtxFingerprint(t *testing.T, cert *x509.Certificate) string {
	t.Helper()
	ctx := peer.NewContext(context.Background(), &peer.Peer{
		AuthInfo: credentials.TLSInfo{
			State: tls.ConnectionState{
				PeerCertificates: []*x509.Certificate{cert},
			},
		},
	})
	return clientSPKIFromContext(ctx)
}

// makeSharedCNCert generates a fresh Ed25519 certificate with the shared generic
// CN "Tergum Client".
func makeSharedCNCert(t *testing.T) *x509.Certificate {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "Tergum Client"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, pub, priv)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}
	return cert
}

// repeatHex returns a string of n '0' hex characters.
func repeatHex(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = '0'
	}
	return string(b)
}
