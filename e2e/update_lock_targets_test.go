package e2e_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// registryClone is a working clone of an E2E bare registry that tests use to
// publish new revisions.
type registryClone struct {
	t    *testing.T
	url  string
	work string
}

func cloneRegistry(t *testing.T, env *Env, name string) registryClone {
	t.Helper()
	url := env.CreateBareRepo(name, map[string]string{"README.md": name})
	work := filepath.Join(t.TempDir(), name)
	gitRun(t, "", "clone", "-q", strings.TrimPrefix(url, "file://"), work)
	return registryClone{t: t, url: url, work: work}
}

// publishSkill commits skills/<name>/SKILL.md with body, pushes main, and
// tags the commit when tag is set.
func (r registryClone) publishSkill(name, body, tag string) {
	r.t.Helper()
	file := filepath.Join(r.work, "skills", name, "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("---\nname: "+name+"\ndescription: "+name+"\n---\n\n"+body+"\n"), 0o644); err != nil {
		r.t.Fatal(err)
	}
	gitRun(r.t, r.work, "add", "-A")
	gitRun(r.t, r.work, "-c", "user.name=E2E", "-c", "user.email=e2e@example.com", "commit", "-qm", body)
	gitRun(r.t, r.work, "push", "-q", "origin", "main")
	if tag != "" {
		gitRun(r.t, r.work, "tag", tag)
		gitRun(r.t, r.work, "push", "-q", "origin", tag)
	}
}

// update --bump with every targeting flag moves only the named source to its
// next release tag, in the repository config of either schema.
func TestTargetedUpdateBumpMovesOnlyTheNamedSource(t *testing.T) {
	for _, schema := range []string{"v2", "v4"} {
		t.Run(schema, func(t *testing.T) {
			env := newEnv(t)
			env.InitGitRepo()
			target, other := cloneRegistry(t, env, "bump-target"), cloneRegistry(t, env, "bump-other")
			for _, r := range []struct {
				clone registryClone
				skill string
			}{{target, "target"}, {other, "other"}} {
				r.clone.publishSkill(r.skill, r.skill+" v1.0", "v1.0")
				r.clone.publishSkill(r.skill, r.skill+" v1.1", "v1.1")
			}
			config := fmt.Sprintf("apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  target: {git: '%s', ref: v1.0}\n  other: {git: '%s', ref: v1.0}\nresources:\n  target: {include: [skill/target]}\n  other: {include: [skill/other]}\n", target.url, other.url)
			if schema == "v2" {
				config = fmt.Sprintf("apiVersion: agent-marketplace/v2\nharnesses: [factory]\nregistries:\n  - url: %s\n    ref: v1.0\n    skills: [target]\n  - url: %s\n    ref: v1.0\n    skills: [other]\n", target.url, other.url)
			}
			env.WriteGlobalConfig("apiVersion: agent-marketplace/" + schema + "\nharnesses: [factory]\n")
			env.WriteRepoConfig(config)
			env.AssertExitCode(env.RunAM("sync"), 0)
			env.AssertFileContains("/workspace/.factory/skills/target/SKILL.md", "target v1.0")

			bump := env.RunAM("update", "--bump", "--scope", "repo", "--registry", target.url, "--ref", "v1.0", "--path", ".", "--kind", "skill", "target")
			if bump.ExitCode != 0 {
				t.Fatalf("targeted bump failed (exit %d):\n%s", bump.ExitCode, bump.Combined)
			}
			got := env.ReadFile("/workspace/.agents/config.yaml")
			if strings.Count(got, "v1.1") != 1 || strings.Count(got, "v1.0") != 1 || !strings.Contains(got, "apiVersion: agent-marketplace/"+schema) {
				t.Fatalf("bump must move only the target source to v1.1 and keep the %s schema:\n%s", schema, got)
			}
			env.AssertFileContains("/workspace/.factory/skills/target/SKILL.md", "target v1.1")
			env.AssertFileContains("/workspace/.factory/skills/other/SKILL.md", "other v1.0")
			env.AssertExitCode(env.RunAM("sync", "--check"), 0)
		})
	}
}

// Config edits never rewrite included fragments, so update --bump leaves a
// ref declared in one untouched, says so, and still succeeds.
func TestUpdateBumpReportsRefsDeclaredInIncludedFiles(t *testing.T) {
	for _, schema := range []string{"v2", "v4"} {
		t.Run(schema, func(t *testing.T) {
			env := newEnv(t)
			env.InitGitRepo()
			included := cloneRegistry(t, env, "bump-included")
			included.publishSkill("target", "target v1.0", "v1.0")
			included.publishSkill("target", "target v1.1", "v1.1")
			env.WriteGlobalConfig("apiVersion: agent-marketplace/" + schema + "\nharnesses: [factory]\n")
			fragment := fmt.Sprintf("sources:\n  target: {git: '%s', ref: v1.0}\nresources:\n  target: {include: [skill/target]}\n", included.url)
			root := "apiVersion: agent-marketplace/v4\ninclude: shared.yaml\n"
			if schema == "v2" {
				fragment = fmt.Sprintf("registries:\n  - url: %s\n    ref: v1.0\n    skills: [target]\n", included.url)
				root = "apiVersion: agent-marketplace/v2\ninclude: shared.yaml\nregistries: []\n"
			}
			env.WriteFile("/workspace/.agents/shared.yaml", fragment)
			env.WriteFile("/workspace/.agents/config.yaml", root)
			env.AssertExitCode(env.RunAM("sync"), 0)

			bump := env.RunAM("update", "--bump")
			if bump.ExitCode != 0 || !strings.Contains(bump.Combined, "declared in an included config file") {
				t.Fatalf("update --bump did not report the included ref (exit %d):\n%s", bump.ExitCode, bump.Combined)
			}
			if got := env.ReadFile("/workspace/.agents/shared.yaml"); got != fragment {
				t.Fatalf("update --bump rewrote the included fragment:\n%s", got)
			}
			if got := env.ReadFile("/workspace/.agents/config.yaml"); got != root {
				t.Fatalf("update --bump changed the root config:\n%s", got)
			}
			env.AssertFileContains("/workspace/.factory/skills/target/SKILL.md", "target v1.0")
			env.AssertExitCode(env.RunAM("sync", "--check"), 0)
		})
	}
}

