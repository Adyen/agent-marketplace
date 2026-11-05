package store_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/postgres"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/sqlite"
)

type observationStoreOpener func(*testing.T) store.MetadataStore

type observationConformanceCase struct {
	name string
	run  func(*testing.T, store.MetadataStore)
}

func TestObservationConformanceSQLite(t *testing.T) {
	runObservationConformance(t, openObservationSQLite)
}

func TestObservationConformancePostgres(t *testing.T) {
	if os.Getenv("POSTGRES_DSN") == "" {
		t.Skip("POSTGRES_DSN not set")
	}
	runObservationConformance(t, openObservationPostgres)
}

// spec: marketplace-registry-observation/both-backends-pass-the-same-observation-conformance-cases
func runObservationConformance(t *testing.T, open observationStoreOpener) {
	t.Helper()
	cases := []observationConformanceCase{
		{name: "observation_writes_leave_published_state_unchanged", run: testObservationWritesLeavePublishedStateUnchanged},
		{name: "held_registry_lease_is_exclusive", run: testHeldRegistryLeaseIsExclusive},
		{name: "expired_registry_lease_is_reclaimable", run: testExpiredRegistryLeaseIsReclaimable},
		{name: "stale_registry_lease_cannot_be_renewed_or_released", run: testStaleRegistryLeaseCannotBeRenewedOrReleased},
		{name: "due_observations_have_stable_order", run: testDueObservationsHaveStableOrder},
		{name: "leased_and_not_due_observations_are_excluded", run: testLeasedAndNotDueObservationsAreExcluded},
		{name: "stale_observation_update_is_atomic", run: testStaleObservationUpdateIsAtomic},
		{name: "host_budget_enforces_operation_and_acquisition_limits", run: testHostBudgetEnforcesOperationAndAcquisitionLimits},
		{name: "host_budget_reacquisition_counts_existing_admissions", run: testHostBudgetReacquisitionCountsExistingAdmissions},
		{name: "released_and_expired_host_admissions_free_capacity", run: testReleasedAndExpiredHostAdmissionsFreeCapacity},
		{name: "failed_refresh_request_rolls_back_desired_state", run: testFailedRefreshRequestRollsBackDesiredState},
		{name: "equivalent_refresh_requests_coalesce_concurrently", run: testEquivalentRefreshRequestsCoalesceConcurrently},
		{name: "terminal_refresh_failure_allows_a_new_request", run: testTerminalRefreshFailureAllowsNewRequest},
		{name: "expired_final_attempt_lease_does_not_coalesce", run: testExpiredFinalAttemptLeaseDoesNotCoalesce},
		{name: "deleting_observation_preserves_immutable_records", run: testDeletingObservationPreservesImmutableRecords},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.run(t, open(t))
		})
	}
}

func openObservationSQLite(t *testing.T) store.MetadataStore {
	t.Helper()
	meta := sqlite.New(filepath.Join(t.TempDir(), "marketplace.db"))
	if err := meta.Open(context.Background()); err != nil {
		t.Fatalf("Open SQLite metadata store: %v", err)
	}
	t.Cleanup(func() { _ = meta.Close() })
	return meta
}

func openObservationPostgres(t *testing.T) store.MetadataStore {
	t.Helper()
	ctx := context.Background()
	dsn := os.Getenv("POSTGRES_DSN")
	schema := testSchemaName(t)
	dropObservationSchema(t, ctx, dsn, schema)
	meta := postgres.NewWithSchema(dsn, schema)
	if err := meta.Open(ctx); err != nil {
		t.Fatalf("Open PostgreSQL metadata store: %v", err)
	}
	t.Cleanup(func() {
		_ = meta.Close()
		dropObservationSchema(t, ctx, dsn, schema)
	})
	return meta
}

