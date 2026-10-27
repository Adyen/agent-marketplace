package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func staticRegistryIndex(registries map[store.RegistryID]store.Registry) func(context.Context) (map[store.RegistryID]store.Registry, error) {
	return func(context.Context) (map[store.RegistryID]store.Registry, error) { return registries, nil }
}

func blockedHandler(release <-chan struct{}, respond func(http.ResponseWriter, *http.Request)) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
			respond(w, r)
		case <-r.Context().Done():
		}
	}
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// spec: marketplace-api/metrics-configuration-and-requests-preserve-the-server-boundary
func TestInstallationLookupServesPublishedSnapshot(t *testing.T) {
	fixture := newMetricsFixture(t, func() string { return prometheusVector(adoptionSamples(), adoptionFixedNow) })
	reader := newTestReader(t, fixture)
	reader.now = func() time.Time { return adoptionFixedNow }
	registries := adoptionTestRegistries()

	if _, ok := reader.lookup(registries); ok {
		t.Fatal("lookup before any refresh returned counts")
	}
	if _, err := reader.refresh(context.Background(), registries); err != nil {
		t.Fatalf("refresh() error = %v", err)
	}
	stop := make(chan struct{})
	var pubWG sync.WaitGroup
	pubWG.Add(1)
	go func() {
		defer pubWG.Done()
		for done := false; !done; {
			select {
			case <-stop:
				done = true
			default:
				_, _ = reader.refresh(context.Background(), registries)
			}
		}
	}()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 25; j++ {
				counts, ok := reader.lookup(registries)
				if !ok || len(counts.Counts) != 1 || counts.Counts[adoptionKey("rA", "skill", "res-a")] != 1 {
					t.Errorf("concurrent lookup = %+v, %v", counts, ok)
					return
				}
			}
		}()
	}
	wg.Wait()
	close(stop)
	pubWG.Wait()
	counts, ok := reader.lookup(registries)
	if !ok || len(counts.Counts) != 1 || counts.Counts[adoptionKey("rA", "skill", "res-a")] != 1 {
		t.Fatalf("published snapshot changed under concurrent refresh: %+v, %v", counts, ok)
	}
	if hits := fixture.hits.Load(); hits < 1 {
		t.Fatalf("metrics endpoint hits = %d, want at least 1 real refresh", hits)
	}
}

