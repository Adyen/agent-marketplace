package receiver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Adyen/agent-marketplace/internal/telemetry"
	tmcfg "github.com/Adyen/agent-marketplace/internal/telemetry/config"

	metricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	mpb "go.opentelemetry.io/proto/otlp/metrics/v1"
	"google.golang.org/protobuf/proto"
)

// DefaultListenAddr is the local address the community telemetry receiver
// binds to when no other address is configured.
const DefaultListenAddr = "localhost:4318"

// Config is the receiver runtime configuration. All fields have sensible
// defaults except StorageDir, which is required.
type Config struct {
	ListenAddr       string
	StorageDir       string
	MaxInstallations int
	Logger           *slog.Logger
}

// Server is a minimal OTLP/HTTP metrics receiver for community telemetry.
type Server struct {
	cfg     Config
	storage *storage
	budget  *cardinality
	mux     *http.ServeMux

	mu            sync.RWMutex
	lastReceive   time.Time
	commandCounts map[string]int64
	harnessCounts map[string]int64
	totalMetrics  int64

	ln net.Listener
}

type parsedRequest struct {
	installationID string
	schemaVersion  string
	commands       map[string]int64
	harnesses      map[string]int64
	metricCount    int64
}

// DefaultStorageDir returns the default receiver storage directory based on
// the XDG Base Directory specification. It prefers $XDG_STATE_HOME and falls
// back to ~/.local/state.
func DefaultStorageDir() (string, error) {
	if xdg := os.Getenv("XDG_STATE_HOME"); filepath.IsAbs(xdg) {
		return filepath.Join(xdg, "agent-marketplace", "telemetry", "receiver"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".local", "state", "agent-marketplace", "telemetry", "receiver"), nil
}

// New creates a Server and prepares its durable state. The server does not
// begin serving until Start is called.
func New(cfg Config) (*Server, error) {
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = DefaultListenAddr
	}
	if cfg.StorageDir == "" {
		return nil, errors.New("storage directory is required")
	}
	if cfg.MaxInstallations <= 0 {
		cfg.MaxInstallations = telemetry.DefaultInstallationIDCardinalityBudget
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	s := &Server{
		cfg:           cfg,
		storage:       newStorage(cfg.StorageDir),
		budget:        newCardinality(cfg.StorageDir, cfg.MaxInstallations),
		commandCounts: make(map[string]int64),
		harnessCounts: make(map[string]int64),
	}

	s.mux = http.NewServeMux()
	s.mux.HandleFunc("/v1/metrics", s.handleMetrics)
	s.mux.HandleFunc("/health", s.handleHealth)
	s.mux.HandleFunc("/v1/dashboard.json", s.handleDashboard)

	if err := s.storage.ensureDir(); err != nil {
		return nil, fmt.Errorf("ensure storage directory: %w", err)
	}
	if err := s.budget.Load(); err != nil {
		return nil, fmt.Errorf("load cardinality budget: %w", err)
	}

	return s, nil
}

// Handler returns the server's HTTP handler. It can be used with httptest
// without calling Start.
func (s *Server) Handler() http.Handler {
	return s.mux
}

// Start binds the configured address and serves until ctx is cancelled. It
// performs a graceful shutdown when the context is done.
func (s *Server) Start(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.cfg.ListenAddr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	s.mu.Lock()
	s.ln = ln
	s.mu.Unlock()

	srv := &http.Server{Handler: s.mux}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			s.cfg.Logger.Warn("telemetry receiver shutdown error", "error", err)
		}
	}()

	s.cfg.Logger.Info("telemetry receiver listening", "addr", s.Addr())
	err = srv.Serve(ln)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// Addr returns the listener address. It is empty until Start is called.
