package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func TestObservationStoreLeasesAndDueSelection(t *testing.T) {
	ctx := context.Background()
	meta, _ := openStores(t)
	base := time.Unix(1000, 0).UTC()
	for _, item := range []store.RegistryObservation{
		{RegistryID: "due-b", NextProbeAt: base, NextReconcileAt: base.Add(time.Hour)},
		{RegistryID: "not-due", NextProbeAt: base.Add(time.Hour), NextReconcileAt: base.Add(time.Hour)},
		{RegistryID: "due-a", NextProbeAt: base.Add(-time.Minute), NextReconcileAt: base.Add(time.Hour)},
	} {
		if err := meta.PutRegistryObservation(ctx, item); err != nil {
			t.Fatalf("PutRegistryObservation(%s): %v", item.RegistryID, err)
		}
	}
	got, err := meta.ListDueRegistryObservations(ctx, base, 10)
	if err != nil {
		t.Fatalf("ListDueRegistryObservations: %v", err)
	}
	if len(got) != 2 || got[0].RegistryID != "due-a" || got[1].RegistryID != "due-b" {
		t.Fatalf("due observations = %#v, want due-a then due-b", got)
	}
	if _, err := meta.AcquireRegistryLease(ctx, "due-a", "worker-a", time.Minute); err != nil {
		t.Fatalf("AcquireRegistryLease: %v", err)
	}
	if _, err := meta.AcquireRegistryLease(ctx, "due-a", "worker-b", time.Minute); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("second registry lease error = %v, want conflict", err)
	}
}

func TestObservationExpiredLeaseAndCompareAndSwap(t *testing.T) {
	ctx := context.Background()
	meta, _ := openStores(t)
	if err := meta.PutRegistryObservation(ctx, store.RegistryObservation{RegistryID: "lease", NextProbeAt: time.Unix(1, 0).UTC()}); err != nil {
		t.Fatal(err)
	}
	before, err := meta.GetRegistryObservation(ctx, "lease")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := meta.AcquireRegistryLease(ctx, "lease", "worker-a", time.Millisecond); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	if _, err := meta.AcquireRegistryLease(ctx, "lease", "worker-b", time.Minute); err != nil {
		t.Fatalf("expired lease reclaim: %v", err)
	}
	stale := before
	stale.WorkLeaseOwner = ""
	stale.WorkLeaseUntil = time.Time{}
	if err := meta.CompareAndSwapRegistryObservation(ctx, "lease", before.ObservationVersion, stale); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("stale observation CAS error = %v, want conflict", err)
	}
	current, err := meta.GetRegistryObservation(ctx, "lease")
	if err != nil {
		t.Fatal(err)
	}
	if current.WorkLeaseOwner != "worker-b" {
		t.Fatalf("stale CAS erased active lease owner %q", current.WorkLeaseOwner)
	}
}

func TestObservationFencingRejectsInterleavedDesiredStateWriter(t *testing.T) {
	ctx := context.Background()
	meta, _ := openStores(t)
	if err := meta.PutRegistryObservation(ctx, store.RegistryObservation{RegistryID: "fenced"}); err != nil {
		t.Fatal(err)
	}
	stale, err := meta.GetRegistryObservation(ctx, "fenced")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := meta.AcquireRegistryLease(ctx, "fenced", "worker", time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := meta.SetDesiredRefresh(ctx, "fenced", store.DesiredRefresh{Commit: "commit", Generation: "generation", JobID: "job", Trigger: "source-change"}); err != nil {
		t.Fatal(err)
	}
	stale.ObservedCommit = "stale-commit"
	if err := meta.CompareAndSwapRegistryObservation(ctx, "fenced", stale.ObservationVersion, stale); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("interleaved stale CAS error = %v, want conflict", err)
	}
	current, err := meta.GetRegistryObservation(ctx, "fenced")
	if err != nil {
		t.Fatal(err)
	}
	if current.WorkLeaseOwner != "worker" || current.DesiredCommit != "commit" {
		t.Fatalf("stale writer changed current observation: %#v", current)
	}
}

