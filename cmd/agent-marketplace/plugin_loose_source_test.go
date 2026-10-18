package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
)

// spec: plugin-packages/plugin-shaped-source-without-format-stays-loose
func TestPluginShapedSourceWithoutFormatStaysLoose(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	root := canonicalTempDir(t)
	home, repo := filepath.Join(root, "home"), filepath.Join(root, "repo")
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	config.SetTestOrganizationConfigPath(t, filepath.Join(root, "missing-org.yaml"))
	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")

	source := filepath.Join(root, "kit")
	writeSkill(t, source, "review", "Review baseline", nil)
	writeFile(t, filepath.Join(source, ".claude-plugin", "plugin.json"), `{"name": "kit"}`)

	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nmaterialization: {mode: file}\nsources:\n  kit: {directory: "+source+"}\nresources:\n  kit: {all: true}\n")
	withWorkingDir(t, repo)
	if output, err := executeCommand("sync", "--no-backup"); err != nil {
		t.Fatalf("sync of a plugin-shaped source without format must keep working: %v\n%s", err, output)
	}

	// Golden captured from the unchanged loose-ingestion path: a directory
	// source without format writes exactly these bytes and no lock.
	want := map[string]string{
		".agents/materialization-manifest.yaml": "version: 1\nentries:\n    - target: factory\n      path: skills/review\n      digest: sha256:0fe8e4e5c5366c968b5a8f4cf1cea2720b370486742732a45da563ce1f98ce76\n",
		".factory/skills/review/SKILL.md":       "---\nname: review\ndescription: Review baseline\nmetadata:\n  tags: null\n---\n\n# review\n",
	}
	for rel, expected := range want {
		got, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("expected loose output %s: %v", rel, err)
		}
		if string(got) != expected {
			t.Fatalf("loose output %s changed:\n%s", rel, got)
		}
	}
	if _, err := os.Lstat(filepath.Join(repo, ".agents", "lock.yaml")); !os.IsNotExist(err) {
		t.Fatalf("a plugin-free directory source gained lock bytes: %v", err)
	}
}
