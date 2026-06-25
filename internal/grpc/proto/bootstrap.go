package proto

// BootstrapRequest is sent by a client during setup to request TLS certificates
// from the server. No authentication is required since the client has no cert yet.
type BootstrapRequest struct{}

// BootstrapResponse contains the PEM-encoded CA certificate and a freshly-issued
// client certificate + private key, all signed by the server's CA.
type BootstrapResponse struct {
	// CACertPEM is the PEM-encoded CA certificate (ca.crt).
	CACertPEM []byte `json:"ca_cert_pem"`
	// ClientCertPEM is the PEM-encoded client certificate (client.crt), signed by the CA.
	ClientCertPEM []byte `json:"client_cert_pem"`
	// ClientKeyPEM is the PEM-encoded client private key (client.key).
	ClientKeyPEM []byte `json:"client_key_pem"`
}
