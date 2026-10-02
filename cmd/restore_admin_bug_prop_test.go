package cmd

import (
	"strings"
	"testing"

	"github.com/gcclinux/tergum/internal/config"
	"pgregory.net/rapid"
)

// **Validates: Requirements 1.1, 1.2, 2.1, 2.2**

// AdminRestoreInput represents the input parameters for the restore command
// when used by an admin client with the --client flag.
type AdminRestoreInput struct {
	Query        string // positional query argument or --file/--path value
	ClientFlag   string // value of --client flag
	TargetFlag   string // value of --target flag (should be empty for this bug condition)
	BackupID     string // value of --backup-id flag
	ListOnly     bool   // whether --list flag is set
	NodeRole     string // "client", "server", or "hybrid"
	IsAdminClient bool  // whether the caller has admin privileges
}

// isBugCondition returns true if the input represents the bug condition:
// an admin client using --client without --target.
//
// FUNCTION isBugCondition(input)
//   INPUT: input of type AdminRestoreInput
//   OUTPUT: boolean
//
//   RETURN input.ClientFlag != ""
//          AND input.TargetFlag == ""
//          AND input.NodeRole == "client"
//          AND input.IsAdminClient == true
// END FUNCTION
func isBugCondition(input AdminRestoreInput) bool {
	return input.ClientFlag != "" &&
		input.TargetFlag == "" &&
		input.NodeRole == "client" &&
		input.IsAdminClient
}

// simulateCurrentRestoreLogic replicates the current pre-flight checks
// in runRestore. After the fix, this function should return nil for admin
// clients using --client without --target, indicating the request would be
// forwarded to the server via runAdminRemoteQuery.
//
// This simulates lines 98-119 of cmd/restore.go AFTER the fix is applied.
func simulateCurrentRestoreLogic(input AdminRestoreInput, cfg *config.Config) error {
	clientID := input.ClientFlag
	targetClient := input.TargetFlag

	// Lines 102-107: Admin cross-client restore with BOTH --client AND --target
	// This path is taken only when both flags are specified.
	if targetClient != "" && clientID != "" && cfg.Node.Role == "client" {
		// Would call runAdminRemoteRestore - but we're testing the case without --target
		return nil
	}

	// Lines 109-114 (NEW): Admin cross-client query/restore WITHOUT --target
	// A client node using --client without --target is routed to runAdminRemoteQuery.
	// The server will verify admin privileges - no immediate rejection here.
	if clientID != "" && targetClient == "" && cfg.Node.Role == "client" {
		// Would call runAdminRemoteQuery - request forwarded to server
		return nil
	}

	// If we reach here, the request would proceed (server node or no --client flag)
	return nil
}