// lock --upgrade advances only the source a --registry/--ref/--path or a
// --kind/name target selects, and never rewrites the authored config.
func TestTargetedLockUpgradeAdvancesOnlyTheSelectedSource(t *testing.T) {
	for _, schema := range []string{"v2", "v4"} {
		t.Run(schema, func(t *testing.T) {
			env := newEnv(t)
			env.InitGitRepo()
			registry, pinned := cloneRegistry(t, env, "lock-target"), cloneRegistry(t, env, "lock-pinned")
			registry.publishSkill("review", "review r1", "")
			pinned.publishSkill("stable", "stable r1", "")
			config := fmt.Sprintf("apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  tools: {git: '%s', ref: main, path: skills}\n  pinned: {git: '%s', ref: main}\nresources:\n  tools: {include: [skill/review]}\n  pinned: {include: [skill/stable]}\n", registry.url, pinned.url)
			if schema == "v2" {
				config = fmt.Sprintf("apiVersion: agent-marketplace/v2\nharnesses: [factory]\nregistries:\n  - url: %s\n    ref: main\n    path: skills\n    skills: {include: [review]}\n  - url: %s\n    ref: main\n    skills: {include: [stable]}\n", registry.url, pinned.url)
			}
			env.WriteGlobalConfig("apiVersion: agent-marketplace/" + schema + "\nharnesses: [factory]\n")
			env.WriteRepoConfig(config)
			authored := env.ReadFile("/workspace/.agents/config.yaml")
			env.AssertExitCode(env.RunAM("sync"), 0)
			lockBefore := env.ReadFile("/workspace/.agents/lock.yaml")
			stableCommit := lockedCommitFor(t, lockBefore, pinned.url)

			registry.publishSkill("review", "review r2", "")
			pinned.publishSkill("stable", "stable r2", "")
			qualified := env.RunAM("lock", "--scope", "repo", "--upgrade", "--registry", registry.url, "--ref", "main", "--path", "skills")
			if qualified.ExitCode != 0 {
				t.Fatalf("qualified lock upgrade failed (exit %d):\n%s", qualified.ExitCode, qualified.Combined)
			}
			afterQualified := env.ReadFile("/workspace/.agents/lock.yaml")
			if afterQualified == lockBefore || lockedCommitFor(t, afterQualified, pinned.url) != stableCommit {
				t.Fatalf("qualified upgrade must advance only the selected source:\nbefore:\n%s\nafter:\n%s", lockBefore, afterQualified)
			}

			registry.publishSkill("review", "review r3", "")
			named := env.RunAM("lock", "--scope", "repo", "--upgrade", "--registry", registry.url, "--kind", "skill", "review")
			if named.ExitCode != 0 || !strings.Contains(named.Combined, "git target skill/review expands to source") {
				t.Fatalf("named lock upgrade failed or did not report its expansion (exit %d):\n%s", named.ExitCode, named.Combined)
			}
			afterNamed := env.ReadFile("/workspace/.agents/lock.yaml")
			if afterNamed == afterQualified || lockedCommitFor(t, afterNamed, pinned.url) != stableCommit {
				t.Fatalf("named upgrade must advance only the selected source:\nbefore:\n%s\nafter:\n%s", afterQualified, afterNamed)
			}
			if got := env.ReadFile("/workspace/.agents/config.yaml"); got != authored {
				t.Fatalf("lock rewrote the authored config:\n%s", got)
			}
			env.AssertExitCode(env.RunAM("sync"), 0)
			env.AssertFileContains("/workspace/.factory/skills/review/SKILL.md", "review r3")
			env.AssertFileContains("/workspace/.factory/skills/stable/SKILL.md", "stable r1")
			env.AssertExitCode(env.RunAM("sync", "--check"), 0)
		})
	}
}

// lockedCommitFor returns the commit recorded for url in a lock file, which
// stores registry URLs without their .git suffix.
func lockedCommitFor(t *testing.T, lock, url string) string {
	t.Helper()
	_, after, found := strings.Cut(lock, "url: "+strings.TrimSuffix(url, ".git")+"\n")
	if !found {
		t.Fatalf("lock does not record %s:\n%s", url, lock)
	}
	for _, line := range strings.Split(after, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "- url:") {
			break
		}
		if key, value, ok := strings.Cut(trimmed, ":"); ok && key == "commit" {
			return strings.TrimSpace(value)
		}
	}
	t.Fatalf("lock records no commit after %s:\n%s", url, lock)
	return ""
}