func TestRegistryLeaseRenewalAndReleaseErrors(t *testing.T) {
	ctx := context.Background()
	meta, _ := openStores(t)
	if err := meta.PutRegistryObservation(ctx, store.RegistryObservation{RegistryID: "release"}); err != nil {
		t.Fatal(err)
	}
	first, err := meta.AcquireRegistryLease(ctx, "release", "owner-a", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	renewed, err := meta.AcquireRegistryLease(ctx, "release", "owner-a", 2*time.Minute)
	if err != nil {
		t.Fatalf("same-owner renewal: %v", err)
	}
	if renewed.WorkLeaseUntil.Before(first.WorkLeaseUntil) || renewed.ObservationVersion <= first.ObservationVersion {
		t.Fatalf("renewal did not advance lease state: first=%#v renewed=%#v", first, renewed)
	}
	if err := meta.ReleaseRegistryLease(ctx, "release", "owner-b"); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("wrong-owner release error = %v, want conflict", err)
	}
	if err := meta.ReleaseRegistryLease(ctx, "missing", "owner-a"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing release error = %v, want not found", err)
	}
	if err := meta.ReleaseRegistryLease(ctx, "release", "owner-a"); err != nil {
		t.Fatalf("owner release: %v", err)
	}
	if err := meta.ReleaseRegistryLease(ctx, "release", "owner-a"); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("released lease error = %v, want conflict", err)
	}
	if _, err := meta.AcquireRegistryLease(ctx, "release", "owner-a", time.Millisecond); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	if err := meta.ReleaseRegistryLease(ctx, "release", "owner-a"); !errors.Is(err, store.ErrLeaseExpired) {
		t.Fatalf("expired lease error = %v, want lease expired", err)
	}
}

func TestExpiredMaxAttemptJobBecomesTerminal(t *testing.T) {
	ctx := context.Background()
	meta, _ := openStores(t)
	if err := meta.EnqueueJob(ctx, store.Job{ID: "terminal-job", Kind: "refresh", MaxAttempts: 1, Payload: []byte("payload")}); err != nil {
		t.Fatal(err)
	}
	if _, err := meta.LeaseJob(ctx, "refresh", "worker-a", time.Millisecond); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	if _, err := meta.LeaseJob(ctx, "refresh", "worker-b", time.Minute); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("releasing expired max-attempt job returned %v, want not found", err)
	}
	job, err := meta.GetJob(ctx, "terminal-job")
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != store.JobStatusTerminalFailed || job.Attempt != 1 || job.WorkerID != "" || !job.LeaseUntil.IsZero() {
		t.Fatalf("terminal job state = %#v", job)
	}
}

func TestEnqueueNilPayloadIsEmptyAcrossBackends(t *testing.T) {
	ctx := context.Background()
	meta, _ := openStores(t)
	if err := meta.EnqueueJob(ctx, store.Job{ID: "empty-payload", Kind: "refresh", Payload: nil}); err != nil {
		t.Fatal(err)
	}
	job, err := meta.GetJob(ctx, "empty-payload")
	if err != nil {
		t.Fatal(err)
	}
	if job.Payload == nil || len(job.Payload) != 0 {
		t.Fatalf("nil payload round-tripped as %#v, want non-nil empty bytes", job.Payload)
	}
}