// spec: marketplace-registry-observation/observation-writes-leave-published-registry-state-unchanged
func testObservationWritesLeavePublishedStateUnchanged(t *testing.T, meta store.MetadataStore) {
	ctx := context.Background()
	before, coordinate, contentDigest := seedObservationPublishedState(t, meta)
	created := createObservation(t, meta, store.RegistryObservation{RegistryID: "registry"})
	next := created
	next.ObservedCommit = "observed-commit"
	next.ProbeStatus = store.RegistryProbeStatusOK
	next.ProbeError = ""
	if _, err := meta.UpdateRegistryObservation(ctx, created.Version, next); err != nil {
		t.Fatalf("UpdateRegistryObservation: %v", err)
	}
	if _, err := meta.AcquireRegistryLease(ctx, "registry", "worker", time.Minute); err != nil {
		t.Fatalf("AcquireRegistryLease: %v", err)
	}
	decision, err := meta.RequestRefresh(ctx, "registry", refreshRequest("refresh-published", "desired-commit", "generation-a", "manual"))
	if err != nil || decision.Job.ID != "refresh-published" || decision.Coalesced {
		t.Fatalf("RequestRefresh = %+v, %v; want newly enqueued refresh-published", decision, err)
	}
	if err := meta.DeleteRegistryObservation(ctx, "registry"); err != nil {
		t.Fatalf("DeleteRegistryObservation: %v", err)
	}
	if _, err := meta.GetRegistryObservation(ctx, "registry"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetRegistryObservation after delete = %v, want ErrNotFound", err)
	}
	assertObservationPublishedState(t, meta, before, coordinate, contentDigest)
}

// spec: marketplace-registry-observation/a-second-claimant-is-refused-a-held-lease
func testHeldRegistryLeaseIsExclusive(t *testing.T, meta store.MetadataStore) {
	ctx := context.Background()
	createObservation(t, meta, store.RegistryObservation{RegistryID: "lease-exclusive"})
	first, err := meta.AcquireRegistryLease(ctx, "lease-exclusive", "worker-a", time.Minute)
	if err != nil {
		t.Fatalf("first AcquireRegistryLease: %v", err)
	}
	renewed, err := meta.RenewRegistryLease(ctx, "lease-exclusive", first, 2*time.Minute)
	if err != nil {
		t.Fatalf("RenewRegistryLease: %v", err)
	}
	if renewed.Owner != first.Owner || renewed.Epoch != first.Epoch+1 || !renewed.Until.After(first.Until) {
		t.Fatalf("renewed lease = %+v, want next epoch for worker-a", renewed)
	}
	if _, err := meta.AcquireRegistryLease(ctx, "lease-exclusive", "worker-b", time.Minute); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("second AcquireRegistryLease error = %v, want ErrConflict", err)
	}
	got, err := meta.GetRegistryObservation(ctx, "lease-exclusive")
	if err != nil {
		t.Fatal(err)
	}
	if got.WorkLeaseOwner != renewed.Owner || got.WorkLeaseEpoch != renewed.Epoch || !got.WorkLeaseUntil.Equal(renewed.Until) {
		t.Fatalf("held lease = %+v, want owner %q epoch %d until %s", got, renewed.Owner, renewed.Epoch, renewed.Until)
	}
}

