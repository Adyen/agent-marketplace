package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/ingest"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/inspection"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/fsblob"
)

func ingestedRootCollection(t *testing.T, meta store.MetadataStore) store.RegistryRevisionPublication {
	t.Helper()
	ctx := context.Background()
	repo := t.TempDir()
	for name, contents := range map[string]string{
		".agents/skills/review/SKILL.md":           "---\nname: review\ndescription: Review code\nlicense: MIT\n---\nReview code carefully.\n",
		".agents/collections/root/COLLECTION.yaml": "name: root\nversion: 1.0.0\ndescription: Root collection\nresources:\n  - name: review\n    kind: skill\n",
	} {
		filename := filepath.Join(repo, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filename, []byte(contents), 0644); err != nil {
			t.Fatal(err)
		}
	}
	registry := store.Registry{ID: "registry", Slug: "registry", Origins: []string{"https://example.test/registry.git"}, Ref: "main", Roots: []store.RegistryRoot{{Path: ".agents", SourceHarness: "factory"}}, Acquisition: store.AcquisitionMirror, License: "MIT", Listed: true}
	if err := meta.PutRegistry(ctx, registry); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{APIVersion: "agent-marketplace.io/marketplace/v1", AcquisitionDefault: "mirror", Policy: config.Policy{HiddenContent: "reject", LicenseAllowlist: []string{"MIT"}, Limits: config.Limits{RepositoryBytes: 100 << 20, FileCount: 1024, FileBytes: 2 << 20, ArtifactBytes: 16 << 20, CloneDuration: config.Duration(time.Minute), IngestionDuration: config.Duration(time.Minute)}}}
	revision, err := ingest.BuildRegistryRevision(ctx, cfg, "managed-test", registry, repo, strings.Repeat("a", 40))
	if err != nil {
		t.Fatal(err)
	}
	if len(revision.Collections) != 1 || len(revision.Resources) != 2 {
		t.Fatalf("fixture did not produce root collection plus leaf: resources=%d collections=%d", len(revision.Resources), len(revision.Collections))
	}
	return store.RegistryRevisionPublication{Occurrence: revision.Occurrence, Resources: revision.Resources, ResourceOccurrences: revision.ResourceOccurrences, Assessments: revision.Assessments, Collections: revision.Collections, ManifestPayload: revision.ManifestPayload, ExtraBlobs: revision.ExtraBlobs, InspectionBlobs: revision.InspectionBlobs}
}

