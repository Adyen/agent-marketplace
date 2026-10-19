package e2e_test

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// Installing through an organization alias into an existing v4 user or
// repository root must leave every later reader and scoped mutation usable.
func TestOrganizationMarketplaceAliasInstallKeepsV4LayersUsable(t *testing.T) {
	for _, tc := range []struct {
		scope, mcpPath string
		setup          func(*Env)
	}{
		{scope: "user", mcpPath: "/root/.factory/mcp.json", setup: func(e *Env) {
			e.WriteGlobalConfig("apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
		}},
		{scope: "repo", mcpPath: "/workspace/.factory/mcp.json", setup: func(e *Env) {
			e.MustRunAM("init", "--harnesses", "factory")
			e.WriteRepoConfig("apiVersion: agent-marketplace/v4\nsources: {}\n")
		}},
	} {
		t.Run(tc.scope, func(t *testing.T) {
			files := marketplaceBehaviorFiles("Behavior body", true)
			files[".mcp.json"] = MergeMCPFile([]MCPServer{{Name: "sonarqube", Command: "echo", Args: []string{"sonarqube"}}})
			fixture := newMarketplaceBehaviorFixture(t, files)
			consumer := newEnv(t)
			consumer.InitGitRepo()
			tc.setup(consumer)
			organization := organizationAliasConfig(fixture.server.URL)
			consumer.WriteOrgConfigWithMode(organization, 0o644)

			install := consumer.RunAM("install", "org:behavior-source/mcpserver/sonarqube", "--scope", tc.scope, "--yes")
			if install.ExitCode != 0 {
				t.Fatalf("install through the organization alias into the v4 %s config failed (exit %d):\n%s", tc.scope, install.ExitCode, install.Combined)
			}
			consumer.AssertFileContains(tc.mcpPath, "sonarqube")
			consumer.AssertFileContains("/root/.factory/skills/skill-one/SKILL.md", "Behavior body")
			for _, args := range [][]string{{"sync"}, {"status"}, {"list"}, {"search", "sonarqube"}, {"update"}, {"sync", "--check"}} {
				consumer.AssertExitCode(consumer.RunAM(args...), 0)
			}

			remove := consumer.RunAM("remove", "sonarqube", "--scope", tc.scope)
			if remove.ExitCode != 0 {
				t.Fatalf("remove from the v4 %s config failed (exit %d):\n%s", tc.scope, remove.ExitCode, remove.Combined)
			}
			data, err := os.ReadFile(consumer.mustResolvePath(tc.mcpPath))
			if err != nil && !os.IsNotExist(err) {
				t.Fatalf("read %s after removal: %v", tc.mcpPath, err)
			}
			if strings.Contains(string(data), "sonarqube") {
				t.Fatalf("removed MCP server is still materialized in %s:\n%s", tc.mcpPath, data)
			}
			consumer.AssertFileContains("/root/.factory/skills/skill-one/SKILL.md", "Behavior body")
			consumer.AssertExitCode(consumer.RunAM("sync", "--check"), 0)
			if got := consumer.ReadFile("/etc/agent-marketplace/config.yaml"); got != organization {
				t.Fatal("scoped lifecycle changed organization selection authority")
			}
			consumer.AssertFileAbsent("/etc/agent-marketplace/lock.yaml")
		})
	}
}

func organizationAliasConfig(url string) string {
	return fmt.Sprintf("apiVersion: agent-marketplace/v4\nsources:\n  org:\n    marketplace: %s\nallowed_marketplaces:\n  - '^http://127\\.0\\.0\\.1:[0-9]+$'\nresources:\n  org:\n    include:\n      - behavior-source/collection/behavior-bundle\n", url)
}

// Reusing an organization alias for the same endpoint but a different locked
// instance remains a real conflict. Identity is authored in independent locks.
func TestOrganizationMarketplaceAliasRefusesADifferentInstance(t *testing.T) {
	fixture := newMarketplaceBehaviorFixture(t, marketplaceBehaviorFiles("Behavior body", true))
	consumer := newEnv(t)
	consumer.InitGitRepo()
	consumer.WriteGlobalConfig(fmt.Sprintf("apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources: {org: {marketplace: %q}}\n", fixture.server.URL))
	consumer.WriteOrgConfigWithMode(fmt.Sprintf("apiVersion: agent-marketplace/v4\nsources: {org: {marketplace: %q}}\n", fixture.server.URL), 0o644)
	consumer.WriteFile("/root/.agents/lock.yaml", fmt.Sprintf("apiVersion: agent-marketplace-lock/v2\nscope: user\nmarketplaces: [{url: %q, instance_id: other-instance}]\n", fixture.server.URL))
	consumer.WriteFile("/etc/agent-marketplace/lock.yaml", fmt.Sprintf("apiVersion: agent-marketplace-lock/v2\nscope: organization\nmarketplaces: [{url: %q, instance_id: behavior-marketplace}]\n", fixture.server.URL))
	before := snapshotConsumerEvidence(t, consumer)
	for _, args := range [][]string{{"status"}, {"sync"}} {
		result := consumer.RunAM(args...)
		if result.ExitCode == 0 || !strings.Contains(result.Combined, `alias "org" duplicates`) {
			t.Fatalf("%v accepted an alias for a different Marketplace instance (exit %d):\n%s", args, result.ExitCode, result.Combined)
		}
		assertConsumerEvidence(t, consumer, before)
	}
	// Matching independently authored identities is a usable alias, proving
	// refusal is not simply a ban on repeated organization/user source names.
	consumer.WriteFile("/root/.agents/lock.yaml", fmt.Sprintf("apiVersion: agent-marketplace-lock/v2\nscope: user\nmarketplaces: [{url: %q, instance_id: behavior-marketplace}]\n", fixture.server.URL))
	consumer.MustRunAM("install", "org:behavior-source/skill/skill-one", "--scope", "user", "--yes")
	consumer.AssertFileContains("/root/.factory/skills/skill-one/SKILL.md", "Behavior body")
}

// The still-relevant outcome of the former migration-success test is an exact
// scoped install whose authored selection and owning lock survive later reads.
func TestOrganizationMarketplaceAliasUserPinSurvivesLockedAndOfflineSync(t *testing.T) {
	fixture := newMarketplaceBehaviorFixture(t, marketplaceBehaviorFiles("Behavior body", true))
	consumer := newEnv(t)
	consumer.InitGitRepo()
	consumer.WriteGlobalConfig("apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	consumer.WriteOrgConfigWithMode(organizationAliasConfig(fixture.server.URL), 0o644)
	// Locked/offline reads require the organization collection's own exact
	// evidence, deployed externally rather than written by the user mutation.
	publisher := fixture.newConsumer(t, fixture.server.URL)
	publisher.MustRunAM("install", "behavior:behavior-source/collection/behavior-bundle", "--yes")
	organizationLock := strings.Replace(publisher.ReadFile("/workspace/.agents/lock.yaml"), "scope: repo", "scope: organization", 1)
	consumer.WriteFile("/etc/agent-marketplace/lock.yaml", organizationLock)
	consumer.MustRunAM("install", "org:behavior-source/skill/skill-one", "--scope", "user", "--yes")
	user := consumer.ReadFile("/root/.agents/config.yaml")
	lock := consumer.ReadFile("/root/.agents/lock.yaml")
	if !strings.Contains(user, "apiVersion: agent-marketplace/v4") || !strings.Contains(user, "behavior-source/skill/skill-one") {
		t.Fatalf("user config lost the organization-alias selection:\n%s", user)
	}
	for _, want := range []string{"scope: user", "instance_id: behavior-marketplace", "registry_id: behavior-source-id", "kind: skill", "name: skill-one"} {
		if !strings.Contains(lock, want) {
			t.Fatalf("owning user lock missing %q:\n%s", want, lock)
		}
	}
	for _, args := range [][]string{{"sync", "--locked"}, {"sync", "--offline"}, {"sync", "--check"}} {
		consumer.MustRunAM(args...)
		if consumer.ReadFile("/root/.agents/config.yaml") != user || consumer.ReadFile("/root/.agents/lock.yaml") != lock {
			t.Fatalf("%v changed authored selection or exact lock", args)
		}
		consumer.AssertFileContains("/root/.factory/skills/skill-one/SKILL.md", "Behavior body")
	}
	consumer.AssertFileContains("/root/.factory/commands/command-one.md", "echo behavior")
	if consumer.ReadFile("/etc/agent-marketplace/lock.yaml") != organizationLock {
		t.Fatal("scoped pin lifecycle changed deployed organization evidence")
	}
}
