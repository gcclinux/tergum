package cmd

import (
	"io"
	"os"
	"testing"
	"time"

	"github.com/gcclinux/tergum/internal/grpc/proto"
	"github.com/gcclinux/tergum/internal/registry"
)

// captureStdout runs fn with os.Stdout redirected to a pipe and returns what
// was written. It follows the save/restore discipline used elsewhere in cmd
// tests for the global jsonOut flag.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("close pipe: %v", err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read pipe: %v", err)
	}
	return string(out)
}

// fixedTimes are stable timestamps so formatTimeAgo's relative output is
// deterministic across the two mappings being compared (they render from the
// SAME absolute instants, so even the relative column matches).
var (
	fixedLastSeen     = time.Now().Add(-90 * time.Minute)
	fixedLastBackup   = time.Now().Add(-26 * time.Hour)
	fixedRegisteredAt = time.Now().Add(-240 * time.Hour)
	fixedMissedAt     = time.Now().Add(-3 * time.Hour)
)

// --- list: local vs remote render identically ---

func TestClientRender_ListLocalRemoteIdentical(t *testing.T) {
	origJSON := jsonOut
	defer func() { jsonOut = origJSON }()

	// Build an equivalent client via both source shapes.
	ci := registry.ClientInfo{
		ClientID:   "client-a",
		Address:    "10.0.0.1:7300",
		OSFamily:   "linux",
		Hostname:   "host-a",
		MachineID:  "machine-a",
		Status:     "online",
		LastSeen:   fixedLastSeen,
		LastBackup: fixedLastBackup, // registry value present -> resolveLastBackup returns it
	}
	ci.RegisteredAt = fixedRegisteredAt

	localRows := []clientListRow{{
		ClientID:     ci.ClientID,
		Address:      ci.Address,
		OSFamily:     ci.OSFamily,
		Hostname:     ci.Hostname,
		MachineID:    ci.MachineID,
		Status:       ci.Status,
		LastSeen:     ci.LastSeen,
		LastBackup:   resolveLastBackup(&ci, t.TempDir()),
		RegisteredAt: ci.RegisteredAt,
	}}

	// The remote path builds rows from a proto response carrying the same instants
	// as RFC3339 (UTC), parsed back to time.Time.
	resp := &proto.ListClientsResponse{Clients: []*proto.ClientSummary{{
		ClientId:     ci.ClientID,
		Address:      ci.Address,
		OsFamily:     ci.OSFamily,
		Hostname:     ci.Hostname,
		MachineId:    ci.MachineID,
		Status:       ci.Status,
		LastSeen:     ci.LastSeen.UTC().Format(time.RFC3339),
		LastBackup:   ci.LastBackup.UTC().Format(time.RFC3339),
		RegisteredAt: ci.RegisteredAt.UTC().Format(time.RFC3339),
	}}}
	remoteRows := make([]clientListRow, 0, len(resp.Clients))
	for _, c := range resp.Clients {
		remoteRows = append(remoteRows, clientListRow{
			ClientID:     c.ClientId,
			Address:      c.Address,
			OSFamily:     c.OsFamily,
			Hostname:     c.Hostname,
			MachineID:    c.MachineId,
			Status:       c.Status,
			LastSeen:     parseProtoTime(c.LastSeen),
			LastBackup:   parseProtoTime(c.LastBackup),
			RegisteredAt: parseProtoTime(c.RegisteredAt),
		})
	}

	for _, json := range []bool{false, true} {
		jsonOut = json
		local := captureStdout(t, func() { renderClientList(localRows) })
		remote := captureStdout(t, func() { renderClientList(remoteRows) })
		if local != remote {
			t.Errorf("list render mismatch (json=%v):\nlocal:\n%s\nremote:\n%s", json, local, remote)
		}
	}
}

// --- status: local vs remote render identically ---

