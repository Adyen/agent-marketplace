package worker

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/acquire"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"gopkg.in/yaml.v3"
)

// spec: marketplace-runtime-configuration/a-comment-and-formatting-edit-causes-no-registry-work
// spec: marketplace-runtime-configuration/a-presentation-only-change-updates-metadata-without-acquisition
// spec: marketplace-runtime-configuration/a-refresh-interval-change-requests-no-refresh
// spec: marketplace-runtime-configuration/an-added-registry-is-observed-and-indexed
// spec: marketplace-runtime-configuration/a-removed-registry-is-unlisted-and-keeps-its-history
// spec: marketplace-source-observation/a-registry-indexed-under-the-earlier-generation-scheme-refreshes-once
func TestServeWorkerReconcilesRuntimeConfiguration(t *testing.T) {
	f := newObservationFixture(t, 1)
	source, path := newWorkerRuntimeSource(t, f)
	stop := startRuntimeWorker(t, f, source)

	initial := source.Startup()
	registryID := store.RegistryID(initial.Registries[0].ID)
	initialGeneration, err := config.IndexingGeneration(initial, initial.Registries[0])
	if err != nil {
		t.Fatal(err)
	}
	awaitWorkerRefreshSettled(t, f, registryID, initialGeneration)
	deferWorkerObservation(t, f, registryID)
	initialObservation, err := f.meta.GetRegistryObservation(f.ctx, registryID)
	if err != nil {
		t.Fatal(err)
	}
	initialRegistry, err := f.meta.GetRegistry(f.ctx, registryID)
	if err != nil {
		t.Fatal(err)
	}
	initialClones := f.counts.clones.Load()
	initialRequests := len(f.requests())

	formatted := marshalWorkerRuntimeConfig(t, source.Current().Config, 4)
	lines := strings.Split(string(formatted), "\n")
	for i := 0; i+1 < len(lines); i++ {
		if strings.Contains(lines[i], "id: "+string(registryID)) && strings.Contains(lines[i+1], "slug: "+initial.Registries[0].Slug) {
			lines[i] = strings.Replace(lines[i], "id: "+string(registryID), "slug: "+initial.Registries[0].Slug, 1)
			lines[i+1] = strings.Replace(lines[i+1], "slug: "+initial.Registries[0].Slug, "id: "+string(registryID), 1)
			break
		}
	}
	formatted = append([]byte("# formatting-only edit\n"), []byte(strings.Join(lines, "\n"))...)
	if _, err := config.ParseConfig(formatted); err != nil {
		t.Fatalf("formatting-only candidate is invalid: %v", err)
	}
	writeWorkerRuntimeCandidate(t, path, formatted)
	for poll := 0; poll < 3; poll++ {
		time.Sleep(config.RuntimePollInterval)
		afterFormattingRegistry, err := f.meta.GetRegistry(f.ctx, registryID)
		if err != nil {
			t.Fatal(err)
		}
		afterFormattingObservation, err := f.meta.GetRegistryObservation(f.ctx, registryID)
		if err != nil {
			t.Fatal(err)
		}
		requests := f.requests()
		if source.Current().Version != 0 || f.counts.clones.Load() != initialClones ||
			len(requests) != initialRequests || afterFormattingRegistry.UpdatedAt != initialRegistry.UpdatedAt ||
			afterFormattingObservation.Version != initialObservation.Version {
			t.Fatalf("formatting-only edit caused runtime work on poll %d: version=%d clones=%d requests=%d registry updated=%s observation version=%d",
				poll+1, source.Current().Version, f.counts.clones.Load(), len(requests),
				afterFormattingRegistry.UpdatedAt, afterFormattingObservation.Version)
		}
	}

	candidate := cloneWorkerRuntimeConfig(t, source.Current().Config)
	candidate.Registries[0].Slug = "renamed-registry"
	candidate.Registries[0].Publisher.Name = "Renamed Publisher"
	writeWorkerRuntimeConfig(t, path, candidate)
	awaitWorkerCondition(t, func() bool { return source.Current().Version == 1 })
	metadata, err := f.meta.GetRegistry(f.ctx, registryID)
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Slug != "renamed-registry" || !reflect.DeepEqual(metadata.Aliases, []string{initial.Registries[0].Slug}) ||
		metadata.Publisher.Name != "Renamed Publisher" || f.counts.clones.Load() != initialClones ||
		len(f.requests()) != initialRequests {
		t.Fatalf("presentation-only change = %+v clones=%d requests=%d", metadata, f.counts.clones.Load(), len(f.requests()))
	}

	candidate = cloneWorkerRuntimeConfig(t, source.Current().Config)
	candidate.Refresh.Interval = config.Duration(30 * 24 * time.Hour)
	writeWorkerRuntimeConfig(t, path, candidate)
	awaitWorkerCondition(t, func() bool { return source.Current().Version == 2 })
	beforeIntervalObservation, err := f.meta.GetRegistryObservation(f.ctx, registryID)
	if err != nil {
		t.Fatal(err)
	}
	setRuntimeObservationDue(t, f, registryID)
	awaitWorkerCondition(t, func() bool {
		observation, err := f.meta.GetRegistryObservation(f.ctx, registryID)
		return err == nil && observation.ObservedAt.After(beforeIntervalObservation.ObservedAt)
	})
	afterIntervalObservation, err := f.meta.GetRegistryObservation(f.ctx, registryID)
	if err != nil {
		t.Fatal(err)
	}
	if !afterIntervalObservation.NextProbeAt.After(time.Now()) ||
		afterIntervalObservation.NextProbeAt.After(time.Now().Add(30*24*time.Hour)) ||
		f.counts.clones.Load() != initialClones || len(f.requests()) != initialRequests {
		t.Fatalf("refresh interval change requested work or scheduled outside interval: observation=%+v clones=%d requests=%d",
			afterIntervalObservation, f.counts.clones.Load(), len(f.requests()))
	}

	candidate = cloneWorkerRuntimeConfig(t, source.Current().Config)
	candidate.Registries[0].PolicyOverrides = map[string]any{"hidden_content": "warn"}
	nextGeneration, err := config.IndexingGeneration(candidate, candidate.Registries[0])
	if err != nil {
		t.Fatal(err)
	}
	requestsBeforeIndexingChange := len(f.requests())
	writeWorkerRuntimeConfig(t, path, candidate)
	awaitWorkerCondition(t, func() bool { return source.Current().Version == 3 })
	awaitWorkerCondition(t, func() bool {
		observation, err := f.meta.GetRegistryObservation(f.ctx, registryID)
		return err == nil && observation.SuccessfulGeneration == nextGeneration
	})
	awaitWorkerRefreshSettled(t, f, registryID, nextGeneration)
	afterIndexingObservation, err := f.meta.GetRegistryObservation(f.ctx, registryID)
	if err != nil {
		t.Fatal(err)
	}
	if afterIndexingObservation.SuccessfulCommit != initialObservation.SuccessfulCommit ||
		f.counts.clones.Load() != initialClones+1 ||
		len(f.requests()) != requestsBeforeIndexingChange+1 {
		t.Fatalf("same-commit indexing change = %+v clones=%d requests=%d",
			afterIndexingObservation, f.counts.clones.Load(), len(f.requests()))
	}
	for range 2 {
		before := afterIndexingObservation.ObservedAt
		setRuntimeObservationDue(t, f, registryID)
		awaitWorkerCondition(t, func() bool {
			observation, err := f.meta.GetRegistryObservation(f.ctx, registryID)
			return err == nil && observation.ObservedAt.After(before)
		})
		afterIndexingObservation, err = f.meta.GetRegistryObservation(f.ctx, registryID)
		if err != nil {
			t.Fatal(err)
		}
	}
	if f.counts.clones.Load() != initialClones+1 || len(f.requests()) != requestsBeforeIndexingChange+1 {
		t.Fatalf("later observations repeated indexing acquisition: clones=%d requests=%d",
			f.counts.clones.Load(), len(f.requests()))
	}

	candidate = cloneWorkerRuntimeConfig(t, source.Current().Config)
	added := candidate.Registries[0]
	added.ID = "src-second"
	added.Slug = "second-registry"
	candidate.Registries = append(candidate.Registries, added)
	addedGeneration, err := config.IndexingGeneration(candidate, added)
	if err != nil {
		t.Fatal(err)
	}
	clonesBeforeAdd := f.counts.clones.Load()
	writeWorkerRuntimeConfig(t, path, candidate)
	awaitWorkerCondition(t, func() bool { return source.Current().Version == 4 })
	addedID := store.RegistryID(added.ID)
	awaitWorkerCondition(t, func() bool {
		observation, err := f.meta.GetRegistryObservation(f.ctx, addedID)
		return err == nil && observation.SuccessfulGeneration == addedGeneration
	})
	awaitWorkerRefreshSettled(t, f, addedID, addedGeneration)
	addedRecord, err := f.meta.GetRegistry(f.ctx, addedID)
	if err != nil || !addedRecord.Listed || f.counts.clones.Load() != clonesBeforeAdd+1 {
		t.Fatalf("added registry = %+v, %v; clones=%d", addedRecord, err, f.counts.clones.Load())
	}

	historyBeforeRemoval, err := f.meta.RegistryHistory(f.ctx, registryID)
	if err != nil {
		t.Fatal(err)
	}
	resourcesBeforeRemoval := registryResourceState(t, f)
	candidate = cloneWorkerRuntimeConfig(t, source.Current().Config)
	readdedRegistry := candidate.Registries[0]
	candidate.Registries = candidate.Registries[1:]
	clonesBeforeRemoval := f.counts.clones.Load()
	writeWorkerRuntimeConfig(t, path, candidate)
	awaitWorkerCondition(t, func() bool { return source.Current().Version == 5 })
	removed, err := f.meta.GetRegistry(f.ctx, registryID)
	if err != nil {
		t.Fatal(err)
	}
	historyAfterRemoval, err := f.meta.RegistryHistory(f.ctx, registryID)
	if err != nil {
		t.Fatal(err)
	}
	if removed.Listed || !reflect.DeepEqual(historyAfterRemoval, historyBeforeRemoval) ||
		!reflect.DeepEqual(registryResourceState(t, f), resourcesBeforeRemoval) {
		t.Fatalf("removed registry did not retain history: registry=%+v history=%v", removed, historyAfterRemoval)
	}
	setRuntimeObservationDue(t, f, registryID)
	awaitWorkerCondition(t, func() bool {
		observation, err := f.meta.GetRegistryObservation(f.ctx, registryID)
		return err == nil && observation.NextProbeAt.After(time.Now().Add(time.Minute))
	})
	if f.counts.clones.Load() != clonesBeforeRemoval {
		t.Fatalf("removed registry was refreshed: clones=%d, want %d", f.counts.clones.Load(), clonesBeforeRemoval)
	}

	beforeReaddObservation, err := f.meta.GetRegistryObservation(f.ctx, registryID)
	if err != nil {
		t.Fatal(err)
	}
	candidate = cloneWorkerRuntimeConfig(t, source.Current().Config)
	candidate.Registries = append(candidate.Registries, readdedRegistry)
	writeWorkerRuntimeConfig(t, path, candidate)
	awaitWorkerCondition(t, func() bool { return source.Current().Version == 6 })
	readded, err := f.meta.GetRegistry(f.ctx, registryID)
	if err != nil || !readded.Listed {
		t.Fatalf("re-added registry = %+v, %v", readded, err)
	}
	awaitWorkerCondition(t, func() bool {
		observation, err := f.meta.GetRegistryObservation(f.ctx, registryID)
		return err == nil && observation.ObservedAt.After(beforeReaddObservation.ObservedAt)
	})

	updateRuntimeObservation(t, f, addedID, func(observation *store.RegistryObservation) {
		observation.SuccessfulGeneration = "startup-v1:previous"
	})
	legacyObservation, err := f.meta.GetRegistryObservation(f.ctx, addedID)
	if err != nil {
		t.Fatal(err)
	}
	currentConfig := source.Current().Config
	currentGeneration, err := config.IndexingGeneration(currentConfig, currentConfig.Registries[0])
	if err != nil {
		t.Fatal(err)
	}
	clonesBeforeUpgrade := f.counts.clones.Load()
	setRuntimeObservationDue(t, f, addedID)
	awaitWorkerCondition(t, func() bool {
		observation, err := f.meta.GetRegistryObservation(f.ctx, addedID)
		return err == nil && observation.SuccessfulGeneration == currentGeneration
	})
	awaitWorkerRefreshSettled(t, f, addedID, currentGeneration)
	if f.counts.clones.Load() != clonesBeforeUpgrade+1 || legacyObservation.SuccessfulGeneration == currentGeneration {
		t.Fatalf("earlier generation scheme was not refreshed once: clones=%d observation=%+v",
			f.counts.clones.Load(), legacyObservation)
	}
	stop()
}