// spec: marketplace-registry-observation/an-expired-lease-is-reclaimable
func testExpiredRegistryLeaseIsReclaimable(t *testing.T, meta store.MetadataStore) {
	ctx := context.Background()
	createObservation(t, meta, store.RegistryObservation{RegistryID: "lease-expired"})
	first, err := meta.AcquireRegistryLease(ctx, "lease-expired", "worker-a", 10*time.Millisecond)
	if err != nil {
		t.Fatalf("first AcquireRegistryLease: %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	second, err := meta.AcquireRegistryLease(ctx, "lease-expired", "worker-b", time.Minute)
	if err != nil {
		t.Fatalf("reclaim expired registry lease: %v", err)
	}
	if second.Owner != "worker-b" || second.Epoch != first.Epoch+1 || !second.Until.After(time.Now()) {
		t.Fatalf("reclaimed lease = %+v, want worker-b epoch %d and future expiry", second, first.Epoch+1)
	}
}

// spec: marketplace-registry-observation/a-stale-holder-cannot-renew-or-release-the-lease
func testStaleRegistryLeaseCannotBeRenewedOrReleased(t *testing.T, meta store.MetadataStore) {
	ctx := context.Background()
	createObservation(t, meta, store.RegistryObservation{RegistryID: "lease-stale"})
	first, err := meta.AcquireRegistryLease(ctx, "lease-stale", "worker-a", 10*time.Millisecond)
	if err != nil {
		t.Fatalf("first AcquireRegistryLease: %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	second, err := meta.AcquireRegistryLease(ctx, "lease-stale", "worker-b", time.Minute)
	if err != nil {
		t.Fatalf("reclaim expired registry lease: %v", err)
	}
	if _, err := meta.RenewRegistryLease(ctx, "lease-stale", first, time.Minute); !errors.Is(err, store.ErrLeaseExpired) {
		t.Fatalf("stale RenewRegistryLease error = %v, want ErrLeaseExpired", err)
	}
	if err := meta.ReleaseRegistryLease(ctx, "lease-stale", first); !errors.Is(err, store.ErrLeaseExpired) {
		t.Fatalf("stale ReleaseRegistryLease error = %v, want ErrLeaseExpired", err)
	}
	got, err := meta.GetRegistryObservation(ctx, "lease-stale")
	if err != nil {
		t.Fatal(err)
	}
	if got.WorkLeaseOwner != second.Owner || got.WorkLeaseEpoch != second.Epoch || !got.WorkLeaseUntil.Equal(second.Until) {
		t.Fatalf("lease after stale operations = %+v, want worker-b lease %+v", got, second)
	}
}

// spec: marketplace-registry-observation/due-observations-come-back-in-a-stable-order
func testDueObservationsHaveStableOrder(t *testing.T, meta store.MetadataStore) {
	ctx := context.Background()
	now := time.Date(2025, 3, 4, 5, 6, 7, 0, time.UTC)
	for _, observation := range []store.RegistryObservation{
		{RegistryID: "due-z", NextProbeAt: now.Add(-time.Hour), NextReconcileAt: now.Add(24 * time.Hour)},
		{RegistryID: "due-b", NextProbeAt: now, NextReconcileAt: now.Add(24 * time.Hour)},
		{RegistryID: "due-a", NextProbeAt: now.Add(time.Hour), NextReconcileAt: now},
		{RegistryID: "future", NextProbeAt: now.Add(time.Hour), NextReconcileAt: now.Add(24 * time.Hour)},
	} {
		createObservation(t, meta, observation)
	}
	want := []store.RegistryID{"due-z", "due-a", "due-b"}
	for run := 0; run < 2; run++ {
		got, err := meta.ListDueRegistryObservations(ctx, now, 10)
		if err != nil {
			t.Fatalf("ListDueRegistryObservations run %d: %v", run, err)
		}
		if ids := observationIDs(got); !reflect.DeepEqual(ids, want) {
			t.Fatalf("due observation IDs run %d = %v, want %v", run, ids, want)
		}
	}
	limited, err := meta.ListDueRegistryObservations(ctx, now, 2)
	if err != nil {
		t.Fatalf("ListDueRegistryObservations with limit: %v", err)
	}
	if ids, want := observationIDs(limited), []store.RegistryID{"due-z", "due-a"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("limited due observation IDs = %v, want %v", ids, want)
	}
}

// spec: marketplace-registry-observation/leased-and-not-yet-due-observations-are-not-selected
func testLeasedAndNotDueObservationsAreExcluded(t *testing.T, meta store.MetadataStore) {
	ctx := context.Background()
	now := time.Date(2025, 4, 5, 6, 7, 8, 0, time.UTC)
	for _, observation := range []store.RegistryObservation{
		{RegistryID: "leased", NextProbeAt: now, NextReconcileAt: now.Add(time.Hour)},
		{RegistryID: "not-due", NextProbeAt: now.Add(time.Minute), NextReconcileAt: now.Add(time.Hour)},
		{RegistryID: "available", NextProbeAt: now.Add(-time.Minute), NextReconcileAt: now.Add(time.Hour)},
	} {
		createObservation(t, meta, observation)
	}
	if _, err := meta.AcquireRegistryLease(ctx, "leased", "worker", time.Hour); err != nil {
		t.Fatalf("AcquireRegistryLease: %v", err)
	}
	got, err := meta.ListDueRegistryObservations(ctx, now, 10)
	if err != nil {
		t.Fatalf("ListDueRegistryObservations: %v", err)
	}
	if ids, want := observationIDs(got), []store.RegistryID{"available"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("due observation IDs = %v, want %v", ids, want)
	}
}

// spec: marketplace-registry-observation/a-stale-observation-update-is-rejected-without-partial-write
func testStaleObservationUpdateIsAtomic(t *testing.T, meta store.MetadataStore) {
	ctx := context.Background()
	created := createObservation(t, meta, store.RegistryObservation{
		RegistryID: "cas", ObservedCommit: "initial", NextProbeAt: time.Date(2025, 5, 6, 7, 8, 9, 0, time.UTC),
	})
	first := created
	first.ObservedCommit = "writer-one"
	first.ProbeStatus = store.RegistryProbeStatusOK
	updated, err := meta.UpdateRegistryObservation(ctx, created.Version, first)
	if err != nil {
		t.Fatalf("first UpdateRegistryObservation: %v", err)
	}
	stale := created
	stale.DesiredCommit = "writer-two"
	stale.DesiredGeneration = "stale-generation"
	if _, err := meta.UpdateRegistryObservation(ctx, created.Version, stale); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("stale UpdateRegistryObservation error = %v, want ErrConflict", err)
	}
	got, err := meta.GetRegistryObservation(ctx, created.RegistryID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, updated) || got.DesiredCommit != "" || got.DesiredGeneration != "" {
		t.Fatalf("observation after stale update = %+v, want first update %+v", got, updated)
	}
}

// spec: marketplace-registry-observation/admission-beyond-host-capacity-is-refused-across-workers
func testHostBudgetEnforcesOperationAndAcquisitionLimits(t *testing.T, meta store.MetadataStore) {
	ctx := context.Background()
	limit := store.HostBudgetLimit{Operations: 2, Acquisitions: 1}
	for _, lease := range []struct {
		host        string
		owner       string
		acquisition bool
	}{
		{host: "ops.example", owner: "worker-a", acquisition: true},
		{host: "ops.example", owner: "worker-b", acquisition: false},
	} {
		if _, err := meta.AcquireHostBudget(ctx, lease.host, lease.owner, lease.acquisition, limit, time.Minute); err != nil {
			t.Fatalf("AcquireHostBudget(%s): %v", lease.owner, err)
		}
	}
	if _, err := meta.AcquireHostBudget(ctx, "ops.example", "worker-c", false, limit, time.Minute); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("operation-capacity error = %v, want ErrConflict", err)
	}
	acquisitionLimit := store.HostBudgetLimit{Operations: 3, Acquisitions: 1}
	if _, err := meta.AcquireHostBudget(ctx, "acquisition.example", "probe-a", false, acquisitionLimit, time.Minute); err != nil {
		t.Fatalf("AcquireHostBudget(probe-a): %v", err)
	}
	if _, err := meta.AcquireHostBudget(ctx, "acquisition.example", "clone-a", true, acquisitionLimit, time.Minute); err != nil {
		t.Fatalf("AcquireHostBudget(clone-a): %v", err)
	}
	if _, err := meta.AcquireHostBudget(ctx, "acquisition.example", "clone-b", true, acquisitionLimit, time.Minute); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("acquisition-capacity error = %v, want ErrConflict", err)
	}
	if _, err := meta.AcquireHostBudget(ctx, "acquisition.example", "probe-b", false, acquisitionLimit, time.Minute); err != nil {
		t.Fatalf("rejected acquisition changed operation capacity: %v", err)
	}
}

// spec: marketplace-registry-observation/reacquisition-cannot-exceed-host-capacity
func testHostBudgetReacquisitionCountsExistingAdmissions(t *testing.T, meta store.MetadataStore) {
	ctx := context.Background()
	limit := store.HostBudgetLimit{Operations: 2, Acquisitions: 1}
	for _, owner := range []string{"worker-a", "worker-b"} {
		if _, err := meta.AcquireHostBudget(ctx, "reacquire.example", owner, false, limit, time.Minute); err != nil {
			t.Fatalf("AcquireHostBudget(%s): %v", owner, err)
		}
	}
	if _, err := meta.AcquireHostBudget(ctx, "reacquire.example", "worker-a", false, limit, time.Minute); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("reacquisition error = %v, want ErrConflict", err)
	}
}

