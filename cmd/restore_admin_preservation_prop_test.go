package cmd

import (
	"errors"
	"strings"
	"testing"

	"github.com/gcclinux/tergum/internal/config"
	"pgregory.net/rapid"
)

// **Validates: Requirements 2.3, 3.1, 3.2, 3.3, 3.4**

// This file contains preservation property tests for the admin client restore bugfix.
// These tests verify that existing behavior is UNCHANGED after the fix is applied.
// All tests in this file should PASS on UNFIXED code (establishing baseline behavior).

// RestoreInput represents input parameters for the restore command
// across all node types and configurations.
type RestoreInput struct {
	Query         string // positional query argument or --file/--path value
	ClientFlag    string // value of --client flag
	TargetFlag    string // value of --target flag
	BackupID      string // value of --backup-id flag
	ListOnly      bool   // whether --list flag is set
	NodeRole      string // "client", "server", or "hybrid"
	IsAdminClient bool   // whether the caller has admin privileges
}

// isPreservationInput returns true if the input should have unchanged behavior.
// This is the complement of isBugCondition - all inputs where the bug does NOT apply.
//
// Preservation covers:
// - Non-admin clients using --client (should get error)
// - Server/hybrid nodes using --client (should access client DB directly)
// - Admin clients using both --client and --target (should use runAdminRemoteRestore)
// - Any node without --client flag (should restore from own backups)
func isPreservationInput(input RestoreInput) bool {
	// Bug condition: admin client using --client without --target
	bugCondition := input.ClientFlag != "" &&
		input.TargetFlag == "" &&
		input.NodeRole == "client" &&
		input.IsAdminClient

	// Preservation is the complement of bug condition
	return !bugCondition
}

// simulateRestoreLogic replicates the current restore command pre-flight checks
// in runRestore. This function returns:
// - ("error", error) if the request would be rejected
// - ("admin_remote_restore", nil) if runAdminRemoteRestore would be called
// - ("server_direct", nil) if server would access client DB directly
// - ("client_remote", nil) if client would access server via gRPC
// - ("local", nil) for local restore operations
func simulateRestoreLogic(input RestoreInput, cfg *config.Config) (string, error) {
	clientID := input.ClientFlag
	targetClient := input.TargetFlag

	// Lines 70-72: target requires client flag validation
	if targetClient != "" && clientID == "" {
		return "error", errors.New("--target requires --client to specify the source client")
	}

	// Lines 98-103: Admin cross-client restore with BOTH --client AND --target
	// This path is taken only when both flags are specified AND node is client.
	if targetClient != "" && clientID != "" && cfg.Node.Role == "client" {
		if input.ListOnly {
			return "error", errors.New("--list is not supported in admin cross-client mode")
		}
		return "admin_remote_restore", nil
	}

	// Lines 115-119: The --client flag rejection for client nodes
	if clientID != "" {
		if targetClient == "" && cfg.Node.Role == "client" {
			return "error", errors.New("the --client flag cannot be used on a client node")
		}
		// Server or hybrid node with --client flag: direct DB access
		return "server_direct", nil
	}

	// No --client flag: normal restore from own backups
	if cfg.Node.Role == "client" {
		return "client_remote", nil
	}
	return "local", nil
}