// spec: marketplace-runtime-configuration/a-refresh-started-for-an-older-generation-cannot-publish
func TestRuntimeConfigurationChangeDuringRefreshPreventsStalePublication(t *testing.T) {
	logs := captureWorkerLogs(t)
	f := newObservationFixture(t, 1)
	source, path := newWorkerRuntimeSource(t, f)
	stop := startRuntimeWorker(t, f, source)

	startup := source.Startup()
	registryID := store.RegistryID(startup.Registries[0].ID)
	initialGeneration, err := config.IndexingGeneration(startup, startup.Registries[0])
	if err != nil {
		t.Fatal(err)
	}
	awaitWorkerRefreshSettled(t, f, registryID, initialGeneration)
	beforeHead := latestHead(t, f)
	beforeResources := registryResourceState(t, f)
	beforeRegistry, err := f.meta.GetRegistry(f.ctx, registryID)
	if err != nil {
		t.Fatal(err)
	}
	firstStarted, firstRelease := f.blockClones()
	firstCandidate := cloneWorkerRuntimeConfig(t, source.Current().Config)
	firstCandidate.Registries[0].PolicyOverrides = map[string]any{"hidden_content": "warn"}
	firstGeneration, err := config.IndexingGeneration(firstCandidate, firstCandidate.Registries[0])
	if err != nil {
		t.Fatal(err)
	}
	writeWorkerRuntimeConfig(t, path, firstCandidate)
	awaitWorkerCondition(t, func() bool { return source.Current().Version == 1 })
	select {
	case <-firstStarted:
	case <-time.After(8 * time.Second):
		t.Fatal("refresh for the first generation did not start acquisition")
	}

	secondCandidate := cloneWorkerRuntimeConfig(t, source.Current().Config)
	secondCandidate.Registries[0].PolicyOverrides = map[string]any{"hidden_content": "reject"}
	secondGeneration, err := config.IndexingGeneration(secondCandidate, secondCandidate.Registries[0])
	if err != nil {
		t.Fatal(err)
	}
	writeWorkerRuntimeConfig(t, path, secondCandidate)
	awaitWorkerCondition(t, func() bool { return source.Current().Version == 2 })

	secondStarted := make(chan struct{}, 2)
	secondRelease := make(chan struct{})
	f.counts.gateMu.Lock()
	f.counts.cloneStarted = secondStarted
	f.counts.cloneRelease = secondRelease
	f.counts.gateMu.Unlock()
	close(firstRelease)
	select {
	case <-secondStarted:
	case <-time.After(8 * time.Second):
		t.Fatal("refresh for the current generation did not start after stale work")
	}
	awaitWorkerCondition(t, func() bool { return strings.Contains(logs.String(), "stale generation prevented publication") })
	firstRequests := f.requests()
	if len(firstRequests) < 3 {
		t.Fatalf("refresh requests after stale publication = %d, want initial and both configured generations", len(firstRequests))
	}
	staleJob, err := f.meta.GetJob(f.ctx, firstRequests[len(firstRequests)-2].request.Job.ID)
	if err != nil {
		t.Fatal(err)
	}
	registryAfterStale, err := f.meta.GetRegistry(f.ctx, registryID)
	if err != nil {
		t.Fatal(err)
	}
	observationAfterStale, err := f.meta.GetRegistryObservation(f.ctx, registryID)
	if err != nil {
		t.Fatal(err)
	}
	headAfterStale := latestHead(t, f)
	if staleJob.Status != "completed" || observationAfterStale.SuccessfulGeneration != initialGeneration ||
		observationAfterStale.DesiredGeneration != secondGeneration ||
		registryAfterStale.RefreshStatus != beforeRegistry.RefreshStatus ||
		registryAfterStale.RefreshError != beforeRegistry.RefreshError ||
		!headAfterStale.Equal(beforeHead) ||
		!reflect.DeepEqual(registryResourceState(t, f), beforeResources) ||
		firstGeneration == secondGeneration {
		t.Fatalf("stale refresh changed published state: job=%+v registry=%+v observation=%+v head=%+v",
			staleJob, registryAfterStale, observationAfterStale, headAfterStale)
	}

	close(secondRelease)
	f.counts.gateMu.Lock()
	f.counts.cloneStarted = nil
	f.counts.cloneRelease = nil
	f.counts.gateMu.Unlock()
	awaitWorkerCondition(t, func() bool {
		observation, err := f.meta.GetRegistryObservation(f.ctx, registryID)
		return err == nil && observation.SuccessfulGeneration == secondGeneration
	})
	awaitWorkerRefreshSettled(t, f, registryID, secondGeneration)
	if f.counts.clones.Load() != 3 {
		t.Fatalf("generation refresh acquisitions = %d, want initial, stale, and current", f.counts.clones.Load())
	}
	stop()
}