// spec: marketplace-registry-observation/released-or-expired-admissions-free-host-capacity
func testReleasedAndExpiredHostAdmissionsFreeCapacity(t *testing.T, meta store.MetadataStore) {
	ctx := context.Background()
	limit := store.HostBudgetLimit{Operations: 1, Acquisitions: 1}
	released, err := meta.AcquireHostBudget(ctx, "release.example", "worker-a", true, limit, time.Minute)
	if err != nil {
		t.Fatalf("AcquireHostBudget(worker-a): %v", err)
	}
	if _, err := meta.AcquireHostBudget(ctx, "release.example", "worker-b", true, limit, time.Minute); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("full host budget error = %v, want ErrConflict", err)
	}
	if err := meta.ReleaseHostBudget(ctx, released); err != nil {
		t.Fatalf("ReleaseHostBudget: %v", err)
	}
	if err := meta.ReleaseHostBudget(ctx, released); !errors.Is(err, store.ErrLeaseExpired) {
		t.Fatalf("stale ReleaseHostBudget error = %v, want ErrLeaseExpired", err)
	}
	if _, err := meta.AcquireHostBudget(ctx, "release.example", "worker-b", true, limit, time.Minute); err != nil {
		t.Fatalf("admission after release: %v", err)
	}
	if _, err := meta.AcquireHostBudget(ctx, "expiry.example", "worker-a", false, limit, 10*time.Millisecond); err != nil {
		t.Fatalf("short AcquireHostBudget: %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	if _, err := meta.AcquireHostBudget(ctx, "expiry.example", "worker-b", false, limit, time.Minute); err != nil {
		t.Fatalf("admission after expiry: %v", err)
	}
}

