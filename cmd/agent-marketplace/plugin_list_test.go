package main

import (
	"bytes"
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

// spec: plugin-delivery/decomposed-skill-gets-a-namespaced-installed-name
func TestSyncSelectedPluginDecomposesMember(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	repo, run := pluginCommandFixture(t, "    - plugin/review-kit\n")
	writeConfig(t, filepath.Join(filepath.Dir(repo), "home", ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [cursor]\n")

	output, err := run("sync", "--no-backup")
	if err != nil {
		t.Fatalf("sync must decompose the selected plugin: %v\n%s", err, output)
	}
	if strings.Contains(output, "plugin.delivery.pending") {
		t.Fatalf("auto delivery must not remain pending:\n%s", output)
	}
	if _, statErr := os.Stat(filepath.Join(repo, ".cursor", "skills", "review-kit-review", "SKILL.md")); statErr != nil {
		t.Fatalf("decomposed member was not installed under its plugin namespace: %v\n%s", statErr, output)
	}
	memberPath := filepath.Join(repo, ".cursor", "skills", "review-kit-review", "SKILL.md")
	if err := os.Remove(memberPath); err != nil {
		t.Fatal(err)
	}
	preview, previewErr := run("sync", "--check")
	if previewErr == nil || !strings.Contains(preview, "review-kit-review") {
		t.Fatalf("sync preview must show the installed namespace: err=%v\n%s", previewErr, preview)
	}
	if _, statErr := os.Lstat(memberPath); !os.IsNotExist(statErr) {
		t.Fatalf("sync --check must not recreate a missing plugin member: %v", statErr)
	}
}

func TestSyncNativePluginUnverifiedFailsWithoutDecomposing(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	repo, run := pluginCommandFixture(t, "    - {kind: plugin, name: review-kit, delivery: native}\n")
	writeConfig(t, filepath.Join(filepath.Dir(repo), "home", ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")

	output, err := run("sync", "--no-backup")
	if err == nil {
		t.Fatalf("sync with unverified native delivery must fail:\n%s", output)
	}
	if !strings.Contains(output, "plugin.delivery.native-unverified") || !strings.Contains(output, "review-kit") {
		t.Fatalf("native delivery must fail as unverified for review-kit:\n%s", output)
	}
	if _, statErr := os.Lstat(filepath.Join(repo, ".factory", "skills", "review")); !os.IsNotExist(statErr) {
		t.Fatalf("native delivery must not decompose plugin members: %v", statErr)
	}
	if _, statErr := os.Lstat(filepath.Join(repo, ".factory", "settings.json")); !os.IsNotExist(statErr) {
		t.Fatalf("native registration remains out of scope: %v", statErr)
	}
}

// spec: plugin-delivery/decomposed-skill-gets-a-namespaced-installed-name
func TestSyncDecomposesPluginMemberForEveryHarness(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	repo, run := pluginCommandFixture(t, "    - {kind: plugin, name: review-kit, delivery: decompose}\n")
	writeConfig(t, filepath.Join(filepath.Dir(repo), "home", ".agents", "config.yaml"),
		"apiVersion: agent-marketplace/v4\nharnesses: [claude, cursor, codex, factory, gemini, windsurf, devin]\n")

	output, err := run("sync", "--no-backup")
	if err != nil {
		t.Fatalf("sync must decompose the selected plugin for all targets: %v\n%s", err, output)
	}
	for _, rel := range []string{
		".claude/skills/review-kit-review/SKILL.md",
		".cursor/skills/review-kit-review/SKILL.md",
		".agents/skills/review-kit-review/SKILL.md",
		".factory/skills/review-kit-review/SKILL.md",
		".gemini/skills/review-kit-review/SKILL.md",
		".windsurf/skills/review-kit-review/SKILL.md",
		".devin/skills/review-kit-review/SKILL.md",
	} {
		if _, err := os.Stat(filepath.Join(repo, filepath.FromSlash(rel))); err != nil {
			t.Errorf("%s member output missing: %v", rel, err)
		}
	}
}

// spec: plugin-delivery/namespace-collision-is-refused
func TestSyncRefusesPluginNamespaceCollisionWithoutChangingOtherResource(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	repo, run := pluginCommandFixture(t, "    - plugin/review-kit\n")
	root := filepath.Dir(repo)
	leafDir := filepath.Join(root, "loose-skills")
	writeSkill(t, leafDir, "review-kit-review", "handwritten", nil)
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"),
		"apiVersion: agent-marketplace/v4\nmaterialization: {mode: file}\nsources:\n  team: {directory: "+filepath.ToSlash(filepath.Join(root, "marketplace"))+", format: claude-marketplace}\n  loose: {directory: "+filepath.ToSlash(leafDir)+"}\nresources:\n  loose:\n    - skill/review-kit-review\n")
	withWorkingDir(t, repo)
	if output, err := run("sync", "--no-backup"); err != nil {
		t.Fatalf("initial independent resource sync failed: %v\n%s", err, output)
	}
	path := filepath.Join(repo, ".claude", "skills", "review-kit-review", "SKILL.md")
	before, readErr := os.ReadFile(path)
	beforeInfo, statErr := os.Stat(path)
	if readErr != nil || statErr != nil {
		t.Fatalf("initial independent resource missing: %v / %v", readErr, statErr)
	}
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"),
		"apiVersion: agent-marketplace/v4\nmaterialization: {mode: file}\nsources:\n  team: {directory: "+filepath.ToSlash(filepath.Join(root, "marketplace"))+", format: claude-marketplace}\n  loose: {directory: "+filepath.ToSlash(leafDir)+"}\nresources:\n  team:\n    - {kind: plugin, name: review-kit, delivery: decompose}\n  loose:\n    - skill/review-kit-review\n")

	output, err := run("sync", "--no-backup")
	if err == nil || !strings.Contains(output, "plugin.namespace-collision") {
		t.Fatalf("sync must refuse the plugin namespace collision: err=%v\n%s", err, output)
	}
	data, readErr := os.ReadFile(path)
	info, statErr := os.Stat(path)
	if readErr != nil || statErr != nil || !bytes.Equal(data, before) || info.Mode() != beforeInfo.Mode() || !info.ModTime().Equal(beforeInfo.ModTime()) {
		t.Fatalf("the colliding selected resource must remain unchanged: read=%v stat=%v before=%s after=%s", readErr, statErr, before, data)
	}
}

func TestSyncRefusesInvalidPluginNamespace(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	repo, _ := pluginCommandFixture(t, "    - plugin/review-kit\n")
	root := filepath.Dir(repo)
	pluginName := strings.Repeat("a", 60)
	catalogDir := filepath.Join(root, "marketplace")
	writeFile(t, filepath.Join(catalogDir, ".claude-plugin", "marketplace.json"),
		`{"name":"team","plugins":[{"name":"`+pluginName+`","source":"./plugins/`+pluginName+`"}]}`)
	writePluginSourceFixture(t, filepath.Join(catalogDir, "plugins", pluginName), ".claude-plugin", pluginName, "review")
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"),
		"apiVersion: agent-marketplace/v4\nmaterialization: {mode: file}\nsources:\n  team: {directory: "+filepath.ToSlash(catalogDir)+", format: claude-marketplace}\nresources:\n  team:\n    - {kind: plugin, name: "+pluginName+", delivery: decompose}\n")
	withWorkingDir(t, repo)

	output, err := executeCommand("sync", "--no-backup")
	if err == nil || !strings.Contains(output, "plugin.namespace-invalid") {
		t.Fatalf("sync must refuse an invalid plugin namespace: err=%v\n%s", err, output)
	}
	if _, statErr := os.Lstat(filepath.Join(repo, ".claude", "skills", pluginName+"-review")); !os.IsNotExist(statErr) {
		t.Fatalf("invalid plugin namespace must not be installed: %v", statErr)
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
