package materialize

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// spec: materialization/hook-support-source-refusal-preserves-installed-native-content
func TestRunHookSupportSourceRefusalPreservesNativeContent(t *testing.T) {
	home, repo, source, outside := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	if err := os.WriteFile(filepath.Join(source, "helper.txt"), []byte("inert hook support"), 0o755); err != nil {
		t.Fatal(err)
	}
	hook := ir.NewHookWithMatcher("support-probe", "PostToolUse", "Edit", "inert original command", "command", nil, ir.SourceRef{File: filepath.Join(source, "hooks.json")})
	resolved := []resolver.ResolvedArtifact{{Entry: catalog.Entry{Name: "support-probe", Kind: ir.KindHook, Resource: hook, Path: filepath.Join(source, "hooks.json")}, Source: config.RepoCommitted, Harnesses: []hcc.Target{hcc.TargetFactory}}}
	initial, err := Run(t.Context(), repo, resolved)
	if err != nil || diag.HasErrors(initial.Diags) {
		t.Fatalf("native initial control failed: %+v %v", initial, err)
	}
	installed := filepath.Join(repo, ".factory", "hooks")
	before, err := os.ReadFile(filepath.Join(installed, "hooks.json"))
	if err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(installed, "helper.txt")); err != nil || string(data) != "inert hook support" {
		t.Fatalf("support positive control missing: %q %v", data, err)
	}
	payload := filepath.Join(outside, "private.txt")
	if err := os.WriteFile(payload, []byte("foreign inert bytes"), 0o640); err != nil {
		t.Fatal(err)
	}
	identity, err := os.Stat(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(payload, filepath.Join(source, "bad.txt")); err != nil {
		t.Fatal(err)
	}
	resolved[0].Resource = ir.NewHookWithMatcher("support-probe", "PostToolUse", "Edit", "inert changed command", "command", nil, ir.SourceRef{File: filepath.Join(source, "hooks.json")})
	report, err := Run(t.Context(), repo, resolved)
	if err != nil || !hasDiagCode(report.Diags, "materialize.hook.support-refused") {
		t.Fatalf("foreign hook source did not refuse: %+v %v", report, err)
	}
	if data, err := os.ReadFile(filepath.Join(installed, "hooks.json")); err != nil || !bytes.Equal(before, data) {
		t.Fatalf("source refusal changed native hook: %q %v", data, err)
	}
	if data, err := os.ReadFile(filepath.Join(installed, "helper.txt")); err != nil || string(data) != "inert hook support" {
		t.Fatalf("source refusal pruned native support: %q %v", data, err)
	}
	if _, err := os.Lstat(filepath.Join(installed, "bad.txt")); !os.IsNotExist(err) {
		t.Fatalf("foreign support published: %v", err)
	}
	if data, err := os.ReadFile(payload); err != nil || string(data) != "foreign inert bytes" {
		t.Fatalf("foreign payload changed: %q %v", data, err)
	}
	if after, err := os.Stat(payload); err != nil || !os.SameFile(identity, after) || after.Mode() != identity.Mode() || !after.ModTime().Equal(identity.ModTime()) {
		t.Fatalf("foreign metadata changed: %v", err)
	}
}

// spec: materialization/contained-hook-support-alias-keeps-native-content-and-mode
func TestRunContainedHookSupportAliasKeepsNativeContentAndMode(t *testing.T) {
	home, repo, source := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	payload := filepath.Join(source, "helper.txt")
	if err := os.WriteFile(payload, []byte("inert contained hook data"), 0o755); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("helper.txt", filepath.Join(source, "alias.txt")); err != nil {
		t.Fatal(err)
	}
	hook := ir.NewHookWithMatcher("support-probe", "PostToolUse", "Edit", "inert hook command", "command", nil, ir.SourceRef{File: filepath.Join(source, "hooks.json")})
	resolved := []resolver.ResolvedArtifact{{Entry: catalog.Entry{Name: "support-probe", Kind: ir.KindHook, Resource: hook, Path: filepath.Join(source, "hooks.json")}, Source: config.RepoCommitted, Harnesses: []hcc.Target{hcc.TargetFactory}}}
	report, err := Run(t.Context(), repo, resolved)
	if err != nil || diag.HasErrors(report.Diags) {
		t.Fatalf("contained alias refused: %+v %v", report, err)
	}
	installed := filepath.Join(repo, ".factory", "hooks", "alias.txt")
	if data, err := os.ReadFile(installed); err != nil || string(data) != "inert contained hook data" {
		t.Fatalf("contained hook alias missing: %q %v", data, err)
	}
	info, err := os.Stat(installed)
	if err != nil || info.Mode().Perm() != 0o755 {
		t.Fatalf("contained hook alias mode changed: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(repo, ".factory", "hooks", "hooks.json")); err != nil || !bytes.Contains(data, []byte("inert hook command")) {
		t.Fatalf("native hook control missing: %q %v", data, err)
	}
	second, err := Run(t.Context(), repo, resolved)
	if err != nil || diag.HasErrors(second.Diags) {
		t.Fatalf("contained repeat failed: %+v %v", second, err)
	}
	if after, err := os.Stat(installed); err != nil || !os.SameFile(info, after) || !info.ModTime().Equal(after.ModTime()) {
		t.Fatalf("contained support repeat rewrote cache: %v", err)
	}
	if after, err := os.Stat(payload); err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("contained source changed: %v", err)
	}
}

