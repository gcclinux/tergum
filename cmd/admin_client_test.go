package cmd

import (
	"database/sql"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/gcclinux/tergum/internal/config"
	"github.com/gcclinux/tergum/internal/registry"
	tlspkg "github.com/gcclinux/tergum/internal/tls"

	_ "modernc.org/sqlite"
)

const (
	testFP  = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	testFP2 = "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"
)

// setupAdminClientTest writes a minimal server-role config and an empty registry
// DB at the config's database path, returning the config file path. It resets
// the relevant global flags around the test.
func setupAdminClientTest(t *testing.T, role string) string {
	t.Helper()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "tergum.db")
	cfgPath := filepath.Join(dir, "tergum.toml")

	cfg := &config.Config{}
	cfg.Node.Role = role
	cfg.Database.Path = dbPath
	cfg.Database.WALMode = true
	if role == "client" {
		cfg.Server.Address = "127.0.0.1"
	}
	if err := writeConfigTOML(cfgPath, cfg); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	// Create the registry DB so openRegistry can open it.
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	reg, err := registry.New(registry.Config{DB: db})
	if err != nil {
		t.Fatalf("new registry: %v", err)
	}
	_ = reg
	db.Close()

	// Point the CLI at this config, reset flags, and restore after the test.
	prevCfg, prevJSON, prevDry := cfgFile, jsonOut, dryRun
	cfgFile, jsonOut, dryRun = cfgPath, false, false
	t.Cleanup(func() {
		cfgFile, jsonOut, dryRun = prevCfg, prevJSON, prevDry
	})

	return cfgPath
}

func seedClientWithFingerprint(t *testing.T, cfgPath, clientID, fp string) {
	t.Helper()
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	db, err := sql.Open("sqlite", cfg.Database.Path)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	reg, err := registry.New(registry.Config{DB: db})
	if err != nil {
		t.Fatalf("new registry: %v", err)
	}
	if _, err := reg.Register(clientID, "10.0.0.5:7400"); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := reg.SetSPKIFingerprint(clientID, fp); err != nil {
		t.Fatalf("set fingerprint: %v", err)
	}
}

func TestAdminClientAddWithFingerprint(t *testing.T) {
	cfgPath := setupAdminClientTest(t, "server")

	if err := runAdminClientAdd("laptop", testFP, true); err != nil {
		t.Fatalf("add: %v", err)
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if len(cfg.Admin.Clients) != 1 {
		t.Fatalf("expected 1 admin client, got %d", len(cfg.Admin.Clients))
	}
	if cfg.Admin.Clients[0].Name != "laptop" || cfg.Admin.Clients[0].Fingerprint != testFP {
		t.Fatalf("unexpected admin client written: %+v", cfg.Admin.Clients[0])
	}

	// Verify the raw TOML carries an [[admin.clients]] block.
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(raw), "[[admin.clients]]") {
		t.Fatalf("expected [[admin.clients]] in TOML, got:\n%s", raw)
	}
}

func TestAdminClientAddNormalizesFingerprint(t *testing.T) {
	cfgPath := setupAdminClientTest(t, "server")

	upper := "  " + strings.ToUpper(testFP) + "  "
	if err := runAdminClientAdd("laptop", upper, true); err != nil {
		t.Fatalf("add: %v", err)
	}
	cfg, _ := config.Load(cfgPath)
	if cfg.Admin.Clients[0].Fingerprint != testFP {
		t.Fatalf("fingerprint not normalized: %q", cfg.Admin.Clients[0].Fingerprint)
	}
}

func TestAdminClientAddBadFingerprint(t *testing.T) {
	setupAdminClientTest(t, "server")
	if err := runAdminClientAdd("laptop", "nothex", true); err == nil {
		t.Fatalf("expected error for invalid fingerprint")
	}
}

func TestAdminClientAddResolvesFromRegistry(t *testing.T) {
	cfgPath := setupAdminClientTest(t, "server")
	seedClientWithFingerprint(t, cfgPath, "laptop", testFP)

	// --yes avoids the interactive prompt.
	if err := runAdminClientAdd("laptop", "", true); err != nil {
		t.Fatalf("add: %v", err)
	}
	cfg, _ := config.Load(cfgPath)
	if len(cfg.Admin.Clients) != 1 || cfg.Admin.Clients[0].Fingerprint != testFP {
		t.Fatalf("expected resolved fingerprint, got %+v", cfg.Admin.Clients)
	}
}

