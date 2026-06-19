package observe

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics holds all registered Prometheus metrics for Tergum.
type Metrics struct {
	FilesBackedUpTotal      prometheus.Counter
	BytesTransferredTotal   prometheus.Counter
	BackupDurationSeconds   prometheus.Histogram
	DedupRatio              prometheus.Gauge
	StorageBytesUsed        prometheus.Gauge
	RetentionDeletionsTotal prometheus.Counter
	GRPCRequestsTotal       *prometheus.CounterVec
	GRPCErrorsTotal         prometheus.Counter
	WatcherEventsTotal      prometheus.Counter
	ConnectedClients        prometheus.Gauge
}

// NewMetrics creates and registers all Prometheus metrics. It returns the Metrics
// struct and the registry they are registered in.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		FilesBackedUpTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "tergum_files_backed_up_total",
			Help: "Total number of files backed up.",
		}),
		BytesTransferredTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "tergum_bytes_transferred_total",
			Help: "Total bytes transferred during backups.",
		}),
		BackupDurationSeconds: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "tergum_backup_duration_seconds",
			Help:    "Histogram of backup durations in seconds.",
			Buckets: prometheus.DefBuckets,
		}),
		DedupRatio: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "tergum_dedup_ratio",
			Help: "Current deduplication ratio.",
		}),
		StorageBytesUsed: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "tergum_storage_bytes_used",
			Help: "Total bytes used in the content-addressable store.",
		}),
		RetentionDeletionsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "tergum_retention_deletions_total",
			Help: "Total number of retention deletions performed.",
		}),
		GRPCRequestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "tergum_grpc_requests_total",
			Help: "Total gRPC requests by method and status code.",
		}, []string{"method", "code"}),
		GRPCErrorsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "tergum_grpc_errors_total",
			Help: "Total gRPC errors.",
		}),
		WatcherEventsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "tergum_watcher_events_total",
			Help: "Total file watcher events received.",
		}),
		ConnectedClients: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "tergum_connected_clients",
			Help: "Number of currently connected clients.",
		}),
	}

	reg.MustRegister(
		m.FilesBackedUpTotal,
		m.BytesTransferredTotal,
		m.BackupDurationSeconds,
		m.DedupRatio,
		m.StorageBytesUsed,
		m.RetentionDeletionsTotal,
		m.GRPCRequestsTotal,
		m.GRPCErrorsTotal,
		m.WatcherEventsTotal,
		m.ConnectedClients,
	)

	return m
}

// HealthResponse is the JSON body returned by the /health endpoint.
type HealthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
	Uptime  string `json:"uptime"`
}

// MetricsServer serves Prometheus metrics on /metrics and health info on /health.
type MetricsServer struct {
	port      int
	version   string
	startTime time.Time
	server    *http.Server
	registry  *prometheus.Registry
	metrics   *Metrics
	mu        sync.Mutex
}

// NewMetricsServer creates a MetricsServer that will listen on the given port.
// It creates a fresh Prometheus registry and registers all Tergum metrics.
func NewMetricsServer(port int, version string) *MetricsServer {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	ms := &MetricsServer{
		port:     port,
		version:  version,
		registry: reg,
		metrics:  m,
	}
	return ms
}

// Metrics returns the registered metrics for use by other components.
func (m *MetricsServer) Metrics() *Metrics {
	return m.metrics
}

// Start begins serving the metrics and health endpoints. It blocks until the
// context is cancelled or Stop is called.
func (m *MetricsServer) Start(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{}))
	mux.HandleFunc("/health", m.handleHealth)

	m.mu.Lock()
	m.startTime = time.Now()
	m.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", m.port),
		Handler: mux,
	}
	m.mu.Unlock()

	// Use a listener so we can detect port bind errors immediately.
	ln, err := net.Listen("tcp", m.server.Addr)
	if err != nil {
		return fmt.Errorf("metrics server listen: %w", err)
	}

	// Shut down when context is done.
	go func() {
		<-ctx.Done()
		_ = m.Stop()
	}()

	if err := m.server.Serve(ln); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// Stop gracefully shuts down the metrics server.
func (m *MetricsServer) Stop() error {
	m.mu.Lock()
	srv := m.server
	m.mu.Unlock()

	if srv == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return srv.Shutdown(ctx)
}

func (m *MetricsServer) handleHealth(w http.ResponseWriter, _ *http.Request) {
	m.mu.Lock()
	uptime := time.Since(m.startTime)
	m.mu.Unlock()

	resp := HealthResponse{
		Status:  "ok",
		Version: m.version,
		Uptime:  formatDuration(uptime),
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// formatDuration produces a human-readable duration like "1h23m45s".
func formatDuration(d time.Duration) string {
	d = d.Truncate(time.Second)
	if d == 0 {
		return "0s"
	}

	var result string
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60

	if h > 0 {
		result += fmt.Sprintf("%dh", h)
	}
	if m > 0 {
		result += fmt.Sprintf("%dm", m)
	}
	if s > 0 {
		result += fmt.Sprintf("%ds", s)
	}
	if result == "" {
		result = "0s"
	}
	return result
}