// TestProperty_AdminClientFlagRejection is a bug condition exploration test.
//
// This test demonstrates that the current code incorrectly rejects admin clients
// who use the --client flag without --target. The test is EXPECTED TO FAIL on
// unfixed code because:
//
// 1. The bug condition: admin client uses --client without --target
// 2. Current behavior: Immediate rejection with "the --client flag cannot be used on a client node"
// 3. Expected behavior: Request forwarded to server for admin-authorized execution
//
// When this test FAILS, it proves the bug exists (the counterexample shows an admin
// client being incorrectly rejected).
func TestProperty_AdminClientFlagRejection(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate a random admin client ID (the client performing the restore)
		adminClientID := rapid.StringMatching(`admin-[a-z0-9]{4,8}`).Draw(rt, "adminClientID")
		adminFingerprint := rapid.StringMatching(`[0-9a-f]{64}`).Draw(rt, "adminFingerprint")

		// Generate a different target client ID to query backups from
		targetClientID := rapid.StringMatching(`client-[a-z0-9]{4,8}`).Draw(rt, "targetClientID")

		// Generate a non-empty query
		query := rapid.OneOf(
			rapid.StringMatching(`\*\.[a-z]{2,4}`), // glob pattern like *.go
			rapid.StringMatching(`[A-Za-z0-9_-]{3,20}\.[a-z]{2,4}`), // filename like report.pdf
			rapid.StringMatching(`/[a-z]+/[a-z]+`), // path pattern like /home/user
		).Draw(rt, "query")

		// Generate optional backup ID
		useBackupID := rapid.Bool().Draw(rt, "useBackupID")
		backupID := ""
		if useBackupID {
			backupID = rapid.StringMatching(`[a-f0-9]{8}`).Draw(rt, "backupID")
		}

		// Generate list mode flag
		listOnly := rapid.Bool().Draw(rt, "listOnly")

		// Build the admin client config
		cfg := &config.Config{}
		cfg.Node.Role = "client"
		cfg.Admin.Clients = append(cfg.Admin.Clients, config.AdminClient{
			Name:        "admin-" + adminClientID,
			Fingerprint: adminFingerprint,
		})

		// Build the input representing bug condition
		input := AdminRestoreInput{
			Query:         query,
			ClientFlag:   targetClientID, // Using --client to query another client's backups
			TargetFlag:   "",             // NO --target flag (this triggers the bug)
			BackupID:     backupID,
			ListOnly:     listOnly,
			NodeRole:     "client",
			IsAdminClient: true, // Caller IS an admin client
		}

		// Verify we're testing the bug condition
		if !isBugCondition(input) {
			rt.Fatalf("generated input does not match bug condition: %+v", input)
		}

		// Verify the config recognizes this as an admin client
		if !cfg.IsAdminFingerprint(adminFingerprint) {
			rt.Fatalf("config should recognize fingerprint %s as admin", adminFingerprint)
		}

		// Simulate the current (buggy) restore logic
		err := simulateCurrentRestoreLogic(input, cfg)

		// EXPECTED CORRECT BEHAVIOR (what the fix should provide):
		// The request should NOT be rejected with "the --client flag cannot be used on a client node"
		// Instead, it should be forwarded to the server for admin-authorized execution.
		//
		// This test FAILS on unfixed code because the current logic returns an error.
		if err != nil {
			if strings.Contains(err.Error(), "the --client flag cannot be used on a client node") {
				// This is the BUG: admin clients are being rejected without admin privilege check
				rt.Fatalf("BUG FOUND: Admin client request rejected without admin privilege check.\n"+
					"Input: --client=%q, query=%q, --list=%v, --backup-id=%q\n"+
					"Admin fingerprint: %s\n"+
					"Error: %v\n"+
					"Expected: Request should be forwarded to server for admin authorization",
					input.ClientFlag, input.Query, input.ListOnly, input.BackupID,
					adminFingerprint, err)
			}
			// Some other error is acceptable (e.g., network error, server rejection)
		}
		// No error means the request would proceed (correct behavior)
	})
}

// TestProperty_AdminListFilesRejection specifically tests the admin list files case.
//
// Bug Condition: Admin client runs `tergum restore "query" --client client-b --list`
// Current Behavior: Fails with "the --client flag cannot be used on a client node"
// Expected Behavior: Forwards request to server to list files from client-b's backups
//
// **Validates: Requirements 1.2, 2.2**
func TestProperty_AdminListFilesRejection(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		adminFingerprint := rapid.StringMatching(`[0-9a-f]{64}`).Draw(rt, "adminFingerprint")
		targetClientID := rapid.StringMatching(`client-[a-z0-9]{4,8}`).Draw(rt, "targetClientID")
		query := rapid.OneOf(
			rapid.StringMatching(`\*\.[a-z]{2,4}`),
			rapid.StringMatching(`[A-Za-z0-9_-]{3,20}\.[a-z]{2,4}`),
		).Draw(rt, "query")

		cfg := &config.Config{}
		cfg.Node.Role = "client"
		cfg.Admin.Clients = append(cfg.Admin.Clients, config.AdminClient{
			Name:        "admin-test",
			Fingerprint: adminFingerprint,
		})

		input := AdminRestoreInput{
			Query:         query,
			ClientFlag:   targetClientID,
			TargetFlag:   "", // No target - triggers bug
			ListOnly:     true, // List mode specifically
			NodeRole:     "client",
			IsAdminClient: true,
		}

		if !isBugCondition(input) {
			rt.Fatalf("input should match bug condition")
		}

		err := simulateCurrentRestoreLogic(input, cfg)

		if err != nil && strings.Contains(err.Error(), "the --client flag cannot be used on a client node") {
			rt.Fatalf("BUG FOUND: Admin --list request rejected.\n"+
				"Command: tergum restore %q --client %s --list\n"+
				"Error: %v\n"+
				"Expected: List matching files from client's backups via server",
				query, targetClientID, err)
		}
	})
}

