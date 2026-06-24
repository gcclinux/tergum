// Package tls provides TLS certificate generation and loading for mutual TLS
// authentication between Tergum clients and servers.
package tls

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

// CertManager generates and loads TLS certificates for mutual TLS.
type CertManager interface {
	// GenerateCerts creates CA, server, and client certificates with Ed25519 keys.
	// Optional hosts can be provided to be added to the server certificate's Subject Alternative Names.
	GenerateCerts(outputDir string, hosts ...string) error
	// LoadServerTLS returns a TLS config for the server requiring client certs.
	LoadServerTLS(caPath, certPath, keyPath string) (*tls.Config, error)
	// LoadClientTLS returns a TLS config for the client trusting the CA.
	LoadClientTLS(caPath, certPath, keyPath string) (*tls.Config, error)
}

// Manager implements CertManager using Ed25519 keys and x509 certificates.
type Manager struct{}

// NewManager returns a new CertManager implementation.
func NewManager() CertManager {
	return &Manager{}
}

// GenerateCerts creates a CA, server certificate, and client certificate, all
// using Ed25519 keys. Output files: ca.crt, ca.key, server.crt, server.key,
// client.crt, client.key in PEM format.
// Optional hosts can be provided to be added to the server certificate's Subject Alternative Names.
func (m *Manager) GenerateCerts(outputDir string, hosts ...string) error {
	if err := os.MkdirAll(outputDir, 0700); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	// Generate CA
	caKey, caCert, caCertBytes, err := generateCA()
	if err != nil {
		return fmt.Errorf("generate CA: %w", err)
	}

	// Generate server certificate signed by CA
	serverCertBytes, serverKey, err := generateServerCert(caCert, caKey, hosts...)
	if err != nil {
		return fmt.Errorf("generate server cert: %w", err)
	}

	// Generate client certificate signed by CA
	clientCertBytes, clientKey, err := generateClientCert(caCert, caKey)
	if err != nil {
		return fmt.Errorf("generate client cert: %w", err)
	}

	// Write all files
	files := []struct {
		name string
		data []byte
	}{
		{"ca.crt", pemEncodeCert(caCertBytes)},
		{"ca.key", pemEncodeKey(caKey)},
		{"server.crt", pemEncodeCert(serverCertBytes)},
		{"server.key", pemEncodeKey(serverKey)},
		{"client.crt", pemEncodeCert(clientCertBytes)},
		{"client.key", pemEncodeKey(clientKey)},
	}

	for _, f := range files {
		path := filepath.Join(outputDir, f.name)
		if err := os.WriteFile(path, f.data, 0600); err != nil {
			return fmt.Errorf("write %s: %w", f.name, err)
		}
	}

	return nil
}

// LoadServerTLS loads certificates and returns a TLS config configured for
// mutual TLS. The server requires and verifies client certificates signed by
// the specified CA.
func (m *Manager) LoadServerTLS(caPath, certPath, keyPath string) (*tls.Config, error) {
	caPool, err := loadCAPool(caPath)
	if err != nil {
		return nil, fmt.Errorf("load CA: %w", err)
	}

	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, fmt.Errorf("load server certificate: %w", err)
	}

	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    caPool,
		MinVersion:   tls.VersionTLS13,
	}, nil
}

// LoadClientTLS loads certificates and returns a TLS config for the client.
// The client trusts the specified CA for server verification and presents its
// own certificate for mutual authentication.
func (m *Manager) LoadClientTLS(caPath, certPath, keyPath string) (*tls.Config, error) {
	caPool, err := loadCAPool(caPath)
	if err != nil {
		return nil, fmt.Errorf("load CA: %w", err)
	}

	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, fmt.Errorf("load client certificate: %w", err)
	}

	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      caPool,
		MinVersion:   tls.VersionTLS13,
	}, nil
}

