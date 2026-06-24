package cmd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	// Stub getLocalIPs to return a predictable address
	getLocalIPs = func() ([]string, error) {
		return []string{"192.168.1.150"}, nil
	}
	os.Setenv("TERGUM_PASSPHRASE", "testpassphrase")

	// Isolate tests from the user's real home/config directory
	tmpDir, err := os.MkdirTemp("", "tergum-test-*")
	if err == nil {
		os.Setenv("APPDATA", tmpDir)
		os.Setenv("USERPROFILE", tmpDir)
		os.Setenv("HOME", tmpDir)
	}

	code := m.Run()

	if tmpDir != "" {
		os.RemoveAll(tmpDir)
	}

	os.Exit(code)
}

func TestSetupGenerateCertsFlag(t *testing.T) {
	// Save existing certs if present so we don't destroy user data.
	defaultCertsDir := filepath.Join(defaultConfigDirForTest(), "certs")
	hadCerts := false
	if _, err := os.Stat(defaultCertsDir); err == nil {
		hadCerts = true
	}

	rootCmd.SetArgs([]string{"setup", "--generate-certs"})
	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("setup --generate-certs returned error: %v", err)
	}

	// Verify files were generated in the default location.
	expectedFiles := []string{"ca.crt", "ca.key", "server.crt", "server.key", "client.crt", "client.key"}
	for _, fname := range expectedFiles {
		path := filepath.Join(defaultCertsDir, fname)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("expected certificate file %s to exist", path)
		}
	}

	// Only clean up if certs didn't exist before the test.
	if !hadCerts {
		os.RemoveAll(defaultCertsDir)
	}
}

func TestSetupGenerateCertsJSON(t *testing.T) {
	// Save existing certs state.
	defaultCertsDir := filepath.Join(defaultConfigDirForTest(), "certs")
	hadCerts := false
	if _, err := os.Stat(defaultCertsDir); err == nil {
		hadCerts = true
	}

	rootCmd.SetArgs([]string{"setup", "--generate-certs", "--json"})
	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("setup --generate-certs --json returned error: %v", err)
	}

	// Only clean up if certs didn't exist before the test.
	if !hadCerts {
		os.RemoveAll(defaultCertsDir)
	}
}

func TestSetupWizardPrompt(t *testing.T) {
	var output bytes.Buffer
	input := strings.NewReader("test answer\n")

	wiz := newSetupWizard(input, &output)
	result := wiz.prompt("Enter value", "default")

	if result != "test answer" {
		t.Errorf("expected 'test answer', got %q", result)
	}
	if !strings.Contains(output.String(), "Enter value [default]:") {
		t.Errorf("expected prompt to contain 'Enter value [default]:', got %q", output.String())
	}
}

func TestSetupWizardPromptDefault(t *testing.T) {
	var output bytes.Buffer
	input := strings.NewReader("\n")

	wiz := newSetupWizard(input, &output)
	result := wiz.prompt("Enter value", "mydefault")

	if result != "mydefault" {
		t.Errorf("expected 'mydefault', got %q", result)
	}
}

func TestSetupWizardPromptChoice(t *testing.T) {
	var output bytes.Buffer
	input := strings.NewReader("server\n")

	wiz := newSetupWizard(input, &output)
	result := wiz.promptChoice("Choose role", []string{"client", "server", "hybrid"}, "client")

	if result != "server" {
		t.Errorf("expected 'server', got %q", result)
	}
}

func TestSetupWizardPromptChoiceInvalid(t *testing.T) {
	var output bytes.Buffer
	input := strings.NewReader("invalid\n")

	wiz := newSetupWizard(input, &output)
	result := wiz.promptChoice("Choose role", []string{"client", "server", "hybrid"}, "client")

	if result != "client" {
		t.Errorf("expected 'client' (default), got %q", result)
	}
}

