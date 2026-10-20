package e2e_test

import (
	"strings"
	"testing"
)

func TestInspectPluginFromV4DirectorySource(t *testing.T) {
	env := newEnv(t)
	env.InitGitRepo()
	env.WriteGlobalConfig("harnesses: [claude]\n")
	env.WriteFile("/workspace/plugins/.claude-plugin/marketplace.json",
		`{"name":"team","plugins":[{"name":"kit","source":"./plugins/kit"}]}`)
	env.WriteFile("/workspace/plugins/plugins/kit/.claude-plugin/plugin.json",
		`{"name":"kit","description":"E2E inspect plugin"}`)
	env.WriteFile("/workspace/plugins/plugins/kit/skills/review/SKILL.md",
		"---\nname: review\ndescription: Review skill\n---\n\nReview body.\n")
	env.WriteRepoConfig(`
materialization: {mode: file}
sources:
  team: {directory: ./plugins, format: claude-marketplace}
`)

	result := env.RunAM("inspect", "plugin/kit")
	env.AssertExitCode(result, 0)
	for _, want := range []string{"PLUGIN kit", "E2E inspect plugin", "claude-plugin", "review", "TARGET DELIVERY"} {
		if !strings.Contains(result.Combined, want) {
			t.Errorf("inspect omitted %q:\n%s", want, result.Combined)
		}
	}
	if !strings.Contains(result.Combined, "REVISION: local") {
		t.Errorf("directory source revision was not identified as local:\n%s", result.Combined)
	}
	env.AssertFileContains("/workspace/.agents/config.yaml", "directory: ./plugins")
	env.AssertFileAbsent("/workspace/.agents/lock.yaml")
	env.AssertFileAbsent("/workspace/.claude")
}
