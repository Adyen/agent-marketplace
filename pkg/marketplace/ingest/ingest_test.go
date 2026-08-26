package ingest

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	hccir "github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/analyze"
	marketplaceapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	marketplaceartifact "github.com/Adyen/agent-marketplace/pkg/marketplace/artifact"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/inspection"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/fsblob"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/sqlite"
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

func TestBuildSourceRevisionWarnsAndKeepsFirstDuplicateAcrossRoots(t *testing.T) {
	ctx := context.Background()
	repoDir := createIngestRepo(t)
	commit, err := repoCommit(repoDir)
	if err != nil {
		t.Fatalf("repo commit: %v", err)
	}

	duplicatePath := filepath.Join(repoDir, ".agents-duplicate", "skills", "review", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(duplicatePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(duplicatePath, []byte("---\nname: review\ndescription: Duplicate review\nlicense: MIT\n---\nDifferent content.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := testConfig(t, repoDir)
	source := store.Source{
		ID:          "src-test",
		Slug:        "example-source",
		Ref:         "main",
		Origins:     []string{"file://" + repoDir},
		Acquisition: store.AcquisitionMirror,
		License:     "MIT",
		Roots: []store.SourceRoot{
			{Path: ".agents", SourceHarness: "factory"},
		},
	}
	baseline, err := BuildSourceRevision(ctx, cfg, "mp-test", source, repoDir, commit)
	if err != nil {
		t.Fatalf("baseline BuildSourceRevision: %v", err)
	}
	var wantDigest store.RevisionDigest
	for _, resource := range baseline.Resources {
		if resource.Coordinate.Kind == "skill" && resource.Coordinate.Name == "review" {
			wantDigest = resource.ContentDigest
		}
	}
	if wantDigest == "" {
		t.Fatal("baseline skill/review not found")
	}

	var logs bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })

	source.Roots = append(source.Roots, store.SourceRoot{Path: ".agents-duplicate", SourceHarness: "factory"})
	rev, err := BuildSourceRevision(ctx, cfg, "mp-test", source, repoDir, commit)
	if err != nil {
		t.Fatalf("BuildSourceRevision: %v", err)
	}
	var gotDigests []store.RevisionDigest
	for _, resource := range rev.Resources {
		if resource.Coordinate.Kind == "skill" && resource.Coordinate.Name == "review" {
			gotDigests = append(gotDigests, resource.ContentDigest)
		}
	}
	if len(gotDigests) != 1 || gotDigests[0] != wantDigest {
		t.Fatalf("skill/review digests = %v, want first occurrence %s", gotDigests, wantDigest)
	}
	for _, want := range []string{"duplicate resource identity", "src-test", "skill/review", ".agents/skills/review/SKILL.md", ".agents-duplicate/skills/review/SKILL.md"} {
		if !strings.Contains(logs.String(), want) {
			t.Fatalf("warning logs missing %q: %s", want, logs.String())
		}
	}
}

func TestBuildSourceRevisionRejectsDuplicateAtSamePath(t *testing.T) {
	repoDir := createIngestRepo(t)
	commit, err := repoCommit(repoDir)
	if err != nil {
		t.Fatalf("repo commit: %v", err)
	}
	source := store.Source{
		ID:          "src-test",
		Slug:        "example-source",
		Ref:         "main",
		Origins:     []string{"file://" + repoDir},
		Acquisition: store.AcquisitionMirror,
		License:     "MIT",
		Roots: []store.SourceRoot{
			{Path: ".agents", SourceHarness: "factory"},
			{Path: ".agents", SourceHarness: "factory"},
		},
	}
	_, err = BuildSourceRevision(context.Background(), testConfig(t, repoDir), "mp-test", source, repoDir, commit)
	if err == nil || !strings.Contains(err.Error(), `duplicate resource "skill/review" at ".agents/skills/review/SKILL.md"`) {
		t.Fatalf("BuildSourceRevision error = %v", err)
	}
}

func TestMCPInspectionRedactsSecretsEverywhere(t *testing.T) {
	const (
		envSecret    = "mcp-env-secret-7f2a"
		headerSecret = "mcp-header-secret-91bd"
		argSecret    = "mcp-arg-secret-c4e8"
		oauthSecret  = "mcp-oauth-secret-d03c"
		urlSecret    = "mcp-url-secret-a62f"
	)
	mcp := hccir.NewMcpServerWithOptions(
		"hostile",
		"node --token "+argSecret,
		[]string{"--token", argSecret},
		map[string]string{"API_TOKEN": envSecret},
		nil,
		hccir.SourceRef{},
		hccir.WithMcpURL("https://mcp.example.invalid/server?access_token="+urlSecret),
		hccir.WithMcpTransport("streamable-http"),
		hccir.WithMcpHeaders(map[string]string{"Authorization": "Bearer " + headerSecret}),
		hccir.WithMcpOAuth("public-client", oauthSecret, "https://mcp.example.invalid/resource?token="+urlSecret),
	)
	report, err := analyze.NewEngine().Analyze(context.Background(), mcp, nil, analyze.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	for _, finding := range report.Findings {
		for _, secret := range []string{envSecret, headerSecret, argSecret, oauthSecret, urlSecret} {
			if strings.Contains(finding.Message, secret) {
				t.Fatalf("MCP finding exposed hostile secret %q: %#v", secret, finding)
			}
		}
	}

	// Establish that the installation artifact intentionally retains the full
	// configuration; inspection must not accidentally reuse this payload.
	artifact, err := hccartifact.FromIR(mcp)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{envSecret, headerSecret, argSecret, oauthSecret, urlSecret} {
		if !strings.Contains(string(artifact.Payload), secret) {
			t.Fatalf("fixture artifact did not contain hostile secret %q", secret)
		}
	}

	primary, support := inspectionInput(mcp, marketplaceartifact.BuildResult{Artifact: artifact}, nil)
	projection, err := inspection.Build(
		store.ResourceCoordinate{Marketplace: "marketplace", Source: "source-id", Kind: "mcpserver", Name: "hostile"},
		"sha256:"+strings.Repeat("a", 64), primary, support, nil, 10, 4096, 8192,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(primary), `"environment_keys":["API_TOKEN"]`) || !strings.Contains(string(primary), `"header_keys":["Authorization"]`) {
		t.Fatalf("redacted primary omitted safe keys: %s", primary)
	}
	if strings.Contains(string(primary), "node --token") || strings.Contains(string(primary), "argument_count") && strings.Contains(string(primary), argSecret) {
		t.Fatalf("redacted primary exposed argv: %s", primary)
	}
	assertNoMCPSecrets := func(where string, values ...[]byte) {
		t.Helper()
		for _, value := range values {
			text := string(value)
			for _, secret := range []string{envSecret, headerSecret, argSecret, oauthSecret, urlSecret} {
				if strings.Contains(text, secret) {
					t.Fatalf("%s exposed hostile MCP secret %q: %s", where, secret, text)
				}
			}
		}
	}
	assertNoMCPSecrets("inspection primary", primary)
	assertNoMCPSecrets("inspection manifest", projection.Payload)
	for digest, body := range projection.Bodies {
		assertNoMCPSecrets("inspection body "+digest, body)
	}

	ctx := context.Background()
	meta := sqlite.New(":memory:")
	if err := meta.Open(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = meta.Close() }()
	blobs := fsblob.New(t.TempDir())
	source := store.Source{ID: "source-id", Slug: "source", Origins: []string{"https://example.invalid/repo"}, Ref: "main", Roots: []store.SourceRoot{{Path: ".agents"}}, Acquisition: store.AcquisitionIndexOnly}
	if err := meta.PutSource(ctx, source); err != nil {
		t.Fatal(err)
	}
	if err := meta.PutMarketplace(ctx, store.Marketplace{ID: "marketplace", Name: "Test", CanonicalURL: "https://example.invalid"}); err != nil {
		t.Fatal(err)
	}
	for digest, body := range projection.Bodies {
		if err := blobs.Put(ctx, store.ArchiveDigest(digest), body); err != nil {
			t.Fatal(err)
		}
	}
	if err := blobs.Put(ctx, store.ArchiveDigest(projection.Manifest.ManifestDigest), projection.Payload); err != nil {
		t.Fatal(err)
	}
	revision := "sha256:" + strings.Repeat("a", 64)
	coord := store.ResourceCoordinate{Marketplace: "marketplace", Source: source.ID, Kind: "mcpserver", Name: "hostile"}
	if err := meta.PutResourceRevision(ctx, store.ResourceRevision{Coordinate: coord, ContentDigest: store.RevisionDigest(revision), Provenance: &store.Provenance{InspectionManifestDigest: store.RevisionDigest(projection.Manifest.ManifestDigest)}}); err != nil {
		t.Fatal(err)
	}

	var logs bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	defer slog.SetDefault(previousLogger)
	server := marketplaceapi.NewServer(config.Config{Instance: config.Instance{ID: "marketplace"}}, meta, blobs)
	httpServer := httptest.NewServer(server.PublicHandler())
	defer httpServer.Close()
	paths := []string{
		"/api/v1/resources/source/mcpserver/hostile/revisions/" + revision + "/inspection",
		"/api/v1/resources/source/mcpserver/hostile/revisions/" + revision + "/inspection/content",
		"/api/v1/resources/source/mcpserver/hostile/revisions/" + revision,
		"/api/v1/inspection/sha256/" + strings.TrimPrefix(projection.Manifest.ManifestDigest, "sha256:"),
		"/api/v1/content/sha256/" + strings.TrimPrefix(inspection.Digest(primary), "sha256:"),
	}
	for _, path := range paths {
		resp, err := http.Get(httpServer.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		data, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			t.Fatal(readErr)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s status = %d, body = %s", path, resp.StatusCode, data)
		}
		assertNoMCPSecrets("API response "+path, data)
	}
	assertNoMCPSecrets("log values", logs.Bytes())
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
	if collection.ArchiveDigest != "" {
		t.Fatalf("virtual collection must not have an archive digest: %q", collection.ArchiveDigest)
	}
	if collection.ContentDigest == "" || len(rev.CollectionManifests) != 1 {
		t.Fatalf("collection standalone publication = digest %q/archive %q, manifests %d", collection.ContentDigest, collection.ArchiveDigest, len(rev.CollectionManifests))
	}
	if len(rev.ExtraBlobs) != 2 || rev.ExtraBlobs[0].Digest == rev.CollectionManifests[0].Digest {
		t.Fatalf("collection publication should contain one leaf archive and one standalone manifest: %#v", rev.ExtraBlobs)
	}
	if len(rev.CollectionManifests[0].Data) == 0 || rev.CollectionManifests[0].Data[0] != '{' || string(rev.CollectionManifests[0].Digest) != string(collection.ContentDigest) {
		t.Fatalf("collection manifest upload = digest %q/data prefix %q", rev.CollectionManifests[0].Digest, rev.CollectionManifests[0].Data)
	}
	if len(rev.Collections) != 1 || rev.Collections[0].ManifestDigest != collection.ContentDigest || len(rev.Collections[0].Members) != 1 {
		t.Fatalf("collection metadata = %#v", rev.Collections)
	}
	manifest, payload, err := marketplaceartifact.DecodeSourceRevisionManifest(bytes.NewReader(rev.ManifestPayload))
	if err != nil {
		t.Fatalf("decode source revision manifest: %v", err)
	}
	manifest.RevisionDigest = string(rev.ManifestDigest)
	if err := marketplaceartifact.VerifySourceRevisionManifest(manifest, payload); err != nil {
		t.Fatalf("verify source revision manifest: %v", err)
	}
	for _, resource := range manifest.Resources {
		if resource.Kind == "collection" && resource.ArchiveDigest != "" {
			t.Fatalf("source revision manifest exposed collection archive digest: %#v", resource)
		}
	}
}

func TestBuildSourceRevisionNestedCollectionDeduplicatesLeavesAndRetainsPaths(t *testing.T) {
	repoDir := createIngestRepo(t)
	baseDir := filepath.Join(repoDir, ".agents", "collections", "base")
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(baseDir, "COLLECTION.yaml"), []byte(`name: base
resources:
  - name: review
    kind: skill
`), 0o644); err != nil {
		t.Fatal(err)
	}
	defaults := filepath.Join(repoDir, ".agents", "collections", "defaults", "COLLECTION.yaml")
	if err := os.WriteFile(defaults, []byte(`name: defaults
includes:
  - name: base
resources:
  - name: review
    kind: skill
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "add", ".")
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v: %s", err, out)
	}
	cmd = exec.Command("git", "commit", "-q", "-m", "nested")
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v: %s", err, out)
	}

	source := store.Source{ID: "src-test", Slug: "example-source", Ref: "main", Origins: []string{"file://" + repoDir}, Acquisition: store.AcquisitionMirror, License: "MIT", Roots: []store.SourceRoot{{Path: ".agents", SourceHarness: "factory"}}}
	rev, err := BuildSourceRevision(context.Background(), testConfig(t, repoDir), "mp-test", source, repoDir, mustRepoCommit(t, repoDir))
	if err != nil {
		t.Fatal(err)
	}
	if len(rev.Collections) != 2 {
		t.Fatalf("expected two collection revisions, got %#v", rev.Collections)
	}
	var rootDigest store.RevisionDigest
	for _, resource := range rev.Resources {
		if resource.Coordinate.Kind == "collection" && resource.Coordinate.Name == "defaults" {
			rootDigest = resource.ContentDigest
		}
	}
	if rootDigest == "" {
		t.Fatal("root collection revision missing")
	}
	var rootManifest []byte
	for _, upload := range rev.CollectionManifests {
		if store.RevisionDigest(upload.Digest) == rootDigest {
			rootManifest = upload.Data
		}
	}
	manifest, _, err := hccartifact.DecodeCollectionManifest(bytes.NewReader(rootManifest))
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Includes) != 1 || manifest.Includes[0] != "base" {
		t.Fatalf("direct includes lost: %#v", manifest.Includes)
	}
	if len(manifest.Leaves) != 1 || len(manifest.Leaves[0].InclusionPaths) != 2 {
		t.Fatalf("deduplicated leaf paths = %#v", manifest.Leaves)
	}
}

func TestBuildSourceRevisionRejectsNestedCollectionCycle(t *testing.T) {
	repoDir := createIngestRepo(t)
	baseDir := filepath.Join(repoDir, ".agents", "collections", "base")
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(baseDir, "COLLECTION.yaml"), []byte("name: base\nincludes:\n  - name: defaults\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	defaults := filepath.Join(repoDir, ".agents", "collections", "defaults", "COLLECTION.yaml")
	if err := os.WriteFile(defaults, []byte("name: defaults\nincludes:\n  - name: base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "add", ".")
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v: %s", err, out)
	}
	cmd = exec.Command("git", "commit", "-q", "-m", "cycle")
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v: %s", err, out)
	}
	source := store.Source{ID: "src-test", Slug: "example-source", Ref: "main", Origins: []string{"file://" + repoDir}, Acquisition: store.AcquisitionMirror, License: "MIT", Roots: []store.SourceRoot{{Path: ".agents", SourceHarness: "factory"}}}
	_, err := BuildSourceRevision(context.Background(), testConfig(t, repoDir), "mp-test", source, repoDir, mustRepoCommit(t, repoDir))
	if err == nil || !strings.Contains(err.Error(), "cyclic collection reference") {
		t.Fatalf("cycle error = %v", err)
	}
}

func mustRepoCommit(t *testing.T, repoDir string) string {
	t.Helper()
	commit, err := repoCommit(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	return commit
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

// resourceRevisionFor returns the built revision for a kind, so description
// assertions do not depend on resource ordering.
func resourceRevisionFor(tb testing.TB, revisions []store.ResourceRevision, kind string) store.ResourceRevision {
	tb.Helper()
	for _, revision := range revisions {
		if revision.Coordinate.Kind == kind {
			return revision
		}
	}
	tb.Fatalf("no %s resource revision was built", kind)
	return store.ResourceRevision{}
}

// The description authored in the registry must survive ingestion all the way
// onto the persisted revision, for every kind that declares one.
func TestBuildSourceRevisionCarriesDescriptions(t *testing.T) {
	ctx := context.Background()
	repoDir := createIngestRepo(t)
	commit, err := repoCommit(repoDir)
	if err != nil {
		t.Fatalf("repo commit: %v", err)
	}

	source := store.Source{
		ID:          store.SourceID("src-test"),
		Slug:        "example-source",
		Ref:         "main",
		Origins:     []string{"file://" + repoDir},
		Acquisition: store.AcquisitionMirror,
		License:     "MIT",
		Roots:       []store.SourceRoot{{Path: ".agents", SourceHarness: "factory"}},
	}

	rev, err := BuildSourceRevision(ctx, testConfig(t, repoDir), store.MarketplaceID("mp-test"), source, repoDir, commit)
	if err != nil {
		t.Fatalf("BuildSourceRevision: %v", err)
	}

	if got := resourceRevisionFor(t, rev.Resources, "skill").Description; got != "Review code" {
		t.Errorf("skill description = %q, want %q", got, "Review code")
	}
	if got := resourceRevisionFor(t, rev.Resources, "collection").Description; got != "Default collection" {
		t.Errorf("collection description = %q, want %q", got, "Default collection")
	}
}

// A policy-rejected resource stays listed for transparency, so it must keep
// its description even though its artifact is stripped.
func TestBuildSourceRevisionKeepsDescriptionWhenRejected(t *testing.T) {
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
		Roots:       []store.SourceRoot{{Path: ".agents", SourceHarness: "factory"}},
	}

	rev, err := BuildSourceRevision(ctx, cfg, store.MarketplaceID("mp-test"), source, repoDir, commit)
	if err != nil {
		t.Fatalf("BuildSourceRevision: %v", err)
	}

	skill := resourceRevisionFor(t, rev.Resources, "skill")
	if skill.ArchiveDigest != "" {
		t.Fatalf("rejected resource should not have an archive digest")
	}
	if skill.Description != "Review code" {
		t.Fatalf("rejected resource lost its description: %q", skill.Description)
	}
}

func TestSupportFilePathSafety(t *testing.T) {
	for _, name := range []string{"../escape", "nested/../escape", "./notes", "nested//notes", ""} {
		if _, err := validateSupportRelativePath(name); err == nil {
			t.Fatalf("unsafe support path accepted: %q", name)
		}
	}
	root := t.TempDir()
	target := filepath.Join(root, "link.txt")
	if err := os.WriteFile(filepath.Join(root, "real.txt"), []byte("safe"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real.txt", target); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := rejectSymlinkPath(root, target); err == nil {
		t.Fatal("symlink support file accepted")
	}
}

func createIngestRepo(tb testing.TB) string {
	tb.Helper()
	return createIngestRepoWithSkillLicense(tb, "MIT")
}

// createIngestRepoWithSkillLicense builds the shared fixture repository. An
// empty license omits the frontmatter key entirely, which is how a resource
// that declares no license at all reaches ingest.
func createIngestRepoWithSkillLicense(tb testing.TB, license string) string {
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
	frontmatter := "---\nname: review\ndescription: Review code\n"
	if license != "" {
		frontmatter += "license: " + license + "\n"
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(frontmatter+"---\nReview the code carefully.\n"), 0o644); err != nil {
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
