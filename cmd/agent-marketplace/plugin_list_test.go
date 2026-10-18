package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
)

func writePluginSourceFixture(t *testing.T, dir, manifestDir, name, member string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, manifestDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, manifestDir, "plugin.json"), []byte(`{"name": "`+name+`"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	skillDir := filepath.Join(dir, "skills", member)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: "+member+"\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func pluginCommandFixture(t *testing.T, resources string) (repo string, run func(args ...string) (string, error)) {
	t.Helper()
	root := canonicalTempDir(t)
	home, repo := filepath.Join(root, "home"), filepath.Join(root, "repo")
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	config.SetTestOrganizationConfigPath(t, filepath.Join(root, "missing-org.yaml"))
	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [claude]\n")

	catalogDir := filepath.Join(root, "marketplace")
	if err := os.MkdirAll(filepath.Join(catalogDir, ".claude-plugin"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"name":"team","plugins":[` +
		`{"name":"review-kit","source":"./plugins/review-kit"},` +
		`{"name":"npm-kit","source":{"source":"npm","package":"npm-kit"}}]}`
	if err := os.WriteFile(filepath.Join(catalogDir, ".claude-plugin", "marketplace.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	writePluginSourceFixture(t, filepath.Join(catalogDir, "plugins", "review-kit"), ".claude-plugin", "review-kit", "review")

	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nmaterialization: {mode: file}\nsources:\n  team: {directory: "+filepath.ToSlash(catalogDir)+", format: claude-marketplace}\nresources:\n  team:\n"+resources)
	withWorkingDir(t, repo)
	return repo, func(args ...string) (string, error) {
		return executeCommand(args...)
	}
}

// spec: plugin-packages/refused-entry-source-type-blocks-only-that-entry
func TestListShowsPluginEntriesIncludingRefused(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	_, run := pluginCommandFixture(t, "    - plugin/review-kit\n")

	output, err := run("list")
	if err != nil {
		t.Fatalf("list failed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "review-kit") || !strings.Contains(output, "npm-kit") {
		t.Fatalf("list must show both plugin entries:\n%s", output)
	}
	if !strings.Contains(output, "plugin.catalog.source-unsupported") {
		t.Fatalf("refused entry must show its diagnostic:\n%s", output)
	}
	if !strings.Contains(output, "plugin") {
		t.Fatalf("plugin entries must show the plugin kind:\n%s", output)
	}
}

func TestSyncSelectedPluginSkipsDeliveryWithPendingWarning(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	repo, run := pluginCommandFixture(t, "    - plugin/review-kit\n")

	output, err := run("sync", "--no-backup")
	if err != nil {
		t.Fatalf("sync must deliver everything else while plugin delivery is pending: %v\n%s", err, output)
	}
	if !strings.Contains(output, "plugin.delivery.pending") || !strings.Contains(output, "review-kit") {
		t.Fatalf("sync must warn plugin.delivery.pending naming the plugin:\n%s", output)
	}
	if _, statErr := os.Lstat(filepath.Join(repo, ".claude", "skills", "review")); !os.IsNotExist(statErr) {
		var names []string
		_ = filepath.Walk(repo, func(p string, _ os.FileInfo, _ error) error {
			names = append(names, p)
			return nil
		})
		t.Fatalf("plugin members must not be delivered: %v\n%s", statErr, strings.Join(names, "\n"))
	}
}

// spec: plugin-packages/structured-entry-carries-a-name-the-locator-cannot
// (editor half proven by TestV4EditorKeepsExistingStructuredEntry in internal/config)
func TestListSelectsStructuredPluginEntry(t *testing.T) {
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
	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [claude]\n")

	source := filepath.Join(root, "portable")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "plugin.json"), []byte(`{"$schema": "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json", "name": "1password.tools"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	skillDir := filepath.Join(source, "skills", "vault")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: vault\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nmaterialization: {mode: file}\nsources:\n  portable: {directory: "+filepath.ToSlash(source)+", format: agent-plugin}\nresources:\n  portable:\n    - {kind: plugin, name: 1password.tools}\n")
	withWorkingDir(t, repo)

	output, err := executeCommand("list")
	if err != nil {
		t.Fatalf("list failed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "1password.tools") || !strings.Contains(output, "enabled") {
		t.Fatalf("structured plugin entry must resolve and list as enabled:\n%s", output)
	}
}
