package sqlite_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/sqlite"
)

func TestManagedConfigurationCorruptMarkerCannotBeReadopted(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "marketplace.db")
	s := sqlite.New(path)
	if err := s.Open(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	if err := s.PutMarketplace(ctx, store.Marketplace{ID: "test", Name: "Test", CanonicalURL: "https://marketplace.test"}); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.ExecContext(ctx, `INSERT INTO managed_configuration_heads (marketplace_id, revision) VALUES ('test', 0)`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CurrentManagedConfiguration(ctx, "test"); !errors.Is(err, store.ErrManagedStateCorrupt) {
		t.Fatalf("corrupt marker read: %v", err)
	}
	_, err = s.ApplyManagedConfiguration(ctx, store.ManagedChangeRequest{Change: store.ManagedChange{
		Marketplace: "test", Actor: store.ManagedActor{Issuer: "proxy", Subject: "admin"},
		Operation: "adopt", Reason: "Explicit adoption", RequestID: "request", IdempotencyKey: "adopt", RequestDigest: "digest",
		Snapshot: json.RawMessage(`{"version":1,"policy_revision":1,"registries":[]}`),
	}})
	if !errors.Is(err, store.ErrManagedStateCorrupt) {
		t.Fatalf("corrupt authority must not rebootstrap: %v", err)
	}
}

func TestManagedConfigurationAuditFailureRollsBackWork(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "marketplace.db")
	s := sqlite.New(path)
	if err := s.Open(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	if err := s.PutMarketplace(ctx, store.Marketplace{ID: "test", Name: "Test", CanonicalURL: "https://marketplace.test"}); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.ExecContext(ctx, `CREATE TRIGGER reject_managed_audit BEFORE INSERT ON managed_configuration_changes BEGIN SELECT RAISE(ABORT, 'audit unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	_, err = s.ApplyManagedConfiguration(ctx, store.ManagedChangeRequest{Change: store.ManagedChange{
		Marketplace: "test", Actor: store.ManagedActor{Issuer: "proxy", Subject: "admin"},
		Operation: "adopt", Reason: "Explicit adoption", RequestID: "request", IdempotencyKey: "adopt", RequestDigest: "digest",
		Snapshot: json.RawMessage(`{"version":1,"policy_revision":1,"registries":[]}`),
	}, Jobs: []store.Job{{ID: "audit-job", Kind: "managed-reconcile", IdempotencyKey: "audit-job", Payload: []byte(`{}`)}}})
	if err == nil {
		t.Fatal("adoption succeeded without an audit record")
	}
	if _, err := s.CurrentManagedConfiguration(ctx, "test"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("failed audit retained authority: %v", err)
	}
	if _, err := s.GetJob(ctx, "audit-job"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("failed audit retained work: %v", err)
	}
}

func TestManagedConfigurationMigration(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "marketplace.db")
	s := sqlite.New(path)
	if err := s.Open(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	for _, table := range []string{"managed_configuration_heads", "managed_configuration_changes"} {
		var count int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil {
			t.Fatalf("managed state table %s unavailable: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("migration adopted configuration implicitly: %s contains %d rows", table, count)
		}
	}
}
