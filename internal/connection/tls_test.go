package connection

import (
	"path/filepath"
	"testing"

	"github.com/gcclinux/tergum/internal/config"
	tlspkg "github.com/gcclinux/tergum/internal/tls"
)

func TestLoadClientTLS_Success(t *testing.T) {
	dir := t.TempDir()
	mgr := tlspkg.NewManager()
	if err := mgr.GenerateCerts(dir); err != nil {
		t.Fatalf("GenerateCerts: %v", err)
	}

	cfg := &config.Config{}
	cfg.TLS.CACert = filepath.Join(dir, "ca.crt")
	cfg.TLS.Cert = filepath.Join(dir, "client.crt")
	cfg.TLS.Key = filepath.Join(dir, "client.key")

	tlsCfg, clientID, err := LoadClientTLS(cfg)
	if err != nil {
		t.Fatalf("LoadClientTLS: %v", err)
	}

	if tlsCfg == nil {
		t.Fatal("expected non-nil tls.Config")
	}

	if tlsCfg.RootCAs == nil {
		t.Error("expected RootCAs to be set")
	}

	if len(tlsCfg.Certificates) != 1 {
		t.Errorf("expected 1 certificate, got %d", len(tlsCfg.Certificates))
	}

	// The generated client cert has CN "Tergum Client"
	if clientID != "Tergum Client" {
		t.Errorf("clientID = %q, want %q", clientID, "Tergum Client")
	}
}

func TestLoadClientTLS_MissingCACert(t *testing.T) {
	cfg := &config.Config{}
	cfg.TLS.CACert = ""
	cfg.TLS.Cert = "/some/cert.pem"
	cfg.TLS.Key = "/some/key.pem"

	_, _, err := LoadClientTLS(cfg)
	if err == nil {
		t.Fatal("expected error for missing CA cert")
	}
}

func TestLoadClientTLS_MissingCert(t *testing.T) {
	cfg := &config.Config{}
	cfg.TLS.CACert = "/some/ca.crt"
	cfg.TLS.Cert = ""
	cfg.TLS.Key = "/some/key.pem"

	_, _, err := LoadClientTLS(cfg)
	if err == nil {
		t.Fatal("expected error for missing cert")
	}
}

func TestLoadClientTLS_MissingKey(t *testing.T) {
	cfg := &config.Config{}
	cfg.TLS.CACert = "/some/ca.crt"
	cfg.TLS.Cert = "/some/cert.pem"
	cfg.TLS.Key = ""

	_, _, err := LoadClientTLS(cfg)
	if err == nil {
		t.Fatal("expected error for missing key")
	}
}

func TestLoadClientTLS_InvalidPaths(t *testing.T) {
	cfg := &config.Config{}
	cfg.TLS.CACert = "/nonexistent/ca.crt"
	cfg.TLS.Cert = "/nonexistent/client.crt"
	cfg.TLS.Key = "/nonexistent/client.key"

	_, _, err := LoadClientTLS(cfg)
	if err == nil {
		t.Fatal("expected error for nonexistent paths")
	}
}

func TestExtractCN_Success(t *testing.T) {
	dir := t.TempDir()
	mgr := tlspkg.NewManager()
	if err := mgr.GenerateCerts(dir); err != nil {
		t.Fatalf("GenerateCerts: %v", err)
	}

	cn, err := extractCN(filepath.Join(dir, "client.crt"), filepath.Join(dir, "client.key"))
	if err != nil {
		t.Fatalf("extractCN: %v", err)
	}

	if cn != "Tergum Client" {
		t.Errorf("CN = %q, want %q", cn, "Tergum Client")
	}
}

func TestExtractCN_InvalidPath(t *testing.T) {
	_, err := extractCN("/nonexistent/cert.pem", "/nonexistent/key.pem")
	if err == nil {
		t.Fatal("expected error for nonexistent paths")
	}
}
