package e2e_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/analyze"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	mpconfig "github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/worker"
)

type marketplaceBehaviorFixture struct {
	env       *Env
	bareURL   string
	cfg       mpconfig.Config
	meta      store.MetadataStore
	blobs     store.BlobStore
	handlerMu sync.RWMutex
	handler   http.Handler
	server    *httptest.Server
}

func newMarketplaceBehaviorFixture(t *testing.T, files map[string]string) *marketplaceBehaviorFixture {
	t.Helper()
	env := newEnv(t)
	env.InitGitRepo()
	bareURL := env.CreateBareRepo("marketplace-behavior", files)
	gitServer := newGitHTTPServer(t, env.repos)
	t.Cleanup(gitServer.Close)
	origin := gitServer.URL + "/marketplace-behavior.git"
	dbPath := filepath.Join(env.workspace, "marketplace.db")
	blobsPath := filepath.Join(env.workspace, "blobs")
	env.WriteFile("/workspace/marketplace.yaml", fmt.Sprintf(`apiVersion: agent-marketplace.io/marketplace/v1
instance:
  id: behavior-marketplace
  name: Behavior Marketplace
  canonical_url: https://example.invalid
storage:
  metadata:
    driver: sqlite
    dsn: %s
  blobs:
    driver: filesystem
    path: %s
acquisition_default: mirror
refresh:
  interval: 1h
policy:
  hidden_content: warn
  license_allowlist: [MIT]
  limits:
    repository_bytes: 104857600
    file_count: 1024
    file_bytes: 2097152
    artifact_bytes: 16777216
    clone_duration: 300s
    ingestion_duration: 120s
registries:
  - id: behavior-source-id
    slug: behavior-source
    origin: %s
    ref: main
    acquisition: mirror
    license: MIT
    roots:
      - path: .
        source_harness: claude
revocations:
  registry_ids: []
  coordinates: []
  revisions: []
`, dbPath, blobsPath, origin))
	result := env.RunAM("serve", "reindex", "--config", "marketplace.yaml")
	if result.ExitCode != 0 {
		t.Fatalf("initial Marketplace reindex failed (exit %d):\n%s", result.ExitCode, result.Combined)
	}
	cfg, meta, blobs := openPublicGateStores(t, env.mustResolvePath("/workspace/marketplace.yaml"))
	handler := mpapi.NewServer(cfg, meta, blobs).PublicHandler()
	fixture := &marketplaceBehaviorFixture{env: env, bareURL: bareURL, cfg: cfg, meta: meta, blobs: blobs, handler: handler}
	server := httptest.NewServer(http.HandlerFunc(fixture.serveHTTP))
	fixture.server = server
	t.Cleanup(func() {
		server.Close()
		_ = meta.Close()
	})
	return fixture
}

func (f *marketplaceBehaviorFixture) serveHTTP(w http.ResponseWriter, r *http.Request) {
	f.handlerMu.RLock()
	defer f.handlerMu.RUnlock()
	f.handler.ServeHTTP(w, r)
}

func (f *marketplaceBehaviorFixture) setHandler(handler http.Handler) {
	f.handlerMu.Lock()
	defer f.handlerMu.Unlock()
	f.handler = handler
}

func marketplaceBehaviorFiles(body string, collection bool) map[string]string {
	files := RegistryFilesEx(
		[]Skill{{Name: "skill-one", Description: "Behavior skill"}},
		[]Command{{Name: "command-one", Description: "Behavior command", Command: "echo behavior"}},
		nil,
		nil,
	)
	files["skills/skill-one/SKILL.md"] = "---\nname: skill-one\ndescription: Behavior skill\nlicense: MIT\n---\n" + body + "\n"
	if collection {
		files["collections/behavior-bundle/COLLECTION.yaml"] = `name: behavior-bundle
version: "1.0"
description: Behavior collection
resources:
  - name: skill-one
    kind: skill
  - name: command-one
    kind: command
`
	}
	return files
}

func (f *marketplaceBehaviorFixture) newConsumer(t *testing.T, endpoint string) *Env {
	t.Helper()
	consumer := newEnv(t)
	consumer.InitGitRepo()
	consumer.WriteGlobalConfig("apiVersion: agent-marketplace/v2\nharnesses: [factory]\nregistries: []")
	consumer.WriteRepoConfig(fmt.Sprintf("apiVersion: agent-marketplace/v2\nmarketplaces:\n  - url: %s\n    instance_id: behavior-marketplace\n    alias: behavior", endpoint))
	return consumer
}

