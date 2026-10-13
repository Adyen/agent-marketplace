package sqlite_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/sqlite"
)

const testRequiresJSON = `[{"kind":"skill","name":"helper"},{"kind":"command","name":"lint","registry":"other","path":"shared"}]`

// spec: marketplace-publication/declared-prerequisites-are-indexed-in-declaration-order
// spec: marketplace-publication/revision-published-before-indexing-stays-unindexed
// spec: marketplace-publication/conflicting-prerequisites-for-an-indexed-revision-are-rejected
func TestRequiresStoredRepairedAndConflicting(t *testing.T) {
	ctx := context.Background()
	s := openSeededStore(t)
	rev := seedRevision(t, s, "review", "")

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
	s := openSeededStore(t)
	coord := store.ResourceCoordinate{Marketplace: "mp", Registry: "reg", Kind: "hook", Name: "guard"}
	rev := store.ResourceRevision{
		Coordinate:    coord,
		ContentDigest: store.RevisionDigest(store.RevisionDigest("sha256:1111111111111111111111111111111111111111111111111111111111111111")),
		Requires:      "[]",
	}
	if err := s.PutResourceRevision(ctx, rev); err != nil {
		t.Fatalf("PutResourceRevision: %v", err)
	}
	if err := s.PutRegistryOccurrence(ctx, store.RegistryOccurrence{
		RegistryID: "reg", Ref: "main", Commit: "commit", RegistryRevisionManifestDigest: "sha256:2222222222222222222222222222222222222222222222222222222222222222",
		Acquisition: store.AcquisitionMirror,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutResourceOccurrence(ctx, store.ResourceOccurrence{
		Coordinate: coord, ContentDigest: rev.ContentDigest, Commit: "commit", ManifestDigest: "sha256:2222222222222222222222222222222222222222222222222222222222222222",
	}); err != nil {
		t.Fatal(err)
	}
	assessment := store.ResourceAssessment{
		Coordinate: coord, ContentDigest: rev.ContentDigest, AnalyzerVersion: "analyzer/v1",
		PolicyDigest: store.PolicyDigest(store.PolicyDigest("sha256:3333333333333333333333333333333333333333333333333333333333333333")), Decision: store.AssessmentDecisionAllow,
		InspectionManifestDigest: store.RevisionDigest(store.RevisionDigest("sha256:4444444444444444444444444444444444444444444444444444444444444444")), AssessedAt: time.Now().UTC(),
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
	s := openSeededStore(t)
	if err := s.PutRegistry(ctx, store.Registry{ID: "reg-unlisted", Slug: "hidden-reg", Listed: false, Acquisition: store.AcquisitionMirror}); err != nil {
		t.Fatal(err)
	}
	listed := seedRevision(t, s, "dep-listed", "")
	listed.Requires = testRequiresJSON
	if err := s.PutResourceRevision(ctx, listed); err != nil {
		t.Fatal(err)
	}
	// Same-registry hook and mcp revisions are out of scope for dependents.
	for _, tc := range []struct{ kind, name, requires string }{
		{"hook", "dep-hook", testRequiresJSON},
		{"mcpserver", "dep-mcp", testRequiresJSON},
	} {
		rev := store.ResourceRevision{
			Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "reg", Kind: tc.kind, Name: tc.name},
			ContentDigest: store.RevisionDigest(store.RevisionDigest("sha256:5555555555555555555555555555555555555555555555555555555555555555")),
			Requires:      tc.requires,
		}
		if err := s.PutResourceRevision(ctx, rev); err != nil {
			t.Fatal(err)
		}
		if err := s.PutResourceOccurrence(ctx, store.ResourceOccurrence{
			Coordinate: rev.Coordinate, ContentDigest: rev.ContentDigest, Commit: "commit", ManifestDigest: "sha256:2222222222222222222222222222222222222222222222222222222222222222",
		}); err != nil {
			t.Fatal(err)
		}
	}
	// An unlisted other-registry dependent is not a candidate.
	other := store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "reg-unlisted", Kind: "skill", Name: "dep-unlisted"},
		ContentDigest: store.RevisionDigest(store.RevisionDigest("sha256:6666666666666666666666666666666666666666666666666666666666666666")),
		Requires:      testRequiresJSON,
	}
	if err := s.PutResourceRevision(ctx, other); err != nil {
		t.Fatal(err)
	}
	if err := s.PutRegistryOccurrence(ctx, store.RegistryOccurrence{
		RegistryID: "reg-unlisted", Ref: "main", Commit: "c2", RegistryRevisionManifestDigest: "sha256:7777777777777777777777777777777777777777777777777777777777777777",
		Acquisition: store.AcquisitionMirror,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutResourceOccurrence(ctx, store.ResourceOccurrence{
		Coordinate: other.Coordinate, ContentDigest: other.ContentDigest, Commit: "c2", ManifestDigest: "sha256:7777777777777777777777777777777777777777777777777777777777777777",
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
	s := openSeededStore(t)
	coord := store.ResourceCoordinate{Marketplace: "mp", Registry: "reg", Kind: "skill", Name: "rooted"}
	rev := store.ResourceRevision{Coordinate: coord, ContentDigest: store.RevisionDigest("sha256:9999999999999999999999999999999999999999999999999999999999999999")}
	if err := s.PutResourceRevision(ctx, rev); err != nil {
		t.Fatal(err)
	}
	if err := s.PutRegistryOccurrence(ctx, store.RegistryOccurrence{
		RegistryID: "reg", Ref: "main", Commit: "c3", Roots: []store.RegistryRoot{{Path: "teams"}, {Path: "shared"}},
		RegistryRevisionManifestDigest: "sha256:8888888888888888888888888888888888888888888888888888888888888888", Acquisition: store.AcquisitionMirror,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetRegistryHead(ctx, "reg", "c3", "commit"); err != nil {
		t.Fatal(err)
	}
	if err := s.PutResourceOccurrence(ctx, store.ResourceOccurrence{
		Coordinate: coord, ContentDigest: rev.ContentDigest, Commit: "c3", ManifestDigest: "sha256:8888888888888888888888888888888888888888888888888888888888888888",
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

// spec: marketplace-publication/declared-prerequisites-are-indexed-in-declaration-order
func TestRequiresMigrationLeavesExistingRowsUnindexed(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "marketplace.db")
	s := sqlite.New(path)
	if err := s.Open(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `
ALTER TABLE resource_revisions DROP COLUMN requires;
DELETE FROM marketplace_metadata_v2_migrations WHERE version = (SELECT max(version) FROM marketplace_metadata_v2_migrations);
INSERT INTO resource_revisions (marketplace_id, registry_id, kind, name, content_digest, description)
VALUES ('mp', 'reg', 'skill', 'legacy', 'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa', '');
`); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	s = sqlite.New(path)
	if err := s.Open(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	db, err = sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var stored sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT requires FROM resource_revisions WHERE marketplace_id='mp'`).Scan(&stored); err != nil {
		t.Fatalf("requires column after migration: %v", err)
	}
	if stored.Valid {
		t.Fatalf("pre-indexing revision requires = %q, want NULL", stored.String)
	}
}