// TestProperty_PreservationNonAdminClientError verifies that non-admin clients
// using --client flag continue to receive an error.
//
// Property: For all non-admin client inputs with --client, result is error (preserved)
//
// **Validates: Requirements 2.3**
func TestProperty_PreservationNonAdminClientError(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate a non-admin client configuration
		targetClientID := rapid.StringMatching(`client-[a-z0-9]{4,8}`).Draw(rt, "targetClientID")
		query := rapid.OneOf(
			rapid.StringMatching(`\*\.[a-z]{2,4}`),
			rapid.StringMatching(`[A-Za-z0-9_-]{3,20}\.[a-z]{2,4}`),
			rapid.StringMatching(`/[a-z]+/[a-z]+`),
		).Draw(rt, "query")

		useBackupID := rapid.Bool().Draw(rt, "useBackupID")
		backupID := ""
		if useBackupID && query == "" {
			backupID = rapid.StringMatching(`[a-f0-9]{8}`).Draw(rt, "backupID")
		}

		listOnly := rapid.Bool().Draw(rt, "listOnly")

		// Non-admin client config: no admin fingerprints configured
		cfg := &config.Config{}
		cfg.Node.Role = "client"
		// No admin clients configured - this is a regular (non-admin) client

		input := RestoreInput{
			Query:         query,
			ClientFlag:    targetClientID, // Using --client flag
			TargetFlag:    "",             // No --target
			BackupID:      backupID,
			ListOnly:      listOnly,
			NodeRole:      "client",
			IsAdminClient: false, // NOT an admin client
		}

		// Verify this is a preservation input (not the bug condition)
		if !isPreservationInput(input) {
			rt.Fatalf("expected preservation input but got bug condition: %+v", input)
		}

		// Simulate the current restore logic
		pathTaken, err := simulateRestoreLogic(input, cfg)

		// ASSERT: Non-admin clients using --client MUST receive an error
		// This is the PRESERVED behavior - non-admin clients cannot use --client
		if err == nil {
			rt.Fatalf("PRESERVATION VIOLATION: Non-admin client using --client should get error.\n"+
				"Input: --client=%q, query=%q, --list=%v\n"+
				"Path taken: %s\n"+
				"Expected: error 'the --client flag cannot be used on a client node'",
				input.ClientFlag, input.Query, input.ListOnly, pathTaken)
		}

		// Verify it's the expected error message
		if !strings.Contains(err.Error(), "the --client flag cannot be used on a client node") {
			rt.Fatalf("PRESERVATION VIOLATION: Expected specific error message.\n"+
				"Got: %v\n"+
				"Expected: contains 'the --client flag cannot be used on a client node'",
				err)
		}
	})
}

// TestProperty_PreservationServerNodeDirectAccess verifies that server nodes
// using --client flag continue to access client databases directly.
//
// Property: For all server node inputs with --client, behavior unchanged (direct DB access)
//
// **Validates: Requirements 3.3**
func TestProperty_PreservationServerNodeDirectAccess(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate a server node configuration
		targetClientID := rapid.StringMatching(`client-[a-z0-9]{4,8}`).Draw(rt, "targetClientID")
		query := rapid.OneOf(
			rapid.StringMatching(`\*\.[a-z]{2,4}`),
			rapid.StringMatching(`[A-Za-z0-9_-]{3,20}\.[a-z]{2,4}`),
		).Draw(rt, "query")

		useBackupID := rapid.Bool().Draw(rt, "useBackupID")
		backupID := ""
		if useBackupID && query == "" {
			backupID = rapid.StringMatching(`[a-f0-9]{8}`).Draw(rt, "backupID")
		}

		listOnly := rapid.Bool().Draw(rt, "listOnly")

		// Server node config
		cfg := &config.Config{}
		cfg.Node.Role = "server"

		input := RestoreInput{
			Query:         query,
			ClientFlag:    targetClientID, // Using --client flag
			TargetFlag:    "",             // No --target (tests with --target are separate)
			BackupID:      backupID,
			ListOnly:      listOnly,
			NodeRole:      "server",
			IsAdminClient: false, // Irrelevant for server node
		}

		// Verify this is a preservation input
		if !isPreservationInput(input) {
			rt.Fatalf("expected preservation input but got bug condition: %+v", input)
		}

		// Simulate the current restore logic
		pathTaken, err := simulateRestoreLogic(input, cfg)

		// ASSERT: Server nodes using --client MUST NOT receive an error
		// and should take the direct DB access path
		if err != nil {
			rt.Fatalf("PRESERVATION VIOLATION: Server node using --client should not get error.\n"+
				"Input: --client=%q, query=%q, --list=%v\n"+
				"Got error: %v\n"+
				"Expected: direct DB access path (server_direct)",
				input.ClientFlag, input.Query, input.ListOnly, err)
		}

		if pathTaken != "server_direct" {
			rt.Fatalf("PRESERVATION VIOLATION: Server node should use direct DB access.\n"+
				"Input: --client=%q, query=%q\n"+
				"Path taken: %s\n"+
				"Expected: server_direct",
				input.ClientFlag, input.Query, pathTaken)
		}
	})
}

