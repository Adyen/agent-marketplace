package e2e_test

import (
	"bytes"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	mpartifact "github.com/Adyen/agent-marketplace/pkg/marketplace/artifact"
	mpconfig "github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/ingest"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/worker"
)

func TestServeResetRepairsLegacyMirrorWithoutSourceChanges(t *testing.T) {
	for _, mode := range []string{"file", "database"} {
		t.Run(mode, func(t *testing.T) { testServeResetRecovery(t, mode) })
	}
}

func testServeResetRecovery(t *testing.T, mode string) {
	t.Helper()
	env, gitOrigin := newServeReindexEnvWithURL(t)
	const script = "#!/bin/sh\necho complete-support\n"
	bare := env.CreateBareRepo("support-reg", map[string]string{
		".agents/skills/review/SKILL.md":           "---\nname: review\ndescription: Review code carefully\nlicense: MIT\n---\nReview using scripts/run.sh.\n",
		".agents/skills/review/scripts/run.sh":     script,
		".agents/collections/team/COLLECTION.yaml": "name: team\ndescription: Team collection\nresources:\n  - kind: skill\n    name: review\n",
	})
	config := strings.NewReplacer(
		"/serve-reg.git", "/support-reg.git",
		"dsn: marketplace.db", "dsn: "+filepath.Join(env.workspace, "marketplace.db"),
		"path: blobs", "path: "+filepath.Join(env.workspace, "blobs"),
		"    slug: serve-source", "    slug: serve-source\n    license: MIT",
	).Replace(env.ReadFile("/workspace/marketplace.yaml"))
	caPath := filepath.Join(env.workspace, "git-ca.pem")
	if mode == "database" {
		gitServer := newGitHTTPServer(t, env.repos)
		t.Cleanup(gitServer.Close)
		tlsServer := httptest.NewTLSServer(gitServer.Config.Handler)
		t.Cleanup(tlsServer.Close)
		env.WriteFile(caPath, string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: tlsServer.Certificate().Raw})))
		config = strings.ReplaceAll(config, strings.ReplaceAll(gitOrigin, "/serve-reg.git", "/support-reg.git"), tlsServer.URL+"/support-reg.git")
		config = strings.ReplaceAll(config, "    origin: ", "    ca_file: "+caPath+"\n    origin: ")
	}
	env.WriteFile("/workspace/marketplace.yaml", config)
	source := filepath.Join(env.workspace, "source")
	env.MustRun(fmt.Sprintf("git clone %q %q", bare, source))
	commit := strings.TrimSpace(env.MustRun(fmt.Sprintf("git -C %q rev-parse HEAD", source)).Stdout)
	cfg, meta, blobs := openPublicGateStores(t, env.mustResolvePath("/workspace/marketplace.yaml"))
	defer func() { _ = meta.Close() }()
	deps := worker.Deps{Config: cfg, MetadataStore: meta, BlobStore: blobs}
	if err := worker.EnsureMarketplace(t.Context(), &deps); err != nil {
		t.Fatal(err)
	}
	registry := store.Registry{ID: "src-serve", Slug: "serve-source", Ref: "main", Origins: []string{cfg.Registries[0].Origin}, Acquisition: store.AcquisitionMirror, Roots: []store.RegistryRoot{{Path: ".agents", SourceHarness: "factory"}}, Listed: true, License: "MIT"}
	if err := meta.PutRegistry(t.Context(), registry); err != nil {
		t.Fatal(err)
	}
	rev, err := ingest.BuildRegistryRevision(t.Context(), cfg, deps.Marketplace.ID, registry, source, commit)
	if err != nil {
		t.Fatal(err)
	}
	if len(rev.Collections) == 0 {
		t.Fatal("fixture did not discover its collection")
	}
	stripLegacySupport(t, &rev)
	var publicationID store.PublicationID
	if mode == "database" {
		publicationID = "legacy-publication"
	}
	if err := store.PublishRegistryRevision(t.Context(), meta, blobs, store.RegistryRevisionPublication{
		PublicationID: publicationID, Occurrence: rev.Occurrence, Resources: rev.Resources,
		ResourceOccurrences: rev.ResourceOccurrences, Assessments: rev.Assessments, Collections: rev.Collections,
		ManifestPayload: rev.ManifestPayload, ExtraBlobs: rev.ExtraBlobs, InspectionBlobs: rev.InspectionBlobs,
	}); err != nil {
		t.Fatal(err)
	}
	coordinate := store.ResourceCoordinate{Marketplace: deps.Marketplace.ID, Registry: registry.ID, Kind: "skill", Name: "review"}
	legacy, err := meta.GetLatestResourceRevision(t.Context(), coordinate)
	if err != nil {
		t.Fatal(err)
	}
	legacyBytes, err := blobs.Get(t.Context(), legacy.ArchiveDigest)
	if err != nil {
		t.Fatal(err)
	}
	if mode == "database" {
		origin, err := url.Parse(cfg.Registries[0].Origin)
		if err != nil {
			t.Fatal(err)
		}
		config += fmt.Sprintf("\nmanagement:\n  mode: database\n  allowed_origins: [%s]\n  profiles:\n    test-git:\n      allowed_origins: [%s]\n      ca_file: %s\n", origin.Scheme+"://"+origin.Host, origin.Scheme+"://"+origin.Host, caPath)
		env.WriteFile("/workspace/marketplace.yaml", config)
		env.MustRunAM("serve", "adopt", "--config", "marketplace.yaml", "--actor", "test", "--reason", "Reset recovery test", "--idempotency-key", "reset-test", "--confirm-maintenance")
		env.WriteFile("/workspace/marketplace.yaml", strings.ReplaceAll(config, "slug: serve-source", "slug: stale-yaml-name"))
		cfg, err = mpconfig.LoadConfig(env.mustResolvePath("/workspace/marketplace.yaml"))
		if err != nil {
			t.Fatal(err)
		}
	}
	env.MustRunAM("serve", "reindex", "serve-source", "--config", "marketplace.yaml")
	repaired, err := meta.GetLatestResourceRevision(t.Context(), coordinate)
	if err != nil || repaired.ContentDigest != legacy.ContentDigest || repaired.ArchiveDigest == legacy.ArchiveDigest || repaired.Occurrence.Commit != commit {
		t.Fatalf("repair must preserve content/source and replace archive: %+v %v", repaired, err)
	}
	if retained, err := blobs.Get(t.Context(), legacy.ArchiveDigest); err != nil || !bytes.Equal(retained, legacyBytes) {
		t.Fatalf("republication lost old blob: %v", err)
	}
	if err := meta.CheckIntegrity(t.Context(), blobs); err != nil {
		t.Fatal(err)
	}
	lintSkill := "---\nname: lint\ndescription: Run the linter\nlicense: MIT\n---\nRun the linter.\n"
	if err := os.MkdirAll(filepath.Join(source, ".agents", "skills", "lint"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, ".agents", "skills", "lint", "SKILL.md"), []byte(lintSkill), 0o644); err != nil {
		t.Fatal(err)
	}
	env.MustRun(fmt.Sprintf("git -C %q add .agents/skills/lint && git -C %q -c user.name='E2E Test' -c user.email='e2e@test.local' commit -q -m 'add lint skill' && git -C %q push -q origin main", source, source, source))
	commit2 := strings.TrimSpace(env.MustRun(fmt.Sprintf("git -C %q rev-parse HEAD", source)).Stdout)
	env.MustRunAM("serve", "reindex", "serve-source", "--config", "marketplace.yaml")
	lintCoordinate := store.ResourceCoordinate{Marketplace: deps.Marketplace.ID, Registry: registry.ID, Kind: "skill", Name: "lint"}
	lint, err := meta.GetLatestResourceRevision(t.Context(), lintCoordinate)
	if err != nil || lint.Occurrence.Commit != commit2 {
		t.Fatalf("new resource not published by the second reindex: %+v %v", lint, err)
	}
	server := httptest.NewServer(mpapi.NewServer(cfg, meta, blobs).PublicHandler())
	defer server.Close()
	lintResp, err := http.Get(server.URL + "/api/v1/resources/serve-source/skill/lint")
	if err != nil {
		t.Fatal(err)
	}
	lintBody, _ := io.ReadAll(lintResp.Body)
	_ = lintResp.Body.Close()
	if lintResp.StatusCode != http.StatusOK || !strings.Contains(string(lintBody), `"name":"lint"`) {
		t.Fatalf("previously absent resource not served: %d %s", lintResp.StatusCode, lintBody)
	}
	historyResp, err := http.Get(server.URL + "/api/v1/registries/serve-source/history")
	if err != nil {
		t.Fatal(err)
	}
	historyBody, _ := io.ReadAll(historyResp.Body)
	_ = historyResp.Body.Close()
	if historyResp.StatusCode != http.StatusOK || !strings.Contains(string(historyBody), commit) || !strings.Contains(string(historyBody), commit2) {
		t.Fatalf("occurrence history did not retain both commits: %d %s", historyResp.StatusCode, historyBody)
	}
	if _, err := meta.GetRegistryOccurrence(t.Context(), registry.ID, commit); err != nil {
		t.Fatalf("first occurrence lost: %v", err)
	}
	if _, err := meta.GetRegistryOccurrence(t.Context(), registry.ID, commit2); err != nil {
		t.Fatalf("second occurrence missing: %v", err)
	}
	preview := env.RunAM("serve", "reset", "serve-source", "--config", "marketplace.yaml", "--dry-run")
	if preview.ExitCode != 0 || !strings.Contains(preview.Combined, "resource_revisions: 3") {
		t.Fatalf("reset preview: exit %d\n%s", preview.ExitCode, preview.Combined)
	}
	if current, err := meta.GetLatestResourceRevision(t.Context(), coordinate); err != nil || current.ArchiveDigest != repaired.ArchiveDigest {
		t.Fatalf("preview changed published revision: %+v %v", current, err)
	}
	if result := env.RunAM("serve", "reset", "src-serve", "--config", "marketplace.yaml", "--confirm-maintenance"); result.ExitCode != 0 {
		t.Fatalf("reset: exit %d\n%s", result.ExitCode, result.Combined)
	}
	env.MustRunAM("serve", "reindex", "serve-source", "--config", "marketplace.yaml")
	republished, err := meta.GetLatestResourceRevision(t.Context(), coordinate)
	if err != nil || republished.ContentDigest != legacy.ContentDigest || republished.ArchiveDigest != repaired.ArchiveDigest || republished.Occurrence.Commit != commit2 {
		t.Fatalf("republication after reset: %+v %v", republished, err)
	}
	if err := meta.CheckIntegrity(t.Context(), blobs); err != nil {
		t.Fatal(err)
	}
	consumer := newEnv(t)
	consumer.InitGitRepo()
	consumer.WriteGlobalConfig("apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	consumer.WriteOrgConfigWithMode(fmt.Sprintf("apiVersion: agent-marketplace/v4\nsources:\n  org:\n    marketplace: %s\nresources:\n  org:\n    include: [serve-source/collection/team]\n", server.URL), 0o644)
	result := consumer.RunAM("sync")
	if result.ExitCode != 0 {
		t.Fatalf("organization collection sync after repair: exit %d\n%s", result.ExitCode, result.Combined)
	}
	installed := consumer.mustResolvePath("/root/.factory/skills/review/scripts/run.sh")
	data, err := os.ReadFile(installed)
	if err != nil || string(data) != script+"\n" {
		t.Fatalf("installed script: %q, %v", data, err)
	}
	consumer.AssertFileAbsent("/root/.factory/skills/review/artifact.tar.gz")
	consumer.AssertFileAbsent("/root/.factory/skills/review/verified.marker")
}

