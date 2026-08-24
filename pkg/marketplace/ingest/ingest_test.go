package ingest

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func TestBuildSourceRevision(t *testing.T) {
	ctx := context.Background()
	repoDir := createIngestRepo(t)
	commit, err := repoCommit(repoDir)
	if err != nil {
		t.Fatalf("repo commit: %v", err)
	}

	cfg := testConfig(t, repoDir)
	source := store.Source{
		ID:          store.SourceID("src-test"),
		Slug:        "example-source",
		Ref:         "main",
		Origins:     []string{"file://" + repoDir},
		Acquisition: store.AcquisitionMirror,
		License:     "MIT",
		Roots: []store.SourceRoot{
			{Path: ".agents", SourceHarness: "factory"},
		},
	}

	rev, err := BuildSourceRevision(ctx, cfg, store.MarketplaceID("mp-test"), source, repoDir, commit)
	if err != nil {
		t.Fatalf("BuildSourceRevision: %v", err)
	}

	if rev.ManifestDigest == "" {
		t.Fatalf("manifest digest should not be empty")
	}
	if len(rev.Resources) == 0 {
		t.Fatalf("expected resources")
	}

	var skill *store.ResourceRevision
	for i := range rev.Resources {
		if rev.Resources[i].Coordinate.Kind == "skill" && rev.Resources[i].Coordinate.Name == "review" {
			skill = &rev.Resources[i]
			break
		}
	}
	if skill == nil {
		t.Fatalf("expected skill/review resource revision")
	}
	if skill.ContentDigest == "" {
		t.Fatalf("skill content digest should not be empty")
	}
	if skill.ArchiveDigest == "" {
		t.Fatalf("skill archive digest should not be empty for mirror mode")
	}
	if skill.License != "MIT" {
		t.Fatalf("skill license = %q, want MIT", skill.License)
	}

	if rev.Occurrence.SourceRevisionManifestDigest != rev.ManifestDigest {
		t.Fatalf("occurrence manifest digest mismatch")
	}
	if rev.Occurrence.Ref != source.Ref {
		t.Fatalf("occurrence ref = %q, want %q", rev.Occurrence.Ref, source.Ref)
	}
	for _, field := range []string{"source_url", "ref", "path", repoDir} {
		if strings.Contains(string(rev.ManifestPayload), field) {
			t.Fatalf("source revision manifest exposed %q: %s", field, rev.ManifestPayload)
		}
	}
}

func TestBuildSourceRevisionIndexOnly(t *testing.T) {
	ctx := context.Background()
	repoDir := createIngestRepo(t)
	commit, err := repoCommit(repoDir)
	if err != nil {
		t.Fatalf("repo commit: %v", err)
	}

	cfg := testConfig(t, repoDir)
	source := store.Source{
		ID:          store.SourceID("src-test"),
		Slug:        "example-source",
		Ref:         "main",
		Origins:     []string{"file://" + repoDir},
		Acquisition: store.AcquisitionIndexOnly,
		License:     "MIT",
		Roots: []store.SourceRoot{
			{Path: ".agents", SourceHarness: "factory"},
		},
	}

	rev, err := BuildSourceRevision(ctx, cfg, store.MarketplaceID("mp-test"), source, repoDir, commit)
	if err != nil {
		t.Fatalf("BuildSourceRevision: %v", err)
	}

	var skill *store.ResourceRevision
	for i := range rev.Resources {
		if rev.Resources[i].Coordinate.Kind == "skill" && rev.Resources[i].Coordinate.Name == "review" {
			skill = &rev.Resources[i]
			break
		}
	}
	if skill == nil {
		t.Fatalf("expected skill/review resource revision")
	}
	if skill.ArchiveDigest != "" {
		t.Fatalf("index-only resource should not have an archive digest")
	}
	key := "skill/review"
	if len(rev.ResourceArchives[key]) == 0 || rev.ResourceArchiveDigest[key] == "" {
		t.Fatalf("index-only revision should retain a local archive for consumer materialization")
	}
}

func TestBuildSourceRevisionCollection(t *testing.T) {
	ctx := context.Background()
	repoDir := createIngestRepo(t)
	commit, err := repoCommit(repoDir)
	if err != nil {
		t.Fatalf("repo commit: %v", err)
	}

	cfg := testConfig(t, repoDir)
	source := store.Source{
		ID:          store.SourceID("src-test"),
		Slug:        "example-source",
		Ref:         "main",
		Origins:     []string{"file://" + repoDir},
		Acquisition: store.AcquisitionMirror,
		License:     "MIT",
		Roots: []store.SourceRoot{
			{Path: ".agents", SourceHarness: "factory"},
		},
	}

	rev, err := BuildSourceRevision(ctx, cfg, store.MarketplaceID("mp-test"), source, repoDir, commit)
	if err != nil {
		t.Fatalf("BuildSourceRevision: %v", err)
	}

	var collection *store.ResourceRevision
	var skill *store.ResourceRevision
	for i := range rev.Resources {
		r := &rev.Resources[i]
		if r.Coordinate.Kind == "collection" && r.Coordinate.Name == "defaults" {
			collection = r
		}
		if r.Coordinate.Kind == "skill" && r.Coordinate.Name == "review" {
			skill = r
		}
	}
	if skill == nil {
		t.Fatalf("expected skill/review resource revision")
	}
	if collection == nil {
		t.Fatalf("expected collection/defaults resource revision")
	}
	if collection.ContentDigest == "" {
		t.Fatalf("collection content digest should not be empty")
	}
	if collection.ArchiveDigest == "" {
		t.Fatalf("collection archive digest should not be empty for mirror mode")
	}
}

