package tls

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestGenerateCerts_CreatesAllFiles(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager()

	if err := mgr.GenerateCerts(dir); err != nil {
		t.Fatalf("GenerateCerts failed: %v", err)
	}

	expectedFiles := []string{
		"ca.crt", "ca.key",
		"server.crt", "server.key",
		"client.crt", "client.key",
	}

	for _, name := range expectedFiles {
		path := filepath.Join(dir, name)
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("expected file %s not found: %v", name, err)
			continue
		}
		if info.Size() == 0 {
			t.Errorf("file %s is empty", name)
		}
	}
}

func TestGenerateCerts_ValidPEM(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager()

	if err := mgr.GenerateCerts(dir); err != nil {
		t.Fatalf("GenerateCerts failed: %v", err)
	}

	// Verify cert files are valid PEM certificates
	certFiles := []string{"ca.crt", "server.crt", "client.crt"}
	for _, name := range certFiles {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		block, _ := pem.Decode(data)
		if block == nil {
			t.Errorf("%s: no PEM block found", name)
			continue
		}
		if block.Type != "CERTIFICATE" {
			t.Errorf("%s: expected CERTIFICATE, got %s", name, block.Type)
		}
		if _, err := x509.ParseCertificate(block.Bytes); err != nil {
			t.Errorf("%s: invalid certificate: %v", name, err)
		}
	}

	// Verify key files are valid PEM private keys
	keyFiles := []string{"ca.key", "server.key", "client.key"}
	for _, name := range keyFiles {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		block, _ := pem.Decode(data)
		if block == nil {
			t.Errorf("%s: no PEM block found", name)
			continue
		}
		if block.Type != "PRIVATE KEY" {
			t.Errorf("%s: expected PRIVATE KEY, got %s", name, block.Type)
		}
		if _, err := x509.ParsePKCS8PrivateKey(block.Bytes); err != nil {
			t.Errorf("%s: invalid private key: %v", name, err)
		}
	}
}

func TestGenerateCerts_CAProperties(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager()

	if err := mgr.GenerateCerts(dir); err != nil {
		t.Fatalf("GenerateCerts failed: %v", err)
	}

	cert := loadCert(t, filepath.Join(dir, "ca.crt"))
	if !cert.IsCA {
		t.Error("CA certificate IsCA should be true")
	}
	if !cert.BasicConstraintsValid {
		t.Error("CA certificate BasicConstraintsValid should be true")
	}
	if cert.Subject.CommonName != "Tergum CA" {
		t.Errorf("CA CommonName = %q, want %q", cert.Subject.CommonName, "Tergum CA")
	}
}

func TestGenerateCerts_ServerCertProperties(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager()

	if err := mgr.GenerateCerts(dir); err != nil {
		t.Fatalf("GenerateCerts failed: %v", err)
	}

	cert := loadCert(t, filepath.Join(dir, "server.crt"))
	if cert.IsCA {
		t.Error("server cert should not be CA")
	}

	// Check SANs
	foundLocalhost := false
	for _, dns := range cert.DNSNames {
		if dns == "localhost" {
			foundLocalhost = true
		}
	}
	if !foundLocalhost {
		t.Error("server cert missing localhost DNS SAN")
	}

	foundLoopback := false
	for _, ip := range cert.IPAddresses {
		if ip.Equal(net.IPv4(127, 0, 0, 1)) {
			foundLoopback = true
		}
	}
	if !foundLoopback {
		t.Error("server cert missing 127.0.0.1 IP SAN")
	}

	// Check ExtKeyUsage
	hasServerAuth := false
	hasClientAuth := false
	for _, usage := range cert.ExtKeyUsage {
		if usage == x509.ExtKeyUsageServerAuth {
			hasServerAuth = true
		}
		if usage == x509.ExtKeyUsageClientAuth {
			hasClientAuth = true
		}
	}
	if !hasServerAuth {
		t.Error("server cert missing ServerAuth ExtKeyUsage")
	}
	if !hasClientAuth {
		t.Error("server cert missing ClientAuth ExtKeyUsage")
	}
}

func TestGenerateCerts_ClientCertProperties(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager()

	if err := mgr.GenerateCerts(dir); err != nil {
		t.Fatalf("GenerateCerts failed: %v", err)
	}

	cert := loadCert(t, filepath.Join(dir, "client.crt"))
	if cert.IsCA {
		t.Error("client cert should not be CA")
	}

	hasClientAuth := false
	for _, usage := range cert.ExtKeyUsage {
		if usage == x509.ExtKeyUsageClientAuth {
			hasClientAuth = true
		}
	}
	if !hasClientAuth {
		t.Error("client cert missing ClientAuth ExtKeyUsage")
	}
}