func TestObservationDesiredRefreshCoalescesAndClearsActiveJob(t *testing.T) {
	ctx := context.Background()
	meta, _ := openStores(t)
	if err := meta.PutRegistryObservation(ctx, store.RegistryObservation{RegistryID: "refresh"}); err != nil {
		t.Fatal(err)
	}
	if err := meta.EnqueueJob(ctx, store.Job{ID: "job-1", Kind: "refresh", Payload: []byte("payload")}); err != nil {
		t.Fatal(err)
	}
	first, err := meta.SetDesiredRefresh(ctx, store.RegistryID("refresh"), store.DesiredRefresh{Commit: "commit-1", Generation: "generation-1", JobID: "job-1", Trigger: "source-change"})
	if err != nil {
		t.Fatalf("SetDesiredRefresh: %v", err)
	}
	if first.ActiveRefreshJobID != "job-1" {
		t.Fatalf("active job = %q, want job-1", first.ActiveRefreshJobID)
	}
	second, err := meta.SetDesiredRefresh(ctx, store.RegistryID("refresh"), store.DesiredRefresh{Commit: "commit-1", Generation: "generation-1", JobID: "job-2", Trigger: "manual"})
	if err != nil {
		t.Fatalf("coalescing SetDesiredRefresh: %v", err)
	}
	if second.ActiveRefreshJobID != "job-1" {
		t.Fatalf("equivalent refresh replaced active job with %q", second.ActiveRefreshJobID)
	}
	cleared, err := meta.ClearActiveRefresh(ctx, store.RegistryID("refresh"), store.JobID("job-1"))
	if err != nil {
		t.Fatalf("ClearActiveRefresh: %v", err)
	}
	if cleared.ActiveRefreshJobID != "" {
		t.Fatalf("terminal cleanup left active job %q", cleared.ActiveRefreshJobID)
	}
	again, err := meta.SetDesiredRefresh(ctx, store.RegistryID("refresh"), store.DesiredRefresh{Commit: "commit-1", Generation: "generation-1", JobID: "job-3", Trigger: "manual"})
	if err != nil {
		t.Fatalf("later SetDesiredRefresh: %v", err)
	}
	if again.ActiveRefreshJobID != "job-3" {
		t.Fatalf("later attempt did not create active job: %q", again.ActiveRefreshJobID)
	}
}

func TestObservationCompareAndSwapMissingObservationReturnsNotFound(t *testing.T) {
	ctx := context.Background()
	meta, _ := openStores(t)
	if err := meta.CompareAndSwapRegistryObservation(ctx, "missing", 1, store.RegistryObservation{ObservedCommit: "commit"}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing observation CAS error = %v, want ErrNotFound", err)
	}
}

func TestObservationDesiredRefreshRecoversMissingOrTerminalActiveJob(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name   string
		status store.JobStatus
	}{
		{name: "missing", status: ""},
		{name: "completed", status: store.JobStatusCompleted},
		{name: "terminal failed", status: store.JobStatusTerminalFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			meta, _ := openStores(t)
			registryID := store.RegistryID("refresh-" + tc.name)
			if err := meta.PutRegistryObservation(ctx, store.RegistryObservation{RegistryID: registryID}); err != nil {
				t.Fatal(err)
			}
			activeJobID := store.JobID("active-" + tc.name)
			if tc.status != "" {
				if err := meta.EnqueueJob(ctx, store.Job{ID: activeJobID, Kind: "refresh", Status: tc.status, Payload: []byte("payload")}); err != nil {
					t.Fatal(err)
				}
			}
			first, err := meta.SetDesiredRefresh(ctx, registryID, store.DesiredRefresh{Commit: "commit", Generation: "generation", JobID: activeJobID, Trigger: "source-change"})
			if err != nil {
				t.Fatal(err)
			}
			if first.ActiveRefreshJobID != activeJobID {
				t.Fatalf("initial active job = %q, want %q", first.ActiveRefreshJobID, activeJobID)
			}
			replacementJobID := store.JobID("replacement-" + tc.name)
			second, err := meta.SetDesiredRefresh(ctx, registryID, store.DesiredRefresh{Commit: "commit", Generation: "generation", JobID: replacementJobID, Trigger: "manual"})
			if err != nil {
				t.Fatal(err)
			}
			if second.ActiveRefreshJobID != store.JobID("replacement-"+tc.name) {
				t.Fatalf("replacement active job = %q, want replacement job", second.ActiveRefreshJobID)
			}
		})
	}
}

func TestObservationHostBudgetIsDeploymentWide(t *testing.T) {
	ctx := context.Background()
	meta, _ := openStores(t)
	limit := store.HostBudgetLimit{MaxOperations: 2, MaxAcquisitions: 1}
	if _, err := meta.AcquireHostBudget(ctx, "git.example", "probe-1", false, limit, time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := meta.AcquireHostBudget(ctx, "git.example", "clone-1", true, limit, time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := meta.AcquireHostBudget(ctx, "git.example", "clone-2", true, limit, time.Minute); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("host budget admission = %v, want conflict", err)
	}
	if err := meta.ReleaseHostBudget(ctx, "git.example", "clone-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := meta.AcquireHostBudget(ctx, "git.example", "clone-2", true, limit, time.Minute); err != nil {
		t.Fatalf("host budget after release: %v", err)
	}
}