func TestBuildSourceRevisionLicenseRejection(t *testing.T) {
	ctx := context.Background()
	repoDir := createIngestRepo(t)
	commit, err := repoCommit(repoDir)
	if err != nil {
		t.Fatalf("repo commit: %v", err)
	}

	cfg := testConfig(t, repoDir)
	cfg.Policy.LicenseAllowlist = []string{"Apache-2.0"}

	source := store.Source{
		ID:          store.SourceID("src-test"),
		Slug:        "example-source",
		Ref:         "main",
		Origins:     []string{"file://" + repoDir},
		Acquisition: store.AcquisitionMirror,
		License:     "MIT",
		Roots: []store.SourceRoot{
			{Path: ".agents", SourceHarness: "factory"},
		},
	}

	rev, err := BuildSourceRevision(ctx, cfg, store.MarketplaceID("mp-test"), source, repoDir, commit)
	if err != nil {
		t.Fatalf("BuildSourceRevision: %v", err)
	}

	var skill *store.ResourceRevision
	for i := range rev.Resources {
		if rev.Resources[i].Coordinate.Kind == "skill" {
			skill = &rev.Resources[i]
			break
		}
	}
	if skill == nil {
		t.Fatalf("expected skill resource revision")
	}
	if skill.ArchiveDigest != "" {
		t.Fatalf("rejected resource should not have an archive digest")
	}
	if len(skill.Findings) == 0 {
		t.Fatalf("rejected resource should have findings")
	}
}

func createIngestRepo(tb testing.TB) string {
	tb.Helper()
	dir := tb.TempDir()
	repo := filepath.Join(dir, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		tb.Fatalf("mkdir repo: %v", err)
	}

	skillDir := filepath.Join(repo, ".agents", "skills", "review")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		tb.Fatalf("mkdir skill: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: review\ndescription: Review code\nlicense: MIT\n---\nReview the code carefully.\n"), 0o644); err != nil {
		tb.Fatalf("write skill: %v", err)
	}

	collectionDir := filepath.Join(repo, ".agents", "collections", "defaults")
	if err := os.MkdirAll(collectionDir, 0o755); err != nil {
		tb.Fatalf("mkdir collection: %v", err)
	}
	collectionYAML := `name: defaults
version: 1.0.0
description: Default collection
resources:
  - name: review
    kind: skill
`
	if err := os.WriteFile(filepath.Join(collectionDir, "COLLECTION.yaml"), []byte(collectionYAML), 0o644); err != nil {
		tb.Fatalf("write collection: %v", err)
	}

	cmds := [][]string{
		{"git", "init", "-q"},
		{"git", "config", "user.email", "test@example.com"},
		{"git", "config", "user.name", "Test"},
		{"git", "add", "."},
		{"git", "commit", "-q", "-m", "initial"},
	}
	for _, c := range cmds {
		cmd := exec.Command(c[0], c[1:]...)
		cmd.Dir = repo
		out, err := cmd.CombinedOutput()
		if err != nil {
			tb.Fatalf("%v: %v\n%s", c, err, out)
		}
	}
	return repo
}

func repoCommit(repoDir string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = repoDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}
	return string(out[:40]), nil
}

func testConfig(tb testing.TB, repoDir string) config.Config {
	tb.Helper()
	return config.Config{
		APIVersion:         "agent-marketplace.io/marketplace/v1",
		AcquisitionDefault: "mirror",
		Refresh:            config.Refresh{Interval: config.Duration(30 * time.Minute)},
		Policy: config.Policy{
			HiddenContent:    "reject",
			LicenseAllowlist: []string{"MIT", "Apache-2.0", "BSD-2-Clause", "BSD-3-Clause"},
			Limits: config.Limits{
				RepositoryBytes:   100 * 1024 * 1024,
				FileCount:         1024,
				FileBytes:         2 * 1024 * 1024,
				ArtifactBytes:     16 * 1024 * 1024,
				CloneDuration:     config.Duration(5 * time.Minute),
				IngestionDuration: config.Duration(2 * time.Minute),
			},
		},
	}
}
