package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/artifact"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/inspection"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/fsblob"
)

func TestReadBlobBoundedStopsAtLimit(t *testing.T) {
	reader := strings.NewReader("0123456789")
	if data, err := store.ReadBlobBounded(reader, 4); !errors.Is(err, store.ErrBlobTooLarge) || data != nil || reader.Len() != 5 {
		t.Fatalf("unbounded read: data=%q remaining=%d error=%v", data, reader.Len(), err)
	}
	if data, err := store.ReadBlobBounded(strings.NewReader("0123"), 4); err != nil || string(data) != "0123" {
		t.Fatalf("exact bound rejected: %q %v", data, err)
	}
}

func blobOwnershipPublication(t testing.TB, registry store.RegistryID, id store.PublicationID, body string) (store.RegistryRevisionPublication, store.ArchiveDigest) {
	t.Helper()
	coordinate := store.ResourceCoordinate{Marketplace: "managed-test", Registry: registry, Kind: "skill", Name: "shared"}
	revision := store.ResourceRevision{Coordinate: coordinate, ContentDigest: store.RevisionDigest(sha256Text("a")), License: "MIT"}
	projection, err := inspection.Build(coordinate, string(revision.ContentDigest), []byte(body), nil, 10, 1<<20, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	assessment := validResourceAssessment()
	assessment.Coordinate = coordinate
	assessment.ContentDigest = revision.ContentDigest
	assessment.InspectionManifestDigest = store.RevisionDigest(projection.Manifest.ManifestDigest)
	assessment, err = store.FinalizeResourceAssessment(assessment)
	if err != nil {
		t.Fatal(err)
	}
	payload, digest, err := artifact.CanonicalRegistryRevisionPayload(artifact.RegistryRevisionManifest{SchemaVersion: "v1", Generator: artifact.RegistryRevisionGeneratorV1, Resources: []artifact.RevisionResource{{Kind: "skill", Name: "shared", ContentDigest: string(revision.ContentDigest)}}})
	if err != nil {
		t.Fatal(err)
	}
	req := store.RegistryRevisionPublication{PublicationID: id, Occurrence: store.RegistryOccurrence{RegistryID: registry, Ref: "main", Commit: "real-commit", Acquisition: store.AcquisitionIndexOnly, RegistryRevisionManifestDigest: store.RevisionDigest(digest)}, Resources: []store.ResourceRevision{revision}, ResourceOccurrences: []store.ResourceOccurrence{{Coordinate: coordinate, ContentDigest: revision.ContentDigest, Commit: "real-commit", ManifestDigest: store.RevisionDigest(digest), SourcePath: "skills/shared"}}, Assessments: []store.ResourceAssessment{assessment}, ManifestPayload: payload, InspectionBlobs: []store.BlobUpload{{Digest: store.ArchiveDigest(projection.Manifest.ManifestDigest), Data: projection.Payload}}}
	for digest, payload := range projection.Bodies {
		req.InspectionBlobs = append(req.InspectionBlobs, store.BlobUpload{Digest: store.ArchiveDigest(digest), Data: payload})
	}
	return req, store.ArchiveDigest(sha256Digest([]byte(body)))
}

func putBlobOwnershipRegistry(t testing.TB, meta store.MetadataStore, id store.RegistryID) {
	t.Helper()
	if err := meta.PutRegistry(context.Background(), store.Registry{ID: id, Slug: string(id), Listed: true, Ref: "main", Acquisition: store.AcquisitionIndexOnly}); err != nil {
		t.Fatal(err)
	}
}

func TestBlobOwnersSharedHistoricalAndWithdrawn(t *testing.T) {
	ctx := context.Background()
	meta, managed := managedStore(t)
	blobs := fsblob.New(filepath.Join(t.TempDir(), "blobs"))
	owners := meta.(store.BlobOwnerStore)
	for _, registry := range []store.RegistryID{"allowed", "withdrawn"} {
		putBlobOwnershipRegistry(t, meta, registry)
		req, _ := blobOwnershipPublication(t, registry, "", "# Shared body")
		if err := store.PublishRegistryRevision(ctx, meta, blobs, req); err != nil {
			t.Fatal(err)
		}
	}
	withdrawn, err := meta.GetRegistry(ctx, "withdrawn")
	if err != nil {
		t.Fatal(err)
	}
	withdrawn.Listed = false
	if err := meta.PutRegistry(ctx, withdrawn); err != nil {
		t.Fatal(err)
	}
	change := managedRequest(0, "withdrawal")
	change.Change.Snapshot = json.RawMessage(`{"version":1,"policy_revision":1,"registries":[{"id":"allowed","slug":"allowed","origin":"https://example.test/allowed.git","ref":"main","acquisition":"index-only","generation":1,"lifecycle":"active","paused":false,"withdrawn":false,"retired":false},{"id":"withdrawn","slug":"withdrawn","origin":"https://example.test/withdrawn.git","ref":"main","acquisition":"index-only","generation":2,"lifecycle":"withdrawn","paused":true,"withdrawn":true,"retired":false}]}`)
	if _, err := managed.ApplyManagedConfiguration(ctx, change); err != nil {
		t.Fatal(err)
	}
	req, bodyDigest := blobOwnershipPublication(t, "allowed", "", "# Shared body")
	resources, registries, err := owners.ListBlobOwners(ctx, "managed-test", "content", bodyDigest)
	if err != nil || len(resources) != 2 || len(registries) != 0 || resources[0].Coordinate.Registry != "allowed" || resources[1].Coordinate.Registry != "withdrawn" {
		t.Fatalf("shared/unlisted/withdrawn owner lost: %+v %+v %v", resources, registries, err)
	}
	resources, registries, err = owners.ListBlobOwners(ctx, "managed-test", "manifest", store.ArchiveDigest(req.Occurrence.RegistryRevisionManifestDigest))
	if err != nil || len(resources) != 2 || len(registries) != 2 || registries[0] != "allowed" || registries[1] != "withdrawn" {
		t.Fatalf("shared manifest owners: %+v %+v %v", resources, registries, err)
	}
	resources, _, err = owners.ListBlobOwners(ctx, "managed-test", "inspection", store.ArchiveDigest(req.Assessments[0].InspectionManifestDigest))
	if err != nil || len(resources) != 1 || resources[0].Coordinate.Registry != "allowed" {
		t.Fatalf("inspection owner: %+v %v", resources, err)
	}
	orphan := store.ArchiveDigest(sha256Digest([]byte("unrelated orphan")))
	if err := blobs.Put(ctx, orphan, []byte("unrelated orphan")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := owners.ListBlobOwners(ctx, "managed-test", "content", orphan); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("orphan allowed: %v", err)
	}
	if _, _, err := owners.ListBlobOwners(ctx, "another-marketplace", "content", bodyDigest); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("marketplace owner scope crossed: %v", err)
	}
}

