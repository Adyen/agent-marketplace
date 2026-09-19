package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func publicationRequest(t testing.TB, id store.PublicationID, names ...string) store.RegistryRevisionPublication {
	t.Helper()
	req := store.RegistryRevisionPublication{PublicationID: id, Occurrence: store.RegistryOccurrence{RegistryID: "registry", Commit: "same-real-git-commit", Ref: "main", Roots: []store.RegistryRoot{{Path: string(id)}}, Acquisition: store.AcquisitionIndexOnly, RegistryRevisionManifestDigest: store.RevisionDigest("manifest-" + string(id))}}
	for _, name := range names {
		coordinate := store.ResourceCoordinate{Marketplace: "managed-test", Registry: "registry", Kind: "skill", Name: name}
		digest := store.RevisionDigest(sha256Text("a"))
		req.Resources = append(req.Resources, store.ResourceRevision{Coordinate: coordinate, ContentDigest: digest, License: "MIT"})
		req.ResourceOccurrences = append(req.ResourceOccurrences, store.ResourceOccurrence{Coordinate: coordinate, ContentDigest: digest, Commit: req.Occurrence.Commit, ManifestDigest: req.Occurrence.RegistryRevisionManifestDigest, SourcePath: string(id) + "/" + name})
		assessment := validResourceAssessment()
		assessment.Coordinate = coordinate
		assessment.ContentDigest = digest
		assessment, err := store.FinalizeResourceAssessment(assessment)
		if err != nil {
			t.Fatal(err)
		}
		req.Assessments = append(req.Assessments, assessment)
	}
	return req
}

func TestPublishRegistryRevisionExplicitIdentity(t *testing.T) {
	ctx := context.Background()
	meta, blobs := openAtomicStores(t)
	if err := meta.PutRegistry(ctx, store.Registry{ID: "registry", Slug: "registry", Ref: "main", Acquisition: store.AcquisitionIndexOnly, Listed: true}); err != nil {
		t.Fatal(err)
	}
	req := publicationRequest(t, "empty-publication")
	req.ManifestPayload = []byte(`{"resources":[]}`)
	req.Occurrence.RegistryRevisionManifestDigest = store.RevisionDigest(sha256Digest(req.ManifestPayload))
	if err := store.PublishRegistryRevision(ctx, meta, blobs, req); err != nil {
		t.Fatal(err)
	}
	current, err := meta.CurrentRegistryPublication(ctx, "registry")
	if err != nil || current.ID != req.PublicationID {
		t.Fatalf("explicit publication missing: %+v %v", current, err)
	}
	payload, err := blobs.Get(ctx, store.ArchiveDigest(current.Occurrence.RegistryRevisionManifestDigest))
	if err != nil || string(payload) != string(req.ManifestPayload) {
		t.Fatalf("manifest unavailable: %s %v", payload, err)
	}
}

func TestPublicationSameCommitMembership(t *testing.T) {
	ctx := context.Background()
	meta, _ := managedStore(t)
	publisher := meta.(store.PublicationStore)
	if err := meta.PutRegistry(ctx, store.Registry{ID: "registry", Slug: "registry", Ref: "main", Acquisition: store.AcquisitionIndexOnly, Listed: true}); err != nil {
		t.Fatal(err)
	}
	first := publicationRequest(t, "first", "removed", "retained")
	if err := publisher.PublishRegistryPublication(ctx, first); err != nil {
		t.Fatal(err)
	}
	old, err := publisher.GetRegistryPublication(ctx, "registry", "first")
	if err != nil {
		t.Fatal(err)
	}
	second := publicationRequest(t, "second", "retained", "added")
	second.ExpectedHeadCommit = first.Occurrence.Commit
	second.Assessments[0].AssessmentDigest = ""
	second.Assessments[0].PolicyDigest = store.PolicyDigest(sha256Text("f"))
	second.Assessments[0].Decision = store.AssessmentDecisionReject
	if err := publisher.PublishRegistryPublication(ctx, second); err != nil {
		t.Fatal(err)
	}
	after, err := publisher.GetRegistryPublication(ctx, "registry", "first")
	if err != nil || !reflect.DeepEqual(old, after) {
		t.Fatalf("history changed: %+v %v", after, err)
	}
	current, err := publisher.CurrentRegistryPublication(ctx, "registry")
	if err != nil || current.ID != "second" || current.Occurrence.Commit != first.Occurrence.Commit {
		t.Fatalf("current: %+v %v", current, err)
	}
	latest, err := meta.LatestRegistryOccurrence(ctx, "registry")
	if err != nil || latest.RegistryRevisionManifestDigest != "manifest-second" {
		t.Fatalf("latest: %+v %v", latest, err)
	}
	removed := first.Resources[0]
	if _, err := meta.GetLatestResourceRevision(ctx, removed.Coordinate); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("removed resource current: %v", err)
	}
	exact, err := meta.GetResourceRevision(ctx, removed.Coordinate, removed.ContentDigest)
	if err != nil || exact.Occurrence.ManifestDigest != "manifest-first" || exact.Occurrence.Commit != first.Occurrence.Commit {
		t.Fatalf("historical exact lost: %+v %v", exact, err)
	}
	found, err := meta.SearchResourceRevisions(ctx, store.ResourceSearchFilter{Marketplace: "managed-test"})
	if err != nil || len(found) != 2 {
		t.Fatalf("search: %+v %v", found, err)
	}
	page, err := meta.ReadCatalogProjectionPage(ctx, store.CatalogProjectionPageRequest{Marketplace: "managed-test", Limit: 10})
	if err != nil || len(page.Resources) != 2 {
		t.Fatalf("projection: %+v %v", page, err)
	}
	for _, r := range page.Resources {
		if r.Coordinate.Name == "removed" || r.Occurrence.ManifestDigest != "manifest-second" {
			t.Fatalf("stale projection: %+v", r)
		}
		if r.Coordinate.Name == "retained" && r.Assessment.Decision != store.AssessmentDecisionReject {
			t.Fatalf("stale assessment: %+v", r)
		}
	}
	second.Resources[0].License = "Apache-2.0"
	if err := publisher.PublishRegistryPublication(ctx, second); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("immutable metadata rewrite: %v", err)
	}
}

