package admin

import (
	"context"
	"errors"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

type maintenanceStore struct {
	store.MetadataStore
	store.ManagedConfigurationStore
	calls int
	fail  error
}

func (s *maintenanceStore) RecordPublicationBlobOwners(context.Context, store.RegistryRevisionPublication, []store.BlobUpload) error {
	return nil
}

func (s *maintenanceStore) BackfillBlobOwners(ctx context.Context, _ store.BlobStore, limit int) (store.BlobOwnerBackfillResult, error) {
	s.calls++
	if _, err := s.CurrentManagedConfiguration(ctx, "marketplace"); !errors.Is(err, store.ErrNotFound) {
		return store.BlobOwnerBackfillResult{}, errors.New("adopted before coverage complete")
	}
	if limit != store.BlobOwnerBackfillLimit {
		return store.BlobOwnerBackfillResult{}, errors.New("unbounded backfill")
	}
	if s.fail != nil {
		return store.BlobOwnerBackfillResult{}, s.fail
	}
	return store.BlobOwnerBackfillResult{Processed: 1, Remaining: s.calls < 2}, nil
}

func TestMaintenanceAdoptionRequiresBackfill(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "complete", true: "failure"}[fail], func(t *testing.T) {
			ctx := context.Background()
			meta, cfg, deployment := adoptionFixture(t)
			wrapped := &maintenanceStore{MetadataStore: meta, ManagedConfigurationStore: meta}
			if fail {
				wrapped.fail = store.ErrBlobOwnersIncomplete
			}
			service := Service{Metadata: wrapped, Deployment: deployment}
			accepted, err := service.AdoptMaintenance(ctx, cfg, nil, AdoptionRequest{Actor: store.ManagedActor{Issuer: "maintenance", Subject: "operator"}, Reason: "Adopt", RequestID: "request", IdempotencyKey: "key"})
			if fail {
				if !errors.Is(err, store.ErrBlobOwnersIncomplete) {
					t.Fatalf("backfill error lost: %v", err)
				}
				if _, err := meta.CurrentManagedConfiguration(ctx, "marketplace"); !errors.Is(err, store.ErrNotFound) {
					t.Fatalf("failed backfill adopted: %v", err)
				}
			} else if err != nil || accepted.Revision != 1 || wrapped.calls != 2 {
				t.Fatalf("maintenance adoption: %+v calls=%d error=%v", accepted, wrapped.calls, err)
			}
		})
	}
}

func TestAdoptionRetryAfterRegistryAdded(t *testing.T) {
	ctx := context.Background()
	meta, cfg, deployment := adoptionFixture(t)
	service := Service{Metadata: meta, Deployment: deployment}
	req := AdoptionRequest{Actor: store.ManagedActor{Issuer: "maintenance", Subject: "operator"}, Reason: "Adopt", RequestID: "request", IdempotencyKey: "key"}
	first, err := service.Adopt(ctx, cfg, req)
	if err != nil {
		t.Fatal(err)
	}
	view, err := service.Read(ctx, "marketplace")
	if err != nil {
		t.Fatal(err)
	}
	registry := view.Snapshot.Registries[0]
	registry.ID, registry.Slug = "new", "new"
	registry.Aliases, registry.HistoricalOrigins, registry.NonLicenseOverrides = nil, nil, nil
	registry.Generation = 0
	if _, err := service.Apply(ctx, "marketplace", Mutation{Action: "upsert_registry", RegistryID: registry.ID, Registry: &registry, ExpectedRevision: 1, Actor: req.Actor, Reason: "Add registry", RequestID: "add", IdempotencyKey: "add"}); err != nil {
		t.Fatal(err)
	}
	retry, err := service.Adopt(ctx, cfg, req)
	if err != nil || retry.Revision != first.Revision {
		t.Fatalf("accepted retry changed: %+v %v", retry, err)
	}
	current, err := service.Read(ctx, "marketplace")
	if err != nil || current.Revision != 2 || len(current.Snapshot.Registries) != 2 {
		t.Fatalf("retry changed live configuration: %+v %v", current, err)
	}
}

func TestAdoptionRejectsOmittedStoredRegistry(t *testing.T) {
	ctx := context.Background()
	meta, cfg, deployment := adoptionFixture(t)
	existing := store.Registry{ID: "omitted", Slug: "omitted", Origins: []string{"https://git.example.test/old.git"}, Ref: "main", Acquisition: store.AcquisitionMirror, Listed: true}
	if err := meta.PutRegistry(ctx, existing); err != nil {
		t.Fatal(err)
	}
	service := Service{Metadata: meta, Deployment: deployment}
	_, err := service.Adopt(ctx, cfg, AdoptionRequest{Actor: store.ManagedActor{Issuer: "maintenance", Subject: "operator"}, Reason: "Adopt", RequestID: "request", IdempotencyKey: "key"})
	if !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("omitted identity must block adoption: %v", err)
	}
	if _, err := meta.CurrentManagedConfiguration(ctx, "marketplace"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("failed adoption changed authority: %v", err)
	}
	preserved, err := meta.GetRegistry(ctx, existing.ID)
	if err != nil || preserved.Slug != existing.Slug || !preserved.Listed {
		t.Fatalf("stored identity changed: %+v %v", preserved, err)
	}
}
