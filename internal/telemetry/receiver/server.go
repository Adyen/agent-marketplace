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

const DefaultPrometheusSeriesBudget = 10000

// Config is the receiver runtime configuration. All fields have sensible
// defaults except StorageDir, which is required.
type Config struct {
	ListenAddr       string
	StorageDir       string
	MaxInstallations int
	MaxSeries        int
	Logger           *slog.Logger
}

// Server is a minimal OTLP/HTTP metrics receiver for community telemetry.
type Server struct {
	cfg     Config
	storage *storage
	budget  *cardinality
	mux     *http.ServeMux
	lease   *storageLease

	ingestMu           sync.Mutex
	mu                 sync.RWMutex
	lastReceive        time.Time
	commandCounts      map[string]int64
	harnessCounts      map[string]int64
	processHarnesses   map[string]int64
	invocationCounts   map[invocationLabels]int64
	durationHistograms map[durationLabels]*aggregatedHistogram
	requestCounts      map[requestLabels]int64
	installationSeen   map[string]time.Time
	totalMetrics       int64
	processMetrics     int64

	ln net.Listener
}

type parsedRequest struct {
	installationID string
	schemaVersion  string
	resourceLabels resourceLabels
	commands       map[string]int64
	harnesses      map[string]int64
	invocations    map[invocationLabels]int64
	durations      map[durationLabels]*aggregatedHistogram
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
	if cfg.MaxSeries <= 0 {
		cfg.MaxSeries = DefaultPrometheusSeriesBudget
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	s := &Server{
		cfg:                cfg,
		storage:            newStorage(cfg.StorageDir),
		budget:             newCardinality(cfg.StorageDir, cfg.MaxInstallations),
		commandCounts:      make(map[string]int64),
		harnessCounts:      make(map[string]int64),
		processHarnesses:   make(map[string]int64),
		invocationCounts:   make(map[invocationLabels]int64),
		durationHistograms: make(map[durationLabels]*aggregatedHistogram),
		requestCounts:      make(map[requestLabels]int64),
		installationSeen:   make(map[string]time.Time),
	}

	s.mux = http.NewServeMux()
	s.mux.HandleFunc("/v1/metrics", s.handleMetrics)
	s.mux.HandleFunc("/metrics", s.handlePrometheusMetrics)
	s.mux.HandleFunc("/health", s.handleHealth)
	s.mux.HandleFunc("/v1/dashboard.json", s.handleDashboard)

	if err := s.storage.ensureDir(); err != nil {
		return nil, fmt.Errorf("ensure storage directory: %w", err)
	}
	lease, err := acquireStorageLease(cfg.StorageDir)
	if err != nil {
		return nil, err
	}
	s.lease = lease
	if err := s.budget.Load(); err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("load cardinality budget: %w", err)
	}
	if err := s.storage.load(s.restoreRecord); err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("load telemetry records: %w", err)
	}

	return s, nil
}

// Handler returns the server's HTTP handler. It can be used with httptest
// without calling Start.
func (s *Server) Handler() http.Handler {
	return s.mux
}

func (s *Server) Close() error {
	return s.lease.Close()
}

// Start binds the configured address and serves until ctx is cancelled. It
// performs a graceful shutdown when the context is done.
func (s *Server) Start(ctx context.Context) error {
	defer func() { _ = s.Close() }()
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
		s.rejectMetrics(w, http.StatusMethodNotAllowed, "method not allowed", "method_not_allowed")
		return
	}
	if ct := r.Header.Get("Content-Type"); ct != "" && ct != "application/x-protobuf" {
		s.rejectMetrics(w, http.StatusUnsupportedMediaType, "unsupported content type, expected application/x-protobuf", "content_type")
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		s.rejectMetrics(w, http.StatusBadRequest, "failed to read body", "invalid_payload")
		return
	}

	var req metricspb.ExportMetricsServiceRequest
	if err := proto.Unmarshal(body, &req); err != nil {
		s.rejectMetrics(w, http.StatusBadRequest, "invalid protobuf payload", "invalid_payload")
		return
	}

	if len(req.ResourceMetrics) != 1 {
		s.rejectMetrics(w, http.StatusBadRequest, "expected exactly one resource metrics", "invalid_payload")
		return
	}

	parsed, err := s.parseResourceMetrics(req.ResourceMetrics[0])
	if err != nil {
		s.rejectMetrics(w, http.StatusBadRequest, err.Error(), "invalid_payload")
		return
	}

	s.ingestMu.Lock()
	defer s.ingestMu.Unlock()
	if s.wouldExceedSeriesBudget(parsed) {
		s.rejectMetrics(w, http.StatusTooManyRequests, "prometheus series budget exceeded", "series_budget")
		return
	}
	if s.wouldOverflow(parsed) {
		s.rejectMetrics(w, http.StatusBadRequest, "metric aggregation overflow", "invalid_payload")
		return
	}

	installationKnown := s.budget.Contains(parsed.installationID)
	accepted, err := s.budget.Add(parsed.installationID)
	if err != nil {
		s.cfg.Logger.Error("failed to check cardinality budget", "error", err)
		s.rejectMetrics(w, http.StatusInternalServerError, "internal error", "cardinality_state")
		return
	}
	if !accepted {
		s.cfg.Logger.Warn("cardinality budget exceeded")
		s.rejectMetrics(w, http.StatusTooManyRequests, "cardinality budget exceeded", "cardinality_budget")
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
		if !installationKnown {
			if rollbackErr := s.budget.Remove(parsed.installationID); rollbackErr != nil {
				s.cfg.Logger.Error("failed to roll back installation cardinality", "error", rollbackErr)
			}
		}
		s.rejectMetrics(w, http.StatusInternalServerError, "failed to persist", "persistence")
		return
	}

	s.updateCounts(parsed, record.ReceivedAt)
	s.recordRequest("accepted", "")
	s.cfg.Logger.Debug("accepted metrics", "metric_count", parsed.metricCount)
	w.WriteHeader(http.StatusOK)
}

