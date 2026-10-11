package store_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

// spec: marketplace-publication/failed-publication-records-the-conflict-cause
// spec: marketplace-publication/acquisition-failure-records-a-sanitized-cause
func TestFailedRefreshAttemptRecordsCodeAndSanitizedCause(t *testing.T) {
	ctx := context.Background()
	meta, managed := managedStore(t)
	if _, err := managed.ApplyManagedConfiguration(ctx, managedRuntimeRequest(0, "adopt")); err != nil {
		t.Fatal(err)
	}
	recorder := meta.(store.ManagedRefreshAttemptStore)
	_, fence := enqueueRuntimeRefresh(t, meta, "failed-refresh", 1, 1, 1, "", 1)
	if err := recorder.RecordManagedRefreshAttempt(ctx, fence, "started", ""); err != nil {
		t.Fatal(err)
	}
	failure := store.ManagedRefreshFailure{Code: "acquisition/scheduled", Cause: "registry acquisition failed"}
	if err := recorder.RecordManagedRefreshAttempt(ctx, fence, "failed", "", failure); err != nil {
		t.Fatalf("failed attempt rejected: %v", err)
	}
	events, err := recorder.ListManagedRefreshAttempts(ctx, store.ManagedRefreshAttemptQuery{Marketplace: "managed-test", JobID: fence.JobID, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	var started, failed *store.ManagedRefreshAttempt
	for i := range events {
		switch events[i].Outcome {
		case "started":
			started = &events[i]
		case "failed":
			failed = &events[i]
		}
	}
	if started == nil || failed == nil {
		t.Fatalf("expected started and failed attempts: %+v", events)
	}
	if started.FailureCode != "" || started.FailureCause != "" {
		t.Fatalf("started attempt carried failure fields: %+v", started)
	}
	if failed.FailureCode != "acquisition/scheduled" || failed.FailureCause != "registry acquisition failed" {
		t.Fatalf("failed attempt missing code/cause: %+v", failed)
	}
	if strings.Contains(failed.FailureCause, "credential=never-expose-this") {
		t.Fatalf("failure cause leaked raw job payload: %+v", failed)
	}

	if err := recorder.RecordManagedRefreshAttempt(ctx, fence, "started", "", failure); !errors.Is(err, store.ErrManagedAttemptInvalid) {
		t.Fatalf("started with failure fields: %v", err)
	}
}

func TestManagedRefreshAttemptDecodesHistoricalRecordWithoutFailureFields(t *testing.T) {
	ctx := context.Background()
	meta, db := runtimeSQLite(t)
	record := `{"sequence":9,"marketplace_id":"managed-test","registry_id":"registry","job_id":"job-old","attempt":1,"worker_id":"w","registry_generation":1,"policy_revision":1,"configuration_revision":1,"actor":{"kind":"operator","id":"tester"},"operation":"scheduled","outcome":"failed","recorded_at":"2025-01-01T00:00:00Z"}`
	if _, err := db.Exec(`INSERT INTO managed_refresh_events (marketplace_id,sequence,registry_id,job_id,attempt,outcome,record) VALUES ('managed-test',9,'registry','job-old',1,'failed',?)`, record); err != nil {
		t.Fatal(err)
	}
	events, err := meta.ListManagedRefreshAttempts(ctx, store.ManagedRefreshAttemptQuery{Marketplace: "managed-test", Limit: 10})
	if err != nil {
		t.Fatalf("historical record rejected: %v", err)
	}
	if len(events) != 1 || events[0].FailureCode != "" || events[0].FailureCause != "" {
		t.Fatalf("historical attempt decoded wrong: %+v", events)
	}
}