func TestGenerateCerts_CertsSignedByCA(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager()

	if err := mgr.GenerateCerts(dir); err != nil {
		t.Fatalf("GenerateCerts failed: %v", err)
	}

	caCert := loadCert(t, filepath.Join(dir, "ca.crt"))
	serverCert := loadCert(t, filepath.Join(dir, "server.crt"))
	clientCert := loadCert(t, filepath.Join(dir, "client.crt"))

	// Verify server cert is signed by CA
	if err := serverCert.CheckSignatureFrom(caCert); err != nil {
		t.Errorf("server cert not signed by CA: %v", err)
	}

	// Verify client cert is signed by CA
	if err := clientCert.CheckSignatureFrom(caCert); err != nil {
		t.Errorf("client cert not signed by CA: %v", err)
	}
}

func TestLoadServerTLS_RequiresClientCert(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager()

	if err := mgr.GenerateCerts(dir); err != nil {
		t.Fatalf("GenerateCerts failed: %v", err)
	}

	cfg, err := mgr.LoadServerTLS(
		filepath.Join(dir, "ca.crt"),
		filepath.Join(dir, "server.crt"),
		filepath.Join(dir, "server.key"),
	)
	if err != nil {
		t.Fatalf("LoadServerTLS failed: %v", err)
	}

	if cfg.ClientAuth != tls.RequireAndVerifyClientCert {
		t.Errorf("ClientAuth = %v, want RequireAndVerifyClientCert", cfg.ClientAuth)
	}
	if cfg.ClientCAs == nil {
		t.Error("ClientCAs should not be nil")
	}
	if cfg.MinVersion != tls.VersionTLS13 {
		t.Errorf("MinVersion = %v, want TLS 1.3", cfg.MinVersion)
	}
	if len(cfg.Certificates) != 1 {
		t.Errorf("expected 1 certificate, got %d", len(cfg.Certificates))
	}
}

func TestLoadClientTLS_TrustsCA(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager()

	if err := mgr.GenerateCerts(dir); err != nil {
		t.Fatalf("GenerateCerts failed: %v", err)
	}

	cfg, err := mgr.LoadClientTLS(
		filepath.Join(dir, "ca.crt"),
		filepath.Join(dir, "client.crt"),
		filepath.Join(dir, "client.key"),
	)
	if err != nil {
		t.Fatalf("LoadClientTLS failed: %v", err)
	}

	if cfg.RootCAs == nil {
		t.Error("RootCAs should not be nil")
	}
	if cfg.MinVersion != tls.VersionTLS13 {
		t.Errorf("MinVersion = %v, want TLS 1.3", cfg.MinVersion)
	}
	if len(cfg.Certificates) != 1 {
		t.Errorf("expected 1 certificate, got %d", len(cfg.Certificates))
	}
}

func TestMTLSHandshake(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager()

	if err := mgr.GenerateCerts(dir); err != nil {
		t.Fatalf("GenerateCerts failed: %v", err)
	}

	serverCfg, err := mgr.LoadServerTLS(
		filepath.Join(dir, "ca.crt"),
		filepath.Join(dir, "server.crt"),
		filepath.Join(dir, "server.key"),
	)
	if err != nil {
		t.Fatalf("LoadServerTLS failed: %v", err)
	}

	clientCfg, err := mgr.LoadClientTLS(
		filepath.Join(dir, "ca.crt"),
		filepath.Join(dir, "client.crt"),
		filepath.Join(dir, "client.key"),
	)
	if err != nil {
		t.Fatalf("LoadClientTLS failed: %v", err)
	}

	// Start a TLS server
	listener, err := tls.Listen("tcp", "127.0.0.1:0", serverCfg)
	if err != nil {
		t.Fatalf("listen failed: %v", err)
	}
	defer listener.Close()

	addr := listener.Addr().String()
	serverDone := make(chan error, 1)

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			serverDone <- err
			return
		}
		defer conn.Close()

		// Force handshake
		tlsConn := conn.(*tls.Conn)
		if err := tlsConn.Handshake(); err != nil {
			serverDone <- err
			return
		}

		// Verify peer certificate
		state := tlsConn.ConnectionState()
		if len(state.PeerCertificates) == 0 {
			serverDone <- fmt.Errorf("no peer certificates")
			return
		}

		// Echo data
		buf := make([]byte, 64)
		n, err := conn.Read(buf)
		if err != nil {
			serverDone <- err
			return
		}
		if _, err := conn.Write(buf[:n]); err != nil {
			serverDone <- err
			return
		}
		serverDone <- nil
	}()

	// Connect client
	clientCfg.ServerName = "localhost"
	conn, err := tls.Dial("tcp", addr, clientCfg)
	if err != nil {
		t.Fatalf("client dial failed: %v", err)
	}
	defer conn.Close()

	// Send and receive data
	msg := []byte("hello mTLS")
	if _, err := conn.Write(msg); err != nil {
		t.Fatalf("client write failed: %v", err)
	}

	buf := make([]byte, 64)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("client read failed: %v", err)
	}

	if string(buf[:n]) != string(msg) {
		t.Errorf("echo mismatch: got %q, want %q", buf[:n], msg)
	}

	// Wait for server goroutine
	if err := <-serverDone; err != nil {
		t.Fatalf("server error: %v", err)
	}
}

