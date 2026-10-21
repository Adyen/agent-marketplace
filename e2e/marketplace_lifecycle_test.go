package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/worker"
	"gopkg.in/yaml.v3"
)

type marketplaceBehaviorFixture struct {
	*marketplaceTarget
}

func newMarketplaceBehaviorFixture(t *testing.T, files map[string]string) *marketplaceBehaviorFixture {
	t.Helper()
	env := newEnv(t)
	env.InitGitRepo()
	gitServer := newGitHTTPServer(t, env.repos)
	t.Cleanup(gitServer.Close)
	return &marketplaceBehaviorFixture{marketplaceTarget: newMarketplaceTarget(
		t,
		env,
		"marketplace-behavior",
		RegistryDef{Files: files},
		gitServer.URL,
		"behavior-marketplace",
		"behavior-source",
		t.TempDir(),
	)}
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
	consumer.WriteGlobalConfig("apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	consumer.WriteRepoConfig(fmt.Sprintf("apiVersion: agent-marketplace/v4\nsources: {behavior: {marketplace: %q}}\n", endpoint))
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

func (m *marketplaceTarget) mutateSource(t *testing.T, path, content string) {
	t.Helper()
	pushRegistryFiles(t, m.bareURL, map[string]string{path: content})
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

// The direct-install oracle reads the owning lock's wire records rather than
// using the production lock decoder or deriving expectations from that lock.
func assertBehaviorMarketplaceDirectLock(t *testing.T, consumer *Env, lockPath, scope, endpoint, kind, name string, evidence publicGateAdvertisedRevision) {
	t.Helper()
	var lock marketplaceAcquisitionLockWire
	raw := consumer.ReadFile(lockPath)
	if err := yaml.Unmarshal([]byte(raw), &lock); err != nil {
		t.Fatalf("decode %s: %v\n%s", lockPath, err, raw)
	}
	if lock.APIVersion != "agent-marketplace-lock/v2" || lock.Scope != scope || len(lock.Registries) != 0 || len(lock.Marketplaces) != 1 {
		t.Fatalf("unexpected direct acquisition-lock version/scope/sources in %s: %#v", lockPath, lock)
	}
	marketplace := lock.Marketplaces[0]
	if marketplace.URL != endpoint || marketplace.InstanceID != "behavior-marketplace" || len(marketplace.RegistryBindings) != 1 || len(marketplace.Resources) != 1 {
		t.Fatalf("unexpected direct acquisition-lock endpoint/identity/records in %s: %#v", lockPath, marketplace)
	}
	binding := marketplace.RegistryBindings[0]
	if binding.Name != "behavior-source" || binding.RegistryID != "behavior-source-id" {
		t.Fatalf("unexpected direct acquisition-lock binding in %s: %#v", lockPath, binding)
	}
	want := publicGateLockedResource{
		RegistryName: "behavior-source", RegistryID: "behavior-source-id",
		Kind: kind, Name: name, Revision: evidence.ContentDigest,
		ContentDigest: evidence.ContentDigest, ArchiveDigest: evidence.ArchiveDigest,
		Acquisition: "mirror",
	}
	if got := marketplace.Resources[0]; !reflect.DeepEqual(got, want) {
		t.Fatalf("direct acquisition record in %s = %#v, want %#v", lockPath, got, want)
	}
}

func assertBehaviorMarketplaceLockHasNoResources(t *testing.T, consumer *Env, lockPath, scope string) {
	t.Helper()
	raw, err := os.ReadFile(consumer.mustResolvePath(lockPath))
	if os.IsNotExist(err) {
		return // An empty owning lock may be removed after deselection.
	}
	if err != nil {
		t.Fatalf("read deselected lock %s: %v", lockPath, err)
	}
	var lock marketplaceAcquisitionLockWire
	if err := yaml.Unmarshal(raw, &lock); err != nil {
		t.Fatalf("decode deselected lock %s: %v", lockPath, err)
	}
	if lock.APIVersion != "agent-marketplace-lock/v2" || lock.Scope != scope || len(lock.Registries) != 0 {
		t.Fatalf("unexpected deselected acquisition-lock version/scope/sources in %s: %#v", lockPath, lock)
	}
	for _, marketplace := range lock.Marketplaces {
		if len(marketplace.Resources) != 0 {
			t.Fatalf("deselection retained acquisition records in %s: %#v", lockPath, marketplace.Resources)
		}
	}
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
			evidence := assertMarketplaceArtifactIntegrity(t, fixture.server.URL, "behavior-marketplace", "behavior-source", "behavior-source-id", tc.kind, tc.name)
			result := consumer.RunAM("install", "behavior-source/"+tc.kind+"/"+tc.name, "--marketplace", "behavior", "--as", tc.alias, "--verbose", "--scope", "repo")
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
			if !strings.Contains(config, "as: "+tc.alias) || !strings.Contains(config, "behavior-source/"+tc.kind+"/"+tc.name) {
				t.Fatalf("exact Marketplace install lost the local alias: %s", config)
			}
			assertBehaviorMarketplaceDirectLock(t, consumer, "/workspace/.agents/lock.yaml", "repo", fixture.server.URL, tc.kind, tc.name, evidence)
			locked := consumer.ReadFile("/workspace/.agents/lock.yaml")
			if !strings.Contains(result.Combined, "Marketplace install preview") {
				t.Fatalf("--verbose omitted the verified Marketplace preview: %s", result.Combined)
			}
			consumer.AssertExitCode(consumer.RunAM("sync", "--check"), 0)
			repeat := consumer.RunAM("install", "behavior-source/"+tc.kind+"/"+tc.name, "--marketplace", "behavior", "--as", tc.alias, "--yes", "--scope", "repo")
			if repeat.ExitCode != 0 || consumer.ReadFile("/workspace/.agents/config.yaml") != config || consumer.ReadFile("/workspace/.agents/lock.yaml") != locked {
				t.Fatalf("repeat aliased install changed selection or exact acquisition: exit=%d output=%s", repeat.ExitCode, repeat.Combined)
			}
			consumer.AssertExitCode(consumer.RunAM("sync", "--offline"), 0)
			consumer.AssertFileContains(tc.output, tc.body)
			if consumer.ReadFile("/workspace/.agents/config.yaml") != config || consumer.ReadFile("/workspace/.agents/lock.yaml") != locked {
				t.Fatal("offline aliased sync changed selection or exact acquisition")
			}
			removed := consumer.RunAM("remove", tc.alias, "--kind", tc.kind, "--scope", "repo")
			if removed.ExitCode != 0 {
				t.Fatalf("remove by installed alias failed (exit %d):\n%s", removed.ExitCode, removed.Combined)
			}
			consumer.AssertFileAbsent(tc.output)
			assertBehaviorMarketplaceLockHasNoResources(t, consumer, "/workspace/.agents/lock.yaml", "repo")
			if config := consumer.ReadFile("/workspace/.agents/config.yaml"); strings.Contains(config, "as: "+tc.alias) {
				t.Fatalf("remove by installed alias retained the pin: %s", config)
			}
			consumer.AssertExitCode(consumer.RunAM("sync", "--check"), 0)
		})
	}
}

