package harnesscompile

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

const supportAssetMode = 0o755

func TestConvertMaterializesClaudePluginSkillAndHookSupportFiles(t *testing.T) {
	source := t.TempDir()
	writeClaudePluginFile(t, source, ".claude-plugin/plugin.json", []byte(`{"name":"support-fixture"}`), 0o644)
	writeClaudePluginFile(t, source, "skills/support-skill/SKILL.md", []byte("---\nname: support-skill\ndescription: support asset fixture\n---\nskill body\n"), 0o644)
	skillAsset := []byte("#!/bin/sh\nprintf 'skill asset\\n'\n")
	hookAsset := []byte("#!/bin/sh\nprintf 'hook asset\\n'\n")
	writeClaudePluginFile(t, source, "skills/support-skill/assets/skill.sh", skillAsset, supportAssetMode)
	writeClaudePluginFile(t, source, "scripts/check.sh", hookAsset, supportAssetMode)
	writeClaudePluginFile(t, source, "hooks/hooks.json", []byte(`{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"${CLAUDE_PLUGIN_ROOT}/scripts/check.sh"}]}]}}`), 0o644)

	output := filepath.Join(t.TempDir(), "materialized")
	_, report, err := convertClaudePlugin(t, source, output, HarnessClaude, WithPluginRoot("/plugins/support-fixture"))
	if err != nil {
		t.Fatalf("convert and materialize Claude plugin: %v", err)
	}
	if report == nil || report.Partial {
		t.Fatalf("materialization report = %#v, want complete report", report)
	}

	assertMaterializedFile(t, output, skillAsset, supportAssetMode)
	assertMaterializedFile(t, output, hookAsset, supportAssetMode)
}

func TestConvertRejectsEscapingClaudeHookSupportSymlink(t *testing.T) {
	source := t.TempDir()
	external := filepath.Join(t.TempDir(), "external.sh")
	externalBytes := []byte("#!/bin/sh\nprintf 'external target\\n'\n")
	if err := os.WriteFile(external, externalBytes, supportAssetMode); err != nil {
		t.Fatalf("write external target: %v", err)
	}
	if err := os.Chmod(external, supportAssetMode); err != nil {
		t.Fatalf("chmod external target: %v", err)
	}

	writeClaudePluginFile(t, source, ".claude-plugin/plugin.json", []byte(`{"name":"unsafe-fixture"}`), 0o644)
	writeClaudePluginFile(t, source, "skills/clean/SKILL.md", []byte("---\nname: clean\ndescription: clean skill\n---\nclean skill body\n"), 0o644)
	writeClaudePluginFile(t, source, "hooks/hooks.json", []byte(`{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"${CLAUDE_PLUGIN_ROOT}/scripts/escape.sh"}]}]}}`), 0o644)
	if err := os.MkdirAll(filepath.Join(source, "scripts"), 0o755); err != nil {
		t.Fatalf("create source scripts directory: %v", err)
	}
	if err := os.Symlink(external, filepath.Join(source, "scripts", "escape.sh")); err != nil {
		t.Fatalf("symlink escaping hook asset: %v", err)
	}

	output := filepath.Join(t.TempDir(), "materialized")
	result, report, err := convertClaudePlugin(t, source, output, HarnessClaude, WithPluginRoot("/plugins/unsafe-fixture"))
	if result == nil {
		t.Fatal("conversion returned no result")
	}
	if !diag.HasCode(result.Diagnostics, "bundle.support-file.unsafe") {
		t.Fatalf("diagnostics = %#v, want bundle.support-file.unsafe", result.Diagnostics)
	}
	if !errors.Is(err, ErrPartialResult) {
		t.Fatalf("materialization error = %v, want ErrPartialResult", err)
	}
	if report == nil || !report.Partial {
		t.Fatalf("materialization report = %#v, want partial report", report)
	}
	assertOutputContains(t, output, []byte("clean skill body"))
	assertOutputHasNoBasenames(t, output, "hooks.json", "escape.sh")

	gotExternal, err := os.ReadFile(external)
	if err != nil {
		t.Fatalf("read external target: %v", err)
	}
	if !bytes.Equal(gotExternal, externalBytes) {
		t.Fatalf("external target bytes = %q, want unchanged %q", gotExternal, externalBytes)
	}
	info, err := os.Stat(external)
	if err != nil {
		t.Fatalf("stat external target: %v", err)
	}
	if got := info.Mode().Perm(); got != supportAssetMode {
		t.Fatalf("external target mode = %04o, want %04o", got, supportAssetMode)
	}
}