func TestMTLSHandshake_RejectsWithoutClientCert(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager()

	if err := mgr.GenerateCerts(dir); err != nil {
		t.Fatalf("GenerateCerts failed: %v", err)
	}

	serverCfg, err := mgr.LoadServerTLS(
		filepath.Join(dir, "ca.crt"),
		filepath.Join(dir, "server.crt"),
		filepath.Join(dir, "server.key"),
	)
	if err != nil {
		t.Fatalf("LoadServerTLS failed: %v", err)
	}

	// Start a TLS server
	listener, err := tls.Listen("tcp", "127.0.0.1:0", serverCfg)
	if err != nil {
		t.Fatalf("listen failed: %v", err)
	}
	defer listener.Close()

	addr := listener.Addr().String()

	// Server goroutine - expects handshake to fail
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		// Force handshake - should fail due to missing client cert
		tlsConn := conn.(*tls.Conn)
		_ = tlsConn.Handshake()
	}()

	// Client without certificate - should fail
	caData, err := os.ReadFile(filepath.Join(dir, "ca.crt"))
	if err != nil {
		t.Fatalf("read CA: %v", err)
	}
	caPool := x509.NewCertPool()
	caPool.AppendCertsFromPEM(caData)

	clientCfg := &tls.Config{
		RootCAs:    caPool,
		ServerName: "localhost",
		MinVersion: tls.VersionTLS13,
		// No Certificates - this client has no cert to present
	}

	conn, err := tls.Dial("tcp", addr, clientCfg)
	if err != nil {
		// Expected: connection refused or handshake failure
		return
	}
	defer conn.Close()

	// Try to use the connection - should fail
	if _, err := conn.Write([]byte("test")); err != nil {
		return // Expected failure
	}

	buf := make([]byte, 64)
	if _, err := conn.Read(buf); err != nil {
		return // Expected failure
	}

	t.Error("expected connection to be rejected without client certificate")
}

func TestLoadServerTLS_InvalidPaths(t *testing.T) {
	mgr := NewManager()

	_, err := mgr.LoadServerTLS("/nonexistent/ca.crt", "/nonexistent/server.crt", "/nonexistent/server.key")
	if err == nil {
		t.Error("expected error for nonexistent paths")
	}
}

func TestLoadClientTLS_InvalidPaths(t *testing.T) {
	mgr := NewManager()

	_, err := mgr.LoadClientTLS("/nonexistent/ca.crt", "/nonexistent/client.crt", "/nonexistent/client.key")
	if err == nil {
		t.Error("expected error for nonexistent paths")
	}
}

func TestGenerateCerts_CreatesDirectory(t *testing.T) {
	base := t.TempDir()
	outputDir := filepath.Join(base, "nested", "certs")
	mgr := NewManager()

	if err := mgr.GenerateCerts(outputDir); err != nil {
		t.Fatalf("GenerateCerts failed: %v", err)
	}

	// Directory should exist with files
	info, err := os.Stat(outputDir)
	if err != nil {
		t.Fatalf("output dir not created: %v", err)
	}
	if !info.IsDir() {
		t.Error("output path is not a directory")
	}
}

// loadCert is a test helper to load and parse a PEM certificate file.
func loadCert(t *testing.T, path string) *x509.Certificate {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		t.Fatalf("%s: no PEM block found", path)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("%s: parse certificate: %v", path, err)
	}
	return cert
}
