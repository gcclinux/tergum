package grpc

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gcclinux/tergum/internal/grpc/proto"
	tlspkg "github.com/gcclinux/tergum/internal/tls"
)

// BootstrapServer implements BootstrapServiceServer, issuing client TLS certificates
// signed by the server's CA. It runs on the bootstrap port (default 7402) without
// requiring client certificates, so newly-provisioned clients can fetch certs during setup.
type BootstrapServer struct {
	proto.UnimplementedBootstrapServiceServer

	certsDir string // path to the directory containing ca.crt, ca.key, server.crt, server.key
}

// BootstrapServerConfig holds configuration for the BootstrapServer.
type BootstrapServerConfig struct {
	// CertsDir is the directory containing the server's CA and server certificates.
	// Expected files: ca.crt, ca.key
	CertsDir string
}

// NewBootstrapServer creates a new BootstrapServer.
func NewBootstrapServer(cfg BootstrapServerConfig) *BootstrapServer {
	return &BootstrapServer{
		certsDir: cfg.CertsDir,
	}
}

// FetchClientCerts issues a fresh client certificate signed by the server's CA and
// returns the CA cert, client cert, and client private key as PEM bytes.
// The private key is transmitted securely over the TLS bootstrap channel.
func (s *BootstrapServer) FetchClientCerts(ctx context.Context, req *proto.BootstrapRequest) (*proto.BootstrapResponse, error) {
	caPath := filepath.Join(s.certsDir, "ca.crt")
	caKeyPath := filepath.Join(s.certsDir, "ca.key")

	// Read the CA cert to include in the response.
	caCertPEM, err := os.ReadFile(caPath)
	if err != nil {
		return nil, fmt.Errorf("read CA cert: %w", err)
	}

	// Issue a new client certificate signed by the CA.
	mgr := tlspkg.NewManager()
	clientCertPEM, clientKeyPEM, err := mgr.IssueClientCert(caPath, caKeyPath)
	if err != nil {
		return nil, fmt.Errorf("issue client cert: %w", err)
	}

	return &proto.BootstrapResponse{
		CACertPEM:     caCertPEM,
		ClientCertPEM: clientCertPEM,
		ClientKeyPEM:  clientKeyPEM,
	}, nil
}

// Ensure BootstrapServer satisfies the interface at compile time.
var _ proto.BootstrapServiceServer = (*BootstrapServer)(nil)
