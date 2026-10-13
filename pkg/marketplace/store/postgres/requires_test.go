package postgres_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/postgres"
	"github.com/jackc/pgx/v5"
)

const testRequiresJSON = `[{"kind":"skill","name":"helper"},{"kind":"command","name":"lint","registry":"other","path":"shared"}]`

func seedRequiresStore(t *testing.T) *postgres.Store {
	t.Helper()
	ctx := context.Background()
	s, _ := newTestStore(t)
	if err := s.PutMarketplace(ctx, store.Marketplace{ID: "mp", Name: "Marketplace"}); err != nil {
		t.Fatalf("PutMarketplace: %v", err)
	}
	if err := s.PutRegistry(ctx, store.Registry{ID: "reg", Slug: "reg", Listed: true, Acquisition: store.AcquisitionMirror}); err != nil {
		t.Fatalf("PutRegistry: %v", err)
	}
	return s
}

// putCurrentRevision seeds a resource revision with its registry occurrence,
// resource occurrence, and a current assessment so it is head-visible.
func putCurrentRevision(t *testing.T, s *postgres.Store, kind, name, commit, manifest string, roots []store.RegistryRoot, sourcePath, requires string) store.ResourceRevision {
	t.Helper()
	ctx := context.Background()
	coord := store.ResourceCoordinate{Marketplace: "mp", Registry: "reg", Kind: kind, Name: name}
	rev := store.ResourceRevision{
		Coordinate:    coord,
		ContentDigest: store.RevisionDigest(digestOf("a")),
		Requires:      requires,
	}
	if err := s.PutResourceRevision(ctx, rev); err != nil {
		t.Fatalf("PutResourceRevision: %v", err)
	}
	if err := s.PutRegistryOccurrence(ctx, store.RegistryOccurrence{
		RegistryID: coord.Registry, Ref: "main", Commit: commit, Roots: roots,
		RegistryRevisionManifestDigest: store.RevisionDigest(manifest),
		Acquisition:                    store.AcquisitionMirror,
	}); err != nil {
		t.Fatalf("PutRegistryOccurrence: %v", err)
	}
	if err := s.PutResourceOccurrence(ctx, store.ResourceOccurrence{
		Coordinate: coord, ContentDigest: rev.ContentDigest, Commit: commit,
		ManifestDigest: store.RevisionDigest(manifest), SourcePath: sourcePath,
	}); err != nil {
		t.Fatalf("PutResourceOccurrence: %v", err)
	}
	assessment, err := store.FinalizeResourceAssessment(store.ResourceAssessment{
		Coordinate: coord, ContentDigest: rev.ContentDigest, AnalyzerVersion: "analyzer/v1",
		PolicyDigest: store.PolicyDigest(digestOf("d")), Decision: store.AssessmentDecisionAllow,
		InspectionManifestDigest: store.RevisionDigest(digestOf("e")), AssessedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("FinalizeResourceAssessment: %v", err)
	}
	if err := s.PutResourceAssessment(ctx, assessment); err != nil {
		t.Fatalf("PutResourceAssessment: %v", err)
	}
	if err := s.SetCurrentResourceAssessment(ctx, assessment); err != nil {
		t.Fatalf("SetCurrentResourceAssessment: %v", err)
	}
	return rev
}

// spec: marketplace-publication/declared-prerequisites-are-indexed-in-declaration-order
// spec: marketplace-publication/revision-published-before-indexing-stays-unindexed
// spec: marketplace-publication/conflicting-prerequisites-for-an-indexed-revision-are-rejected
func TestRequiresStoredRepairedAndConflicting(t *testing.T) {
	ctx := context.Background()
	s := seedRequiresStore(t)
	rev := putCurrentRevision(t, s, "skill", "review", "commit", digestOf("c"), nil, "", "")

	got, err := s.GetResourceRevision(ctx, rev.Coordinate, rev.ContentDigest)
	if err != nil {
		t.Fatal(err)
	}
	if got.Requires != "" {
		t.Fatalf("requires before indexing = %q, want unindexed", got.Requires)
	}

	// A later publication of the same content records its prerequisites.
	indexed := rev
	indexed.Requires = testRequiresJSON
	if err := s.PutResourceRevision(ctx, indexed); err != nil {
		t.Fatalf("re-publish fills unindexed requires: %v", err)
	}
	got, err = s.GetResourceRevision(ctx, rev.Coordinate, rev.ContentDigest)
	if err != nil {
		t.Fatal(err)
	}
	if got.Requires != testRequiresJSON {
		t.Fatalf("requires = %q, want %q", got.Requires, testRequiresJSON)
	}

	// Re-publication of the same indexed value is idempotent.
	if err := s.PutResourceRevision(ctx, indexed); err != nil {
		t.Fatalf("idempotent re-publish: %v", err)
	}

	// A different indexed value conflicts as immutable metadata.
	conflicting := rev
	conflicting.Requires = `[{"kind":"skill","name":"other"}]`
	err = s.PutResourceRevision(ctx, conflicting)
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("conflicting requires error = %v, want ErrConflict", err)
	}
	got, err = s.GetResourceRevision(ctx, rev.Coordinate, rev.ContentDigest)
	if err != nil {
		t.Fatal(err)
	}
	if got.Requires != testRequiresJSON {
		t.Fatalf("requires after conflict = %q, want %q", got.Requires, testRequiresJSON)
	}
}