func (f *marketplaceBehaviorFixture) resourceDigest(t *testing.T, kind, name string) string {
	t.Helper()
	response := getPublicGateResponse(t, f.server.URL+"/api/v1/resources/behavior-source/"+kind+"/"+name)
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("resource %s/%s status = %d", kind, name, response.StatusCode)
	}
	var revision struct {
		ContentDigest string `json:"content_digest"`
	}
	if err := json.NewDecoder(response.Body).Decode(&revision); err != nil {
		t.Fatal(err)
	}
	return revision.ContentDigest
}

func (f *marketplaceBehaviorFixture) mutateSource(t *testing.T, path, content string) {
	t.Helper()
	work := t.TempDir()
	gitRun(t, "", "clone", "-b", "main", strings.TrimPrefix(f.bareURL, "file://"), work)
	fullPath := filepath.Join(work, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, work, "config", "user.email", "e2e@example.com")
	gitRun(t, work, "config", "user.name", "E2E")
	gitRun(t, work, "add", ".")
	gitRun(t, work, "commit", "-m", "update source")
	gitRun(t, work, "push", "origin", "main")
}

func (f *marketplaceBehaviorFixture) reindex(t *testing.T, cfg mpconfig.Config) {
	t.Helper()
	if err := worker.Reindex(context.Background(), worker.Deps{Config: cfg, MetadataStore: f.meta, BlobStore: f.blobs}, cfg, "behavior-source"); err != nil {
		t.Fatalf("Marketplace reindex: %v", err)
	}
}

func TestMarketplacePinRemainsExactUntilUpdateAndWorksOffline(t *testing.T) {
	fixture := newMarketplaceBehaviorFixture(t, marketplaceBehaviorFiles("version one", false))
	consumer := fixture.newConsumer(t, fixture.server.URL)
	oldDigest := fixture.resourceDigest(t, "skill", "skill-one")
	result := consumer.RunAM("install", "behavior:behavior-source/skill/skill-one", "--yes")
	if result.ExitCode != 0 {
		t.Fatalf("install revision one failed (exit %d):\n%s", result.ExitCode, result.Combined)
	}
	consumer.AssertFileContains("/workspace/.factory/skills/skill-one/SKILL.md", "version one")

	fixture.mutateSource(t, "skills/skill-one/SKILL.md", "---\nname: skill-one\ndescription: Behavior skill\nlicense: MIT\n---\nversion two\n")
	fixture.reindex(t, fixture.cfg)
	newDigest := fixture.resourceDigest(t, "skill", "skill-one")
	if newDigest == oldDigest {
		t.Fatal("source update did not publish a new exact revision")
	}
	result = consumer.RunAM("sync")
	if result.ExitCode != 0 {
		t.Fatalf("exact-pin sync failed (exit %d):\n%s", result.ExitCode, result.Combined)
	}
	consumer.AssertFileContains("/workspace/.factory/skills/skill-one/SKILL.md", "version one")
	if config := consumer.ReadFile("/workspace/.agents/config.yaml"); !strings.Contains(config, oldDigest) || strings.Contains(config, newDigest) {
		t.Fatalf("ordinary sync moved the exact pin:\n%s", config)
	}

	result = consumer.RunAM("update")
	if result.ExitCode != 0 {
		t.Fatalf("Marketplace update failed (exit %d):\n%s", result.ExitCode, result.Combined)
	}
	consumer.AssertFileContains("/workspace/.factory/skills/skill-one/SKILL.md", "version two")
	if config := consumer.ReadFile("/workspace/.agents/config.yaml"); strings.Contains(config, oldDigest) || !strings.Contains(config, newDigest) {
		t.Fatalf("update did not replace the exact pin:\n%s", config)
	}
	fixture.server.Close()
	result = consumer.RunAM("sync", "--offline")
	if result.ExitCode != 0 {
		t.Fatalf("updated pin failed offline (exit %d):\n%s", result.ExitCode, result.Combined)
	}
	consumer.AssertFileContains("/workspace/.factory/skills/skill-one/SKILL.md", "version two")
}

