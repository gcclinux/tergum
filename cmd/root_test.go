package cmd

import (
	"strings"
	"testing"
)

func TestVersionCommand(t *testing.T) {
	rootCmd.SetArgs([]string{"version"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("version command returned error: %v", err)
	}
}

func TestVersionCommandJSON(t *testing.T) {
	rootCmd.SetArgs([]string{"version", "--json"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("version --json command returned error: %v", err)
	}
}

func TestExecuteReturnsZeroForVersion(t *testing.T) {
	rootCmd.SetArgs([]string{"version"})
	code := Execute()
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
}

func TestRootCommandHasAllSubcommands(t *testing.T) {
	expected := []string{
		"setup", "server", "backup", "restore", "delete",
		"list", "stop", "watch", "retention", "status",
		"migrate", "version",
	}

	commands := rootCmd.Commands()
	names := make(map[string]bool)
	for _, c := range commands {
		names[c.Name()] = true
	}

	for _, name := range expected {
		if !names[name] {
			t.Errorf("missing subcommand: %s", name)
		}
	}
}

func TestGlobalFlagsExist(t *testing.T) {
	flags := rootCmd.PersistentFlags()

	if flags.Lookup("config") == nil {
		t.Error("missing --config flag")
	}
	if flags.Lookup("json") == nil {
		t.Error("missing --json flag")
	}
	if flags.Lookup("dry-run") == nil {
		t.Error("missing --dry-run flag")
	}
}

func TestExitCodeMapping(t *testing.T) {
	// Verify Execute returns 0 for a known-good command.
	rootCmd.SetArgs([]string{"version"})
	code := Execute()
	if code != 0 {
		t.Fatalf("expected exit code 0 for version, got %d", code)
	}
}

func TestBackupCommandAcceptsLevel(t *testing.T) {
	rootCmd.SetArgs([]string{"backup", "--level", "full"})
	err := rootCmd.Execute()
	// backup now requires config/setup; verify flag is accepted even if execution fails
	// due to missing config (the important thing is that --level full is a valid flag)
	if err != nil && !strings.Contains(err.Error(), "loading") && !strings.Contains(err.Error(), "passphrase") {
		t.Fatalf("backup --level full returned unexpected error: %v", err)
	}
}

func TestDeleteCommandAcceptsDryRun(t *testing.T) {
	rootCmd.SetArgs([]string{"delete", "--dry-run", "--all-backups"})
	err := rootCmd.Execute()
	// delete now requires proper config/db; verify flag is accepted
	if err != nil && !strings.Contains(err.Error(), "loading") && !strings.Contains(err.Error(), "database") {
		t.Fatalf("delete --dry-run --all-backups returned unexpected error: %v", err)
	}
}

func TestRetentionRunAcceptsDryRun(t *testing.T) {
	rootCmd.SetArgs([]string{"retention", "run", "--dry-run"})
	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("retention run --dry-run returned error: %v", err)
	}
}

func TestRestoreCommandAcceptsFile(t *testing.T) {
	rootCmd.SetArgs([]string{"restore", "--file", "test.txt", "--list"})
	err := rootCmd.Execute()
	if err != nil && !strings.Contains(err.Error(), "loading") && !strings.Contains(err.Error(), "config") {
		t.Fatalf("restore --file test.txt --list returned unexpected error: %v", err)
	}
}

func TestRestoreCommandMutuallyExclusive(t *testing.T) {
	rootCmd.SetArgs([]string{"restore", "positional.txt", "--file", "test.txt", "--list"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error when specifying both positional arg and --file flag")
	}
	if !strings.Contains(err.Error(), "cannot specify both a positional search query and the --file flag") {
		t.Fatalf("expected mutual exclusion error, got: %v", err)
	}
}