// spec: marketplace-registry-observation/a-failed-refresh-request-changes-neither-desired-state-nor-the-active-refresh
func testFailedRefreshRequestRollsBackDesiredState(t *testing.T, meta store.MetadataStore) {
	ctx := context.Background()
	createObservation(t, meta, store.RegistryObservation{
		RegistryID: "refresh-rollback", DesiredCommit: "old-commit", DesiredGeneration: "old-generation",
	})
	first, err := meta.RequestRefresh(ctx, "refresh-rollback", refreshRequest("active-job", "old-commit", "old-generation", "manual"))
	if err != nil {
		t.Fatalf("initial RequestRefresh: %v", err)
	}
	collision := store.Job{ID: "collision-existing", Kind: "refresh", Payload: []byte(`{"existing":true}`), MaxAttempts: 2, IdempotencyKey: "collision-key"}
	if err := meta.EnqueueJob(ctx, collision); err != nil {
		t.Fatalf("EnqueueJob(collision): %v", err)
	}
	collision, err = meta.GetJob(ctx, collision.ID)
	if err != nil {
		t.Fatal(err)
	}
	before, err := meta.GetRegistryObservation(ctx, "refresh-rollback")
	if err != nil {
		t.Fatal(err)
	}
	failedRequest := refreshRequest("refresh-collision-new", "new-commit", "new-generation", "source-change")
	failedRequest.Job.IdempotencyKey = "collision-key"
	if _, err := meta.RequestRefresh(ctx, "refresh-rollback", failedRequest); err == nil {
		t.Fatal("RequestRefresh with a conflicting idempotency key succeeded")
	}
	got, err := meta.GetRegistryObservation(ctx, "refresh-rollback")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, before) || got.ActiveRefreshJobID != first.Job.ID {
		t.Fatalf("failed refresh changed observation: got %+v, want %+v", got, before)
	}
	stored, err := meta.GetJob(ctx, collision.ID)
	if err != nil || !reflect.DeepEqual(stored, collision) {
		t.Fatalf("collision job after failed request = %+v, %v; want %+v", stored, err, collision)
	}
	if _, err := meta.GetJob(ctx, failedRequest.Job.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unexpected new refresh job lookup error = %v, want ErrNotFound", err)
	}
}

