package worker

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/acquire"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/admin"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func TestManagedPreflightRecordsSupersededWithoutStarted(t *testing.T) {
	ctx := context.Background()
	deps, service := managedFixture(t)
	attempts := deps.MetadataStore.(store.ManagedRefreshAttemptStore)
	if err := scheduleManaged(ctx, deps, "prior"); err != nil {
		t.Fatal(err)
	}
	prior, err := deps.MetadataStore.LeaseJob(ctx, "managed-refresh", "prior", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := processManagedRefresh(ctx, deps, prior, "prior", func(context.Context, string, string, acquire.Credentials, acquire.Limits, time.Duration, ...string) (*acquire.Result, error) {
		return nil, ErrManagedRefresh
	}); !errors.Is(err, ErrManagedRefresh) {
		t.Fatal(err)
	}
	before, err := attempts.GetManagedRegistryRefreshState(ctx, "test-mp", "src-test")
	if err != nil {
		t.Fatal(err)
	}
	if err := scheduleManaged(ctx, deps, "stale"); err != nil {
		t.Fatal(err)
	}
	j, err := deps.MetadataStore.LeaseJob(ctx, "managed-refresh", "worker", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Apply(ctx, "test-mp", admin.Mutation{Action: "pause", RegistryID: "src-test", ExpectedRevision: 1, Actor: store.ManagedActor{Issuer: "test", Subject: "operator"}, Reason: "pause", RequestID: "pause", IdempotencyKey: "pause"})
	if err != nil {
		t.Fatal(err)
	}
	err = processManagedRefresh(ctx, deps, j, "worker", func(context.Context, string, string, acquire.Credentials, acquire.Limits, time.Duration, ...string) (*acquire.Result, error) {
		t.Fatal("stale job acquired")
		return nil, nil
	})
	if !errors.Is(err, ErrManagedSuperseded) {
		t.Fatalf("preflight: %v", err)
	}
	events, err := attempts.ListManagedRefreshAttempts(ctx, store.ManagedRefreshAttemptQuery{Marketplace: "test-mp", JobID: j.ID, Limit: 10})
	if err != nil || len(events) != 1 || events[0].Outcome != "superseded" {
		t.Fatalf("events: %+v %v", events, err)
	}
	after, err := attempts.GetManagedRegistryRefreshState(ctx, "test-mp", "src-test")
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("obsolete attempt changed aggregate: %+v %+v %v", before, after, err)
	}
	terminal, err := attempts.GetManagedRefreshJob(ctx, "test-mp", j.ID)
	if err != nil || !terminal.Terminal {
		t.Fatalf("job not terminal: %+v %v", terminal, err)
	}
}

func TestManagedManualReindexAuditAttribution(t *testing.T) {
	ctx := context.Background()
	deps, _ := managedFixture(t)
	deps.ReindexActor = store.ManagedActor{Issuer: "test-cli", Subject: "operator-123"}
	view, err := managedView(ctx, deps)
	if err != nil {
		t.Fatal(err)
	}
	id, err := enqueueManagedReindex(ctx, deps, view, view.Snapshot.Registries[0])
	if err != nil {
		t.Fatal(err)
	}
	j, err := deps.MetadataStore.LeaseJobByID(ctx, id, "worker", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	err = processManagedRefresh(ctx, deps, j, "worker", func(context.Context, string, string, acquire.Credentials, acquire.Limits, time.Duration, ...string) (*acquire.Result, error) {
		return nil, errors.New("private acquisition diagnostic")
	})
	if !errors.Is(err, ErrManagedRefresh) {
		t.Fatalf("refresh result: %v", err)
	}
	events, err := deps.MetadataStore.(store.ManagedRefreshAttemptStore).ListManagedRefreshAttempts(ctx, store.ManagedRefreshAttemptQuery{Marketplace: "test-mp", JobID: id, Limit: 10})
	if err != nil || len(events) != 2 {
		t.Fatalf("attempts: %+v %v", events, err)
	}
	event := events[0]
	if event.Actor != deps.ReindexActor || event.ChangeOperation != "reindex" || event.Operation != "reconcile" || event.ParentJobID == "" || event.RequestID == "" || event.ConfigurationRevision != 2 || event.Outcome != "failed" {
		t.Fatalf("missing audited attribution: %+v", event)
	}
	parent, err := deps.MetadataStore.GetJob(ctx, event.ParentJobID)
	if err != nil || parent.Status != store.JobStatusCompleted {
		t.Fatalf("manual parent left pending: %+v %v", parent, err)
	}
	if _, err := deps.MetadataStore.LeaseJob(ctx, "managed-refresh", "unexpected", time.Minute); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("parent duplicated child: %v", err)
	}
}
