// Package connection provides helpers for establishing mTLS connections
// between Tergum client and server nodes.
package connection

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"

	"github.com/gcclinux/tergum/internal/config"
	tlspkg "github.com/gcclinux/tergum/internal/tls"
)

// LoadClientTLS builds a *tls.Config for client-mode mTLS connections and
// extracts the client identity (CN) from the loaded certificate.
// It reuses the existing tls.NewManager() infrastructure to load certificates.
func LoadClientTLS(cfg *config.Config) (*tls.Config, string, error) {
	if cfg.TLS.CACert == "" {
		return nil, "", fmt.Errorf("tls.ca_cert is required for client connections")
	}
	if cfg.TLS.Cert == "" {
		return nil, "", fmt.Errorf("tls.cert is required for client connections")
	}
	if cfg.TLS.Key == "" {
		return nil, "", fmt.Errorf("tls.key is required for client connections")
	}

	mgr := tlspkg.NewManager()
	tlsCfg, err := mgr.LoadClientTLS(cfg.TLS.CACert, cfg.TLS.Cert, cfg.TLS.Key)
	if err != nil {
		return nil, "", fmt.Errorf("load client TLS: %w", err)
	}

	// Extract the Common Name from the client certificate to use as clientID.
	clientID, err := extractCN(cfg.TLS.Cert, cfg.TLS.Key)
	if err != nil {
		return nil, "", fmt.Errorf("extract client identity: %w", err)
	}

	return tlsCfg, clientID, nil
}

// extractCN loads a certificate key pair and returns the Subject Common Name.
func extractCN(certPath, keyPath string) (string, error) {
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return "", fmt.Errorf("load key pair: %w", err)
	}

	if len(cert.Certificate) == 0 {
		return "", fmt.Errorf("certificate file contains no certificates")
	}

	x509Cert, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return "", fmt.Errorf("parse certificate: %w", err)
	}

	if x509Cert.Subject.CommonName == "" {
		return "", fmt.Errorf("certificate has no Common Name (CN)")
	}

	return x509Cert.Subject.CommonName, nil
}
