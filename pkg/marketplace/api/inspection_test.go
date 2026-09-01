package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/inspection"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/fsblob"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/sqlite"
)

func TestDigestRoutesRequireSha256PathEncoding(t *testing.T) {
	digest := strings.Repeat("a", 64)
	for _, route := range []string{
		"/api/v1/inspection/sha256/" + digest,
		"/api/v1/content/sha256/" + digest,
	} {
		got, ok := digestPath(route, strings.Split(route, "sha256/")[0])
		if !ok || got != "sha256:"+digest {
			t.Fatalf("digestPath(%q) = %q, %v", route, got, ok)
		}
	}
	for _, route := range []string{
		"/api/v1/inspection/" + "sha256:" + digest,
		"/api/v1/inspection/sha256/" + strings.ToUpper(digest),
		"/api/v1/inspection/sha256/" + digest + "/extra",
	} {
		if _, ok := digestPath(route, "/api/v1/inspection/"); ok {
			t.Fatalf("invalid digest route accepted: %q", route)
		}
	}
}

func TestContentPolicyRejectsActiveTextAndUnknownBytes(t *testing.T) {
	for _, body := range []string{"<html><script>alert(1)</script>", "<?xml version=\"1.0\"?>", "body { color: red }", "const exploit = true"} {
		if media, ok := fixedContentMediaType([]byte(body)); ok {
			t.Fatalf("unsafe body classified as %q: %q", media, body)
		}
	}
	if _, ok := fixedContentMediaType([]byte{0xff, 0xfe, 0xfd}); ok {
		t.Fatal("unknown binary body accepted")
	}
	if media, ok := fixedContentMediaType([]byte(`{"safe":true}`)); !ok || media != "application/json" {
		t.Fatalf("safe JSON policy = %q, %v", media, ok)
	}
}