// spec: marketplace-api/fresh-last-good-counts-survive-a-failed-proactive-refresh
func TestInstallationFailedRefreshRetainsLastGoodUntilMaxStale(t *testing.T) {
	fixture := newMetricsFixture(t, func() string { return prometheusVector(adoptionSamples(), adoptionFixedNow) })
	reader := newTestReader(t, fixture)
	now := adoptionFixedNow
	reader.now = func() time.Time { return now }
	registries := adoptionTestRegistries()

	if _, err := reader.refresh(context.Background(), registries); err != nil {
		t.Fatalf("refresh() error = %v", err)
	}
	fixture.handler.Store(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	now = now.Add(4 * time.Minute)
	if _, err := reader.refresh(context.Background(), registries); err == nil {
		t.Fatal("refresh() against failing endpoint = nil, want unavailable")
	}
	counts, fresh := reader.lookup(registries)
	if counts == nil || !fresh || counts.Counts[adoptionKey("rA", "skill", "res-a")] != 1 {
		t.Fatalf("fresh last-good not retained after failed refresh: %+v, %v", counts, fresh)
	}
	now = adoptionFixedNow.Add(reader.timing.MaxAge)
	if counts, fresh := reader.lookup(registries); counts == nil || fresh {
		t.Fatalf("expired last-good not served as stale within max_stale: %+v, %v", counts, fresh)
	}
	now = adoptionFixedNow.Add(reader.timing.MaxStale - time.Nanosecond)
	if counts, _ := reader.lookup(registries); counts == nil {
		t.Fatal("last-good dropped before max_stale")
	}
	now = adoptionFixedNow.Add(reader.timing.MaxStale)
	if counts, _ := reader.lookup(registries); counts != nil {
		t.Fatal("stale last-good still served at max_stale")
	}
}

// spec: marketplace-api/registry-identity-and-inventory-expiry-prevent-unsafe-counts
func TestInstallationStaleSnapshotNeverServesChangedIdentity(t *testing.T) {
	fixture := newMetricsFixture(t, func() string { return prometheusVector(adoptionSamples(), adoptionFixedNow) })
	reader := newTestReader(t, fixture)
	now := adoptionFixedNow
	reader.now = func() time.Time { return now }
	registries := adoptionTestRegistries()
	if _, err := reader.refresh(context.Background(), registries); err != nil {
		t.Fatalf("refresh() error = %v", err)
	}
	now = adoptionFixedNow.Add(reader.timing.MaxAge + time.Minute)
	changed := adoptionTestRegistries()
	rA := changed["rA"]
	rA.Origins = []string{"https://example.test/team/moved-alpha.git"}
	changed["rA"] = rA
	if counts, _ := reader.lookup(changed); counts != nil {
		t.Fatal("stale snapshot served counts under a changed registry identity")
	}
	if counts, fresh := reader.lookup(registries); counts == nil || fresh {
		t.Fatalf("stale snapshot not served under its own identity: %+v, %v", counts, fresh)
	}
}

// spec: marketplace-api/fresh-last-good-counts-survive-a-failed-proactive-refresh
func TestInstallationSuccessfulRefreshReplacesStaleSnapshot(t *testing.T) {
	fixture := newMetricsFixture(t, func() string { return prometheusVector(adoptionSamples(), adoptionFixedNow) })
	reader := newTestReader(t, fixture)
	now := adoptionFixedNow
	reader.now = func() time.Time { return now }
	registries := adoptionTestRegistries()
	if _, err := reader.refresh(context.Background(), registries); err != nil {
		t.Fatalf("refresh() error = %v", err)
	}
	now = adoptionFixedNow.Add(reader.timing.MaxAge + time.Minute)
	if _, fresh := reader.lookup(registries); fresh {
		t.Fatal("snapshot still fresh past max_age")
	}
	if _, err := reader.refresh(context.Background(), registries); err != nil {
		t.Fatalf("refresh() after expiry error = %v", err)
	}
	if counts, fresh := reader.lookup(registries); counts == nil || !fresh {
		t.Fatalf("successful refresh did not replace the stale snapshot: %+v, %v", counts, fresh)
	}
}

// spec: marketplace-api/registry-identity-and-inventory-expiry-prevent-unsafe-counts
func TestInstallationRefreshRejectsExpiredCandidate(t *testing.T) {
	fixture := newMetricsFixture(t, func() string { return prometheusVector(adoptionSamples(), adoptionFixedNow) })
	reader := newTestReader(t, fixture)
	calls := &atomic.Int32{}
	reader.now = func() time.Time {
		if calls.Add(1) == 1 {
			return adoptionFixedNow
		}
		return adoptionFixedNow.Add(10 * time.Minute)
	}
	if _, err := reader.refresh(context.Background(), adoptionTestRegistries()); err == nil {
		t.Fatal("refresh() published a candidate already expired at publication")
	}
	if _, ok := reader.lookup(adoptionTestRegistries()); ok {
		t.Fatal("expired candidate was published to the snapshot")
	}
}

// spec: marketplace-api/background-scheduling-coalesces-hints-and-respects-backoff
func TestInstallationHintSkipsFreshCompatibleSnapshot(t *testing.T) {
	fixture := newMetricsFixture(t, func() string { return prometheusVector(adoptionSamples(), adoptionFixedNow) })
	reader := newTestReader(t, fixture)
	now := adoptionFixedNow
	reader.now = func() time.Time { return now }
	registries := adoptionTestRegistries()
	index := staticRegistryIndex(registries)

	if _, err := reader.refresh(context.Background(), registries); err != nil {
		t.Fatalf("refresh() error = %v", err)
	}
	if delay := reader.attempt(context.Background(), index, true); delay <= 0 {
		t.Fatalf("hint on fresh compatible snapshot delay = %v, want a positive reschedule", delay)
	}
	if hits := fixture.hits.Load(); hits != adoptionRefreshRequests {
		t.Fatalf("hint repeated work for a still-fresh compatible snapshot: hits = %d, want 1", hits)
	}

	reader.nextWake = adoptionFixedNow.Add(90 * time.Second)
	if delay := reader.attempt(context.Background(), index, true); delay != 90*time.Second {
		t.Fatalf("redundant hint delay = %v, want the preserved nextWake remainder", delay)
	}
	if !reader.nextWake.Equal(adoptionFixedNow.Add(90 * time.Second)) {
		t.Fatalf("redundant hint rewrote nextWake to %v", reader.nextWake)
	}
	if hits := fixture.hits.Load(); hits != adoptionRefreshRequests {
		t.Fatalf("redundant hint repeated fresh work: hits = %d, want 1", hits)
	}

	expired := reader.snapshot.Load().expiresAt
	index = func(context.Context) (map[store.RegistryID]store.Registry, error) {
		now = expired.Add(time.Second)
		return registries, nil
	}
	reader.attempt(context.Background(), index, true)
	if hits := fixture.hits.Load(); hits != 2*adoptionRefreshRequests {
		t.Fatalf("hint skipped a snapshot that expired during metadata load: hits = %d, want 2", hits)
	}
}

// spec: marketplace-api/background-scheduling-coalesces-hints-and-respects-backoff
// spec: marketplace-api/searches-remain-responsive-during-refresh
func TestInstallationRunSerialAttemptsAndCoalescedHints(t *testing.T) {
	release := make(chan struct{})
	var inFlight, maxInFlight atomic.Int32
	fixture := newMetricsFixture(t, func() string { return prometheusVector(adoptionSamples(), time.Now()) })
	fixture.handler.Store(func(w http.ResponseWriter, r *http.Request) {
		current := inFlight.Add(1)
		defer inFlight.Add(-1)
		for {
			if old := maxInFlight.Load(); current <= old || maxInFlight.CompareAndSwap(old, current) {
				break
			}
		}
		blockedHandler(release, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(operandVector(prometheusVector(adoptionSamples(), time.Now()), r.Form.Get("query"))))
		})(w, r)
	})
	reader := newTestReader(t, fixture)
	reader.timing.Interval = time.Hour
	reader.timing.RetryInterval = 50 * time.Millisecond
	reader.timing.Jitter = 0

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		reader.run(ctx, staticRegistryIndex(adoptionTestRegistries()))
		close(done)
	}()
	var releaseOnce sync.Once
	t.Cleanup(func() {
		cancel()
		releaseOnce.Do(func() { close(release) })
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("worker did not join after cancellation")
		}
	})

	waitFor(t, "first refresh attempt", func() bool { return fixture.hits.Load() == adoptionRefreshRequests })
	for i := 0; i < 5; i++ {
		reader.hint()
	}
	releaseOnce.Do(func() { close(release) })
	waitFor(t, "published snapshot", func() bool {
		_, ok := reader.lookup(adoptionTestRegistries())
		return ok
	})
	if got := maxInFlight.Load(); got != adoptionRefreshRequests {
		t.Fatalf("concurrent in-flight requests = %d, want one attempt of %d operands", got, adoptionRefreshRequests)
	}
	time.Sleep(150 * time.Millisecond)
	if hits := fixture.hits.Load(); hits != adoptionRefreshRequests {
		t.Fatalf("coalesced hints repeated fresh work: hits = %d, want 1", hits)
	}
}

