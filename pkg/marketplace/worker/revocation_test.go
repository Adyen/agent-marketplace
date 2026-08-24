package worker

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/sqlite"
)

type failingRevocationStore struct {
	store.MetadataStore
	failAt int
}

func (s *failingRevocationStore) WithTx(ctx context.Context, fn func(context.Context, store.MetadataStore) error) error {
	return s.MetadataStore.WithTx(ctx, func(txCtx context.Context, tx store.MetadataStore) error {
		return fn(txCtx, &failingRevocationTx{MetadataStore: tx, failAt: s.failAt})
	})
}

type failingRevocationTx struct {
	store.MetadataStore
	failAt int
	puts   int
}

func (s *failingRevocationTx) PutRevocation(ctx context.Context, revocation store.Revocation) error {
	s.puts++
	if s.puts == s.failAt {
		return errors.New("injected revocation failure")
	}
	return s.MetadataStore.PutRevocation(ctx, revocation)
}

func TestEnsureRevocationsExactAdditionsAreAtomic(t *testing.T) {
	ctx := context.Background()
	base := sqlite.New(":memory:")
	if err := base.Open(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = base.Close() })
	meta := &failingRevocationStore{MetadataStore: base, failAt: 2}
	digestA := "sha256:" + strings.Repeat("a", 64)
	digestB := "sha256:" + strings.Repeat("b", 64)
	deps := Deps{
		Config: config.Config{Revocations: config.Revocations{Revisions: []string{
			"mp/src/skill/a@" + digestA,
			"mp/src/skill/b@" + digestB,
		}}},
		MetadataStore: meta,
	}

	if err := EnsureRevocations(ctx, deps); err == nil {
		t.Fatal("EnsureRevocations succeeded despite injected failure")
	}
	revocations, err := base.ListRevocations(ctx, store.RevocationRevision)
	if err != nil {
		t.Fatal(err)
	}
	if len(revocations) != 0 {
		t.Fatalf("exact revocations after rollback = %d, want 0", len(revocations))
	}
}

func TestRunFailsAfterRevocationReconciliationFailure(t *testing.T) {
	ctx := context.Background()
	base := sqlite.New(":memory:")
	if err := base.Open(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = base.Close() })
	meta := &failingRevocationStore{MetadataStore: base, failAt: 1}
	deps := Deps{
		Config: config.Config{
			Instance:    config.Instance{ID: "mp", Name: "Marketplace"},
			Revocations: config.Revocations{Revisions: []string{"mp/src/skill/a@sha256:" + strings.Repeat("a", 64)}},
			Refresh:     config.Refresh{Interval: config.Duration(time.Second)},
		},
		MetadataStore: meta,
	}
	if err := Run(ctx, deps, deps.Config); err == nil || !strings.Contains(err.Error(), "injected revocation failure") {
		t.Fatalf("Run error = %v, want reconciliation failure", err)
	}
}
