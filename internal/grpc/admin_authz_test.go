package grpc

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"database/sql"
	"encoding/hex"
	"math/big"
	"testing"
	"time"

	"github.com/gcclinux/tergum/internal/config"
	"github.com/gcclinux/tergum/internal/db"
	"github.com/gcclinux/tergum/internal/grpc/proto"
	"github.com/gcclinux/tergum/internal/model"
	"github.com/gcclinux/tergum/internal/registry"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	_ "modernc.org/sqlite"
)

// This file holds the four mandated admin-client authorization proofs for
// FEAT-004. They exercise the server-side gate directly on the CommandServer
// handlers over contexts carrying a verified mTLS peer certificate — the same
// identity path the real transport uses (clientSPKIFromContext). Authorization
// keys off the certificate SPKI fingerprint, never the CN or client-id
// metadata, so these tests build real Ed25519 leaf certs and inject them via
// peer.NewContext (mirroring the harness helpers in admin_policy_test.go).

// --- harness helpers ---

// makeNamedCert generates a fresh Ed25519 leaf certificate with the given CN
// and returns the parsed cert together with its SPKI fingerprint (SHA-256 of
// RawSubjectPublicKeyInfo, lowercase hex) — the trusted admin identity.
func makeNamedCert(t *testing.T, cn string) (*x509.Certificate, string) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: cn},
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
	sum := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	return cert, hex.EncodeToString(sum[:])
}

// peerCtx returns a context carrying cert as a verified mTLS peer certificate.
func peerCtx(cert *x509.Certificate) context.Context {
	return peer.NewContext(context.Background(), &peer.Peer{
		AuthInfo: credentials.TLSInfo{
			State: tls.ConnectionState{
				PeerCertificates: []*x509.Certificate{cert},
			},
		},
	})
}

// peerCtxWithClientID returns a peer context that additionally carries a
// client-id gRPC metadata value — used to prove name-spoofing is ignored.
func peerCtxWithClientID(cert *x509.Certificate, clientID string) context.Context {
	ctx := peerCtx(cert)
	return metadata.NewIncomingContext(ctx, metadata.Pairs("client-id", clientID))
}

// newAuthzRegistry builds an in-memory registry (shared-cache sqlite) for the
// authorization tests, mirroring the setup used in registry_test.go.
func newAuthzRegistry(t *testing.T) *registry.Registry {
	t.Helper()
	database, err := sql.Open("sqlite", "file::memory:?cache=shared&_authz="+randToken())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	database.SetMaxOpenConns(1)
	if _, err := database.Exec("PRAGMA journal_mode=WAL"); err != nil {
		database.Close()
		t.Fatalf("wal: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	reg, err := registry.New(registry.Config{
		DB:               database,
		OfflineThreshold: 90 * time.Second,
		CheckInterval:    time.Hour, // disable background churn during the test
	})
	if err != nil {
		t.Fatalf("new registry: %v", err)
	}
	return reg
}

// randToken returns a short random hex token so each test gets an isolated
// shared-cache in-memory database (shared caches are keyed by the DSN).
func randToken() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// scopeRepo is a mockRepo that honors the ClientID filter in ListJobs so the
// non-admin force-scope assertions can observe which jobs a caller sees.
type scopeRepo struct {
	mockRepo
	all []model.BackupJob
}

func (r *scopeRepo) ListJobs(ctx context.Context, filter db.JobFilter) ([]model.BackupJob, error) {
	if filter.ClientID == nil {
		return r.all, nil
	}
	var out []model.BackupJob
	for _, j := range r.all {
		if j.ClientID == *filter.ClientID {
			out = append(out, j)
		}
	}
	return out, nil
}

// stubCrossRestorer records the request it received and returns a canned result,
// proving an admin caller reaches the orchestrator.
type stubCrossRestorer struct {
	called bool
	req    CrossRestoreRequest
	result CrossRestoreResult
	err    error
}

func (s *stubCrossRestorer) RestoreToTarget(ctx context.Context, req CrossRestoreRequest) (CrossRestoreResult, error) {
	s.called = true
	s.req = req
	return s.result, s.err
}

// stubWatcherController records start/stop calls so the admin-allowed path can
// be observed.
type stubWatcherController struct {
	started []string
	stopped []string
	err     error
}

func (s *stubWatcherController) StartClientWatcher(ctx context.Context, clientID string) error {
	s.started = append(s.started, clientID)
	return s.err
}

