package webui

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/gcclinux/tergum/internal/config"
	registryPkg "github.com/gcclinux/tergum/internal/registry"
)

const webuiTestFP = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// fakeAdminPolicy records Reload() invocations so handler tests can assert the
// synchronous policy poke happens on mutations.
type fakeAdminPolicy struct {
	reloads int
}

func (f *fakeAdminPolicy) IsAdmin(string) bool { return false }
func (f *fakeAdminPolicy) Reload() error {
	f.reloads++
	return nil
}

// fakeRegistry satisfies ClientRegistry for admin-client resolution/status.
type fakeRegistry struct {
	clients map[string]*registryPkg.ClientInfo
}

func (r *fakeRegistry) ListClients() []registryPkg.ClientInfo {
	out := make([]registryPkg.ClientInfo, 0, len(r.clients))
	for _, c := range r.clients {
		out = append(out, *c)
	}
	return out
}
func (r *fakeRegistry) GetClient(id string) *registryPkg.ClientInfo {
	if c, ok := r.clients[id]; ok {
		cp := *c
		return &cp
	}
	return nil
}
func (r *fakeRegistry) SetSchedule(string, registryPkg.ScheduleConfig) error { return nil }
func (r *fakeRegistry) SetDisabled(string, bool) error                       { return nil }

func newAdminClientTestServer(t *testing.T) (*Server, *fakeAdminPolicy, string) {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "tergum.toml")

	cfg := &config.Config{}
	cfg.Node.Role = "server"
	cfg.Database.Path = filepath.Join(dir, "tergum.db")
	f, err := os.OpenFile(cfgPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		t.Fatalf("create config: %v", err)
	}
	if err := toml.NewEncoder(f).Encode(cfg); err != nil {
		t.Fatalf("encode config: %v", err)
	}
	f.Close()

	policy := &fakeAdminPolicy{}
	s := &Server{
		configPath:  cfgPath,
		fullCfg:     cfg,
		adminPolicy: policy,
		logger:      slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
		clientRegistry: &fakeRegistry{clients: map[string]*registryPkg.ClientInfo{
			"laptop": {ClientID: "laptop", Status: "online", SPKIFingerprint: webuiTestFP},
		}},
	}
	return s, policy, cfgPath
}

func TestAdminClientsAddWritesTOMLAndReloads(t *testing.T) {
	s, policy, cfgPath := newAdminClientTestServer(t)

	body := `{"name":"laptop","fingerprint":"` + webuiTestFP + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin-clients", strings.NewReader(body))
	rr := httptest.NewRecorder()
	s.handleAPIAdminClients(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("POST status = %d, body: %s", rr.Code, rr.Body.String())
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if len(cfg.Admin.Clients) != 1 || cfg.Admin.Clients[0].Fingerprint != webuiTestFP {
		t.Fatalf("expected admin client written, got %+v", cfg.Admin.Clients)
	}

	raw, _ := os.ReadFile(cfgPath)
	if !strings.Contains(string(raw), "[[admin.clients]]") {
		t.Fatalf("expected [[admin.clients]] in TOML, got:\n%s", raw)
	}
	if policy.reloads != 1 {
		t.Fatalf("expected 1 adminPolicy.Reload() call, got %d", policy.reloads)
	}
}

func TestAdminClientsAddResolvesFromRegistry(t *testing.T) {
	s, _, cfgPath := newAdminClientTestServer(t)

	// Name only — the handler resolves the fingerprint via the registry.
	body := `{"name":"laptop"}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin-clients", strings.NewReader(body))
	rr := httptest.NewRecorder()
	s.handleAPIAdminClients(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("POST status = %d, body: %s", rr.Code, rr.Body.String())
	}
	cfg, _ := config.Load(cfgPath)
	if len(cfg.Admin.Clients) != 1 || cfg.Admin.Clients[0].Fingerprint != webuiTestFP {
		t.Fatalf("expected resolved fingerprint, got %+v", cfg.Admin.Clients)
	}
}

func TestAdminClientsDeleteRemovesAndReloads(t *testing.T) {
	s, policy, cfgPath := newAdminClientTestServer(t)

	// Seed one entry via the add handler first.
	addBody := `{"name":"laptop","fingerprint":"` + webuiTestFP + `"}`
	addReq := httptest.NewRequest(http.MethodPost, "/api/admin-clients", strings.NewReader(addBody))
	s.handleAPIAdminClients(httptest.NewRecorder(), addReq)

	delBody := `{"name":"laptop"}`
	delReq := httptest.NewRequest(http.MethodDelete, "/api/admin-clients", strings.NewReader(delBody))
	rr := httptest.NewRecorder()
	s.handleAPIAdminClients(rr, delReq)

	if rr.Code != http.StatusOK {
		t.Fatalf("DELETE status = %d, body: %s", rr.Code, rr.Body.String())
	}
	cfg, _ := config.Load(cfgPath)
	if len(cfg.Admin.Clients) != 0 {
		t.Fatalf("expected empty admin list, got %+v", cfg.Admin.Clients)
	}
	// One reload for the add, one for the delete.
	if policy.reloads != 2 {
		t.Fatalf("expected 2 adminPolicy.Reload() calls, got %d", policy.reloads)
	}
}

func TestAdminClientsListReturnsEntries(t *testing.T) {
	s, _, _ := newAdminClientTestServer(t)

	addBody := `{"name":"laptop","fingerprint":"` + webuiTestFP + `"}`
	addReq := httptest.NewRequest(http.MethodPost, "/api/admin-clients", strings.NewReader(addBody))
	s.handleAPIAdminClients(httptest.NewRecorder(), addReq)

	req := httptest.NewRequest(http.MethodGet, "/api/admin-clients", nil)
	rr := httptest.NewRecorder()
	s.handleAPIAdminClients(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("GET status = %d", rr.Code)
	}
	var resp struct {
		Clients []adminClientView `json:"clients"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Clients) != 1 || resp.Clients[0].Fingerprint != webuiTestFP {
		t.Fatalf("unexpected list response: %+v", resp.Clients)
	}
	if resp.Clients[0].Status != "online" {
		t.Fatalf("expected online status from registry, got %q", resp.Clients[0].Status)
	}
}