// spec: marketplace-registry-observation/concurrent-equivalent-refresh-requests-coalesce-into-one-active-refresh
func testEquivalentRefreshRequestsCoalesceConcurrently(t *testing.T, meta store.MetadataStore) {
	ctx := context.Background()
	createObservation(t, meta, store.RegistryObservation{RegistryID: "refresh-concurrent"})
	const workers = 8
	type result struct {
		decision store.RefreshDecision
		err      error
	}
	results := make(chan result, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			jobID := store.JobID(fmt.Sprintf("concurrent-job-%d", i))
			decision, err := meta.RequestRefresh(ctx, "refresh-concurrent", store.RefreshRequest{
				Commit: "same-commit", Generation: "same-generation", Trigger: "manual",
				Job: store.Job{ID: jobID, Kind: "refresh", Payload: []byte(`{}`), MaxAttempts: 2},
			})
			results <- result{decision: decision, err: err}
		}(i)
	}
	wg.Wait()
	close(results)
	var jobID store.JobID
	coalesced := 0
	for result := range results {
		if result.err != nil {
			t.Fatalf("concurrent RequestRefresh: %v", result.err)
		}
		if jobID == "" {
			jobID = result.decision.Job.ID
		}
		if result.decision.Job.ID != jobID {
			t.Fatalf("coalesced job IDs differ: %q and %q", jobID, result.decision.Job.ID)
		}
		if result.decision.Coalesced {
			coalesced++
		}
	}
	if coalesced != workers-1 {
		t.Fatalf("coalesced requests = %d, want %d", coalesced, workers-1)
	}
	found := 0
	for i := 0; i < workers; i++ {
		candidate := store.JobID(fmt.Sprintf("concurrent-job-%d", i))
		if _, err := meta.GetJob(ctx, candidate); err == nil {
			found++
		} else if !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("GetJob(%s): %v", candidate, err)
		}
	}
	if found != 1 {
		t.Fatalf("enqueued equivalent refresh jobs = %d, want 1", found)
	}
	observation, err := meta.GetRegistryObservation(ctx, "refresh-concurrent")
	if err != nil {
		t.Fatal(err)
	}
	if observation.ActiveRefreshJobID != jobID {
		t.Fatalf("active refresh job = %q, want %q", observation.ActiveRefreshJobID, jobID)
	}
}

// spec: marketplace-registry-observation/a-terminal-refresh-failure-clears-the-active-refresh
func testTerminalRefreshFailureAllowsNewRequest(t *testing.T, meta store.MetadataStore) {
	ctx := context.Background()
	createObservation(t, meta, store.RegistryObservation{RegistryID: "refresh-terminal"})
	first, err := meta.RequestRefresh(ctx, "refresh-terminal", refreshRequest("terminal-job", "commit", "generation", "source-change"))
	if err != nil {
		t.Fatalf("initial RequestRefresh: %v", err)
	}
	leased, err := meta.LeaseJobByID(ctx, first.Job.ID, "worker", time.Minute)
	if err != nil {
		t.Fatalf("LeaseJobByID: %v", err)
	}
	if err := meta.FailJob(ctx, leased.ID, "worker", 0); err != nil {
		t.Fatalf("terminal FailJob: %v", err)
	}
	afterFailure, err := meta.GetRegistryObservation(ctx, "refresh-terminal")
	if err != nil {
		t.Fatal(err)
	}
	if afterFailure.ActiveRefreshJobID != "" {
		t.Fatalf("active refresh after terminal failure = %q, want empty", afterFailure.ActiveRefreshJobID)
	}
	next, err := meta.RequestRefresh(ctx, "refresh-terminal", refreshRequest("replacement-job", "commit", "generation", "manual"))
	if err != nil || next.Coalesced || next.Job.ID != "replacement-job" {
		t.Fatalf("replacement RequestRefresh = %+v, %v; want new replacement-job", next, err)
	}
}