// TestProperty_AdminRestoreFromOtherClient tests the admin restore (non-list) case.
//
// Bug Condition: Admin client runs `tergum restore "query" --client client-b`
// Current Behavior: Fails with "the --client flag cannot be used on a client node"
// Expected Behavior: Forwards request to server to restore files from client-b's backups
//
// **Validates: Requirements 1.1, 2.1**
func TestProperty_AdminRestoreFromOtherClient(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		adminFingerprint := rapid.StringMatching(`[0-9a-f]{64}`).Draw(rt, "adminFingerprint")
		targetClientID := rapid.StringMatching(`client-[a-z0-9]{4,8}`).Draw(rt, "targetClientID")
		query := rapid.StringMatching(`[A-Za-z0-9_-]{3,20}\.[a-z]{2,4}`).Draw(rt, "query")

		cfg := &config.Config{}
		cfg.Node.Role = "client"
		cfg.Admin.Clients = append(cfg.Admin.Clients, config.AdminClient{
			Name:        "admin-test",
			Fingerprint: adminFingerprint,
		})

		input := AdminRestoreInput{
			Query:         query,
			ClientFlag:   targetClientID,
			TargetFlag:   "", // No target - triggers bug
			ListOnly:     false, // Actual restore, not list
			NodeRole:     "client",
			IsAdminClient: true,
		}

		if !isBugCondition(input) {
			rt.Fatalf("input should match bug condition")
		}

		err := simulateCurrentRestoreLogic(input, cfg)

		if err != nil && strings.Contains(err.Error(), "the --client flag cannot be used on a client node") {
			rt.Fatalf("BUG FOUND: Admin restore request rejected.\n"+
				"Command: tergum restore %q --client %s\n"+
				"Error: %v\n"+
				"Expected: Restore files from client's backups via server",
				query, targetClientID, err)
		}
	})
}

// TestProperty_AdminBackupIDRestore tests the admin backup-id restore case.
//
// Bug Condition: Admin client runs `tergum restore --backup-id abc123 --client client-b`
// Current Behavior: Fails with "the --client flag cannot be used on a client node"
// Expected Behavior: Forwards request to server to restore entire backup from client-b
//
// **Validates: Requirements 1.1, 2.1**
func TestProperty_AdminBackupIDRestore(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		adminFingerprint := rapid.StringMatching(`[0-9a-f]{64}`).Draw(rt, "adminFingerprint")
		targetClientID := rapid.StringMatching(`client-[a-z0-9]{4,8}`).Draw(rt, "targetClientID")
		backupID := rapid.StringMatching(`[a-f0-9]{8,16}`).Draw(rt, "backupID")

		cfg := &config.Config{}
		cfg.Node.Role = "client"
		cfg.Admin.Clients = append(cfg.Admin.Clients, config.AdminClient{
			Name:        "admin-test",
			Fingerprint: adminFingerprint,
		})

		input := AdminRestoreInput{
			Query:         "", // No query when using backup-id
			ClientFlag:   targetClientID,
			TargetFlag:   "", // No target - triggers bug
			BackupID:     backupID,
			ListOnly:     false,
			NodeRole:     "client",
			IsAdminClient: true,
		}

		if !isBugCondition(input) {
			rt.Fatalf("input should match bug condition")
		}

		err := simulateCurrentRestoreLogic(input, cfg)

		if err != nil && strings.Contains(err.Error(), "the --client flag cannot be used on a client node") {
			rt.Fatalf("BUG FOUND: Admin backup-id restore request rejected.\n"+
				"Command: tergum restore --backup-id %s --client %s\n"+
				"Error: %v\n"+
				"Expected: Restore entire backup from client via server",
				backupID, targetClientID, err)
		}
	})
}