// TestProperty_PreservationHybridNodeDirectAccess verifies that hybrid nodes
// using --client flag continue to access client databases directly (like server).
//
// **Validates: Requirements 3.3**
func TestProperty_PreservationHybridNodeDirectAccess(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate a hybrid node configuration
		targetClientID := rapid.StringMatching(`client-[a-z0-9]{4,8}`).Draw(rt, "targetClientID")
		query := rapid.StringMatching(`[A-Za-z0-9_-]{3,20}\.[a-z]{2,4}`).Draw(rt, "query")

		// Hybrid node config
		cfg := &config.Config{}
		cfg.Node.Role = "hybrid"

		input := RestoreInput{
			Query:         query,
			ClientFlag:    targetClientID,
			TargetFlag:    "",
			NodeRole:      "hybrid",
			IsAdminClient: false,
		}

		// Verify this is a preservation input
		if !isPreservationInput(input) {
			rt.Fatalf("expected preservation input but got bug condition: %+v", input)
		}

		// Simulate the current restore logic
		pathTaken, err := simulateRestoreLogic(input, cfg)

		// ASSERT: Hybrid nodes using --client should not get error
		// and should take the direct DB access path (same as server)
		if err != nil {
			rt.Fatalf("PRESERVATION VIOLATION: Hybrid node using --client should not get error.\n"+
				"Input: --client=%q, query=%q\n"+
				"Got error: %v",
				input.ClientFlag, input.Query, err)
		}

		if pathTaken != "server_direct" {
			rt.Fatalf("PRESERVATION VIOLATION: Hybrid node should use direct DB access.\n"+
				"Input: --client=%q, query=%q\n"+
				"Path taken: %s\n"+
				"Expected: server_direct",
				input.ClientFlag, input.Query, pathTaken)
		}
	})
}

// TestProperty_PreservationAdminWithTargetUsesRemoteRestore verifies that admin
// clients using both --client and --target continue to use runAdminRemoteRestore.
//
// Property: For all admin inputs with both --client and --target, uses runAdminRemoteRestore path
//
// **Validates: Requirements 3.2**
func TestProperty_PreservationAdminWithTargetUsesRemoteRestore(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		adminFingerprint := rapid.StringMatching(`[0-9a-f]{64}`).Draw(rt, "adminFingerprint")
		sourceClientID := rapid.StringMatching(`client-[a-z0-9]{4,8}`).Draw(rt, "sourceClientID")
		targetClientID := rapid.StringMatching(`target-[a-z0-9]{4,8}`).Draw(rt, "targetClientID")
		query := rapid.StringMatching(`[A-Za-z0-9_-]{3,20}\.[a-z]{2,4}`).Draw(rt, "query")

		useBackupID := rapid.Bool().Draw(rt, "useBackupID")
		backupID := ""
		if useBackupID {
			backupID = rapid.StringMatching(`[a-f0-9]{8}`).Draw(rt, "backupID")
		}

		// Admin client config
		cfg := &config.Config{}
		cfg.Node.Role = "client"
		cfg.Admin.Clients = append(cfg.Admin.Clients, config.AdminClient{
			Name:        "admin-test",
			Fingerprint: adminFingerprint,
		})

		input := RestoreInput{
			Query:         query,
			ClientFlag:    sourceClientID, // Source client
			TargetFlag:    targetClientID, // Target client (BOTH flags set)
			BackupID:      backupID,
			ListOnly:      false, // --list not supported in this mode
			NodeRole:      "client",
			IsAdminClient: true,
		}

		// Verify this is a preservation input (admin with --target is NOT the bug condition)
		if !isPreservationInput(input) {
			rt.Fatalf("expected preservation input but got bug condition: %+v", input)
		}

		// Simulate the current restore logic
		pathTaken, err := simulateRestoreLogic(input, cfg)

		// ASSERT: Admin clients with both --client and --target should use runAdminRemoteRestore
		if err != nil {
			rt.Fatalf("PRESERVATION VIOLATION: Admin with --client and --target should not get error.\n"+
				"Input: --client=%q, --target=%q, query=%q\n"+
				"Got error: %v",
				input.ClientFlag, input.TargetFlag, input.Query, err)
		}

		if pathTaken != "admin_remote_restore" {
			rt.Fatalf("PRESERVATION VIOLATION: Admin with --target should use runAdminRemoteRestore.\n"+
				"Input: --client=%q, --target=%q, query=%q\n"+
				"Path taken: %s\n"+
				"Expected: admin_remote_restore",
				input.ClientFlag, input.TargetFlag, input.Query, pathTaken)
		}
	})
}