// spec: marketplace-api/background-scheduling-coalesces-hints-and-respects-backoff
func TestInstallationRunRetriesAfterFailureWithoutTraffic(t *testing.T) {
	fixture := newMetricsFixture(t, func() string { return prometheusVector(adoptionSamples(), time.Now()) })
	fixture.handler.Store(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	})
	reader := newTestReader(t, fixture)
	reader.timing.Interval = time.Hour
	reader.timing.RetryInterval = 40 * time.Millisecond
	reader.timing.Jitter = 0

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		reader.run(ctx, staticRegistryIndex(adoptionTestRegistries()))
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("worker did not join after cancellation")
		}
	})

	waitFor(t, "a retry after failure backoff", func() bool { return fixture.hits.Load() >= 2 })
}

// spec: marketplace-api/registry-identity-and-inventory-expiry-prevent-unsafe-counts
func TestInstallationRunEarlierInventoryHorizon(t *testing.T) {
	almostStale := time.Now().Add(-30*24*time.Hour + 60*time.Millisecond)
	samples := []inventoryMetricSample{
		aggregateResponseSample("count", 1, map[string]string{"am_registry": "rA", "resource_kind": "skill", "resource_name": "res-a"}),
		aggregateResponseSample("fresh", 1, nil),
		aggregateResponseSample("valid_until", float64(almostStale.UnixMicro()), nil),
	}
	body := prometheusVector(samples, time.Now())
	fixture := newMetricsFixture(t, func() string { return body })
	reader := newTestReader(t, fixture)
	reader.timing.Interval = time.Hour
	reader.timing.RetryInterval = 80 * time.Millisecond
	reader.timing.Jitter = 0

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		reader.run(ctx, staticRegistryIndex(adoptionTestRegistries()))
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("worker did not join after cancellation")
		}
	})

	waitFor(t, "refresh at the inventory horizon", func() bool { return fixture.hits.Load() >= 2 })
	before := fixture.hits.Load()
	time.Sleep(400 * time.Millisecond)
	if hits := fixture.hits.Load(); hits-before > 10*adoptionRefreshRequests {
		t.Fatalf("expired inventory caused a busy retry loop: %d extra hits in 400ms", hits-before)
	}
}

