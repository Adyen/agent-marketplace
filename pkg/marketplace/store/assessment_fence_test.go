package store_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/artifact"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/inspection"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/fsblob"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/sqlite"
)

// spec: marketplace-publication/stale-same-commit-assessment-is-refused
// spec: marketplace-publication/valid-same-commit-retry-preserves-the-assessment
func TestStaleSameCommitPublicationDoesNotReplaceCurrentAssessment(t *testing.T) {
	ctx := context.Background()
	meta, _ := managedStore(t)
	blobs := fsblob.New(t.TempDir())
	coordinate := store.ResourceCoordinate{Marketplace: "managed-test", Registry: "assessment-registry", Kind: "skill", Name: "review"}
	if err := meta.PutRegistry(ctx, store.Registry{ID: coordinate.Registry, Slug: string(coordinate.Registry), Ref: "main", Listed: true, Acquisition: store.AcquisitionIndexOnly}); err != nil {
		t.Fatal(err)
	}
	revision := store.RevisionDigest("sha256:" + strings.Repeat("a", 64))
	body := []byte("# review\n")
	projection, err := inspection.Build(coordinate, string(revision), body, nil, 10, 1024, 4096)
	if err != nil {
		t.Fatal(err)
	}
	manifestPayload, manifestDigest, err := artifact.CanonicalRegistryRevisionPayload(artifact.RegistryRevisionManifest{SchemaVersion: "v1", Generator: artifact.RegistryRevisionGeneratorV1, Resources: []artifact.RevisionResource{{Kind: coordinate.Kind, Name: coordinate.Name, ContentDigest: string(revision)}}})
	if err != nil {
		t.Fatal(err)
	}
	publication := func(decision store.AssessmentDecision, salt, expected string) store.RegistryRevisionPublication {
		assessment, err := store.FinalizeResourceAssessment(store.ResourceAssessment{
			Coordinate: coordinate, ContentDigest: revision, AnalyzerVersion: "test/v1",
			PolicyDigest: store.PolicyDigest("sha256:" + strings.Repeat(salt, 64)), Decision: decision,
			InspectionManifestDigest: store.RevisionDigest(projection.Manifest.ManifestDigest), AssessedAt: time.Unix(1704067200, 0).UTC(),
		})
		if err != nil {
			t.Fatal(err)
		}
		manifest := store.RevisionDigest(manifestDigest)
		return store.RegistryRevisionPublication{
			Occurrence:          store.RegistryOccurrence{RegistryID: coordinate.Registry, Ref: "main", Commit: "commit-current", Acquisition: store.AcquisitionIndexOnly, RegistryRevisionManifestDigest: manifest},
			Resources:           []store.ResourceRevision{{Coordinate: coordinate, ContentDigest: revision}},
			ResourceOccurrences: []store.ResourceOccurrence{{Coordinate: coordinate, ContentDigest: revision, Commit: "commit-current", ManifestDigest: manifest, SourcePath: "skills/review"}},
			Assessments:         []store.ResourceAssessment{assessment},
			InspectionBlobs: []store.BlobUpload{
				{Digest: store.ArchiveDigest(projection.Manifest.ManifestDigest), Data: projection.Payload},
				{Digest: store.ArchiveDigest(inspection.Digest(body)), Data: body},
			},
			ExpectedHeadCommit: expected,
			ManifestPayload:    manifestPayload,
		}
	}
	fresh := publication(store.AssessmentDecisionReject, "c", "")
	if err := store.PublishRegistryRevision(ctx, meta, blobs, fresh); err != nil {
		t.Fatalf("first publication failed: %v", err)
	}
	current, err := meta.GetResourceRevision(ctx, coordinate, revision)
	if err != nil || current.Assessment.Decision != store.AssessmentDecisionReject {
		t.Fatalf("reject control not established: %+v %v", current, err)
	}
	head, err := meta.LatestRegistryOccurrence(ctx, coordinate.Registry)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"commit-outdated", ""} {
		stale := publication(store.AssessmentDecisionAllow, "b", expected)
		err := store.PublishRegistryRevision(ctx, meta, blobs, stale)
		if !errors.Is(err, store.ErrConflict) {
			t.Fatalf("stale publication with expected head %q was not refused: %v", expected, err)
		}
		after, err := meta.GetResourceRevision(ctx, coordinate, revision)
		if err != nil || !reflect.DeepEqual(after, current) {
			t.Fatalf("stale publication changed the current resource: %+v %v", after, err)
		}
		afterHead, err := meta.LatestRegistryOccurrence(ctx, coordinate.Registry)
		if err != nil || !afterHead.Equal(head) {
			t.Fatalf("stale publication changed the head: %+v %v", afterHead, err)
		}
		assessments, err := meta.ListResourceAssessments(ctx, coordinate, revision)
		if err != nil || len(assessments) != 1 {
			t.Fatalf("conflicting assessment survived rollback: %+v %v", assessments, err)
		}
	}
	for _, change := range []string{"license", "occurrence", "origin", "extra-resource"} {
		stale := publication(store.AssessmentDecisionReject, "c", "commit-outdated")
		switch change {
		case "license":
			stale.Resources[0].License = "Apache-2.0"
		case "occurrence":
			stale.ResourceOccurrences[0].SourcePath = "different/review"
		case "origin":
			stale.Occurrence.SourceOrigin = "https://different.example.invalid/registry.git"
		case "extra-resource":
			extra := stale.Resources[0]
			extra.Coordinate.Name = "extra"
			stale.Resources = append(stale.Resources, extra)
		}
		if err := store.PublishRegistryRevision(ctx, meta, blobs, stale); !errors.Is(err, store.ErrConflict) {
			t.Fatalf("stale changed %s was accepted as a no-op: %v", change, err)
		}
	}
	for _, expected := range []string{head.Commit, "", "commit-outdated"} {
		retry := publication(store.AssessmentDecisionReject, "c", expected)
		if err := store.PublishRegistryRevision(ctx, meta, blobs, retry); err != nil {
			t.Fatalf("already-current retry with expected head %q failed: %v", expected, err)
		}
		after, err := meta.GetResourceRevision(ctx, coordinate, revision)
		if err != nil || !reflect.DeepEqual(after, current) {
			t.Fatalf("verified retry changed the assessment: %+v %v", after, err)
		}
	}
}