// loadCAPool reads a PEM-encoded CA certificate and returns a CertPool.
func loadCAPool(caPath string) (*x509.CertPool, error) {
	caPEM, err := os.ReadFile(caPath)
	if err != nil {
		return nil, fmt.Errorf("read CA file: %w", err)
	}

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("failed to parse CA certificate")
	}

	return pool, nil
}

// generateCA creates a self-signed CA certificate with Ed25519 key.
// Validity: 10 years.
func generateCA() (ed25519.PrivateKey, *x509.Certificate, []byte, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, nil, err
	}

	serial, err := randomSerial()
	if err != nil {
		return nil, nil, nil, err
	}

	template := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			Organization: []string{"Tergum"},
			CommonName:   "Tergum CA",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            1,
	}

	certBytes, err := x509.CreateCertificate(rand.Reader, template, template, pub, priv)
	if err != nil {
		return nil, nil, nil, err
	}

	cert, err := x509.ParseCertificate(certBytes)
	if err != nil {
		return nil, nil, nil, err
	}

	return priv, cert, certBytes, nil
}

// generateServerCert creates a server certificate signed by the CA.
// Includes SANs: localhost, 127.0.0.1, plus any additional hosts. Validity: 1 year.
func generateServerCert(caCert *x509.Certificate, caKey ed25519.PrivateKey, hosts ...string) ([]byte, ed25519.PrivateKey, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, err
	}

	serial, err := randomSerial()
	if err != nil {
		return nil, nil, err
	}

	dnsNames := []string{"localhost"}
	ipAddresses := []net.IP{net.IPv4(127, 0, 0, 1)}

	for _, host := range hosts {
		if host == "" {
			continue
		}
		if ip := net.ParseIP(host); ip != nil {
			found := false
			for _, existingIP := range ipAddresses {
				if existingIP.Equal(ip) {
					found = true
					break
				}
			}
			if !found {
				ipAddresses = append(ipAddresses, ip)
			}
		} else {
			found := false
			for _, existingDNS := range dnsNames {
				if existingDNS == host {
					found = true
					break
				}
			}
			if !found {
				dnsNames = append(dnsNames, host)
			}
		}
	}

	template := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			Organization: []string{"Tergum"},
			CommonName:   "Tergum Server",
		},
		NotBefore: time.Now(),
		NotAfter:  time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:  x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{
			x509.ExtKeyUsageServerAuth,
		},
		DNSNames:    dnsNames,
		IPAddresses: ipAddresses,
	}

	certBytes, err := x509.CreateCertificate(rand.Reader, template, caCert, pub, caKey)
	if err != nil {
		return nil, nil, err
	}

	return certBytes, priv, nil
}

// generateClientCert creates a client certificate signed by the CA.
// Validity: 1 year.
func generateClientCert(caCert *x509.Certificate, caKey ed25519.PrivateKey) ([]byte, ed25519.PrivateKey, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, err
	}

	serial, err := randomSerial()
	if err != nil {
		return nil, nil, err
	}

	template := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			Organization: []string{"Tergum"},
			CommonName:   "Tergum Client",
		},
		NotBefore: time.Now(),
		NotAfter:  time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:  x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{
			x509.ExtKeyUsageClientAuth,
		},
	}

	certBytes, err := x509.CreateCertificate(rand.Reader, template, caCert, pub, caKey)
	if err != nil {
		return nil, nil, err
	}

	return certBytes, priv, nil
}

// pemEncodeCert encodes a DER certificate as PEM.
func pemEncodeCert(certDER []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certDER,
	})
}

// pemEncodeKey encodes an Ed25519 private key as PEM (PKCS8).
func pemEncodeKey(key ed25519.PrivateKey) []byte {
	pkcs8, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		// Ed25519 keys should always marshal successfully
		panic(fmt.Sprintf("failed to marshal private key: %v", err))
	}
	return pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: pkcs8,
	})
}

// randomSerial generates a random serial number for certificates.
func randomSerial() (*big.Int, error) {
	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	return rand.Int(rand.Reader, serialLimit)
}
