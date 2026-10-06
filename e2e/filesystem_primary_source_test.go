package e2e_test

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFilesystemPrimarySourceRefusalKeepsNativeGeneration(t *testing.T) {
	env := newEnv(t)
	env.InitGitRepo()
	env.MustRunAM("init", "--harnesses", "factory")
	root := filepath.Join(env.workspace, "vendor", "registry")
	source := filepath.Join(root, "skills", "primary-probe", "SKILL.md")
	env.WriteFile(source, "---\nname: primary-probe\ndescription: primary control\n---\n\ninert native body\n")
	env.MustRunAM("install", "primary-probe", "--from", "./vendor/registry", "--scope", "repo")
	env.MustRunAM("sync", "--check")
	installed := filepath.Join(env.workspace, ".factory", "skills", "primary-probe", "SKILL.md")
	before := captureDest(t, installed)
	foreign := filepath.Join(env.home, "foreign-primary.md")
	env.WriteFile(foreign, "---\nname: primary-probe\ndescription: foreign control\n---\n\nforeign inert body\n")
	foreignBefore := captureDest(t, foreign)
	if err := os.Rename(source, filepath.Join(root, "retained-primary.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(foreign, source); err != nil {
		t.Fatal(err)
	}
	result := env.RunAM("sync")
	env.AssertExitCode(result, 1)
	requireDestUntouched(t, before, captureDest(t, installed))
	requireDestUntouched(t, foreignBefore, captureDest(t, foreign))
}