func (s *Server) Addr() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.ln == nil {
		return ""
	}
	return s.ln.Addr().String()
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if ct := r.Header.Get("Content-Type"); ct != "" && ct != "application/x-protobuf" {
		http.Error(w, "unsupported content type, expected application/x-protobuf", http.StatusUnsupportedMediaType)
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	var req metricspb.ExportMetricsServiceRequest
	if err := proto.Unmarshal(body, &req); err != nil {
		http.Error(w, "invalid protobuf payload", http.StatusBadRequest)
		return
	}

	if len(req.ResourceMetrics) != 1 {
		http.Error(w, "expected exactly one resource metrics", http.StatusBadRequest)
		return
	}

	parsed, err := s.parseResourceMetrics(req.ResourceMetrics[0])
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	accepted, err := s.budget.Add(parsed.installationID)
	if err != nil {
		s.cfg.Logger.Error("failed to check cardinality budget", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if !accepted {
		s.cfg.Logger.Warn("cardinality budget exceeded", "installation_id", parsed.installationID)
		http.Error(w, "cardinality budget exceeded", http.StatusTooManyRequests)
		return
	}

	record := &requestRecord{
		ReceivedAt:     time.Now().UTC(),
		InstallationID: parsed.installationID,
		SchemaVersion:  parsed.schemaVersion,
		Commands:       parsed.commands,
		Harnesses:      parsed.harnesses,
		MetricCount:    parsed.metricCount,
	}
	if err := s.storage.append(record); err != nil {
		s.cfg.Logger.Error("failed to persist metrics", "error", err)
		http.Error(w, "failed to persist", http.StatusInternalServerError)
		return
	}

	s.updateCounts(parsed)
	s.cfg.Logger.Debug("accepted metrics",
		"installation_id", parsed.installationID,
		"metric_count", parsed.metricCount,
	)
	w.WriteHeader(http.StatusOK)
}

func (s *Server) parseResourceMetrics(rm *mpb.ResourceMetrics) (*parsedRequest, error) {
	if rm == nil {
		return nil, errors.New("resource metrics is nil")
	}

	attrs := attributeMap(rm.Resource.GetAttributes())
	schemaVersion := attrs[string(tmcfg.ResourceAttrSchemaVersion)]
	if schemaVersion != tmcfg.SchemaVersion {
		return nil, fmt.Errorf("invalid telemetry.schema.version %q, expected %q", schemaVersion, tmcfg.SchemaVersion)
	}

	installationID := attrs[string(tmcfg.ResourceAttrInstallationID)]
	if installationID == "" {
		return nil, errors.New("missing installation.id resource attribute")
	}

	parsed := &parsedRequest{
		installationID: installationID,
		schemaVersion:  schemaVersion,
		commands:       make(map[string]int64),
		harnesses:      make(map[string]int64),
	}

	for _, sm := range rm.GetScopeMetrics() {
		for _, m := range sm.GetMetrics() {
			if m == nil {
				continue
			}
			switch m.GetName() {
			case string(tmcfg.CommandCounterName):
				for _, dp := range m.GetSum().GetDataPoints() {
					attrs := attributeMap(dp.GetAttributes())
					cmd := attrs[string(tmcfg.ResourceAttrCommandName)]
					harnesses := attrs[string(tmcfg.ResourceAttrHarnesses)]
					value := dp.GetAsInt()
					if cmd != "" {
						parsed.commands[cmd] += value
					}
					if harnesses != "" {
						for _, h := range strings.Split(harnesses, ",") {
							h = strings.TrimSpace(h)
							if h == "" {
								continue
							}
							parsed.harnesses[h] += value
						}
					}
					parsed.metricCount++
				}
			case string(tmcfg.CommandDurationName):
				parsed.metricCount += int64(len(m.GetHistogram().GetDataPoints()))
			default:
				return nil, fmt.Errorf("metric %q is not allowed", m.GetName())
			}
		}
	}

	return parsed, nil
}

func (s *Server) updateCounts(p *parsedRequest) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	for k, v := range p.commands {
		s.commandCounts[k] += v
	}
	for k, v := range p.harnesses {
		s.harnessCounts[k] += v
	}
	s.totalMetrics += p.metricCount
	s.lastReceive = now
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.mu.RLock()
	last := s.lastReceive
	s.mu.RUnlock()

	resp := map[string]string{"status": "ok"}
	if last.IsZero() {
		resp["last_receive"] = "never"
	} else {
		resp["last_receive"] = last.Format(time.RFC3339)
	}
	writeJSON(w, resp)
}

type dashboardResponse struct {
	Status      string          `json:"status"`
	LastReceive string          `json:"last_receive"`
	Adoption    adoptionSummary `json:"adoption"`
	Health      healthSummary   `json:"health"`
}

type adoptionSummary struct {
	Commands  map[string]int64 `json:"commands"`
	Harnesses map[string]int64 `json:"harnesses"`
}

type healthSummary struct {
	UniqueInstallations int64 `json:"unique_installations"`
	StorageSizeBytes    int64 `json:"storage_size_bytes"`
	MetricCount         int64 `json:"metric_count"`
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.mu.RLock()
	last := s.lastReceive
	commands := make(map[string]int64, len(s.commandCounts))
	for k, v := range s.commandCounts {
		commands[k] = v
	}
	harnesses := make(map[string]int64, len(s.harnessCounts))
	for k, v := range s.harnessCounts {
		harnesses[k] = v
	}
	totalMetrics := s.totalMetrics
	s.mu.RUnlock()

	storageSize, _ := s.storage.size()

	resp := dashboardResponse{
		Status:      "ok",
		LastReceive: last.Format(time.RFC3339),
		Adoption: adoptionSummary{
			Commands:  commands,
			Harnesses: harnesses,
		},
		Health: healthSummary{
			UniqueInstallations: int64(s.budget.Count()),
			StorageSizeBytes:    storageSize,
			MetricCount:         totalMetrics,
		},
	}
	if last.IsZero() {
		resp.LastReceive = "never"
	}

	writeJSON(w, resp)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	b, err := json.Marshal(v)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_, _ = w.Write(b)
}

func attributeMap(attrs []*commonpb.KeyValue) map[string]string {
	out := make(map[string]string, len(attrs))
	for _, kv := range attrs {
		if kv == nil {
			continue
		}
		out[kv.Key] = kv.Value.GetStringValue()
	}
	return out
}