// spec: marketplace-registry-observation/an-expired-lease-on-the-final-permitted-attempt-does-not-coalesce-a-refresh-request
func testExpiredFinalAttemptLeaseDoesNotCoalesce(t *testing.T, meta store.MetadataStore) {
	ctx := context.Background()
	createObservation(t, meta, store.RegistryObservation{RegistryID: "refresh-dead-lease"})
	first, err := meta.RequestRefresh(ctx, "refresh-dead-lease", refreshRequest("dead-lease-job", "commit", "generation", "manual"))
	if err != nil {
		t.Fatalf("initial RequestRefresh: %v", err)
	}
	leased, err := meta.LeaseJobByID(ctx, first.Job.ID, "crashed-worker", time.Minute)
	if err != nil {
		t.Fatalf("LeaseJobByID: %v", err)
	}
	live, err := meta.RequestRefresh(ctx, "refresh-dead-lease", refreshRequest("unused-live-job", "commit", "generation", "manual"))
	if err != nil || !live.Coalesced || live.Job.ID != first.Job.ID {
		t.Fatalf("live-lease RequestRefresh = %+v, %v; want coalesced onto %s", live, err, first.Job.ID)
	}
	if err := meta.ExtendLease(ctx, leased.ID, leased.WorkerID, -time.Second); err != nil {
		t.Fatalf("expire lease: %v", err)
	}
	next, err := meta.RequestRefresh(ctx, "refresh-dead-lease", refreshRequest("replacement-dead-lease-job", "commit", "generation", "manual"))
	if err != nil || next.Coalesced || next.Job.ID != "replacement-dead-lease-job" {
		t.Fatalf("expired final-attempt RequestRefresh = %+v, %v; want new replacement-dead-lease-job", next, err)
	}
}

// spec: marketplace-registry-observation/removing-an-observation-keeps-immutable-records
func testDeletingObservationPreservesImmutableRecords(t *testing.T, meta store.MetadataStore) {
	ctx := context.Background()
	before, coordinate, contentDigest := seedObservationPublishedState(t, meta)
	createObservation(t, meta, store.RegistryObservation{RegistryID: "registry"})
	if err := meta.DeleteRegistryObservation(ctx, "registry"); err != nil {
		t.Fatalf("DeleteRegistryObservation: %v", err)
	}
	if _, err := meta.GetRegistryObservation(ctx, "registry"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetRegistryObservation after delete = %v, want ErrNotFound", err)
	}
	assertObservationPublishedState(t, meta, before, coordinate, contentDigest)
}

type observationPublishedState struct {
	registry        store.Registry
	occurrence      store.RegistryOccurrence
	latest          store.RegistryOccurrence
	history         []store.RegistryOccurrence
	resource        store.AssessedResourceRevision
	assessments     []store.ResourceAssessment
	resourceHistory []store.AssessedResourceRevision
}