func TestMarketplaceInstallArtifactFailureIsTransactional(t *testing.T) {
	fixture := newMarketplaceBehaviorFixture(t, marketplaceBehaviorFiles("transactional", false))
	var artifactRequests atomic.Int32
	var failArtifact atomic.Bool
	failArtifact.Store(true)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/v1/artifacts/") {
			artifactRequests.Add(1)
			if failArtifact.Load() {
				http.Error(w, "artifact unavailable", http.StatusBadGateway)
				return
			}
		}
		fixture.serveHTTP(w, r)
	}))
	defer proxy.Close()
	consumer := fixture.newConsumer(t, proxy.URL)
	before := consumer.ReadFile("/workspace/.agents/config.yaml")

	result := consumer.RunAM("install", "behavior:behavior-source/skill/skill-one", "--yes")
	if result.ExitCode == 0 || artifactRequests.Load() != 1 {
		t.Fatalf("failed artifact install = exit %d, requests %d:\n%s", result.ExitCode, artifactRequests.Load(), result.Combined)
	}
	if after := consumer.ReadFile("/workspace/.agents/config.yaml"); after != before {
		t.Fatalf("failed install mutated config:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	consumer.AssertFileAbsent("/workspace/.factory/skills/skill-one")
	failArtifact.Store(false)
	result = consumer.RunAM("install", "behavior:behavior-source/skill/skill-one", "--yes")
	if result.ExitCode != 0 {
		t.Fatalf("install did not recover after artifact became available (exit %d):\n%s", result.ExitCode, result.Combined)
	}
	consumer.AssertFileContains("/workspace/.factory/skills/skill-one/SKILL.md", "transactional")
}

func TestMarketplaceIdentityAndAllowlistFailClosed(t *testing.T) {
	fixture := newMarketplaceBehaviorFixture(t, marketplaceBehaviorFiles("identity", false))

	t.Run("instance mismatch", func(t *testing.T) {
		consumer := newEnv(t)
		consumer.InitGitRepo()
		consumer.WriteGlobalConfig("apiVersion: agent-marketplace/v2\nharnesses: [factory]\nregistries: []")
		consumer.WriteRepoConfig(fmt.Sprintf("apiVersion: agent-marketplace/v2\nmarketplaces:\n  - url: %s\n    instance_id: wrong-id\n    alias: behavior", fixture.server.URL))
		before := consumer.ReadFile("/workspace/.agents/config.yaml")
		result := consumer.RunAM("install", "behavior:behavior-source/skill/skill-one", "--yes")
		if result.ExitCode == 0 || !strings.Contains(result.Combined, "ID mismatch") {
			t.Fatalf("instance mismatch result = exit %d:\n%s", result.ExitCode, result.Combined)
		}
		if consumer.ReadFile("/workspace/.agents/config.yaml") != before {
			t.Fatal("instance mismatch mutated config")
		}
	})

	t.Run("blocked endpoint", func(t *testing.T) {
		var requests atomic.Int32
		proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requests.Add(1)
			fixture.serveHTTP(w, r)
		}))
		defer proxy.Close()
		consumer := newEnv(t)
		consumer.InitGitRepo()
		consumer.WriteGlobalConfig(fmt.Sprintf("apiVersion: agent-marketplace/v2\nharnesses: [factory]\nregistries: []\nallowed_marketplaces: ['^https://allowed\\.invalid$']\nmarketplaces:\n  - url: %s\n    instance_id: behavior-marketplace\n    alias: behavior", proxy.URL))
		result := consumer.RunAM("search", "--marketplace", "behavior")
		if result.ExitCode == 0 || !strings.Contains(result.Combined, "blocked") || requests.Load() != 0 {
			t.Fatalf("blocked endpoint result = exit %d, requests %d:\n%s", result.ExitCode, requests.Load(), result.Combined)
		}
		consumer.WriteGlobalConfig(fmt.Sprintf("apiVersion: agent-marketplace/v2\nharnesses: [factory]\nregistries: []\nallowed_marketplaces: ['^http://127\\.0\\.0\\.1:[0-9]+$']\nmarketplaces:\n  - url: %s\n    instance_id: behavior-marketplace\n    alias: behavior", proxy.URL))
		result = consumer.RunAM("search", "--marketplace", "behavior")
		if result.ExitCode != 0 || requests.Load() == 0 {
			t.Fatalf("allowed endpoint result = exit %d, requests %d:\n%s", result.ExitCode, requests.Load(), result.Combined)
		}
	})
}

