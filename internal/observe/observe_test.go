package observe

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

func TestSetupLogging_ValidLevels(t *testing.T) {
	levels := []string{"debug", "info", "warn", "error"}
	for _, lvl := range levels {
		if err := SetupLogging(lvl, "text"); err != nil {
			t.Errorf("SetupLogging(%q, \"text\") returned error: %v", lvl, err)
		}
	}
}

func TestSetupLogging_ValidFormats(t *testing.T) {
	formats := []string{"text", "json"}
	for _, fmt := range formats {
		if err := SetupLogging("info", fmt); err != nil {
			t.Errorf("SetupLogging(\"info\", %q) returned error: %v", fmt, err)
		}
	}
}

func TestSetupLogging_InvalidLevel(t *testing.T) {
	err := SetupLogging("trace", "text")
	if err == nil {
		t.Error("expected error for invalid level, got nil")
	}
}

func TestSetupLogging_InvalidFormat(t *testing.T) {
	err := SetupLogging("info", "xml")
	if err == nil {
		t.Error("expected error for invalid format, got nil")
	}
}

func TestLogger_HasComponent(t *testing.T) {
	_ = SetupLogging("info", "text")
	logger := Logger("backup")
	if logger == nil {
		t.Fatal("Logger returned nil")
	}
	// Verify the logger is usable (no panic).
	logger.Info("test message", "key", "value")
}

func TestNewMetrics_Registration(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	if m.FilesBackedUpTotal == nil {
		t.Error("FilesBackedUpTotal not registered")
	}
	if m.BytesTransferredTotal == nil {
		t.Error("BytesTransferredTotal not registered")
	}
	if m.BackupDurationSeconds == nil {
		t.Error("BackupDurationSeconds not registered")
	}
	if m.DedupRatio == nil {
		t.Error("DedupRatio not registered")
	}
	if m.StorageBytesUsed == nil {
		t.Error("StorageBytesUsed not registered")
	}
	if m.RetentionDeletionsTotal == nil {
		t.Error("RetentionDeletionsTotal not registered")
	}
	if m.GRPCRequestsTotal == nil {
		t.Error("GRPCRequestsTotal not registered")
	}
	if m.GRPCErrorsTotal == nil {
		t.Error("GRPCErrorsTotal not registered")
	}
	if m.WatcherEventsTotal == nil {
		t.Error("WatcherEventsTotal not registered")
	}
	if m.ConnectedClients == nil {
		t.Error("ConnectedClients not registered")
	}

	// Verify we can gather metrics (they are properly registered).
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("failed to gather metrics: %v", err)
	}
	// At minimum the counters start at 0 which won't produce metric families
	// unless incremented. Increment one to test gathering.
	m.FilesBackedUpTotal.Inc()
	mfs, err = reg.Gather()
	if err != nil {
		t.Fatalf("failed to gather metrics after increment: %v", err)
	}
	found := false
	for _, mf := range mfs {
		if mf.GetName() == "tergum_files_backed_up_total" {
			found = true
			break
		}
	}
	if !found {
		t.Error("tergum_files_backed_up_total not found in gathered metrics")
	}
}

func TestMetricsServer_HealthEndpoint(t *testing.T) {
	srv := NewMetricsServer(0, "3.0.0") // port 0 = auto-assign

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start on a random port.
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Start(ctx)
	}()

	// Give the server a moment to start.
	time.Sleep(100 * time.Millisecond)

	// We used port 0, but MetricsServer uses the provided port.
	// Let's use a fixed port for this test.
	cancel()
	<-errCh // drain

	// Restart with a specific port.
	srv = NewMetricsServer(17490, "3.0.0")
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()

	go func() {
		_ = srv.Start(ctx2)
	}()
	time.Sleep(100 * time.Millisecond)

	resp, err := http.Get("http://localhost:17490/health")
	if err != nil {
		t.Fatalf("GET /health failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var health HealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		t.Fatalf("failed to decode health response: %v", err)
	}

	if health.Status != "ok" {
		t.Errorf("expected status \"ok\", got %q", health.Status)
	}
	if health.Version != "3.0.0" {
		t.Errorf("expected version \"3.0.0\", got %q", health.Version)
	}
	if health.Uptime == "" {
		t.Error("uptime should not be empty")
	}

	cancel2()
	time.Sleep(50 * time.Millisecond)
}

