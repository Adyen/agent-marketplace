package admin

import (
	"context"
	"errors"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

type lookupCounts struct{ finds, history, current int }
type lookupTrackingStore struct {
	store.MetadataStore
	store.ManagedConfigurationStore
	counts *lookupCounts
}

func (s *lookupTrackingStore) WithTx(ctx context.Context, fn func(context.Context, store.MetadataStore) error) error {
	return s.MetadataStore.WithTx(ctx, func(ctx context.Context, tx store.MetadataStore) error {
		return fn(ctx, &lookupTrackingStore{MetadataStore: tx, ManagedConfigurationStore: tx.(store.ManagedConfigurationStore), counts: s.counts})
	})
}
func (s *lookupTrackingStore) FindManagedChange(ctx context.Context, id store.MarketplaceID, actor store.ManagedActor, operation, key string) (store.ManagedChange, error) {
	s.counts.finds++
	return s.ManagedConfigurationStore.FindManagedChange(ctx, id, actor, operation, key)
}
func (s *lookupTrackingStore) ManagedConfigurationHistory(context.Context, store.MarketplaceID, int64, int) ([]store.ManagedChange, error) {
	s.counts.history++
	return nil, errors.New("history scan forbidden during mutation")
}
func (s *lookupTrackingStore) CurrentManagedConfiguration(ctx context.Context, id store.MarketplaceID) (store.ManagedChange, error) {
	s.counts.current++
	return s.ManagedConfigurationStore.CurrentManagedConfiguration(ctx, id)
}

func TestIdempotencyUsesOneIndexedLookupBeforeCurrentRevision(t *testing.T) {
	s := managedFixture(t)
	ctx := context.Background()
	counts := &lookupCounts{}
	s.Metadata = &lookupTrackingStore{MetadataStore: s.Metadata, ManagedConfigurationStore: s.Metadata.(store.ManagedConfigurationStore), counts: counts}
	m := mutation("pause", 1)
	accepted, err := s.Apply(ctx, "marketplace", m)
	if err != nil {
		t.Fatal(err)
	}
	if *counts != (lookupCounts{finds: 1, current: 1}) {
		t.Fatalf("fresh mutation queries: %+v", counts)
	}
	*counts = lookupCounts{}
	retry, err := s.Apply(ctx, "marketplace", m)
	if err != nil || retry.Revision != accepted.Revision {
		t.Fatalf("retry: %+v %v", retry, err)
	}
	if *counts != (lookupCounts{finds: 1}) {
		t.Fatalf("retry read stale base or scanned history: %+v", counts)
	}
	*counts = lookupCounts{}
	m.Reason = "different"
	if _, err = s.Apply(ctx, "marketplace", m); !errors.Is(err, store.ErrManagedIdempotencyConflict) {
		t.Fatalf("changed body: %v", err)
	}
	if *counts != (lookupCounts{finds: 1}) {
		t.Fatalf("conflict queries: %+v", counts)
	}
	*counts = lookupCounts{}
	m.Actor.Subject = "another-admin"
	if _, err = s.Apply(ctx, "marketplace", m); !errors.Is(err, store.ErrManagedRevisionConflict) {
		t.Fatalf("actor scope: %v", err)
	}
	if *counts != (lookupCounts{finds: 1, current: 1}) {
		t.Fatalf("actor scoped queries: %+v", counts)
	}
}