// spec: marketplace-api/registry-identity-and-inventory-expiry-prevent-unsafe-counts
func TestInstallationIdentityChangeDuringFetch(t *testing.T) {
	release := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	fixture := newMetricsFixture(t, func() string { return prometheusVector(adoptionSamples(), time.Now()) })
	fixture.handler.Store(blockedHandler(release, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(operandVector(prometheusVector(adoptionSamples(), time.Now()), r.Form.Get("query"))))
	}))
	reader := newTestReader(t, fixture)
	reader.timing.Interval = time.Hour
	reader.timing.RetryInterval = time.Hour
	reader.timing.Jitter = 0

	current := &atomic.Value{}
	registries := adoptionTestRegistries()
	current.Store(registries)
	index := func(context.Context) (map[store.RegistryID]store.Registry, error) {
		return current.Load().(map[store.RegistryID]store.Registry), nil
	}
	attemptDone := make(chan time.Duration, 1)
	go func() { attemptDone <- reader.attempt(context.Background(), index, false) }()

	waitFor(t, "fetch in flight", func() bool { return fixture.hits.Load() == adoptionRefreshRequests })
	changed := adoptionTestRegistries()
	rA := changed["rA"]
	rA.Origins = []string{"https://example.test/team/moved-alpha.git"}
	changed["rA"] = rA
	current.Store(changed)
	releaseOnce.Do(func() { close(release) })
	select {
	case <-attemptDone:
	case <-time.After(5 * time.Second):
		t.Fatal("attempt did not finish after the fetch was released")
	}

	if _, ok := reader.lookup(changed); ok {
		t.Fatal("registry change during fetch leaked counts under the new signature")
	}
	if _, ok := reader.lookup(registries); !ok {
		t.Fatal("published snapshot lost its own signature")
	}
	reader.attempt(context.Background(), index, false)
	if _, ok := reader.lookup(changed); !ok {
		t.Fatal("refresh did not recover against current registry metadata")
	}
	if hits := fixture.hits.Load(); hits != 2*adoptionRefreshRequests {
		t.Fatalf("hits = %d, want %d", hits, 2*adoptionRefreshRequests)
	}
}

type slogCapture struct {
	mu      sync.Mutex
	records []string
}

