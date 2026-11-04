package worker

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/acquire"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/job"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

// spec: marketplace-source-observation/repeated-unchanged-observations-perform-no-acquisition
// spec: marketplace-source-observation/a-missing-ref-preserves-the-last-published-index
func TestObserverDoesNotAcquireUnchangedOrFailedSources(t *testing.T) {
	f := newObservationFixture(t, 1)
	if err := Reindex(f.ctx, f.deps, f.cfg, f.cfg.Registries[0].Slug); err != nil {
		t.Fatalf("initial Reindex: %v", err)
	}
	beforeRegistry, err := f.meta.GetRegistry(f.ctx, store.RegistryID(f.cfg.Registries[0].ID))
	if err != nil {
		t.Fatal(err)
	}
	beforeHead := latestHead(t, f)
	beforeResources := registryResourceState(t, f)
	beforeClones := f.counts.clones.Load()

	now := time.Now().UTC().Add(time.Minute)
	if err := newObserver(f.deps, f.cfg, func() time.Time { return now }).runCycle(f.ctx); err != nil {
		t.Fatalf("unchanged observation: %v", err)
	}
	if f.counts.clones.Load() != beforeClones {
		t.Fatalf("unchanged source acquisitions = %d, want %d", f.counts.clones.Load(), beforeClones)
	}
	observation, err := f.meta.GetRegistryObservation(f.ctx, store.RegistryID(f.cfg.Registries[0].ID))
	if err != nil {
		t.Fatal(err)
	}
	setObservationDue(t, f.meta, observation.RegistryID)
	if err := newObserver(f.deps, f.cfg, func() time.Time { return now }).runCycle(f.ctx); err != nil {
		t.Fatalf("repeated unchanged observation: %v", err)
	}
	afterHead := latestHead(t, f)
	afterResources := registryResourceState(t, f)
	afterRegistry, err := f.meta.GetRegistry(f.ctx, beforeRegistry.ID)
	if err != nil {
		t.Fatal(err)
	}
	if afterHead.Commit != beforeHead.Commit || !reflect.DeepEqual(afterResources, beforeResources) || afterRegistry.RefreshStatus != beforeRegistry.RefreshStatus || afterRegistry.RefreshError != beforeRegistry.RefreshError {
		t.Fatalf("unchanged source changed published state: before=(%s,%v,%s,%q) after=(%s,%v,%s,%q)",
			beforeHead.Commit, beforeResources, beforeRegistry.RefreshStatus, beforeRegistry.RefreshError,
			afterHead.Commit, afterResources, afterRegistry.RefreshStatus, afterRegistry.RefreshError)
	}
	if f.counts.clones.Load() != beforeClones || f.counts.probes.Load() != 2 {
		t.Fatalf("unchanged source counts = clones %d, probes %d; want %d clones and two probes", f.counts.clones.Load(), f.counts.probes.Load(), beforeClones)
	}

	setBareBranch(t, f, "main", "")
	setObservationDue(t, f.meta, observation.RegistryID)
	if err := newObserver(f.deps, f.cfg, func() time.Time { return now }).runCycle(f.ctx); err != nil {
		t.Fatalf("missing-ref observation: %v", err)
	}
	failed, err := f.meta.GetRegistryObservation(f.ctx, observation.RegistryID)
	if err != nil {
		t.Fatal(err)
	}
	if failed.ProbeStatus != store.RegistryProbeStatusFailed || failed.ConsecutiveProbeFailures != 1 {
		t.Fatalf("missing-ref observation = status %q failures %d, want failed/1", failed.ProbeStatus, failed.ConsecutiveProbeFailures)
	}
	afterFailureHead := latestHead(t, f)
	afterFailureRegistry, err := f.meta.GetRegistry(f.ctx, beforeRegistry.ID)
	if err != nil {
		t.Fatal(err)
	}
	if afterFailureHead.Commit != beforeHead.Commit || !reflect.DeepEqual(registryResourceState(t, f), beforeResources) ||
		afterFailureRegistry.RefreshStatus != beforeRegistry.RefreshStatus || afterFailureRegistry.RefreshError != beforeRegistry.RefreshError ||
		f.counts.clones.Load() != beforeClones {
		t.Fatal("missing ref changed the last published index, refresh status, or acquisition count")
	}
}

