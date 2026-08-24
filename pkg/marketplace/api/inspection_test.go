package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/analyze"
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

func TestExactSkillInspectionIsImmutableAndReadableWhenRevoked(t *testing.T) {
	ctx := context.Background()
	meta := sqlite.New(":memory:")
	if err := meta.Open(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = meta.Close() }()
	blobs := fsblob.New(t.TempDir())
	const revision = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	src := store.Source{ID: "source-id", Slug: "source", Origins: []string{"https://example.com/repo"}, Ref: "main", Roots: []store.SourceRoot{{Path: ".agents"}}, Acquisition: store.AcquisitionIndexOnly}
	if err := meta.PutSource(ctx, src); err != nil {
		t.Fatal(err)
	}
	coord := store.ResourceCoordinate{Marketplace: "marketplace", Source: src.ID, Kind: KindSkill, Name: "review"}
	body := []byte("# Safe review\n\n[remote](https://example.com)\n")
	projection, err := inspection.Build(coord, revision, body, nil, []analyze.Finding{{Code: "warn", Severity: analyze.OutcomeWarn, PolicyOutcome: analyze.OutcomeWarn, SourceRange: analyze.SourceRangeAt("skill/review/body", string(body), 2)}}, 10, 1024, 4096)
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
	if err := meta.PutResourceRevision(ctx, store.ResourceRevision{Coordinate: coord, ContentDigest: revision, Findings: []store.Finding{{Code: "warn", Severity: analyze.OutcomeWarn}}, Provenance: &store.Provenance{InspectionManifestDigest: store.RevisionDigest(projection.Manifest.ManifestDigest)}, Revocation: &store.Revocation{Kind: store.RevocationRevision, TargetID: "revoked", Reason: "test"}}); err != nil {
		t.Fatal(err)
	}
	if err := meta.PutMarketplace(ctx, store.Marketplace{ID: "marketplace", Name: "Test", CanonicalURL: "https://example.com"}); err != nil {
		t.Fatal(err)
	}

	server := NewServer(config.Config{Instance: config.Instance{ID: "marketplace"}}, meta, blobs)
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
	resp, err = http.Get(testServer.URL + "/api/v1/inspection/sha256/" + strings.TrimPrefix(projection.Manifest.ManifestDigest, "sha256:"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("digest inspection status = %d", resp.StatusCode)
	}
}