func TestMarketplaceAssessmentChangesWithoutChangingRevision(t *testing.T) {
	fixture := newMarketplaceBehaviorFixture(t, marketplaceBehaviorFiles("assessment", false))
	digest := fixture.resourceDigest(t, "skill", "skill-one")
	cfg := fixture.cfg
	cfg.Policy.LicenseAllowlist = []string{"Apache-2.0"}
	fixture.reindex(t, cfg)
	if got := fixture.resourceDigest(t, "skill", "skill-one"); got != digest {
		t.Fatalf("policy-only reindex changed resource revision from %s to %s", digest, got)
	}
	body := getPublicGate(t, fixture.server.URL+"/api/v1/resources/behavior-source/skill/skill-one/revisions/"+digest)
	if !strings.Contains(body, `"decision":"reject"`) || !strings.Contains(body, `"content_digest":"`+digest+`"`) {
		t.Fatalf("exact revision did not expose the replacement assessment:\n%s", body)
	}
	consumer := fixture.newConsumer(t, fixture.server.URL)
	before := consumer.ReadFile("/workspace/.agents/config.yaml")
	result := consumer.RunAM("install", "behavior:behavior-source/skill/skill-one", "--yes")
	if result.ExitCode == 0 || !strings.Contains(result.Combined, "rejected") {
		t.Fatalf("rejected reassessment install = exit %d:\n%s", result.ExitCode, result.Combined)
	}
	if consumer.ReadFile("/workspace/.agents/config.yaml") != before {
		t.Fatal("rejected reassessment mutated config")
	}
}

func TestMarketplaceRevocationPreservesInstalledOutputAndSupportsExactOverride(t *testing.T) {
	fixture := newMarketplaceBehaviorFixture(t, marketplaceBehaviorFiles("revocable", false))
	consumer := fixture.newConsumer(t, fixture.server.URL)
	digest := fixture.resourceDigest(t, "skill", "skill-one")
	result := consumer.RunAM("install", "behavior:behavior-source/skill/skill-one", "--yes")
	if result.ExitCode != 0 {
		t.Fatalf("install before revocation failed (exit %d):\n%s", result.ExitCode, result.Combined)
	}
	cfg := fixture.cfg
	cfg.Revocations.RegistryIDs = []string{"behavior-source-id"}
	if err := worker.EnsureRevocations(context.Background(), worker.Deps{Config: cfg, MetadataStore: fixture.meta, BlobStore: fixture.blobs}); err != nil {
		t.Fatalf("reconcile revocation: %v", err)
	}
	fixture.setHandler(mpapi.NewServer(cfg, fixture.meta, fixture.blobs).PublicHandler())

	result = consumer.RunAM("sync")
	if result.ExitCode == 0 || !strings.Contains(result.Combined, "revoked") {
		t.Fatalf("revoked sync = exit %d:\n%s", result.ExitCode, result.Combined)
	}
	consumer.AssertFileContains("/workspace/.factory/skills/skill-one/SKILL.md", "revocable")
	result = consumer.RunAM("sync", "--offline")
	if result.ExitCode == 0 || !strings.Contains(result.Combined, "revoked") {
		t.Fatalf("cached revocation was not enforced offline (exit %d):\n%s", result.ExitCode, result.Combined)
	}
	coordinate := "behavior-marketplace/behavior-source-id/skill/skill-one@" + digest
	result = consumer.RunAM("sync", "--allow-revoked", coordinate)
	if result.ExitCode != 0 {
		t.Fatalf("exact revocation override failed (exit %d):\n%s", result.ExitCode, result.Combined)
	}
	result = consumer.RunAM("sync", "--check", "--allow-revoked", coordinate)
	if result.ExitCode == 0 {
		t.Fatal("sync --check accepted --allow-revoked")
	}
}

