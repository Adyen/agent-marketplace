package config

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Adyen/agent-marketplace/internal/redact"
)

const RuntimePollInterval = time.Second

// Snapshot is an immutable version of the effective worker configuration.
type Snapshot struct {
	Version uint64
	Config  Config
}

// RuntimeSource polls one operator configuration file and retains its last
// successfully applied worker-safe configuration.
type RuntimeSource struct {
	path    string
	startup Config
	current atomic.Pointer[Snapshot]

	mu              sync.RWMutex
	lastDigest      [32]byte
	rejected        map[[32]byte]struct{}
	restartRequired []string
}

// LoadRuntimeSource reads and validates the startup configuration.
func LoadRuntimeSource(path string) (*RuntimeSource, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %q: %w", path, err)
	}
	cfg, err := ParseConfig(data)
	if err != nil {
		return nil, fmt.Errorf("parse config %q: %w", path, err)
	}
	source := &RuntimeSource{
		path:       path,
		startup:    cfg,
		lastDigest: sha256.Sum256(data),
		rejected:   make(map[[32]byte]struct{}),
	}
	source.current.Store(&Snapshot{Config: cfg})
	return source, nil
}

// Startup returns the validated configuration used to initialize serve.
func (s *RuntimeSource) Startup() Config {
	return s.startup
}

// Current returns the latest successfully applied worker configuration.
func (s *RuntimeSource) Current() Snapshot {
	return *s.current.Load()
}

// RestartRequired returns the startup-owned fields that differ from the file.
func (s *RuntimeSource) RestartRequired() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return slices.Clone(s.restartRequired)
}

// Run polls configuration bytes until ctx is cancelled.
func (s *RuntimeSource) Run(ctx context.Context, interval time.Duration, apply func(context.Context, Snapshot, Config, Classification) error) error {
	if interval <= 0 {
		interval = RuntimePollInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			s.poll(ctx, apply)
		}
	}
}

func (s *RuntimeSource) poll(ctx context.Context, apply func(context.Context, Snapshot, Config, Classification) error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		s.reject(sha256.Sum256([]byte("missing configuration")), boundedRuntimeReason(err))
		return
	}
	digest := sha256.Sum256(data)
	s.mu.RLock()
	unchanged := digest == s.lastDigest
	s.mu.RUnlock()
	if unchanged {
		return
	}
	candidate, err := ParseConfig(data)
	if err != nil {
		s.reject(digest, boundedRuntimeReason(err))
		return
	}
	current := s.Current()
	classification, err := Classify(s.startup, current.Config, candidate)
	if err != nil {
		s.reject(digest, boundedRuntimeReason(err))
		return
	}
	s.setRestartRequired(classification.RestartRequired)
	effective := retainStartupOwnedFields(s.startup, candidate)
	if !classification.hasWorkerChanges() {
		s.setLastDigest(digest)
		return
	}
	if apply != nil {
		if err := apply(ctx, current, effective, classification); err != nil {
			slog.Error("config candidate apply failed", "reason", boundedRuntimeReason(err))
			return
		}
	}
	next := &Snapshot{Version: current.Version + 1, Config: effective}
	s.current.Store(next)
	s.setLastDigest(digest)
	slog.Info("config candidate applied",
		"version", next.Version,
		"added", len(classification.Added),
		"removed", len(classification.Removed),
		"indexing_changed", len(classification.IndexingChanged),
		"metadata_changed", len(classification.MetadataChanged))
}

func (s *RuntimeSource) reject(digest [32]byte, reason string) {
	s.mu.Lock()
	s.lastDigest = digest
	if _, exists := s.rejected[digest]; exists {
		s.mu.Unlock()
		return
	}
	s.rejected[digest] = struct{}{}
	s.mu.Unlock()
	slog.Warn("config candidate rejected", "reason", reason)
}

func (s *RuntimeSource) setLastDigest(digest [32]byte) {
	s.mu.Lock()
	s.lastDigest = digest
	s.mu.Unlock()
}

func (s *RuntimeSource) setRestartRequired(fields []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if slices.Equal(s.restartRequired, fields) {
		return
	}
	s.restartRequired = slices.Clone(fields)
	if len(fields) == 0 {
		slog.Info("restart-required config drift cleared")
	} else {
		slog.Warn("restart-required config drift detected", "fields", strings.Join(fields, ","))
	}
}

func retainStartupOwnedFields(startup, candidate Config) Config {
	candidate.Listen = startup.Listen
	candidate.OperationsListen = startup.OperationsListen
	candidate.Storage = startup.Storage
	candidate.Instance = startup.Instance
	candidate.Revocations = startup.Revocations
	candidate.Management = startup.Management
	candidate.Admin = startup.Admin
	candidate.CatalogProjection = startup.CatalogProjection
	candidate.InstallationMetrics = startup.InstallationMetrics
	candidate.RefreshConcurrency = startup.RefreshConcurrency
	return candidate
}

func (c Classification) hasWorkerChanges() bool {
	return len(c.Added)+len(c.Removed)+len(c.IndexingChanged)+len(c.MetadataChanged) != 0 ||
		c.RefreshIntervalChanged
}

func boundedRuntimeReason(err error) string {
	if err == nil {
		return ""
	}
	message := redact.Error(err).Error()
	const limit = 200
	runes := []rune(message)
	if len(runes) > limit {
		return string(runes[:limit-1]) + "…"
	}
	return message
}
