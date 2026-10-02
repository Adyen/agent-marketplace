package store_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func requireCatalogResetter(t *testing.T, meta store.MetadataStore) store.RegistryCatalogResetter {
	t.Helper()
	r, ok := meta.(store.RegistryCatalogResetter)
	if !ok {
		t.Fatal("metadata store does not support scoped catalog reset")
	}
	return r
}

func TestRegistryCatalogResetPreservesConfigurationAndOtherRegistries(t *testing.T) {
	ctx := t.Context()
	meta, managed := managedStore(t)
	publisher := meta.(store.PublicationStore)
	for _, id := range []store.RegistryID{"registry", "unrelated"} {
		if err := meta.PutRegistry(ctx, store.Registry{ID: id, Slug: string(id), Ref: "main", Acquisition: store.AcquisitionMirror, Origins: []string{"https://example.test/repo.git"}, Listed: true, RefreshStatus: store.RegistryRefreshStatusOK}); err != nil {
			t.Fatal(err)
		}
	}
	first := publicationRequest(t, "first", "review")
	first.Resources[0].ArchiveDigest = store.ArchiveDigest(sha256Text("b"))
	if err := publisher.PublishRegistryPublication(ctx, first); err != nil {
		t.Fatal(err)
	}
	legacy := first.Occurrence
	legacy.Commit = "older-commit"
	if err := meta.PutRegistryOccurrence(ctx, legacy); err != nil {
		t.Fatal(err)
	}
	if err := meta.SetRegistryHead(ctx, "registry", legacy.Commit, ""); err != nil {
		t.Fatal(err)
	}
	legacyMember := first.ResourceOccurrences[0]
	legacyMember.Commit = legacy.Commit
	if err := meta.PutResourceOccurrence(ctx, legacyMember); err != nil {
		t.Fatal(err)
	}
	collection := store.CollectionRevision{ID: "collection-id", RegistryID: "registry", ManifestDigest: store.RevisionDigest(sha256Text("c"))}
	if err := meta.PutCollectionRevision(ctx, collection); err != nil {
		t.Fatal(err)
	}
	other := publicationRequest(t, "other", "keep")
	other.Occurrence.RegistryID = "unrelated"
	for i := range other.Resources {
		other.Resources[i].Coordinate.Registry = "unrelated"
		other.Resources[i].ArchiveDigest = first.Resources[0].ArchiveDigest
		other.ResourceOccurrences[i].Coordinate.Registry = "unrelated"
		other.Assessments[i].Coordinate.Registry = "unrelated"
		other.Assessments[i].AssessmentDigest = ""
	}
	if err := publisher.PublishRegistryPublication(ctx, other); err != nil {
		t.Fatal(err)
	}
	adoption := managedRequest(0, "adopt")
	adoption.Change.Snapshot = publicationSnapshot(1, 1, false, false, false)
	if _, err := managed.ApplyManagedConfiguration(ctx, adoption); err != nil {
		t.Fatal(err)
	}
	revocation := store.Revocation{Kind: store.RevocationRegistry, TargetID: "registry", Reason: "keep policy", AppliedAt: time.Now().UTC()}
	if err := meta.PutRevocation(ctx, revocation); err != nil {
		t.Fatal(err)
	}
	settings, err := meta.GetRegistry(ctx, "registry")
	if err != nil {
		t.Fatal(err)
	}
	history, err := managed.ManagedConfigurationHistory(ctx, "managed-test", 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	before, err := meta.ReadCatalogProjectionPage(ctx, store.CatalogProjectionPageRequest{Marketplace: "managed-test", Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	reset := requireCatalogResetter(t, meta)
	preview, err := reset.ResetRegistryCatalog(ctx, "registry", true)
	if err != nil || preview["resource_revisions"] != 1 || preview["registry_publications"] != 1 || preview["registry_occurrences"] != 1 || preview["collection_revisions"] != 1 {
		t.Fatalf("preview: %v, %v", preview, err)
	}
	unchanged, err := meta.ReadCatalogProjectionPage(ctx, store.CatalogProjectionPageRequest{Marketplace: "managed-test", Limit: 100})
	if err != nil || !reflect.DeepEqual(before, unchanged) {
		t.Fatalf("preview changed catalog: %+v, %v", unchanged, err)
	}
	deleted, err := reset.ResetRegistryCatalog(ctx, "registry", false)
	if err != nil || !reflect.DeepEqual(preview, deleted) {
		t.Fatalf("reset: %v, %v", deleted, err)
	}
	if _, err := meta.GetResourceRevision(ctx, first.Resources[0].Coordinate, first.Resources[0].ContentDigest); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("retained exact resource: %v", err)
	}
	if _, err := publisher.GetRegistryPublication(ctx, "registry", "first"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("retained publication: %v", err)
	}
	if _, err := meta.GetCollectionRevision(ctx, collection.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("retained collection: %v", err)
	}
	occurrences, err := meta.RegistryHistory(ctx, "registry")
	if err != nil || len(occurrences) != 0 {
		t.Fatalf("retained occurrences: %v, %v", occurrences, err)
	}
	afterSettings, err := meta.GetRegistry(ctx, "registry")
	if err != nil {
		t.Fatal(err)
	}
	if afterSettings.RefreshStatus != store.RegistryRefreshStatusUnknown || afterSettings.RefreshError != "" || !afterSettings.RefreshedAt.IsZero() {
		t.Fatalf("stale refresh status: %+v", afterSettings)
	}
	afterSettings.RefreshStatus = settings.RefreshStatus
	afterSettings.UpdatedAt = settings.UpdatedAt
	if !reflect.DeepEqual(settings, afterSettings) {
		t.Fatalf("registry configuration changed: before=%+v after=%+v", settings, afterSettings)
	}
	afterHistory, err := managed.ManagedConfigurationHistory(ctx, "managed-test", 0, 100)
	if err != nil || !reflect.DeepEqual(history, afterHistory) {
		t.Fatalf("managed configuration/audit changed: %v, %v", afterHistory, err)
	}
	if got, err := meta.GetRevocation(ctx, revocation.Kind, revocation.TargetID); err != nil || got.Reason != revocation.Reason {
		t.Fatalf("revocation lost: %+v, %v", got, err)
	}
	if _, err := meta.GetLatestResourceRevision(ctx, other.Resources[0].Coordinate); err != nil {
		t.Fatalf("unrelated resource lost: %v", err)
	}
	owners, err := meta.ListResourceRevisionsByArchiveDigest(ctx, "managed-test", first.Resources[0].ArchiveDigest)
	if err != nil || len(owners) != 1 || owners[0].Coordinate.Registry != "unrelated" {
		t.Fatalf("shared archive ownership: %+v, %v", owners, err)
	}
	after, err := meta.ReadCatalogProjectionPage(ctx, store.CatalogProjectionPageRequest{Marketplace: "managed-test", Limit: 100})
	if err != nil || after.CatalogVersion <= before.CatalogVersion || len(after.Resources) != 1 {
		t.Fatalf("catalog not invalidated: %+v, %v", after, err)
	}
	if _, err := reset.ResetRegistryCatalog(ctx, "registry", false); err != nil {
		t.Fatalf("repeated reset: %v", err)
	}
	job, err := meta.LeaseJobByID(ctx, "job-adopt", "worker", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	first.ManagedFence = &store.ManagedFence{Marketplace: "managed-test", RegistryGeneration: 1, PolicyRevision: 1, JobID: job.ID, WorkerID: "worker", Attempt: job.Attempt}
	first.PublicationID = "rebuilt"
	first.Resources[0].ArchiveDigest = store.ArchiveDigest(sha256Text("d"))
	first.Occurrence.RegistryRevisionManifestDigest = store.RevisionDigest(sha256Text("e"))
	first.ResourceOccurrences[0].ManifestDigest = first.Occurrence.RegistryRevisionManifestDigest
	if err := publisher.PublishRegistryPublication(ctx, first); err != nil {
		t.Fatalf("republish same content with repaired archive: %v", err)
	}
}

func TestRegistryCatalogResetRollsBack(t *testing.T) {
	meta, _ := managedStore(t)
	ctx := t.Context()
	if err := meta.PutRegistry(ctx, store.Registry{ID: "registry", Slug: "registry", Ref: "main", Acquisition: store.AcquisitionMirror}); err != nil {
		t.Fatal(err)
	}
	pub := publicationRequest(t, "before", "review")
	if err := meta.(store.PublicationStore).PublishRegistryPublication(ctx, pub); err != nil {
		t.Fatal(err)
	}
	abort := errors.New("abort transaction")
	err := meta.WithTx(ctx, func(ctx context.Context, tx store.MetadataStore) error {
		if _, err := requireCatalogResetter(t, tx).ResetRegistryCatalog(ctx, "registry", false); err != nil {
			return err
		}
		return abort
	})
	if !errors.Is(err, abort) {
		t.Fatal(err)
	}
	if _, err := meta.GetLatestResourceRevision(ctx, pub.Resources[0].Coordinate); err != nil {
		t.Fatalf("rollback lost resource: %v", err)
	}
	for _, id := range []store.RegistryID{"", "missing"} {
		if _, err := requireCatalogResetter(t, meta).ResetRegistryCatalog(ctx, id, false); err == nil {
			t.Fatalf("accepted invalid registry %q", id)
		}
	}
	if err := meta.EnqueueJob(ctx, store.Job{ID: "active", Kind: "managed-refresh", Payload: []byte(`{}`), MaxAttempts: 2}); err != nil {
		t.Fatal(err)
	}
	if _, err := meta.LeaseJobByID(ctx, "active", "worker", time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := requireCatalogResetter(t, meta).ResetRegistryCatalog(ctx, "registry", false); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("reset while a worker has a live lease: %v", err)
	}
	if _, err := meta.GetLatestResourceRevision(ctx, pub.Resources[0].Coordinate); err != nil {
		t.Fatalf("refused reset lost resource: %v", err)
	}
}
