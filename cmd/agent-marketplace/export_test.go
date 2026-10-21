package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func lockedPluginExportFixture(t *testing.T, skill string, withHook bool, configure ...func(string)) (root, repo string, run func(...string) (string, error)) {
	t.Helper()
	root = pluginLockFixture(t)
	origin, working := createBareRepoWithWorkingClone(t, "plugin-export.git")
	writeCatalogRepo(t, working, "")
	pluginDir := filepath.Join(working, "plugins", "local-kit")
	if skill != "" {
		writeFile(t, filepath.Join(pluginDir, "skills", "local-skill", "SKILL.md"), skill)
	}
	if withHook {
		writeFile(t, filepath.Join(pluginDir, "hooks", "hooks.json"),
			`{"PreToolUse":[{"matcher":"*","hooks":[{"type":"command","command":"echo check"}]}]}`)
	}
	for _, configurePackage := range configure {
		configurePackage(pluginDir)
	}
	commitAndPushAll(t, working, "seed export plugin")

	repo = filepath.Join(root, "repo")
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"),
		"apiVersion: agent-marketplace/v4\nmaterialization: {mode: file}\nsources:\n  team: {git: file://"+
			filepath.ToSlash(origin)+", ref: main, format: claude-marketplace}\nresources:\n  team:\n    - plugin/local-kit\n")
	withWorkingDir(t, repo)
	run = func(args ...string) (string, error) { return executeCommand(args...) }
	if output, err := run("sync", "--no-backup"); err != nil {
		t.Fatalf("lock export fixture plugin: %v\n%s", err, output)
	}
	return root, repo, run
}

// spec: plugin-portable-format/export-with-a-loss-writes-nothing
func TestExportPluginLossDefaultLeavesOutputAbsent(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	root, _, run := lockedPluginExportFixture(t, "", true)
	out := filepath.Join(root, "exported")
	output, err := run("export", "plugin/local-kit", "--format", "agent-plugin", "--out", out)
	if err == nil || !strings.Contains(output+err.Error(), "plugin.export.loss") {
		t.Fatalf("lossy export error = %v\n%s", err, output)
	}
	if !strings.Contains(output, "LOSS:") || strings.Contains(output, "OMITTED:") {
		t.Fatalf("failed export used incorrect loss wording: %s", output)
	}
	if _, statErr := os.Lstat(out); !os.IsNotExist(statErr) {
		t.Fatalf("failed export left --out present: %v", statErr)
	}
	assertNoExportStaging(t, root)
}

