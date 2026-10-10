package materialize

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func isolateIndependentHookRun(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	stateHome := filepath.Join(home, "state")
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	t.Setenv("XDG_STATE_HOME", stateHome)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	stateRoot, err := ownership.StateRoot()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(stateRoot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("ownership state root pre-exists: %s %v", stateRoot, err)
	}
	return stateRoot
}

// spec: hook-native-independent-satisfaction/independent-clients-recognize-complete-repository-projection
// spec: hook-native-independent-satisfaction/complete-portable-support-establishes-satisfaction
// spec: hook-native-independent-satisfaction/reconstruction-grants-no-ownership-or-pruning
// spec: hook-native-independent-satisfaction/read-only-reconstruction-leaves-absent-state-absent
func TestRunIndependentNativeHookSatisfaction(t *testing.T) {
	fixture := newIndependentFactoryFixture(t)
	fixture.writeNative(t, fixture.nativeBody)
	fixture.writeSupport(t, fixture.supportBytes, 0o755)
	extraSupportPath := filepath.Join(filepath.Dir(filepath.Dir(fixture.supportPath)), "unrelated.json")
	extraSupportBytes := []byte(`{"PreToolUse":[{"_managed_by":"agent-marketplace","matcher":"Manual","hooks":[{"type":"command","command":"echo handwritten support"}]}]}`)
	if err := os.WriteFile(extraSupportPath, extraSupportBytes, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(extraSupportPath, 0o640); err != nil {
		t.Fatal(err)
	}
	nativeBefore := snapshotIndependentFile(t, fixture.nativePath)
	supportBefore := snapshotIndependentFile(t, fixture.supportPath)
	extraSupportBefore := snapshotIndependentFile(t, extraSupportPath)

	fixture.run(t)
	assertIndependentFileUnchanged(t, fixture.nativePath, nativeBefore)
	assertIndependentFileUnchanged(t, fixture.supportPath, supportBefore)
	assertIndependentFileAbsent(t, fixture.legacyPath)
	assertIndependentFileUnchanged(t, extraSupportPath, extraSupportBefore)
	store, err := ownership.Open(fixture.nativeRoot)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := InspectNativeHooks(store, fixture.nativeRoot, hcc.TargetFactory, false)
	if err != nil {
		t.Fatalf("InspectNativeHooks: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("native entries = %#v, want selected plus unrelated unmanaged entry", entries)
	}
	for _, entry := range entries {
		if entry.Status != ownership.StatusUnmanaged {
			t.Fatalf("reconstruction changed native ownership status: %#v", entry)
		}
	}
	proof, _ := store.Proof("hooks.json")
	if proof.OwnedCount() != 0 || len(proof.OwnedIDs()) != 0 || len(proof.HookSatisfactions()) != 0 {
		t.Fatalf("independent reconstruction published private proof: %+v", proof)
	}
	assertIndependentStateAbsent(t, fixture.stateRoot)
	dryReport, err := Run(context.Background(), fixture.repoRoot, []resolver.ResolvedArtifact{fixture.resolved}, WithDryRun())
	if err != nil || dryReport == nil {
		t.Fatalf("dry Run: %v", err)
	}
	for _, root := range dryReport.PerRoot {
		for _, written := range root.Written {
			if strings.Contains(written, "hooks.json") || strings.Contains(written, "hook-support") {
				t.Fatalf("dry Run reports a pending native/support hook write: %s", written)
			}
		}
	}
	assertIndependentStateAbsent(t, fixture.stateRoot)
	if _, err := Run(context.Background(), fixture.repoRoot, nil); err != nil {
		t.Fatalf("Run after deselection: %v", err)
	}
	assertIndependentFileAbsent(t, fixture.legacyPath)
	assertIndependentFileUnchanged(t, fixture.nativePath, nativeBefore)
	assertIndependentFileUnchanged(t, fixture.supportPath, supportBefore)
	assertIndependentFileUnchanged(t, extraSupportPath, extraSupportBefore)
	assertIndependentStateAbsent(t, fixture.stateRoot)
}

// spec: hook-native-independent-satisfaction/unproven-hooks-retain-ordinary-legacy-emission
func TestRunIndependentHookWithoutNativeProjectionRetainsLegacyOutput(t *testing.T) {
	stateRoot := isolateIndependentHookRun(t)
	fixture := writeProjectionSource(t, "echo legacy-control")
	repoRoot := t.TempDir()
	report, err := Run(context.Background(), repoRoot, []resolver.ResolvedArtifact{fixture.resolved})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report == nil {
		t.Fatal("Run returned a nil report")
	}
	legacyPath := filepath.Join(repoRoot, ".factory", "hooks", "hooks.json")
	legacy, err := os.ReadFile(legacyPath)
	if err != nil || !bytes.Contains(legacy, []byte("echo legacy-control")) {
		t.Fatalf("ordinary unproven hook legacy output = %q, %v", legacy, err)
	}
	assertIndependentStateAbsent(t, stateRoot)
}

// spec: hook-native-independent-satisfaction/stale-unavailable-or-unverified-source-establishes-no-satisfaction
// spec: hook-native-independent-satisfaction/unproven-hooks-retain-ordinary-legacy-emission
func TestRunIndependentNativeHookUnsafeSourceRetainsLegacy(t *testing.T) {
	fixture := newIndependentFactoryFixture(t)
	sourceDir := filepath.Dir(fixture.sourceDefinition)
	helperTargetName := "helper-target.sh"
	helperLinkName := "helper-link.sh"
	helperBytes := []byte("inert contained helper bytes\n")
	helperTargetPath := filepath.Join(sourceDir, helperTargetName)
	helperLinkPath := filepath.Join(sourceDir, helperLinkName)
	if err := os.WriteFile(helperTargetPath, helperBytes, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(helperTargetName, helperLinkPath); err != nil {
		t.Fatal(err)
	}
	sourceDefinitionBefore := snapshotIndependentFile(t, fixture.sourceDefinition)
	helperTargetBefore := snapshotIndependentFile(t, helperTargetPath)
	helperLinkInfo, err := os.Lstat(helperLinkPath)
	if err != nil || helperLinkInfo.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("source helper is not a symlink: %v", err)
	}
	helperLinkTarget, err := os.Readlink(helperLinkPath)
	if err != nil || helperLinkTarget != helperTargetName {
		t.Fatalf("source helper target = %q, %v", helperLinkTarget, err)
	}
	assertIndependentFileAbsent(t, fixture.nativePath)
	baseline := fixture.run(t)
	if len(baseline.BlockedResources) != 0 {
		t.Fatalf("legacy control blocked resources = %#v", baseline.BlockedResources)
	}
	legacyBefore := snapshotIndependentFile(t, fixture.legacyPath)
	if !bytes.Contains(legacyBefore.data, []byte("scripts/run.sh")) {
		t.Fatalf("legacy control did not emit the selected hook: %s", legacyBefore.data)
	}
	legacyHelperPath := filepath.Join(fixture.nativeRoot, "hooks", helperLinkName)
	legacyHelperBefore := snapshotIndependentFile(t, legacyHelperPath)
	if !bytes.Equal(legacyHelperBefore.data, helperBytes) || legacyHelperBefore.info.Mode().Perm() != 0o640 {
		t.Fatalf("legacy control did not preserve contained helper bytes/mode: %+v", legacyHelperBefore.info)
	}
	hasLegacyWrite := func(report *Report) bool {
		for _, root := range report.PerRoot {
			for _, written := range root.Written {
				if filepath.Clean(filepath.Join(root.OutDir, written)) == fixture.legacyPath {
					return true
				}
			}
		}
		return false
	}
	if !hasLegacyWrite(baseline) {
		t.Fatalf("legacy control did not report the selected legacy write: %#v", baseline.PerRoot)
	}
	if err := os.Remove(fixture.legacyPath); err != nil {
		t.Fatal(err)
	}

	fixture.writeNative(t, fixture.nativeBody)
	fixture.writeSupport(t, fixture.supportBytes, 0o755)
	nativeSupportRoot := filepath.Join(fixture.nativeRoot, "hook-support", fixture.sourceKey, fixture.name)
	if err := os.MkdirAll(nativeSupportRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	nativeHelperTarget := filepath.Join(nativeSupportRoot, helperTargetName)
	nativeHelperLink := filepath.Join(nativeSupportRoot, helperLinkName)
	if err := os.WriteFile(nativeHelperTarget, helperBytes, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(nativeHelperLink, helperBytes, 0o640); err != nil {
		t.Fatal(err)
	}
	handwrittenSupport := filepath.Join(nativeSupportRoot, "handwritten.txt")
	handwrittenBytes := []byte("unrelated handwritten support\n")
	if err := os.WriteFile(handwrittenSupport, handwrittenBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	nativeBefore := snapshotIndependentFile(t, fixture.nativePath)
	runSupportBefore := snapshotIndependentFile(t, fixture.supportPath)
	nativeHelperTargetBefore := snapshotIndependentFile(t, nativeHelperTarget)
	nativeHelperLinkBefore := snapshotIndependentFile(t, nativeHelperLink)
	handwrittenBefore := snapshotIndependentFile(t, handwrittenSupport)

	report, runErr := Run(context.Background(), fixture.repoRoot, []resolver.ResolvedArtifact{fixture.resolved})
	if report == nil {
		t.Fatalf("Run returned nil report: %v", runErr)
	}
	if runErr != nil {
		t.Errorf("Run returned error instead of retaining legacy behavior: %v", runErr)
	}
	if diag.HasErrors(report.Diags) {
		t.Errorf("Run reported an error instead of retaining legacy behavior: %#v", report.Diags)
	}
	if len(report.BlockedResources) != 0 {
		t.Errorf("Run blocked the selected resource instead of falling back to legacy: %#v", report.BlockedResources)
	}
	legacyAfter, err := os.ReadFile(fixture.legacyPath)
	if err != nil {
		t.Errorf("Run did not recreate the selected legacy hook: %v", err)
	} else if !bytes.Equal(legacyBefore.data, legacyAfter) {
		t.Errorf("legacy output = %s, want the ordinary legacy bytes %s", legacyAfter, legacyBefore.data)
	}
	if !hasLegacyWrite(report) {
		t.Errorf("Run did not report the selected legacy write: %#v", report.PerRoot)
	}

	assertIndependentFileUnchanged(t, fixture.sourceDefinition, sourceDefinitionBefore)
	assertIndependentFileUnchanged(t, helperTargetPath, helperTargetBefore)
	helperLinkAfter, err := os.Lstat(helperLinkPath)
	if err != nil || !os.SameFile(helperLinkInfo, helperLinkAfter) || helperLinkAfter.Mode()&os.ModeSymlink == 0 {
		t.Errorf("source helper link changed: %v", err)
	}
	if target, err := os.Readlink(helperLinkPath); err != nil || target != helperLinkTarget {
		t.Errorf("source helper link target = %q, %v; want %q", target, err, helperLinkTarget)
	}
	assertIndependentFileUnchanged(t, legacyHelperPath, legacyHelperBefore)
	assertIndependentFileUnchanged(t, fixture.nativePath, nativeBefore)
	assertIndependentFileUnchanged(t, fixture.supportPath, runSupportBefore)
	assertIndependentFileUnchanged(t, nativeHelperTarget, nativeHelperTargetBefore)
	assertIndependentFileUnchanged(t, nativeHelperLink, nativeHelperLinkBefore)
	assertIndependentFileUnchanged(t, handwrittenSupport, handwrittenBefore)
	assertIndependentStateAbsent(t, fixture.stateRoot)
}

type independentFactoryFixture struct {
	resolved         resolver.ResolvedArtifact
	sourceRoot       string
	sourceDefinition string
	repoRoot         string
	stateRoot        string
	sourceKey        string
	name             string
	nativeRoot       string
	nativePath       string
	legacyPath       string
	supportPath      string
	nativeBody       []byte
	supportBytes     []byte
}

func newIndependentFactoryFixture(t *testing.T) independentFactoryFixture {
	t.Helper()
	stateRoot := isolateIndependentHookRun(t)
	source := writeFactoryProjectionDefinition(t, []byte(`{"PreToolUse":[{"matcher":"Execute","commandRegex":"^make.*$","hooks":[{"type":"command","command":"${DROID_PLUGIN_ROOT}/scripts/run.sh"}]}]}`))
	if source.resolved.Name != "PreToolUse-command" {
		t.Fatalf("catalog hook name = %q, want PreToolUse-command", source.resolved.Name)
	}
	name := source.resolved.InstalledName
	if name == "" {
		name = source.resolved.Name
	}
	repoRoot := t.TempDir()
	sourceKey := oracleSourceKey(t, source.root)
	nativeRoot := filepath.Join(repoRoot, ".factory")
	return independentFactoryFixture{
		resolved:         source.resolved,
		sourceRoot:       source.root,
		sourceDefinition: source.definition,
		repoRoot:         repoRoot,
		stateRoot:        stateRoot,
		sourceKey:        sourceKey,
		name:             name,
		nativeRoot:       nativeRoot,
		nativePath:       filepath.Join(nativeRoot, "hooks.json"),
		legacyPath:       filepath.Join(nativeRoot, "hooks", "hooks.json"),
		supportPath:      filepath.Join(nativeRoot, "hook-support", sourceKey, name, "scripts", "run.sh"),
		nativeBody:       independentFactoryNativeBody(sourceKey, name),
		supportBytes:     []byte("#!/bin/sh\necho witness\n"),
	}
}

func independentFactoryNativeBody(sourceKey, name string) []byte {
	handler := `{"type":"command","command":"\"$FACTORY_PROJECT_DIR\"/.factory/hook-support/` + sourceKey + "/" + name + `/scripts/run.sh"}`
	group := `{"hooks":[` + handler + `],"matcher":"Execute","commandRegex":"^make.*$"}`
	return []byte(`{"PostToolUse":[{"matcher":"Edit","hooks":[{"type":"command","command":"echo handwritten"}]}],"PreToolUse":[` + group + `]}` + "\n")
}

func (f independentFactoryFixture) selectedHandler() string {
	return `{"type":"command","command":"\"$FACTORY_PROJECT_DIR\"/.factory/hook-support/` + f.sourceKey + "/" + f.name + `/scripts/run.sh"}`
}

func (f independentFactoryFixture) selectedGroup() string {
	return `{"hooks":[` + f.selectedHandler() + `],"matcher":"Execute","commandRegex":"^make.*$"}`
}

func (f independentFactoryFixture) writeNative(t *testing.T, data []byte) {
	t.Helper()
	if err := os.MkdirAll(f.nativeRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.nativePath, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func (f independentFactoryFixture) writeSupport(t *testing.T, data []byte, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(f.supportPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.supportPath, data, mode); err != nil {
		t.Fatal(err)
	}
}

func (f independentFactoryFixture) run(t *testing.T) *Report {
	t.Helper()
	return runIndependentPublic(t, f.repoRoot, []resolver.ResolvedArtifact{f.resolved})
}

type independentFileSnapshot struct {
	data []byte
	info os.FileInfo
}

func snapshotIndependentFile(t *testing.T, path string) independentFileSnapshot {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return independentFileSnapshot{data: data, info: info}
}

func assertIndependentFileUnchanged(t *testing.T, path string, before independentFileSnapshot) {
	t.Helper()
	after := snapshotIndependentFile(t, path)
	if !bytes.Equal(before.data, after.data) || !os.SameFile(before.info, after.info) || before.info.Mode().Perm() != after.info.Mode().Perm() || !before.info.ModTime().Equal(after.info.ModTime()) {
		t.Fatalf("read-only hook satisfaction changed %s", path)
	}
}

func assertIndependentFileAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected %s to remain absent, got %v", path, err)
	}
}

func assertIndependentStateAbsent(t *testing.T, stateRoot string) {
	t.Helper()
	if _, err := os.Lstat(stateRoot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read-only satisfaction created ownership state: %s %v", stateRoot, err)
	}
}

func runIndependentPublic(t *testing.T, repoRoot string, resolved []resolver.ResolvedArtifact) *Report {
	t.Helper()
	report, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report == nil {
		t.Fatal("Run returned a nil report")
	}
	for _, diagnostic := range report.Diags {
		if diagnostic.Severity == diag.SeverityError {
			t.Fatalf("Run reported an error diagnostic: %#v", diagnostic)
		}
	}
	return report
}

func writeIndependentClaudeHooks(t *testing.T, target hcc.Target, definition []byte) []resolver.ResolvedArtifact {
	t.Helper()
	root := t.TempDir()
	hooksPath := filepath.Join(root, "hooks", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(hooksPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hooksPath, definition, 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, err := catalog.Load(root, "claude")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}
	if loaded.SourceHarness != "claude" {
		t.Fatalf("catalog source harness = %q, want claude", loaded.SourceHarness)
	}
	var resolved []resolver.ResolvedArtifact
	for _, entry := range loaded.Entries {
		if entry.Kind != ir.KindHook {
			continue
		}
		resolved = append(resolved, resolver.ResolvedArtifact{
			Entry:       entry,
			RegistryURL: root,
			CatalogKey:  root,
			Source:      config.RepoCommitted,
			V4Kind:      config.V4SourceDirectory,
			Harnesses:   []hcc.Target{target},
		})
	}
	if len(resolved) == 0 {
		t.Fatalf("catalog.Load returned no hooks: %#v", loaded.Entries)
	}
	return resolved
}

// spec: hook-native-independent-satisfaction/independent-clients-recognize-complete-repository-projection
// spec: hook-native-location-tolerance/handwritten-native-satisfaction-prevents-legacy-recreation
// spec: hook-native-location-tolerance/handwritten-hook-does-not-become-owned
func TestRunIndependentNativeHookTargetLayouts(t *testing.T) {
	definition := []byte(`{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo target-layout"}]}]}}`)
	cases := []struct {
		name          string
		target        hcc.Target
		nativeRel     string
		nativeRootRel string
		legacyRel     string
		body          []byte
	}{
		{
			name:          "claude",
			target:        hcc.TargetClaude,
			nativeRel:     ".claude/settings.json",
			nativeRootRel: ".claude",
			legacyRel:     ".claude/hooks/hooks.json",
			body:          []byte(`{"hooks":{"PostToolUse":[{"matcher":"Edit","hooks":[{"type":"command","command":"echo handwritten"}]}],"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo target-layout"}]}]}}` + "\n"),
		},
		{
			name:          "cursor",
			target:        hcc.TargetCursor,
			nativeRel:     ".cursor/hooks.json",
			nativeRootRel: ".cursor",
			legacyRel:     ".cursor/hooks/hooks.json",
			body:          []byte(`{"version":1,"hooks":{"sessionStart":[{"type":"command","command":"echo handwritten"}],"preToolUse":[{"type":"command","command":"echo target-layout","matcher":"^Shell$"}]}}` + "\n"),
		},
		{
			name:          "windsurf",
			target:        hcc.TargetWindsurf,
			nativeRel:     ".devin/hooks.json",
			nativeRootRel: ".",
			legacyRel:     ".windsurf/hooks.json",
			body:          []byte(`{"hooks":{"post_run_command":[{"command":"echo handwritten"}],"pre_run_command":[{"command":"echo target-layout"}]}}` + "\n"),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stateRoot := isolateIndependentHookRun(t)
			resolved := writeIndependentClaudeHooks(t, tc.target, definition)
			repoRoot := t.TempDir()
			nativePath := filepath.Join(repoRoot, filepath.FromSlash(tc.nativeRel))
			nativeRoot := filepath.Join(repoRoot, filepath.FromSlash(tc.nativeRootRel))
			if err := os.MkdirAll(filepath.Dir(nativePath), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(nativePath, tc.body, 0o644); err != nil {
				t.Fatal(err)
			}
			before := snapshotIndependentFile(t, nativePath)
			report := runIndependentPublic(t, repoRoot, resolved)
			if len(report.Diags) != 0 {
				t.Fatalf("Run diagnostics = %#v, want none", report.Diags)
			}
			assertIndependentFileAbsent(t, filepath.Join(repoRoot, filepath.FromSlash(tc.legacyRel)))
			assertIndependentFileUnchanged(t, nativePath, before)
			store, err := ownership.Open(nativeRoot)
			if err != nil {
				t.Fatal(err)
			}
			entries, err := InspectNativeHooks(store, nativeRoot, tc.target, false)
			if err != nil {
				t.Fatalf("InspectNativeHooks: %v", err)
			}
			if len(entries) != 2 {
				t.Fatalf("native entries = %#v, want selected plus unrelated unmanaged entry", entries)
			}
			for _, entry := range entries {
				if entry.Status != ownership.StatusUnmanaged {
					t.Fatalf("native entry was claimed: %#v", entry)
				}
			}
			assertIndependentStateAbsent(t, stateRoot)
		})
	}
}

// spec: hook-native-independent-satisfaction/independent-clients-recognize-complete-repository-projection
func TestRunIndependentNativeHookDistinctCatalogResourcesReordered(t *testing.T) {
	stateRoot := isolateIndependentHookRun(t)
	definition := []byte(`{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo first"}]},{"matcher":"Read","hooks":[{"type":"command","command":"echo second"}]}]}}`)
	resolved := writeIndependentClaudeHooks(t, hcc.TargetClaude, definition)
	if len(resolved) != 2 || resolved[0].Name == resolved[1].Name {
		t.Fatalf("catalog.Load selected resources = %#v, want two distinct hook resources", resolved)
	}
	repoRoot := t.TempDir()
	nativePath := filepath.Join(repoRoot, ".claude", "settings.json")
	nativeBody := []byte(`{"hooks":{"PostToolUse":[{"matcher":"Edit","hooks":[{"type":"command","command":"echo handwritten"}]}],"PreToolUse":[{"matcher":"Read","hooks":[{"type":"command","command":"echo second"}]},{"matcher":"Bash","hooks":[{"type":"command","command":"echo first"}]}]}}` + "\n")
	if err := os.MkdirAll(filepath.Dir(nativePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(nativePath, nativeBody, 0o644); err != nil {
		t.Fatal(err)
	}
	before := snapshotIndependentFile(t, nativePath)
	report := runIndependentPublic(t, repoRoot, resolved)
	if len(report.Diags) != 0 {
		t.Fatalf("Run diagnostics = %#v, want none", report.Diags)
	}
	assertIndependentFileAbsent(t, filepath.Join(repoRoot, ".claude", "hooks", "hooks.json"))
	assertIndependentFileUnchanged(t, nativePath, before)
	store, err := ownership.Open(filepath.Join(repoRoot, ".claude"))
	if err != nil {
		t.Fatal(err)
	}
	entries, err := InspectNativeHooks(store, filepath.Join(repoRoot, ".claude"), hcc.TargetClaude, false)
	if err != nil || len(entries) != 3 {
		t.Fatalf("InspectNativeHooks entries = %#v, %v", entries, err)
	}
	for _, entry := range entries {
		if entry.Status != ownership.StatusUnmanaged {
			t.Fatalf("native entry was claimed: %#v", entry)
		}
	}
	assertIndependentStateAbsent(t, stateRoot)
}

func replaceIndependentText(t *testing.T, body []byte, old, new string) []byte {
	t.Helper()
	if strings.Count(string(body), old) != 1 {
		t.Fatalf("expected one occurrence of %q in native fixture", old)
	}
	return []byte(strings.Replace(string(body), old, new, 1))
}

// spec: hook-native-independent-satisfaction/changed-execution-fields-do-not-establish-satisfaction
// spec: hook-native-independent-satisfaction/partial-handler-coverage-suppresses-nothing
// spec: hook-native-independent-satisfaction/reconstruction-grants-no-ownership-or-pruning
// spec: hook-native-independent-satisfaction/target-scope-and-resource-derivations-remain-isolated
func TestRunIndependentNativeHookFactoryNativeMismatchesRetainLegacy(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, independentFactoryFixture) []byte
	}{
		{
			name: "event",
			mutate: func(t *testing.T, f independentFactoryFixture) []byte {
				return replaceIndependentText(t, f.nativeBody, `"PreToolUse":[`, `"PermissionRequest":[`)
			},
		},
		{
			name: "matcher",
			mutate: func(t *testing.T, f independentFactoryFixture) []byte {
				return replaceIndependentText(t, f.nativeBody, `"matcher":"Execute"`, `"matcher":"Read"`)
			},
		},
		{
			name: "handler_type",
			mutate: func(t *testing.T, f independentFactoryFixture) []byte {
				handler := f.selectedHandler()
				changed := strings.Replace(handler, `"type":"command"`, `"type":"prompt"`, 1)
				return replaceIndependentText(t, f.nativeBody, handler, changed)
			},
		},
		{
			name: "command",
			mutate: func(t *testing.T, f independentFactoryFixture) []byte {
				handler := f.selectedHandler()
				changed := strings.Replace(handler, "scripts/run.sh", "scripts/other.sh", 1)
				return replaceIndependentText(t, f.nativeBody, handler, changed)
			},
		},
		{
			name: "prompt",
			mutate: func(t *testing.T, f independentFactoryFixture) []byte {
				handler := f.selectedHandler()
				changed := strings.TrimSuffix(handler, "}") + `,"prompt":"different"}`
				return replaceIndependentText(t, f.nativeBody, handler, changed)
			},
		},
		{
			name: "timeout",
			mutate: func(t *testing.T, f independentFactoryFixture) []byte {
				handler := f.selectedHandler()
				changed := strings.TrimSuffix(handler, "}") + `,"timeout":19}`
				return replaceIndependentText(t, f.nativeBody, handler, changed)
			},
		},
		{
			name: "environment_reference",
			mutate: func(t *testing.T, f independentFactoryFixture) []byte {
				handler := f.selectedHandler()
				changed := strings.Replace(handler, `\"$FACTORY_PROJECT_DIR\"`, `\"$HOME\"`, 1)
				return replaceIndependentText(t, f.nativeBody, handler, changed)
			},
		},
		{
			name: "meaningful_execution_extra",
			mutate: func(t *testing.T, f independentFactoryFixture) []byte {
				handler := f.selectedHandler()
				changed := strings.TrimSuffix(handler, "}") + `,"working_directory":"/tmp"}`
				return replaceIndependentText(t, f.nativeBody, handler, changed)
			},
		},
		{
			name: "ownership_marker_is_part_of_canonical_value",
			mutate: func(t *testing.T, f independentFactoryFixture) []byte {
				group := f.selectedGroup()
				changed := strings.TrimSuffix(group, "}") + `,"_managed_by":"agent-marketplace"}`
				return replaceIndependentText(t, f.nativeBody, group, changed)
			},
		},
		{
			name: "group_command_regex",
			mutate: func(t *testing.T, f independentFactoryFixture) []byte {
				return replaceIndependentText(t, f.nativeBody, `"commandRegex":"^make.*$"`, `"commandRegex":"^other.*$"`)
			},
		},
		{
			name: "partial_handler_group",
			mutate: func(t *testing.T, f independentFactoryFixture) []byte {
				return replaceIndependentText(t, f.nativeBody, f.selectedGroup(), `{"hooks":[],"matcher":"Execute","commandRegex":"^make.*$"}`)
			},
		},
		{
			name: "missing_selected_group",
			mutate: func(t *testing.T, f independentFactoryFixture) []byte {
				return replaceIndependentText(t, f.nativeBody, `"PreToolUse":[`+f.selectedGroup()+`]`, `"PreToolUse":[]`)
			},
		},
		{
			name: "duplicate_selected_group_cardinality",
			mutate: func(t *testing.T, f independentFactoryFixture) []byte {
				group := f.selectedGroup()
				return replaceIndependentText(t, f.nativeBody, `"PreToolUse":[`+group+`]`, `"PreToolUse":[`+group+`,`+group+`]`)
			},
		},
		{
			name: "wrong_resource_support_namespace",
			mutate: func(t *testing.T, f independentFactoryFixture) []byte {
				handler := f.selectedHandler()
				changed := strings.Replace(handler, "/"+f.name+"/scripts/", "/other-resource/scripts/", 1)
				return replaceIndependentText(t, f.nativeBody, handler, changed)
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newIndependentFactoryFixture(t)
			body := tc.mutate(t, f)
			f.writeNative(t, body)
			f.writeSupport(t, f.supportBytes, 0o755)
			nativeBefore := snapshotIndependentFile(t, f.nativePath)
			supportBefore := snapshotIndependentFile(t, f.supportPath)
			f.run(t)
			legacy, err := os.ReadFile(f.legacyPath)
			if err != nil || !bytes.Contains(legacy, []byte("run.sh")) {
				t.Fatalf("incomplete native value did not retain legacy output: %v %q", err, legacy)
			}
			assertIndependentFileUnchanged(t, f.nativePath, nativeBefore)
			assertIndependentFileUnchanged(t, f.supportPath, supportBefore)
			assertIndependentStateAbsent(t, f.stateRoot)
		})
	}
}

// spec: hook-native-independent-satisfaction/support-bytes-and-permissions-participate-in-the-match
func TestRunIndependentNativeHookFactorySupportMismatchRetainsLegacy(t *testing.T) {
	tests := []struct {
		name    string
		present bool
		bytes   []byte
		mode    os.FileMode
	}{
		{name: "missing"},
		{name: "bytes", present: true, bytes: []byte("#!/bin/sh\necho different\n"), mode: 0o755},
		{name: "mode", present: true, mode: 0o644},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newIndependentFactoryFixture(t)
			f.writeNative(t, f.nativeBody)
			if tc.present {
				data := tc.bytes
				if data == nil {
					data = f.supportBytes
				}
				f.writeSupport(t, data, tc.mode)
			}
			nativeBefore := snapshotIndependentFile(t, f.nativePath)
			var supportBefore independentFileSnapshot
			if tc.present {
				supportBefore = snapshotIndependentFile(t, f.supportPath)
			}
			f.run(t)
			legacy, err := os.ReadFile(f.legacyPath)
			if err != nil || !bytes.Contains(legacy, []byte("run.sh")) {
				t.Fatalf("incomplete support did not retain legacy output: %v %q", err, legacy)
			}
			assertIndependentFileUnchanged(t, f.nativePath, nativeBefore)
			if tc.present {
				assertIndependentFileUnchanged(t, f.supportPath, supportBefore)
			} else {
				assertIndependentFileAbsent(t, f.supportPath)
			}
			assertIndependentStateAbsent(t, f.stateRoot)
		})
	}
}

// spec: hook-native-independent-satisfaction/target-scope-and-resource-derivations-remain-isolated
func TestRunIndependentNativeHookTargetIsolation(t *testing.T) {
	stateRoot := isolateIndependentHookRun(t)
	definition := []byte(`{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo target-layout"}]}]}}`)
	resolved := writeIndependentClaudeHooks(t, hcc.TargetClaude, definition)
	resolved[0].Harnesses = []hcc.Target{hcc.TargetClaude, hcc.TargetCursor}
	repoRoot := t.TempDir()
	claudePath := filepath.Join(repoRoot, ".claude", "settings.json")
	claudeBody := []byte(`{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo target-layout"}]}]}}` + "\n")
	cursorPath := filepath.Join(repoRoot, ".cursor", "hooks.json")
	cursorBody := []byte(`{"version":1,"hooks":{"preToolUse":[{"type":"command","command":"echo different-target","matcher":"^Shell$"}]}}` + "\n")
	for _, file := range []struct {
		path string
		data []byte
	}{{claudePath, claudeBody}, {cursorPath, cursorBody}} {
		if err := os.MkdirAll(filepath.Dir(file.path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file.path, file.data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	claudeBefore := snapshotIndependentFile(t, claudePath)
	cursorBefore := snapshotIndependentFile(t, cursorPath)
	report := runIndependentPublic(t, repoRoot, resolved)
	if len(report.Diags) != 0 {
		t.Fatalf("Run diagnostics = %#v, want none", report.Diags)
	}
	assertIndependentFileAbsent(t, filepath.Join(repoRoot, ".claude", "hooks", "hooks.json"))
	cursorLegacy := filepath.Join(repoRoot, ".cursor", "hooks", "hooks.json")
	if data, err := os.ReadFile(cursorLegacy); err != nil || !bytes.Contains(data, []byte("echo target-layout")) {
		t.Fatalf("unmatched Cursor target did not retain its legacy output: %v %q", err, data)
	}
	assertIndependentFileUnchanged(t, claudePath, claudeBefore)
	assertIndependentFileUnchanged(t, cursorPath, cursorBefore)
	assertIndependentStateAbsent(t, stateRoot)
}

// spec: hook-native-independent-satisfaction/target-scope-and-resource-derivations-remain-isolated
func TestRunIndependentNativeHookMachineScopeRetainsLegacy(t *testing.T) {
	stateRoot := isolateIndependentHookRun(t)
	home := filepath.Dir(filepath.Dir(stateRoot))
	definition := []byte(`{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo machine-scope"}]}]}}`)
	resolved := writeIndependentClaudeHooks(t, hcc.TargetClaude, definition)
	resolved[0].Source = config.UserPersonal
	repoRoot := t.TempDir()
	nativeRoot := filepath.Join(home, ".claude")
	nativePath := filepath.Join(nativeRoot, "settings.json")
	nativeBody := []byte(`{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo machine-scope"}]}]}}` + "\n")
	if err := os.MkdirAll(nativeRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(nativePath, nativeBody, 0o644); err != nil {
		t.Fatal(err)
	}
	before := snapshotIndependentFile(t, nativePath)
	report := runIndependentPublic(t, repoRoot, resolved)
	if len(report.Diags) != 0 {
		t.Fatalf("Run diagnostics = %#v, want none", report.Diags)
	}
	legacyPath := filepath.Join(nativeRoot, "hooks", "hooks.json")
	if data, err := os.ReadFile(legacyPath); err != nil || !bytes.Contains(data, []byte("echo machine-scope")) {
		t.Fatalf("machine-scope native hook changed legacy behavior: %v %q", err, data)
	}
	assertIndependentFileUnchanged(t, nativePath, before)
	assertIndependentStateAbsent(t, stateRoot)
}

func assertIndependentRefusal(t *testing.T, report *Report, target hcc.Target, forbidden ...string) {
	t.Helper()
	if report == nil {
		t.Fatal("Run returned a nil report")
	}
	for _, diagnostic := range report.Diags {
		if diagnostic.Code != diag.CodeHookNativeToleranceRefused || diagnostic.Target != string(target) {
			continue
		}
		for _, value := range forbidden {
			if value != "" && strings.Contains(diagnostic.Message, value) {
				t.Fatalf("refusal diagnostic exposed a rejected value: %#v", diagnostic)
			}
		}
		return
	}
	t.Fatalf("Run diagnostics = %#v, want %s refusal for %s", report.Diags, diag.CodeHookNativeToleranceRefused, target)
}

// spec: hook-native-independent-satisfaction/unsafe-support-refuses-the-affected-target-without-mutation
func TestRunIndependentNativeHookFactoryUnsafeSupportRefuses(t *testing.T) {
	for _, shape := range []string{"support_root_symlink", "support_leaf_symlink", "hard_link", "oversized"} {
		t.Run(shape, func(t *testing.T) {
			f := newIndependentFactoryFixture(t)
			f.writeNative(t, f.nativeBody)
			var external string
			var externalBefore independentFileSnapshot
			var linkPath string
			var linkTarget string
			var oversizedBefore os.FileInfo
			switch shape {
			case "support_root_symlink":
				externalRoot := t.TempDir()
				external = filepath.Join(externalRoot, f.sourceKey, f.name, "scripts", "run.sh")
				if err := os.MkdirAll(filepath.Dir(external), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(external, f.supportBytes, 0o755); err != nil {
					t.Fatal(err)
				}
				externalBefore = snapshotIndependentFile(t, external)
				linkPath = filepath.Join(f.nativeRoot, "hook-support")
				linkTarget = externalRoot
				if err := os.Symlink(linkTarget, linkPath); err != nil {
					t.Fatal(err)
				}
			case "support_leaf_symlink":
				external = filepath.Join(t.TempDir(), "sentinel.sh")
				if err := os.WriteFile(external, f.supportBytes, 0o755); err != nil {
					t.Fatal(err)
				}
				externalBefore = snapshotIndependentFile(t, external)
				if err := os.MkdirAll(filepath.Dir(f.supportPath), 0o755); err != nil {
					t.Fatal(err)
				}
				linkPath = f.supportPath
				linkTarget = external
				if err := os.Symlink(linkTarget, linkPath); err != nil {
					t.Fatal(err)
				}
			case "hard_link":
				f.writeSupport(t, f.supportBytes, 0o755)
				external = filepath.Join(t.TempDir(), "sentinel.sh")
				if err := os.Link(f.supportPath, external); err != nil {
					t.Fatal(err)
				}
				externalBefore = snapshotIndependentFile(t, external)
			case "oversized":
				if err := os.MkdirAll(filepath.Dir(f.supportPath), 0o755); err != nil {
					t.Fatal(err)
				}
				file, err := os.OpenFile(f.supportPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
				if err != nil {
					t.Fatal(err)
				}
				if err := file.Truncate(hookSourceFingerprintMaxBytes + 1); err != nil {
					_ = file.Close()
					t.Fatal(err)
				}
				if err := file.Chmod(0o755); err != nil {
					_ = file.Close()
					t.Fatal(err)
				}
				if err := file.Close(); err != nil {
					t.Fatal(err)
				}
				oversizedBefore, err = os.Stat(f.supportPath)
				if err != nil {
					t.Fatal(err)
				}
			}
			nativeBefore := snapshotIndependentFile(t, f.nativePath)
			report, err := Run(context.Background(), f.repoRoot, []resolver.ResolvedArtifact{f.resolved})
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			assertIndependentRefusal(t, report, hcc.TargetFactory, string(f.supportBytes), "${DROID_PLUGIN_ROOT}")
			assertIndependentFileAbsent(t, f.legacyPath)
			assertIndependentFileUnchanged(t, f.nativePath, nativeBefore)
			if external != "" {
				assertIndependentFileUnchanged(t, external, externalBefore)
			}
			if linkPath != "" {
				if got, err := os.Readlink(linkPath); err != nil || got != linkTarget {
					t.Fatalf("support symlink changed: %q %v", got, err)
				}
			}
			if shape == "oversized" {
				oversizedAfter, err := os.Stat(f.supportPath)
				if err != nil || !os.SameFile(oversizedBefore, oversizedAfter) || oversizedAfter.Size() != hookSourceFingerprintMaxBytes+1 || oversizedAfter.Mode().Perm() != 0o755 || !oversizedBefore.ModTime().Equal(oversizedAfter.ModTime()) {
					t.Fatalf("oversized support changed: before=%v after=%v err=%v", oversizedBefore, oversizedAfter, err)
				}
			}
			assertIndependentStateAbsent(t, f.stateRoot)
		})
	}
}

// spec: hook-native-independent-satisfaction/unsafe-support-refuses-the-affected-target-without-mutation
func TestRunIndependentNativeHookNativeRootLinkRefuses(t *testing.T) {
	f := newIndependentFactoryFixture(t)
	externalRoot := t.TempDir()
	externalNative := filepath.Join(externalRoot, "hooks.json")
	if err := os.WriteFile(externalNative, f.nativeBody, 0o644); err != nil {
		t.Fatal(err)
	}
	externalSupport := filepath.Join(externalRoot, "hook-support", f.sourceKey, f.name, "scripts", "run.sh")
	if err := os.MkdirAll(filepath.Dir(externalSupport), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(externalSupport, f.supportBytes, 0o755); err != nil {
		t.Fatal(err)
	}
	nativeBefore := snapshotIndependentFile(t, externalNative)
	supportBefore := snapshotIndependentFile(t, externalSupport)
	if err := os.Symlink(externalRoot, f.nativeRoot); err != nil {
		t.Fatal(err)
	}
	report, err := Run(context.Background(), f.repoRoot, []resolver.ResolvedArtifact{f.resolved})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	assertIndependentRefusal(t, report, hcc.TargetFactory, string(f.supportBytes), "${DROID_PLUGIN_ROOT}")
	assertIndependentFileAbsent(t, f.legacyPath)
	assertIndependentFileAbsent(t, filepath.Join(externalRoot, "hooks", "hooks.json"))
	assertIndependentFileUnchanged(t, externalNative, nativeBefore)
	assertIndependentFileUnchanged(t, externalSupport, supportBefore)
	if got, err := os.Readlink(f.nativeRoot); err != nil || got != externalRoot {
		t.Fatalf("native root link changed: %q %v", got, err)
	}
	assertIndependentStateAbsent(t, f.stateRoot)
}

// spec: hook-native-independent-satisfaction/unsafe-support-refuses-the-affected-target-without-mutation
func TestRunIndependentNativeHookFIFORefusesInBoundedChild(t *testing.T) {
	const childEnv = "AGENT_MARKETPLACE_NATIVE_HOOK_FIFO_CHILD"
	if os.Getenv(childEnv) == "1" {
		f := newIndependentFactoryFixture(t)
		f.writeNative(t, f.nativeBody)
		if err := os.MkdirAll(filepath.Dir(f.supportPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := unix.Mkfifo(f.supportPath, 0o755); err != nil {
			t.Fatal(err)
		}
		nativeBefore := snapshotIndependentFile(t, f.nativePath)
		report, err := Run(context.Background(), f.repoRoot, []resolver.ResolvedArtifact{f.resolved})
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		assertIndependentRefusal(t, report, hcc.TargetFactory, string(f.supportBytes), "${DROID_PLUGIN_ROOT}")
		assertIndependentFileAbsent(t, f.legacyPath)
		assertIndependentFileUnchanged(t, f.nativePath, nativeBefore)
		if info, err := os.Lstat(f.supportPath); err != nil || info.Mode()&os.ModeNamedPipe == 0 {
			t.Fatalf("FIFO support path changed: %v %v", info, err)
		}
		assertIndependentStateAbsent(t, f.stateRoot)
		t.Log("FIFO_REFUSAL_OK")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.v", "-test.run=^TestRunIndependentNativeHookFIFORefusesInBoundedChild$")
	cmd.Env = append(os.Environ(), childEnv+"=1")
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("bounded FIFO child timed out: %s", output)
	}
	if err != nil {
		t.Fatalf("FIFO child exited %v: %s", err, output)
	}
	if !bytes.Contains(output, []byte("FIFO_REFUSAL_OK")) {
		t.Fatalf("FIFO child did not report its completed refusal: %s", output)
	}
}

// spec: hook-native-independent-satisfaction/unsafe-support-refuses-the-affected-target-without-mutation
func TestHookSupportRootDuplicateIsCloseOnExec(t *testing.T) {
	rootFD, err := unix.Open(t.TempDir(), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = unix.Close(rootFD) }()
	rootFlags, err := unix.FcntlInt(uintptr(rootFD), unix.F_GETFD, 0)
	if err != nil {
		t.Fatal(err)
	}
	if rootFlags&unix.FD_CLOEXEC == 0 {
		t.Fatal("root descriptor is not close-on-exec")
	}
	for _, tc := range []struct {
		name     string
		relative string
	}{
		{name: "empty root", relative: ""},
		{name: "dot root", relative: "."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			duplicateFD, err := openHookSupportDirAt(rootFD, tc.relative)
			if err != nil {
				t.Fatalf("openHookSupportDirAt: %v", err)
			}
			defer func() { _ = unix.Close(duplicateFD) }()
			flags, err := unix.FcntlInt(uintptr(duplicateFD), unix.F_GETFD, 0)
			if err != nil {
				t.Fatal(err)
			}
			if flags&unix.FD_CLOEXEC == 0 {
				t.Error("duplicated support root descriptor is not close-on-exec")
			}
		})
	}
}

func TestNativeHookInstalledSupportGenerationConflict(t *testing.T) {
	root := t.TempDir()
	supportPath := filepath.Join(root, "hook-support", "scripts", "run.sh")
	if err := os.MkdirAll(filepath.Dir(supportPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(supportPath, []byte("AAAAAAAA"), 0o755); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(supportPath)
	if err != nil {
		t.Fatal(err)
	}
	rootFD, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = unix.Close(rootFD) }()
	supportFD, err := openHookSupportDirAt(rootFD, "hook-support")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = unix.Close(supportFD) }()
	var total int64
	replacement := []byte("BBBBBBBB")
	_, _, err = readHookSourceSnapshotFileChecked(supportFD, "scripts/run.sh", &total, func(*os.File) {
		if writeErr := os.WriteFile(supportPath, replacement, 0o755); writeErr != nil {
			t.Fatal(writeErr)
		}
		changedTime := before.ModTime().Add(2 * time.Second)
		if timeErr := os.Chtimes(supportPath, changedTime, changedTime); timeErr != nil {
			t.Fatal(timeErr)
		}
	})
	if !errors.Is(err, dependencytxn.ErrGenerationConflict) || !errors.Is(err, fsboundary.ErrGenerationConflict) {
		t.Fatalf("changed support descriptor error = %v, want both generation conflicts", err)
	}
	if got, err := os.ReadFile(supportPath); err != nil || !bytes.Equal(got, replacement) {
		t.Fatalf("controlled support replacement was changed: %v %q", err, got)
	}
}

func writeIndependentOwnershipRecord(t *testing.T, nativeRoot, rel string, nativeBytes []byte, fileState ownership.FileState) string {
	t.Helper()
	store, err := ownership.Open(nativeRoot)
	if err != nil {
		t.Fatal(err)
	}
	fileState.ContentDigest = ownership.Digest(nativeBytes)
	record := ownership.Record{
		Version:       1,
		CanonicalRoot: store.CanonicalRoot(),
		RootKey:       store.RootKey(),
		Generation:    1,
		UpdatedAt:     time.Now().UTC().Format(time.RFC3339Nano),
		Files:         map[string]ownership.FileState{rel: fileState},
	}
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	statePath := store.StateFile()
	if err := os.MkdirAll(filepath.Dir(statePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return statePath
}

// spec: hook-native-independent-satisfaction/existing-private-proof-cannot-be-bypassed
func TestRunIndependentNativeHookPrivateEvidenceCannotBeBypassed(t *testing.T) {
	tests := []struct {
		name    string
		seed    func(*testing.T, independentFactoryFixture, string) string
		corrupt bool
	}{
		{
			name: "selected_resource_without_fingerprint",
			seed: func(t *testing.T, f independentFactoryFixture, _ string) string {
				layout, _ := nativeHookLayoutFor(hcc.TargetFactory, false)
				entry := nativeHookProofEntry(t, "PostToolUse", `{"command":"echo stale","type":"command"}`, f.name, "")
				seedNativeProof(t, f.nativeRoot, layout, f.nativeBody, entry)
				store, err := ownership.Open(f.nativeRoot)
				if err != nil {
					t.Fatal(err)
				}
				return store.StateFile()
			},
		},
		{
			name: "selected_resource_different_fingerprint",
			seed: func(t *testing.T, f independentFactoryFixture, _ string) string {
				layout, _ := nativeHookLayoutFor(hcc.TargetFactory, false)
				wrong := "sha256:" + strings.Repeat("a", 64)
				entry := nativeHookProofEntry(t, "PostToolUse", `{"command":"echo stale","type":"command"}`, f.name, wrong)
				seedNativeProof(t, f.nativeRoot, layout, f.nativeBody, entry)
				store, err := ownership.Open(f.nativeRoot)
				if err != nil {
					t.Fatal(err)
				}
				return store.StateFile()
			},
		},
		{
			name: "partial_drifted_owned_entry",
			seed: func(t *testing.T, f independentFactoryFixture, fingerprint string) string {
				layout, _ := nativeHookLayoutFor(hcc.TargetFactory, false)
				entry := nativeHookProofEntry(t, "PostToolUse", `{"command":"echo stale","type":"command"}`, f.name, fingerprint)
				seedNativeProof(t, f.nativeRoot, layout, f.nativeBody, entry)
				store, err := ownership.Open(f.nativeRoot)
				if err != nil {
					t.Fatal(err)
				}
				return store.StateFile()
			},
		},
		{
			name: "contradictory_source_evidence",
			seed: func(t *testing.T, f independentFactoryFixture, fingerprint string) string {
				layout, _ := nativeHookLayoutFor(hcc.TargetFactory, false)
				wrong := "sha256:" + strings.Repeat("b", 64)
				entry := nativeHookProofEntry(t, "PostToolUse", `{"command":"echo stale","type":"command"}`, "other-resource", wrong)
				entry.SourceEvidence = []ownership.HookSourceEvidence{{Resource: f.name, SourceDigest: fingerprint}}
				seedNativeProof(t, f.nativeRoot, layout, f.nativeBody, entry)
				store, err := ownership.Open(f.nativeRoot)
				if err != nil {
					t.Fatal(err)
				}
				return store.StateFile()
			},
		},
		{
			name: "partial_satisfaction_only_source",
			seed: func(t *testing.T, f independentFactoryFixture, fingerprint string) string {
				valueDigest := ownership.Digest([]byte(`{"command":"echo stale","type":"command"}`))
				satisfaction := ownership.HookSatisfaction{
					Container:      "PostToolUse",
					IdentityDigest: ownership.HookIdentityDigest("PostToolUse", valueDigest),
					ValueDigest:    valueDigest,
					Sources:        []ownership.HookSourceEvidence{{Resource: f.name, SourceDigest: fingerprint}},
				}
				layout, _ := nativeHookLayoutFor(hcc.TargetFactory, false)
				return writeIndependentOwnershipRecord(t, f.nativeRoot, layout.rel, f.nativeBody, ownership.FileState{
					HookSatisfactions: []ownership.HookSatisfaction{satisfaction},
				})
			},
		},
		{
			name:    "corrupt_state_refuses",
			corrupt: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newIndependentFactoryFixture(t)
			f.writeNative(t, f.nativeBody)
			f.writeSupport(t, f.supportBytes, 0o755)
			fingerprint, err := nativeHookFingerprint(f.resolved, hcc.TargetFactory, false)
			if err != nil {
				t.Fatal(err)
			}
			var statePath string
			if tc.corrupt {
				store, err := ownership.Open(f.nativeRoot)
				if err != nil {
					t.Fatal(err)
				}
				statePath = store.StateFile()
				if err := os.MkdirAll(filepath.Dir(statePath), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(statePath, []byte("{corrupt private state"), 0o600); err != nil {
					t.Fatal(err)
				}
			} else {
				statePath = tc.seed(t, f, fingerprint)
			}
			nativeBefore := snapshotIndependentFile(t, f.nativePath)
			supportBefore := snapshotIndependentFile(t, f.supportPath)
			stateBefore := snapshotIndependentFile(t, statePath)
			report, err := Run(context.Background(), f.repoRoot, []resolver.ResolvedArtifact{f.resolved})
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if tc.corrupt {
				assertIndependentRefusal(t, report, hcc.TargetFactory, string(f.supportBytes), "${DROID_PLUGIN_ROOT}")
				assertIndependentFileAbsent(t, f.legacyPath)
			} else {
				for _, diagnostic := range report.Diags {
					if diagnostic.Severity == diag.SeverityError {
						t.Fatalf("Run reported an error diagnostic: %#v", diagnostic)
					}
				}
				legacy, err := os.ReadFile(f.legacyPath)
				if err != nil || !bytes.Contains(legacy, []byte("run.sh")) {
					t.Fatalf("private partial evidence was bypassed: %v %q", err, legacy)
				}
			}
			assertIndependentFileUnchanged(t, f.nativePath, nativeBefore)
			assertIndependentFileUnchanged(t, f.supportPath, supportBefore)
			assertIndependentFileUnchanged(t, statePath, stateBefore)
		})
	}
}

// spec: hook-native-independent-satisfaction/existing-private-proof-cannot-be-bypassed
func TestRunIndependentNativeHookSharedPrivateSatisfactionCannotBeBypassed(t *testing.T) {
	f := newIndependentFactoryFixture(t)
	f.writeNative(t, f.nativeBody)
	f.writeSupport(t, f.supportBytes, 0o755)
	layout, ok := nativeHookLayoutFor(hcc.TargetFactory, false)
	if !ok {
		t.Fatal("Factory repository hook layout is unavailable")
	}
	sourceDigest := "sha256:" + strings.Repeat("e", 64)
	entry := nativeHookProofEntry(t, "PreToolUse", f.selectedGroup(), "other-resource", sourceDigest)
	satisfaction := ownership.HookSatisfaction{
		Container:      entry.Container,
		IdentityDigest: entry.IdentityDigest,
		ValueDigest:    entry.ValueDigest,
		Sources:        []ownership.HookSourceEvidence{{Resource: "other-resource", SourceDigest: sourceDigest}},
	}
	statePath := writeIndependentOwnershipRecord(t, f.nativeRoot, layout.rel, f.nativeBody, ownership.FileState{
		HookSatisfactions: []ownership.HookSatisfaction{satisfaction},
	})
	assertIndependentFileAbsent(t, f.legacyPath)
	nativeBefore := snapshotIndependentFile(t, f.nativePath)
	supportBefore := snapshotIndependentFile(t, f.supportPath)
	stateBefore := snapshotIndependentFile(t, statePath)

	f.run(t)

	legacy, err := os.ReadFile(f.legacyPath)
	if err != nil || !bytes.Contains(legacy, []byte("run.sh")) {
		t.Fatalf("public Run suppressed the legacy hook for a native identity with another resource's private satisfaction: %v %q", err, legacy)
	}
	assertIndependentFileUnchanged(t, f.nativePath, nativeBefore)
	assertIndependentFileUnchanged(t, f.supportPath, supportBefore)
	assertIndependentFileUnchanged(t, statePath, stateBefore)
}

// spec: hook-native-independent-satisfaction/target-scope-and-resource-derivations-remain-isolated
func TestRunIndependentNativeHookUnrelatedPrivateProofDoesNotBlock(t *testing.T) {
	f := newIndependentFactoryFixture(t)
	f.writeNative(t, f.nativeBody)
	f.writeSupport(t, f.supportBytes, 0o755)
	layout, _ := nativeHookLayoutFor(hcc.TargetFactory, false)
	unrelated := nativeHookProofEntry(t, "PostToolUse", `{"hooks":[{"type":"command","command":"echo handwritten"}],"matcher":"Edit"}`, "unrelated-resource", "sha256:"+strings.Repeat("c", 64))
	seedNativeProof(t, f.nativeRoot, layout, f.nativeBody, unrelated)
	store, err := ownership.Open(f.nativeRoot)
	if err != nil {
		t.Fatal(err)
	}
	stateBefore := snapshotIndependentFile(t, store.StateFile())
	nativeBefore := snapshotIndependentFile(t, f.nativePath)
	supportBefore := snapshotIndependentFile(t, f.supportPath)
	f.run(t)
	assertIndependentFileAbsent(t, f.legacyPath)
	assertIndependentFileUnchanged(t, f.nativePath, nativeBefore)
	assertIndependentFileUnchanged(t, f.supportPath, supportBefore)
	assertIndependentFileUnchanged(t, store.StateFile(), stateBefore)
	entries, err := InspectNativeHooks(store, f.nativeRoot, hcc.TargetFactory, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Event == "PreToolUse" && entry.Status != ownership.StatusUnmanaged {
			t.Fatalf("independent entry acquired unrelated proof: %#v", entry)
		}
	}
}

// spec: hook-native-location-tolerance/native-owned-entry-prevents-duplicate-legacy-output
func TestRunIndependentNativeHookValidDurableProofStillSuppresses(t *testing.T) {
	f := newIndependentFactoryFixture(t)
	f.writeNative(t, f.nativeBody)
	f.writeSupport(t, f.supportBytes, 0o755)
	fingerprint, err := nativeHookFingerprint(f.resolved, hcc.TargetFactory, false)
	if err != nil {
		t.Fatal(err)
	}
	layout, _ := nativeHookLayoutFor(hcc.TargetFactory, false)
	entry := nativeHookProofEntry(t, "PreToolUse", f.selectedGroup(), f.name, fingerprint)
	seedNativeProof(t, f.nativeRoot, layout, f.nativeBody, entry)
	store, err := ownership.Open(f.nativeRoot)
	if err != nil {
		t.Fatal(err)
	}
	stateBefore := snapshotIndependentFile(t, store.StateFile())
	nativeBefore := snapshotIndependentFile(t, f.nativePath)
	supportBefore := snapshotIndependentFile(t, f.supportPath)
	f.run(t)
	assertIndependentFileAbsent(t, f.legacyPath)
	assertIndependentFileUnchanged(t, f.nativePath, nativeBefore)
	assertIndependentFileUnchanged(t, f.supportPath, supportBefore)
	assertIndependentFileUnchanged(t, store.StateFile(), stateBefore)
	entries, err := InspectNativeHooks(store, f.nativeRoot, hcc.TargetFactory, false)
	if err != nil {
		t.Fatal(err)
	}
	var selectedOwned bool
	for _, current := range entries {
		if current.Event == "PreToolUse" && current.Status == ownership.StatusOwnedClean {
			selectedOwned = true
		}
	}
	if !selectedOwned {
		t.Fatalf("durable selected proof was not retained: %#v", entries)
	}
}

// spec: hook-native-independent-satisfaction/target-scope-and-resource-derivations-remain-isolated
func TestRunIndependentNativeHookUnrelatedSatisfactionProofDoesNotBlock(t *testing.T) {
	f := newIndependentFactoryFixture(t)
	f.writeNative(t, f.nativeBody)
	f.writeSupport(t, f.supportBytes, 0o755)
	layout, _ := nativeHookLayoutFor(hcc.TargetFactory, false)
	valueDigest := ownership.Digest([]byte(`{"command":"echo other source","type":"command"}`))
	satisfaction := ownership.HookSatisfaction{
		Container:      "PostToolUse",
		IdentityDigest: ownership.HookIdentityDigest("PostToolUse", valueDigest),
		ValueDigest:    valueDigest,
		Sources:        []ownership.HookSourceEvidence{{Resource: "unrelated-resource", SourceDigest: "sha256:" + strings.Repeat("d", 64)}},
	}
	statePath := writeIndependentOwnershipRecord(t, f.nativeRoot, layout.rel, f.nativeBody, ownership.FileState{
		HookSatisfactions: []ownership.HookSatisfaction{satisfaction},
	})
	stateBefore := snapshotIndependentFile(t, statePath)
	nativeBefore := snapshotIndependentFile(t, f.nativePath)
	supportBefore := snapshotIndependentFile(t, f.supportPath)
	f.run(t)
	assertIndependentFileAbsent(t, f.legacyPath)
	assertIndependentFileUnchanged(t, f.nativePath, nativeBefore)
	assertIndependentFileUnchanged(t, f.supportPath, supportBefore)
	assertIndependentFileUnchanged(t, statePath, stateBefore)
}

// spec: hook-native-independent-satisfaction/stale-unavailable-or-unverified-source-establishes-no-satisfaction
// spec: hook-native-independent-satisfaction/unproven-hooks-retain-ordinary-legacy-emission
func TestRunIndependentNativeHookStaleOrUnavailableSourceRetainsLegacy(t *testing.T) {
	for _, removeDefinition := range []bool{false, true} {
		name := "stale_definition"
		if removeDefinition {
			name = "unavailable_definition"
		}
		t.Run(name, func(t *testing.T) {
			f := newIndependentFactoryFixture(t)
			f.writeNative(t, f.nativeBody)
			f.writeSupport(t, f.supportBytes, 0o755)
			nativeBefore := snapshotIndependentFile(t, f.nativePath)
			supportBefore := snapshotIndependentFile(t, f.supportPath)
			var sourceBefore independentFileSnapshot
			if removeDefinition {
				if err := os.Remove(f.sourceDefinition); err != nil {
					t.Fatal(err)
				}
			} else {
				before, err := os.ReadFile(f.sourceDefinition)
				if err != nil {
					t.Fatal(err)
				}
				changed := bytes.Replace(before, []byte("${DROID_PLUGIN_ROOT}/scripts/run.sh"), []byte("${DROID_PLUGIN_ROOT}/scripts/changed.sh"), 1)
				if bytes.Equal(before, changed) {
					t.Fatal("Factory source fixture did not contain the expected command")
				}
				if err := os.WriteFile(f.sourceDefinition, changed, 0o644); err != nil {
					t.Fatal(err)
				}
				sourceBefore = snapshotIndependentFile(t, f.sourceDefinition)
			}
			f.run(t)
			legacy, err := os.ReadFile(f.legacyPath)
			if err != nil || !bytes.Contains(legacy, []byte("run.sh")) {
				t.Fatalf("stale or unavailable source did not retain ordinary legacy output: %v %q", err, legacy)
			}
			assertIndependentFileUnchanged(t, f.nativePath, nativeBefore)
			assertIndependentFileUnchanged(t, f.supportPath, supportBefore)
			if removeDefinition {
				assertIndependentFileAbsent(t, f.sourceDefinition)
			} else {
				assertIndependentFileUnchanged(t, f.sourceDefinition, sourceBefore)
			}
			assertIndependentStateAbsent(t, f.stateRoot)
		})
	}
}

// spec: hook-native-independent-satisfaction/marked-legacy-twins-remain-byte-identical
func TestRunIndependentNativeHookPreservesMarkedLegacyTwin(t *testing.T) {
	f := newIndependentFactoryFixture(t)
	f.run(t)
	if _, err := os.Stat(f.legacyPath); err != nil {
		t.Fatalf("baseline legacy hook was not created: %v", err)
	}
	if err := os.Chmod(f.legacyPath, 0o640); err != nil {
		t.Fatal(err)
	}
	legacyBefore := snapshotIndependentFile(t, f.legacyPath)
	if !bytes.Contains(legacyBefore.data, []byte("agent-marketplace")) {
		t.Fatalf("baseline legacy output lacks its marker: %q", legacyBefore.data)
	}
	f.writeNative(t, f.nativeBody)
	f.writeSupport(t, f.supportBytes, 0o755)
	nativeBefore := snapshotIndependentFile(t, f.nativePath)
	supportBefore := snapshotIndependentFile(t, f.supportPath)
	f.run(t)
	assertIndependentFileUnchanged(t, f.legacyPath, legacyBefore)
	assertIndependentFileUnchanged(t, f.nativePath, nativeBefore)
	assertIndependentFileUnchanged(t, f.supportPath, supportBefore)
	assertIndependentStateAbsent(t, f.stateRoot)
}

// spec: hook-native-independent-satisfaction/unsafe-support-refuses-the-affected-target-without-mutation
// spec: hook-native-independent-satisfaction/target-scope-and-resource-derivations-remain-isolated
func TestRunIndependentNativeHookFactorySupportRefusalDoesNotBlockClaude(t *testing.T) {
	f := newIndependentFactoryFixture(t)
	f.writeNative(t, f.nativeBody)
	externalRoot := t.TempDir()
	externalSupport := filepath.Join(externalRoot, f.sourceKey, f.name, "scripts", "run.sh")
	if err := os.MkdirAll(filepath.Dir(externalSupport), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(externalSupport, f.supportBytes, 0o755); err != nil {
		t.Fatal(err)
	}
	externalBefore := snapshotIndependentFile(t, externalSupport)
	if err := os.Symlink(externalRoot, filepath.Join(f.nativeRoot, "hook-support")); err != nil {
		t.Fatal(err)
	}
	claudeResolved := writeIndependentClaudeHooks(t, hcc.TargetClaude, []byte(`{"hooks":{"PostToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo sibling-target"}]}]}}`))
	repoRoot := f.repoRoot
	claudePath := filepath.Join(repoRoot, ".claude", "settings.json")
	claudeBody := []byte(`{"hooks":{"PostToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo sibling-target"}]}]}}` + "\n")
	if err := os.MkdirAll(filepath.Dir(claudePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(claudePath, claudeBody, 0o644); err != nil {
		t.Fatal(err)
	}
	factoryBefore := snapshotIndependentFile(t, f.nativePath)
	claudeBefore := snapshotIndependentFile(t, claudePath)
	resolved := append([]resolver.ResolvedArtifact{f.resolved}, claudeResolved...)
	report, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	assertIndependentRefusal(t, report, hcc.TargetFactory, string(f.supportBytes), "${DROID_PLUGIN_ROOT}")
	for _, diagnostic := range report.Diags {
		if diagnostic.Severity == diag.SeverityError && diagnostic.Target == string(hcc.TargetClaude) {
			t.Fatalf("independent Claude target was blocked: %#v", diagnostic)
		}
	}
	if len(report.BlockedResources) != 1 || report.BlockedResources[0].Target != hcc.TargetFactory {
		t.Fatalf("blocked resources = %#v, want only the Factory hook", report.BlockedResources)
	}
	assertIndependentFileAbsent(t, f.legacyPath)
	assertIndependentFileAbsent(t, filepath.Join(repoRoot, ".claude", "hooks", "hooks.json"))
	assertIndependentFileUnchanged(t, f.nativePath, factoryBefore)
	assertIndependentFileUnchanged(t, claudePath, claudeBefore)
	assertIndependentFileUnchanged(t, externalSupport, externalBefore)
	assertIndependentStateAbsent(t, f.stateRoot)
}

// spec: hook-native-independent-satisfaction/changed-execution-fields-do-not-establish-satisfaction
func TestRunIndependentNativeHookDoesNotReadForeignCommandPath(t *testing.T) {
	const childEnv = "AGENT_MARKETPLACE_NATIVE_HOOK_COMMAND_PATH_CHILD"
	if os.Getenv(childEnv) == "1" {
		f := newIndependentFactoryFixture(t)
		foreignHome := t.TempDir()
		commandPath := filepath.Join(foreignHome, "scripts", "command.sh")
		if err := os.MkdirAll(filepath.Dir(commandPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := unix.Mkfifo(commandPath, 0o755); err != nil {
			t.Fatal(err)
		}
		nativeBody := replaceIndependentText(t, f.nativeBody, f.selectedHandler(), `{"type":"command","command":"`+filepath.ToSlash(commandPath)+`"}`)
		f.writeNative(t, nativeBody)
		nativeBefore := snapshotIndependentFile(t, f.nativePath)
		report := f.run(t)
		legacy, err := os.ReadFile(f.legacyPath)
		if err != nil || !bytes.Contains(legacy, []byte("run.sh")) {
			t.Fatalf("unmatched native command did not retain legacy output: %v %q", err, legacy)
		}
		assertIndependentFileUnchanged(t, f.nativePath, nativeBefore)
		if info, err := os.Lstat(commandPath); err != nil || info.Mode()&os.ModeNamedPipe == 0 {
			t.Fatalf("foreign command path changed: %v %v", info, err)
		}
		assertIndependentStateAbsent(t, f.stateRoot)
		if len(report.Diags) != 0 {
			t.Fatalf("command path was treated as support input: %#v", report.Diags)
		}
		t.Log("COMMAND_PATH_NOT_READ")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.v", "-test.run=^TestRunIndependentNativeHookDoesNotReadForeignCommandPath$")
	cmd.Env = append(os.Environ(), childEnv+"=1")
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("bounded command-path child timed out: %s", output)
	}
	if err != nil {
		t.Fatalf("command-path child exited %v: %s", err, output)
	}
	if !bytes.Contains(output, []byte("COMMAND_PATH_NOT_READ")) {
		t.Fatalf("command-path child did not complete its public Run control: %s", output)
	}
}

func TestNativeHookEntryMultisetRequiresCompleteUnmanagedProjection(t *testing.T) {
	testEntry := func(event string, rule string, status ownership.Status) (ownedJSONExistingEntry, NativeHookEntry) {
		digest := ownership.Digest([]byte(rule))
		id := ownership.EntryID{
			Kind:           ownership.KindHook,
			Container:      event,
			IdentityDigest: ownership.HookIdentityDigest(event, digest),
		}
		return ownedJSONExistingEntry{value: ownership.EntryValue{ID: id, ValueDigest: digest}}, NativeHookEntry{
			Event: event, ID: id, ValueDigest: digest, Status: status,
		}
	}
	firstExpected, firstActual := testEntry("PreToolUse", `{"hooks":[{"command":"echo first","type":"command"}],"matcher":"Bash"}`, ownership.StatusUnmanaged)
	secondExpected, secondActual := testEntry("PostToolUse", `{"hooks":[{"command":"echo second","type":"command"}],"matcher":"Read"}`, ownership.StatusUnmanaged)
	_, unrelated := testEntry("SessionStart", `{"hooks":[{"command":"echo unrelated","type":"command"}]}`, ownership.StatusUnmanaged)
	tests := []struct {
		name     string
		expected []ownedJSONExistingEntry
		actual   []NativeHookEntry
		want     bool
	}{
		{
			name:     "complete_reordered_with_handwritten_entry",
			expected: []ownedJSONExistingEntry{firstExpected, secondExpected},
			actual:   []NativeHookEntry{secondActual, unrelated, firstActual},
			want:     true,
		},
		{
			name:     "one_projected_entry_missing",
			expected: []ownedJSONExistingEntry{firstExpected, secondExpected},
			actual:   []NativeHookEntry{firstActual},
		},
		{
			name:     "duplicate_projected_identity",
			expected: []ownedJSONExistingEntry{firstExpected, secondExpected},
			actual:   []NativeHookEntry{firstActual, secondActual, firstActual},
		},
		{
			name:     "matching_entry_is_owned",
			expected: []ownedJSONExistingEntry{firstExpected},
			actual:   []NativeHookEntry{{Event: firstActual.Event, ID: firstActual.ID, ValueDigest: firstActual.ValueDigest, Status: ownership.StatusOwnedClean}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := nativeHookEntryMultisetMatches(tc.expected, tc.actual); got != tc.want {
				t.Fatalf("nativeHookEntryMultisetMatches() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestNativeHookProjectionPruneFenceIsRepositoryOnlyAndNarrow(t *testing.T) {
	repoRoot := t.TempDir()
	for _, tc := range []struct {
		target   hcc.Target
		pruneRel string
		file     string
	}{
		{target: hcc.TargetFactory, pruneRel: ".factory", file: "hooks.json"},
		{target: hcc.TargetClaude, pruneRel: ".claude", file: "settings.json"},
		{target: hcc.TargetCursor, pruneRel: ".cursor", file: "hooks.json"},
		{target: hcc.TargetWindsurf, pruneRel: ".windsurf"},
	} {
		t.Run(string(tc.target), func(t *testing.T) {
			destinations, err := harness.ResolveDestinations(tc.target, false, repoRoot)
			if err != nil {
				t.Fatalf("ResolveDestinations: %v", err)
			}
			fence := extendNativeHookProjectionPruneFence(tc.target, false, filepath.Join(repoRoot, tc.pruneRel), destinations, nil)
			if tc.file == "" {
				if len(fence) != 0 {
					t.Fatalf("out-of-root HookProjection was protected: %v", fence)
				}
				return
			}
			if !fence[tc.file] || !fence["hook-support/"] {
				t.Fatalf("HookProjection fence = %v, want %q and hook-support/", fence, tc.file)
			}
			if fence["hooks/hooks.json"] || fence["mcp.json"] {
				t.Fatalf("fence widened into legacy aggregates: %v", fence)
			}
		})
	}

	stateRoot := isolateIndependentHookRun(t)
	home := filepath.Dir(filepath.Dir(stateRoot))
	destinations, err := harness.ResolveDestinations(hcc.TargetFactory, true, repoRoot)
	if err != nil {
		t.Fatalf("machine ResolveDestinations: %v", err)
	}
	prior := map[string]bool{"mcp.json": true}
	fence := extendNativeHookProjectionPruneFence(hcc.TargetFactory, true, filepath.Join(home, ".factory"), destinations, prior)
	if len(fence) != len(prior) || !fence["mcp.json"] || fence["hooks.json"] || fence["hook-support/"] {
		t.Fatalf("machine-scope fence changed: %v", fence)
	}
}