func TestPublicationSameCommitPolicyOnly(t *testing.T) {
	ctx := context.Background()
	meta, _ := managedStore(t)
	publisher := meta.(store.PublicationStore)
	if err := meta.PutRegistry(ctx, store.Registry{ID: "registry", Slug: "registry", Ref: "main", Acquisition: store.AcquisitionIndexOnly, Listed: true}); err != nil {
		t.Fatal(err)
	}
	first := publicationRequest(t, "policy-one", "unchanged")
	if err := publisher.PublishRegistryPublication(ctx, first); err != nil {
		t.Fatal(err)
	}
	second := first
	second.PublicationID = "policy-two"
	second.Assessments = append([]store.ResourceAssessment(nil), first.Assessments...)
	second.Assessments[0].AssessmentDigest = ""
	second.Assessments[0].PolicyDigest = store.PolicyDigest(sha256Text("f"))
	second.Assessments[0].Decision = store.AssessmentDecisionReject
	if err := publisher.PublishRegistryPublication(ctx, second); err != nil {
		t.Fatal(err)
	}
	current, err := meta.GetLatestResourceRevision(ctx, first.Resources[0].Coordinate)
	if err != nil || current.Assessment.Decision != store.AssessmentDecisionReject || current.Occurrence.ManifestDigest != first.Occurrence.RegistryRevisionManifestDigest {
		t.Fatalf("policy-only publication: %+v %v", current, err)
	}
	historical, err := publisher.GetRegistryPublication(ctx, "registry", "policy-one")
	if err != nil || historical.Assessments[0].AssessmentDigest != first.Assessments[0].AssessmentDigest {
		t.Fatalf("historical assessment changed: %+v %v", historical, err)
	}
}