func TestConvertWithoutPluginRootDoesNotEmbedClaudeSourcePath(t *testing.T) {
	source := t.TempDir()
	writeClaudePluginFile(t, source, ".claude-plugin/plugin.json", []byte(`{"name":"portable-fixture"}`), 0o644)
	writeClaudePluginFile(t, source, "skills/portable/SKILL.md", []byte("---\nname: portable\ndescription: portable skill\n---\nportable skill body\n"), 0o644)
	skillAsset := []byte("#!/bin/sh\nprintf 'portable skill asset\\n'\n")
	writeClaudePluginFile(t, source, "skills/portable/assets/check.sh", skillAsset, supportAssetMode)

	output := filepath.Join(t.TempDir(), "materialized")
	_, report, err := convertClaudePlugin(t, source, output, HarnessWindsurf)
	if err != nil {
		t.Fatalf("convert and materialize Claude plugin without plugin root: %v", err)
	}
	if report == nil || report.Partial {
		t.Fatalf("materialization report = %#v, want complete report", report)
	}
	assertMaterializedFile(t, output, skillAsset, supportAssetMode)
	assertOutputDoesNotContain(t, output, source)
}

func convertClaudePlugin(t *testing.T, source, output string, target Harness, opts ...Option) (*Result, *WriteReport, error) {
	t.Helper()
	root, err := os.OpenRoot(source)
	if err != nil {
		t.Fatalf("open Claude plugin source root: %v", err)
	}
	defer func() {
		if err := root.Close(); err != nil {
			t.Errorf("close Claude plugin source root: %v", err)
		}
	}()

	result, err := Convert(root.FS(), HarnessClaude, target, opts...)
	if err != nil {
		t.Fatalf("Convert(Claude, %s): %v", target, err)
	}
	report, materializeErr := materializeResult(t, result, output)
	return result, report, materializeErr
}

func materializeResult(t *testing.T, result *Result, output string) (*WriteReport, error) {
	t.Helper()
	report, err := Materialize(result, output, WithWriteMode(WriteModeOverwrite))
	return report, err
}

func writeClaudePluginFile(t *testing.T, root, name string, content []byte, mode fs.FileMode) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create parent for %s: %v", name, err)
	}
	if err := os.WriteFile(path, content, mode); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatalf("chmod %s: %v", name, err)
	}
}

func assertMaterializedFile(t *testing.T, root string, content []byte, mode fs.FileMode) {
	t.Helper()
	matches := findMaterializedFiles(t, root, func(_ string, data []byte) bool {
		return bytes.Equal(data, content)
	})
	if len(matches) != 1 {
		t.Fatalf("materialized files with exact content %q = %v, want one", content, matches)
	}
	if got := matches[0].mode.Perm(); got != mode {
		t.Fatalf("materialized file %q mode = %04o, want %04o", matches[0].path, got, mode)
	}
}

func assertOutputContains(t *testing.T, root string, content []byte) {
	t.Helper()
	matches := findMaterializedFiles(t, root, func(_ string, data []byte) bool {
		return bytes.Contains(data, content)
	})
	if len(matches) == 0 {
		t.Fatalf("no materialized file contains %q", content)
	}
}

func assertOutputHasNoBasenames(t *testing.T, root string, basenames ...string) {
	t.Helper()
	denied := make(map[string]struct{}, len(basenames))
	for _, basename := range basenames {
		denied[basename] = struct{}{}
	}
	for _, file := range findMaterializedFiles(t, root, func(string, []byte) bool { return true }) {
		if _, exists := denied[filepath.Base(file.path)]; exists {
			t.Errorf("unexpected materialized file %q", file.path)
		}
	}
}

func assertOutputDoesNotContain(t *testing.T, root, forbidden string) {
	t.Helper()
	for _, file := range findMaterializedFiles(t, root, func(string, []byte) bool { return true }) {
		if strings.Contains(file.path, forbidden) || strings.Contains(string(file.data), forbidden) {
			t.Errorf("materialized output %q embeds source path %q", file.path, forbidden)
		}
	}
}

type materializedFile struct {
	path string
	data []byte
	mode fs.FileMode
}

func findMaterializedFiles(t *testing.T, root string, match func(string, []byte) bool) []materializedFile {
	t.Helper()
	var files []materializedFile
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if match(relative, data) {
			files = append(files, materializedFile{path: relative, data: data, mode: info.Mode()})
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk materialized output %q: %v", root, err)
	}
	return files
}