func (h *slogCapture) Enabled(context.Context, slog.Level) bool { return true }
func (h *slogCapture) Handle(_ context.Context, record slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	text := record.Message
	record.Attrs(func(attr slog.Attr) bool {
		text += " " + attr.Value.String()
		return true
	})
	h.records = append(h.records, text)
	return nil
}
func (h *slogCapture) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *slogCapture) WithGroup(string) slog.Handler      { return h }

// spec: marketplace-api/refresh-health-distinguishes-attempts-from-search-fallback
func TestInstallationRefreshMetricsAndAttemptLogging(t *testing.T) {
	metrics := newServerMetrics(nil)
	capture := &slogCapture{}
	previous := slog.Default()
	slog.SetDefault(slog.New(capture))
	t.Cleanup(func() { slog.SetDefault(previous) })

	fixture := newMetricsFixture(t, func() string { return prometheusVector(adoptionSamples(), adoptionFixedNow) })
	reader := newTestReader(t, fixture)
	reader.now = func() time.Time { return adoptionFixedNow }
	reader.metrics = metrics
	index := staticRegistryIndex(adoptionTestRegistries())
	t.Setenv("MARKETPLACE_METRICS_TEST_PRIVACY", "private-token")
	reader.tokenEnv = "MARKETPLACE_METRICS_TEST_PRIVACY"

	reader.attempt(context.Background(), index, false)
	fixture.handler.Store(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	})
	reader.attempt(context.Background(), index, false)

	if _, ok := reader.lookup(adoptionTestRegistries()); !ok {
		t.Fatal("fresh last-good snapshot dropped after a failed refresh")
	}
	reader.hint()

	recorder := httptest.NewRecorder()
	metrics.scrape.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := recorder.Body.String()
	for _, want := range []string{
		`agent_marketplace_installation_metrics_refresh_total{outcome="success"} 1`,
		`agent_marketplace_installation_metrics_refresh_total{outcome="unavailable"} 1`,
		`agent_marketplace_installation_metrics_refresh_duration_seconds_count 2`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("operations metrics missing %q:\n%s", want, body)
		}
	}
	if !strings.Contains(body, "agent_marketplace_installation_metrics_last_success_timestamp_seconds") {
		t.Fatalf("operations metrics missing last-success timestamp:\n%s", body)
	}
	for _, leaked := range []string{"install-1", "private-token"} {
		if strings.Contains(body, leaked) {
			t.Fatalf("operations metrics leaked %q", leaked)
		}
	}
	if len(capture.records) != 1 || !strings.Contains(capture.records[0], "unavailable") {
		t.Fatalf("per-attempt failure log = %v, want one unavailable record", capture.records)
	}
	if strings.Contains(capture.records[0], "install-1") || strings.Contains(capture.records[0], "private-token") {
		t.Fatalf("failure log leaked sensitive data: %q", capture.records[0])
	}
}

// spec: marketplace-api/background-scheduling-coalesces-hints-and-respects-backoff
// spec: marketplace-api/refresh-health-distinguishes-attempts-from-search-fallback
func TestInstallationAttemptSchedulesFromEvaluationTime(t *testing.T) {
	metrics := newServerMetrics(nil)
	fixture := newMetricsFixture(t, func() string { return prometheusVector(adoptionSamples(), adoptionFixedNow) })
	fixture.handler.Store(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(operandVector(prometheusVector(adoptionSamples(), adoptionFixedNow), r.Form.Get("query"))))
	})
	reader := newTestReader(t, fixture)
	reader.metrics = metrics
	reader.timing.Interval = 4 * time.Minute
	reader.timing.Jitter = 0
	registries := adoptionTestRegistries()
	base := adoptionFixedNow
	var now atomic.Int64
	now.Store(base.UnixNano())
	reader.now = func() time.Time { return time.Unix(0, now.Load()) }
	index := func(context.Context) (map[store.RegistryID]store.Registry, error) {
		now.Store(base.Add(time.Second).UnixNano())
		return registries, nil
	}
	fixture.handler.Store(func(w http.ResponseWriter, r *http.Request) {
		now.Store(base.Add(2 * time.Second).UnixNano())
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(operandVector(prometheusVector(adoptionSamples(), adoptionFixedNow), r.Form.Get("query"))))
	})

	delay := reader.attempt(context.Background(), index, false)
	wantWake := base.Add(time.Second).Add(reader.timing.Interval)
	if !reader.nextWake.Equal(wantWake) {
		t.Fatalf("nextWake = %v, want evaluation time + interval = %v", reader.nextWake, wantWake)
	}
	if want := wantWake.Sub(base.Add(2 * time.Second)); delay != want {
		t.Fatalf("attempt delay = %v, want %v", delay, want)
	}
	if got := testutil.ToFloat64(metrics.installationLastSuccess); got != float64(base.Add(time.Second).Unix()) {
		t.Fatalf("last-success gauge = %v, want evaluation timestamp %v", got, base.Add(time.Second).Unix())
	}

	reader.timing.Jitter = 30 * time.Second
	now.Store(base.UnixNano())
	reader.attempt(context.Background(), index, false)
	low := base.Add(time.Second).Add(reader.timing.Interval)
	high := low.Add(reader.timing.Jitter)
	if reader.nextWake.Before(low) || reader.nextWake.After(high) {
		t.Fatalf("jittered nextWake = %v, want within [%v, %v]", reader.nextWake, low, high)
	}
}

