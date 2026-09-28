package e2e_test

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// Users who keep a legacy (v2) user config, or work in a repository whose
// committed config is still v2, install through the organization Marketplace
// alias while the organization config is v4 without a deployed lock. The
// install must leave every later command usable.
func TestOrganizationMarketplaceAliasInstallKeepsLegacyLayersUsable(t *testing.T) {
	for _, tc := range []struct {
		scope, mcpPath string
		setup          func(*Env)
	}{
		{scope: "user", mcpPath: "/root/.factory/mcp.json", setup: func(e *Env) {
			e.WriteGlobalConfig("apiVersion: agent-marketplace/v2\nharnesses: [factory]\nregistries: []")
		}},
		{scope: "repo", mcpPath: "/workspace/.factory/mcp.json", setup: func(e *Env) {
			e.MustRunAM("init", "--harnesses", "factory")
			e.WriteRepoConfig("apiVersion: agent-marketplace/v2\nregistries: []")
		}},
	} {
		t.Run(tc.scope, func(t *testing.T) {
			files := marketplaceBehaviorFiles("Behavior body", true)
			files[".mcp.json"] = MergeMCPFile([]MCPServer{{Name: "sonarqube", Command: "echo", Args: []string{"sonarqube"}}})
			fixture := newMarketplaceBehaviorFixture(t, files)
			consumer := newEnv(t)
			consumer.InitGitRepo()
			tc.setup(consumer)
			consumer.WriteOrgConfigWithMode(organizationAliasConfig(fixture.server.URL), 0o644)

			install := consumer.RunAM("install", "org:behavior-source/mcpserver/sonarqube", "--scope", tc.scope, "--yes")
			if install.ExitCode != 0 {
				t.Fatalf("install through the organization alias into the legacy %s config failed (exit %d):\n%s", tc.scope, install.ExitCode, install.Combined)
			}
			consumer.AssertFileContains(tc.mcpPath, "sonarqube")
			for _, args := range [][]string{{"sync"}, {"status"}, {"list"}, {"search", "sonarqube"}, {"update"}, {"sync", "--check"}} {
				consumer.AssertExitCode(consumer.RunAM(args...), 0)
			}

			remove := consumer.RunAM("remove", "sonarqube", "--scope", tc.scope)
			if remove.ExitCode != 0 {
				t.Fatalf("remove from the legacy %s config failed (exit %d):\n%s", tc.scope, remove.ExitCode, remove.Combined)
			}
			data, err := os.ReadFile(consumer.mustResolvePath(tc.mcpPath))
			if err != nil && !os.IsNotExist(err) {
				t.Fatalf("read %s after removal: %v", tc.mcpPath, err)
			}
			if strings.Contains(string(data), "sonarqube") {
				t.Fatalf("removed MCP server is still materialized in %s:\n%s", tc.mcpPath, data)
			}
			consumer.AssertExitCode(consumer.RunAM("sync", "--check"), 0)
		})
	}
}

func organizationAliasConfig(url string) string {
	return fmt.Sprintf("apiVersion: agent-marketplace/v4\nsources:\n  org:\n    marketplace: %s\nallowed_marketplaces:\n  - '^http://127\\.0\\.0\\.1:[0-9]+$'\nresources:\n  org:\n    include:\n      - behavior-source/collection/behavior-bundle\n", url)
}

// A legacy entry that reuses the organization alias for the same URL but
// records a different Marketplace instance is a real conflict. It is refused
// once the organization's verified instance is known.
func TestOrganizationMarketplaceAliasRefusesADifferentInstance(t *testing.T) {
	fixture := newMarketplaceBehaviorFixture(t, marketplaceBehaviorFiles("Behavior body", true))
	consumer := newEnv(t)
	consumer.InitGitRepo()
	consumer.WriteGlobalConfig(fmt.Sprintf("apiVersion: agent-marketplace/v2\nharnesses: [factory]\nregistries: []\nmarketplaces:\n  - url: %s\n    instance_id: other-instance\n    alias: org\n", fixture.server.URL))
	consumer.WriteOrgConfigWithMode(organizationAliasConfig(fixture.server.URL), 0o644)
	for _, args := range [][]string{{"status"}, {"sync"}} {
		result := consumer.RunAM(args...)
		if result.ExitCode == 0 || !strings.Contains(result.Combined, `alias "org" duplicates`) {
			t.Fatalf("%v accepted an alias for a different Marketplace instance (exit %d):\n%s", args, result.ExitCode, result.Combined)
		}
	}
}

// After an organization-alias install into a legacy user config, migration
// converts that config to v4 and the result stays in sync.
func TestOrganizationMarketplaceAliasLegacyUserConfigMigrates(t *testing.T) {
	fixture := newMarketplaceBehaviorFixture(t, marketplaceBehaviorFiles("Behavior body", true))
	consumer := newEnv(t)
	consumer.InitGitRepo()
	consumer.WriteGlobalConfig("apiVersion: agent-marketplace/v2\nharnesses: [factory]\nregistries: []")
	consumer.WriteOrgConfigWithMode(organizationAliasConfig(fixture.server.URL), 0o644)
	consumer.AssertExitCode(consumer.RunAM("install", "org:behavior-source/skill/skill-one", "--scope", "user", "--yes"), 0)

	migrate := consumer.RunAM("migrate", "--scope", "user")
	if migrate.ExitCode != 0 {
		t.Fatalf("migrate after the organization-alias install failed (exit %d):\n%s", migrate.ExitCode, migrate.Combined)
	}
	user := consumer.ReadFile("/root/.agents/config.yaml")
	if !strings.Contains(user, "apiVersion: agent-marketplace/v4") || !strings.Contains(user, "behavior-source/skill/skill-one") {
		t.Fatalf("migrated user config lost the organization-alias selection:\n%s", user)
	}
	consumer.AssertExitCode(consumer.RunAM("sync", "--check"), 0)
	consumer.AssertFileExists("/root/.factory/skills/skill-one/SKILL.md")
}