func TestIngestedRootCollectionOwnershipCoverage(t *testing.T) {
	for _, mode := range []string{"legacy-publication", "managed-publication", "legacy-backfill"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			meta, managed := managedStore(t)
			blobs := fsblob.New(filepath.Join(t.TempDir(), "blobs"))
			request := ingestedRootCollection(t, meta)
			collection := request.Collections[0]
			found := false
			for _, resource := range request.Resources {
				if resource.Coordinate.Kind == "collection" {
					found = true
					if resource.Coordinate.Name != "root" || string(resource.ContentDigest) != collection.ID || resource.ContentDigest != collection.ManifestDigest {
						t.Fatalf("collection identity contract diverged: %+v %+v", resource, collection)
					}
				}
			}
			if !found {
				t.Fatal("root collection resource absent")
			}
			if mode == "managed-publication" {
				adopt := managedRuntimeRequest(0, "adopt")
				adopt.Change.Snapshot = mutateRuntimeSnapshot(t, adopt.Change.Snapshot, func(_ map[string]any, registries []any) {
					r := registries[0].(map[string]any)
					r["acquisition"] = "mirror"
					r["license"] = "MIT"
					r["roots"] = []any{map[string]any{"path": ".agents", "source_harness": "factory"}}
				})
				if _, err := managed.ApplyManagedConfiguration(ctx, adopt); err != nil {
					t.Fatal(err)
				}
				_, fence := enqueueRuntimeRefresh(t, meta, "collection-refresh", 1, 1, 1, "", 1)
				if err := meta.(store.ManagedRefreshAttemptStore).RecordManagedRefreshAttempt(ctx, fence, "started", ""); err != nil {
					t.Fatal(err)
				}
				request.PublicationID = "managed-root"
				request.ManagedFence = &fence
			}
			if mode == "legacy-backfill" {
				request.PublicationID = "unindexed-root"
				if err := meta.(store.PublicationStore).PublishRegistryPublication(ctx, request); err != nil {
					t.Fatal(err)
				}
				uploads := append([]store.BlobUpload{{Digest: store.ArchiveDigest(request.Occurrence.RegistryRevisionManifestDigest), Data: request.ManifestPayload}}, request.ExtraBlobs...)
				uploads = append(uploads, request.InspectionBlobs...)
				for _, upload := range uploads {
					if err := blobs.Put(ctx, upload.Digest, upload.Data); err != nil {
						t.Fatal(err)
					}
				}
				if _, _, err := meta.(store.BlobOwnerStore).ListBlobOwners(ctx, "managed-test", "manifest", store.ArchiveDigest(collection.ManifestDigest)); !errors.Is(err, store.ErrBlobOwnersIncomplete) {
					t.Fatalf("unindexed positive control: %v", err)
				}
				result, err := store.BackfillBlobOwners(ctx, meta, blobs)
				if err != nil || result.Processed != 4 || result.Remaining {
					t.Fatalf("all seeded kinds did not backfill: %+v %v", result, err)
				}
			} else if err := store.PublishRegistryRevision(ctx, meta, blobs, request); err != nil {
				t.Fatal(err)
			}
			owners, registries, err := meta.(store.BlobOwnerStore).ListBlobOwners(ctx, "managed-test", "manifest", store.ArchiveDigest(collection.ManifestDigest))
			if err != nil || len(owners) != 2 || len(registries) != 1 || owners[0].Coordinate.Kind != "collection" || owners[0].Coordinate.Name != "root" || owners[1].Coordinate.Kind != "skill" || owners[1].Coordinate.Name != "review" {
				t.Fatalf("fresh collection owners: %+v %+v %v", owners, registries, err)
			}
			if owners, _, err := meta.(store.BlobOwnerStore).ListBlobOwners(ctx, "managed-test", "manifest", store.ArchiveDigest(request.Occurrence.RegistryRevisionManifestDigest)); err != nil || len(owners) != 2 {
				t.Fatalf("registry manifest ownership: %+v %v", owners, err)
			}
			for _, assessment := range request.Assessments {
				digest := store.ArchiveDigest(assessment.InspectionManifestDigest)
				if owners, _, err := meta.(store.BlobOwnerStore).ListBlobOwners(ctx, "managed-test", "inspection", digest); err != nil || len(owners) != 1 || owners[0].Coordinate != assessment.Coordinate {
					t.Fatalf("inspection ownership: %+v %v", owners, err)
				}
				payload, err := blobs.Get(ctx, digest)
				if err != nil {
					t.Fatal(err)
				}
				var manifest inspection.Manifest
				if err := json.Unmarshal(payload, &manifest); err != nil {
					t.Fatal(err)
				}
				for _, file := range manifest.Files {
					if file.InspectionClass == "text" || file.InspectionClass == "image" {
						if owners, _, err := meta.(store.BlobOwnerStore).ListBlobOwners(ctx, "managed-test", "content", store.ArchiveDigest(file.ContentDigest)); err != nil || len(owners) == 0 {
							t.Fatalf("body ownership: %+v %v", owners, err)
						}
					}
				}
			}
			result, err := store.BackfillBlobOwners(ctx, meta, blobs)
			if err != nil || result.Processed != 0 || result.Remaining {
				t.Fatalf("permanent pending source after valid publication: %+v %v", result, err)
			}
		})
	}
}

