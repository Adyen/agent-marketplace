package e2e_test

import (
	"strings"
	"testing"
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