// spec: marketplace-publication/declared-prerequisites-are-indexed-in-declaration-order
func TestEmptyPrerequisiteListRoundTrips(t *testing.T) {
	ctx := context.Background()
	s := seedRequiresStore(t)
	coord := store.ResourceCoordinate{Marketplace: "mp", Registry: "reg", Kind: "hook", Name: "guard"}
	rev := store.ResourceRevision{
		Coordinate:    coord,
		ContentDigest: store.RevisionDigest(digestOf("1")),
		Requires:      "[]",
	}
	if err := s.PutResourceRevision(ctx, rev); err != nil {
		t.Fatalf("PutResourceRevision: %v", err)
	}
	if err := s.PutRegistryOccurrence(ctx, store.RegistryOccurrence{
		RegistryID: "reg", Ref: "main", Commit: "commit", RegistryRevisionManifestDigest: store.RevisionDigest(digestOf("2")),
		Acquisition: store.AcquisitionMirror,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutResourceOccurrence(ctx, store.ResourceOccurrence{
		Coordinate: coord, ContentDigest: rev.ContentDigest, Commit: "commit", ManifestDigest: store.RevisionDigest(digestOf("2")),
	}); err != nil {
		t.Fatal(err)
	}
	assessment, err := store.FinalizeResourceAssessment(store.ResourceAssessment{
		Coordinate: coord, ContentDigest: rev.ContentDigest, AnalyzerVersion: "analyzer/v1",
		PolicyDigest: store.PolicyDigest(digestOf("3")), Decision: store.AssessmentDecisionAllow,
		InspectionManifestDigest: store.RevisionDigest(digestOf("4")), AssessedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.PutResourceAssessment(ctx, assessment); err != nil {
		t.Fatal(err)
	}
	if err := s.SetCurrentResourceAssessment(ctx, assessment); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetLatestResourceRevision(ctx, coord)
	if err != nil {
		t.Fatal(err)
	}
	if got.Requires != "[]" {
		t.Fatalf("requires = %q, want indexed empty list", got.Requires)
	}
}

// spec: marketplace-api/listed-other-registry-dependent-is-listed
// spec: marketplace-api/hidden-and-unlisted-dependents-are-not-listed
func TestListCurrentRequirementCandidatesScopesByListedRegistry(t *testing.T) {
	ctx := context.Background()
	s := seedRequiresStore(t)
	if err := s.PutRegistry(ctx, store.Registry{ID: "reg-unlisted", Slug: "hidden-reg", Listed: false, Acquisition: store.AcquisitionMirror}); err != nil {
		t.Fatal(err)
	}
	putCurrentRevision(t, s, "skill", "dep-listed", "commit", digestOf("c"), nil, "", testRequiresJSON)
	// An indexed same-registry revision still unpublished for indexing reports
	// an empty requires string rather than NULL.
	putCurrentRevision(t, s, "skill", "dep-unindexed", "commit", digestOf("c"), nil, "", "")
	// Same-registry hook and mcp revisions are out of scope for dependents.
	for _, tc := range []struct{ kind, name string }{
		{"hook", "dep-hook"},
		{"mcpserver", "dep-mcp"},
	} {
		coord := store.ResourceCoordinate{Marketplace: "mp", Registry: "reg", Kind: tc.kind, Name: tc.name}
		rev := store.ResourceRevision{
			Coordinate:    coord,
			ContentDigest: store.RevisionDigest(digestOf("5")),
			Requires:      testRequiresJSON,
		}
		if err := s.PutResourceRevision(ctx, rev); err != nil {
			t.Fatal(err)
		}
		if err := s.PutResourceOccurrence(ctx, store.ResourceOccurrence{
			Coordinate: coord, ContentDigest: rev.ContentDigest, Commit: "commit", ManifestDigest: store.RevisionDigest(digestOf("c")),
		}); err != nil {
			t.Fatal(err)
		}
	}
	// An unlisted other-registry dependent is not a candidate.
	other := store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "reg-unlisted", Kind: "skill", Name: "dep-unlisted"},
		ContentDigest: store.RevisionDigest(digestOf("6")),
		Requires:      testRequiresJSON,
	}
	if err := s.PutResourceRevision(ctx, other); err != nil {
		t.Fatal(err)
	}
	if err := s.PutRegistryOccurrence(ctx, store.RegistryOccurrence{
		RegistryID: "reg-unlisted", Ref: "main", Commit: "c2", RegistryRevisionManifestDigest: store.RevisionDigest(digestOf("7")),
		Acquisition: store.AcquisitionMirror,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutResourceOccurrence(ctx, store.ResourceOccurrence{
		Coordinate: other.Coordinate, ContentDigest: other.ContentDigest, Commit: "c2", ManifestDigest: store.RevisionDigest(digestOf("7")),
	}); err != nil {
		t.Fatal(err)
	}

	candidates, err := s.ListCurrentRequirementCandidates(ctx, "mp", "reg")
	if err != nil {
		t.Fatal(err)
	}
	got := make(map[string]string)
	for _, c := range candidates {
		got[c.RegistrySlug+"/"+c.Kind+"/"+c.Name] = c.Requires
	}
	if len(candidates) == 0 || got["reg/skill/dep-listed"] != testRequiresJSON {
		t.Fatalf("candidates = %+v, want reg/skill/dep-listed with requires", candidates)
	}
	if value, ok := got["reg/skill/dep-unindexed"]; !ok || value != "" {
		t.Fatalf("unindexed candidate requires = %q present=%v, want mapped empty string", value, ok)
	}
	if _, ok := got["hidden-reg/skill/dep-unlisted"]; ok {
		t.Fatalf("unlisted other-registry dependent listed: %+v", candidates)
	}
	if _, ok := got["reg/hook/dep-hook"]; ok {
		t.Fatalf("hook dependent listed: %+v", candidates)
	}

	// From the unlisted registry's own perspective it is in scope.
	own, err := s.ListCurrentRequirementCandidates(ctx, "mp", "reg-unlisted")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range own {
		if c.RegistrySlug == "hidden-reg" && c.Name == "dep-unlisted" {
			found = true
		}
	}
	if !found {
		t.Fatalf("own-registry unlisted dependent missing: %+v", own)
	}
}

// spec: marketplace-api/path-hint-must-equal-the-derived-source-root
// spec: marketplace-api/file-prefix-or-ambiguous-root-does-not-satisfy-a-path-hint
func TestGetCurrentOccurrenceRoots(t *testing.T) {
	ctx := context.Background()
	s := seedRequiresStore(t)
	coord := store.ResourceCoordinate{Marketplace: "mp", Registry: "reg", Kind: "skill", Name: "rooted"}
	rev := store.ResourceRevision{Coordinate: coord, ContentDigest: store.RevisionDigest(digestOf("9"))}
	if err := s.PutResourceRevision(ctx, rev); err != nil {
		t.Fatal(err)
	}
	if err := s.PutRegistryOccurrence(ctx, store.RegistryOccurrence{
		RegistryID: "reg", Ref: "main", Commit: "c3", Roots: []store.RegistryRoot{{Path: "teams"}, {Path: "shared"}},
		RegistryRevisionManifestDigest: store.RevisionDigest(digestOf("8")), Acquisition: store.AcquisitionMirror,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetRegistryHead(ctx, "reg", "c3", "commit"); err != nil {
		t.Fatal(err)
	}
	if err := s.PutResourceOccurrence(ctx, store.ResourceOccurrence{
		Coordinate: coord, ContentDigest: rev.ContentDigest, Commit: "c3", ManifestDigest: store.RevisionDigest(digestOf("8")),
		SourcePath: "teams/review/SKILL.md",
	}); err != nil {
		t.Fatal(err)
	}

	roots, err := s.GetCurrentOccurrenceRoots(ctx, coord)
	if err != nil {
		t.Fatal(err)
	}
	if roots.ContentDigest != rev.ContentDigest || roots.SourcePath != "teams/review/SKILL.md" || len(roots.Roots) != 2 {
		t.Fatalf("roots = %+v", roots)
	}
	if !store.SourceRootMatches(roots.Roots, roots.SourcePath, "teams") {
		t.Fatal("path hint equal to the derived source root must match")
	}
	if store.SourceRootMatches(roots.Roots, roots.SourcePath, "teams/review") {
		t.Fatal("file prefix that is not the derived source root must not match")
	}

	if _, ok := store.DerivedSourceRoot([]store.RegistryRoot{{Path: ""}, {Path: "teams"}}, "teams/review/SKILL.md"); ok {
		t.Fatal("nested roots make the derived source root ambiguous")
	}

	missing := coord
	missing.Name = "absent"
	if _, err := s.GetCurrentOccurrenceRoots(ctx, missing); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing occurrence error = %v, want ErrNotFound", err)
	}
}

// spec: marketplace-api/path-hint-must-equal-the-derived-source-root
func TestGetCurrentOccurrenceRootsFromPublicationHead(t *testing.T) {
	ctx := context.Background()
	s := seedRequiresStore(t)
	coord := store.ResourceCoordinate{Marketplace: "mp", Registry: "reg", Kind: "skill", Name: "rooted"}
	rev := store.ResourceRevision{Coordinate: coord, ContentDigest: store.RevisionDigest(digestOf("9"))}
	assessment, err := store.FinalizeResourceAssessment(store.ResourceAssessment{
		Coordinate: coord, ContentDigest: rev.ContentDigest, AnalyzerVersion: "analyzer/v1",
		PolicyDigest: store.PolicyDigest(digestOf("d")), Decision: store.AssessmentDecisionAllow,
		InspectionManifestDigest: store.RevisionDigest(digestOf("e")), AssessedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	err = s.PublishRegistryPublication(ctx, store.RegistryRevisionPublication{
		PublicationID: "pub-1",
		Occurrence: store.RegistryOccurrence{
			RegistryID: "reg", Ref: "main", Commit: "pubcommit",
			Roots:                          []store.RegistryRoot{{Path: "teams"}, {Path: "shared"}},
			RegistryRevisionManifestDigest: store.RevisionDigest(digestOf("8")),
			Acquisition:                    store.AcquisitionMirror,
		},
		Resources: []store.ResourceRevision{rev},
		ResourceOccurrences: []store.ResourceOccurrence{{
			Coordinate: coord, ContentDigest: rev.ContentDigest, Commit: "pubcommit",
			ManifestDigest: store.RevisionDigest(digestOf("8")), SourcePath: "teams/review/SKILL.md",
		}},
		Assessments: []store.ResourceAssessment{assessment},
	})
	if err != nil {
		t.Fatalf("PublishRegistryPublication: %v", err)
	}

	roots, err := s.GetCurrentOccurrenceRoots(ctx, coord)
	if err != nil {
		t.Fatal(err)
	}
	if roots.ContentDigest != rev.ContentDigest || roots.SourcePath != "teams/review/SKILL.md" || len(roots.Roots) != 2 {
		t.Fatalf("publication-head roots = %+v", roots)
	}
	if !store.SourceRootMatches(roots.Roots, roots.SourcePath, "teams") {
		t.Fatal("publication roots must satisfy the derived source root")
	}
}

// spec: marketplace-publication/declared-prerequisites-are-indexed-in-declaration-order
func TestRequiresMigrationLeavesExistingRowsUnindexed(t *testing.T) {
	ctx := context.Background()
	dsn := testDSN(t)
	schema := testSchema(t)
	t.Cleanup(func() { dropSchema(t, dsn, schema) })
	quoted := pgx.Identifier{schema}.Sanitize()

	s := postgres.NewWithSchema(dsn, schema)
	if err := s.Open(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	// A fresh database migrates in a nullable requires column.
	var nullable string
	if err := db.QueryRowContext(ctx,
		`SELECT is_nullable FROM information_schema.columns WHERE table_schema=$1 AND table_name='resource_revisions' AND column_name='requires'`,
		schema).Scan(&nullable); err != nil {
		t.Fatalf("requires column after fresh migration: %v", err)
	}
	if nullable != "YES" {
		t.Fatalf("requires is_nullable = %q, want YES", nullable)
	}

	if _, err := db.ExecContext(ctx, `
SET search_path TO `+quoted+`;
ALTER TABLE resource_revisions DROP COLUMN requires;
DELETE FROM marketplace_metadata_v2_schema_migrations WHERE version = (SELECT max(version) FROM marketplace_metadata_v2_schema_migrations);
INSERT INTO resource_revisions (marketplace_id, registry_id, kind, name, content_digest, description)
VALUES ('mp', 'reg', 'skill', 'legacy', 'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa', '');
`); err != nil {
		t.Fatal(err)
	}

	s = postgres.NewWithSchema(dsn, schema)
	if err := s.Open(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	var stored sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT requires FROM `+quoted+`.resource_revisions WHERE marketplace_id='mp'`).Scan(&stored); err != nil {
		t.Fatalf("requires column after migration: %v", err)
	}
	if stored.Valid {
		t.Fatalf("pre-indexing revision requires = %q, want NULL", stored.String)
	}
}