func TestMarketplaceCollectionSelectorLifecycle(t *testing.T) {
	fixture := newMarketplaceBehaviorFixture(t, marketplaceBehaviorFiles("collection member", true))
	consumer := fixture.newConsumer(t, fixture.server.URL)
	result := consumer.RunAM("install", "behavior:behavior-source/collection/behavior-bundle", "--yes")
	if result.ExitCode != 0 {
		t.Fatalf("collection install failed (exit %d):\n%s", result.ExitCode, result.Combined)
	}
	consumer.AssertFileContains("/workspace/.factory/skills/skill-one/SKILL.md", "collection member")
	consumer.AssertFileExists("/workspace/.factory/commands/command-one.md")
	if config := consumer.ReadFile("/workspace/.agents/config.yaml"); !strings.Contains(config, "selected_by:") || !strings.Contains(config, "name: behavior-bundle") {
		t.Fatalf("collection install did not persist selector edges:\n%s", config)
	}

	result = consumer.RunAM("install", "behavior:behavior-source/skill/skill-one", "--yes")
	if result.ExitCode != 0 {
		t.Fatalf("direct member install failed (exit %d):\n%s", result.ExitCode, result.Combined)
	}
	result = consumer.RunAM("remove", "skill-one", "--kind", "skill")
	if result.ExitCode != 0 {
		t.Fatalf("direct selector removal failed (exit %d):\n%s", result.ExitCode, result.Combined)
	}
	consumer.AssertRepoFactorySkillExists("skill-one")
	if config := consumer.ReadFile("/workspace/.agents/config.yaml"); strings.Contains(config, "direct: true") || !strings.Contains(config, "selected_by:") {
		t.Fatalf("direct removal did not preserve only the collection selector:\n%s", config)
	}

	result = consumer.RunAM("remove", "behavior-bundle", "--kind", "collection")
	if result.ExitCode != 0 {
		t.Fatalf("collection removal failed (exit %d):\n%s", result.ExitCode, result.Combined)
	}
	consumer.AssertRepoFactorySkillAbsent("skill-one")
	consumer.AssertFileAbsent("/workspace/.factory/commands/command-one.md")
}

func newMarketplaceAnalyzerFixture(t *testing.T, files map[string]string, analyzers []mpconfig.Analyzer) *marketplaceBehaviorFixture {
	t.Helper()
	fixture := newMarketplaceBehaviorFixture(t, files)
	fixture.cfg.Policy.Analyzers = analyzers
	fixture.reindex(t, fixture.cfg)
	fixture.setHandler(mpapi.NewServer(fixture.cfg, fixture.meta, fixture.blobs).PublicHandler())
	return fixture
}

func TestMarketplaceAnalyzerSingleBuiltin(t *testing.T) {
	enabled := true
	fixture := newMarketplaceAnalyzerFixture(t, marketplaceBehaviorFiles("<!-- hidden -->", false), []mpconfig.Analyzer{
		{ID: analyze.AnalyzerHiddenContent, Enabled: &enabled},
	})

	digest := fixture.resourceDigest(t, "skill", "skill-one")
	body := getPublicGate(t, fixture.server.URL+"/api/v1/resources/behavior-source/skill/skill-one/revisions/"+digest)
	var rev struct {
		Assessment struct {
			Decision string `json:"decision"`
		} `json:"assessment"`
		Audits []struct {
			AnalyzerID   string `json:"analyzer_id"`
			ResultDigest string `json:"result_digest"`
		} `json:"audits"`
	}
	if err := json.Unmarshal([]byte(body), &rev); err != nil {
		t.Fatalf("decode revision: %v", err)
	}
	if rev.Assessment.Decision != string(store.AssessmentDecisionWarn) {
		t.Fatalf("decision = %q, want %q", rev.Assessment.Decision, store.AssessmentDecisionWarn)
	}
	if len(rev.Audits) != 1 || rev.Audits[0].AnalyzerID != analyze.AnalyzerHiddenContent {
		t.Fatalf("audits = %+v, want one %q audit", rev.Audits, analyze.AnalyzerHiddenContent)
	}
	if rev.Audits[0].ResultDigest == "" {
		t.Fatalf("audit summary missing result_digest")
	}
	firstResultDigest := rev.Audits[0].ResultDigest

	detail := getPublicGate(t, fixture.server.URL+"/api/v1/resources/behavior-source/skill/skill-one/revisions/"+digest+"/audits/"+analyze.AnalyzerHiddenContent)
	if !strings.Contains(detail, `"verdict":"warn"`) {
		t.Fatalf("audit detail did not warn: %s", detail)
	}
	if !strings.Contains(detail, analyze.FindingHTMLComment) {
		t.Fatalf("audit detail missing HTML comment finding: %s", detail)
	}

	fixture.reindex(t, fixture.cfg)
	body2 := getPublicGate(t, fixture.server.URL+"/api/v1/resources/behavior-source/skill/skill-one/revisions/"+digest)
	var rev2 struct {
		Assessment struct {
			Decision string `json:"decision"`
		} `json:"assessment"`
		Audits []struct {
			AnalyzerID   string `json:"analyzer_id"`
			ResultDigest string `json:"result_digest"`
		} `json:"audits"`
	}
	if err := json.Unmarshal([]byte(body2), &rev2); err != nil {
		t.Fatalf("decode reindexed revision: %v", err)
	}
	if rev2.Assessment.Decision != string(store.AssessmentDecisionWarn) {
		t.Fatalf("reindexed decision = %q, want %q", rev2.Assessment.Decision, store.AssessmentDecisionWarn)
	}
	if len(rev2.Audits) != 1 || rev2.Audits[0].AnalyzerID != analyze.AnalyzerHiddenContent {
		t.Fatalf("reindexed audits = %+v, want one %q audit", rev2.Audits, analyze.AnalyzerHiddenContent)
	}
	if rev2.Audits[0].ResultDigest != firstResultDigest {
		t.Fatalf("reindexed result_digest changed: %q != %q", rev2.Audits[0].ResultDigest, firstResultDigest)
	}

	detail2 := getPublicGate(t, fixture.server.URL+"/api/v1/resources/behavior-source/skill/skill-one/revisions/"+digest+"/audits/"+analyze.AnalyzerHiddenContent)
	if detail2 != detail {
		t.Fatalf("reindexed audit detail changed:\nfirst:\n%s\nsecond:\n%s", detail, detail2)
	}
}

