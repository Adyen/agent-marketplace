package e2e_test

import (
	"fmt"
	"testing"
)

// spec: plugin-delivery/claude-probe-confirms-repository-scope-stays-decomposed
func TestClaudeRepoScopeAutoPluginCollectionInstall(t *testing.T) {
	e := newEnv(t)
	e.InitGitRepo()
	f := newCompatibilityFixture(e, "claude", buildCompatibilityHelper(t, "compat-mcp"))

	config := "harnesses: [claude]\nmaterialization:\n  mode: symlink\n"
	e.WriteGlobalConfig(config)
	config += fmt.Sprintf("sources:\n  fixture: {directory: %q, source_harness: claude}\n", f.registry)
	config, pluginName, pluginSkill := addClaudePluginProbeSource(e, f, config, "auto")
	e.WriteRepoConfig(config)

	result := e.RunAM("install", f.name("collection"), "--kind", "collection", "--scope", "repo")
	if result.ExitCode != 0 {
		t.Fatalf("repo-scope collection install failed (exit %d):\n%s", result.ExitCode, result.Combined)
	}
	if err := assertClaudeRepositoryPluginDecomposed(e.workspace, pluginName, pluginSkill); err != nil {
		t.Fatal(err)
	}
}
