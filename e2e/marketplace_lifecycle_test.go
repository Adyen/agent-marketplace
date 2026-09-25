package e2e_test

import (
	"context"
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

func TestOrganizationMarketplaceCollectionMemberUserOverride(t *testing.T) {
	fixture := newMarketplaceBehaviorFixture(t, marketplaceBehaviorFiles("organization member", true))
	consumer := newEnv(t)
	consumer.InitGitRepo()
	consumer.WriteGlobalConfig("apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	organization := fmt.Sprintf(`apiVersion: agent-marketplace/v4
sources:
  org: {marketplace: %s}
resources:
  org:
    include: [behavior-source/collection/behavior-bundle]
`, fixture.server.URL)
	consumer.WriteOrgConfig(organization)
	result := consumer.RunAM("sync")
	if result.ExitCode != 0 {
		t.Fatalf("optional organization collection sync failed (exit %d):\n%s", result.ExitCode, result.Combined)
	}
	consumer.AssertFileContains("/root/.factory/skills/skill-one/SKILL.md", "organization member")
	consumer.WriteGlobalConfig(fmt.Sprintf(`apiVersion: agent-marketplace/v4
harnesses: [factory]
sources:
  company: {marketplace: %s}
resources:
  company:
    exclude: [behavior-source/skill/skill-one]
`, fixture.server.URL))
	result = consumer.RunAM("sync")
	if result.ExitCode != 0 {
		t.Fatalf("user override sync failed (exit %d):\n%s", result.ExitCode, result.Combined)
	}
	consumer.AssertFileAbsent("/root/.factory/skills/skill-one")
	consumer.AssertFileContains("/root/.factory/commands/command-one.md", "echo behavior")
	consumer.WriteOrgConfig(organization + "    required: true\n")
	result = consumer.RunAM("sync")
	if result.ExitCode != 0 {
		t.Fatalf("required organization collection sync failed (exit %d):\n%s", result.ExitCode, result.Combined)
	}
	consumer.AssertFileContains("/root/.factory/skills/skill-one/SKILL.md", "organization member")
}

func TestMarketplaceInstallFlagsKeepExactInstalledName(t *testing.T) {
	fixture := newMarketplaceBehaviorFixture(t, marketplaceBehaviorFiles("Named install", false))
	for _, tc := range []struct {
		kind, name, alias, output, original, body string
	}{
		{"skill", "skill-one", "review-copy", "/workspace/.factory/skills/review-copy/SKILL.md", "/workspace/.factory/skills/skill-one", "Named install"},
		{"command", "command-one", "invoke-copy", "/workspace/.factory/commands/invoke-copy.md", "/workspace/.factory/commands/command-one.md", "echo behavior"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			consumer := fixture.newConsumer(t, fixture.server.URL)
			result := consumer.RunAM("install", "behavior-source/"+tc.kind+"/"+tc.name, "--marketplace", "behavior", "--as", tc.alias, "--accept-risk", "--verbose", "--scope", "repo")
			if result.ExitCode != 0 {
				t.Fatalf("Marketplace flag install failed (exit %d):\n%s", result.ExitCode, result.Combined)
			}
			if !consumer.CheckFileExists(tc.output) {
				t.Fatalf("aliased installation produced no %s: output=%s config=%s", tc.alias, result.Combined, consumer.ReadFile("/workspace/.agents/config.yaml"))
			}
			consumer.AssertFileContains(tc.output, tc.body)
			if tc.kind == "skill" {
				consumer.AssertFileContains(tc.output, "name: "+tc.alias)
			}
			consumer.AssertFileAbsent(tc.original)
			config := consumer.ReadFile("/workspace/.agents/config.yaml")
			if !strings.Contains(config, "installed_name: "+tc.alias) || !strings.Contains(config, "kind: "+tc.kind) {
				t.Fatalf("exact Marketplace install lost the local alias: %s", config)
			}
			if !strings.Contains(result.Combined, "Marketplace install preview") {
				t.Fatalf("--verbose omitted the verified Marketplace preview: %s", result.Combined)
			}
			consumer.AssertExitCode(consumer.RunAM("sync", "--check"), 0)
			repeat := consumer.RunAM("install", "behavior-source/"+tc.kind+"/"+tc.name, "--marketplace", "behavior", "--as", tc.alias, "--yes", "--scope", "repo")
			if repeat.ExitCode != 0 || consumer.ReadFile("/workspace/.agents/config.yaml") != config {
				t.Fatalf("repeat aliased install changed selection: exit=%d output=%s", repeat.ExitCode, repeat.Combined)
			}
			consumer.AssertExitCode(consumer.RunAM("sync", "--offline"), 0)
			consumer.AssertFileContains(tc.output, tc.body)
			removed := consumer.RunAM("remove", tc.alias, "--kind", tc.kind, "--scope", "repo")
			if removed.ExitCode != 0 {
				t.Fatalf("remove by installed alias failed (exit %d):\n%s", removed.ExitCode, removed.Combined)
			}
			consumer.AssertFileAbsent(tc.output)
			if config := consumer.ReadFile("/workspace/.agents/config.yaml"); strings.Contains(config, "installed_name: "+tc.alias) {
				t.Fatalf("remove by installed alias retained the pin: %s", config)
			}
			consumer.AssertExitCode(consumer.RunAM("sync", "--check"), 0)
		})
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

// The organization config declares its Marketplace under an alias and
// selects a collection without deploying a lock, and init creates a v4 user
// config. A user installs a hosted MCP server through that alias into the
// user config (v4.0.8 failed with v4.writer-opt-in).
func TestOrganizationMarketplaceAliasInstallsMCPServerIntoV4UserConfig(t *testing.T) {
	files := marketplaceBehaviorFiles("Behavior body", true)
	files[".mcp.json"] = MergeMCPFile([]MCPServer{{Name: "sonarqube", Command: "echo", Args: []string{"sonarqube"}}})
	fixture := newMarketplaceBehaviorFixture(t, files)
	consumer := newEnv(t)
	consumer.InitGitRepo()
	consumer.MustRunAM("init", "--harnesses", "factory")
	consumer.WriteOrgConfigWithMode(fmt.Sprintf(`apiVersion: agent-marketplace/v4
sources:
  org:
    marketplace: %s
allowed_marketplaces:
  - '^http://127\.0\.0\.1:[0-9]+$'
resources:
  org:
    include:
      - behavior-source/collection/behavior-bundle
`, fixture.server.URL), 0o644)

	result := consumer.RunAM("install", "org:behavior-source/mcpserver/sonarqube", "--scope", "user", "--yes")
	if result.ExitCode != 0 {
		t.Fatalf("install through the organization Marketplace alias into v4 user config failed (exit %d):\n%s", result.ExitCode, result.Combined)
	}
	userConfig := consumer.ReadFile("/root/.agents/config.yaml")
	for _, want := range []string{"apiVersion: agent-marketplace/v4", "marketplace: " + fixture.server.URL, "behavior-source/mcp/sonarqube"} {
		if !strings.Contains(userConfig, want) {
			t.Fatalf("v4 user config missing %q:\n%s", want, userConfig)
		}
	}
	if strings.Contains(userConfig, "marketplaces:") {
		t.Fatalf("v4 user config gained the legacy marketplaces key:\n%s", userConfig)
	}
	consumer.AssertFileContains("/root/.agents/lock.yaml", "sonarqube")
	consumer.AssertFileContains("/root/.factory/mcp.json", "sonarqube")
	consumer.AssertFileAbsent("/etc/agent-marketplace/lock.yaml")
	consumer.AssertExitCode(consumer.RunAM("sync"), 0)
	consumer.AssertExitCode(consumer.RunAM("sync", "--check"), 0)
}

// With the deployed organization shape and a v4 user config from init, a
// hosted MCP server installed into a repository without a config lives in a
// new v4 repository root and lock, survives update, and removes cleanly while
// the organization lock stays absent.
func TestOrganizationMarketplaceAliasRepositoryLifecycle(t *testing.T) {
	files := marketplaceBehaviorFiles("Behavior body", true)
	files[".mcp.json"] = MergeMCPFile([]MCPServer{{Name: "sonarqube", Command: "echo", Args: []string{"sonarqube"}}})
	fixture := newMarketplaceBehaviorFixture(t, files)
	consumer := newEnv(t)
	consumer.InitGitRepo()
	consumer.MustRunAM("init", "--harnesses", "factory")
	consumer.WriteOrgConfigWithMode(fmt.Sprintf(`apiVersion: agent-marketplace/v4
sources:
  org:
    marketplace: %s
allowed_marketplaces:
  - '^http://127\.0\.0\.1:[0-9]+$'
resources:
  org:
    include:
      - behavior-source/collection/behavior-bundle
`, fixture.server.URL), 0o644)

	result := consumer.RunAM("install", "org:behavior-source/mcpserver/sonarqube", "--scope", "repo", "--yes")
	if result.ExitCode != 0 {
		t.Fatalf("install through the organization alias into repo scope failed (exit %d):\n%s", result.ExitCode, result.Combined)
	}
	repoConfig := consumer.ReadFile("/workspace/.agents/config.yaml")
	for _, want := range []string{"apiVersion: agent-marketplace/v4", "marketplace: " + fixture.server.URL, "behavior-source/mcp/sonarqube"} {
		if !strings.Contains(repoConfig, want) {
			t.Fatalf("new repository config missing %q:\n%s", want, repoConfig)
		}
	}
	consumer.AssertFileContains("/workspace/.agents/lock.yaml", "sonarqube")
	consumer.AssertFileContains("/workspace/.factory/mcp.json", "sonarqube")
	consumer.AssertFileContains("/root/.factory/skills/skill-one/SKILL.md", "Behavior body")
	consumer.AssertExitCode(consumer.RunAM("sync", "--check"), 0)

	for _, args := range [][]string{{"update"}, {"sync", "--check"}, {"status"}, {"doctor"}} {
		consumer.AssertExitCode(consumer.RunAM(args...), 0)
	}

	result = consumer.RunAM("remove", "sonarqube", "--scope", "repo")
	if result.ExitCode != 0 {
		t.Fatalf("remove from the v4 repository config failed (exit %d):\n%s", result.ExitCode, result.Combined)
	}
	if data, err := os.ReadFile(consumer.mustResolvePath("/workspace/.factory/mcp.json")); err == nil && strings.Contains(string(data), "sonarqube") {
		t.Fatalf("removed MCP server is still materialized:\n%s", data)
	}
	consumer.AssertFileContains("/root/.factory/skills/skill-one/SKILL.md", "Behavior body")
	for _, args := range [][]string{{"gc"}, {"sync", "--check"}} {
		consumer.AssertExitCode(consumer.RunAM(args...), 0)
	}
	consumer.AssertFileAbsent("/etc/agent-marketplace/lock.yaml")
}
