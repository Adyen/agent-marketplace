package main

import (
	"os"
	"path/filepath"
	"testing"
)

const handwrittenGeminiSubagent = "---\nname: worker\ndescription: written by hand\n---\n\n# worker (user-owned)\n"

// syncedGeminiSubagentRepo syncs a repository whose Gemini output holds one
// managed subagent and one managed skill next to a handwritten subagent, and
// returns the Gemini root.
func syncedGeminiSubagentRepo(t *testing.T) string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	registry := filepath.Join(t.TempDir(), "registry")
	writeSubagent(t, registry, "helper", "Helps", []string{"Read"})
	writeSkill(t, registry, "keep", "Kept", nil)

	repo := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [gemini]\nsources:\n  local: {directory: "+registry+"}\nresources:\n  local: {include: [skill/keep, subagent/helper]}\n")
	withWorkingDir(t, repo)

	root := filepath.Join(repo, ".gemini")
	writeFile(t, filepath.Join(root, "agents", "worker.md"), handwrittenGeminiSubagent)
	if out, err := executeCommand("sync"); err != nil {
		t.Fatalf("seed sync: %v\n%s", err, out)
	}
	for _, installed := range []string{filepath.Join(root, "agents", "helper.md"), filepath.Join(root, "skills", "keep")} {
		if _, err := os.Lstat(installed); err != nil {
			t.Fatalf("seed sync did not install %s: %v", installed, err)
		}
	}
	return root
}

func requireHandwrittenGeminiSubagent(t *testing.T, root string) {
	t.Helper()
	got, err := os.ReadFile(filepath.Join(root, "agents", "worker.md"))
	if err != nil || string(got) != handwrittenGeminiSubagent {
		t.Fatalf("handwritten subagent changed: %q, %v", got, err)
	}
}

// spec: materialization/deselected-gemini-subagents-are-removed
func TestRemovingTheLastGeminiSubagentRemovesItsOutput(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	root := syncedGeminiSubagentRepo(t)

	if out, err := executeCommand("remove", "--kind", "subagent", "--scope", "repo", "helper"); err != nil {
		t.Fatalf("remove: %v\n%s", err, out)
	}
	if out, err := executeCommand("sync"); err != nil {
		t.Fatalf("sync: %v\n%s", err, out)
	}

	if _, err := os.Lstat(filepath.Join(root, "agents", "helper.md")); !os.IsNotExist(err) {
		t.Errorf("removed subagent survived the sync (stat = %v)", err)
	}
	if _, err := os.Lstat(filepath.Join(root, "skills", "keep")); err != nil {
		t.Errorf("selected skill was removed with the subagent: %v", err)
	}
	requireHandwrittenGeminiSubagent(t, root)
	if out, err := executeCommand("sync", "--check"); err != nil {
		t.Errorf("check after the removal reports drift: %v\n%s", err, out)
	}
}

// spec: materialization/deinit-removes-gemini-subagents
func TestDeinitRemovesGeminiSubagents(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	root := syncedGeminiSubagentRepo(t)

	if out, err := executeCommand("deinit", "--scope", "repo"); err != nil {
		t.Fatalf("deinit: %v\n%s", err, out)
	}

	for _, removed := range []string{filepath.Join(root, "agents", "helper.md"), filepath.Join(root, "skills", "keep")} {
		if _, err := os.Lstat(removed); !os.IsNotExist(err) {
			t.Errorf("deinit left %s behind (stat = %v)", removed, err)
		}
	}
	requireHandwrittenGeminiSubagent(t, root)
}