// spec: plugin-portable-format/allow-loss-export-omits-and-reports-non-security-members
func TestExportPluginAllowsLossAndReportsOmittedHook(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	root, _, run := lockedPluginExportFixture(t, "", true)
	out := filepath.Join(root, "exported")
	if err := os.Mkdir(out, 0o755); err != nil {
		t.Fatal(err)
	}
	output, err := run("export", "plugin/local-kit", "--format", "agent-plugin", "--out", out, "--allow-loss")
	if err != nil {
		t.Fatalf("allow-loss export: %v\n%s", err, output)
	}
	if !strings.Contains(output, "OMITTED:") || strings.Contains(output, "LOSS:") ||
		!strings.Contains(output, "hook") || !strings.Contains(output, "PreToolUse") {
		t.Fatalf("omitted hook was not reported: %s", output)
	}
	if _, err := os.Stat(filepath.Join(out, "plugin.json")); err != nil {
		t.Fatalf("exported manifest missing: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(out, "hooks", "hooks.json")); !os.IsNotExist(err) {
		t.Fatalf("omitted hook definition remains in package: %v", err)
	}
	assertNoExportStaging(t, root)
}

func assertNoExportStaging(t *testing.T, parent string) {
	t.Helper()
	staging, err := filepath.Glob(filepath.Join(parent, ".agent-marketplace-export-*"))
	if err != nil || len(staging) != 0 {
		t.Fatalf("export staging residue = %v, %v", staging, err)
	}
}

// spec: plugin-portable-format/allow-loss-cannot-drop-a-restriction
func TestExportPluginAllowLossCannotDropSkillRestriction(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	skill := "---\nname: local-skill\ndescription: Local skill\ntriggers: [user]\n---\nBody\n"
	root, _, run := lockedPluginExportFixture(t, skill, false)
	out := filepath.Join(root, "exported")
	output, err := run("export", "plugin/local-kit", "--format", "agent-plugin", "--out", out, "--allow-loss")
	if err == nil || !strings.Contains(output+err.Error(), "plugin.export.restriction") {
		t.Fatalf("restriction was dropped: %v\n%s", err, output)
	}
	if _, statErr := os.Lstat(out); !os.IsNotExist(statErr) {
		t.Fatalf("restriction refusal left --out present: %v", statErr)
	}
}

// spec: plugin-portable-format/export-into-a-non-empty-directory-is-refused
func TestExportPluginRefusesNonEmptyOutputDirectory(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	root, _, run := lockedPluginExportFixture(t, "", false)
	out := filepath.Join(root, "existing")
	if err := os.Mkdir(out, 0o755); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(out, "keep.txt")
	writeFile(t, sentinel, "unchanged")
	output, err := run("export", "plugin/local-kit", "--format", "agent-plugin", "--out", out)
	if err == nil || !strings.Contains(output+err.Error(), "plugin.export.out-not-empty") {
		t.Fatalf("non-empty output error = %v\n%s", err, output)
	}
	got, readErr := os.ReadFile(sentinel)
	if readErr != nil || string(got) != "unchanged" {
		t.Fatalf("non-empty output was changed: %q, %v", got, readErr)
	}
}

func TestExportPluginRefusesUnlockedMissingAmbiguousAndNonDirectoryTargets(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	t.Run("unlocked", func(t *testing.T) {
		_, run := pluginCommandFixture(t, "    - plugin/review-kit\n")
		out := filepath.Join(t.TempDir(), "exported")
		output, err := run("export", "plugin/review-kit", "--format", "agent-plugin", "--out", out)
		if err == nil {
			t.Fatalf("unlocked plugin export succeeded:\n%s", output)
		}
		if !strings.Contains(output+err.Error(), "lock") && !strings.Contains(output+err.Error(), "locked") {
			t.Fatalf("unlocked refusal did not identify missing lock: %v\n%s", err, output)
		}
		if _, statErr := os.Lstat(out); !os.IsNotExist(statErr) {
			t.Fatalf("unlocked refusal wrote output: %v", statErr)
		}
	})
	t.Run("missing", func(t *testing.T) {
		root, _, run := lockedPluginExportFixture(t, "", false)
		out := filepath.Join(root, "missing")
		output, err := run("export", "plugin/absent", "--format", "agent-plugin", "--out", out)
		if err == nil || !strings.Contains(output+err.Error(), "not found") {
			t.Fatalf("missing plugin error = %v\n%s", err, output)
		}
		if _, statErr := os.Lstat(out); !os.IsNotExist(statErr) {
			t.Fatalf("missing-plugin refusal wrote output: %v", statErr)
		}
	})
	t.Run("ambiguous", func(t *testing.T) {
		repo, run := pluginCommandFixture(t, "    include: []\n")
		root := filepath.Dir(repo)
		catalogDir := filepath.Join(root, "marketplace")
		secondCatalog := filepath.Join(root, "marketplace-two")
		if err := os.MkdirAll(filepath.Join(secondCatalog, ".claude-plugin"), 0o755); err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(secondCatalog, ".claude-plugin", "marketplace.json"),
			`{"name":"team-two","plugins":[{"name":"review-kit","source":"./plugins/review-kit"}]}`)
		writePluginSourceFixture(t, filepath.Join(secondCatalog, "plugins", "review-kit"), ".claude-plugin", "review-kit", "review")
		writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"),
			"apiVersion: agent-marketplace/v4\nsources:\n  first: {directory: "+filepath.ToSlash(catalogDir)+", format: claude-marketplace}\n  second: {directory: "+filepath.ToSlash(secondCatalog)+", format: claude-marketplace}\nresources:\n  first: {include: []}\n  second: {include: []}\n")
		out := filepath.Join(t.TempDir(), "exported")
		output, err := run("export", "plugin/review-kit", "--format", "agent-plugin", "--out", out)
		if err == nil || !strings.Contains(output+err.Error(), "multiple configured registries") {
			t.Fatalf("ambiguous plugin error = %v\n%s", err, output)
		}
		if _, statErr := os.Lstat(out); !os.IsNotExist(statErr) {
			t.Fatalf("ambiguous-plugin refusal wrote output: %v", statErr)
		}
	})
	root, _, run := lockedPluginExportFixture(t, "", false)
	out := filepath.Join(root, "regular-file")
	writeFile(t, out, "preserve")
	target := filepath.Join(root, "target")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(root, "linked")
	if err := os.Symlink(target, linked); err != nil {
		t.Fatal(err)
	}
	for _, outputPath := range []string{out, linked} {
		output, err := run("export", "plugin/local-kit", "--format", "agent-plugin", "--out", outputPath)
		if err == nil {
			t.Fatalf("non-directory output %q succeeded:\n%s", outputPath, output)
		}
	}
	if got, err := os.ReadFile(out); err != nil || string(got) != "preserve" {
		t.Fatalf("non-directory output changed: %q, %v", got, err)
	}
	if info, err := os.Stat(target); err != nil || !info.IsDir() {
		t.Fatalf("symlink target changed: %v, %v", info, err)
	}
}

func TestExportPluginRejectsUnsupportedFormatBeforeWriting(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	root, _, run := lockedPluginExportFixture(t, "", false)
	out := filepath.Join(root, "unsupported")
	output, err := run("export", "plugin/local-kit", "--format", "claude-plugin", "--out", out)
	if err == nil || !strings.Contains(output+err.Error(), "plugin.format.unsupported") {
		t.Fatalf("unsupported format error = %v\n%s", err, output)
	}
	if _, statErr := os.Lstat(out); !os.IsNotExist(statErr) {
		t.Fatalf("unsupported format wrote output: %v", statErr)
	}
}