func TestMarketplacePinRemainsExactUntilUpdateAndWorksOffline(t *testing.T) {
	fixture := newMarketplaceBehaviorFixture(t, marketplaceBehaviorFiles("version one", false))
	consumer := fixture.newConsumer(t, fixture.server.URL)
	oldEvidence := assertMarketplaceArtifactIntegrity(t, fixture.server.URL, "behavior-marketplace", "behavior-source", "behavior-source-id", "skill", "skill-one")
	result := consumer.RunAM("install", "behavior:behavior-source/skill/skill-one", "--yes")
	if result.ExitCode != 0 {
		t.Fatalf("install revision one failed (exit %d):\n%s", result.ExitCode, result.Combined)
	}
	consumer.AssertFileContains("/workspace/.factory/skills/skill-one/SKILL.md", "version one")
	consumer.AssertFileContains("/workspace/.agents/config.yaml", "behavior-source/skill/skill-one")
	config := consumer.ReadFile("/workspace/.agents/config.yaml")
	assertBehaviorMarketplaceDirectLock(t, consumer, "/workspace/.agents/lock.yaml", "repo", fixture.server.URL, "skill", "skill-one", oldEvidence)
	oldLock := consumer.ReadFile("/workspace/.agents/lock.yaml")

	fixture.mutateSource(t, "skills/skill-one/SKILL.md", "---\nname: skill-one\ndescription: Behavior skill\nlicense: MIT\n---\nversion two\n")
	fixture.reindex(t, fixture.cfg)
	newEvidence := assertMarketplaceArtifactIntegrity(t, fixture.server.URL, "behavior-marketplace", "behavior-source", "behavior-source-id", "skill", "skill-one")
	if newEvidence.ContentDigest == oldEvidence.ContentDigest || newEvidence.ArchiveDigest == oldEvidence.ArchiveDigest {
		t.Fatal("source update did not publish new verified content and archive revisions")
	}
	result = consumer.RunAM("sync")
	if result.ExitCode != 0 {
		t.Fatalf("exact-pin sync failed (exit %d):\n%s", result.ExitCode, result.Combined)
	}
	consumer.AssertFileContains("/workspace/.factory/skills/skill-one/SKILL.md", "version one")
	assertBehaviorMarketplaceDirectLock(t, consumer, "/workspace/.agents/lock.yaml", "repo", fixture.server.URL, "skill", "skill-one", oldEvidence)
	if consumer.ReadFile("/workspace/.agents/config.yaml") != config || consumer.ReadFile("/workspace/.agents/lock.yaml") != oldLock {
		t.Fatal("ordinary sync changed selection intent or advanced/rewrote its exact acquisition lock")
	}

	result = consumer.RunAM("update")
	if result.ExitCode != 0 {
		t.Fatalf("Marketplace update failed (exit %d):\n%s", result.ExitCode, result.Combined)
	}
	consumer.AssertFileContains("/workspace/.factory/skills/skill-one/SKILL.md", "version two")
	assertBehaviorMarketplaceDirectLock(t, consumer, "/workspace/.agents/lock.yaml", "repo", fixture.server.URL, "skill", "skill-one", newEvidence)
	updatedLock := consumer.ReadFile("/workspace/.agents/lock.yaml")
	if consumer.ReadFile("/workspace/.agents/config.yaml") != config || updatedLock == oldLock {
		t.Fatal("update did not replace the exact acquisition while preserving unconstrained selection intent")
	}
	fixture.server.Close()
	result = consumer.RunAM("sync", "--offline")
	if result.ExitCode != 0 {
		t.Fatalf("updated pin failed offline (exit %d):\n%s", result.ExitCode, result.Combined)
	}
	consumer.AssertFileContains("/workspace/.factory/skills/skill-one/SKILL.md", "version two")
	assertBehaviorMarketplaceDirectLock(t, consumer, "/workspace/.agents/lock.yaml", "repo", fixture.server.URL, "skill", "skill-one", newEvidence)
	if consumer.ReadFile("/workspace/.agents/config.yaml") != config || consumer.ReadFile("/workspace/.agents/lock.yaml") != updatedLock {
		t.Fatal("offline reuse changed selection intent or the updated exact acquisition lock")
	}
}