func TestBlobOwnersCrossRouteDigestIsConservative(t *testing.T) {
	ctx := context.Background()
	meta, _ := managedStore(t)
	blobs := fsblob.New(filepath.Join(t.TempDir(), "blobs"))
	putBlobOwnershipRegistry(t, meta, "manifest-owner")
	putBlobOwnershipRegistry(t, meta, "content-owner")
	first, _ := blobOwnershipPublication(t, "manifest-owner", "", "# Ordinary body")
	if err := store.PublishRegistryRevision(ctx, meta, blobs, first); err != nil {
		t.Fatal(err)
	}
	second, digest := blobOwnershipPublication(t, "content-owner", "", string(first.ManifestPayload))
	if err := store.PublishRegistryRevision(ctx, meta, blobs, second); err != nil {
		t.Fatal(err)
	}
	resources, registries, err := meta.(store.BlobOwnerStore).ListBlobOwners(ctx, "managed-test", "content", digest)
	if err != nil || len(resources) != 2 || len(registries) != 2 {
		t.Fatalf("content alias bypassed manifest owners: %+v %+v %v", resources, registries, err)
	}
}

func TestBlobOwnersCollectionIncludesForeignLeaf(t *testing.T) {
	ctx := context.Background()
	meta, _ := managedStore(t)
	blobs := fsblob.New(filepath.Join(t.TempDir(), "blobs"))
	putBlobOwnershipRegistry(t, meta, "leaf-registry")
	putBlobOwnershipRegistry(t, meta, "collection-registry")
	leaf, _ := blobOwnershipPublication(t, "leaf-registry", "", "# Foreign leaf")
	if err := store.PublishRegistryRevision(ctx, meta, blobs, leaf); err != nil {
		t.Fatal(err)
	}
	collectionPayload, digest, err := hccartifact.EncodeCollectionManifest(hccartifact.CollectionManifest{SchemaVersion: "v1", Name: "bundle", Members: []hccartifact.CollectionMemberEntry{{Kind: "skill", Name: "shared", RegistryID: "leaf-registry", Revision: string(leaf.Resources[0].ContentDigest)}}, SupportedSchemas: []string{"v1"}})
	if err != nil {
		t.Fatal(err)
	}
	coordinate := store.ResourceCoordinate{Marketplace: "managed-test", Registry: "collection-registry", Kind: "collection", Name: "bundle"}
	revision := store.ResourceRevision{Coordinate: coordinate, ContentDigest: store.RevisionDigest(digest), License: "MIT"}
	projection, err := inspection.Build(coordinate, digest, collectionPayload, nil, 10, 1<<20, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	assessment := validResourceAssessment()
	assessment.Coordinate = coordinate
	assessment.ContentDigest = revision.ContentDigest
	assessment.InspectionManifestDigest = store.RevisionDigest(projection.Manifest.ManifestDigest)
	assessment, err = store.FinalizeResourceAssessment(assessment)
	if err != nil {
		t.Fatal(err)
	}
	registryPayload, registryDigest, err := artifact.CanonicalRegistryRevisionPayload(artifact.RegistryRevisionManifest{SchemaVersion: "v1", Generator: artifact.RegistryRevisionGeneratorV1, Resources: []artifact.RevisionResource{{Kind: "collection", Name: "bundle", ContentDigest: digest}}})
	if err != nil {
		t.Fatal(err)
	}
	occurrence := store.RegistryOccurrence{RegistryID: "collection-registry", Commit: "commit", Ref: "main", Acquisition: store.AcquisitionIndexOnly, RegistryRevisionManifestDigest: store.RevisionDigest(registryDigest)}
	if err := meta.PutRegistryOccurrence(ctx, occurrence); err != nil {
		t.Fatal(err)
	}
	if err := meta.PutResourceRevision(ctx, revision); err != nil {
		t.Fatal(err)
	}
	if err := meta.PutResourceOccurrence(ctx, store.ResourceOccurrence{Coordinate: coordinate, ContentDigest: revision.ContentDigest, Commit: occurrence.Commit, ManifestDigest: occurrence.RegistryRevisionManifestDigest}); err != nil {
		t.Fatal(err)
	}
	if err := meta.PutResourceAssessment(ctx, assessment); err != nil {
		t.Fatal(err)
	}
	if err := meta.SetCurrentResourceAssessment(ctx, assessment); err != nil {
		t.Fatal(err)
	}
	if err := meta.PutCollectionRevision(ctx, store.CollectionRevision{ID: digest, RegistryID: "collection-registry", ManifestDigest: store.RevisionDigest(digest), Members: []store.ResourceCoordinate{leaf.Resources[0].Coordinate}}); err != nil {
		t.Fatal(err)
	}
	for _, blob := range []store.BlobUpload{{Digest: store.ArchiveDigest(digest), Data: collectionPayload}, {Digest: store.ArchiveDigest(registryDigest), Data: registryPayload}, {Digest: store.ArchiveDigest(projection.Manifest.ManifestDigest), Data: projection.Payload}} {
		if err := blobs.Put(ctx, blob.Digest, blob.Data); err != nil {
			t.Fatal(err)
		}
	}
	result, err := store.BackfillBlobOwners(ctx, meta, blobs)
	if err != nil || result.Remaining {
		t.Fatalf("collection backfill: %+v %v", result, err)
	}
	resources, registries, err := meta.(store.BlobOwnerStore).ListBlobOwners(ctx, "managed-test", "manifest", store.ArchiveDigest(digest))
	if err != nil || len(resources) != 2 || len(registries) != 1 || resources[0].Coordinate != coordinate || resources[1].Coordinate != leaf.Resources[0].Coordinate {
		t.Fatalf("collection foreign owner omitted: %+v %+v %v", resources, registries, err)
	}
}

func TestBlobOwnersCannotIgnoreIntrinsicArchiveOwner(t *testing.T) {
	ctx := context.Background()
	meta, _ := managedStore(t)
	blobs := fsblob.New(filepath.Join(t.TempDir(), "blobs"))
	putBlobOwnershipRegistry(t, meta, "registry")
	req, digest := blobOwnershipPublication(t, "registry", "", "# Shared bytes")
	if err := store.PublishRegistryRevision(ctx, meta, blobs, req); err != nil {
		t.Fatal(err)
	}
	if owners, _, err := meta.(store.BlobOwnerStore).ListBlobOwners(ctx, "managed-test", "content", digest); err != nil || len(owners) != 1 {
		t.Fatalf("control: %+v %v", owners, err)
	}
	unassessed := store.ResourceRevision{Coordinate: store.ResourceCoordinate{Marketplace: "managed-test", Registry: "registry", Kind: "skill", Name: "unassessed"}, ContentDigest: store.RevisionDigest(sha256Text("b")), ArchiveDigest: digest}
	if err := meta.PutResourceRevision(ctx, unassessed); err != nil {
		t.Fatal(err)
	}
	if _, _, err := meta.(store.BlobOwnerStore).ListBlobOwners(ctx, "managed-test", "content", digest); !errors.Is(err, store.ErrBlobOwnersIncomplete) {
		t.Fatalf("content route ignored unassessed archive alias: %v", err)
	}
}

func TestBlobOwnersRetainOldAssessmentBodies(t *testing.T) {
	ctx := context.Background()
	meta, _ := managedStore(t)
	blobs := fsblob.New(filepath.Join(t.TempDir(), "blobs"))
	putBlobOwnershipRegistry(t, meta, "registry")
	first, oldBody := blobOwnershipPublication(t, "registry", "first", "# Original inspection")
	if err := store.PublishRegistryRevision(ctx, meta, blobs, first); err != nil {
		t.Fatal(err)
	}
	second, newBody := blobOwnershipPublication(t, "registry", "second", "# New inspection")
	if err := store.PublishRegistryRevision(ctx, meta, blobs, second); err != nil {
		t.Fatal(err)
	}
	for _, digest := range []store.ArchiveDigest{oldBody, newBody} {
		owners, _, err := meta.(store.BlobOwnerStore).ListBlobOwners(ctx, "managed-test", "content", digest)
		if err != nil || len(owners) != 1 || owners[0].Coordinate != first.Resources[0].Coordinate {
			t.Fatalf("historical body ownership lost: %+v %v", owners, err)
		}
	}
	owners, _, err := meta.(store.BlobOwnerStore).ListBlobOwners(ctx, "managed-test", "inspection", store.ArchiveDigest(first.Assessments[0].InspectionManifestDigest))
	if err != nil || len(owners) != 1 || owners[0].Assessment.InspectionManifestDigest != second.Assessments[0].InspectionManifestDigest {
		t.Fatalf("old inspection should retain owner with current restriction: %+v %v", owners, err)
	}
}

func seedUnindexedBlobPublication(t *testing.T, meta store.MetadataStore, blobs store.BlobStore, registry store.RegistryID) store.ArchiveDigest {
	t.Helper()
	ctx := context.Background()
	putBlobOwnershipRegistry(t, meta, registry)
	req, digest := blobOwnershipPublication(t, registry, "legacy", "# Legacy shared body")
	if err := meta.(store.PublicationStore).PublishRegistryPublication(ctx, req); err != nil {
		t.Fatal(err)
	}
	for _, blob := range append(req.InspectionBlobs, store.BlobUpload{Digest: store.ArchiveDigest(req.Occurrence.RegistryRevisionManifestDigest), Data: req.ManifestPayload}) {
		if err := blobs.Put(ctx, blob.Digest, blob.Data); err != nil {
			t.Fatal(err)
		}
	}
	return digest
}

func TestBlobOwnersBoundedBackfillCompleteness(t *testing.T) {
	ctx := context.Background()
	meta, _ := managedStore(t)
	blobs := fsblob.New(filepath.Join(t.TempDir(), "blobs"))
	digest := seedUnindexedBlobPublication(t, meta, blobs, "first")
	seedUnindexedBlobPublication(t, meta, blobs, "second")
	owners := meta.(store.BlobOwnerStore)
	if _, _, err := owners.ListBlobOwners(ctx, "managed-test", "content", digest); !errors.Is(err, store.ErrBlobOwnersIncomplete) {
		t.Fatalf("unindexed legacy data accepted: %v", err)
	}
	result, err := meta.(store.BlobOwnerMaintenanceStore).BackfillBlobOwners(ctx, blobs, 1)
	if err != nil || result.Processed != 1 || !result.Remaining {
		t.Fatalf("bounded backfill: %+v %v", result, err)
	}
	if _, _, err := owners.ListBlobOwners(ctx, "managed-test", "content", digest); !errors.Is(err, store.ErrBlobOwnersIncomplete) {
		t.Fatalf("partial shared digest coverage accepted: %v", err)
	}
	result, err = store.BackfillBlobOwners(ctx, meta, blobs)
	if err != nil || result.Processed != 3 || result.Remaining {
		t.Fatalf("complete backfill: %+v %v", result, err)
	}
	got, _, err := owners.ListBlobOwners(ctx, "managed-test", "content", digest)
	if err != nil || len(got) != 2 {
		t.Fatalf("backfilled owners incomplete: %+v %v", got, err)
	}
	result, err = store.BackfillBlobOwners(ctx, meta, blobs)
	if err != nil || result.Processed != 0 || result.Remaining {
		t.Fatalf("backfill retry: %+v %v", result, err)
	}
	seedUnindexedBlobPublication(t, meta, blobs, "third")
	if _, _, err := owners.ListBlobOwners(ctx, "managed-test", "content", digest); !errors.Is(err, store.ErrBlobOwnersIncomplete) {
		t.Fatalf("new low-level metadata failed to invalidate coverage: %v", err)
	}
}

func TestBlobOwnersPublicationRollback(t *testing.T) {
	ctx := context.Background()
	meta, _ := managedStore(t)
	blobs := fsblob.New(filepath.Join(t.TempDir(), "blobs"))
	putBlobOwnershipRegistry(t, meta, "registry")
	req, digest := blobOwnershipPublication(t, "registry", "rolled-back", "# Rollback bytes")
	aborted := errors.New("abort metadata transaction after ownership insertion")
	err := meta.WithTx(ctx, func(txCtx context.Context, tx store.MetadataStore) error {
		if err := store.PublishRegistryRevision(txCtx, tx, blobs, req); err != nil {
			return err
		}
		owners, _, err := tx.(store.BlobOwnerStore).ListBlobOwners(txCtx, "managed-test", "content", digest)
		if err != nil || len(owners) != 1 {
			t.Fatalf("non-vacuous transaction control: %+v %v", owners, err)
		}
		return aborted
	})
	if !errors.Is(err, aborted) {
		t.Fatal(err)
	}
	if _, _, err := meta.(store.BlobOwnerStore).ListBlobOwners(ctx, "managed-test", "content", digest); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("rolled-back ownership visible: %v", err)
	}
	if _, err := meta.GetResourceRevision(ctx, req.Resources[0].Coordinate, req.Resources[0].ContentDigest); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("rolled-back revision visible: %v", err)
	}
	if _, err := meta.(store.PublicationStore).CurrentRegistryPublication(ctx, "registry"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("rolled-back head visible: %v", err)
	}
	if exists, err := blobs.Exists(ctx, digest); err != nil || !exists {
		t.Fatalf("expected harmless orphan blob: %t %v", exists, err)
	}
}