func stripLegacySupport(t *testing.T, rev *ingest.Revision) {
	t.Helper()
	replacements := make(map[store.ArchiveDigest]store.ArchiveDigest)
	for i := range rev.ExtraBlobs {
		blob := &rev.ExtraBlobs[i]
		canonical, _, err := hccartifact.Decode(bytes.NewReader(blob.Data))
		if err != nil || len(canonical.Support) == 0 {
			continue
		}
		canonical.Support = nil
		raw, digest, err := hccartifact.Encode(canonical)
		if err != nil {
			t.Fatal(err)
		}
		replacements[blob.Digest] = store.ArchiveDigest(digest)
		blob.Digest, blob.Data = store.ArchiveDigest(digest), raw
	}
	if len(replacements) != 1 {
		t.Fatalf("expected one legacy support archive, got %d", len(replacements))
	}
	for i := range rev.Resources {
		if digest, ok := replacements[rev.Resources[i].ArchiveDigest]; ok {
			rev.Resources[i].ArchiveDigest = digest
		}
	}
	var manifest mpartifact.RegistryRevisionManifest
	if err := json.Unmarshal(rev.ManifestPayload, &manifest); err != nil {
		t.Fatal(err)
	}
	for i := range manifest.Resources {
		if digest, ok := replacements[store.ArchiveDigest(manifest.Resources[i].ArchiveDigest)]; ok {
			manifest.Resources[i].ArchiveDigest = string(digest)
		}
	}
	payload, digest, err := mpartifact.CanonicalRegistryRevisionPayload(manifest)
	if err != nil {
		t.Fatal(err)
	}
	rev.ManifestPayload = payload
	rev.ManifestDigest = store.RevisionDigest(digest)
	rev.Occurrence.RegistryRevisionManifestDigest = store.RevisionDigest(digest)
	for i := range rev.ResourceOccurrences {
		rev.ResourceOccurrences[i].ManifestDigest = store.RevisionDigest(digest)
	}
}
