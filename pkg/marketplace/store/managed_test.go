package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/postgres"
)

func managedStore(t *testing.T) (store.MetadataStore, store.ManagedConfigurationStore) {
	t.Helper()
	var meta store.MetadataStore
	if dsn := os.Getenv("POSTGRES_DSN"); dsn != "" {
		pg := postgres.NewWithSchema(dsn, fmt.Sprintf("managed_%d", time.Now().UnixNano()))
		if err := pg.Open(context.Background()); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = pg.Close() })
		meta = pg
	} else {
		meta, _ = openStores(t)
	}
	if err := meta.PutMarketplace(context.Background(), store.Marketplace{ID: "managed-test", Name: "Managed", CanonicalURL: "https://marketplace.test"}); err != nil {
		t.Fatal(err)
	}
	managed, ok := meta.(store.ManagedConfigurationStore)
	if !ok {
		t.Fatal("metadata adapter does not implement transactional managed configuration")
	}
	return meta, managed
}

func managedRequest(parent int64, key string) store.ManagedChangeRequest {
	return store.ManagedChangeRequest{Change: store.ManagedChange{
		Marketplace: "managed-test", ParentRevision: parent,
		Actor:     store.ManagedActor{Issuer: "test-proxy", Subject: "operator"},
		Operation: "configuration.apply", Reason: "Update approved catalog settings",
		RequestID: "request-" + key, IdempotencyKey: key, RequestDigest: "digest-" + key,
		Snapshot: json.RawMessage(`{"version":1,"policy_revision":1,"registries":[],"license_policy":{"allowlist":["MIT"],"undeclared":"reject"}}`),
	}, Jobs: []store.Job{{ID: store.JobID("job-" + key), Kind: "managed-reconcile", Payload: []byte(`{}`), IdempotencyKey: "work-" + key}}}
}

func TestManagedConfigurationAtomicChangeAndRetry(t *testing.T) {
	ctx := context.Background()
	meta, managed := managedStore(t)
	if _, err := managed.CurrentManagedConfiguration(ctx, "managed-test"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("file-mode database implicitly adopted: %v", err)
	}
	req := managedRequest(0, "adopt")
	accepted, err := managed.ApplyManagedConfiguration(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if accepted.Revision != 1 || accepted.ParentRevision != 0 || accepted.CreatedAt.IsZero() || !reflect.DeepEqual(accepted.JobIDs, []store.JobID{"job-adopt"}) {
		t.Fatalf("invalid accepted change: %+v", accepted)
	}
	job, err := meta.GetJob(ctx, "job-adopt")
	if err != nil || job.Status != store.JobStatusPending {
		t.Fatalf("accepted change lost work: %+v, %v", job, err)
	}
	second, err := managed.ApplyManagedConfiguration(ctx, managedRequest(1, "second"))
	if err != nil || second.Revision != 2 {
		t.Fatalf("second change: %+v, %v", second, err)
	}
	retry, err := managed.ApplyManagedConfiguration(ctx, req)
	if err != nil || !reflect.DeepEqual(retry, accepted) {
		t.Fatalf("identical retry must precede stale-version rejection: %+v, %v", retry, err)
	}
	req.Change.RequestDigest = "different-body"
	if _, err := managed.ApplyManagedConfiguration(ctx, req); !errors.Is(err, store.ErrManagedIdempotencyConflict) {
		t.Fatalf("changed retry: %v", err)
	}
	if _, err := managed.ApplyManagedConfiguration(ctx, managedRequest(1, "stale")); !errors.Is(err, store.ErrManagedRevisionConflict) {
		t.Fatalf("stale write: %v", err)
	}
	if _, err := meta.GetJob(ctx, "job-stale"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("stale write queued work: %v", err)
	}
	history, err := managed.ManagedConfigurationHistory(ctx, "managed-test", 0, 1)
	if err != nil || len(history) != 1 || history[0].Revision != 2 {
		t.Fatalf("bounded newest-first history: %+v, %v", history, err)
	}
	history, err = managed.ManagedConfigurationHistory(ctx, "managed-test", 2, 10)
	if err != nil || len(history) != 1 || !reflect.DeepEqual(history[0], accepted) {
		t.Fatalf("history cursor: %+v, %v", history, err)
	}
}

func TestManagedConfigurationWorkFailureRollsBackAdoption(t *testing.T) {
	ctx := context.Background()
	meta, managed := managedStore(t)
	req := managedRequest(0, "adopt")
	if err := meta.EnqueueJob(ctx, req.Jobs[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := managed.ApplyManagedConfiguration(ctx, req); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("work collision must reject adoption: %v", err)
	}
	if _, err := managed.CurrentManagedConfiguration(ctx, "managed-test"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("failed adoption retained authority marker: %v", err)
	}
	history, err := managed.ManagedConfigurationHistory(ctx, "managed-test", 0, 10)
	if err != nil || len(history) != 0 {
		t.Fatalf("failed adoption retained audit record: %+v, %v", history, err)
	}
	req.Jobs[0].ID = "replacement-job"
	req.Jobs[0].IdempotencyKey = "replacement-work"
	if _, err := managed.ApplyManagedConfiguration(ctx, req); err != nil {
		t.Fatalf("rolled back idempotency key cannot be retried: %v", err)
	}
}

func TestManagedConfigurationOuterTransactionRollback(t *testing.T) {
	ctx := context.Background()
	meta, managed := managedStore(t)
	aborted := errors.New("abort enclosing operation")
	err := meta.WithTx(ctx, func(ctx context.Context, tx store.MetadataStore) error {
		m, ok := tx.(store.ManagedConfigurationStore)
		if !ok {
			return store.ErrTopology
		}
		if _, err := m.ApplyManagedConfiguration(ctx, managedRequest(0, "nested")); err != nil {
			return err
		}
		return aborted
	})
	if !errors.Is(err, aborted) {
		t.Fatal(err)
	}
	if _, err := managed.CurrentManagedConfiguration(ctx, "managed-test"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("enclosing rollback retained managed authority: %v", err)
	}
	if _, err := meta.GetJob(ctx, "job-nested"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("enclosing rollback retained work: %v", err)
	}
}

func TestManagedConfigurationCompetingEditsHaveOneWinner(t *testing.T) {
	ctx := context.Background()
	meta, managed := managedStore(t)
	if _, err := managed.ApplyManagedConfiguration(ctx, managedRequest(0, "adopt")); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, key := range []string{"first", "second"} {
		wg.Go(func() {
			_, err := managed.ApplyManagedConfiguration(ctx, managedRequest(1, key))
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	wins, conflicts := 0, 0
	for err := range errs {
		switch {
		case err == nil:
			wins++
		case errors.Is(err, store.ErrManagedRevisionConflict):
			conflicts++
		default:
			t.Fatalf("unexpected competing write error: %v", err)
		}
	}
	if wins != 1 || conflicts != 1 {
		t.Fatalf("wins=%d conflicts=%d", wins, conflicts)
	}
	current, err := managed.CurrentManagedConfiguration(ctx, "managed-test")
	if err != nil || current.Revision != 2 {
		t.Fatalf("current: %+v, %v", current, err)
	}
	jobs := 0
	for _, id := range []store.JobID{"job-first", "job-second"} {
		if _, err := meta.GetJob(ctx, id); err == nil {
			jobs++
		} else if !errors.Is(err, store.ErrNotFound) {
			t.Fatal(err)
		}
	}
	if jobs != 1 {
		t.Fatalf("competing edits queued %d jobs", jobs)
	}
}