func publicationSnapshot(generation, policy int64, paused, withdrawn, retired bool) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{"version":1,"policy_revision":%d,"registries":[{"id":"registry","slug":"registry","origin":"https://example.test/registry.git","ref":"main","acquisition":"index-only","generation":%d,"lifecycle":"active","paused":%t,"withdrawn":%t,"retired":%t}]}`, policy, generation, paused, withdrawn, retired))
}

func TestPublicationManagedFence(t *testing.T) {
	for _, scenario := range []string{"generation", "policy", "paused", "withdrawn", "retired", "worker", "attempt", "expired", "expected", "missing"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			meta, managed := managedStore(t)
			publisher := meta.(store.PublicationStore)
			if err := meta.PutRegistry(ctx, store.Registry{ID: "registry", Slug: "registry", Ref: "main", Acquisition: store.AcquisitionIndexOnly, Listed: true}); err != nil {
				t.Fatal(err)
			}
			adopt := managedRequest(0, "adopt")
			adopt.Change.Snapshot = publicationSnapshot(1, 1, false, false, false)
			if _, err := managed.ApplyManagedConfiguration(ctx, adopt); err != nil {
				t.Fatal(err)
			}
			job, err := meta.LeaseJobByID(ctx, "job-adopt", "worker", time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			req := publicationRequest(t, "first", "original")
			req.ManagedFence = &store.ManagedFence{Marketplace: "managed-test", RegistryGeneration: 1, PolicyRevision: 1, JobID: job.ID, WorkerID: "worker", Attempt: job.Attempt}
			if err := publisher.PublishRegistryPublication(ctx, req); err != nil {
				t.Fatalf("positive control: %v", err)
			}
			req = publicationRequest(t, "stale", "unreachable")
			req.ManagedFence = &store.ManagedFence{Marketplace: "managed-test", RegistryGeneration: 1, PolicyRevision: 1, JobID: job.ID, WorkerID: "worker", Attempt: job.Attempt, ExpectedPublicationID: "first"}
			switch scenario {
			case "generation", "policy", "paused", "withdrawn", "retired":
				change := managedRequest(1, "edit")
				g, p := int64(1), int64(1)
				if scenario == "generation" || scenario == "paused" || scenario == "withdrawn" || scenario == "retired" {
					g = 2
				}
				if scenario == "paused" || scenario == "withdrawn" || scenario == "retired" {
					req.ManagedFence.RegistryGeneration = g
				}
				if scenario == "policy" {
					p = 2
				}
				change.Change.Snapshot = publicationSnapshot(g, p, scenario == "paused", scenario == "withdrawn", scenario == "retired")
				if _, err := managed.ApplyManagedConfiguration(ctx, change); err != nil {
					t.Fatal(err)
				}
			case "worker":
				req.ManagedFence.WorkerID = "another"
			case "attempt":
				req.ManagedFence.Attempt++
			case "expired":
				if err := meta.EnqueueJob(ctx, store.Job{ID: "short", Kind: "short", Payload: []byte(`{}`), MaxAttempts: 2}); err != nil {
					t.Fatal(err)
				}
				short, err := meta.LeaseJobByID(ctx, "short", "worker", time.Nanosecond)
				if err != nil {
					t.Fatal(err)
				}
				req.ManagedFence.JobID = short.ID
				req.ManagedFence.Attempt = short.Attempt
			case "expected":
				req.ManagedFence.ExpectedPublicationID = "wrong"
			case "missing":
				req.ManagedFence = nil
			}
			before, err := meta.ReadCatalogProjectionPage(ctx, store.CatalogProjectionPageRequest{Marketplace: "managed-test", Limit: 10})
			if err != nil {
				t.Fatal(err)
			}
			if err := publisher.PublishRegistryPublication(ctx, req); !errors.Is(err, store.ErrPublicationFence) {
				t.Fatalf("stale publication accepted: %v", err)
			}
			after, err := meta.ReadCatalogProjectionPage(ctx, store.CatalogProjectionPageRequest{Marketplace: "managed-test", Limit: 10})
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatalf("failed fence changed projection: %+v %v", after, err)
			}
			if _, err := publisher.GetRegistryPublication(ctx, "registry", "stale"); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("failed fence retained publication: %v", err)
			}
			if _, err := meta.GetResourceRevision(ctx, req.Resources[0].Coordinate, req.Resources[0].ContentDigest); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("failed fence exposed resource: %v", err)
			}
		})
	}
}

func TestPublicationCompareAndSwapConcurrent(t *testing.T) {
	ctx := context.Background()
	meta, managed := managedStore(t)
	publisher := meta.(store.PublicationStore)
	if err := meta.PutRegistry(ctx, store.Registry{ID: "registry", Slug: "registry", Ref: "main", Acquisition: store.AcquisitionIndexOnly, Listed: true}); err != nil {
		t.Fatal(err)
	}
	adopt := managedRequest(0, "adopt")
	adopt.Change.Snapshot = publicationSnapshot(1, 1, false, false, false)
	if _, err := managed.ApplyManagedConfiguration(ctx, adopt); err != nil {
		t.Fatal(err)
	}
	job, err := meta.LeaseJobByID(ctx, "job-adopt", "worker", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, id := range []store.PublicationID{"one", "two"} {
		req := publicationRequest(t, id, string(id))
		req.ManagedFence = &store.ManagedFence{Marketplace: "managed-test", RegistryGeneration: 1, PolicyRevision: 1, JobID: job.ID, WorkerID: "worker", Attempt: job.Attempt}
		go func() { <-start; results <- publisher.PublishRegistryPublication(ctx, req) }()
	}
	close(start)
	succeeded, fenced := 0, 0
	for range 2 {
		err := <-results
		if err == nil {
			succeeded++
		} else if errors.Is(err, store.ErrPublicationFence) {
			fenced++
		} else {
			t.Fatal(err)
		}
	}
	if succeeded != 1 || fenced != 1 {
		t.Fatalf("CAS winners=%d fenced=%d", succeeded, fenced)
	}
	page, err := meta.ReadCatalogProjectionPage(ctx, store.CatalogProjectionPageRequest{Marketplace: "managed-test", Limit: 10})
	if err != nil || len(page.Resources) != 1 {
		t.Fatalf("partial CAS publication: %+v %v", page, err)
	}
}

func TestFindManagedChangeScope(t *testing.T) {
	ctx := context.Background()
	_, managed := managedStore(t)
	req := managedRequest(0, "key")
	accepted, err := managed.ApplyManagedConfiguration(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	got, err := managed.FindManagedChange(ctx, "managed-test", req.Change.Actor, req.Change.Operation, "key")
	if err != nil || !reflect.DeepEqual(got, accepted) {
		t.Fatalf("lookup: %+v %v", got, err)
	}
	for _, actor := range []store.ManagedActor{{Issuer: "other", Subject: req.Change.Actor.Subject}, {Issuer: req.Change.Actor.Issuer, Subject: "other"}} {
		if _, err := managed.FindManagedChange(ctx, "managed-test", actor, req.Change.Operation, "key"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("actor scope crossed: %v", err)
		}
	}
	if _, err := managed.FindManagedChange(ctx, "managed-test", req.Change.Actor, "other", "key"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("operation scope crossed: %v", err)
	}
}