func TestMarketplaceInstallArtifactFailureIsTransactional(t *testing.T) {
	fixture := newMarketplaceBehaviorFixture(t, marketplaceBehaviorFiles("transactional", false))
	var artifactRequests atomic.Int32
	var failArtifact atomic.Bool
	failArtifact.Store(true)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/v1/artifacts/") || strings.HasPrefix(r.URL.Path, "/api/v1/archives/") {
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
		consumer.WriteGlobalConfig("apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
		consumer.WriteRepoConfig(fmt.Sprintf("apiVersion: agent-marketplace/v4\nsources: {behavior: {marketplace: %q}}\n", fixture.server.URL))
		// Endpoint identity is acquisition evidence, not a v4 source field.
		lock := fmt.Sprintf("apiVersion: agent-marketplace-lock/v2\nscope: repo\nmarketplaces:\n  - url: %s\n    instance_id: wrong-id\n", fixture.server.URL)
		consumer.WriteFile("/workspace/.agents/lock.yaml", lock)
		before := consumer.ReadFile("/workspace/.agents/config.yaml")
		result := consumer.RunAM("install", "behavior:behavior-source/skill/skill-one", "--yes")
		if result.ExitCode == 0 || !strings.Contains(result.Combined, "ID mismatch") {
			t.Fatalf("instance mismatch result = exit %d:\n%s", result.ExitCode, result.Combined)
		}
		if consumer.ReadFile("/workspace/.agents/config.yaml") != before || consumer.ReadFile("/workspace/.agents/lock.yaml") != lock {
			t.Fatal("instance mismatch mutated config or lock")
		}
		consumer.AssertFileAbsent("/workspace/.factory/skills/skill-one")
		consumer.WriteFile("/workspace/.agents/lock.yaml", strings.Replace(lock, "wrong-id", "behavior-marketplace", 1))
		consumer.MustRunAM("install", "behavior:behavior-source/skill/skill-one", "--yes")
		consumer.AssertFileContains("/workspace/.factory/skills/skill-one/SKILL.md", "identity")
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
		consumer.WriteGlobalConfig(fmt.Sprintf("apiVersion: agent-marketplace/v4\nharnesses: [factory]\nallowed_marketplaces: ['^https://allowed\\.invalid$']\nsources: {behavior: {marketplace: %q}}\n", proxy.URL))
		consumer.WriteFile("/root/.agents/lock.yaml", fmt.Sprintf("apiVersion: agent-marketplace-lock/v2\nscope: user\nmarketplaces: [{url: %q, instance_id: behavior-marketplace}]\n", proxy.URL))
		before := snapshotConsumerEvidence(t, consumer)
		result := consumer.RunAM("search", "--marketplace", "behavior")
		if result.ExitCode == 0 || !strings.Contains(result.Combined, "blocked") || requests.Load() != 0 {
			t.Fatalf("blocked endpoint result = exit %d, requests %d:\n%s", result.ExitCode, requests.Load(), result.Combined)
		}
		assertConsumerEvidence(t, consumer, before)
		consumer.WriteGlobalConfig(fmt.Sprintf("apiVersion: agent-marketplace/v4\nharnesses: [factory]\nallowed_marketplaces: ['^http://127\\.0\\.0\\.1:[0-9]+$']\nsources: {behavior: {marketplace: %q}}\n", proxy.URL))
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
	if config := consumer.ReadFile("/workspace/.agents/config.yaml"); !strings.Contains(config, "behavior-source/collection/behavior-bundle") || strings.Contains(config, "behavior-source/skill/skill-one") {
		t.Fatalf("collection install did not persist only the collection intent:\n%s", config)
	}
	if lock := consumer.ReadFile("/workspace/.agents/lock.yaml"); strings.Count(lock, "selected_by:") != 2 || !strings.Contains(lock, "name: behavior-bundle") {
		t.Fatalf("collection acquisition lock did not persist both member selector edges:\n%s", lock)
	}

	result = consumer.RunAM("install", "behavior:behavior-source/skill/skill-one", "--yes")
	if result.ExitCode != 0 {
		t.Fatalf("direct member install failed (exit %d):\n%s", result.ExitCode, result.Combined)
	}
	if config := consumer.ReadFile("/workspace/.agents/config.yaml"); !strings.Contains(config, "behavior-source/skill/skill-one") || !strings.Contains(config, "behavior-source/collection/behavior-bundle") {
		t.Fatalf("direct member install did not add a separate selector alongside the collection:\n%s", config)
	}
	result = consumer.RunAM("remove", "skill-one", "--kind", "skill")
	if result.ExitCode != 0 {
		t.Fatalf("direct selector removal failed (exit %d):\n%s", result.ExitCode, result.Combined)
	}
	consumer.AssertRepoFactorySkillExists("skill-one")
	if config := consumer.ReadFile("/workspace/.agents/config.yaml"); strings.Contains(config, "behavior-source/skill/skill-one") || !strings.Contains(config, "behavior-source/collection/behavior-bundle") {
		t.Fatalf("direct removal did not preserve only the collection selector:\n%s", config)
	}
	if lock := consumer.ReadFile("/workspace/.agents/lock.yaml"); strings.Count(lock, "selected_by:") != 2 {
		t.Fatalf("direct removal lost collection acquisition edges:\n%s", lock)
	}

	result = consumer.RunAM("remove", "behavior-bundle", "--kind", "collection")
	if result.ExitCode != 0 {
		t.Fatalf("collection removal failed (exit %d):\n%s", result.ExitCode, result.Combined)
	}
	consumer.AssertRepoFactorySkillAbsent("skill-one")
	consumer.AssertFileAbsent("/workspace/.factory/commands/command-one.md")
}

// spec: resource-removal/remove-a-marketplace-pin-by-its-endpoint
func TestMarketplaceRemoveSuggestedEndpointKeepsLocalSelection(t *testing.T) {
	const configPath, lockPath = "/workspace/.agents/config.yaml", "/workspace/.agents/lock.yaml"
	fixture := newMarketplaceBehaviorFixture(t, marketplaceBehaviorFiles("Hosted skill", false))
	consumer := fixture.newConsumer(t, fixture.server.URL)
	consumer.WriteFile("/workspace/resources/skills/keep/SKILL.md", (Skill{Name: "keep", Description: "Keep selected"}).SkillFile())
	consumer.WriteRepoConfig(fmt.Sprintf("apiVersion: agent-marketplace/v4\nsources: {behavior: {marketplace: %q}, local: {directory: ./resources, source_harness: factory}}\nresources: {local: [skill/skill-one, skill/keep]}\n", fixture.server.URL))
	result := consumer.RunAM("install", "behavior:behavior-source/skill/skill-one", "--yes")
	if result.ExitCode != 0 {
		t.Fatalf("Marketplace install failed (exit %d):\n%s", result.ExitCode, result.Combined)
	}
	consumer.AssertFileContains("/workspace/.factory/skills/skill-one/SKILL.md", "Hosted skill")

	configBefore, lockBefore := consumer.ReadFile(configPath), consumer.ReadFile(lockPath)
	if !strings.Contains(lockBefore, "skill-one") {
		t.Fatalf("acquisition lock does not record the Marketplace pin:\n%s", lockBefore)
	}
	result = consumer.RunAM("remove", "skill-one")
	suggestion := fmt.Sprintf("--scope repo --from %q   (REPO)", fixture.server.URL)
	if result.ExitCode == 0 || !strings.Contains(result.Combined, "\n  "+suggestion) {
		t.Fatalf("ambiguous removal did not suggest %q (exit %d):\n%s", suggestion, result.ExitCode, result.Combined)
	}
	if consumer.ReadFile(configPath) != configBefore || consumer.ReadFile(lockPath) != lockBefore {
		t.Fatal("ambiguity refusal changed configuration or acquisition lock")
	}

	result = consumer.RunAM("remove", "skill-one", "--scope", "repo", "--from", fixture.server.URL)
	if result.ExitCode != 0 {
		t.Fatalf("suggested Marketplace removal failed (exit %d):\n%s", result.ExitCode, result.Combined)
	}
	cfg, lock := consumer.ReadFile(configPath), consumer.ReadFile(lockPath)
	if strings.Contains(cfg, "behavior-source/skill/skill-one") || !strings.Contains(cfg, "skill/skill-one") || !strings.Contains(cfg, "skill/keep") || strings.Contains(lock, "skill-one") {
		t.Fatalf("endpoint removal did not remove only the Marketplace pin:\nconfig:\n%s\nlock:\n%s", cfg, lock)
	}
	consumer.AssertRepoFactorySkillAbsent("skill-one")
	consumer.AssertRepoFactorySkillExists("keep")

	result = consumer.RunAM("remove", "skill-one")
	if result.ExitCode != 0 || strings.Contains(consumer.ReadFile(configPath), "skill/skill-one") {
		t.Fatalf("stale local selection was not removable after the pin (exit %d):\n%s", result.ExitCode, result.Combined)
	}
	if result = consumer.RunAM("sync", "--check"); result.ExitCode != 0 {
		t.Fatalf("sync --check after removals failed (exit %d):\n%s", result.ExitCode, result.Combined)
	}
	consumer.AssertRepoFactorySkillExists("keep")
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
	data, err := os.ReadFile(consumer.mustResolvePath("/workspace/.factory/mcp.json"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("read the repository MCP config after removal: %v", err)
	}
	if strings.Contains(string(data), "sonarqube") {
		t.Fatalf("removed MCP server is still materialized:\n%s", data)
	}
	consumer.AssertFileContains("/root/.factory/skills/skill-one/SKILL.md", "Behavior body")
	for _, args := range [][]string{{"gc"}, {"sync", "--check"}} {
		consumer.AssertExitCode(consumer.RunAM(args...), 0)
	}
	consumer.AssertFileAbsent("/etc/agent-marketplace/lock.yaml")
}