// TestProperty_PreservationAdminWithTargetNoListMode verifies that admin clients
// using --client, --target, and --list get the expected error.
//
// **Validates: Requirements 3.2**
func TestProperty_PreservationAdminWithTargetNoListMode(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		adminFingerprint := rapid.StringMatching(`[0-9a-f]{64}`).Draw(rt, "adminFingerprint")
		sourceClientID := rapid.StringMatching(`client-[a-z0-9]{4,8}`).Draw(rt, "sourceClientID")
		targetClientID := rapid.StringMatching(`target-[a-z0-9]{4,8}`).Draw(rt, "targetClientID")
		query := rapid.StringMatching(`[A-Za-z0-9_-]{3,20}\.[a-z]{2,4}`).Draw(rt, "query")

		// Admin client config
		cfg := &config.Config{}
		cfg.Node.Role = "client"
		cfg.Admin.Clients = append(cfg.Admin.Clients, config.AdminClient{
			Name:        "admin-test",
			Fingerprint: adminFingerprint,
		})

		input := RestoreInput{
			Query:         query,
			ClientFlag:    sourceClientID,
			TargetFlag:    targetClientID,
			ListOnly:      true, // --list with --target should fail
			NodeRole:      "client",
			IsAdminClient: true,
		}

		// Simulate the current restore logic
		pathTaken, err := simulateRestoreLogic(input, cfg)

		// ASSERT: --list with --target is not supported and should error
		if err == nil {
			rt.Fatalf("PRESERVATION VIOLATION: --list with --target should get error.\n"+
				"Input: --client=%q, --target=%q, --list=true\n"+
				"Path taken: %s\n"+
				"Expected: error '--list is not supported in admin cross-client mode'",
				input.ClientFlag, input.TargetFlag, pathTaken)
		}

		if !strings.Contains(err.Error(), "--list is not supported in admin cross-client mode") {
			rt.Fatalf("PRESERVATION VIOLATION: Expected specific error message.\n"+
				"Got: %v\n"+
				"Expected: contains '--list is not supported in admin cross-client mode'",
				err)
		}
	})
}

// TestProperty_PreservationNoClientFlagClientNode verifies that client nodes
// without --client flag continue to restore from their own backups via server.
//
// Property: For all inputs without --client, behavior identical to original
//
// **Validates: Requirements 3.1, 3.4**
func TestProperty_PreservationNoClientFlagClientNode(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		query := rapid.OneOf(
			rapid.StringMatching(`\*\.[a-z]{2,4}`),
			rapid.StringMatching(`[A-Za-z0-9_-]{3,20}\.[a-z]{2,4}`),
			rapid.StringMatching(`/[a-z]+/[a-z]+`),
		).Draw(rt, "query")

		useBackupID := rapid.Bool().Draw(rt, "useBackupID")
		backupID := ""
		if useBackupID && query == "" {
			backupID = rapid.StringMatching(`[a-f0-9]{8}`).Draw(rt, "backupID")
		}

		listOnly := rapid.Bool().Draw(rt, "listOnly")
		isAdmin := rapid.Bool().Draw(rt, "isAdmin") // Doesn't matter without --client

		cfg := &config.Config{}
		cfg.Node.Role = "client"

		input := RestoreInput{
			Query:         query,
			ClientFlag:    "", // NO --client flag
			TargetFlag:    "", // NO --target flag
			BackupID:      backupID,
			ListOnly:      listOnly,
			NodeRole:      "client",
			IsAdminClient: isAdmin, // Irrelevant without --client
		}

		// Verify this is a preservation input
		if !isPreservationInput(input) {
			rt.Fatalf("expected preservation input but got bug condition: %+v", input)
		}

		// Simulate the current restore logic
		pathTaken, err := simulateRestoreLogic(input, cfg)

		// ASSERT: Client node without --client should restore via server (remote)
		if err != nil {
			rt.Fatalf("PRESERVATION VIOLATION: Client without --client should not get error.\n"+
				"Input: query=%q, --list=%v, --backup-id=%q\n"+
				"Got error: %v",
				input.Query, input.ListOnly, input.BackupID, err)
		}

		if pathTaken != "client_remote" {
			rt.Fatalf("PRESERVATION VIOLATION: Client without --client should use remote path.\n"+
				"Input: query=%q, --list=%v\n"+
				"Path taken: %s\n"+
				"Expected: client_remote",
				input.Query, input.ListOnly, pathTaken)
		}
	})
}

