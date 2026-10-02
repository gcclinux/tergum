package grpc

import (
	"context"
	"testing"
	"time"

	"github.com/gcclinux/tergum/internal/config"
	"github.com/gcclinux/tergum/internal/grpc/proto"
	"github.com/gcclinux/tergum/internal/registry"
	"google.golang.org/grpc/codes"
)

// This file holds the authorization and data proofs for the admin-gated,
// read-only remote client-view RPCs (ListClients / GetClientStatus). They
// exercise the server handlers directly over contexts carrying a verified mTLS
// peer certificate — the same identity path the real transport uses
// (clientSPKIFromContext) — reusing the harness helpers in admin_authz_test.go
// (makeNamedCert, peerCtx, peerCtxWithClientID, newAuthzRegistry,
// writeAdminConfig, NewConfigAdminPolicy, codeOf). Authorization keys off the
// certificate SPKI fingerprint, never the CN or client-id metadata.

// buildRemoteViewServer builds a CommandServer whose trusted admin is the SPKI
// fingerprint adminFP (so a test's own fresh cert can be the admin). It
// registers client-a with richer state (schedule, watcher, last-seen,
// last-backup) and client-b minimal.
func buildRemoteViewServer(t *testing.T, adminFP string) *CommandServer {
	t.Helper()
	reg := newAuthzRegistry(t)

	if _, err := reg.Register("admin-ops", "tunnel://admin-ops"); err != nil {
		t.Fatalf("register admin: %v", err)
	}
	if err := reg.SetSPKIFingerprint("admin-ops", adminFP); err != nil {
		t.Fatalf("set admin spki: %v", err)
	}

	// client-a: richer state so GetClientStatus has fields to assert.
	if _, err := reg.Register("client-a", "10.0.0.1:7300"); err != nil {
		t.Fatalf("register A: %v", err)
	}
	if err := reg.Heartbeat("client-a"); err != nil { // sets last-seen + online
		t.Fatalf("heartbeat A: %v", err)
	}
	if err := reg.SetWatcherActive("client-a", true); err != nil {
		t.Fatalf("set A watcher: %v", err)
	}
	if err := reg.SetLastBackup("client-a", time.Now().Add(-2*time.Hour)); err != nil {
		t.Fatalf("set A last backup: %v", err)
	}
	if err := reg.SetSchedule("client-a", registry.ScheduleConfig{
		FullBackupCron: "0 2 * * 0",
		AutoBackupCron: "*/30 * * * *",
	}); err != nil {
		t.Fatalf("set A schedule: %v", err)
	}

	// client-b: minimal.
	if _, err := reg.Register("client-b", "10.0.0.2:7300"); err != nil {
		t.Fatalf("register B: %v", err)
	}

	cfgPath := writeAdminConfig(t, []config.AdminClient{{Name: "admin-ops", Fingerprint: adminFP}})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	policy := NewConfigAdminPolicy(ctx, cfgPath, time.Hour)
	if !policy.IsAdmin(adminFP) {
		t.Fatalf("precondition: admin fingerprint should be authorized")
	}

	return NewCommandServer(CommandServerConfig{
		BackupEngine: &mockBackupEngine{},
		Repo:         &scopeRepo{},
		Registry:     reg,
		AdminPolicy:  policy,
		Version:      "test",
	})
}

// --- (a) admin can call both RPCs and receives correct data ---

