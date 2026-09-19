package e2e_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	mptestutil "github.com/Adyen/agent-marketplace/internal/testutil/marketplace"
)

func TestMigrateV4ExplicitFlagAndFailureNoWrite(t *testing.T) {
	env := newEnv(t)
	env.InitGitRepo()
	env.WriteRepoConfig(`apiVersion: agent-marketplace/v2
registries:
  - url: https://example.test/agents.git
    ref: main
    skills: {include: [review]}
`)
	before := env.ReadFile("/workspace/.agents/config.yaml")

	preview := env.RunAM("migrate", "--to", "v4", "--scope", "repo", "--dry-run")
	env.AssertExitCode(preview, 0)
	env.AssertOutputContains(preview, "Would migrate repository config")
	if got := env.ReadFile("/workspace/.agents/config.yaml"); got != before {
		t.Fatalf("dry-run changed repository config: %q", got)
	}

	migrated := env.RunAM("migrate", "--to", "v4", "--scope", "repo")
	env.AssertExitCode(migrated, 0)
	env.AssertOutputContains(migrated, "Migrated repository config")
	v4 := env.ReadFile("/workspace/.agents/config.yaml")
	if !strings.Contains(v4, "apiVersion: agent-marketplace/v4") || !strings.Contains(v4, "sources:") || !strings.Contains(v4, "resources:") {
		t.Fatalf("migration did not publish v4 config: %s", v4)
	}

	unsupported := "apiVersion: agent-marketplace/v2\ninclude: child.yaml"
	env.WriteRepoConfig(unsupported)
	env.WriteFile("/workspace/.agents/child.yaml", "apiVersion: agent-marketplace/v2\nregistries: []\n")
	failed := env.RunAM("migrate", "--to", "v4", "--scope", "repo")
	env.AssertExitCode(failed, 1)
	env.AssertOutputContains(failed, "v4.migration-unsupported")
	if got := env.ReadFile("/workspace/.agents/config.yaml"); got != unsupported {
		t.Fatalf("failed migration changed repository config: %q", got)
	}
}

func TestMigrateV4MarketplaceExactPin(t *testing.T) {
	env := newEnv(t)
	env.InitGitRepo()

	// Verified cache evidence under the subprocess HOME, offline: the
	// marketplace endpoint is never contacted.
	seed := mptestutil.SeedVerifiedMarketplaceSkill(t,
		filepath.Join(env.home, ".cache", "agent-marketplace", "marketplace"),
		"https://market.example", "mp", "src", "review")

	env.WriteRepoConfig(`apiVersion: agent-marketplace/v2
marketplaces:
  - url: https://market.example
    instance_id: mp
    resources:
      - registry_id: src
        registry_slug: tools
        kind: skill
        name: review
        revision: ` + seed.ContentDigest + `
        digest: ` + seed.ContentDigest + `
        direct: true
`)
	before := env.ReadFile("/workspace/.agents/config.yaml")

	preview := env.RunAM("migrate", "--scope", "repo", "--dry-run")
	env.AssertExitCode(preview, 0)
	env.AssertOutputContains(preview, "Would migrate repository config")
	if got := env.ReadFile("/workspace/.agents/config.yaml"); got != before {
		t.Fatalf("dry-run changed repository config: %q", got)
	}
	if _, err := os.Stat(env.mustResolvePath("/workspace/.agents/lock.yaml")); !os.IsNotExist(err) {
		t.Fatalf("dry-run unexpectedly wrote lock: %v", err)
	}

	migrated := env.RunAM("migrate", "--scope", "repo")
	env.AssertExitCode(migrated, 0)
	env.AssertOutputContains(migrated, "Migrated repository config")

	v4 := env.ReadFile("/workspace/.agents/config.yaml")
	for _, want := range []string{"apiVersion: agent-marketplace/v4", "marketplace: https://market.example", "tools/skill/review"} {
		if !strings.Contains(v4, want) {
			t.Fatalf("migrated config missing %q:\n%s", want, v4)
		}
	}
	lock := env.ReadFile("/workspace/.agents/lock.yaml")
	for _, want := range []string{"apiVersion: agent-marketplace-lock/v2", "instance_id: mp", "registry_name: tools", seed.ContentDigest, seed.ArchiveDigest} {
		if !strings.Contains(lock, want) {
			t.Fatalf("migrated lock missing %q:\n%s", want, lock)
		}
	}
}
