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
	if _, err := meta.AcquireRegistryLease(ctx, "lease", "worker-a", time.Millisecond); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	if _, err := meta.AcquireRegistryLease(ctx, "lease", "worker-b", time.Minute); err != nil {
		t.Fatalf("expired lease reclaim: %v", err)
	}
	current, err := meta.GetRegistryObservation(ctx, "lease")
	if err != nil {
		t.Fatal(err)
	}
	stale := current
	stale.ObservedAt = current.ObservedAt.Add(-time.Second)
	if err := meta.CompareAndSwapRegistryObservation(ctx, "lease", current.ObservedAt.Add(-time.Second), stale); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("stale observation CAS error = %v, want conflict", err)
	}
}

func TestObservationDesiredRefreshCoalescesAndCleansTerminalJob(t *testing.T) {
	ctx := context.Background()
	meta, _ := openStores(t)
	if err := meta.PutRegistryObservation(ctx, store.RegistryObservation{RegistryID: "refresh"}); err != nil {
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