func BenchmarkLegacyRegistryPublicationDisk(b *testing.B) {
	logger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	b.Cleanup(func() { slog.SetDefault(logger) })
	ctx := context.Background()
	meta := sqlite.New(filepath.Join(b.TempDir(), "metadata.db"))
	if err := meta.Open(ctx); err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = meta.Close() })
	if err := meta.PutRegistry(ctx, store.Registry{ID: "registry", Slug: "registry", Ref: "main", Acquisition: store.AcquisitionIndexOnly}); err != nil {
		b.Fatal(err)
	}
	blobs := fsblob.New(b.TempDir())
	manifest := []byte(`{"schema_version":"v1","generator":"agent-marketplace registry-revision/v1","resources":[]}`)
	request := store.RegistryRevisionPublication{
		Occurrence:      store.RegistryOccurrence{RegistryID: "registry", Ref: "main", Commit: "commit-current", Acquisition: store.AcquisitionIndexOnly, RegistryRevisionManifestDigest: store.RevisionDigest(sha256Digest(manifest))},
		ManifestPayload: manifest,
	}
	if err := store.PublishRegistryRevision(ctx, meta, blobs, request); err != nil {
		b.Fatal(err)
	}
	request.ExpectedHeadCommit = request.Occurrence.Commit
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if err := store.PublishRegistryRevision(ctx, meta, blobs, request); err != nil {
			b.Fatal(err)
		}
	}
}