func TestAdminRemoteView_AdminAllowedAndCorrectData(t *testing.T) {
	adminCert, adminFP := makeNamedCert(t, "admin-ops")
	srv := buildRemoteViewServer(t, adminFP)
	adminCtx := peerCtx(adminCert)

	// ListClients returns every registered client (admin-ops, client-a,
	// client-b), sorted by client ID.
	lc, err := srv.ListClients(adminCtx, &proto.ListClientsRequest{})
	if err != nil {
		t.Fatalf("admin ListClients error: %v", err)
	}
	byID := make(map[string]*proto.ClientSummary, len(lc.Clients))
	var ids []string
	for _, c := range lc.Clients {
		byID[c.ClientId] = c
		ids = append(ids, c.ClientId)
	}
	for _, want := range []string{"admin-ops", "client-a", "client-b"} {
		if byID[want] == nil {
			t.Fatalf("expected client %q in list, got %v", want, ids)
		}
	}
	// Results are sorted ascending by client ID.
	for i := 1; i < len(lc.Clients); i++ {
		if lc.Clients[i-1].ClientId > lc.Clients[i].ClientId {
			t.Errorf("list not sorted by client ID: %v", ids)
		}
	}
	a := byID["client-a"]
	if a.Address != "10.0.0.1:7300" {
		t.Errorf("client-a address = %q, want 10.0.0.1:7300", a.Address)
	}
	// Timestamps are RFC3339 and non-empty where set.
	if a.LastSeen == "" {
		t.Error("client-a last_seen should be set")
	} else if _, err := time.Parse(time.RFC3339, a.LastSeen); err != nil {
		t.Errorf("client-a last_seen not RFC3339: %q", a.LastSeen)
	}
	if a.LastBackup == "" {
		t.Error("client-a last_backup should be set")
	} else if _, err := time.Parse(time.RFC3339, a.LastBackup); err != nil {
		t.Errorf("client-a last_backup not RFC3339: %q", a.LastBackup)
	}

	// GetClientStatus(client-a) returns Found with the full set of fields.
	st, err := srv.GetClientStatus(adminCtx, &proto.GetClientStatusRequest{ClientId: "client-a"})
	if err != nil {
		t.Fatalf("admin GetClientStatus error: %v", err)
	}
	if !st.Found {
		t.Fatal("client-a status should be Found")
	}
	if st.Status == "" {
		t.Error("client-a status string should be set")
	}
	if !st.WatcherActive {
		t.Error("client-a watcher should be active")
	}
	if !st.HasSchedule || st.FullBackupCron != "0 2 * * 0" || st.AutoBackupCron != "*/30 * * * *" {
		t.Errorf("client-a schedule not carried: has=%v full=%q auto=%q",
			st.HasSchedule, st.FullBackupCron, st.AutoBackupCron)
	}
	if st.LastBackup == "" {
		t.Error("client-a status last_backup should be set")
	} else if _, err := time.Parse(time.RFC3339, st.LastBackup); err != nil {
		t.Errorf("client-a status last_backup not RFC3339: %q", st.LastBackup)
	}
}

// --- (b) non-admin is denied both RPCs ---

func TestAdminRemoteView_NonAdminDenied(t *testing.T) {
	_, adminFP := makeNamedCert(t, "admin-ops")
	srv := buildRemoteViewServer(t, adminFP)

	userCert, userFP := makeNamedCert(t, "client-a")
	if srv.callerIsAdmin(peerCtx(userCert)) {
		t.Fatalf("precondition: non-admin caller must not be authorized (fp %s)", userFP)
	}
	userCtx := peerCtx(userCert)

	if _, err := srv.ListClients(userCtx, &proto.ListClientsRequest{}); codeOf(err) != codes.PermissionDenied {
		t.Errorf("non-admin ListClients code = %v, want PermissionDenied", codeOf(err))
	}
	if _, err := srv.GetClientStatus(userCtx, &proto.GetClientStatusRequest{ClientId: "client-a"}); codeOf(err) != codes.PermissionDenied {
		t.Errorf("non-admin GetClientStatus code = %v, want PermissionDenied", codeOf(err))
	}
}

// --- (c) anti-spoof: authorization keys off the trusted SPKI, not a name ---

func TestAdminRemoteView_NameSpoofViaClientIDMetadataStillDenied(t *testing.T) {
	_, adminFP := makeNamedCert(t, "admin-ops")
	srv := buildRemoteViewServer(t, adminFP)

	// Attacker presents its OWN cert but injects client-id metadata equal to the
	// admin's client NAME. Authorization keys off the cert SPKI, so this fails.
	attackerCert, _ := makeNamedCert(t, "client-a")
	spoofCtx := peerCtxWithClientID(attackerCert, "admin-ops")

	if srv.callerIsAdmin(spoofCtx) {
		t.Fatal("name-spoof via client-id metadata must NOT grant admin")
	}
	if _, err := srv.ListClients(spoofCtx, &proto.ListClientsRequest{}); codeOf(err) != codes.PermissionDenied {
		t.Errorf("spoofed ListClients code = %v, want PermissionDenied", codeOf(err))
	}
	if _, err := srv.GetClientStatus(spoofCtx, &proto.GetClientStatusRequest{ClientId: "client-a"}); codeOf(err) != codes.PermissionDenied {
		t.Errorf("spoofed GetClientStatus code = %v, want PermissionDenied", codeOf(err))
	}
}

// --- (d) unknown client to an admin returns Found=false, no error ---

func TestAdminRemoteView_UnknownClientReturnsNotFound(t *testing.T) {
	adminCert, adminFP := makeNamedCert(t, "admin-ops")
	srv := buildRemoteViewServer(t, adminFP)
	adminCtx := peerCtx(adminCert)

	st, err := srv.GetClientStatus(adminCtx, &proto.GetClientStatusRequest{ClientId: "no-such-client"})
	if err != nil {
		t.Fatalf("admin GetClientStatus(unknown) error: %v", err)
	}
	if st.Found {
		t.Error("unknown client must report Found=false")
	}
}