func (s *Server) parseResourceMetrics(rm *mpb.ResourceMetrics) (*parsedRequest, error) {
	if rm == nil {
		return nil, errors.New("resource metrics is nil")
	}

	attrs, err := attributeMap(rm.Resource.GetAttributes(), resourceAttributes)
	if err != nil {
		return nil, err
	}
	schemaVersion := attrs[string(tmcfg.ResourceAttrSchemaVersion)]
	if schemaVersion != tmcfg.SchemaVersion {
		return nil, fmt.Errorf("invalid telemetry.schema.version %q, expected %q", schemaVersion, tmcfg.SchemaVersion)
	}

	installationID := attrs[string(tmcfg.ResourceAttrInstallationID)]
	if !installationIDPattern.MatchString(installationID) {
		return nil, errors.New("invalid installation.id resource attribute")
	}

	parsed := &parsedRequest{
		installationID: installationID,
		schemaVersion:  schemaVersion,
		resourceLabels: resourceLabels{
			cliVersion: attrs[string(tmcfg.ResourceAttrCLIVersion)],
			osName:     attrs[string(tmcfg.ResourceAttrOSName)],
			arch:       attrs[string(tmcfg.ResourceAttrArch)],
		},
		commands:    make(map[string]int64),
		harnesses:   make(map[string]int64),
		invocations: make(map[invocationLabels]int64),
		durations:   make(map[durationLabels]*aggregatedHistogram),
	}
	if err := validateResourceLabels(parsed.resourceLabels); err != nil {
		return nil, err
	}

	for _, sm := range rm.GetScopeMetrics() {
		for _, m := range sm.GetMetrics() {
			if m == nil {
				continue
			}
			switch m.GetName() {
			case string(tmcfg.CommandCounterName):
				sum := m.GetSum()
				if sum == nil || !sum.GetIsMonotonic() || sum.GetAggregationTemporality() != mpb.AggregationTemporality_AGGREGATION_TEMPORALITY_DELTA {
					return nil, fmt.Errorf("metric %q must be a monotonic delta sum", m.GetName())
				}
				for _, dp := range sum.GetDataPoints() {
					if dp == nil {
						return nil, fmt.Errorf("command counter data point is nil")
					}
					value, ok := dp.Value.(*mpb.NumberDataPoint_AsInt)
					if !ok || value.AsInt < 0 {
						return nil, fmt.Errorf("command counter must contain non-negative integer values")
					}
					metricAttrs, err := attributeMap(dp.GetAttributes(), metricAttributes)
					if err != nil {
						return nil, err
					}
					if err := validateMetricAttributes(metricAttrs); err != nil {
						return nil, err
					}
					cmd := metricAttrs[string(tmcfg.ResourceAttrCommandName)]
					harnesses := metricAttrs[string(tmcfg.ResourceAttrHarnesses)]
					count := dp.GetAsInt()
					if err := addInt64(parsed.commands, cmd, count); err != nil {
						return nil, err
					}
					if err := addInt64(parsed.invocations, newInvocationLabels(parsed.resourceLabels, metricAttrs), count); err != nil {
						return nil, err
					}
					for _, h := range strings.Split(harnesses, ",") {
						h = strings.TrimSpace(h)
						if h != "" {
							if err := addInt64(parsed.harnesses, h, count); err != nil {
								return nil, err
							}
						}
					}
					parsed.metricCount++
				}
			case string(tmcfg.CommandDurationName):
				histogram := m.GetHistogram()
				if histogram == nil || histogram.GetAggregationTemporality() != mpb.AggregationTemporality_AGGREGATION_TEMPORALITY_DELTA {
					return nil, fmt.Errorf("metric %q must be a delta histogram", m.GetName())
				}
				for _, dp := range histogram.GetDataPoints() {
					duration, err := newDurationPoint(dp)
					if err != nil {
						return nil, err
					}
					parsed.durations[duration.labels], err = mergeHistogram(parsed.durations[duration.labels], duration)
					if err != nil {
						return nil, err
					}
					parsed.metricCount++
				}
			default:
				return nil, fmt.Errorf("metric %q is not allowed", m.GetName())
			}
		}
	}
	if parsed.metricCount == 0 {
		return nil, errors.New("telemetry payload contains no metric data points")
	}

	return parsed, nil
}