func TestMarketplaceAnalyzerRunnerRedactsStderrSecret(t *testing.T) {
	secret := "super-secret-runner-token-xyz"
	t.Setenv("RUNNER_SECRET", secret)

	runnerPath := filepath.Join(t.TempDir(), "runner.sh")
	script := "#!/bin/sh\necho \"$RUNNER_SECRET\" >&2\nexit 1\n"
	if err := os.WriteFile(runnerPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write runner: %v", err)
	}
	sum := sha256.Sum256([]byte(script))
	digest := "sha256:" + hex.EncodeToString(sum[:])

	enabled := true
	fixture := newMarketplaceAnalyzerFixture(t, marketplaceBehaviorFiles("runner test", false), []mpconfig.Analyzer{
		{
			ID:      "e2e.runner",
			Version: "1.0.0",
			Role:    analyze.RoleGating,
			Enabled: &enabled,
			Run: &mpconfig.AnalyzerRun{
				Command:              runnerPath,
				Timeout:              mpconfig.Duration(30 * time.Second),
				ImplementationDigest: digest,
				EnvPassthrough:       []string{"RUNNER_SECRET"},
			},
		},
	})

	digest = fixture.resourceDigest(t, "skill", "skill-one")
	detail := getPublicGate(t, fixture.server.URL+"/api/v1/resources/behavior-source/skill/skill-one/revisions/"+digest+"/audits/e2e.runner")
	if strings.Contains(detail, secret) {
		t.Fatalf("audit detail leaked runner secret: %s", detail)
	}
	if !strings.Contains(detail, "[REDACTED]") {
		t.Fatalf("audit detail did not redact secret: %s", detail)
	}
	if !strings.Contains(detail, `"verdict":"error"`) {
		t.Fatalf("audit detail did not report error verdict: %s", detail)
	}
	if !strings.Contains(detail, "marketplace.policy.audit-unavailable") {
		t.Fatalf("audit detail missing audit-unavailable finding: %s", detail)
	}
	if !strings.Contains(detail, `"consentable":false`) {
		t.Fatalf("audit detail missing non-consentable audit-unavailable finding: %s", detail)
	}

	consumer := fixture.newConsumer(t, fixture.server.URL)
	beforeConfig := consumer.ReadFile("/workspace/.agents/config.yaml")
	consumer.AssertFileAbsent("/workspace/.factory/skills/skill-one")

	for _, flag := range []string{"--accept-risk", "--yes"} {
		result := consumer.RunAM("install", "behavior:behavior-source/skill/skill-one", flag)
		if result.ExitCode == 0 {
			t.Fatalf("install with %s succeeded (exit %d):\n%s", flag, result.ExitCode, result.Combined)
		}
		if strings.Contains(result.Combined, secret) {
			t.Fatalf("install with %s leaked runner secret", flag)
		}
		if !strings.Contains(result.Combined, "marketplace.policy.audit-unavailable") ||
			!strings.Contains(result.Combined, "non-consentable") ||
			!strings.Contains(result.Combined, "cannot bypass") {
			t.Fatalf("install with %s missing refusal messaging (exit %d):\n%s", flag, result.ExitCode, result.Combined)
		}
		if after := consumer.ReadFile("/workspace/.agents/config.yaml"); after != beforeConfig {
			t.Fatalf("install with %s mutated config (exit %d):\nbefore:\n%s\nafter:\n%s", flag, result.ExitCode, beforeConfig, after)
		}
		consumer.AssertFileAbsent("/workspace/.factory/skills/skill-one")
	}
}
