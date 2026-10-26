package e2e_test

// spec: hook-sync-diagnostics/mutation-commands-list-completed-hook-changes
func init() {
	for _, git := range []bool{false, true} {
		for _, harness := range []string{"claude", "factory"} {
			for _, scope := range []string{"repo", "user"} {
				native, _ := bundleRootNative(harness, scope)
				steps := []Step{{
					Args:         []string{"install", "--all", "--kind", "hook", "--scope", scope},
					MustPass:     true,
					OutContains:  []string{"Hook changes:", harness + " (", " add ", `command: "echo mutation-v1"`, "materialize.hook.review"},
					FileContains: map[string]string{native: "echo mutation-v1"},
				}, {
					Args: []string{"update"}, MustPass: true,
					OutNotContains: []string{"Hook changes:", "materialize.hook.review"},
					PathsUnchanged: []string{native},
				}}
				last := "echo mutation-v1"
				if !git {
					root := "/workspace/resources"
					if scope == "user" {
						root = "/root/.agents/resources"
					}
					steps = append(steps, Step{
						Action: ActionWriteFile, Path: root + "/hooks/hooks.json", Body: bundleRootHooks("echo mutation-v2", ""),
						Args: []string{"update"}, MustPass: true,
						OutContains:  []string{"Hook changes:", " replace ", `- command: "echo mutation-v1"`, `+ command: "echo mutation-v2"`, "materialize.hook.review"},
						FileContains: map[string]string{native: "echo mutation-v2"},
					})
					last = "echo mutation-v2"
				}
				steps = append(steps, Step{
					Args: []string{"remove", "PreToolUse-command", "--kind", "hook", "--scope", scope}, MustPass: true,
					OutContains: []string{"Hook changes:", " remove ", `- command: "` + last + `"`, "materialize.hook.review"},
				})
				testTable = append(testTable, bundleRootCase(
					"mutation_review_"+bundleRootSourceName(git)+"_"+harness+"_"+scope,
					"Given selected hooks, when resource mutations publish them, then each completed change is listed and unchanged updates stay quiet",
					harness, scope, git, bundleRootFiles(bundleRootHooks("echo mutation-v1", "")), steps...,
				))
			}
		}
	}
}
