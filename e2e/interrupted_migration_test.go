package e2e_test

import (
	"strings"
	"testing"
)

// The process dies after publishing the first file of `migrate --scope all`.
// Read-only commands must refuse the half-published state instead of using
// it, and the next mutating command must finish the migration coherently.
func TestInterruptedMigrationRecoversOnNextMutation(t *testing.T) {
	env := newEnv(t)
	env.InitGitRepo()
	env.WriteGlobalConfig("harnesses: [factory]\nregistries: []")
	for _, name := range []string{"review", "second"} {
		env.WriteFile("/workspace/vendor/registry/skills/"+name+"/SKILL.md", "---\nname: "+name+"\ndescription: "+name+"\n---\n\n"+name+" body\n")
	}
	env.WriteRepoConfig("registries:\n  - path: ./vendor/registry\n    skills:\n      include: [review]\n")
	env.WriteLocalConfig("registries:\n  - path: ./vendor/registry\n    skills:\n      include: [second]\n")
	env.MustRunAM("sync")
	roots := []string{"/root/.agents/config.yaml", "/workspace/.agents/config.yaml", "/workspace/.agents/config.local.yaml"}
	migratedRoots := func() int {
		t.Helper()
		count := 0
		for _, path := range roots {
			if strings.Contains(env.ReadFile(path), "apiVersion: agent-marketplace/v4") {
				count++
			}
		}
		return count
	}
	snapshot := func() map[string]string {
		t.Helper()
		files := make(map[string]string, len(roots))
		for _, path := range roots {
			files[path] = env.ReadFile(path)
		}
		return files
	}

	crash := env.RunAMWithEnv(map[string]string{"AGENT_MARKETPLACE_E2E_CRASH_AT": "after-publish:0"}, "migrate", "--scope", "all")
	if crash.ExitCode != 137 {
		t.Fatalf("injected crash did not kill migrate (exit %d):\n%s", crash.ExitCode, crash.Combined)
	}
	if got := migratedRoots(); got != 1 {
		t.Fatalf("crash left %d of %d roots migrated; want exactly the first published file", got, len(roots))
	}
	interrupted := snapshot()

	check := env.RunAM("sync", "--check")
	if check.ExitCode == 0 {
		t.Fatalf("read-only sync --check accepted a half-published migration:\n%s", check.Combined)
	}
	for path, want := range interrupted {
		if got := env.ReadFile(path); got != want {
			t.Fatalf("read-only check changed %s during an unrecovered transaction", path)
		}
	}

	env.MustRunAM("sync")
	if got := migratedRoots(); got != len(roots) {
		t.Fatalf("recovery left %d of %d roots migrated", got, len(roots))
	}
	env.AssertFileContains("/workspace/.factory/skills/review/SKILL.md", "review body")
	env.AssertFileContains("/workspace/.factory/skills/second/SKILL.md", "second body")
	env.AssertExitCode(env.RunAM("sync", "--check"), 0)

	recovered := snapshot()
	env.MustRunAM("migrate", "--scope", "all")
	for path, want := range recovered {
		if got := env.ReadFile(path); got != want {
			t.Fatalf("migrate after recovery rewrote %s:\nbefore:\n%s\nafter:\n%s", path, want, got)
		}
	}
}
