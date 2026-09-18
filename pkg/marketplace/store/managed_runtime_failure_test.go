package store_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/fsblob"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/sqlite"
)

func runtimeSQLite(t *testing.T) (*sqlite.Store, *sql.DB) {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "runtime.db")
	meta := sqlite.New(path)
	if err := meta.Open(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = meta.Close() })
	if err := meta.PutMarketplace(ctx, store.Marketplace{ID: "managed-test", Name: "Runtime", CanonicalURL: "https://example.test"}); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return meta, db
}

func TestManagedProjectionAuditFailureRollsBackIdentityAndWork(t *testing.T) {
	ctx := context.Background()
	meta, db := runtimeSQLite(t)
	if _, err := db.Exec(`CREATE TRIGGER reject_projection_audit BEFORE INSERT ON managed_configuration_changes BEGIN SELECT RAISE(ABORT,'audit unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	request := managedRuntimeRequest(0, "adopt")
	if _, err := meta.ApplyManagedConfiguration(ctx, request); err == nil {
		t.Fatal("configuration accepted without audit")
	}
	if _, err := meta.GetRegistry(ctx, "registry"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("audit failure retained registry: %v", err)
	}
	if _, err := meta.GetJob(ctx, "job-adopt"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("audit failure retained work: %v", err)
	}
	if _, err := meta.CurrentManagedConfiguration(ctx, "managed-test"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("audit failure retained config: %v", err)
	}
	for _, table := range []string{"managed_registry_identities", "managed_registry_names"} {
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("audit failure retained %s reservations: %d %v", table, count, err)
		}
	}
	if _, err := db.Exec(`DROP TRIGGER reject_projection_audit`); err != nil {
		t.Fatal(err)
	}
	if _, err := meta.ApplyManagedConfiguration(ctx, request); err != nil {
		t.Fatalf("valid retry after audit recovery: %v", err)
	}
	if registry, err := meta.GetRegistry(ctx, "registry"); err != nil || registry.Slug != "registry" {
		t.Fatalf("positive projection: %+v %v", registry, err)
	}
}

func TestManagedAttemptFailureRollsBackPublicationAndCompletion(t *testing.T) {
	ctx := context.Background()
	meta, db := runtimeSQLite(t)
	if _, err := meta.ApplyManagedConfiguration(ctx, managedRuntimeRequest(0, "adopt")); err != nil {
		t.Fatal(err)
	}
	job, fence := enqueueRuntimeRefresh(t, meta, "refresh", 1, 1, 1, "", 1)
	if err := meta.RecordManagedRefreshAttempt(ctx, fence, "started", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TRIGGER reject_managed_success BEFORE INSERT ON managed_refresh_events WHEN NEW.outcome='succeeded' BEGIN SELECT RAISE(ABORT,'attempt unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	blobs := fsblob.New(filepath.Join(t.TempDir(), "blobs"))
	publication, _ := blobOwnershipPublication(t, "registry", "success", "# Attempt transaction")
	publication.ManagedFence = &fence
	commit := func() error {
		return meta.WithTx(ctx, func(txCtx context.Context, tx store.MetadataStore) error {
			if err := store.PublishRegistryRevision(txCtx, tx, blobs, publication); err != nil {
				return err
			}
			if err := tx.(store.ManagedRefreshAttemptStore).RecordManagedRefreshAttempt(txCtx, fence, "succeeded", publication.PublicationID); err != nil {
				return err
			}
			return tx.CompleteJob(txCtx, job.ID, job.WorkerID)
		})
	}
	if err := commit(); err == nil {
		t.Fatal("publication committed despite unavailable attempt log")
	}
	if _, err := meta.CurrentRegistryPublication(ctx, "registry"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("attempt failure leaked publication: %v", err)
	}
	if raw, err := meta.GetJob(ctx, job.ID); err != nil || raw.Status != store.JobStatusLeased {
		t.Fatalf("attempt failure completed job: %+v %v", raw, err)
	}
	if state, err := meta.GetManagedRegistryRefreshState(ctx, "managed-test", "registry"); err != nil || state.LastSuccess != nil || state.LastAttempt.Outcome != "started" {
		t.Fatalf("attempt failure leaked success status: %+v %v", state, err)
	}
	if events, err := meta.ListManagedRefreshAttempts(ctx, store.ManagedRefreshAttemptQuery{Marketplace: "managed-test", Limit: 10}); err != nil || len(events) != 1 {
		t.Fatalf("attempt failure retained event: %+v %v", events, err)
	}
	if _, err := db.Exec(`DROP TRIGGER reject_managed_success`); err != nil {
		t.Fatal(err)
	}
	if err := commit(); err != nil {
		t.Fatalf("same publication failed after attempt log recovery: %v", err)
	}
	if job, err := meta.GetManagedRefreshJob(ctx, "managed-test", job.ID); err != nil || job.State != "completed" {
		t.Fatalf("positive atomic completion: %+v %v", job, err)
	}
}