func TestStartedAttemptReplayAfterSiblingPublication(t *testing.T) {
	ctx := context.Background()
	meta, managed := managedStore(t)
	recorder := meta.(store.ManagedRefreshAttemptStore)
	if _, err := managed.ApplyManagedConfiguration(ctx, managedRuntimeRequest(0, "adopt")); err != nil {
		t.Fatal(err)
	}
	_, original := enqueueRuntimeRefresh(t, meta, "original", 1, 1, 1, "", 1)
	if err := recorder.RecordManagedRefreshAttempt(ctx, original, "started", ""); err != nil {
		t.Fatal(err)
	}
	_, sibling := enqueueRuntimeRefresh(t, meta, "sibling", 1, 1, 1, "", 1)
	if err := recorder.RecordManagedRefreshAttempt(ctx, sibling, "started", ""); err != nil {
		t.Fatal(err)
	}
	publication := publicationRequest(t, "sibling-publication", "resource")
	publication.ManagedFence = &sibling
	if err := meta.WithTx(ctx, func(txCtx context.Context, tx store.MetadataStore) error {
		if err := tx.(store.PublicationStore).PublishRegistryPublication(txCtx, publication); err != nil {
			return err
		}
		if err := tx.(store.ManagedRefreshAttemptStore).RecordManagedRefreshAttempt(txCtx, sibling, "succeeded", publication.PublicationID); err != nil {
			return err
		}
		return tx.CompleteJob(txCtx, sibling.JobID, sibling.WorkerID)
	}); err != nil {
		t.Fatal(err)
	}
	before, err := recorder.ListManagedRefreshAttempts(ctx, store.ManagedRefreshAttemptQuery{Marketplace: "managed-test", Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	state, err := recorder.GetManagedRegistryRefreshState(ctx, "managed-test", "registry")
	if err != nil {
		t.Fatal(err)
	}
	if err := recorder.RecordManagedRefreshAttempt(ctx, original, "started", ""); err != nil {
		t.Fatalf("identical started replay rejected after sibling publication: %v", err)
	}
	after, err := recorder.ListManagedRefreshAttempts(ctx, store.ManagedRefreshAttemptQuery{Marketplace: "managed-test", Limit: 100})
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("replay appended/changed events: %+v %v", after, err)
	}
	afterState, err := recorder.GetManagedRegistryRefreshState(ctx, "managed-test", "registry")
	if err != nil || !reflect.DeepEqual(state, afterState) {
		t.Fatalf("replay replaced sibling status: %+v %v", afterState, err)
	}
	_, fresh := enqueueRuntimeRefresh(t, meta, "fresh-stale-head", 1, 1, 1, "", 1)
	fresh.ExpectedPublicationID = ""
	if err := recorder.RecordManagedRefreshAttempt(ctx, fresh, "started", ""); !errors.Is(err, store.ErrPublicationFence) {
		t.Fatalf("new start bypassed head CAS: %v", err)
	}
	stalePublication := publicationRequest(t, "must-not-publish", "other-resource")
	stalePublication.ManagedFence = &original
	if err := meta.(store.PublicationStore).PublishRegistryPublication(ctx, stalePublication); !errors.Is(err, store.ErrPublicationFence) {
		t.Fatalf("audit replay renewed publication permission: %v", err)
	}
	changed := original
	changed.ExpectedPublicationID = publication.PublicationID
	if err := recorder.RecordManagedRefreshAttempt(ctx, changed, "started", ""); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("different fence replay accepted: %v", err)
	}
	pause := managedRuntimeRequest(1, "pause")
	pause.Change.Snapshot = publicationSnapshot(2, 1, true, false, false)
	if _, err := managed.ApplyManagedConfiguration(ctx, pause); err != nil {
		t.Fatal(err)
	}
	if err := recorder.RecordManagedRefreshAttempt(ctx, original, "started", ""); !errors.Is(err, store.ErrPublicationFence) {
		t.Fatalf("replay ignored current generation/lifecycle fence: %v", err)
	}
	if err := meta.ExtendLease(ctx, original.JobID, original.WorkerID, -time.Second); err != nil {
		t.Fatal(err)
	}
	if err := recorder.RecordManagedRefreshAttempt(ctx, original, "started", ""); !errors.Is(err, store.ErrLeaseExpired) {
		t.Fatalf("expired owner replay accepted: %v", err)
	}
}

func TestUnmanagedPublicationCannotBypassDatabaseAuthorityUsingOtherMarketplace(t *testing.T) {
	ctx := context.Background()
	meta, managed := managedStore(t)
	if err := meta.PutMarketplace(ctx, store.Marketplace{ID: "other-marketplace", Name: "Other", CanonicalURL: "https://other.test"}); err != nil {
		t.Fatal(err)
	}
	putBlobOwnershipRegistry(t, meta, "other-registry")
	request := publicationRequest(t, "before-managed")
	request.Occurrence.RegistryID = "other-registry"
	publisher := meta.(store.PublicationStore)
	if err := publisher.PublishRegistryPublication(ctx, request); err != nil {
		t.Fatalf("unmanaged positive control: %v", err)
	}
	if _, err := managed.ApplyManagedConfiguration(ctx, managedRuntimeRequest(0, "adopt")); err != nil {
		t.Fatal(err)
	}
	request.PublicationID = "unfenced-after-adoption"
	request.ExpectedHeadCommit = request.Occurrence.Commit
	if err := publisher.PublishRegistryPublication(ctx, request); !errors.Is(err, store.ErrPublicationFence) {
		t.Fatalf("other marketplace row bypassed database authority: %v", err)
	}
	if _, err := publisher.GetRegistryPublication(ctx, "other-registry", request.PublicationID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("denied publication persisted: %v", err)
	}
	if current, err := publisher.CurrentRegistryPublication(ctx, "other-registry"); err != nil || current.ID != "before-managed" {
		t.Fatalf("unfenced request moved unrelated head: %+v %v", current, err)
	}
}