func TestInspectionRoutesSupportEveryMarketplaceKind(t *testing.T) {
	ctx := context.Background()
	meta := sqlite.New(":memory:")
	if err := meta.Open(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = meta.Close() }()
	blobs := fsblob.New(t.TempDir())
	src := store.Registry{ID: "source-id", Slug: "source", Origins: []string{"https://example.com/repo"}, Ref: "main", Roots: []store.RegistryRoot{{Path: ".agents"}}, Acquisition: store.AcquisitionIndexOnly}
	if err := meta.PutRegistry(ctx, src); err != nil {
		t.Fatal(err)
	}
	if err := meta.PutMarketplace(ctx, store.Marketplace{ID: "marketplace", Name: "Test", CanonicalURL: "https://example.com"}); err != nil {
		t.Fatal(err)
	}
	const revisionPrefix = "sha256:"
	for i, kind := range []string{"skill", "command", "subagent", "hook", "mcpserver", "collection"} {
		revision := revisionPrefix + strings.Repeat(string('a'+byte(i)), 64)
		coord := store.ResourceCoordinate{Marketplace: "marketplace", Registry: src.ID, Kind: kind, Name: "example"}
		body := []byte("# safe " + kind)
		if kind == "mcpserver" || kind == "collection" {
			body = []byte(`{"name":"safe"}`)
		}
		projection, err := inspection.Build(coord, revision, body, map[string][]byte{"diagram.svg": []byte("<svg>unsafe</svg>"), "notes.txt": []byte("safe")}, 10, 1024, 4096)
		if err != nil {
			t.Fatal(err)
		}
		for digest, data := range projection.Bodies {
			if err := blobs.Put(ctx, store.ArchiveDigest(digest), data); err != nil {
				t.Fatal(err)
			}
		}
		if err := blobs.Put(ctx, store.ArchiveDigest(projection.Manifest.ManifestDigest), projection.Payload); err != nil {
			t.Fatal(err)
		}
		resource := store.ResourceRevision{Coordinate: coord, ContentDigest: store.RevisionDigest(revision)}
		commit := strings.Repeat(string('1'+byte(i)), 40)
		manifest := store.RevisionDigest("sha256:" + strings.Repeat(string('1'+byte(i)), 64))
		if err := meta.PutResourceRevision(ctx, resource); err != nil {
			t.Fatal(err)
		}
		if err := meta.PutRegistryOccurrence(ctx, store.RegistryOccurrence{RegistryID: src.ID, Ref: src.Ref, Commit: commit, RegistryRevisionManifestDigest: manifest, Acquisition: src.Acquisition, OccurredAt: time.Now().UTC()}); err != nil {
			t.Fatal(err)
		}
		if err := meta.PutResourceOccurrence(ctx, store.ResourceOccurrence{Coordinate: coord, ContentDigest: resource.ContentDigest, Commit: commit, ManifestDigest: manifest}); err != nil {
			t.Fatal(err)
		}
		assessment, err := store.FinalizeResourceAssessment(store.ResourceAssessment{Coordinate: coord, ContentDigest: resource.ContentDigest, AnalyzerVersion: "test/v1", PolicyDigest: store.PolicyDigest("sha256:" + strings.Repeat("f", 64)), Decision: store.AssessmentDecisionAllow, InspectionManifestDigest: store.RevisionDigest(projection.Manifest.ManifestDigest), AssessedAt: time.Now().UTC()})
		if err != nil {
			t.Fatal(err)
		}
		if err := meta.PutResourceAssessment(ctx, assessment); err != nil {
			t.Fatal(err)
		}
		if err := meta.SetCurrentResourceAssessment(ctx, assessment); err != nil {
			t.Fatal(err)
		}
	}
	server := NewServer(config.Config{Instance: config.Instance{ID: "marketplace"}}, meta, blobs)
	testServer := httptest.NewServer(server.PublicHandler())
	defer testServer.Close()
	for _, kind := range []string{"skill", "command", "subagent", "hook", "mcpserver", "collection"} {
		revision := "sha256:" + strings.Repeat(string('a'+byte(indexOfKind(kind))), 64)
		url := testServer.URL + "/api/v1/resources/source/" + kind + "/example/revisions/" + revision + "/inspection"
		resp, err := http.Get(url)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s inspection status = %d", kind, resp.StatusCode)
		}
		resp, err = http.Get(url + "/content")
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		wantMedia := inspection.MediaMarkdown
		if kind == "mcpserver" || kind == "collection" {
			wantMedia = inspection.MediaJSON
		}
		if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != wantMedia {
			t.Fatalf("%s inspection content = %d/%q, want %q", kind, resp.StatusCode, resp.Header.Get("Content-Type"), wantMedia)
		}
	}
}

func indexOfKind(kind string) int {
	for i, candidate := range []string{"skill", "command", "subagent", "hook", "mcpserver", "collection"} {
		if candidate == kind {
			return i
		}
	}
	return 0
}

func TestPublicMCPArtifactIsDownloadable(t *testing.T) {
	ctx := context.Background()
	meta := sqlite.New(":memory:")
	if err := meta.Open(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = meta.Close() }()
	blobs := fsblob.New(t.TempDir())
	source := store.Registry{ID: "source-id", Slug: "source", Origins: []string{"https://example.com/repo"}, Ref: "main", Roots: []store.RegistryRoot{{Path: ".agents"}}, Acquisition: store.AcquisitionMirror}
	if err := meta.PutRegistry(ctx, source); err != nil {
		t.Fatal(err)
	}
	if err := meta.PutMarketplace(ctx, store.Marketplace{ID: "marketplace", Name: "Test", CanonicalURL: "https://example.com"}); err != nil {
		t.Fatal(err)
	}
	artifact, err := hccartifact.FromIR(ir.NewMcpServer("remote", "npx", []string{"-y", "remote-mcp"}, nil, nil, ir.SourceRef{}))
	if err != nil {
		t.Fatal(err)
	}
	artifactData, archive, err := hccartifact.Encode(artifact)
	if err != nil {
		t.Fatal(err)
	}
	revision := store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "marketplace", Registry: source.ID, Kind: "mcpserver", Name: "remote"},
		ContentDigest: store.RevisionDigest(artifact.Manifest.ContentDigest), ArchiveDigest: store.ArchiveDigest(archive),
	}
	publishAssessedTestRevision(ctx, t, meta, revision, store.AssessmentDecisionAllow, store.RevisionDigest("sha256:"+strings.Repeat("d", 64)), strings.Repeat("e", 40))
	if err := blobs.Put(ctx, revision.ArchiveDigest, artifactData); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewServer(config.Config{Instance: config.Instance{ID: "marketplace"}}, meta, blobs).PublicHandler())
	defer server.Close()
	resp, err := http.Get(server.URL + "/api/v1/artifacts/" + strings.Replace(archive, ":", "/", 1))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("MCP artifact status = %d, want %d: %s", resp.StatusCode, http.StatusOK, body)
	}
	if string(body) != string(artifactData) {
		t.Fatal("MCP artifact response does not match stored archive")
	}
}