func TestBlobOwnersBackfillRejectsCorruptImmutablePayload(t *testing.T) {
	ctx := context.Background()
	meta, _ := managedStore(t)
	blobs := fsblob.New(filepath.Join(t.TempDir(), "blobs"))
	digest := seedUnindexedBlobPublication(t, meta, blobs, "registry")
	req, _ := blobOwnershipPublication(t, "registry", "legacy", "# Legacy shared body")
	inspectionDigest := store.ArchiveDigest(req.Assessments[0].InspectionManifestDigest)
	if err := blobs.Put(ctx, inspectionDigest, []byte(`{"untrusted":"wrong bytes"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.BackfillBlobOwners(ctx, meta, blobs); !errors.Is(err, store.ErrBlobOwnershipInvalid) {
		t.Fatalf("corrupt immutable payload indexed: %v", err)
	}
	if _, _, err := meta.(store.BlobOwnerStore).ListBlobOwners(ctx, "managed-test", "content", digest); !errors.Is(err, store.ErrBlobOwnersIncomplete) {
		t.Fatalf("failed backfill marked coverage complete: %v", err)
	}
	if err := blobs.Put(ctx, inspectionDigest, []byte(strings.Repeat("x", (1<<20)+1))); err != nil {
		t.Fatal(err)
	}
	if _, err := store.BackfillBlobOwners(ctx, meta, blobs); !errors.Is(err, store.ErrBlobTooLarge) {
		t.Fatalf("oversize immutable payload read without bound: %v", err)
	}
	if err := blobs.Put(ctx, inspectionDigest, req.InspectionBlobs[0].Data); err != nil {
		t.Fatal(err)
	}
	if result, err := store.BackfillBlobOwners(ctx, meta, blobs); err != nil || result.Remaining {
		t.Fatalf("restored immutable payload could not backfill: %+v %v", result, err)
	}
}

func TestBlobOwnersManagedPublication(t *testing.T) {
	ctx := context.Background()
	meta, managed := managedStore(t)
	blobs := fsblob.New(filepath.Join(t.TempDir(), "blobs"))
	putBlobOwnershipRegistry(t, meta, "registry")
	adopt := managedRequest(0, "adopt")
	adopt.Change.Snapshot = publicationSnapshot(1, 1, false, false, false)
	if _, err := managed.ApplyManagedConfiguration(ctx, adopt); err != nil {
		t.Fatal(err)
	}
	job, err := meta.LeaseJobByID(ctx, "job-adopt", "worker", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	req, digest := blobOwnershipPublication(t, "registry", "managed-publication", "# Managed bytes")
	req.ManagedFence = &store.ManagedFence{Marketplace: "managed-test", RegistryGeneration: 1, PolicyRevision: 1, JobID: job.ID, WorkerID: "worker", Attempt: job.Attempt}
	if err := store.PublishRegistryRevision(ctx, meta, blobs, req); err != nil {
		t.Fatal(err)
	}
	owners, _, err := meta.(store.BlobOwnerStore).ListBlobOwners(ctx, "managed-test", "content", digest)
	if err != nil || len(owners) != 1 {
		t.Fatalf("managed publication owners: %+v %v", owners, err)
	}
}