// spec: marketplace-source-observation/a-changed-commit-triggers-exactly-one-refresh
// spec: marketplace-source-observation/concurrent-triggers-coalesce-onto-one-refresh
func TestChangedCommitRequestsOneRefreshAndManualReindexCoalesces(t *testing.T) {
	f := newObservationFixture(t, 1)
	if err := Reindex(f.ctx, f.deps, f.cfg, f.cfg.Registries[0].Slug); err != nil {
		t.Fatalf("initial Reindex: %v", err)
	}
	initialClones := f.counts.clones.Load()
	advanceBareBranch(t, f, "main", "changed resource")
	now := time.Now().UTC().Add(time.Minute)
	observer := newObserver(f.deps, f.cfg, func() time.Time { return now })
	if err := observer.runCycle(f.ctx); err != nil {
		t.Fatalf("changed observation: %v", err)
	}
	observation, err := f.meta.GetRegistryObservation(f.ctx, store.RegistryID(f.cfg.Registries[0].ID))
	if err != nil {
		t.Fatal(err)
	}
	firstJob, err := f.meta.GetJob(f.ctx, observation.ActiveRefreshJobID)
	if err != nil {
		t.Fatalf("load requested refresh: %v", err)
	}
	var firstPayload refreshPayload
	if err := json.Unmarshal(firstJob.Payload, &firstPayload); err != nil {
		t.Fatalf("decode requested refresh: %v", err)
	}
	if firstPayload.Trigger != "source-change" || firstPayload.Commit != observation.ObservedCommit ||
		firstPayload.Generation == "" || firstJob.MaxAttempts != 3 {
		t.Fatalf("scheduled payload/job = %+v / %+v", firstPayload, firstJob)
	}

	setObservationDue(t, f.meta, observation.RegistryID)
	if err := observer.runCycle(f.ctx); err != nil {
		t.Fatalf("repeated changed observation: %v", err)
	}
	secondObservation, err := f.meta.GetRegistryObservation(f.ctx, observation.RegistryID)
	if err != nil {
		t.Fatal(err)
	}
	if secondObservation.ActiveRefreshJobID != firstJob.ID {
		t.Fatalf("active refresh changed from %q to %q", firstJob.ID, secondObservation.ActiveRefreshJobID)
	}
	if err := Reindex(f.ctx, f.deps, f.cfg, f.cfg.Registries[0].Slug); err != nil {
		t.Fatalf("manual Reindex over active source refresh: %v", err)
	}
	requests := f.requests()
	if len(requests) < 3 || !requests[len(requests)-1].decision.Coalesced ||
		requests[len(requests)-1].decision.Job.ID != firstJob.ID {
		t.Fatalf("manual trigger did not coalesce onto source refresh: %+v", requests)
	}
	if f.counts.clones.Load() != initialClones+1 {
		t.Fatalf("changed commit and manual coalescing acquisitions = %d, want %d", f.counts.clones.Load(), initialClones+1)
	}
	indexed, err := f.meta.GetRegistryObservation(f.ctx, observation.RegistryID)
	if err != nil {
		t.Fatal(err)
	}
	if indexed.SuccessfulCommit != observation.ObservedCommit ||
		indexed.SuccessfulGeneration != firstPayload.Generation || indexed.LastSuccessfulIndexAt.IsZero() {
		t.Fatalf("successful index identity/time not recorded: %+v", indexed)
	}
}

// spec: marketplace-source-observation/a-manual-reindex-refreshes-an-unchanged-registry-once
func TestManualReindexRequestsOneBoundedRefresh(t *testing.T) {
	f := newObservationFixture(t, 1)
	for i := 0; i < 2; i++ {
		if err := Reindex(f.ctx, f.deps, f.cfg, f.cfg.Registries[0].Slug); err != nil {
			t.Fatalf("Reindex %d: %v", i, err)
		}
	}
	requests := f.requests()
	if len(requests) != 2 {
		t.Fatalf("manual refresh requests = %d, want one per invocation", len(requests))
	}
	for _, record := range requests {
		if record.request.Trigger != "manual" || record.request.Job.MaxAttempts != 1 {
			t.Fatalf("manual request = %+v, want trigger manual and one attempt", record.request)
		}
	}
	if requests[0].request.Job.IdempotencyKey == requests[1].request.Job.IdempotencyKey {
		t.Fatalf("manual request idempotency keys are not unique: %q", requests[0].request.Job.IdempotencyKey)
	}
	if f.counts.clones.Load() != 2 {
		t.Fatalf("manual refresh acquisitions = %d, want 2", f.counts.clones.Load())
	}
}