func seedObservationPublishedState(t *testing.T, meta store.MetadataStore) (observationPublishedState, store.ResourceCoordinate, store.RevisionDigest) {
	t.Helper()
	ctx := context.Background()
	if err := meta.PutMarketplace(ctx, store.Marketplace{ID: "managed-test", Name: "Published", CanonicalURL: "https://marketplace.example"}); err != nil {
		t.Fatal(err)
	}
	registry := store.Registry{ID: "registry", Slug: "published", Ref: "main", Acquisition: store.AcquisitionIndexOnly, Listed: true}
	if err := meta.PutRegistry(ctx, registry); err != nil {
		t.Fatal(err)
	}
	occurrence := store.RegistryOccurrence{
		RegistryID: registry.ID, Ref: "main", Roots: []store.RegistryRoot{{Path: "skills"}},
		Commit: "published-commit", RegistryRevisionManifestDigest: "published-manifest",
		Acquisition: store.AcquisitionIndexOnly,
	}
	if err := meta.PutRegistryOccurrence(ctx, occurrence); err != nil {
		t.Fatal(err)
	}
	coordinate := store.ResourceCoordinate{Marketplace: "managed-test", Registry: registry.ID, Kind: "skill", Name: "published"}
	contentDigest := store.RevisionDigest("sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	revision := store.ResourceRevision{Coordinate: coordinate, ContentDigest: contentDigest, License: "MIT"}
	if err := meta.PutResourceRevision(ctx, revision); err != nil {
		t.Fatal(err)
	}
	resourceOccurrence := store.ResourceOccurrence{
		Coordinate: coordinate, ContentDigest: contentDigest, Commit: occurrence.Commit,
		ManifestDigest: occurrence.RegistryRevisionManifestDigest, SourcePath: "skills/published/SKILL.md",
	}
	if err := meta.PutResourceOccurrence(ctx, resourceOccurrence); err != nil {
		t.Fatal(err)
	}
	assessment, err := store.FinalizeResourceAssessment(store.ResourceAssessment{
		Coordinate: coordinate, ContentDigest: contentDigest, AnalyzerVersion: "observation-test/v1",
		PolicyDigest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Decision: store.AssessmentDecisionAllow,
		InspectionManifestDigest: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc", AssessedAt: time.Date(2025, 6, 7, 8, 9, 10, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := meta.PutResourceAssessment(ctx, assessment); err != nil {
		t.Fatal(err)
	}
	if err := meta.SetCurrentResourceAssessment(ctx, assessment); err != nil {
		t.Fatal(err)
	}
	registry, err = meta.GetRegistry(ctx, registry.ID)
	if err != nil {
		t.Fatal(err)
	}
	occurrence, err = meta.GetRegistryOccurrence(ctx, occurrence.RegistryID, occurrence.Commit)
	if err != nil {
		t.Fatal(err)
	}
	latest, err := meta.LatestRegistryOccurrence(ctx, registry.ID)
	if err != nil {
		t.Fatal(err)
	}
	history, err := meta.RegistryHistory(ctx, registry.ID)
	if err != nil {
		t.Fatal(err)
	}
	resource, err := meta.GetResourceRevision(ctx, coordinate, contentDigest)
	if err != nil {
		t.Fatal(err)
	}
	assessments, err := meta.ListResourceAssessments(ctx, coordinate, contentDigest)
	if err != nil {
		t.Fatal(err)
	}
	resourceHistory, err := meta.ListResourceRevisions(ctx, coordinate)
	if err != nil {
		t.Fatal(err)
	}
	return observationPublishedState{
		registry: registry, occurrence: occurrence, latest: latest, history: history,
		resource: resource, assessments: assessments, resourceHistory: resourceHistory,
	}, coordinate, contentDigest
}

func assertObservationPublishedState(t *testing.T, meta store.MetadataStore, before observationPublishedState, coordinate store.ResourceCoordinate, contentDigest store.RevisionDigest) {
	t.Helper()
	ctx := context.Background()
	registry, err := meta.GetRegistry(ctx, before.registry.ID)
	if err != nil {
		t.Fatal(err)
	}
	occurrence, err := meta.GetRegistryOccurrence(ctx, before.occurrence.RegistryID, before.occurrence.Commit)
	if err != nil {
		t.Fatal(err)
	}
	latest, err := meta.LatestRegistryOccurrence(ctx, before.registry.ID)
	if err != nil {
		t.Fatal(err)
	}
	history, err := meta.RegistryHistory(ctx, before.registry.ID)
	if err != nil {
		t.Fatal(err)
	}
	resource, err := meta.GetResourceRevision(ctx, coordinate, contentDigest)
	if err != nil {
		t.Fatal(err)
	}
	assessments, err := meta.ListResourceAssessments(ctx, coordinate, contentDigest)
	if err != nil {
		t.Fatal(err)
	}
	resourceHistory, err := meta.ListResourceRevisions(ctx, coordinate)
	if err != nil {
		t.Fatal(err)
	}
	after := observationPublishedState{
		registry: registry, occurrence: occurrence, latest: latest, history: history,
		resource: resource, assessments: assessments, resourceHistory: resourceHistory,
	}
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("observation state changed published metadata: before=%+v after=%+v", before, after)
	}
}

func createObservation(t *testing.T, meta store.MetadataStore, observation store.RegistryObservation) store.RegistryObservation {
	t.Helper()
	created, err := meta.CreateRegistryObservation(context.Background(), observation)
	if err != nil {
		t.Fatalf("CreateRegistryObservation(%s): %v", observation.RegistryID, err)
	}
	return created
}

func refreshRequest(jobID, commit, generation, trigger string) store.RefreshRequest {
	return store.RefreshRequest{
		Commit: commit, Generation: generation, Trigger: trigger,
		Job: store.Job{ID: store.JobID(jobID), Kind: "refresh", Payload: []byte(`{}`), MaxAttempts: 1},
	}
}

func observationIDs(observations []store.RegistryObservation) []store.RegistryID {
	ids := make([]store.RegistryID, len(observations))
	for i, observation := range observations {
		ids[i] = observation.RegistryID
	}
	return ids
}
