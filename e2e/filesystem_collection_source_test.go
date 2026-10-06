package e2e_test

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFilesystemCollectionSourceRefusalKeepsNativeGeneration(t *testing.T) {
	env := newEnv(t)
	env.InitGitRepo()
	env.MustRunAM("init", "--harnesses", "factory")
	root := filepath.Join(env.workspace, "vendor", "registry")
	env.WriteFile(filepath.Join(root, "skills", "collection-probe", "SKILL.md"), "---\nname: collection-probe\ndescription: collection control\n---\n\ninert native body\n")
	definition := filepath.Join(root, "collections", "bundle", "COLLECTION.yaml")
	env.WriteFile(definition, "name: bundle\nresources:\n  - name: collection-probe\n")
	env.MustRunAM("install", "bundle", "--kind", "collection", "--from", "./vendor/registry", "--scope", "repo")
	env.MustRunAM("sync", "--check")
	installed := filepath.Join(env.workspace, ".factory", "skills", "collection-probe", "SKILL.md")
	before := captureDest(t, installed)
	foreign := filepath.Join(env.home, "foreign-collection.yaml")
	env.WriteFile(foreign, "name: bundle\nresources: []\n")
	foreignBefore := captureDest(t, foreign)
	if err := os.Rename(definition, filepath.Join(root, "retained-collection.yaml")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(foreign, definition); err != nil {
		t.Fatal(err)
	}
	result := env.RunAM("sync")
	env.AssertExitCode(result, 1)
	requireDestUntouched(t, before, captureDest(t, installed))
	requireDestUntouched(t, foreignBefore, captureDest(t, foreign))
}