func TestClientRender_StatusLocalRemoteIdentical(t *testing.T) {
	origJSON := jsonOut
	defer func() { jsonOut = origJSON }()

	ci := registry.ClientInfo{
		ClientID:      "client-a",
		Address:       "10.0.0.1:7300",
		OSFamily:      "linux",
		Hostname:      "host-a",
		MachineID:     "machine-a",
		Status:        "online",
		Disabled:      false,
		WatcherActive: true,
		LastSeen:      fixedLastSeen,
		LastBackup:    fixedLastBackup,
		RegisteredAt:  fixedRegisteredAt,
		Schedule: &registry.ScheduleConfig{
			FullBackupCron: "0 2 * * 0",
			AutoBackupCron: "*/30 * * * *",
		},
		MissedBackups: []registry.MissedBackup{
			{Level: "auto", ScheduledAt: fixedMissedAt},
		},
	}

	// Local view from registry.ClientInfo.
	localView := clientStatusView{
		ClientID:      ci.ClientID,
		Address:       ci.Address,
		OSFamily:      ci.OSFamily,
		Hostname:      ci.Hostname,
		MachineID:     ci.MachineID,
		Status:        ci.Status,
		Disabled:      ci.Disabled,
		WatcherActive: ci.WatcherActive,
		LastSeen:      ci.LastSeen,
		LastBackup:    resolveLastBackup(&ci, t.TempDir()),
		RegisteredAt:  ci.RegisteredAt,
		Schedule: &clientSchedule{
			FullBackupCron: ci.Schedule.FullBackupCron,
			AutoBackupCron: ci.Schedule.AutoBackupCron,
		},
	}
	for _, mb := range ci.MissedBackups {
		localView.MissedBackups = append(localView.MissedBackups, clientMissedBackup{
			Level:       mb.Level,
			ScheduledAt: mb.ScheduledAt,
		})
	}

	// Remote view from a proto response carrying the same instants as RFC3339.
	resp := &proto.ClientStatusResponse{
		Found:          true,
		ClientId:       ci.ClientID,
		Address:        ci.Address,
		OsFamily:       ci.OSFamily,
		Hostname:       ci.Hostname,
		MachineId:      ci.MachineID,
		Status:         ci.Status,
		Disabled:       ci.Disabled,
		WatcherActive:  ci.WatcherActive,
		LastSeen:       ci.LastSeen.UTC().Format(time.RFC3339),
		LastBackup:     ci.LastBackup.UTC().Format(time.RFC3339),
		RegisteredAt:   ci.RegisteredAt.UTC().Format(time.RFC3339),
		HasSchedule:    true,
		FullBackupCron: ci.Schedule.FullBackupCron,
		AutoBackupCron: ci.Schedule.AutoBackupCron,
		MissedBackups:  int32(len(ci.MissedBackups)),
		MissedBackupDetails: []*proto.MissedBackupDetail{
			{Level: "auto", ScheduledAt: fixedMissedAt.UTC().Format(time.RFC3339)},
		},
	}
	remoteView := clientStatusView{
		ClientID:      resp.ClientId,
		Address:       resp.Address,
		OSFamily:      resp.OsFamily,
		Hostname:      resp.Hostname,
		MachineID:     resp.MachineId,
		Status:        resp.Status,
		Disabled:      resp.Disabled,
		WatcherActive: resp.WatcherActive,
		LastSeen:      parseProtoTime(resp.LastSeen),
		LastBackup:    parseProtoTime(resp.LastBackup),
		RegisteredAt:  parseProtoTime(resp.RegisteredAt),
	}
	if resp.HasSchedule {
		remoteView.Schedule = &clientSchedule{
			FullBackupCron: resp.FullBackupCron,
			AutoBackupCron: resp.AutoBackupCron,
		}
	}
	for _, mb := range resp.MissedBackupDetails {
		remoteView.MissedBackups = append(remoteView.MissedBackups, clientMissedBackup{
			Level:       mb.Level,
			ScheduledAt: parseProtoTime(mb.ScheduledAt),
		})
	}

	for _, json := range []bool{false, true} {
		jsonOut = json
		local := captureStdout(t, func() { renderClientStatus(localView) })
		remote := captureStdout(t, func() { renderClientStatus(remoteView) })
		if local != remote {
			t.Errorf("status render mismatch (json=%v):\nlocal:\n%s\nremote:\n%s", json, local, remote)
		}
	}
}

// --- list: empty renders the stable "No clients registered." line ---

func TestClientRender_ListEmpty(t *testing.T) {
	origJSON := jsonOut
	defer func() { jsonOut = origJSON }()
	jsonOut = false

	out := captureStdout(t, func() { renderClientList(nil) })
	if out != "No clients registered.\n" {
		t.Errorf("empty list output = %q, want %q", out, "No clients registered.\n")
	}
}