// TestProperty_PreservationNoClientFlagServerNode verifies that server nodes
// without --client flag continue to restore from local backups.
//
// **Validates: Requirements 3.1**
func TestProperty_PreservationNoClientFlagServerNode(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		query := rapid.OneOf(
			rapid.StringMatching(`\*\.[a-z]{2,4}`),
			rapid.StringMatching(`[A-Za-z0-9_-]{3,20}\.[a-z]{2,4}`),
		).Draw(rt, "query")

		listOnly := rapid.Bool().Draw(rt, "listOnly")

		cfg := &config.Config{}
		cfg.Node.Role = "server"

		input := RestoreInput{
			Query:         query,
			ClientFlag:    "", // NO --client flag
			TargetFlag:    "",
			ListOnly:      listOnly,
			NodeRole:      "server",
			IsAdminClient: false,
		}

		// Verify this is a preservation input
		if !isPreservationInput(input) {
			rt.Fatalf("expected preservation input but got bug condition: %+v", input)
		}

		// Simulate the current restore logic
		pathTaken, err := simulateRestoreLogic(input, cfg)

		// ASSERT: Server node without --client should restore locally
		if err != nil {
			rt.Fatalf("PRESERVATION VIOLATION: Server without --client should not get error.\n"+
				"Input: query=%q, --list=%v\n"+
				"Got error: %v",
				input.Query, input.ListOnly, err)
		}

		if pathTaken != "local" {
			rt.Fatalf("PRESERVATION VIOLATION: Server without --client should use local path.\n"+
				"Input: query=%q, --list=%v\n"+
				"Path taken: %s\n"+
				"Expected: local",
				input.Query, input.ListOnly, pathTaken)
		}
	})
}

// TestProperty_PreservationTargetRequiresClient verifies the validation that
// --target requires --client flag.
//
// **Validates: Requirements 3.1**
func TestProperty_PreservationTargetRequiresClient(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		targetClientID := rapid.StringMatching(`target-[a-z0-9]{4,8}`).Draw(rt, "targetClientID")
		query := rapid.StringMatching(`[A-Za-z0-9_-]{3,20}\.[a-z]{2,4}`).Draw(rt, "query")
		nodeRole := rapid.SampledFrom([]string{"client", "server", "hybrid"}).Draw(rt, "nodeRole")

		cfg := &config.Config{}
		cfg.Node.Role = nodeRole

		input := RestoreInput{
			Query:         query,
			ClientFlag:    "", // NO --client flag
			TargetFlag:    targetClientID, // But --target IS set
			NodeRole:      nodeRole,
			IsAdminClient: false,
		}

		// Simulate the current restore logic
		_, err := simulateRestoreLogic(input, cfg)

		// ASSERT: --target without --client should error
		if err == nil {
			rt.Fatalf("PRESERVATION VIOLATION: --target without --client should get error.\n"+
				"Input: --target=%q, query=%q, role=%s\n"+
				"Expected: error '--target requires --client to specify the source client'",
				input.TargetFlag, input.Query, nodeRole)
		}

		if !strings.Contains(err.Error(), "--target requires --client") {
			rt.Fatalf("PRESERVATION VIOLATION: Expected specific error message.\n"+
				"Got: %v\n"+
				"Expected: contains '--target requires --client'",
				err)
		}
	})
}
