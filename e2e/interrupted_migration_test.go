package e2e_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The obsolete migration fixture now exercises a supported multi-destination
// journaled purge. Its independently expected final state is absence of the
// repository config and lock, while the user config and handwritten output stay.
func TestInterruptedPurgeRecoversOnNextMutation(t *testing.T) {
	env := newEnv(t)
	env.InitGitRepo()
	env.WriteGlobalConfig("apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	url := env.CreateBareRepo("interrupted-purge", SkillsOnly(Skill{Name: "review"}))
	env.WriteRepoConfig("apiVersion: agent-marketplace/v4\nsources: {team: {git: '" + url + "', ref: main}}\nresources: {team: {include: [skill/review]}}\n")
	env.MustRunAM("sync")
	env.WriteFile("/workspace/.factory/skills/handwritten/SKILL.md", "HANDWRITTEN\n")
	user := env.ReadFile("/root/.agents/config.yaml")
	paths := []string{"/workspace/.agents/config.yaml", "/workspace/.agents/lock.yaml"}

	crash := env.RunAMWithEnv(map[string]string{"AGENT_MARKETPLACE_E2E_CRASH_AT": "after-publish:0"}, "deinit", "--scope", "repo", "--purge-config")
	if crash.ExitCode != 137 {
		t.Fatalf("fault injection did not terminate journaled purge (exit %d):\n%s", crash.ExitCode, crash.Combined)
	}
	remaining := 0
	interrupted := map[string]string{}
	for _, path := range paths {
		if _, err := os.Lstat(env.mustResolvePath(path)); err == nil {
			remaining++
			fingerprint, err := pathFingerprint(env.mustResolvePath(path))
			if err != nil {
				t.Fatal(err)
			}
			interrupted[path] = fingerprint
		} else if !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
	if remaining != 1 {
		t.Fatalf("interruption left %d destinations, want exactly one of two published", remaining)
	}
	const journalRoot = "/root/.local/state/agent-marketplace/dependencies"
	journalPattern := filepath.Join(env.mustResolvePath(journalRoot), ".dependencytxn-*.json")
	if journals, err := filepath.Glob(journalPattern); err != nil || len(journals) != 1 {
		t.Fatalf("interruption did not retain one pending journal: %v %v", journals, err)
	}
	journalBefore, err := pathFingerprint(env.mustResolvePath(journalRoot))
	if err != nil {
		t.Fatal(err)
	}
	check := env.RunAM("sync", "--check")
	if check.ExitCode == 0 || !strings.Contains(check.Combined, "dependency.recovery-required") {
		t.Fatalf("read-only sync did not refuse pending work:\n%s", check.Combined)
	}
	for _, path := range paths {
		if want, exists := interrupted[path]; exists {
			if got, err := pathFingerprint(env.mustResolvePath(path)); err != nil || got != want {
				t.Fatalf("read-only check changed %s: %v", path, err)
			}
		} else {
			env.AssertFileAbsent(path)
		}
	}
	if after, err := pathFingerprint(env.mustResolvePath(journalRoot)); err != nil || after != journalBefore {
		t.Fatalf("read-only check changed pending journal evidence: %v", err)
	}
	env.MustRunAM("sync")
	for _, path := range paths {
		env.AssertFileAbsent(path)
	}
	if journals, err := filepath.Glob(journalPattern); err != nil || len(journals) != 0 {
		t.Fatalf("recovery did not finalize the pending journal: %v %v", journals, err)
	}
	if got := env.ReadFile("/root/.agents/config.yaml"); got != user {
		t.Fatal("recovery changed the independent user configuration")
	}
	if got := env.ReadFile("/workspace/.factory/skills/handwritten/SKILL.md"); got != "HANDWRITTEN\n" {
		t.Fatalf("purge/recovery changed unowned output: %q", got)
	}
	env.MustRunAM("sync", "--check")
}
