package materialize

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// spec: materialization/support-copy-refuses-a-source-link-outside-its-root
func TestRunSupportCopyRefusesEscapingSourceLink(t *testing.T) {
	home, repo, source, external := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("inert compiled skill"), 0o644); err != nil {
		t.Fatal(err)
	}
	payload := filepath.Join(external, "private.txt")
	if err := os.WriteFile(payload, []byte("foreign inert bytes"), 0o640); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(payload, filepath.Join(source, "reference.txt")); err != nil {
		t.Fatal(err)
	}
	resolved := []resolver.ResolvedArtifact{{Entry: catalog.Entry{Name: "source-probe", Path: source, Resource: ir.NewSkill("source-probe", "Source control", "inert body", nil, nil, ir.SourceRef{})}, Source: config.RepoCommitted, Harnesses: []hcc.Target{hcc.TargetFactory}}}
	report, err := Run(t.Context(), repo, resolved)
	if err != nil {
		t.Fatal(err)
	}
	if !hasDiagCode(report.Diags, "materialize.write.error") {
		t.Fatalf("support source escape did not refuse at cache preparation: %+v", report.Diags)
	}
	if _, err := os.Lstat(filepath.Join(repo, ".factory", "skills", "source-probe")); !os.IsNotExist(err) {
		t.Fatalf("refused source was published: %v", err)
	}
	if raw, err := os.ReadFile(payload); err != nil || string(raw) != "foreign inert bytes" {
		t.Fatalf("source refusal changed external bytes: %q %v", raw, err)
	}
	if after, err := os.Stat(payload); err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("source refusal changed external metadata: %v", err)
	}
}

// spec: materialization/support-copy-keeps-contained-relative-alias-content-and-mode
func TestRunSupportCopyKeepsContainedAliasAndMode(t *testing.T) {
	home, repo, source := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	if err := os.MkdirAll(filepath.Join(source, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	payload := filepath.Join(source, "scripts", "helper.txt")
	if err := os.WriteFile(payload, []byte("inert executable data"), 0o755); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("scripts/helper.txt", filepath.Join(source, "reference.txt")); err != nil {
		t.Fatal(err)
	}
	resolved := []resolver.ResolvedArtifact{{Entry: catalog.Entry{Name: "source-probe", Path: source, Resource: ir.NewSkill("source-probe", "Source control", "inert body", nil, nil, ir.SourceRef{})}, Source: config.RepoCommitted, Harnesses: []hcc.Target{hcc.TargetFactory}}}
	report, err := Run(t.Context(), repo, resolved)
	if err != nil || hasDiagCode(report.Diags, "materialize.write.error") {
		t.Fatalf("contained support source failed: %+v %v", report, err)
	}
	installed := filepath.Join(repo, ".factory", "skills", "source-probe", "reference.txt")
	if raw, err := os.ReadFile(installed); err != nil || string(raw) != "inert executable data" {
		t.Fatalf("contained support payload missing: %q %v", raw, err)
	}
	info, err := os.Stat(installed)
	if err != nil || info.Mode().Perm() != 0o755 {
		t.Fatalf("support mode changed: %v", err)
	}
	if after, err := os.Stat(payload); err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("contained source changed: %v", err)
	}
	second, err := Run(t.Context(), repo, resolved)
	if err != nil || hasDiagCode(second.Diags, "materialize.write.error") {
		t.Fatalf("contained repeat failed: %+v %v", second, err)
	}
	if after, err := os.Stat(installed); err != nil || after.Mode().Perm() != info.Mode().Perm() {
		t.Fatalf("support repeat changed permissions: %v", err)
	}
	if raw, err := os.ReadFile(installed); err != nil || string(raw) != "inert executable data" {
		t.Fatalf("support repeat changed payload: %q %v", raw, err)
	}
	if after, err := os.Stat(payload); err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("support repeat changed source: %v", err)
	}
}

// spec: materialization/support-source-refusal-preserves-an-installed-cache-generation
func TestRunSupportSourceRefusalPreservesInstalledGeneration(t *testing.T) {
	home, repo, source, external := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	if err := os.WriteFile(filepath.Join(source, "reference.txt"), []byte("safe original support"), 0o644); err != nil {
		t.Fatal(err)
	}
	resolved := []resolver.ResolvedArtifact{{Entry: catalog.Entry{Name: "source-probe", Path: source, Resource: ir.NewSkill("source-probe", "Source control", "original body", nil, nil, ir.SourceRef{})}, Source: config.RepoCommitted, Harnesses: []hcc.Target{hcc.TargetFactory}}}
	initial, err := Run(t.Context(), repo, resolved)
	if err != nil || hasDiagCode(initial.Diags, "materialize.write.error") {
		t.Fatalf("valid initial skill missing: %+v %v", initial, err)
	}
	installed := filepath.Join(repo, ".factory", "skills", "source-probe")
	before, err := os.ReadFile(filepath.Join(installed, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(external, "payload.txt"), []byte("foreign inert bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(external, "payload.txt"), filepath.Join(source, "bad.txt")); err != nil {
		t.Fatal(err)
	}
	resolved[0].Resource = ir.NewSkill("source-probe", "Source control", "changed body", nil, nil, ir.SourceRef{})
	report, err := Run(t.Context(), repo, resolved)
	if err != nil || !hasDiagCode(report.Diags, "materialize.write.error") {
		t.Fatalf("bad source did not refuse: %+v %v", report, err)
	}
	if after, err := os.ReadFile(filepath.Join(installed, "SKILL.md")); err != nil || !bytes.Equal(before, after) {
		t.Fatalf("source refusal replaced installed compiled bytes: %q %v", after, err)
	}
	if after, err := os.ReadFile(filepath.Join(installed, "reference.txt")); err != nil || string(after) != "safe original support" {
		t.Fatalf("source refusal pruned installed support: %q %v", after, err)
	}
	if _, err := os.Lstat(filepath.Join(installed, "bad.txt")); !os.IsNotExist(err) {
		t.Fatalf("bad source materialized: %v", err)
	}
	cacheDir, err := filepath.EvalSymlinks(installed)
	if err != nil {
		t.Fatal(err)
	}
	stages, err := filepath.Glob(filepath.Join(filepath.Dir(cacheDir), ".support-staging-*"))
	if err != nil || len(stages) != 0 {
		t.Fatalf("source refusal left attempt staging: %v %v", stages, err)
	}
}

func TestEmptySupportSourceDoesNotCaptureWorkingDirectory(t *testing.T) {
	for _, missingOK := range []bool{true, false} {
		t.Run(map[bool]string{true: "optional", false: "required"}[missingOK], func(t *testing.T) {
			cwd, destination := t.TempDir(), t.TempDir()
			t.Chdir(cwd)
			if err := os.WriteFile(filepath.Join(cwd, "inert.txt"), []byte("not a support source"), 0o644); err != nil {
				t.Fatal(err)
			}
			err := copySupportFilesExcluding("", destination, nil, "", missingOK)
			if missingOK && err != nil || !missingOK && !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("empty source changed missing contract: %v", err)
			}
			if entries, err := os.ReadDir(destination); err != nil || len(entries) != 0 {
				t.Fatalf("empty source imported cwd: %v %v", entries, err)
			}
		})
	}
}