func (s *stubWatcherController) StopClientWatcher(ctx context.Context, clientID string) error {
	s.stopped = append(s.stopped, clientID)
	return s.err
}

// codeOf extracts the gRPC status code from an error (OK when nil).
func codeOf(err error) codes.Code {
	return status.Code(err)
}

// --- (a) admin passes the gate for every server-equivalent op ---

func TestAdminAuthz_AdminAllowedForAllServerEquivalentOps(t *testing.T) {
	reg := newAuthzRegistry(t)

	// Register the three participating clients with their SPKI fingerprints so
	// requireActiveClient and the admin lookup resolve.
	adminCert, adminFP := makeNamedCert(t, "admin-ops")
	if _, err := reg.Register("admin-ops", "tunnel://admin-ops"); err != nil {
		t.Fatalf("register admin: %v", err)
	}
	if err := reg.SetSPKIFingerprint("admin-ops", adminFP); err != nil {
		t.Fatalf("set admin spki: %v", err)
	}
	if _, err := reg.Register("client-a", "tunnel://client-a"); err != nil {
		t.Fatalf("register A: %v", err)
	}
	if _, err := reg.Register("client-b", "tunnel://client-b"); err != nil {
		t.Fatalf("register B: %v", err)
	}

	// Put the admin's SPKI into the config admin list and build a live policy.
	cfgPath := writeAdminConfig(t, []config.AdminClient{{Name: "admin-ops", Fingerprint: adminFP}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	policy := NewConfigAdminPolicy(ctx, cfgPath, time.Hour)
	if !policy.IsAdmin(adminFP) {
		t.Fatalf("precondition: admin fingerprint should be authorized")
	}

	restorer := &stubCrossRestorer{result: CrossRestoreResult{FilesSent: 3, FilesReceived: 3, FilesFailed: 0}}
	watcher := &stubWatcherController{}

	repo := &scopeRepo{all: []model.BackupJob{
		{BackupID: "ja", ClientID: "client-a", Status: model.JobCompleted, StartedAt: time.Now()},
		{BackupID: "jb", ClientID: "client-b", Status: model.JobCompleted, StartedAt: time.Now()},
	}}

	srv := NewCommandServer(CommandServerConfig{
		BackupEngine:      &mockBackupEngine{},
		Repo:              repo,
		Registry:          reg,
		AdminPolicy:       policy,
		CrossRestorer:     restorer,
		WatcherController: watcher,
		Version:           "test",
	})

	adminContext := peerCtx(adminCert)

	// RestoreToTarget A->B succeeds via the stub CrossRestorer.
	resp, err := srv.RestoreToTarget(adminContext, &proto.RestoreToTargetRequest{
		SourceClientId: "client-a",
		TargetClientId: "client-b",
		Query:          "*",
	})
	if err != nil {
		t.Fatalf("admin RestoreToTarget error: %v", err)
	}
	if !restorer.called {
		t.Error("expected the cross-restorer to be invoked for an admin caller")
	}
	if resp.FilesSent != 3 || !resp.Success {
		t.Errorf("unexpected restore response: %+v", resp)
	}

	// ControlClientWatcher succeeds via the stub ClientWatcherController.
	if _, err := srv.ControlClientWatcher(adminContext, &proto.ControlWatcherRequest{
		TargetClientId: "client-a",
		Start:          true,
	}); err != nil {
		t.Fatalf("admin ControlClientWatcher error: %v", err)
	}
	if len(watcher.started) != 1 || watcher.started[0] != "client-a" {
		t.Errorf("expected StartClientWatcher(client-a), got %+v", watcher.started)
	}

	// ListBackups with another client's id is NOT forced to self — admin sees
	// the requested client's jobs.
	lb, err := srv.ListBackups(adminContext, &proto.ListBackupsRequest{ClientId: "client-b"})
	if err != nil {
		t.Fatalf("admin ListBackups(client-b): %v", err)
	}
	if len(lb.Backups) != 1 || lb.Backups[0].ClientId != "client-b" {
		t.Errorf("admin should see client-b's jobs, got %+v", lb.Backups)
	}

	// ListBackups with an empty id is NOT forced to self — admin sees all jobs.
	lbAll, err := srv.ListBackups(adminContext, &proto.ListBackupsRequest{})
	if err != nil {
		t.Fatalf("admin ListBackups(empty): %v", err)
	}
	if len(lbAll.Backups) != 2 {
		t.Errorf("admin should see all jobs with empty id, got %d", len(lbAll.Backups))
	}

	// DeleteFromBackup does NOT return PermissionDenied for an admin; it instead
	// hits the "deletion engine not configured" precondition (§3.2.4). Marked
	// pending — the nil-engine precondition is the expected stand-in until the
	// deletion engine is wired on the command service.
	_, delErr := srv.DeleteFromBackup(adminContext, &proto.DeleteRequest{BackupId: "ja", FilePath: "/x"})
	if codeOf(delErr) == codes.PermissionDenied {
		t.Error("admin DeleteFromBackup must not be PermissionDenied")
	}
	if codeOf(delErr) != codes.InvalidArgument {
		t.Errorf("admin DeleteFromBackup should hit the nil-engine precondition (InvalidArgument), got %v", codeOf(delErr))
	}

	// GetRetention is global/unchanged — ungated for all callers; with no
	// retention engine configured it reports the standard precondition rather
	// than any admin-specific denial.
	_, retErr := srv.GetRetention(adminContext, &proto.RetentionRequest{})
	if codeOf(retErr) == codes.PermissionDenied {
		t.Error("GetRetention must never be admin-gated")
	}
}

// --- (b) non-admin is denied / force-scoped ---

func TestAdminAuthz_NonAdminDeniedAndScoped(t *testing.T) {
	reg := newAuthzRegistry(t)

	// A non-admin client whose SPKI is NOT in the admin list.
	userCert, userFP := makeNamedCert(t, "client-a")
	if _, err := reg.Register("client-a", "tunnel://client-a"); err != nil {
		t.Fatalf("register A: %v", err)
	}
	if err := reg.SetSPKIFingerprint("client-a", userFP); err != nil {
		t.Fatalf("set A spki: %v", err)
	}
	if _, err := reg.Register("client-b", "tunnel://client-b"); err != nil {
		t.Fatalf("register B: %v", err)
	}

	// An admin exists in the config, but the caller below is NOT it.
	_, adminFP := makeNamedCert(t, "admin-ops")
	cfgPath := writeAdminConfig(t, []config.AdminClient{{Name: "admin-ops", Fingerprint: adminFP}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	policy := NewConfigAdminPolicy(ctx, cfgPath, time.Hour)
	if policy.IsAdmin(userFP) {
		t.Fatalf("precondition: non-admin caller must not be authorized")
	}

	repo := &scopeRepo{all: []model.BackupJob{
		{BackupID: "ja", ClientID: "client-a", Status: model.JobCompleted, StartedAt: time.Now()},
		{BackupID: "jb", ClientID: "client-b", Status: model.JobCompleted, StartedAt: time.Now()},
	}}

	srv := NewCommandServer(CommandServerConfig{
		BackupEngine:      &mockBackupEngine{},
		Repo:              repo,
		Registry:          reg,
		AdminPolicy:       policy,
		CrossRestorer:     &stubCrossRestorer{},
		WatcherController: &stubWatcherController{},
		Version:           "test",
	})

	userContext := peerCtx(userCert)

	// RestoreToTarget -> PermissionDenied.
	if _, err := srv.RestoreToTarget(userContext, &proto.RestoreToTargetRequest{
		SourceClientId: "client-b",
		TargetClientId: "client-a",
		Query:          "*",
	}); codeOf(err) != codes.PermissionDenied {
		t.Errorf("non-admin RestoreToTarget code = %v, want PermissionDenied", codeOf(err))
	}

	// ControlClientWatcher -> PermissionDenied.
	if _, err := srv.ControlClientWatcher(userContext, &proto.ControlWatcherRequest{
		TargetClientId: "client-b",
		Start:          true,
	}); codeOf(err) != codes.PermissionDenied {
		t.Errorf("non-admin ControlClientWatcher code = %v, want PermissionDenied", codeOf(err))
	}

	// DeleteFromBackup -> PermissionDenied, asserted BEFORE the nil-engine
	// precondition (the §3.2.4 hard gate ordering).
	if _, err := srv.DeleteFromBackup(userContext, &proto.DeleteRequest{BackupId: "ja"}); codeOf(err) != codes.PermissionDenied {
		t.Errorf("non-admin DeleteFromBackup code = %v, want PermissionDenied", codeOf(err))
	}

	// ListBackups with another client's id is forced to the caller's own scope.
	lb, err := srv.ListBackups(userContext, &proto.ListBackupsRequest{ClientId: "client-b"})
	if err != nil {
		t.Fatalf("non-admin ListBackups(client-b): %v", err)
	}
	for _, j := range lb.Backups {
		if j.ClientId != "client-a" {
			t.Errorf("non-admin leaked job for %q; must only see own (client-a) jobs", j.ClientId)
		}
	}
	if len(lb.Backups) != 1 {
		t.Errorf("expected exactly the caller's own 1 job, got %d", len(lb.Backups))
	}

	// ListBackups with an EMPTY id is also forced to the caller's own scope
	// (proving the empty-id soft spot is closed).
	lbEmpty, err := srv.ListBackups(userContext, &proto.ListBackupsRequest{})
	if err != nil {
		t.Fatalf("non-admin ListBackups(empty): %v", err)
	}
	for _, j := range lbEmpty.Backups {
		if j.ClientId != "client-a" {
			t.Errorf("non-admin empty-id leaked job for %q; must only see own jobs", j.ClientId)
		}
	}
	if len(lbEmpty.Backups) != 1 {
		t.Errorf("empty-id non-admin should see only the caller's own 1 job, got %d", len(lbEmpty.Backups))
	}

	// GetRetention stays global/ungated.
	if _, err := srv.GetRetention(userContext, &proto.RetentionRequest{}); codeOf(err) == codes.PermissionDenied {
		t.Error("GetRetention must never be admin-gated for a non-admin caller")
	}
}

// --- (c) authorization keys off the trusted TLS identity, not a name ---

func TestAdminAuthz_NameSpoofViaClientIDMetadataStillDenied(t *testing.T) {
	reg := newAuthzRegistry(t)

	// The real admin, identified only by its cert SPKI (NAME "admin-ops").
	adminCert, adminFP := makeNamedCert(t, "admin-ops")
	if _, err := reg.Register("admin-ops", "tunnel://admin-ops"); err != nil {
		t.Fatalf("register admin: %v", err)
	}
	if err := reg.SetSPKIFingerprint("admin-ops", adminFP); err != nil {
		t.Fatalf("set admin spki: %v", err)
	}

	// A non-admin attacker with its own distinct key, registered as client-a.
	attackerCert, attackerFP := makeNamedCert(t, "client-a")
	if _, err := reg.Register("client-a", "tunnel://client-a"); err != nil {
		t.Fatalf("register attacker: %v", err)
	}
	if err := reg.SetSPKIFingerprint("client-a", attackerFP); err != nil {
		t.Fatalf("set attacker spki: %v", err)
	}
	if _, err := reg.Register("client-b", "tunnel://client-b"); err != nil {
		t.Fatalf("register B: %v", err)
	}

	cfgPath := writeAdminConfig(t, []config.AdminClient{{Name: "admin-ops", Fingerprint: adminFP}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	policy := NewConfigAdminPolicy(ctx, cfgPath, time.Hour)

	srv := NewCommandServer(CommandServerConfig{
		BackupEngine:      &mockBackupEngine{},
		Repo:              &scopeRepo{},
		Registry:          reg,
		AdminPolicy:       policy,
		CrossRestorer:     &stubCrossRestorer{},
		WatcherController: &stubWatcherController{},
		Version:           "test",
	})

	// Sanity: the real admin cert IS authorized.
	if !srv.callerIsAdmin(peerCtx(adminCert)) {
		t.Fatalf("precondition: the real admin cert should be authorized")
	}

	// The attacker sends client-id metadata equal to the admin's client NAME
	// "admin-ops", but presents its OWN cert. Authorization keys off the cert
	// SPKI, so the spoof must fail.
	spoofCtx := peerCtxWithClientID(attackerCert, "admin-ops")

	if srv.callerIsAdmin(spoofCtx) {
		t.Fatal("name-spoof via client-id metadata must NOT grant admin")
	}

	if _, err := srv.RestoreToTarget(spoofCtx, &proto.RestoreToTargetRequest{
		SourceClientId: "client-b",
		TargetClientId: "client-a",
		Query:          "*",
	}); codeOf(err) != codes.PermissionDenied {
		t.Errorf("spoofed RestoreToTarget code = %v, want PermissionDenied", codeOf(err))
	}

	if _, err := srv.ControlClientWatcher(spoofCtx, &proto.ControlWatcherRequest{
		TargetClientId: "client-b",
		Start:          true,
	}); codeOf(err) != codes.PermissionDenied {
		t.Errorf("spoofed ControlClientWatcher code = %v, want PermissionDenied", codeOf(err))
	}
}