func newWorkerRuntimeSource(t *testing.T, f *observationFixture) (*config.RuntimeSource, string) {
	t.Helper()
	cfg := f.cfg
	dir := t.TempDir()
	cfg.Registries[0].Origin = "https://example.com/resources.git"
	cfg.Storage = config.Storage{
		Metadata: config.StorageMetadata{Driver: "sqlite", DSN: filepath.Join(dir, "runtime.db")},
		Blobs:    config.StorageBlobs{Driver: "filesystem", Path: filepath.Join(dir, "blobs")},
	}
	path := filepath.Join(dir, "operator.yaml")
	if err := os.WriteFile(path, marshalWorkerRuntimeConfig(t, cfg, 2), 0o600); err != nil {
		t.Fatalf("write runtime source config: %v", err)
	}
	source, err := config.LoadRuntimeSource(path)
	if err != nil {
		t.Fatalf("LoadRuntimeSource: %v", err)
	}
	f.cfg = source.Startup()
	f.deps.Config = f.cfg
	f.deps.RuntimeSource = source
	resolve := f.deps.ResolveRef
	f.deps.ResolveRef = func(ctx context.Context, _ string, ref string, credentials acquire.Credentials, timeout time.Duration) (string, error) {
		return resolve(ctx, "file://"+f.bare, ref, credentials, timeout)
	}
	clone := f.deps.Clone
	f.deps.Clone = func(ctx context.Context, _ string, ref string, credentials acquire.Credentials, limits acquire.Limits, timeout time.Duration, roots ...string) (*acquire.Result, error) {
		return clone(ctx, "file://"+f.bare, ref, credentials, limits, timeout, roots...)
	}
	return source, path
}

