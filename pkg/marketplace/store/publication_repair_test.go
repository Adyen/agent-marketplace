package store_test

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/inspection"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/fsblob"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/postgres"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/sqlite"
	"github.com/jackc/pgx/v5"
)

type capturedRepairLogs struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *capturedRepairLogs) Enabled(context.Context, slog.Level) bool { return true }
func (h *capturedRepairLogs) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r)
	return nil
}
func (h *capturedRepairLogs) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *capturedRepairLogs) WithGroup(string) slog.Handler      { return h }

func repairLogCapture(t *testing.T) *capturedRepairLogs {
	t.Helper()
	capture := &capturedRepairLogs{}
	previous := slog.Default()
	slog.SetDefault(slog.New(capture))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return capture
}

func forEachRepairStore(t *testing.T, run func(t *testing.T, meta store.MetadataStore, blobs store.BlobStore)) {
	t.Helper()
	t.Run("sqlite", func(t *testing.T) {
		dir := t.TempDir()
		meta := sqlite.New(filepath.Join(dir, "marketplace.db"))
		if err := meta.Open(context.Background()); err != nil {
			t.Fatalf("Open sqlite metadata store: %v", err)
		}
		t.Cleanup(func() { _ = meta.Close() })
		run(t, meta, fsblob.New(filepath.Join(dir, "blobs")))
	})
	t.Run("postgres", func(t *testing.T) {
		dsn := os.Getenv("POSTGRES_DSN")
		if dsn == "" {
			t.Skip("POSTGRES_DSN not set")
		}
		schema := testSchemaName(t)
		meta := postgres.NewWithSchema(dsn, schema)
		if err := meta.Open(context.Background()); err != nil {
			t.Fatalf("Open PostgreSQL metadata store: %v", err)
		}
		t.Cleanup(func() { _ = meta.Close() })
		t.Cleanup(func() {
			db, err := sql.Open("pgx", dsn)
			if err != nil {
				return
			}
			defer func() { _ = db.Close() }()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_, _ = db.ExecContext(ctx, "DROP SCHEMA IF EXISTS "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
		})
		run(t, meta, fsblob.New(t.TempDir()))
	})
}

func seedRevision(t *testing.T, ctx context.Context, meta store.MetadataStore, coord store.ResourceCoordinate, rev store.ResourceRevision) {
	t.Helper()
	if err := meta.PutResourceRevision(ctx, rev); err != nil {
		t.Fatalf("PutResourceRevision: %v", err)
	}
	manifest := store.RevisionDigest(sha256Text("f"))
	if err := meta.PutRegistryOccurrence(ctx, store.RegistryOccurrence{
		RegistryID: coord.Registry, Ref: "main", Commit: "seed-commit",
		RegistryRevisionManifestDigest: manifest, Acquisition: store.AcquisitionMirror,
	}); err != nil {
		t.Fatalf("PutRegistryOccurrence: %v", err)
	}
	if err := meta.PutResourceOccurrence(ctx, store.ResourceOccurrence{
		Coordinate: coord, ContentDigest: rev.ContentDigest, Commit: "seed-commit", ManifestDigest: manifest,
	}); err != nil {
		t.Fatalf("PutResourceOccurrence: %v", err)
	}
	assessment := validResourceAssessment()
	assessment.Coordinate = coord
	assessment.ContentDigest = rev.ContentDigest
	finalized, err := store.FinalizeResourceAssessment(assessment)
	if err != nil {
		t.Fatalf("FinalizeResourceAssessment: %v", err)
	}
	if err := meta.PutResourceAssessment(ctx, finalized); err != nil {
		t.Fatalf("PutResourceAssessment: %v", err)
	}
	if err := meta.SetCurrentResourceAssessment(ctx, finalized); err != nil {
		t.Fatalf("SetCurrentResourceAssessment: %v", err)
	}
}

// spec: marketplace-publication/drifted-archive-digest-republishes-without-conflict
func TestPublicationRepairsDriftedArchiveDigest(t *testing.T) {
	forEachRepairStore(t, func(t *testing.T, meta store.MetadataStore, blobs store.BlobStore) {
		ctx := context.Background()
		coord := store.ResourceCoordinate{Marketplace: "mp", Registry: "reg", Kind: "skill", Name: "review"}
		rev := store.ResourceRevision{
			Coordinate:    coord,
			ContentDigest: store.RevisionDigest(sha256Text("c")),
			ArchiveDigest: store.ArchiveDigest(sha256Text("a")),
			License:       "MIT",
			Description:   "original",
		}
		seedRevision(t, ctx, meta, coord, rev)
		if err := blobs.Put(ctx, rev.ArchiveDigest, []byte("old archive bytes")); err != nil {
			t.Fatalf("Put old artifact blob: %v", err)
		}

		repaired := rev
		repaired.ArchiveDigest = store.ArchiveDigest(sha256Text("b"))

		capture := repairLogCapture(t)
		if err := meta.PutResourceRevision(ctx, repaired); err != nil {
			t.Fatalf("drifted PutResourceRevision: %v", err)
		}
		if err := blobs.Put(ctx, repaired.ArchiveDigest, []byte("new archive bytes")); err != nil {
			t.Fatalf("Put new artifact blob: %v", err)
		}

		got, err := meta.GetResourceRevision(ctx, coord, rev.ContentDigest)
		if err != nil {
			t.Fatalf("GetResourceRevision: %v", err)
		}
		if got.ArchiveDigest != repaired.ArchiveDigest {
			t.Fatalf("served archive digest = %q, want repaired %q", got.ArchiveDigest, repaired.ArchiveDigest)
		}
		if got.ContentDigest != rev.ContentDigest || got.License != rev.License || got.Description != rev.Description {
			t.Fatalf("repair changed revision identity or metadata: %+v", got.ResourceRevision)
		}
		owners, err := meta.ListResourceRevisionsByArchiveDigest(ctx, coord.Marketplace, rev.ArchiveDigest)
		if err != nil || len(owners) != 1 || owners[0].ContentDigest != rev.ContentDigest {
			t.Fatalf("old archive digest lost ownership linkage: %+v %v", owners, err)
		}
		owners, err = meta.ListResourceRevisionsByArchiveDigest(ctx, coord.Marketplace, repaired.ArchiveDigest)
		if err != nil || len(owners) != 1 || owners[0].ContentDigest != rev.ContentDigest {
			t.Fatalf("new archive digest has no ownership linkage: %+v %v", owners, err)
		}
		oldBlob, err := blobs.Get(ctx, rev.ArchiveDigest)
		if err != nil || string(oldBlob) != "old archive bytes" {
			t.Fatalf("old artifact no longer downloadable: %v", err)
		}

		found := false
		for _, record := range capture.records {
			attrs := map[string]string{}
			record.Attrs(func(a slog.Attr) bool {
				attrs[a.Key] = a.Value.String()
				return true
			})
			if attrs["registry"] == "reg" && attrs["old_archive_digest"] == string(rev.ArchiveDigest) && attrs["new_archive_digest"] == string(repaired.ArchiveDigest) {
				found = true
			}
		}
		if !found {
			t.Fatalf("no structured repair log with old/new digest: %+v", capture.records)
		}
	})
}

// spec: marketplace-publication/drifted-license-or-description-still-conflicts
func TestPublicationDriftedLicenseOrDescriptionStillConflicts(t *testing.T) {
	forEachRepairStore(t, func(t *testing.T, meta store.MetadataStore, blobs store.BlobStore) {
		ctx := context.Background()
		for _, mutate := range []struct {
			name string
			edit func(*store.ResourceRevision)
		}{
			{"license", func(r *store.ResourceRevision) { r.License = "Apache-2.0" }},
			{"description", func(r *store.ResourceRevision) { r.Description = "changed" }},
			{"license-with-archive-drift", func(r *store.ResourceRevision) {
				r.License = "Apache-2.0"
				r.ArchiveDigest = store.ArchiveDigest(sha256Text("d"))
			}},
		} {
			t.Run(mutate.name, func(t *testing.T) {
				coord := store.ResourceCoordinate{Marketplace: "mp", Registry: store.RegistryID("reg-" + mutate.name), Kind: "skill", Name: "review"}
				rev := store.ResourceRevision{
					Coordinate:    coord,
					ContentDigest: store.RevisionDigest(sha256Text("c")),
					ArchiveDigest: store.ArchiveDigest(sha256Text("a")),
					License:       "MIT",
					Description:   "original",
				}
				seedRevision(t, ctx, meta, coord, rev)
				conflicting := rev
				mutate.edit(&conflicting)
				if err := meta.PutResourceRevision(ctx, conflicting); !errors.Is(err, store.ErrConflict) {
					t.Fatalf("conflicting revision error = %v, want ErrConflict", err)
				}
				stored, err := meta.GetLatestResourceRevision(ctx, coord)
				if err != nil {
					t.Fatalf("visible revision unreadable after refused write: %v", err)
				}
				if stored.ResourceRevision != rev {
					t.Fatalf("stored revision changed: %+v, want %+v", stored.ResourceRevision, rev)
				}
			})
		}
	})
}

// spec: marketplace-publication/drifted-license-or-description-still-conflicts
func TestPublicationConflictRollsBackWholePublication(t *testing.T) {
	forEachRepairStore(t, func(t *testing.T, meta store.MetadataStore, blobs store.BlobStore) {
		ctx := context.Background()
		if err := meta.PutRegistry(ctx, store.Registry{ID: "registry", Slug: "registry", Ref: "main", Acquisition: store.AcquisitionMirror, Listed: true}); err != nil {
			t.Fatal(err)
		}
		coord := store.ResourceCoordinate{Marketplace: "mp", Registry: "registry", Kind: "skill", Name: "review"}
		content := store.RevisionDigest(sha256Text("c"))
		body := []byte("# review\n")
		projection, err := inspection.Build(coord, string(content), body, nil, 10, 1024, 4096)
		if err != nil {
			t.Fatal(err)
		}
		inspectionBlobs := []store.BlobUpload{
			{Digest: store.ArchiveDigest(projection.Manifest.ManifestDigest), Data: projection.Payload},
			{Digest: store.ArchiveDigest(inspection.Digest(body)), Data: body},
		}
		oldArchive := store.ArchiveDigest(sha256Digest([]byte("old archive bytes")))
		first := mirrorPublication(t, coord, content, oldArchive, "commit-1", projection, inspectionBlobs)
		first.PublicationID = "pub-1"
		first.Resources[0].License = "MIT"
		first.ExtraBlobs = []store.BlobUpload{{Digest: oldArchive, Data: []byte("old archive bytes")}}
		if err := store.PublishRegistryRevision(ctx, meta, blobs, first); err != nil {
			t.Fatalf("first publication: %v", err)
		}

		newArchive := store.ArchiveDigest(sha256Digest([]byte("new archive bytes")))
		second := mirrorPublication(t, coord, content, newArchive, "commit-2", projection, inspectionBlobs)
		second.PublicationID = "pub-2"
		second.Resources[0].License = "Apache-2.0"
		second.ExtraBlobs = []store.BlobUpload{{Digest: newArchive, Data: []byte("new archive bytes")}}
		second.ExpectedHeadCommit = first.Occurrence.Commit
		if err := store.PublishRegistryRevision(ctx, meta, blobs, second); !errors.Is(err, store.ErrConflict) {
			t.Fatalf("drifted license publication error = %v, want ErrConflict", err)
		}
		stored, err := meta.GetLatestResourceRevision(ctx, coord)
		if err != nil {
			t.Fatalf("visible revision unreadable after rolled-back publication: %v", err)
		}
		if stored.ArchiveDigest != oldArchive || stored.License != "MIT" {
			t.Fatalf("rolled-back publication changed the stored revision: %+v", stored.ResourceRevision)
		}
		if _, err := meta.GetRegistryOccurrence(ctx, "registry", "commit-2"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("rolled-back publication left occurrence history: %v", err)
		}
		publisher, ok := meta.(store.PublicationStore)
		if !ok {
			t.Fatal("metadata store does not expose publication records")
		}
		if _, err := publisher.GetRegistryPublication(ctx, "registry", "pub-2"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("rolled-back publication retained history: %v", err)
		}
		current, err := publisher.CurrentRegistryPublication(ctx, "registry")
		if err != nil || current.ID != "pub-1" {
			t.Fatalf("current publication after rollback: %+v %v", current, err)
		}
	})
}

// spec: marketplace-publication/drifted-archive-digest-republishes-without-conflict
func TestRegistryOccurrenceRepairsDriftedMirroredDigest(t *testing.T) {
	forEachRepairStore(t, func(t *testing.T, meta store.MetadataStore, _ store.BlobStore) {
		ctx := context.Background()
		if err := meta.PutRegistry(ctx, store.Registry{ID: "reg", Slug: "reg", Ref: "main", Acquisition: store.AcquisitionMirror, Listed: true}); err != nil {
			t.Fatal(err)
		}
		occ := store.RegistryOccurrence{
			RegistryID: "reg", Ref: "main", Commit: "0123456789abcdef0123456789abcdef01234567",
			RegistryRevisionManifestDigest: store.RevisionDigest(sha256Text("e")),
			Acquisition:                    store.AcquisitionMirror,
			MirroredArtifactDigest:         store.ArchiveDigest(sha256Text("a")),
		}
		if err := meta.PutRegistryOccurrence(ctx, occ); err != nil {
			t.Fatalf("PutRegistryOccurrence: %v", err)
		}
		repaired := occ
		repaired.MirroredArtifactDigest = store.ArchiveDigest(sha256Text("b"))
		repaired.RegistryRevisionManifestDigest = store.RevisionDigest(sha256Text("d"))
		if err := meta.PutRegistryOccurrence(ctx, repaired); err != nil {
			t.Fatalf("drifted PutRegistryOccurrence: %v", err)
		}
		got, err := meta.GetRegistryOccurrence(ctx, "reg", occ.Commit)
		if err != nil {
			t.Fatalf("GetRegistryOccurrence: %v", err)
		}
		if got.MirroredArtifactDigest != repaired.MirroredArtifactDigest {
			t.Fatalf("mirrored artifact digest = %q, want repaired %q", got.MirroredArtifactDigest, repaired.MirroredArtifactDigest)
		}
		if got.RegistryRevisionManifestDigest != repaired.RegistryRevisionManifestDigest {
			t.Fatalf("manifest digest = %q, want repaired %q", got.RegistryRevisionManifestDigest, repaired.RegistryRevisionManifestDigest)
		}
		changed := repaired
		changed.Roots = []store.RegistryRoot{{Path: ".other"}}
		if err := meta.PutRegistryOccurrence(ctx, changed); !errors.Is(err, store.ErrConflict) {
			t.Fatalf("conflicting occurrence error = %v, want ErrConflict", err)
		}
	})
}

// spec: marketplace-api/same-content-revision-carries-a-repaired-archive-digest
func TestPublicationServesRepairedArchiveDigestAndRetainsOldArtifact(t *testing.T) {
	forEachRepairStore(t, func(t *testing.T, meta store.MetadataStore, blobs store.BlobStore) {
		ctx := context.Background()
		if err := meta.PutRegistry(ctx, store.Registry{ID: "registry", Slug: "registry", Ref: "main", Acquisition: store.AcquisitionMirror, Listed: true}); err != nil {
			t.Fatal(err)
		}
		coord := store.ResourceCoordinate{Marketplace: "mp", Registry: "registry", Kind: "skill", Name: "review"}
		content := store.RevisionDigest(sha256Text("c"))
		body := []byte("# review\n")
		projection, err := inspection.Build(coord, string(content), body, nil, 10, 1024, 4096)
		if err != nil {
			t.Fatal(err)
		}
		inspectionBlobs := []store.BlobUpload{
			{Digest: store.ArchiveDigest(projection.Manifest.ManifestDigest), Data: projection.Payload},
			{Digest: store.ArchiveDigest(inspection.Digest(body)), Data: body},
		}

		oldArchive := store.ArchiveDigest(sha256Digest([]byte("old archive bytes")))
		first := mirrorPublication(t, coord, content, oldArchive, "commit-1", projection, inspectionBlobs)
		first.ExtraBlobs = []store.BlobUpload{{Digest: oldArchive, Data: []byte("old archive bytes")}}
		if err := store.PublishRegistryRevision(ctx, meta, blobs, first); err != nil {
			t.Fatalf("first publication: %v", err)
		}
		got, err := meta.GetResourceRevision(ctx, coord, content)
		if err != nil || got.ArchiveDigest != oldArchive {
			t.Fatalf("initial revision: %+v %v", got, err)
		}

		newArchive := store.ArchiveDigest(sha256Digest([]byte("new archive bytes")))
		second := mirrorPublication(t, coord, content, newArchive, "commit-2", projection, inspectionBlobs)
		second.ExtraBlobs = []store.BlobUpload{{Digest: newArchive, Data: []byte("new archive bytes")}}
		second.ExpectedHeadCommit = first.Occurrence.Commit
		if err := store.PublishRegistryRevision(ctx, meta, blobs, second); err != nil {
			t.Fatalf("repaired publication: %v", err)
		}
		got, err = meta.GetResourceRevision(ctx, coord, content)
		if err != nil || got.ArchiveDigest != newArchive {
			t.Fatalf("served archive digest = %q, want repaired %q (%v)", got.ArchiveDigest, newArchive, err)
		}
		if data, err := blobs.Get(ctx, oldArchive); err != nil || string(data) != "old archive bytes" {
			t.Fatalf("previously published artifact lost: %v", err)
		}
		if data, err := blobs.Get(ctx, newArchive); err != nil || string(data) != "new archive bytes" {
			t.Fatalf("new artifact not downloadable: %v", err)
		}
		owners, err := meta.ListResourceRevisionsByArchiveDigest(ctx, coord.Marketplace, oldArchive)
		if err != nil || len(owners) != 1 || owners[0].ContentDigest != content {
			t.Fatalf("old artifact lost durable ownership: %+v %v", owners, err)
		}
	})
}

func mirrorPublication(t *testing.T, coord store.ResourceCoordinate, content store.RevisionDigest, archive store.ArchiveDigest, commit string, projection inspection.Projection, inspectionBlobs []store.BlobUpload) store.RegistryRevisionPublication {
	t.Helper()
	rev := store.ResourceRevision{Coordinate: coord, ContentDigest: content, ArchiveDigest: archive}
	manifest := []byte(`{"schema_version":"v1","generator":"test"}`)
	manifestDigest := store.RevisionDigest(sha256Digest(manifest))
	return store.RegistryRevisionPublication{
		Occurrence: store.RegistryOccurrence{
			RegistryID: coord.Registry, Ref: "main", Commit: commit,
			RegistryRevisionManifestDigest: manifestDigest,
			Acquisition:                    store.AcquisitionMirror,
			MirroredArtifactDigest:         archive,
		},
		Resources:           []store.ResourceRevision{rev},
		ResourceOccurrences: []store.ResourceOccurrence{{Coordinate: coord, ContentDigest: content, Commit: commit, ManifestDigest: manifestDigest}},
		Assessments:         []store.ResourceAssessment{assessmentFor(t, rev, projection.Manifest.ManifestDigest)},
		ManifestPayload:     manifest,
		InspectionBlobs:     inspectionBlobs,
	}
}