func TestSetupWizardPromptYesNo(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		defaultYes bool
		expected   bool
	}{
		{"yes input", "y\n", false, true},
		{"no input", "n\n", true, false},
		{"empty default yes", "\n", true, true},
		{"empty default no", "\n", false, false},
		{"YES input", "YES\n", false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			input := strings.NewReader(tt.input)

			wiz := newSetupWizard(input, &output)
			result := wiz.promptYesNo("Proceed?", tt.defaultYes)

			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestBuildConfig(t *testing.T) {
	cfg := buildConfig("client", "192.168.1.5", "192.168.1.10", "/tmp/storage", "/tmp/certs", "/tmp/config", true)

	if cfg.Node.Role != "client" {
		t.Errorf("expected role 'client', got %q", cfg.Node.Role)
	}
	if cfg.Server.Address != "192.168.1.5" {
		t.Errorf("expected address '192.168.1.5', got %q", cfg.Server.Address)
	}
	if cfg.Node.Hostname != "192.168.1.10" {
		t.Errorf("expected hostname '192.168.1.10', got %q", cfg.Node.Hostname)
	}
	if cfg.Server.CommandPort != 7400 {
		t.Errorf("expected command port 7400, got %d", cfg.Server.CommandPort)
	}
	if cfg.Server.DataPort != 7401 {
		t.Errorf("expected data port 7401, got %d", cfg.Server.DataPort)
	}
	if !cfg.Encryption.Enabled {
		t.Error("expected encryption enabled")
	}
	if cfg.TLS.CACert != filepath.Join("/tmp/certs", "ca.crt") {
		t.Errorf("unexpected CA cert path: %q", cfg.TLS.CACert)
	}
	// Client role should use client cert
	if cfg.TLS.Cert != filepath.Join("/tmp/certs", "client.crt") {
		t.Errorf("expected client cert, got %q", cfg.TLS.Cert)
	}
}

func TestBuildConfigServerRole(t *testing.T) {
	cfg := buildConfig("server", "", "", "/var/lib/tergum/storage", "/etc/tergum/certs", "/etc/tergum", true)

	if cfg.Node.Role != "server" {
		t.Errorf("expected role 'server', got %q", cfg.Node.Role)
	}
	if cfg.Server.Address != "" {
		t.Errorf("expected empty address for server, got %q", cfg.Server.Address)
	}
	// Server role should use server cert
	if cfg.TLS.Cert != filepath.Join("/etc/tergum/certs", "server.crt") {
		t.Errorf("expected server cert, got %q", cfg.TLS.Cert)
	}
	if !cfg.WebUI.Enabled {
		t.Error("expected WebUI enabled for server role")
	}
}

func TestBuildConfigNoCerts(t *testing.T) {
	cfg := buildConfig("client", "localhost", "", "/tmp/storage", "/tmp/certs", "/tmp/config", false)

	if cfg.TLS.CACert != "" {
		t.Errorf("expected empty TLS paths when certs not generated, got CA: %q", cfg.TLS.CACert)
	}
	if cfg.TLS.Cert != "" {
		t.Errorf("expected empty TLS cert path, got: %q", cfg.TLS.Cert)
	}
}

func TestWriteConfigTOML(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "subdir", "tergum.toml")

	cfg := buildConfig("client", "localhost", "", "/tmp/storage", "/tmp/certs", tmpDir, true)
	err := writeConfigTOML(configPath, cfg)
	if err != nil {
		t.Fatalf("writeConfigTOML failed: %v", err)
	}

	// Verify file was written
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("cannot read config file: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, `role = "client"`) {
		t.Error("config file missing role = client")
	}
	if !strings.Contains(content, `address = "localhost"`) {
		t.Error("config file missing server address")
	}

	// Verify file permissions (non-Windows only)
	if os.PathSeparator != '\\' {
		info, _ := os.Stat(configPath)
		if info.Mode().Perm() != 0600 {
			t.Errorf("expected file mode 0600, got %o", info.Mode().Perm())
		}
	}
}

func TestInteractiveSetupRequiresPassphrase(t *testing.T) {
	tmpDir := t.TempDir()
	if os.PathSeparator == '\\' {
		t.Setenv("APPDATA", tmpDir)
		t.Setenv("USERPROFILE", tmpDir)
	} else {
		t.Setenv("HOME", tmpDir)
	}

	// Provide inputs for all prompts but leave passphrase empty
	// role=client, clientIPchoice=1, serverAddress=localhost, generateCerts=n, passphrase=
	input := strings.NewReader("client\n1\nlocalhost\nn\n\n")
	var output bytes.Buffer

	wiz := newSetupWizard(input, &output)
	err := runInteractiveSetup(wiz)

	if err == nil {
		t.Fatal("expected error for empty passphrase")
	}
	if !strings.Contains(err.Error(), "passphrase is required") {
		t.Errorf("expected passphrase error, got: %v", err)
	}
}

func TestInteractiveSetupShortPassphrase(t *testing.T) {
	tmpDir := t.TempDir()
	if os.PathSeparator == '\\' {
		t.Setenv("APPDATA", tmpDir)
		t.Setenv("USERPROFILE", tmpDir)
	} else {
		t.Setenv("HOME", tmpDir)
	}

	// Provide all prompts with a too-short passphrase
	// role=client, clientIPchoice=1, serverAddress=localhost, generateCerts=n, passphrase=short
	input := strings.NewReader("client\n1\nlocalhost\nn\nshort\n")
	var output bytes.Buffer

	wiz := newSetupWizard(input, &output)
	err := runInteractiveSetup(wiz)

	if err == nil {
		t.Fatal("expected error for short passphrase")
	}
	if !strings.Contains(err.Error(), "at least 8 characters") {
		t.Errorf("expected length error, got: %v", err)
	}
}

func TestInteractiveSetupFullFlow(t *testing.T) {
	tmpDir := t.TempDir()
	if os.PathSeparator == '\\' {
		t.Setenv("APPDATA", tmpDir)
		t.Setenv("USERPROFILE", tmpDir)
	} else {
		t.Setenv("HOME", tmpDir)
	}

	// Provide all required inputs for client role (no storage path prompted):
	// role=client, clientIPchoice=1, server_address=localhost, generate_certs=n, passphrase=mysecretpass123
	// include paths: (empty = scan home), scan home=y
	// exclude patterns: use defaults=y, additional=(empty)
	// watcher: n
	// schedule: (empty), (empty)
	inputs := "client\n1\nlocalhost\nn\nmysecretpass123\n\ny\ny\n\nn\n\n\n"
	input := strings.NewReader(inputs)
	var output bytes.Buffer

	wiz := newSetupWizard(input, &output)
	err := runInteractiveSetup(wiz)

	if err != nil {
		t.Fatalf("interactive setup failed: %v", err)
	}

	// Verify output contains completion message
	if !strings.Contains(output.String(), "Setup complete") {
		t.Error("expected completion message in output")
	}
}

func TestInteractiveSetupHybridFlow(t *testing.T) {
	tmpDir := t.TempDir()
	if os.PathSeparator == '\\' {
		t.Setenv("APPDATA", tmpDir)
		t.Setenv("USERPROFILE", tmpDir)
	} else {
		t.Setenv("HOME", tmpDir)
	}

	storagePath := filepath.Join(tmpDir, "storage")
	// Provide all required inputs for hybrid role (storage path is prompted):
	// role=hybrid, server_address=localhost, storage_path=storagePath, generate_certs=n, passphrase=mysecretpass123
	// include paths: (empty = scan home), scan home=y
	// exclude patterns: use defaults=y, additional=(empty)
	// watcher: n
	// schedule: (empty), (empty)
	inputs := fmt.Sprintf("hybrid\nlocalhost\n%s\nn\nmysecretpass123\n\ny\ny\n\nn\n\n\n", storagePath)
	input := strings.NewReader(inputs)
	var output bytes.Buffer

	wiz := newSetupWizard(input, &output)
	err := runInteractiveSetup(wiz)

	if err != nil {
		t.Fatalf("interactive setup failed: %v", err)
	}

	// Verify storage directory was created
	if _, err := os.Stat(storagePath); os.IsNotExist(err) {
		t.Error("storage directory was not created")
	}

	// Verify output contains completion message
	if !strings.Contains(output.String(), "Setup complete") {
		t.Error("expected completion message in output")
	}
}

func TestDefaultStoragePath(t *testing.T) {
	serverPath := defaultStoragePath("server")
	if serverPath == "" {
		t.Error("expected non-empty server storage path")
	}

	clientPath := defaultStoragePath("client")
	if clientPath == "" {
		t.Error("expected non-empty client storage path")
	}

	// Client and server should have different default paths
	if serverPath == clientPath {
		t.Error("expected different default paths for server and client")
	}
}

// defaultConfigDirForTest returns the config dir used by the test environment.
func defaultConfigDirForTest() string {
	// Reuse the same logic as config.DefaultConfigDir()
	if os.PathSeparator == '\\' {
		appData := os.Getenv("APPDATA")
		if appData == "" {
			home, _ := os.UserHomeDir()
			appData = filepath.Join(home, "AppData", "Roaming")
		}
		return filepath.Join(appData, "tergum")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "tergum")
}

func TestInteractiveSetupExistingConfig(t *testing.T) {
	tmpDir := t.TempDir()
	if os.PathSeparator == '\\' {
		t.Setenv("APPDATA", tmpDir)
		t.Setenv("USERPROFILE", tmpDir)
	} else {
		t.Setenv("HOME", tmpDir)
	}

	// Phase 1: Initial Setup
	inputs1 := "client\n1\nlocalhost\nn\nmysecretpass123\n\ny\ny\n\nn\n\n\n"
	var output1 bytes.Buffer
	wiz1 := newSetupWizard(strings.NewReader(inputs1), &output1)
	if err := runInteractiveSetup(wiz1); err != nil {
		t.Fatalf("first setup failed: %v\noutput: %s", err, output1.String())
	}

	// Verify salt file exists
	configDir := defaultConfigDirForTest()
	saltPath := filepath.Join(configDir, "salt")
	if _, err := os.Stat(saltPath); err != nil {
		t.Fatalf("expected salt file to exist: %v", err)
	}
	originalSalt, _ := os.ReadFile(saltPath)

	// Phase 2: Rerun setup, choose NOT to overwrite, verify correct passphrase
	// inputs: role, clientIPchoice, server, certs, overwrite=n, passphrase=mysecretpass123
	inputs2 := "client\n1\nlocalhost\nn\nn\nmysecretpass123\n\ny\ny\n\nn\n\n\n"
	var output2 bytes.Buffer
	wiz2 := newSetupWizard(strings.NewReader(inputs2), &output2)
	if err := runInteractiveSetup(wiz2); err != nil {
		t.Fatalf("setup with correct passphrase failed: %v\noutput: %s", err, output2.String())
	}

	// Verify salt was NOT overwritten
	newSalt, _ := os.ReadFile(saltPath)
	if !bytes.Equal(originalSalt, newSalt) {
		t.Error("salt was overwritten despite choosing not to")
	}

	// Phase 3: Rerun setup, choose NOT to overwrite, enter WRONG passphrase, do NOT retry
	// inputs: role, clientIPchoice, server, certs, overwrite=n, passphrase=wrongpass, retry=n
	inputs3 := "client\n1\nlocalhost\nn\nn\nwrongpass\nn\n"
	var output3 bytes.Buffer
	wiz3 := newSetupWizard(strings.NewReader(inputs3), &output3)
	err := runInteractiveSetup(wiz3)
	if err == nil {
		t.Fatal("expected setup to fail with wrong passphrase")
	}
	if !strings.Contains(err.Error(), "incorrect passphrase") {
		t.Errorf("expected incorrect passphrase error, got: %v\noutput: %s", err, output3.String())
	}
}

