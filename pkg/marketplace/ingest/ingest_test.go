package ingest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
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

func TestBuildRegistryRevision(t *testing.T) {
	ctx := context.Background()
	repoDir := createIngestRepo(t)
	commit, err := repoCommit(repoDir)
	if err != nil {
		t.Fatalf("repo commit: %v", err)
	}

	cfg := testConfig(t, repoDir)
	source := store.Registry{
		ID:          store.RegistryID("src-test"),
		Slug:        "example-source",
		Ref:         "main",
		Origins:     []string{"file://" + repoDir},
		Acquisition: store.AcquisitionMirror,
		License:     "MIT",
		Roots: []store.RegistryRoot{
			{Path: ".agents", SourceHarness: "factory"},
		},
	}

	rev, err := BuildRegistryRevision(ctx, cfg, store.MarketplaceID("mp-test"), source, repoDir, commit)
	if err != nil {
		t.Fatalf("BuildRegistryRevision: %v", err)
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

	if rev.Occurrence.RegistryRevisionManifestDigest != rev.ManifestDigest {
		t.Fatalf("occurrence manifest digest mismatch")
	}
	if rev.Occurrence.Ref != source.Ref {
		t.Fatalf("occurrence ref = %q, want %q", rev.Occurrence.Ref, source.Ref)
	}
	for _, field := range []string{"registry_url", "ref", "path", repoDir} {
		if strings.Contains(string(rev.ManifestPayload), field) {
			t.Fatalf("registry revision manifest exposed %q: %s", field, rev.ManifestPayload)
		}
	}
}

func TestBuildRegistryRevisionRecursivelyIngestsNestedBundles(t *testing.T) {
	repoDir := createIngestRepo(t)
	for _, fixture := range []struct {
		plugin string
		name   string
	}{
		{plugin: "alpha", name: "skill-a"},
		{plugin: "teams/platform/beta", name: "skill-b"},
	} {
		skillDir := filepath.Join(repoDir, "plugins", fixture.plugin, "skills", fixture.name)
		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			t.Fatal(err)
		}
		body := "---\nname: " + fixture.name + "\ndescription: Nested skill\nlicense: MIT\n---\nNested body.\n"
		if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	collectionDir := filepath.Join(repoDir, "plugins", "alpha", "collections", "nested-defaults")
	if err := os.MkdirAll(collectionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	collection := "name: nested-defaults\nversion: 1.0.0\nresources:\n  - name: skill-a\n    kind: skill\n"
	if err := os.WriteFile(filepath.Join(collectionDir, "COLLECTION.yaml"), []byte(collection), 0o644); err != nil {
		t.Fatal(err)
	}
	commit, err := repoCommit(repoDir)
	if err != nil {
		t.Fatalf("repo commit: %v", err)
	}
	source := store.Registry{
		ID:          "src-test",
		Slug:        "example-source",
		Ref:         "main",
		Origins:     []string{"file://" + repoDir},
		Acquisition: store.AcquisitionMirror,
		License:     "MIT",
		Roots:       []store.RegistryRoot{{Path: "plugins", SourceHarness: "claude"}},
	}

	rev, err := BuildRegistryRevision(context.Background(), testConfig(t, repoDir), "mp-test", source, repoDir, commit)
	if err != nil {
		t.Fatalf("BuildRegistryRevision: %v", err)
	}
	got := make(map[string]bool)
	for _, resource := range rev.Resources {
		got[resource.Coordinate.Kind+"/"+resource.Coordinate.Name] = true
	}
	for _, want := range []string{"skill/skill-a", "skill/skill-b", "collection/nested-defaults"} {
		if !got[want] {
			t.Errorf("missing recursively discovered resource %q: %v", want, got)
		}
	}
}

func TestBuildRegistryRevisionRejectsNestedBundleDiagnostics(t *testing.T) {
	repoDir := createIngestRepo(t)
	skillDir := filepath.Join(repoDir, "plugins", "broken", "skills", "broken")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: [broken\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	commit, err := repoCommit(repoDir)
	if err != nil {
		t.Fatalf("repo commit: %v", err)
	}
	source := store.Registry{
		ID:          "src-test",
		Slug:        "example-source",
		Ref:         "main",
		Origins:     []string{"file://" + repoDir},
		Acquisition: store.AcquisitionMirror,
		Roots:       []store.RegistryRoot{{Path: "plugins", SourceHarness: "claude"}},
	}

	_, err = BuildRegistryRevision(context.Background(), testConfig(t, repoDir), "mp-test", source, repoDir, commit)
	if err == nil || !strings.Contains(err.Error(), diag.CodeIngestClaudeLoadSkillFailed) {
		t.Fatalf("BuildRegistryRevision error = %v", err)
	}
}

func TestBuildRegistryRevisionRejectsMissingRoot(t *testing.T) {
	repoDir := createIngestRepo(t)
	commit, err := repoCommit(repoDir)
	if err != nil {
		t.Fatalf("repo commit: %v", err)
	}
	source := store.Registry{
		ID:          "src-test",
		Slug:        "example-source",
		Ref:         "main",
		Origins:     []string{"file://" + repoDir},
		Acquisition: store.AcquisitionMirror,
		Roots:       []store.RegistryRoot{{Path: "missing", SourceHarness: "claude"}},
	}

	_, err = BuildRegistryRevision(context.Background(), testConfig(t, repoDir), "mp-test", source, repoDir, commit)
	if err == nil || !strings.Contains(err.Error(), `root path "missing" does not exist`) {
		t.Fatalf("BuildRegistryRevision error = %v", err)
	}
}

func TestBuildRegistryRevisionWarnsAndKeepsFirstDuplicateAcrossRoots(t *testing.T) {
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
	source := store.Registry{
		ID:          "src-test",
		Slug:        "example-source",
		Ref:         "main",
		Origins:     []string{"file://" + repoDir},
		Acquisition: store.AcquisitionMirror,
		License:     "MIT",
		Roots: []store.RegistryRoot{
			{Path: ".agents", SourceHarness: "factory"},
		},
	}
	baseline, err := BuildRegistryRevision(ctx, cfg, "mp-test", source, repoDir, commit)
	if err != nil {
		t.Fatalf("baseline BuildRegistryRevision: %v", err)
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

	source.Roots = append(source.Roots, store.RegistryRoot{Path: ".agents-duplicate", SourceHarness: "factory"})
	rev, err := BuildRegistryRevision(ctx, cfg, "mp-test", source, repoDir, commit)
	if err != nil {
		t.Fatalf("BuildRegistryRevision: %v", err)
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

func TestBuildRegistryRevisionRejectsDuplicateAtSamePath(t *testing.T) {
	repoDir := createIngestRepo(t)
	commit, err := repoCommit(repoDir)
	if err != nil {
		t.Fatalf("repo commit: %v", err)
	}
	source := store.Registry{
		ID:          "src-test",
		Slug:        "example-source",
		Ref:         "main",
		Origins:     []string{"file://" + repoDir},
		Acquisition: store.AcquisitionMirror,
		License:     "MIT",
		Roots: []store.RegistryRoot{
			{Path: ".agents", SourceHarness: "factory"},
			{Path: ".agents", SourceHarness: "factory"},
		},
	}
	_, err = BuildRegistryRevision(context.Background(), testConfig(t, repoDir), "mp-test", source, repoDir, commit)
	if err == nil || !strings.Contains(err.Error(), `duplicate resource "skill/review" at ".agents/skills/review/SKILL.md"`) {
		t.Fatalf("BuildRegistryRevision error = %v", err)
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
	report, err := analyze.NewEngine().Analyze(context.Background(), analyze.Subject{
		Resource: mcp,
		Files:    nil,
		Limits:   analyze.DefaultLimits(),
	})
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
		store.ResourceCoordinate{Marketplace: "marketplace", Registry: "source-id", Kind: "mcpserver", Name: "hostile"},
		"sha256:"+strings.Repeat("a", 64), primary, support, 10, 4096, 8192,
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
	source := store.Registry{ID: "source-id", Slug: "source", Origins: []string{"https://example.invalid/repo"}, Ref: "main", Roots: []store.RegistryRoot{{Path: ".agents"}}, Acquisition: store.AcquisitionIndexOnly}
	if err := meta.PutRegistry(ctx, source); err != nil {
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
	coord := store.ResourceCoordinate{Marketplace: "marketplace", Registry: source.ID, Kind: "mcpserver", Name: "hostile"}
	resource := store.ResourceRevision{Coordinate: coord, ContentDigest: store.RevisionDigest(revision)}
	commit := strings.Repeat("c", 40)
	manifestDigest := store.RevisionDigest("sha256:" + strings.Repeat("d", 64))
	assessment, err := store.FinalizeResourceAssessment(store.ResourceAssessment{Coordinate: coord, ContentDigest: resource.ContentDigest, AnalyzerVersion: analyze.AnalyzerVersionV1, PolicyDigest: store.PolicyDigest("sha256:" + strings.Repeat("b", 64)), Decision: store.AssessmentDecisionWarn, InspectionManifestDigest: store.RevisionDigest(projection.Manifest.ManifestDigest), AssessedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	for _, put := range []func() error{
		func() error { return meta.PutResourceRevision(ctx, resource) },
		func() error {
			return meta.PutRegistryOccurrence(ctx, store.RegistryOccurrence{RegistryID: source.ID, Ref: source.Ref, Commit: commit, RegistryRevisionManifestDigest: manifestDigest, Acquisition: source.Acquisition, OccurredAt: time.Now().UTC()})
		},
		func() error {
			return meta.PutResourceOccurrence(ctx, store.ResourceOccurrence{Coordinate: coord, ContentDigest: resource.ContentDigest, Commit: commit, ManifestDigest: manifestDigest})
		},
		func() error { return meta.PutResourceAssessment(ctx, assessment) },
		func() error { return meta.SetCurrentResourceAssessment(ctx, assessment) },
	} {
		if err := put(); err != nil {
			t.Fatal(err)
		}
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

func TestBuildRegistryRevisionIndexOnly(t *testing.T) {
	ctx := context.Background()
	repoDir := createIngestRepo(t)
	commit, err := repoCommit(repoDir)
	if err != nil {
		t.Fatalf("repo commit: %v", err)
	}

	cfg := testConfig(t, repoDir)
	cfg.Policy.LicenseAllowlist = []string{"Apache-2.0"}
	source := store.Registry{
		ID:          store.RegistryID("src-test"),
		Slug:        "example-source",
		Ref:         "main",
		Origins:     []string{"file://" + repoDir},
		Acquisition: store.AcquisitionIndexOnly,
		License:     "MIT",
		Roots: []store.RegistryRoot{
			{Path: ".agents", SourceHarness: "factory"},
		},
	}

	rev, err := BuildRegistryRevision(ctx, cfg, store.MarketplaceID("mp-test"), source, repoDir, commit)
	if err != nil {
		t.Fatalf("BuildRegistryRevision: %v", err)
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
	if assessment := resourceAssessmentFor(t, rev.Assessments, "skill"); assessment.Decision != store.AssessmentDecisionReject {
		t.Fatalf("assessment decision = %q, want reject", assessment.Decision)
	}
}

func TestBuildRegistryRevisionCollection(t *testing.T) {
	ctx := context.Background()
	repoDir := createIngestRepo(t)
	commit, err := repoCommit(repoDir)
	if err != nil {
		t.Fatalf("repo commit: %v", err)
	}

	cfg := testConfig(t, repoDir)
	source := store.Registry{
		ID:          store.RegistryID("src-test"),
		Slug:        "example-source",
		Ref:         "main",
		Origins:     []string{"file://" + repoDir},
		Acquisition: store.AcquisitionMirror,
		License:     "MIT",
		Roots: []store.RegistryRoot{
			{Path: ".agents", SourceHarness: "factory"},
		},
	}

	rev, err := BuildRegistryRevision(ctx, cfg, store.MarketplaceID("mp-test"), source, repoDir, commit)
	if err != nil {
		t.Fatalf("BuildRegistryRevision: %v", err)
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
	manifest, payload, err := marketplaceartifact.DecodeRegistryRevisionManifest(bytes.NewReader(rev.ManifestPayload))
	if err != nil {
		t.Fatalf("decode registry revision manifest: %v", err)
	}
	manifest.RevisionDigest = string(rev.ManifestDigest)
	if err := marketplaceartifact.VerifyRegistryRevisionManifest(manifest, payload); err != nil {
		t.Fatalf("verify registry revision manifest: %v", err)
	}
	for _, resource := range manifest.Resources {
		if resource.Kind == "collection" && resource.ArchiveDigest != "" {
			t.Fatalf("registry revision manifest exposed collection archive digest: %#v", resource)
		}
	}
}

func TestBuildRegistryRevisionNestedCollectionDeduplicatesLeavesAndRetainsPaths(t *testing.T) {
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

	source := store.Registry{ID: "src-test", Slug: "example-source", Ref: "main", Origins: []string{"file://" + repoDir}, Acquisition: store.AcquisitionMirror, License: "MIT", Roots: []store.RegistryRoot{{Path: ".agents", SourceHarness: "factory"}}}
	rev, err := BuildRegistryRevision(context.Background(), testConfig(t, repoDir), "mp-test", source, repoDir, mustRepoCommit(t, repoDir))
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

func TestBuildRegistryRevisionRejectsNestedCollectionCycle(t *testing.T) {
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
	source := store.Registry{ID: "src-test", Slug: "example-source", Ref: "main", Origins: []string{"file://" + repoDir}, Acquisition: store.AcquisitionMirror, License: "MIT", Roots: []store.RegistryRoot{{Path: ".agents", SourceHarness: "factory"}}}
	_, err := BuildRegistryRevision(context.Background(), testConfig(t, repoDir), "mp-test", source, repoDir, mustRepoCommit(t, repoDir))
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

func TestBuildRegistryRevisionLicenseRejection(t *testing.T) {
	ctx := context.Background()
	repoDir := createIngestRepo(t)
	commit, err := repoCommit(repoDir)
	if err != nil {
		t.Fatalf("repo commit: %v", err)
	}

	cfg := testConfig(t, repoDir)
	cfg.Policy.LicenseAllowlist = []string{"Apache-2.0"}

	source := store.Registry{
		ID:          store.RegistryID("src-test"),
		Slug:        "example-source",
		Ref:         "main",
		Origins:     []string{"file://" + repoDir},
		Acquisition: store.AcquisitionMirror,
		License:     "MIT",
		Roots: []store.RegistryRoot{
			{Path: ".agents", SourceHarness: "factory"},
		},
	}

	rev, err := BuildRegistryRevision(ctx, cfg, store.MarketplaceID("mp-test"), source, repoDir, commit)
	if err != nil {
		t.Fatalf("BuildRegistryRevision: %v", err)
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
	if skill.ArchiveDigest == "" {
		t.Fatal("rejected assessment should not erase intrinsic archive identity")
	}
	assessment := resourceAssessmentFor(t, rev.Assessments, "skill")
	if len(assessment.Findings) == 0 || assessment.Decision != store.AssessmentDecisionReject {
		t.Fatalf("rejected resource assessment = %#v", assessment)
	}
}

func TestRegistryRevisionIdentityIgnoresPolicyAssessment(t *testing.T) {
	ctx := context.Background()
	repoDir := createIngestRepo(t)
	commit := mustRepoCommit(t, repoDir)
	source := store.Registry{ID: "src-test", Slug: "example-source", Ref: "main", Origins: []string{"file://" + repoDir}, Acquisition: store.AcquisitionMirror, License: "MIT", Roots: []store.RegistryRoot{{Path: ".agents", SourceHarness: "factory"}}}
	allowed, err := BuildRegistryRevision(ctx, testConfig(t, repoDir), "mp-test", source, repoDir, commit)
	if err != nil {
		t.Fatalf("allowed BuildRegistryRevision: %v", err)
	}
	blockedConfig := testConfig(t, repoDir)
	blockedConfig.Policy.LicenseAllowlist = []string{"Apache-2.0"}
	blocked, err := BuildRegistryRevision(ctx, blockedConfig, "mp-test", source, repoDir, commit)
	if err != nil {
		t.Fatalf("blocked BuildRegistryRevision: %v", err)
	}
	if allowed.ManifestDigest != blocked.ManifestDigest {
		t.Fatalf("source manifest digest changed with policy: %s != %s", allowed.ManifestDigest, blocked.ManifestDigest)
	}
	allowedSkill := resourceRevisionFor(t, allowed.Resources, "skill")
	blockedSkill := resourceRevisionFor(t, blocked.Resources, "skill")
	if allowedSkill.ArchiveDigest == "" || allowedSkill.ArchiveDigest != blockedSkill.ArchiveDigest {
		t.Fatalf("archive digest changed with policy: %s != %s", allowedSkill.ArchiveDigest, blockedSkill.ArchiveDigest)
	}
}

func resourceAssessmentFor(tb testing.TB, assessments []store.ResourceAssessment, kind string) store.ResourceAssessment {
	tb.Helper()
	for _, assessment := range assessments {
		if assessment.Coordinate.Kind == kind {
			return assessment
		}
	}
	tb.Fatalf("no %s resource assessment was built", kind)
	return store.ResourceAssessment{}
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
func TestBuildRegistryRevisionCarriesDescriptions(t *testing.T) {
	ctx := context.Background()
	repoDir := createIngestRepo(t)
	commit, err := repoCommit(repoDir)
	if err != nil {
		t.Fatalf("repo commit: %v", err)
	}

	source := store.Registry{
		ID:          store.RegistryID("src-test"),
		Slug:        "example-source",
		Ref:         "main",
		Origins:     []string{"file://" + repoDir},
		Acquisition: store.AcquisitionMirror,
		License:     "MIT",
		Roots:       []store.RegistryRoot{{Path: ".agents", SourceHarness: "factory"}},
	}

	rev, err := BuildRegistryRevision(ctx, testConfig(t, repoDir), store.MarketplaceID("mp-test"), source, repoDir, commit)
	if err != nil {
		t.Fatalf("BuildRegistryRevision: %v", err)
	}

	if got := resourceRevisionFor(t, rev.Resources, "skill").Description; got != "Review code" {
		t.Errorf("skill description = %q, want %q", got, "Review code")
	}
	if got := resourceRevisionFor(t, rev.Resources, "collection").Description; got != "Default collection" {
		t.Errorf("collection description = %q, want %q", got, "Default collection")
	}
}

// A policy-rejected resource stays listed for transparency, so it must keep
// its intrinsic metadata while its assessment blocks installation.
func TestBuildRegistryRevisionKeepsDescriptionWhenRejected(t *testing.T) {
	ctx := context.Background()
	repoDir := createIngestRepo(t)
	commit, err := repoCommit(repoDir)
	if err != nil {
		t.Fatalf("repo commit: %v", err)
	}

	cfg := testConfig(t, repoDir)
	cfg.Policy.LicenseAllowlist = []string{"Apache-2.0"}

	source := store.Registry{
		ID:          store.RegistryID("src-test"),
		Slug:        "example-source",
		Ref:         "main",
		Origins:     []string{"file://" + repoDir},
		Acquisition: store.AcquisitionMirror,
		License:     "MIT",
		Roots:       []store.RegistryRoot{{Path: ".agents", SourceHarness: "factory"}},
	}

	rev, err := BuildRegistryRevision(ctx, cfg, store.MarketplaceID("mp-test"), source, repoDir, commit)
	if err != nil {
		t.Fatalf("BuildRegistryRevision: %v", err)
	}

	skill := resourceRevisionFor(t, rev.Resources, "skill")
	if skill.ArchiveDigest == "" {
		t.Fatal("rejected assessment should retain intrinsic archive identity")
	}
	if assessment := resourceAssessmentFor(t, rev.Assessments, "skill"); assessment.Decision != store.AssessmentDecisionReject {
		t.Fatalf("assessment decision = %q, want reject", assessment.Decision)
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
		{"git", "init", "-q", "--initial-branch=main"},
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

func TestEffectivePolicyHiddenContentOverride(t *testing.T) {
	for _, tc := range []struct {
		outcome string
		want    string
	}{
		{outcome: "warn", want: analyze.OutcomeWarn},
		{outcome: "reject", want: analyze.OutcomeReject},
	} {
		policy := effectivePolicy(config.Config{Policy: config.Policy{HiddenContent: tc.outcome}}, nil)
		if got := policy.OutcomeFor(analyze.FindingHTMLComment); got != tc.want {
			t.Errorf("hidden_content %q outcome = %q, want %q", tc.outcome, got, tc.want)
		}
	}
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

func TestBuildRegistryRevisionCollectionAnalyzerDeferredAndCached(t *testing.T) {
	ctx := context.Background()
	repoDir := createIngestRepo(t)
	commit, err := repoCommit(repoDir)
	if err != nil {
		t.Fatalf("repo commit: %v", err)
	}

	script, counter, digest := writeCounterAnalyzer(t)
	cfg := testConfig(t, repoDir)
	cfg.Policy.Analyzers = []config.Analyzer{{
		ID:      "marketplace.test",
		Role:    analyze.RoleGating,
		Version: "1.0.0",
		Run: &config.AnalyzerRun{
			Command:              script,
			Args:                 []string{counter},
			Timeout:              config.Duration(time.Minute),
			ImplementationDigest: digest,
		},
	}}

	source := store.Registry{
		ID:          store.RegistryID("src-test"),
		Slug:        "example-source",
		Ref:         "main",
		Origins:     []string{"file://" + repoDir},
		Acquisition: store.AcquisitionMirror,
		License:     "MIT",
		Roots: []store.RegistryRoot{
			{Path: ".agents", SourceHarness: "factory"},
		},
	}

	db := filepath.Join(t.TempDir(), "marketplace.db")
	meta := sqlite.New(db)
	if err := meta.Open(ctx); err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = meta.Close() })

	rev1, err := BuildRegistryRevisionWithAuditStore(ctx, cfg, store.MarketplaceID("mp-test"), source, repoDir, commit, meta)
	if err != nil {
		t.Fatalf("BuildRegistryRevisionWithAuditStore: %v", err)
	}
	if got := counterSize(t, counter); got != 2 {
		t.Fatalf("first build counter=%d, want 2 (skill + collection)", got)
	}
	collectionAudit1 := findCollectionAudit(t, rev1.Audits, "defaults")
	if collectionAudit1.SubjectDigest == "" {
		t.Fatal("collection audit missing subject digest")
	}
	if collectionAudit1.ResultDigest == "" {
		t.Fatal("collection audit missing result digest")
	}

	for _, a := range rev1.Audits {
		if err := meta.PutResourceAudit(ctx, a); err != nil {
			t.Fatalf("PutResourceAudit: %v", err)
		}
	}

	rev2, err := BuildRegistryRevisionWithAuditStore(ctx, cfg, store.MarketplaceID("mp-test"), source, repoDir, commit, meta)
	if err != nil {
		t.Fatalf("BuildRegistryRevisionWithAuditStore second build: %v", err)
	}
	if got := counterSize(t, counter); got != 2 {
		t.Fatalf("second build counter=%d, want 2 (cache hit)", got)
	}
	collectionAudit2 := findCollectionAudit(t, rev2.Audits, "defaults")
	if collectionAudit2.SubjectDigest != collectionAudit1.SubjectDigest {
		t.Fatalf("cached collection subject digest changed: %s != %s", collectionAudit2.SubjectDigest, collectionAudit1.SubjectDigest)
	}
	if collectionAudit2.ResultDigest != collectionAudit1.ResultDigest {
		t.Fatalf("cached collection result digest changed: %s != %s", collectionAudit2.ResultDigest, collectionAudit1.ResultDigest)
	}
	if !collectionAudit2.AssessedAt.Equal(collectionAudit1.AssessedAt) {
		t.Fatalf("cached collection assessed_at changed: %v != %v", collectionAudit2.AssessedAt, collectionAudit1.AssessedAt)
	}

	skillPath := filepath.Join(repoDir, ".agents", "skills", "review", "SKILL.md")
	skillBody, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatalf("read skill body: %v", err)
	}
	if err := os.WriteFile(skillPath, append(skillBody, []byte("\n<!-- changed -->\n")...), 0o644); err != nil {
		t.Fatalf("write skill body: %v", err)
	}
	for _, c := range [][]string{{"git", "add", "."}, {"git", "commit", "-q", "-m", "changed"}} {
		cmd := exec.Command(c[0], c[1:]...)
		cmd.Dir = repoDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", c, err, out)
		}
	}

	newCommit, err := repoCommit(repoDir)
	if err != nil {
		t.Fatalf("repo commit after change: %v", err)
	}
	rev3, err := BuildRegistryRevisionWithAuditStore(ctx, cfg, store.MarketplaceID("mp-test"), source, repoDir, newCommit, meta)
	if err != nil {
		t.Fatalf("BuildRegistryRevisionWithAuditStore after leaf change: %v", err)
	}
	if got := counterSize(t, counter); got != 4 {
		t.Fatalf("after leaf change counter=%d, want 4 (skill + collection rerun)", got)
	}

	collectionRev1 := findResourceRevision(t, rev1.Resources, "collection", "defaults")
	collectionRev3 := findResourceRevision(t, rev3.Resources, "collection", "defaults")
	if collectionRev1.ContentDigest == collectionRev3.ContentDigest {
		t.Fatalf("collection manifest digest did not change after leaf change")
	}
	collectionAudit3 := findCollectionAudit(t, rev3.Audits, "defaults")
	if collectionAudit3.SubjectDigest == collectionAudit1.SubjectDigest {
		t.Fatalf("collection subject digest did not change after leaf change")
	}
	if collectionAudit3.ResultDigest == "" {
		t.Fatal("collection audit after leaf change missing result digest")
	}
}

func writeCounterAnalyzer(t *testing.T) (script, counter, digest string) {
	t.Helper()
	dir := t.TempDir()
	counter = filepath.Join(dir, "counter")
	script = filepath.Join(dir, "counter.sh")
	content := `#!/bin/sh
read -r _
printf '%s\n' '{"schema_version":"agent-marketplace.analyzer/v1","analyzer_id":"marketplace.test","analyzer_version":"1.0.0","verdict":"pass","summary":"ok"}'
printf 'x' >> "$1"
`
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatalf("write counter script: %v", err)
	}
	data, err := os.ReadFile(script)
	if err != nil {
		t.Fatalf("read counter script: %v", err)
	}
	sum := sha256.Sum256(data)
	digest = "sha256:" + hex.EncodeToString(sum[:])
	return
}

func counterSize(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatalf("read counter: %v", err)
	}
	return len(data)
}

func findCollectionAudit(t *testing.T, audits []store.ResourceAudit, name string) store.ResourceAudit {
	t.Helper()
	for _, a := range audits {
		if a.Coordinate.Kind == "collection" && a.Coordinate.Name == name {
			return a
		}
	}
	t.Fatalf("no collection audit for %q", name)
	return store.ResourceAudit{}
}

func findResourceRevision(t *testing.T, revs []store.ResourceRevision, kind, name string) store.ResourceRevision {
	t.Helper()
	for _, r := range revs {
		if r.Coordinate.Kind == kind && r.Coordinate.Name == name {
			return r
		}
	}
	t.Fatalf("no %s/%s resource revision", kind, name)
	return store.ResourceRevision{}
}

// TestToStoreFindingsSupportLocationByMembership proves support-file finding
// locations resolve by exact FileSet membership, so support paths colliding
// with primary-resource pseudo-path substrings (/body, /prompt, /Description,
// /command, /url, /name) keep their correct persisted path and byte offsets.
// Exact FileSet membership wins for the raw path, so legal support keys that
// begin "support/" or live beneath the <kind>/<name> pseudo-subtree still
// resolve to support content; only the ambiguous bare pseudo-root retains
// primary precedence, while non-member pseudo-paths and unknown files fall
// back to the primary body.
func TestToStoreFindingsSupportLocationByMembership(t *testing.T) {
	body := []byte("primary body\n")
	support := analyze.FileSet{
		"notes/body":           []byte("alpha\nbeta\n"),
		"docs/prompt.md":       []byte("g\ngamma\n"),
		"guide/Description.md": []byte("del\ndelta\n"),
		"plain.md":             []byte("epsilon\nx\n"),
		"prompt.txt":           []byte("kappa\nlambda\n"),
		"skill/demo":           []byte("zeta\neta\n"),
		"skill/demo/Body":      []byte("theta\niota\n"),
		"skill/demo/x.md":      []byte("tau\nupsilon\n"),
		"skill/helper.md":      []byte("mu\nnu\n"),
		"support/notes.md":     []byte("rho\nsigma\n"),
	}
	primaryPath := inspection.PrimaryPath("skill")

	cases := []struct {
		name     string
		file     string
		wantPath string
		wantByte int64
	}{
		{"body suffix collision", "notes/body", "support/notes/body", 6},
		{"prompt substring collision", "docs/prompt.md", "support/docs/prompt.md", 2},
		{"description substring collision", "guide/Description.md", "support/guide/Description.md", 4},
		{"support-prefixed key", "support/plain.md", "support/plain.md", 8},
		{"explicit support reference", "support/notes/body", "support/notes/body", 6},
		{"reserved support-namespace key", "support/notes.md", "support/support/notes.md", 4},
		{"hook prompt support key", "prompt.txt", "support/prompt.txt", 6},
		{"explicit prompt support reference", "support/prompt.txt", "support/prompt.txt", 6},
		{"canonical primary path", "SKILL.md", primaryPath, 0},
		{"primary pseudo-root collision", "skill/demo", primaryPath, 0},
		{"primary pseudo-prefix member", "skill/demo/Body", "support/skill/demo/Body", 6},
		{"pseudo-subtree support member", "skill/demo/x.md", "support/skill/demo/x.md", 4},
		{"primary pseudo-path", "skill/demo/body", primaryPath, 0},
		{"non-colliding kind-subtree support key", "skill/helper.md", "support/skill/helper.md", 3},
		{"unknown path", "missing.md", primaryPath, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			findings := []analyze.Finding{{
				Code:        "test.finding",
				Severity:    analyze.OutcomeWarn,
				Message:     "m",
				SourceRange: &analyze.SourceRange{File: tc.file, Line: 2, Column: 1},
			}}
			got := toStoreFindings(findings, primaryPath, "demo", body, support)
			if len(got) != 1 || got[0].Location == nil {
				t.Fatalf("expected one located finding, got %+v", got)
			}
			loc := got[0].Location
			if loc.Path != tc.wantPath {
				t.Fatalf("location path = %q, want %q", loc.Path, tc.wantPath)
			}
			if tc.wantPath == primaryPath {
				// Byte offsets come from the primary body: line 2 col 1 is
				// past the end, clamped to len(body).
				if loc.StartByte != int64(len(body)) {
					t.Fatalf("primary start byte = %d, want %d", loc.StartByte, len(body))
				}
				return
			}
			if loc.StartByte != tc.wantByte {
				t.Fatalf("support start byte = %d, want %d", loc.StartByte, tc.wantByte)
			}
		})
	}
}

// TestLineColumnBytesClampsAtEOL proves an over-EOL column stops at the
// newline of the addressed line instead of walking into subsequent lines.
func TestLineColumnBytesClampsAtEOL(t *testing.T) {
	content := []byte("ab\ncdef\n")
	cases := []struct {
		name      string
		line      int
		column    int
		wantStart int
		wantEnd   int
	}{
		{"within line", 1, 2, 1, 2},
		{"at newline column", 1, 3, 2, 2},
		{"past end of line", 1, 9, 2, 2},
		{"past end of later line", 2, 8, 7, 7},
		{"past end of content", 5, 1, len(content), len(content)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			start, end := lineColumnBytes(content, tc.line, tc.column)
			if start != tc.wantStart || end != tc.wantEnd {
				t.Fatalf("lineColumnBytes(%d, %d) = (%d, %d), want (%d, %d)",
					tc.line, tc.column, start, end, tc.wantStart, tc.wantEnd)
			}
		})
	}
}
