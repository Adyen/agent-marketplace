package e2e_test

import (
	"fmt"
	"strings"
	"testing"

	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/worker"
)

// newV4MarketplaceConsumer is a user whose config init created as v4 and whose
// organization config declares the Marketplace under the org alias.
func newV4MarketplaceConsumer(t *testing.T, fixture *marketplaceBehaviorFixture) *Env {
	t.Helper()
	consumer := newEnv(t)
	consumer.InitGitRepo()
	consumer.MustRunAM("init", "--harnesses", "factory")
	consumer.WriteOrgConfigWithMode(fmt.Sprintf("apiVersion: agent-marketplace/v4\nsources:\n  org:\n    marketplace: %s\nallowed_marketplaces:\n  - '^http://127\\.0\\.0\\.1:[0-9]+$'\n", fixture.server.URL), 0o644)
	return consumer
}

// A v4 user installs a hosted resource by bare coordinate with --marketplace,
// under a local name with --as, accepting its findings with --accept-risk,
// and pages through search results.
func TestMarketplaceInstallAndSearchFlagsAgainstV4UserConfig(t *testing.T) {
	fixture := newMarketplaceBehaviorFixture(t, marketplaceBehaviorFiles("Named v4 install", false))
	consumer := newV4MarketplaceConsumer(t, fixture)

	result := consumer.RunAM("install", "behavior-source/skill/skill-one", "--marketplace", "org", "--as", "review-copy", "--accept-risk", "--scope", "user")
	if result.ExitCode != 0 {
		t.Fatalf("install with --marketplace --as --accept-risk into a v4 user config failed (exit %d):\n%s", result.ExitCode, result.Combined)
	}
	consumer.AssertFileContains("/root/.factory/skills/review-copy/SKILL.md", "name: review-copy")
	consumer.AssertFileAbsent("/root/.factory/skills/skill-one")
	if config := consumer.ReadFile("/root/.agents/config.yaml"); !strings.Contains(config, "apiVersion: agent-marketplace/v4") || !strings.Contains(config, "review-copy") {
		t.Fatalf("v4 user config lost the local name or its schema:\n%s", config)
	}
	consumer.AssertExitCode(consumer.RunAM("sync", "--check"), 0)

	all := consumer.RunAM("search", "--output", "json")
	page := consumer.RunAM("search", "--limit", "1", "--offset", "1", "--output", "json")
	if all.ExitCode != 0 || page.ExitCode != 0 {
		t.Fatalf("search failed: all exit %d, page exit %d:\n%s\n%s", all.ExitCode, page.ExitCode, all.Combined, page.Combined)
	}
	if total, paged := strings.Count(all.Stdout, `"name": "`), strings.Count(page.Stdout, `"name": "`); total < 2 || paged != 1 {
		t.Fatalf("search --limit 1 --offset 1 returned %d of %d results, want 1:\n%s", paged, total, page.Stdout)
	}
}

// A repository v4 configuration accepts Marketplace findings with --accept-risk.
func TestMarketplaceInstallAcceptRiskAgainstV4RepositoryConfig(t *testing.T) {
	fixture := newMarketplaceBehaviorFixture(t, marketplaceBehaviorFiles("Accepted risk", false))
	consumer := fixture.newConsumer(t, fixture.server.URL)
	result := consumer.RunAM("install", "behavior:behavior-source/skill/skill-one", "--accept-risk")
	if result.ExitCode != 0 {
		t.Fatalf("install --accept-risk into a v4 repository config failed (exit %d):\n%s", result.ExitCode, result.Combined)
	}
	consumer.AssertFileContains("/workspace/.factory/skills/skill-one/SKILL.md", "Accepted risk")
	consumer.AssertExitCode(consumer.RunAM("sync", "--check"), 0)
}

// After the Marketplace revokes a resource a v4 user installed, sync refuses
// it and --allow-revoked admits exactly that pin for one invocation.
func TestMarketplaceRevocationOverrideAgainstV4UserConfig(t *testing.T) {
	fixture := newMarketplaceBehaviorFixture(t, marketplaceBehaviorFiles("revocable v4", false))
	consumer := newV4MarketplaceConsumer(t, fixture)
	digest := fixture.resourceDigest(t, "skill", "skill-one")
	if result := consumer.RunAM("install", "org:behavior-source/skill/skill-one", "--scope", "user", "--yes"); result.ExitCode != 0 {
		t.Fatalf("install before revocation failed (exit %d):\n%s", result.ExitCode, result.Combined)
	}
	cfg := fixture.cfg
	cfg.Revocations.RegistryIDs = []string{"behavior-source-id"}
	if err := worker.EnsureRevocations(t.Context(), worker.Deps{Config: cfg, MetadataStore: fixture.meta, BlobStore: fixture.blobs}); err != nil {
		t.Fatalf("reconcile revocation: %v", err)
	}
	fixture.setHandler(mpapi.NewServer(cfg, fixture.meta, fixture.blobs).PublicHandler())

	if result := consumer.RunAM("sync"); result.ExitCode == 0 || !strings.Contains(result.Combined, "revoked") {
		t.Fatalf("sync accepted a revoked pin (exit %d):\n%s", result.ExitCode, result.Combined)
	}
	consumer.AssertFileContains("/root/.factory/skills/skill-one/SKILL.md", "revocable v4")
	coordinate := "behavior-marketplace/behavior-source-id/skill/skill-one@" + digest
	if result := consumer.RunAM("sync", "--allow-revoked", coordinate); result.ExitCode != 0 {
		t.Fatalf("exact revocation override failed (exit %d):\n%s", result.ExitCode, result.Combined)
	}
}