func TestMetricsServer_MetricsEndpoint(t *testing.T) {
	srv := NewMetricsServer(17491, "3.0.0")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = srv.Start(ctx)
	}()
	time.Sleep(100 * time.Millisecond)

	// Increment a metric so it shows up.
	srv.Metrics().FilesBackedUpTotal.Add(5)

	resp, err := http.Get("http://localhost:17491/metrics")
	if err != nil {
		t.Fatalf("GET /metrics failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// Read body and check for our metric.
	buf := make([]byte, 8192)
	n, _ := resp.Body.Read(buf)
	body := string(buf[:n])

	if !contains(body, "tergum_files_backed_up_total 5") {
		t.Errorf("expected tergum_files_backed_up_total 5 in body, got:\n%s", body)
	}

	cancel()
	time.Sleep(50 * time.Millisecond)
}

func TestMetricsServer_Stop(t *testing.T) {
	srv := NewMetricsServer(17492, "3.0.0")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Start(ctx)
	}()
	time.Sleep(100 * time.Millisecond)

	if err := srv.Stop(); err != nil {
		t.Fatalf("Stop() returned error: %v", err)
	}

	// Server should no longer accept connections.
	time.Sleep(50 * time.Millisecond)
	_, err := http.Get("http://localhost:17492/health")
	if err == nil {
		t.Error("expected error after server stop, but request succeeded")
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{0, "0s"},
		{5 * time.Second, "5s"},
		{65 * time.Second, "1m5s"},
		{time.Hour + 23*time.Minute + 45*time.Second, "1h23m45s"},
		{2 * time.Hour, "2h"},
		{30 * time.Minute, "30m"},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%v", tt.d), func(t *testing.T) {
			got := formatDuration(tt.d)
			if got != tt.want {
				t.Errorf("formatDuration(%v) = %q, want %q", tt.d, got, tt.want)
			}
		})
	}
}

func TestGRPCRequestsTotal_Labels(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	// Increment with labels.
	m.GRPCRequestsTotal.WithLabelValues("TriggerBackup", "OK").Inc()
	m.GRPCRequestsTotal.WithLabelValues("GetStatus", "NotFound").Inc()

	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("failed to gather: %v", err)
	}

	found := false
	for _, mf := range mfs {
		if mf.GetName() == "tergum_grpc_requests_total" {
			found = true
			if len(mf.GetMetric()) != 2 {
				t.Errorf("expected 2 label combinations, got %d", len(mf.GetMetric()))
			}
		}
	}
	if !found {
		t.Error("tergum_grpc_requests_total not found")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsCheck(s, substr))
}

func containsCheck(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestLogWriterAndHistory(t *testing.T) {
	err := SetupLogging("info", "text")
	if err != nil {
		t.Fatalf("failed to setup logging: %v", err)
	}

	initialHistory := GetLogHistory()

	var listenerCalled bool
	var listenerMsg string
	RegisterLogListener(func(line string) {
		listenerCalled = true
		listenerMsg = line
	})

	testMsg := "hello world test log line"
	slog.Info(testMsg)

	history := GetLogHistory()
	if len(history) <= len(initialHistory) {
		t.Errorf("expected history length to increase, got %d", len(history))
	}

	found := false
	for _, line := range history {
		if strings.Contains(line, testMsg) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected to find message %q in history, history: %v", testMsg, history)
	}

	if !listenerCalled {
		t.Error("expected log listener to be called")
	}
	if !strings.Contains(listenerMsg, testMsg) {
		t.Errorf("expected listener message to contain %q, got %q", testMsg, listenerMsg)
	}
}