// spec: marketplace-api/timing-overrides-and-explicit-zero-jitter-are-honored
func TestInstallationTimeoutOverrideBoundsBackendRequest(t *testing.T) {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	fixture := newMetricsFixture(t, func() string { return prometheusVector(adoptionSamples(), adoptionFixedNow) })
	fixture.handler.Store(blockedHandler(release, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	timeout := config.Duration(100 * time.Millisecond)
	reader := newInstallationMetricsReader(config.InstallationMetrics{URL: fixture.server.URL, Timeout: &timeout}, "mp")
	reader.now = func() time.Time { return adoptionFixedNow }
	if reader.client.Timeout != 100*time.Millisecond {
		t.Fatalf("reader client timeout = %v, want 100ms", reader.client.Timeout)
	}

	start := time.Now()
	if _, err := reader.refresh(context.Background(), adoptionTestRegistries()); err == nil {
		t.Fatal("refresh() against a blocked backend returned nil error")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("refresh() exceeded the configured timeout: %v", elapsed)
	}
	if hits := fixture.hits.Load(); hits != adoptionRefreshRequests {
		t.Fatalf("metrics endpoint hits = %d, want %d", hits, adoptionRefreshRequests)
	}
}

// spec: marketplace-api/searches-remain-responsive-during-refresh
func TestSearchInstallationsServesWhileWorkerTelemetryBlocked(t *testing.T) {
	fallbackOrder := []string{"alpha", "preview", "beta", "preview-extra", "zulu", "local-only"}
	rankedOrder := []string{"preview-extra", "beta", "zulu", "preview", "alpha", "local-only"}
	wantCounts := map[string]int64{"preview-extra": 9, "beta": 3, "zulu": 3, "preview": 2, "alpha": 1}
	tests := []struct {
		name      string
		seed      bool
		expire    bool
		wantOrder []string
	}{
		{name: "cold", wantOrder: fallbackOrder},
		{name: "warm", seed: true, wantOrder: rankedOrder},
		{name: "expired", seed: true, expire: true, wantOrder: rankedOrder},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			public, fixture, server, _ := adoptionSearchSetup(t, adoptionSearchSamples())
			var now atomic.Int64
			now.Store(adoptionFixedNow.UnixNano())
			server.installations.now = func() time.Time { return time.Unix(0, now.Load()) }
			server.installations.timing.Timeout = time.Minute
			server.installations.client.Timeout = time.Minute
			registries, err := server.registryIndex(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if test.seed {
				if _, err := server.installations.refresh(context.Background(), registries); err != nil {
					t.Fatalf("seed refresh() error = %v", err)
				}
			}
			if test.expire {
				now.Store(adoptionFixedNow.Add(server.installations.timing.MaxAge + time.Minute).UnixNano())
			}

			backendDone, release, backendEntered := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var backendOnce, releaseOnce, enteredOnce sync.Once
			fixture.handler.Store(func(w http.ResponseWriter, r *http.Request) {
				enteredOnce.Do(func() { close(backendEntered) })
				select {
				case <-release:
				case <-r.Context().Done():
					backendOnce.Do(func() { close(backendDone) })
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(operandVector(prometheusVector(adoptionSearchSamples(), adoptionFixedNow), r.Form.Get("query"))))
			})
			releaseFunc := func() { releaseOnce.Do(func() { close(release) }) }
			t.Cleanup(releaseFunc)

			hitsBefore := fixture.hits.Load()
			previousSnapshot := server.installations.snapshot.Load()
			workerCtx, cancelWorker := context.WithCancel(context.Background())
			workerDone := make(chan struct{})
			go func() {
				server.installations.run(workerCtx, server.registryIndex)
				close(workerDone)
			}()
			t.Cleanup(func() {
				cancelWorker()
				releaseFunc()
				select {
				case <-workerDone:
				case <-time.After(5 * time.Second):
					t.Error("worker did not join after cancellation")
				}
			})

			waitFor(t, "worker to issue a telemetry request", func() bool { return fixture.hits.Load() > hitsBefore })
			select {
			case <-backendEntered:
			case <-time.After(5 * time.Second):
				t.Fatal("worker telemetry request did not reach the blocked backend")
			}

			doSearch := func(ctx context.Context) (adoptionSearchResult, error) {
				var result adoptionSearchResult
				req, err := http.NewRequestWithContext(ctx, http.MethodGet, public.URL+"/api/v1/search?sort=installations", nil)
				if err != nil {
					return result, err
				}
				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					return result, err
				}
				defer func() { _ = resp.Body.Close() }()
				raw, _ := io.ReadAll(resp.Body)
				if resp.StatusCode != http.StatusOK {
					return result, errors.New("search status " + resp.Status)
				}
				return result, json.Unmarshal(raw, &result)
			}

			type searchOutcome struct {
				result adoptionSearchResult
				err    error
			}
			results := make(chan searchOutcome, 3)
			for i := 0; i < 3; i++ {
				go func() {
					ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
					defer cancel()
					result, err := doSearch(ctx)
					results <- searchOutcome{result, err}
				}()
			}
			for i := 0; i < 3; i++ {
				select {
				case got := <-results:
					if got.err != nil {
						t.Fatalf("concurrent search failed: %v", got.err)
					}
					if !slices.Equal(searchNames(got.result), test.wantOrder) {
						t.Fatalf("search order under blocked refresh = %v, want %v", searchNames(got.result), test.wantOrder)
					}
					wantCounted := test.seed
					for _, item := range got.result.Items {
						want, known := wantCounts[item.Name]
						got := int64(-1)
						if item.InstallationCount != nil {
							got = *item.InstallationCount
						}
						if !wantCounted || !known {
							want = -1
						}
						if got != want {
							t.Fatalf("installation_count[%q] = %d, want %d", item.Name, got, want)
						}
					}
				case <-time.After(5 * time.Second):
					t.Fatal("search blocked behind the in-flight telemetry refresh")
				}
			}

			searchCtx, cancelSearch := context.WithCancel(context.Background())
			if _, err := doSearch(searchCtx); err != nil {
				t.Fatalf("search for cancellation proof failed: %v", err)
			}
			cancelSearch()
			select {
			case <-backendDone:
				t.Fatal("search cancellation propagated to the telemetry request")
			case <-time.After(200 * time.Millisecond):
			}

			releaseFunc()
			waitFor(t, "worker to publish a usable snapshot", func() bool {
				if server.installations.snapshot.Load() == previousSnapshot {
					return false
				}
				_, ok := server.installations.lookup(registries)
				return ok
			})
			result, err := doSearch(context.Background())
			if err != nil || !slices.Equal(searchNames(result), rankedOrder) {
				t.Fatalf("post-release search = %v, err = %v; want %v", searchNames(result), err, rankedOrder)
			}
		})
	}
}