// spec: materialization/hook-source-refusal-keeps-independent-valid-native-work
func TestRunHookSourceRefusalKeepsIndependentNativeWork(t *testing.T) {
	home, repo, badSource, goodSource, outside := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	if err := os.WriteFile(filepath.Join(outside, "foreign.txt"), []byte("foreign inert bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "foreign.txt"), filepath.Join(badSource, "bad.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(goodSource, "good.txt"), []byte("inert good support"), 0o755); err != nil {
		t.Fatal(err)
	}
	bad := resolverHook(filepath.Join(badSource, "hooks.json"), "bad-hook")
	bad.Harnesses = []hcc.Target{hcc.TargetFactory}
	good := resolverHook(filepath.Join(goodSource, "hooks.json"), "good-hook")
	good.Resource = ir.NewHookWithMatcher("good-hook", "PostToolUse", "Edit", "inert good command", "command", nil, ir.SourceRef{File: filepath.Join(goodSource, "hooks.json")})
	good.Harnesses = []hcc.Target{hcc.TargetFactory}
	report, err := Run(t.Context(), repo, []resolver.ResolvedArtifact{bad, good})
	if err != nil || !hasDiagCode(report.Diags, "materialize.hook.support-refused") {
		t.Fatalf("bad source did not refuse: %+v %v", report, err)
	}
	installed := filepath.Join(repo, ".factory", "hooks")
	if data, err := os.ReadFile(filepath.Join(installed, "hooks.json")); err != nil || !bytes.Contains(data, []byte("inert good command")) || bytes.Contains(data, []byte("bad-hook")) {
		t.Fatalf("valid native sibling was suppressed or bad hook emitted: %q %v", data, err)
	}
	if data, err := os.ReadFile(filepath.Join(installed, "good.txt")); err != nil || string(data) != "inert good support" {
		t.Fatalf("valid native support missing: %q %v", data, err)
	}
	if _, err := os.Lstat(filepath.Join(installed, "bad.txt")); !os.IsNotExist(err) {
		t.Fatalf("bad sibling support emitted: %v", err)
	}
}

// spec: materialization/targets-without-hook-support-lanes-do-not-read-unneeded-sources
func TestRunGeminiWithoutHookSupportLaneDoesNotReadSources(t *testing.T) {
	home, repo, source, outside := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	settings := filepath.Join(repo, ".gemini", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settings), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settings, []byte(`{"custom":{"keep":"handwritten"}}`), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "helper.txt"), []byte("inert Gemini support"), 0o755); err != nil {
		t.Fatal(err)
	}
	hook := ir.NewHookWithMatcher("gemini-probe", "PostToolUse", "Edit", "inert Gemini original", "command", nil, ir.SourceRef{File: filepath.Join(source, "hooks.json")})
	resolved := []resolver.ResolvedArtifact{{Entry: catalog.Entry{Name: "gemini-probe", Kind: ir.KindHook, Resource: hook, Path: filepath.Join(source, "hooks.json")}, Source: config.RepoCommitted, Harnesses: []hcc.Target{hcc.TargetGemini}}}
	initial, err := Run(t.Context(), repo, resolved)
	if err != nil || diag.HasErrors(initial.Diags) {
		t.Fatalf("Gemini native control failed: %+v %v", initial, err)
	}
	before, err := os.ReadFile(settings)
	if err != nil || !bytes.Contains(before, []byte("inert Gemini original")) || !bytes.Contains(before, []byte("handwritten")) {
		t.Fatalf("Gemini settings control missing: %q %v", before, err)
	}
	identity, err := os.Stat(settings)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "foreign.txt"), []byte("foreign inert bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "foreign.txt"), filepath.Join(source, "bad.txt")); err != nil {
		t.Fatal(err)
	}
	resolved[0].Resource = ir.NewHookWithMatcher("gemini-probe", "PostToolUse", "Edit", "inert Gemini changed", "command", nil, ir.SourceRef{File: filepath.Join(source, "hooks.json")})
	report, err := Run(t.Context(), repo, resolved)
	if err != nil || diag.HasErrors(report.Diags) {
		t.Fatalf("uncopied support sources blocked Gemini: %+v %v", report, err)
	}
	if data, err := os.ReadFile(settings); err != nil || !bytes.Contains(data, []byte("inert Gemini changed")) || !bytes.Contains(data, []byte("handwritten")) {
		t.Fatalf("Gemini native update/control missing: %q %v", data, err)
	}
	if after, err := os.Stat(settings); err != nil || identity.Mode() != after.Mode() {
		t.Fatalf("Gemini native permissions changed: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(repo, ".gemini", "hooks", "bad.txt")); !os.IsNotExist(err) {
		t.Fatalf("foreign Gemini support emitted: %v", err)
	}
}
