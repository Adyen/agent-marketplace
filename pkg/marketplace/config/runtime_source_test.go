package config

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// spec: marketplace-runtime-configuration/unchanged-configuration-bytes-cause-no-work
func TestRuntimeSourceIgnoresUnchangedBytes(t *testing.T) {
	source, err := LoadRuntimeSource(writeConfig(t, validConfigYAML()))
	if err != nil {
		t.Fatalf("LoadRuntimeSource: %v", err)
	}
	var applies atomic.Int32
	stop := startRuntimeSource(t, source, func(context.Context, Snapshot, Config, Classification) error {
		applies.Add(1)
		return nil
	})
	time.Sleep(30 * time.Millisecond)
	if applies.Load() != 0 || source.Current().Version != 0 {
		t.Fatalf("unchanged source did work: applies=%d snapshot=%+v", applies.Load(), source.Current())
	}
	stop()
}

// spec: marketplace-runtime-configuration/a-replaced-configuration-file-is-applied
func TestRuntimeSourceAppliesAtomicReplacementAndStops(t *testing.T) {
	path := writeConfig(t, validConfigYAML())
	source, err := LoadRuntimeSource(path)
	if err != nil {
		t.Fatalf("LoadRuntimeSource: %v", err)
	}
	applied := make(chan struct {
		current        Snapshot
		candidate      Config
		classification Classification
	}, 1)
	stop := startRuntimeSource(t, source, func(_ context.Context, current Snapshot, candidate Config, classification Classification) error {
		applied <- struct {
			current        Snapshot
			candidate      Config
			classification Classification
		}{current, candidate, classification}
		return nil
	})

	replacement := filepath.Join(filepath.Dir(path), "replacement.yaml")
	candidate := strings.Replace(validConfigYAML(), "interval: 30m", "interval: 45m", 1)
	if err := os.WriteFile(replacement, []byte(candidate), 0o600); err != nil {
		t.Fatalf("write replacement: %v", err)
	}
	if err := os.Rename(replacement, path); err != nil {
		t.Fatalf("rename replacement: %v", err)
	}
	select {
	case got := <-applied:
		if got.current.Version != 0 || got.candidate.Refresh.Interval.Duration() != 45*time.Minute ||
			!got.classification.RefreshIntervalChanged {
			t.Fatalf("applied replacement = %+v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("atomic replacement was not applied")
	}
	awaitRuntimeCondition(t, func() bool {
		return source.Current().Version == 1 && source.Current().Config.Refresh.Interval.Duration() == 45*time.Minute
	})
	stop()
}

// spec: marketplace-runtime-configuration/a-partially-written-configuration-is-rejected
// spec: marketplace-runtime-configuration/a-missing-configuration-file-keeps-every-registry
// spec: marketplace-runtime-configuration/a-configuration-that-fails-validation-is-rejected
func TestRuntimeSourceRejectsCandidatesAndKeepsLastGood(t *testing.T) {
	logs := captureRuntimeLogs(t)
	path := writeConfig(t, validConfigYAML())
	source, err := LoadRuntimeSource(path)
	if err != nil {
		t.Fatalf("LoadRuntimeSource: %v", err)
	}
	var applies atomic.Int32
	startRuntimeSource(t, source, func(context.Context, Snapshot, Config, Classification) error {
		applies.Add(1)
		return nil
	})

	const secret = "runtime-config-secret"
	if err := os.WriteFile(path, []byte("registries:\n  - token: "+secret+"\n  - id: ["), 0o600); err != nil {
		t.Fatalf("write partial candidate: %v", err)
	}
	awaitRuntimeCondition(t, func() bool { return logs.Count("config candidate rejected") == 1 })
	time.Sleep(20 * time.Millisecond)
	if source.Current().Version != 0 || len(source.Current().Config.Registries) != 1 ||
		logs.Count("config candidate rejected") != 1 {
		t.Fatalf("partial candidate changed last-good state or was logged repeatedly: snapshot=%+v logs=%q", source.Current(), logs.String())
	}

	if err := os.Remove(path); err != nil {
		t.Fatalf("remove config: %v", err)
	}
	awaitRuntimeCondition(t, func() bool { return logs.Count("config candidate rejected") == 2 })
	if source.Current().Version != 0 || len(source.Current().Config.Registries) != 1 {
		t.Fatalf("missing candidate changed last-good state: %+v", source.Current())
	}

	duplicate := strings.Replace(validConfigYAML(), "revocations:\n", registryYAML("")+"revocations:\n", 1)
	if err := os.WriteFile(path, []byte(duplicate), 0o600); err != nil {
		t.Fatalf("write invalid candidate: %v", err)
	}
	awaitRuntimeCondition(t, func() bool { return logs.Count("config candidate rejected") == 3 })
	if source.Current().Version != 0 || len(source.Current().Config.Registries) != 1 {
		t.Fatalf("invalid candidate changed last-good state: %+v", source.Current())
	}

	second := strings.ReplaceAll(registryYAML(""), "id: src-test", "id: src-second")
	second = strings.ReplaceAll(second, "slug: example-source", "slug: second-source")
	valid := strings.Replace(validConfigYAML(), "revocations:\n", second+"revocations:\n", 1)
	if err := os.WriteFile(path, []byte(valid), 0o600); err != nil {
		t.Fatalf("write valid candidate: %v", err)
	}
	awaitRuntimeCondition(t, func() bool { return source.Current().Version == 1 })
	if len(source.Current().Config.Registries) != 2 || applies.Load() != 1 {
		t.Fatalf("later valid candidate was not applied: snapshot=%+v applies=%d", source.Current(), applies.Load())
	}
	if strings.Contains(logs.String(), secret) {
		t.Fatalf("candidate rejection log exposed credential value: %q", logs.String())
	}
}

// spec: marketplace-runtime-configuration/a-worker-safe-change-applies-while-restart-required-changes-are-reported
// spec: marketplace-runtime-configuration/restoring-startup-values-clears-the-restart-required-report
func TestRuntimeSourceReportsAndClearsRestartRequiredFields(t *testing.T) {
	logs := captureRuntimeLogs(t)
	path := writeConfig(t, validConfigYAML())
	source, err := LoadRuntimeSource(path)
	if err != nil {
		t.Fatalf("LoadRuntimeSource: %v", err)
	}
	applied := make(chan struct {
		config         Config
		classification Classification
	}, 1)
	stop := startRuntimeSource(t, source, func(_ context.Context, _ Snapshot, candidate Config, classification Classification) error {
		applied <- struct {
			config         Config
			classification Classification
		}{candidate, classification}
		return nil
	})

	candidate := strings.Replace(validConfigYAML(), "listen: 127.0.0.1:8080", "listen: 127.0.0.1:8181", 1)
	candidate = strings.Replace(candidate, "dsn: /tmp/marketplace.db", "dsn: /tmp/other.db", 1)
	candidate = strings.Replace(candidate, "name: Test Marketplace", "name: Other Marketplace", 1)
	candidate = strings.Replace(candidate, "registry_ids: []", "registry_ids: [src-test]", 1)
	candidate = strings.Replace(candidate, "    license: MIT", "    license: MIT\n    policy_overrides:\n      hidden_content: warn", 1)
	if err := os.WriteFile(path, []byte(candidate), 0o600); err != nil {
		t.Fatalf("write candidate: %v", err)
	}
	var first struct {
		config         Config
		classification Classification
	}
	select {
	case first = <-applied:
	case <-time.After(2 * time.Second):
		t.Fatal("worker-safe candidate was not applied")
	}
	want := []string{"listen", "storage", "instance", "revocations"}
	if !equalStrings(first.classification.RestartRequired, want) ||
		first.config.Listen != source.Startup().Listen ||
		first.config.Storage != source.Startup().Storage ||
		first.config.Instance != source.Startup().Instance ||
		len(first.config.Revocations.RegistryIDs) != 0 ||
		first.config.Registries[0].PolicyOverrides["hidden_content"] != "warn" {
		t.Fatalf("candidate classification or effective config = %+v / %+v", first.classification, first.config)
	}
	awaitRuntimeCondition(t, func() bool { return equalStrings(source.RestartRequired(), want) })

	restored := strings.Replace(validConfigYAML(), "    license: MIT", "    license: MIT\n    policy_overrides:\n      hidden_content: warn", 1)
	if err := os.WriteFile(path, []byte(restored), 0o600); err != nil {
		t.Fatalf("restore startup-owned values: %v", err)
	}
	awaitRuntimeCondition(t, func() bool {
		return len(source.RestartRequired()) == 0 && strings.Contains(logs.String(), "restart-required config drift cleared")
	})
	if len(applied) != 0 {
		t.Fatal("restart-only restoration requested worker registry work")
	}
	stop()
}

func startRuntimeSource(t *testing.T, source *RuntimeSource, apply func(context.Context, Snapshot, Config, Classification) error) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- source.Run(ctx, 5*time.Millisecond, apply) }()
	var once sync.Once
	stop := func() {
		once.Do(func() {
			cancel()
			select {
			case err := <-done:
				if err != nil && !errors.Is(err, context.Canceled) {
					t.Errorf("RuntimeSource.Run: %v", err)
				}
			case <-time.After(time.Second):
				t.Error("RuntimeSource.Run did not stop after cancellation")
			}
		})
	}
	t.Cleanup(stop)
	return stop
}

func awaitRuntimeCondition(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.After(3 * time.Second)
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		if condition() {
			return
		}
		select {
		case <-deadline:
			t.Fatal("runtime configuration condition was not reached")
		case <-ticker.C:
		}
	}
}

type lockedRuntimeLogBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedRuntimeLogBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedRuntimeLogBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func (b *lockedRuntimeLogBuffer) Count(message string) int {
	return strings.Count(b.String(), message)
}

func captureRuntimeLogs(t *testing.T) *lockedRuntimeLogBuffer {
	t.Helper()
	buffer := &lockedRuntimeLogBuffer{}
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buffer, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return buffer
}