func (s *Server) updateCounts(p *parsedRequest, receivedAt time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.applyAdoptionLocked(receivedAt, p.installationID, p.commands, p.harnesses, p.metricCount)
	s.processMetrics += p.metricCount
	for k, v := range p.harnesses {
		s.processHarnesses[k] += v
	}
	for k, v := range p.invocations {
		s.invocationCounts[k] += v
	}
	for labels, histogram := range p.durations {
		s.durationHistograms[labels] = mergeAggregatedHistogram(s.durationHistograms[labels], histogram)
	}
}

func (s *Server) restoreRecord(record *requestRecord) error {
	if err := validateStoredRecord(record); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !canAddInt64(s.totalMetrics, record.MetricCount) {
		return errors.New("stored metric count overflow")
	}
	for command, count := range record.Commands {
		if !canAddInt64(s.commandCounts[command], count) {
			return errors.New("stored command count overflow")
		}
	}
	for harness, count := range record.Harnesses {
		if !canAddInt64(s.harnessCounts[harness], count) {
			return errors.New("stored harness count overflow")
		}
	}
	s.applyAdoptionLocked(record.ReceivedAt, record.InstallationID, record.Commands, record.Harnesses, record.MetricCount)
	return nil
}

func validateStoredRecord(record *requestRecord) error {
	if record == nil || record.ReceivedAt.IsZero() {
		return errors.New("stored telemetry record has invalid receive time")
	}
	if !installationIDPattern.MatchString(record.InstallationID) {
		return errors.New("stored telemetry record has invalid installation id")
	}
	if record.SchemaVersion != tmcfg.SchemaVersion || record.MetricCount <= 0 {
		return errors.New("stored telemetry record has invalid schema or metric count")
	}
	for command, count := range record.Commands {
		if len(command) > 128 || !commandNamePattern.MatchString(command) || count < 0 {
			return errors.New("stored telemetry record has invalid command")
		}
	}
	for harness, count := range record.Harnesses {
		if _, ok := knownHarnesses[harness]; !ok || count < 0 {
			return errors.New("stored telemetry record has invalid harness")
		}
	}
	return nil
}

func (s *Server) applyAdoptionLocked(receivedAt time.Time, installationID string, commands, harnesses map[string]int64, metricCount int64) {
	for k, v := range commands {
		s.commandCounts[k] += v
	}
	for k, v := range harnesses {
		s.harnessCounts[k] += v
	}
	if receivedAt.After(s.installationSeen[installationID]) {
		s.installationSeen[installationID] = receivedAt
	}
	s.totalMetrics += metricCount
	if receivedAt.After(s.lastReceive) {
		s.lastReceive = receivedAt
	}
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

func attributeMap(attrs []*commonpb.KeyValue, allowed map[string]struct{}) (map[string]string, error) {
	out := make(map[string]string, len(attrs))
	for _, kv := range attrs {
		if kv == nil || kv.Value == nil {
			return nil, errors.New("nil telemetry attribute")
		}
		if _, ok := allowed[kv.Key]; !ok {
			return nil, fmt.Errorf("telemetry attribute %q is not allowed", kv.Key)
		}
		if _, duplicate := out[kv.Key]; duplicate {
			return nil, fmt.Errorf("duplicate telemetry attribute %q", kv.Key)
		}
		value, ok := kv.Value.Value.(*commonpb.AnyValue_StringValue)
		if !ok {
			return nil, fmt.Errorf("telemetry attribute %q must be a string", kv.Key)
		}
		out[kv.Key] = value.StringValue
	}
	if len(out) != len(allowed) {
		return nil, errors.New("missing required telemetry attribute")
	}
	return out, nil
}
