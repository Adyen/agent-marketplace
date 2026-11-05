package e2e_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestBrokenGitChildDoesNotSyncParent(t *testing.T) {
	env := newEnv(t)
	env.InitGitRepo()
	env.WriteGlobalConfig("harnesses: [factory]\n")
	url := env.CreateBareRepo("parent-boundary", SkillsOnly(Skill{Name: "parent-only"}))
	env.WriteRepoConfig(fmt.Sprintf("harnesses: [factory]\nsources: {team: {git: %q, ref: main}}\nresources: {team: {include: [skill/parent-only]}}\n", url))
	child := filepath.Join(env.workspace, "child")
	if err := os.Mkdir(child, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(child, ".git"), []byte("gitdir: missing-metadata\n"), 0600); err != nil {
		t.Fatal(err)
	}
	parentConfig := filepath.Join(env.workspace, ".agents", "config.yaml")
	before, err := pathFingerprint(parentConfig)
	if err != nil {
		t.Fatal(err)
	}
	result := env.RunAMFrom("/workspace/child", "sync")
	if result.ExitCode != 0 {
		t.Fatalf("bounded empty-child sync failed: %+v", result)
	}
	after, err := pathFingerprint(parentConfig)
	if err != nil || after != before {
		t.Fatal("child changed parent config")
	}
	if _, err := os.Lstat(filepath.Join(env.workspace, ".factory")); !os.IsNotExist(err) {
		t.Fatalf("child sync installed into parent: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(env.workspace, ".agents", "lock.yaml")); !os.IsNotExist(err) {
		t.Fatalf("child sync created parent lock: %v", err)
	}
	env.MustRunAM("sync")
	if _, err := os.Stat(filepath.Join(env.workspace, ".factory", "skills", "parent-only", "SKILL.md")); err != nil {
		t.Fatalf("positive parent control did not install: %v", err)
	}
}
