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
			if data, err := os.ReadFile(consumer.mustResolvePath(tc.mcpPath)); err == nil && strings.Contains(string(data), "sonarqube") {
				t.Fatalf("removed MCP server is still materialized in %s:\n%s", tc.mcpPath, data)
			}
			consumer.AssertExitCode(consumer.RunAM("sync", "--check"), 0)
		})
	}
}