func TestExactSkillInspectionIsImmutableAndReadableWhenRevoked(t *testing.T) {
	ctx := context.Background()
	meta := sqlite.New(":memory:")
	if err := meta.Open(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = meta.Close() }()
	blobs := fsblob.New(t.TempDir())
	const revision = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	src := store.Registry{ID: "source-id", Slug: "source", Origins: []string{"https://example.com/repo"}, Ref: "main", Roots: []store.RegistryRoot{{Path: ".agents"}}, Acquisition: store.AcquisitionIndexOnly}
	if err := meta.PutRegistry(ctx, src); err != nil {
		t.Fatal(err)
	}
	coord := store.ResourceCoordinate{Marketplace: "marketplace", Registry: src.ID, Kind: KindSkill, Name: "review"}
	body := []byte("# Hostile review\n\n<div>raw HTML</div>\n<script>alert(1)</script>\n![remote image](https://evil.invalid/pixel.png)\n[link](https://example.com/docs)\n")
	support := map[string][]byte{
		"notes.md":    []byte("<script>support must stay hidden</script>"),
		"data.json":   []byte(`{"safe":true}`),
		"config.yaml": []byte("safe: true\\n"),
		"pixel.png":   {0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 0, 0, 0, 0x0d, 'I', 'H', 'D', 'R', 0, 0, 0, 1, 0, 0, 0, 1, 8, 6, 0, 0, 0},
	}
	projection, err := inspection.Build(coord, revision, body, support, 10, 1024, 4096)
	if err != nil {
		t.Fatal(err)
	}
	primaryFile := projection.Manifest.Files[0]
	if primaryFile.Path != "SKILL.md" || primaryFile.MediaType != inspection.MediaMarkdown || primaryFile.InspectionClass != inspection.ClassText {
		t.Fatalf("hostile primary policy = %#v", primaryFile)
	}
	if string(projection.Bodies[primaryFile.ContentDigest]) != string(body) {
		t.Fatal("hostile primary Markdown was not retained as canonical bytes")
	}
	var supportDigest string
	for _, file := range projection.Manifest.Files[1:] {
		if file.Path == "support/notes.md" {
			supportDigest = file.ContentDigest
			if file.InspectionClass != inspection.ClassMetadata || len(projection.Bodies[file.ContentDigest]) != 0 {
				t.Fatalf("hostile support policy = %#v", file)
			}
		}
	}
	if supportDigest == "" {
		t.Fatal("hostile support metadata missing")
	}
	for digest, data := range projection.Bodies {
		if err := blobs.Put(ctx, store.ArchiveDigest(digest), data); err != nil {
			t.Fatal(err)
		}
	}
	if err := blobs.Put(ctx, store.ArchiveDigest(projection.Manifest.ManifestDigest), projection.Payload); err != nil {
		t.Fatal(err)
	}
	resource := store.ResourceRevision{Coordinate: coord, ContentDigest: revision}
	if err := meta.PutResourceRevision(ctx, resource); err != nil {
		t.Fatal(err)
	}
	commit := strings.Repeat("e", 40)
	manifest := store.RevisionDigest("sha256:" + strings.Repeat("d", 64))
	if err := meta.PutRegistryOccurrence(ctx, store.RegistryOccurrence{RegistryID: src.ID, Ref: src.Ref, Commit: commit, RegistryRevisionManifestDigest: manifest, Acquisition: src.Acquisition, OccurredAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if err := meta.PutResourceOccurrence(ctx, store.ResourceOccurrence{Coordinate: coord, ContentDigest: revision, Commit: commit, ManifestDigest: manifest}); err != nil {
		t.Fatal(err)
	}
	assessment, err := store.FinalizeResourceAssessment(store.ResourceAssessment{Coordinate: coord, ContentDigest: revision, AnalyzerVersion: "test/v1", PolicyDigest: store.PolicyDigest("sha256:" + strings.Repeat("f", 64)), Decision: store.AssessmentDecisionWarn, Findings: []store.Finding{{Code: "warn", Severity: "warn"}}, InspectionManifestDigest: store.RevisionDigest(projection.Manifest.ManifestDigest), AssessedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	if err := meta.PutResourceAssessment(ctx, assessment); err != nil {
		t.Fatal(err)
	}
	if err := meta.SetCurrentResourceAssessment(ctx, assessment); err != nil {
		t.Fatal(err)
	}
	if err := meta.PutMarketplace(ctx, store.Marketplace{ID: "marketplace", Name: "Test", CanonicalURL: "https://example.com"}); err != nil {
		t.Fatal(err)
	}

	server := NewServer(config.Config{Instance: config.Instance{ID: "marketplace"}, Revocations: config.Revocations{Revisions: []string{coord.String() + "@" + revision}}}, meta, blobs)
	testServer := httptest.NewServer(server.PublicHandler())
	defer testServer.Close()
	base := testServer.URL + "/api/v1/resources/source/skill/review/revisions/" + revision
	resp, err := http.Get(base + "/inspection")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(data), `"primary_path":"SKILL.md"`) {
		t.Fatalf("inspection response = %d/%s", resp.StatusCode, data)
	}
	if resp.Header.Get("Cache-Control") != "public, max-age=31536000, immutable" || resp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("inspection headers = %#v", resp.Header)
	}
	resp, err = http.Get(base + "/inspection/content")
	if err != nil {
		t.Fatal(err)
	}
	data, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(data) != string(body) || resp.Header.Get("Content-Type") != inspection.MediaMarkdown {
		t.Fatalf("content response = %d/%q/%q", resp.StatusCode, data, resp.Header.Get("Content-Type"))
	}
	for _, expected := range []struct {
		path  string
		media string
		body  []byte
	}{
		{path: "support/data.json", media: inspection.MediaJSON, body: support["data.json"]},
		{path: "support/config.yaml", media: inspection.MediaYAML, body: support["config.yaml"]},
		{path: "support/pixel.png", media: inspection.MediaPNG, body: support["pixel.png"]},
	} {
		var digest string
		for _, file := range projection.Manifest.Files {
			if file.Path == expected.path {
				digest = file.ContentDigest
			}
		}
		if digest == "" {
			t.Fatalf("missing %s in inspection manifest", expected.path)
		}
		resp, err = http.Get(base + "/inspection/content/" + digest)
		if err != nil {
			t.Fatal(err)
		}
		data, _ = io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK || string(data) != string(expected.body) || resp.Header.Get("Content-Type") != expected.media {
			t.Fatalf("%s content = %d/%q/%q", expected.path, resp.StatusCode, data, resp.Header.Get("Content-Type"))
		}
	}
	resp, err = http.Get(base + "/inspection/content/" + supportDigest)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unsafe support content status = %d", resp.StatusCode)
	}
	resp, err = http.Get(testServer.URL + "/api/v1/content/" + strings.Replace(primaryFile.ContentDigest, ":", "/", 1))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("global hostile content status = %d", resp.StatusCode)
	}
	resp, err = http.Get(testServer.URL + "/api/v1/inspection/sha256/" + strings.TrimPrefix(projection.Manifest.ManifestDigest, "sha256:"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("digest inspection status = %d", resp.StatusCode)
	}
}