func TestAdminClientAddUnknownClientErrors(t *testing.T) {
	setupAdminClientTest(t, "server")
	if err := runAdminClientAdd("ghost", "", true); err == nil {
		t.Fatalf("expected error for client with no recorded fingerprint")
	}
}

func TestAdminClientAddDryRunWritesNothing(t *testing.T) {
	cfgPath := setupAdminClientTest(t, "server")
	dryRun = true

	if err := runAdminClientAdd("laptop", testFP, true); err != nil {
		t.Fatalf("add: %v", err)
	}
	cfg, _ := config.Load(cfgPath)
	if len(cfg.Admin.Clients) != 0 {
		t.Fatalf("dry-run should not write; got %d clients", len(cfg.Admin.Clients))
	}
}

func TestAdminClientRemoveByName(t *testing.T) {
	cfgPath := setupAdminClientTest(t, "server")
	if err := runAdminClientAdd("laptop", testFP, true); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := runAdminClientRemove("laptop", ""); err != nil {
		t.Fatalf("remove: %v", err)
	}
	cfg, _ := config.Load(cfgPath)
	if len(cfg.Admin.Clients) != 0 {
		t.Fatalf("expected empty admin list, got %d", len(cfg.Admin.Clients))
	}
}

func TestAdminClientRemoveByFingerprint(t *testing.T) {
	cfgPath := setupAdminClientTest(t, "server")
	if err := runAdminClientAdd("laptop", testFP, true); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := runAdminClientRemove("ignored-name", testFP); err != nil {
		t.Fatalf("remove: %v", err)
	}
	cfg, _ := config.Load(cfgPath)
	if len(cfg.Admin.Clients) != 0 {
		t.Fatalf("expected empty admin list, got %d", len(cfg.Admin.Clients))
	}
}

func TestAdminClientRemoveUnchanged(t *testing.T) {
	setupAdminClientTest(t, "server")
	// Removing a nonexistent entry should not error (reports unchanged).
	if err := runAdminClientRemove("nope", ""); err != nil {
		t.Fatalf("remove unchanged: %v", err)
	}
}

func TestAdminClientList(t *testing.T) {
	setupAdminClientTest(t, "server")
	if err := runAdminClientAdd("laptop", testFP, true); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := runAdminClientList(); err != nil {
		t.Fatalf("list: %v", err)
	}
}

func TestClientFingerprintPrints64Hex(t *testing.T) {
	dir := t.TempDir()
	certsDir := filepath.Join(dir, "certs")
	if err := tlspkg.NewManager().GenerateCerts(certsDir); err != nil {
		t.Fatalf("generate certs: %v", err)
	}

	cfgPath := filepath.Join(dir, "tergum.toml")
	cfg := &config.Config{}
	cfg.Node.Role = "client"
	cfg.Server.Address = "127.0.0.1"
	cfg.Database.Path = filepath.Join(dir, "tergum.db")
	cfg.TLS.CACert = filepath.Join(certsDir, "ca.crt")
	cfg.TLS.Cert = filepath.Join(certsDir, "client.crt")
	cfg.TLS.Key = filepath.Join(certsDir, "client.key")
	if err := writeConfigTOML(cfgPath, cfg); err != nil {
		t.Fatalf("write config: %v", err)
	}

	prevCfg := cfgFile
	cfgFile = cfgPath
	t.Cleanup(func() { cfgFile = prevCfg })

	// Capture stdout to assert the printed fingerprint shape.
	oldStdout := os.Stdout
	rd, wr, _ := os.Pipe()
	os.Stdout = wr

	err := runClientFingerprint()

	wr.Close()
	os.Stdout = oldStdout
	buf := make([]byte, 4096)
	n, _ := rd.Read(buf)
	out := strings.TrimSpace(string(buf[:n]))

	if err != nil {
		t.Fatalf("fingerprint: %v", err)
	}
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(out) {
		t.Fatalf("expected 64-hex lowercase fingerprint, got %q", out)
	}
}

func TestAdminClientRoleGuardRejectsClient(t *testing.T) {
	setupAdminClientTest(t, "client")
	err := runAdminClientList()
	if err == nil {
		t.Fatalf("expected role-guard error on a client node")
	}
	if !strings.Contains(err.Error(), "admin-client") {
		t.Fatalf("error should name 'admin-client', got: %v", err)
	}
}