func startRuntimeWorker(t *testing.T, f *observationFixture, source *config.RuntimeSource) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	deps := f.deps
	deps.Config = source.Startup()
	deps.RuntimeSource = source
	done := make(chan error, 1)
	go func() { done <- Run(ctx, deps, source.Startup()) }()
	var once sync.Once
	stop := func() {
		once.Do(func() {
			cancel()
			select {
			case err := <-done:
				if err != nil && !errors.Is(err, context.Canceled) {
					t.Errorf("worker.Run: %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Error("worker.Run did not stop after cancellation")
			}
		})
	}
	t.Cleanup(stop)
	return stop
}

func cloneWorkerRuntimeConfig(t *testing.T, cfg config.Config) config.Config {
	t.Helper()
	parsed, err := config.ParseConfig(marshalWorkerRuntimeConfig(t, cfg, 2))
	if err != nil {
		t.Fatalf("clone runtime config: %v", err)
	}
	return parsed
}

func marshalWorkerRuntimeConfig(t *testing.T, cfg config.Config, indent int) []byte {
	t.Helper()
	var buffer bytes.Buffer
	encoder := yaml.NewEncoder(&buffer)
	encoder.SetIndent(indent)
	if err := encoder.Encode(cfg); err != nil {
		t.Fatalf("marshal runtime config: %v", err)
	}
	if err := encoder.Close(); err != nil {
		t.Fatalf("close runtime config encoder: %v", err)
	}
	return buffer.Bytes()
}

func writeWorkerRuntimeConfig(t *testing.T, path string, cfg config.Config) {
	t.Helper()
	writeWorkerRuntimeCandidate(t, path, marshalWorkerRuntimeConfig(t, cfg, 2))
}

func writeWorkerRuntimeCandidate(t *testing.T, path string, data []byte) {
	t.Helper()
	replacement := path + ".replacement"
	if err := os.WriteFile(replacement, data, 0o600); err != nil {
		t.Fatalf("write runtime candidate: %v", err)
	}
	if err := os.Rename(replacement, path); err != nil {
		t.Fatalf("replace runtime config: %v", err)
	}
}

func deferWorkerObservation(t *testing.T, f *observationFixture, registryID store.RegistryID) {
	t.Helper()
	deferredUntil := time.Now().Add(time.Hour)
	updateRuntimeObservation(t, f, registryID, func(observation *store.RegistryObservation) {
		observation.NextProbeAt = deferredUntil
		observation.NextReconcileAt = deferredUntil
	})
	awaitWorkerCondition(t, func() bool {
		observation, err := f.meta.GetRegistryObservation(f.ctx, registryID)
		return err == nil && observation.WorkLeaseOwner == "" &&
			observation.NextProbeAt.After(time.Now().Add(config.RuntimePollInterval)) &&
			observation.NextReconcileAt.After(time.Now().Add(config.RuntimePollInterval))
	})
}

func setRuntimeObservationDue(t *testing.T, f *observationFixture, registryID store.RegistryID) {
	t.Helper()
	updateRuntimeObservation(t, f, registryID, func(observation *store.RegistryObservation) {
		observation.NextProbeAt = time.Time{}
	})
}

func updateRuntimeObservation(t *testing.T, f *observationFixture, registryID store.RegistryID, change func(*store.RegistryObservation)) {
	t.Helper()
	awaitWorkerCondition(t, func() bool {
		current, err := f.meta.GetRegistryObservation(f.ctx, registryID)
		if err != nil || current.WorkLeaseOwner != "" {
			return false
		}
		next := current
		change(&next)
		_, err = f.meta.UpdateRegistryObservation(f.ctx, current.Version, next)
		if errors.Is(err, store.ErrConflict) {
			return false
		}
		if err != nil {
			t.Fatalf("UpdateRegistryObservation(%s): %v", registryID, err)
		}
		return true
	})
}

func awaitWorkerCondition(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.After(12 * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if condition() {
			return
		}
		select {
		case <-deadline:
			t.Fatal("worker runtime configuration condition was not reached")
		case <-ticker.C:
		}
	}
}

func awaitWorkerRefreshSettled(t *testing.T, f *observationFixture, registryID store.RegistryID, generation string) {
	t.Helper()
	awaitWorkerCondition(t, func() bool {
		observation, err := f.meta.GetRegistryObservation(f.ctx, registryID)
		if err != nil || observation.SuccessfulGeneration != generation ||
			observation.SuccessfulCommit == "" || observation.LastSuccessfulIndexAt.IsZero() {
			return false
		}
		if observation.ActiveRefreshJobID != "" {
			job, err := f.meta.GetJob(f.ctx, observation.ActiveRefreshJobID)
			if err != nil || job.Status != store.JobStatusCompleted {
				return false
			}
		}
		for _, request := range f.requests() {
			if request.decision.Job.ID == "" {
				continue
			}
			job, err := f.meta.GetJob(f.ctx, request.decision.Job.ID)
			if err != nil || job.Status != store.JobStatusCompleted {
				return false
			}
		}
		return true
	})
}

type workerLogBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *workerLogBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *workerLogBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func captureWorkerLogs(t *testing.T) *workerLogBuffer {
	t.Helper()
	buffer := &workerLogBuffer{}
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buffer, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return buffer
}
