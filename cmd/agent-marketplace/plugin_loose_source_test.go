package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

	origin, working := createBareRepoWithWorkingClone(t, "kit.git")
	if resolved, err := filepath.EvalSymlinks(origin); err == nil {
		origin = resolved
	}
	writeSkill(t, working, "review", "Review baseline", nil)
	writeFile(t, filepath.Join(working, ".claude-plugin", "plugin.json"), `{"name": "kit"}`)
	commitAndPushAll(t, working, "seed")
	commit := gitHead(t, working)

	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nmaterialization: {mode: file}\nsources:\n  kit: {git: file://"+filepath.ToSlash(origin)+", ref: main}\nresources:\n  kit: {all: true}\n")
	withWorkingDir(t, repo)
	if output, err := executeCommand("sync", "--no-backup"); err != nil {
		t.Fatalf("sync of a plugin-shaped source without format must keep working: %v\n%s", err, output)
	}

	// Golden captured from the unchanged loose-ingestion path: a git source
	// without format writes exactly these output bytes and this lock shape.
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

	lockPath := filepath.Join(repo, ".agents", "lock.yaml")
	lockBytes, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("a plugin-free git source must still write its lock: %v", err)
	}
	normalized := strings.ReplaceAll(string(lockBytes), commit, "<commit>")
	for i, line := range strings.Split(normalized, "\n") {
		if strings.HasPrefix(line, "  - url: ") {
			parts := strings.Split(normalized, "\n")
			parts[i] = "  - url: <url>"
			normalized = strings.Join(parts, "\n")
			break
		}
	}
	wantLock := "apiVersion: agent-marketplace-lock/v2\nscope: repo\nregistries:\n  - url: <url>\n    ref: main\n    commit: sha1:<commit>\n    roots:\n      - path: .\n        source_harness: claude\n        revision_schema: v4\n        revision_digest: sha256:"
	if !strings.HasPrefix(normalized, wantLock) {
		t.Fatalf("plugin-free lock gained unexpected bytes:\n%s", normalized)
	}
	if strings.Contains(normalized, "plugin") || strings.Contains(normalized, "plugin-v1") {
		t.Fatalf("plugin-free lock must not carry plugin records:\n%s", normalized)
	}
}

func gitHead(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("git rev-parse: %v", err)
	}
	return strings.TrimSpace(string(out))
}