func TestStartupConfigurationChangeRequestsRefresh(t *testing.T) {
	f := newObservationFixture(t, 1)
	if err := Reindex(f.ctx, f.deps, f.cfg, f.cfg.Registries[0].Slug); err != nil {
		t.Fatalf("initial Reindex: %v", err)
	}
	registryID := store.RegistryID(f.cfg.Registries[0].ID)
	before, err := f.meta.GetRegistryObservation(f.ctx, registryID)
	if err != nil {
		t.Fatal(err)
	}
	f.cfg.Registries[0].Publisher.Name = "Updated Publisher"
	f.deps.Config = f.cfg
	setObservationDue(t, f.meta, registryID)
	now := time.Now().UTC().Add(time.Minute)
	if err := newObserver(f.deps, f.cfg, func() time.Time { return now }).runCycle(f.ctx); err != nil {
		t.Fatalf("configuration-change observation: %v", err)
	}
	after, err := f.meta.GetRegistryObservation(f.ctx, registryID)
	if err != nil {
		t.Fatal(err)
	}
	requested, err := f.meta.GetJob(f.ctx, after.ActiveRefreshJobID)
	if err != nil {
		t.Fatalf("load config-change job: %v", err)
	}
	var payload refreshPayload
	if err := json.Unmarshal(requested.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Trigger != "config-change" || payload.Commit != before.SuccessfulCommit ||
		payload.Generation == before.SuccessfulGeneration || requested.MaxAttempts != 3 {
		t.Fatalf("config-change job = %+v / %+v, previous generation %q", requested, payload, before.SuccessfulGeneration)
	}
	runRequestedJob(t, f, requested.ID)
	if f.counts.clones.Load() != 2 {
		t.Fatalf("startup configuration change acquisitions = %d, want 2 including initial index", f.counts.clones.Load())
	}
}

// spec: marketplace-source-observation/a-due-reconciliation-refreshes-an-unchanged-registry-once
func TestDueReconciliationRefreshesAndAdvancesItsSchedule(t *testing.T) {
	f := newObservationFixture(t, 1)
	if err := Reindex(f.ctx, f.deps, f.cfg, f.cfg.Registries[0].Slug); err != nil {
		t.Fatalf("initial Reindex: %v", err)
	}
	registryID := store.RegistryID(f.cfg.Registries[0].ID)
	now := time.Now().UTC().Truncate(time.Second).Add(time.Minute)
	updateObservation(t, f.meta, registryID, func(o *store.RegistryObservation) {
		o.NextProbeAt = now.Add(-time.Minute)
		o.NextReconcileAt = now.Add(-time.Minute)
	})
	beforeClones := f.counts.clones.Load()
	if err := newObserver(f.deps, f.cfg, func() time.Time { return now }).runCycle(f.ctx); err != nil {
		t.Fatalf("due reconciliation: %v", err)
	}
	observation, err := f.meta.GetRegistryObservation(f.ctx, registryID)
	if err != nil {
		t.Fatal(err)
	}
	requested, err := f.meta.GetJob(f.ctx, observation.ActiveRefreshJobID)
	if err != nil {
		t.Fatalf("load reconciliation job: %v", err)
	}
	var payload refreshPayload
	if err := json.Unmarshal(requested.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Trigger != "reconciliation" || requested.MaxAttempts != 3 {
		t.Fatalf("reconciliation job = %+v, payload %+v", requested, payload)
	}
	if !observation.NextReconcileAt.After(now) || observation.NextReconcileAt.Sub(now) > 24*time.Hour {
		t.Fatalf("next reconciliation = %s, want a future slot within 24h", observation.NextReconcileAt)
	}
	if want := expectedPhaseSlot(registryID, now, 24*time.Hour).Truncate(time.Microsecond); !observation.NextReconcileAt.Equal(want) {
		t.Fatalf("next reconciliation = %s, want next 24h phase %s", observation.NextReconcileAt, want)
	}
	runRequestedJob(t, f, requested.ID)
	if f.counts.clones.Load() != beforeClones+1 {
		t.Fatalf("reconciliation acquisitions = %d, want one", f.counts.clones.Load()-beforeClones)
	}
}

// spec: marketplace-source-observation/stable-phases-are-deterministic-across-restarts
// spec: marketplace-source-observation/probe-failure-backoff-is-deterministic-and-capped
func TestObserverUsesStablePhasesAndCappedBackoff(t *testing.T) {
	t.Run("stable phase", func(t *testing.T) {
		f := newObservationFixture(t, 2)
		for _, src := range f.cfg.Registries {
			if err := Reindex(f.ctx, f.deps, f.cfg, src.Slug); err != nil {
				t.Fatalf("initial Reindex %s: %v", src.ID, err)
			}
			setObservationDue(t, f.meta, store.RegistryID(src.ID))
		}
		now := time.Now().UTC().Truncate(time.Minute)
		if err := newObserver(f.deps, f.cfg, func() time.Time { return now }).runCycle(f.ctx); err != nil {
			t.Fatalf("first observer cycle: %v", err)
		}
		phases := make(map[store.RegistryID]time.Duration, len(f.cfg.Registries))
		for _, src := range f.cfg.Registries {
			id := store.RegistryID(src.ID)
			got, err := f.meta.GetRegistryObservation(f.ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			want := expectedPhaseSlot(id, now, time.Duration(f.cfg.Refresh.Interval))
			if !got.NextProbeAt.Equal(want.Truncate(time.Microsecond)) {
				t.Fatalf("first next probe for %s = %s, want %s", id, got.NextProbeAt, want.Truncate(time.Microsecond))
			}
			phases[id] = time.Duration(got.NextProbeAt.UnixNano() % int64(f.cfg.Refresh.Interval))
		}
		if phases[store.RegistryID(f.cfg.Registries[0].ID)] == phases[store.RegistryID(f.cfg.Registries[1].ID)] {
			t.Fatal("fixture registry IDs unexpectedly share the same refresh phase")
		}
		for _, src := range f.cfg.Registries {
			id := store.RegistryID(src.ID)
			before, err := f.meta.GetRegistryObservation(f.ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			now = before.NextProbeAt
			if err := newObserver(f.deps, f.cfg, func() time.Time { return now }).runCycle(f.ctx); err != nil {
				t.Fatalf("restarted observer cycle for %s: %v", id, err)
			}
			after, err := f.meta.GetRegistryObservation(f.ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			if time.Duration(after.NextProbeAt.UnixNano()%int64(f.cfg.Refresh.Interval)) != phases[id] {
				t.Fatalf("phase for %s changed after observer restart: %s", id, after.NextProbeAt)
			}
		}
	})

	t.Run("backoff", func(t *testing.T) {
		f := newObservationFixture(t, 1)
		if err := Reindex(f.ctx, f.deps, f.cfg, f.cfg.Registries[0].Slug); err != nil {
			t.Fatalf("initial Reindex: %v", err)
		}
		f.cfg.Registries[0].Ref = "missing"
		f.deps.Config = f.cfg
		registryID := store.RegistryID(f.cfg.Registries[0].ID)
		now := time.Now().UTC().Truncate(time.Second)
		for failure := 1; failure <= 12; failure++ {
			if err := newObserver(f.deps, f.cfg, func() time.Time { return now }).runCycle(f.ctx); err != nil {
				t.Fatalf("failed probe %d: %v", failure, err)
			}
			got, err := f.meta.GetRegistryObservation(f.ctx, registryID)
			if err != nil {
				t.Fatal(err)
			}
			wantDelay := expectedProbeDelay(registryID, failure, time.Duration(f.cfg.Refresh.Interval), 24*time.Hour)
			wantNext := now.Add(wantDelay).Truncate(time.Microsecond)
			if got.ConsecutiveProbeFailures != failure || !got.NextProbeAt.Equal(wantNext) {
				t.Fatalf("failure %d next probe = %s failures=%d, want %s failures=%d", failure, got.NextProbeAt, got.ConsecutiveProbeFailures, wantNext, failure)
			}
			if wantDelay > 24*time.Hour {
				t.Fatalf("failure %d delay %s exceeds reconciliation cap", failure, wantDelay)
			}
			now = got.NextProbeAt
		}
		f.cfg.Registries[0].Ref = "main"
		f.deps.Config = f.cfg
		setObservationDue(t, f.meta, registryID)
		if err := newObserver(f.deps, f.cfg, func() time.Time { return now }).runCycle(f.ctx); err != nil {
			t.Fatalf("successful probe after failures: %v", err)
		}
		recovered, err := f.meta.GetRegistryObservation(f.ctx, registryID)
		if err != nil {
			t.Fatal(err)
		}
		if recovered.ConsecutiveProbeFailures != 0 || recovered.ProbeStatus != store.RegistryProbeStatusOK {
			t.Fatalf("successful probe did not reset backoff state: %+v", recovered)
		}
	})
}

// spec: marketplace-source-observation/host-budget-delay-is-visible
func TestObserverLeavesHostBudgetRefusalDueAndLogsOverdueTime(t *testing.T) {
	f := newObservationFixture(t, 1)
	if err := Reindex(f.ctx, f.deps, f.cfg, f.cfg.Registries[0].Slug); err != nil {
		t.Fatalf("initial Reindex: %v", err)
	}
	now := time.Now().UTC()
	registryID := store.RegistryID(f.cfg.Registries[0].ID)
	due := now.Add(-7 * time.Minute)
	updateObservation(t, f.meta, registryID, func(o *store.RegistryObservation) { o.NextProbeAt = due })
	first, err := f.meta.AcquireHostBudget(f.ctx, "local", "occupier-one", false, store.HostBudgetLimit{Operations: 2, Acquisitions: 1}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	second, err := f.meta.AcquireHostBudget(f.ctx, "local", "occupier-two", false, store.HostBudgetLimit{Operations: 2, Acquisitions: 1}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = f.meta.ReleaseHostBudget(f.ctx, first)
		_ = f.meta.ReleaseHostBudget(f.ctx, second)
	}()
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	defer slog.SetDefault(previous)

	beforeProbes := f.counts.probes.Load()
	if err := newObserver(f.deps, f.cfg, func() time.Time { return now }).runCycle(f.ctx); err != nil {
		t.Fatalf("budget-delayed cycle: %v", err)
	}
	got, err := f.meta.GetRegistryObservation(f.ctx, registryID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.NextProbeAt.Equal(due) || f.counts.probes.Load() != beforeProbes {
		t.Fatalf("budget refusal moved due time or probed: next=%s probes=%d", got.NextProbeAt, f.counts.probes.Load()-beforeProbes)
	}
	for _, want := range []string{"source observation delayed by host budget", string(registryID), "local", "overdue"} {
		if !strings.Contains(logs.String(), want) {
			t.Fatalf("budget-delay log %q lacks %q", logs.String(), want)
		}
	}
}

// spec: marketplace-source-observation/two-workers-observe-each-due-registry-once
// spec: marketplace-source-observation/host-operations-stay-within-the-budget-across-workers
// spec: marketplace-source-observation/observation-telemetry-contains-no-credentials
func TestObserversShareRegistryAndHostBudgetsAndRedactTelemetry(t *testing.T) {
	f := newObservationFixture(t, 2)
	const token = "worker-telemetry-token"
	t.Setenv("AM_TEST_MARKETPLACE_TOKEN", token)
	for i := range f.cfg.Registries {
		f.cfg.Registries[i].Credentials.Token = &config.TokenCredential{Env: "AM_TEST_MARKETPLACE_TOKEN"}
	}
	f.deps.Config = f.cfg
	for i := range f.cfg.Registries {
		if err := Reindex(f.ctx, f.deps, f.cfg, f.cfg.Registries[i].Slug); err != nil {
			t.Fatalf("initial Reindex %s: %v", f.cfg.Registries[i].ID, err)
		}
		observation, err := f.meta.GetRegistryObservation(f.ctx, store.RegistryID(f.cfg.Registries[i].ID))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(observation.SuccessfulGeneration, token) {
			t.Fatal("startup generation contains the resolved token")
		}
	}
	advanceBareBranch(t, f, "main", "changed source")
	for _, src := range f.cfg.Registries {
		setObservationDue(t, f.meta, store.RegistryID(src.ID))
	}
	now := time.Now().UTC().Add(time.Minute)
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	defer slog.SetDefault(previous)

	var workers sync.WaitGroup
	for i := 0; i < 2; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			if err := newObserver(f.deps, f.cfg, func() time.Time { return now }).runCycle(f.ctx); err != nil {
				t.Errorf("observer cycle: %v", err)
			}
		}()
	}
	workers.Wait()
	if got, want := f.counts.probes.Load(), int64(len(f.cfg.Registries)); got != want {
		t.Fatalf("probe count across two observers = %d, want one per registry (%d)", got, want)
	}
	if f.counts.maxProbes.Load() > 2 {
		t.Fatalf("concurrent host operations = %d, want at most 2", f.counts.maxProbes.Load())
	}
	jobIDs := make([]store.JobID, 0, len(f.cfg.Registries))
	for _, src := range f.cfg.Registries {
		observation, err := f.meta.GetRegistryObservation(f.ctx, store.RegistryID(src.ID))
		if err != nil {
			t.Fatal(err)
		}
		if observation.ActiveRefreshJobID == "" {
			t.Fatalf("registry %s has no refresh request", src.ID)
		}
		jobIDs = append(jobIDs, observation.ActiveRefreshJobID)
	}
	logOutput := logs.String()
	for _, want := range []string{"source observation", "changed", "refresh requested", "source-change"} {
		if !strings.Contains(logOutput, want) {
			t.Fatalf("telemetry %q lacks %q", logOutput, want)
		}
	}
	if strings.Contains(logOutput, token) {
		t.Fatalf("worker telemetry leaked configured token: %q", logOutput)
	}
	started, release := f.blockClones()
	defer f.releaseCloneGate()
	clonesBefore := f.counts.clones.Load()
	done := make(chan error, len(jobIDs))
	for i, id := range jobIDs {
		workerID := fmt.Sprintf("budget-worker-%d", i)
		go func(id store.JobID, workerID string) {
			leased, err := f.meta.LeaseJobByID(f.ctx, id, workerID, time.Minute)
			if err == nil {
				err = processRefreshJob(f.ctx, f.deps, leased, workerID)
			}
			done <- err
		}(id, workerID)
	}
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("no acquisition entered the clone seam")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("host-budget-refused refresh: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("second refresh did not release after host-budget refusal")
	}
	select {
	case <-started:
		t.Fatal("more than one acquisition entered the clone seam for the same host")
	case <-time.After(25 * time.Millisecond):
	}
	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("admitted refresh: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("admitted refresh did not finish after the clone gate opened")
	}
	f.releaseCloneGate()
	for _, id := range jobIDs {
		requested, err := f.meta.GetJob(f.ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if requested.Status == job.Pending {
			runRequestedJob(t, f, id)
		}
	}
	if f.counts.maxClones.Load() > 1 || f.counts.clones.Load() != clonesBefore+2 {
		t.Fatalf("host acquisition budget allowed concurrent or duplicate clones: max=%d count=%d want count %d",
			f.counts.maxClones.Load(), f.counts.clones.Load(), clonesBefore+2)
	}
	for _, src := range f.cfg.Registries {
		setObservationDue(t, f.meta, store.RegistryID(src.ID))
	}
	if err := newObserver(f.deps, f.cfg, func() time.Time { return now }).runCycle(f.ctx); err != nil {
		t.Fatalf("unchanged observations: %v", err)
	}
	f.cfg.Registries[0].Ref = "missing"
	f.deps.Config = f.cfg
	setObservationDue(t, f.meta, store.RegistryID(f.cfg.Registries[0].ID))
	if err := newObserver(f.deps, f.cfg, func() time.Time { return now }).runCycle(f.ctx); err != nil {
		t.Fatalf("failed observation: %v", err)
	}
	logOutput = logs.String()
	for _, outcome := range []string{"changed", "unchanged", "failed", "refresh requested", "source-change"} {
		if !strings.Contains(logOutput, outcome) {
			t.Fatalf("observation telemetry %q lacks %q", logOutput, outcome)
		}
	}
	if strings.Contains(logOutput, token) {
		t.Fatalf("worker telemetry leaked token after all observation outcomes: %q", logOutput)
	}
	const password = "worker-url-password"
	for _, ref := range []string{indexedCommit(t, f), strings.Repeat("b", 40), "main"} {
		f.cfg.Registries[0].Origin = "http://worker-user:" + password + "@127.0.0.1:1/unreachable.git"
		f.cfg.Registries[0].Ref = ref
		f.deps.Config = f.cfg
		setObservationDue(t, f.meta, store.RegistryID(f.cfg.Registries[0].ID))
		if err := newObserver(f.deps, f.cfg, func() time.Time { return now }).runCycle(f.ctx); err != nil {
			t.Fatalf("credential-bearing observation %q: %v", ref, err)
		}
	}
	logOutput = logs.String()
	for _, secret := range []string{token, password} {
		if strings.Contains(logOutput, secret) {
			t.Fatalf("worker telemetry leaked credential %q: %q", secret, logOutput)
		}
	}
	if !strings.Contains(logOutput, "http://***@127.0.0.1:1/unreachable.git") {
		t.Fatalf("credential-bearing origin was not visibly redacted: %q", logOutput)
	}
}

type observationFixture struct {
	ctx    context.Context
	repo   string
	bare   string
	cfg    config.Config
	deps   Deps
	meta   store.MetadataStore
	counts *observationCounts
}

type observationCounts struct {
	probes       atomic.Int64
	clones       atomic.Int64
	active       atomic.Int64
	maxProbes    atomic.Int64
	activeClones atomic.Int64
	maxClones    atomic.Int64
	requestMu    sync.Mutex
	requestLog   []refreshRequestRecord
	gateMu       sync.RWMutex
	cloneStarted chan struct{}
	cloneRelease chan struct{}
}

type refreshRequestRecord struct {
	request  store.RefreshRequest
	decision store.RefreshDecision
}

type refreshRequestRecorder struct {
	store.MetadataStore
	counts *observationCounts
}

func (s *refreshRequestRecorder) CurrentManagedConfiguration(ctx context.Context, id store.MarketplaceID) (store.ManagedChange, error) {
	current, ok := s.MetadataStore.(interface {
		CurrentManagedConfiguration(context.Context, store.MarketplaceID) (store.ManagedChange, error)
	})
	if !ok {
		return store.ManagedChange{}, store.ErrTopology
	}
	return current.CurrentManagedConfiguration(ctx, id)
}

func (s *refreshRequestRecorder) RequestRefresh(ctx context.Context, id store.RegistryID, request store.RefreshRequest) (store.RefreshDecision, error) {
	decision, err := s.MetadataStore.RequestRefresh(ctx, id, request)
	s.counts.requestMu.Lock()
	s.counts.requestLog = append(s.counts.requestLog, refreshRequestRecord{request: request, decision: decision})
	s.counts.requestMu.Unlock()
	return decision, err
}

func newObservationFixture(t *testing.T, registryCount int) *observationFixture {
	t.Helper()
	repo, cleanup := createTestRepo(t)
	t.Cleanup(cleanup)
	bare := filepath.Join(t.TempDir(), "origin.git")
	runWorkerGit(t, "", "clone", "--bare", "--quiet", repo, bare)
	cfg := createTestConfig(t, repo)
	cfg.Registries[0].Origin = "file://" + bare
	for i := 1; i < registryCount; i++ {
		src := cfg.Registries[0]
		src.ID = fmt.Sprintf("src-test-%d", i)
		src.Slug = fmt.Sprintf("example-registry-%d", i)
		cfg.Registries = append(cfg.Registries, src)
	}
	meta, blobs := openTestStores(t)
	t.Cleanup(func() { _ = meta.Close() })
	counts := &observationCounts{}
	recorder := &refreshRequestRecorder{MetadataStore: meta, counts: counts}
	deps := Deps{
		Config:        cfg,
		MetadataStore: recorder,
		BlobStore:     blobs,
		ResolveRef: func(ctx context.Context, origin, ref string, cred acquire.Credentials, timeout time.Duration) (string, error) {
			counts.probes.Add(1)
			active := counts.active.Add(1)
			defer counts.active.Add(-1)
			for peak := counts.maxProbes.Load(); active > peak && !counts.maxProbes.CompareAndSwap(peak, active); peak = counts.maxProbes.Load() {
			}
			return acquire.ResolveRemoteRef(ctx, origin, ref, cred, timeout)
		},
		Clone: func(ctx context.Context, origin, ref string, cred acquire.Credentials, limits acquire.Limits, timeout time.Duration, roots ...string) (*acquire.Result, error) {
			counts.clones.Add(1)
			active := counts.activeClones.Add(1)
			defer counts.activeClones.Add(-1)
			for peak := counts.maxClones.Load(); active > peak && !counts.maxClones.CompareAndSwap(peak, active); peak = counts.maxClones.Load() {
			}
			counts.gateMu.RLock()
			started, release := counts.cloneStarted, counts.cloneRelease
			counts.gateMu.RUnlock()
			if started != nil {
				started <- struct{}{}
				select {
				case <-release:
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}
			return acquire.Clone(ctx, origin, ref, cred, limits, timeout, roots...)
		},
	}
	if err := EnsureMarketplace(context.Background(), &deps); err != nil {
		t.Fatalf("EnsureMarketplace: %v", err)
	}
	return &observationFixture{ctx: context.Background(), repo: repo, bare: bare, cfg: cfg, deps: deps, meta: recorder, counts: counts}
}

func (f *observationFixture) requests() []refreshRequestRecord {
	f.counts.requestMu.Lock()
	defer f.counts.requestMu.Unlock()
	return append([]refreshRequestRecord(nil), f.counts.requestLog...)
}

func (f *observationFixture) blockClones() (<-chan struct{}, chan<- struct{}) {
	f.counts.gateMu.Lock()
	defer f.counts.gateMu.Unlock()
	started := make(chan struct{}, 4)
	release := make(chan struct{})
	f.counts.cloneStarted = started
	f.counts.cloneRelease = release
	return started, release
}

func (f *observationFixture) releaseCloneGate() {
	f.counts.gateMu.Lock()
	defer f.counts.gateMu.Unlock()
	if f.counts.cloneRelease != nil {
		select {
		case <-f.counts.cloneRelease:
		default:
			close(f.counts.cloneRelease)
		}
	}
	f.counts.cloneStarted = nil
	f.counts.cloneRelease = nil
}

func latestHead(t *testing.T, f *observationFixture) store.RegistryOccurrence {
	t.Helper()
	head, err := f.meta.LatestRegistryOccurrence(f.ctx, store.RegistryID(f.cfg.Registries[0].ID))
	if err != nil {
		t.Fatalf("LatestRegistryOccurrence: %v", err)
	}
	return head
}

func indexedCommit(t *testing.T, f *observationFixture) string {
	t.Helper()
	observation, err := f.meta.GetRegistryObservation(f.ctx, store.RegistryID(f.cfg.Registries[0].ID))
	if err != nil {
		t.Fatal(err)
	}
	return observation.SuccessfulCommit
}

func registryResourceState(t *testing.T, f *observationFixture) map[string][]store.AssessedResourceRevision {
	t.Helper()
	state := make(map[string][]store.AssessedResourceRevision)
	for _, resource := range []struct{ kind, name string }{
		{"skill", "review"},
		{"command", "lint"},
		{"subagent", "reviewer"},
		{"hook", "PreToolUse-command"},
		{"mcpserver", "docs"},
		{"collection", "default"},
	} {
		coord := store.ResourceCoordinate{
			Marketplace: store.MarketplaceID(f.cfg.Instance.ID),
			Registry:    store.RegistryID(f.cfg.Registries[0].ID),
			Kind:        resource.kind,
			Name:        resource.name,
		}
		revisions, err := f.meta.ListResourceRevisions(f.ctx, coord)
		if err != nil {
			t.Fatalf("ListResourceRevisions(%s): %v", coord, err)
		}
		state[resource.kind+"/"+resource.name] = revisions
	}
	return state
}

func setObservationDue(t *testing.T, meta store.MetadataStore, id store.RegistryID) {
	t.Helper()
	updateObservation(t, meta, id, func(o *store.RegistryObservation) { o.NextProbeAt = time.Time{} })
}

func updateObservation(t *testing.T, meta store.MetadataStore, id store.RegistryID, change func(*store.RegistryObservation)) {
	t.Helper()
	current, err := meta.GetRegistryObservation(context.Background(), id)
	if err != nil {
		t.Fatalf("GetRegistryObservation(%s): %v", id, err)
	}
	next := current
	change(&next)
	if _, err := meta.UpdateRegistryObservation(context.Background(), current.Version, next); err != nil {
		t.Fatalf("UpdateRegistryObservation(%s): %v", id, err)
	}
}

func advanceBareBranch(t *testing.T, f *observationFixture, ref, content string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(f.repo, "README.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	runWorkerGit(t, f.repo, "add", "README.md")
	runWorkerGit(t, f.repo, "commit", "-q", "-m", content)
	commit := strings.TrimSpace(workerGitOutput(t, f.repo, "rev-parse", "HEAD"))
	setBareBranch(t, f, ref, commit)
	return commit
}

func setBareBranch(t *testing.T, f *observationFixture, ref, commit string) {
	t.Helper()
	if commit == "" {
		runWorkerGit(t, "", "--git-dir", f.bare, "update-ref", "-d", "refs/heads/"+ref)
		return
	}
	refspec := "+refs/heads/" + ref + ":refs/heads/" + ref
	runWorkerGit(t, "", "--git-dir", f.bare, "fetch", "--quiet", f.repo, refspec)
	got := strings.TrimSpace(workerGitOutput(t, "", "--git-dir", f.bare, "rev-parse", "refs/heads/"+ref))
	if got != commit {
		t.Fatalf("updated bare ref %s = %q, want %q", ref, got, commit)
	}
}

func runRequestedJob(t *testing.T, f *observationFixture, id store.JobID) {
	t.Helper()
	workerID := "observation-test-worker"
	requested, err := f.meta.LeaseJobByID(f.ctx, id, workerID, time.Minute)
	if err != nil {
		t.Fatalf("LeaseJobByID(%s): %v", id, err)
	}
	if err := processRefreshJob(f.ctx, f.deps, requested, workerID); err != nil {
		t.Fatalf("process refresh job %s: %v", id, err)
	}
}

func expectedPhaseSlot(id store.RegistryID, now time.Time, interval time.Duration) time.Time {
	h := fnv.New64a()
	_, _ = h.Write([]byte(id))
	phase := time.Duration(h.Sum64() % uint64(interval))
	offset := time.Duration(now.UnixNano() % int64(interval))
	delta := (phase - offset + interval) % interval
	if delta == 0 {
		delta = interval
	}
	return now.Add(delta)
}

func expectedProbeDelay(id store.RegistryID, failure int, interval, cap time.Duration) time.Duration {
	d := interval
	for step := 1; step < failure && d < cap; step++ {
		if d > cap/2 {
			d = cap
		} else {
			d *= 2
		}
	}
	if d > cap {
		d = cap
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(id))
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], uint64(failure))
	_, _ = h.Write(encoded[:])
	low := d / 2
	return low + time.Duration(h.Sum64()%uint64(d-low+1))
}

func runWorkerGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}

func workerGitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
	return string(output)
}
