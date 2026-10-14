package materialize

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func requireHookSupportRefusalPair(t *testing.T, report *Report, target hcc.Target, resource string) {
	t.Helper()
	var compile, refused *diag.Diagnostic
	var refusedCount int
	for i := range report.Diags {
		diagnostic := &report.Diags[i]
		if diagnostic.Code == "materialize.compile.error" {
			compile = diagnostic
		}
		if diagnostic.Code == diag.CodeMaterializeHookSupportRefused {
			refused = diagnostic
			refusedCount++
		}
	}
	if compile == nil || refused == nil || refusedCount != 1 {
		t.Fatalf("source refusal lacks exactly one generic and registered diagnostic: %+v", report.Diags)
	}
	if compile.Severity != diag.SeverityError || refused.Severity != diag.SeverityError ||
		compile.Target != string(target) || compile.Resource != resource ||
		refused.Target != compile.Target || refused.Resource != compile.Resource || refused.Source != compile.Source ||
		refused.Message != "Hook support source could not be admitted safely; installed hook content preserved" {
		t.Fatalf("source refusal diagnostics differ: generic=%+v registered=%+v", compile, refused)
	}
}

// spec: materialization/hook-support-source-refusal-preserves-installed-native-content
func TestRunHookSupportSourceRefusalPreservesNativeContent(t *testing.T) {
	home, repo, source, outside := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	hooksDir := filepath.Join(source, "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	definitionPath := filepath.Join(hooksDir, "hooks.json")
	definition := []byte(`{"hooks":{"PostToolUse":[{"matcher":"Write|Edit","hooks":[{"type":"command","command":"inert original command"}]}]}}`)
	if err := os.WriteFile(definitionPath, definition, 0o644); err != nil {
		t.Fatal(err)
	}
	helperPath := filepath.Join(hooksDir, "helper.txt")
	if err := os.WriteFile(helperPath, []byte("inert hook support"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(helperPath, 0o755); err != nil {
		t.Fatal(err)
	}
	resolved := resolverHook(t, definitionPath, "support-probe")
	factoryRoot := filepath.Join(repo, ".factory")
	supportPlan, supportPath := nativeHookActivationSupportPath(t, resolved, hcc.TargetFactory, false, factoryRoot)
	if len(supportPlan.artifacts) != 1 {
		t.Fatalf("source-backed support plan = %+v", supportPlan.artifacts)
	}
	initial, err := Run(t.Context(), repo, []resolver.ResolvedArtifact{resolved})
	if err != nil || diag.HasErrors(initial.Diags) {
		t.Fatalf("native initial control failed: %+v %v", initial, err)
	}
	nativePath := filepath.Join(factoryRoot, "hooks.json")
	nativeBefore, nativeMode, nativeMtime := fileSnapshot(t, nativePath)
	nativeIdentity, err := os.Stat(nativePath)
	if err != nil || !bytes.Contains(nativeBefore, []byte("inert original command")) || !bytes.Contains(nativeBefore, []byte("^(Create|Edit|ApplyPatch)$")) {
		t.Fatalf("source-bound Factory native output missing: %q %v", nativeBefore, err)
	}
	supportBefore, supportMode, supportMtime := fileSnapshot(t, supportPath)
	supportIdentity, err := os.Stat(supportPath)
	if err != nil || string(supportBefore) != "inert hook support" || supportMode.Perm() != 0o755 {
		t.Fatalf("source-backed support copy missing: %q %v / %v", supportBefore, supportMode, err)
	}
	store, err := ownership.Open(factoryRoot)
	if err != nil {
		t.Fatal(err)
	}
	proofBefore, proofMode, proofMtime := fileSnapshot(t, store.StateFile())
	proofIdentity, err := os.Stat(store.StateFile())
	if err != nil {
		t.Fatal(err)
	}
	manifestPath, err := portableManifestPath(repo, false)
	if err != nil {
		t.Fatal(err)
	}
	manifestBefore, manifestMode, manifestMtime := fileSnapshot(t, manifestPath)
	manifestIdentity, err := os.Stat(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	helperBefore, helperMode, helperMtime := fileSnapshot(t, helperPath)
	payload := filepath.Join(outside, "private.txt")
	if err := os.WriteFile(payload, []byte("foreign inert bytes"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(payload, 0o640); err != nil {
		t.Fatal(err)
	}
	payloadBefore, payloadMode, payloadMtime := fileSnapshot(t, payload)
	payloadIdentity, err := os.Stat(payload)
	if err != nil {
		t.Fatal(err)
	}
	badSupport := filepath.Join(hooksDir, "bad.txt")
	if err := os.Symlink(payload, badSupport); err != nil {
		t.Fatal(err)
	}
	badLinkBefore, err := os.Lstat(badSupport)
	if err != nil {
		t.Fatal(err)
	}
	badLinkTarget, err := os.Readlink(badSupport)
	if err != nil {
		t.Fatal(err)
	}
	changedDefinition := []byte(`{"hooks":{"PostToolUse":[{"matcher":"Write|Edit","hooks":[{"type":"command","command":"inert changed command"}]}]}}`)
	if err := os.WriteFile(definitionPath, changedDefinition, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(definitionPath, 0o644); err != nil {
		t.Fatal(err)
	}
	changedDefinitionBefore, changedDefinitionMode, changedDefinitionMtime := fileSnapshot(t, definitionPath)
	resolved = resolverHook(t, definitionPath, "support-probe")
	report, err := Run(t.Context(), repo, []resolver.ResolvedArtifact{resolved})
	if err != nil || report == nil {
		t.Fatalf("foreign hook source did not refuse: %+v %v", report, err)
	}
	requireHookSupportRefusalPair(t, report, hcc.TargetFactory, "support-probe")
	blocked := false
	for _, resource := range report.BlockedResources {
		blocked = blocked || resource.Target == hcc.TargetFactory && resource.Name == "support-probe"
	}
	if !blocked {
		t.Fatalf("source refusal did not block its native resource: %+v", report.BlockedResources)
	}
	assertPathAbsent(t, filepath.Join(factoryRoot, filepath.FromSlash(supportPlan.relBase), "bad.txt"))
	assertFileUnchanged(t, nativePath, nativeBefore, nativeMode, nativeMtime)
	assertFileUnchanged(t, supportPath, supportBefore, supportMode, supportMtime)
	assertFileUnchanged(t, store.StateFile(), proofBefore, proofMode, proofMtime)
	assertFileUnchanged(t, manifestPath, manifestBefore, manifestMode, manifestMtime)
	assertFileUnchanged(t, helperPath, helperBefore, helperMode, helperMtime)
	assertFileUnchanged(t, definitionPath, changedDefinitionBefore, changedDefinitionMode, changedDefinitionMtime)
	assertFileUnchanged(t, payload, payloadBefore, payloadMode, payloadMtime)
	if after, err := os.Stat(nativePath); err != nil || !os.SameFile(nativeIdentity, after) {
		t.Fatalf("native hook identity changed: %v", err)
	}
	if after, err := os.Stat(supportPath); err != nil || !os.SameFile(supportIdentity, after) {
		t.Fatalf("installed support identity changed: %v", err)
	}
	if after, err := os.Stat(store.StateFile()); err != nil || !os.SameFile(proofIdentity, after) {
		t.Fatalf("ownership proof identity changed: %v", err)
	}
	if after, err := os.Stat(manifestPath); err != nil || !os.SameFile(manifestIdentity, after) {
		t.Fatalf("portable manifest identity changed: %v", err)
	}
	if after, err := os.Stat(payload); err != nil || !os.SameFile(payloadIdentity, after) {
		t.Fatalf("foreign payload identity changed: %v", err)
	}
	if after, err := os.Lstat(badSupport); err != nil || !os.SameFile(badLinkBefore, after) || after.Mode() != badLinkBefore.Mode() || !after.ModTime().Equal(badLinkBefore.ModTime()) {
		t.Fatalf("bad source link changed: %v", err)
	}
	if link, err := os.Readlink(badSupport); err != nil || link != badLinkTarget {
		t.Fatalf("bad source link target changed: %q %v", link, err)
	}
	if data, err := os.ReadFile(payload); err != nil || string(data) != "foreign inert bytes" {
		t.Fatalf("foreign payload changed: %q %v", data, err)
	}
}

// spec: materialization/contained-hook-support-alias-keeps-native-content-and-mode
func TestRunContainedHookSupportAliasKeepsNativeContentAndMode(t *testing.T) {
	home, repo, source := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	hooksDir := filepath.Join(source, "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	definitionPath := filepath.Join(hooksDir, "hooks.json")
	definition := []byte(`{"hooks":{"PostToolUse":[{"matcher":"Write|Edit","hooks":[{"type":"command","command":"inert hook command"}]}]}}`)
	if err := os.WriteFile(definitionPath, definition, 0o644); err != nil {
		t.Fatal(err)
	}
	payload := filepath.Join(hooksDir, "helper.txt")
	if err := os.WriteFile(payload, []byte("inert contained hook data"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(payload, 0o755); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(payload)
	if err != nil {
		t.Fatal(err)
	}
	aliasSource := filepath.Join(hooksDir, "alias.txt")
	if err := os.Symlink("helper.txt", aliasSource); err != nil {
		t.Fatal(err)
	}
	aliasBefore, err := os.Lstat(aliasSource)
	if err != nil {
		t.Fatal(err)
	}
	aliasTarget, err := os.Readlink(aliasSource)
	if err != nil {
		t.Fatal(err)
	}
	resolved := resolverHook(t, definitionPath, "support-probe")
	factoryRoot := filepath.Join(repo, ".factory")
	supportPlan, _ := nativeHookActivationSupportPath(t, resolved, hcc.TargetFactory, false, factoryRoot)
	installed := make(map[string]string, len(supportPlan.artifacts))
	for _, planned := range supportPlan.artifacts {
		name := filepath.Base(filepath.FromSlash(planned.Path))
		if name == "helper.txt" || name == "alias.txt" {
			installed[name] = filepath.Join(factoryRoot, filepath.FromSlash(planned.Path))
		}
	}
	aliasOutput, helperOutput := installed["alias.txt"], installed["helper.txt"]
	if aliasOutput == "" || helperOutput == "" {
		t.Fatalf("contained alias source plan omitted its actual support files: %+v", supportPlan.artifacts)
	}
	report, err := Run(t.Context(), repo, []resolver.ResolvedArtifact{resolved})
	if err != nil || diag.HasErrors(report.Diags) {
		t.Fatalf("contained alias refused: %+v %v", report, err)
	}
	aliasBytes, aliasMode, aliasMtime := fileSnapshot(t, aliasOutput)
	helperBytes, helperMode, helperMtime := fileSnapshot(t, helperOutput)
	if string(aliasBytes) != "inert contained hook data" || string(helperBytes) != "inert contained hook data" ||
		aliasMode.Perm() != 0o755 || helperMode.Perm() != 0o755 {
		t.Fatalf("contained support copies = %q/%v and %q/%v", aliasBytes, aliasMode, helperBytes, helperMode)
	}
	aliasInfo, err := os.Lstat(aliasOutput)
	if err != nil || !aliasInfo.Mode().IsRegular() {
		t.Fatalf("installed alias is not a regular copy: %v / %v", aliasInfo, err)
	}
	nativePath := filepath.Join(factoryRoot, "hooks.json")
	if data, err := os.ReadFile(nativePath); err != nil || !bytes.Contains(data, []byte("inert hook command")) || !bytes.Contains(data, []byte("^(Create|Edit|ApplyPatch)$")) {
		t.Fatalf("native Factory hook control missing at %s: %q %v", nativePath, data, err)
	}
	second, err := Run(t.Context(), repo, []resolver.ResolvedArtifact{resolved})
	if err != nil || diag.HasErrors(second.Diags) {
		t.Fatalf("contained repeat failed: %+v %v", second, err)
	}
	assertFileUnchanged(t, aliasOutput, aliasBytes, aliasMode, aliasMtime)
	assertFileUnchanged(t, helperOutput, helperBytes, helperMode, helperMtime)
	if after, err := os.Lstat(aliasOutput); err != nil || !os.SameFile(aliasInfo, after) {
		t.Fatalf("contained support repeat replaced alias copy: %v", err)
	}
	if after, err := os.Stat(payload); err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("contained source changed: %v", err)
	}
	aliasAfter, err := os.Lstat(aliasSource)
	if err != nil || !os.SameFile(aliasBefore, aliasAfter) || aliasBefore.Mode() != aliasAfter.Mode() || !aliasBefore.ModTime().Equal(aliasAfter.ModTime()) {
		t.Fatalf("source alias changed: %v", err)
	}
	if target, err := os.Readlink(aliasSource); err != nil || target != aliasTarget {
		t.Fatalf("source alias target changed: %q %v", target, err)
	}
}

// spec: hook-resources/partial-tool-coverage-suppresses-the-hook
func TestRunFactoryEditOnlyHookRefusalDoesNotPublish(t *testing.T) {
	t.Run("edit-only", func(t *testing.T) {
		home, repo, source := t.TempDir(), t.TempDir(), t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
		t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
		t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
		hooksDir := filepath.Join(source, "hooks")
		if err := os.MkdirAll(hooksDir, 0o755); err != nil {
			t.Fatal(err)
		}
		definitionPath := filepath.Join(hooksDir, "hooks.json")
		definition := []byte(`{"hooks":{"PostToolUse":[{"matcher":"Edit","hooks":[{"type":"command","command":"inert Edit-only command"}]}]}}`)
		if err := os.WriteFile(definitionPath, definition, 0o644); err != nil {
			t.Fatal(err)
		}
		helperPath := filepath.Join(hooksDir, "helper.txt")
		if err := os.WriteFile(helperPath, []byte("inert Edit-only support"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(helperPath, 0o755); err != nil {
			t.Fatal(err)
		}
		definitionBefore, definitionMode, definitionMtime := fileSnapshot(t, definitionPath)
		definitionInfo, err := os.Stat(definitionPath)
		if err != nil {
			t.Fatal(err)
		}
		helperBefore, helperMode, helperMtime := fileSnapshot(t, helperPath)
		helperInfo, err := os.Stat(helperPath)
		if err != nil {
			t.Fatal(err)
		}
		resolved := resolverHook(t, definitionPath, "edit-only-probe")
		report, err := Run(t.Context(), repo, []resolver.ResolvedArtifact{resolved})
		if err != nil || report == nil || !hasDiagCode(report.Diags, diag.CodeHookMatcherUntranslatable) {
			t.Fatalf("Edit-only Factory matcher was not refused: %+v %v", report, err)
		}
		blocked := false
		for _, resource := range report.BlockedResources {
			blocked = blocked || resource.Target == hcc.TargetFactory && resource.Name == "edit-only-probe"
		}
		if !blocked {
			t.Fatalf("Edit-only Factory hook was not blocked: %+v", report.BlockedResources)
		}
		factoryRoot := filepath.Join(repo, ".factory")
		assertPathAbsent(t, filepath.Join(factoryRoot, "hooks.json"))
		assertPathAbsent(t, filepath.Join(factoryRoot, "hook-support"))
		manifestPath, err := portableManifestPath(repo, false)
		if err != nil {
			t.Fatal(err)
		}
		assertPathAbsent(t, manifestPath)
		assertPathAbsent(t, filepath.Join(home, "state", "agent-marketplace", "ownership"))
		assertFileUnchanged(t, definitionPath, definitionBefore, definitionMode, definitionMtime)
		assertFileUnchanged(t, helperPath, helperBefore, helperMode, helperMtime)
		if after, err := os.Stat(definitionPath); err != nil || !os.SameFile(definitionInfo, after) {
			t.Fatalf("Edit-only source definition identity changed: %v", err)
		}
		if after, err := os.Stat(helperPath); err != nil || !os.SameFile(helperInfo, after) {
			t.Fatalf("Edit-only support source identity changed: %v", err)
		}
	})
}

// spec: materialization/hook-source-refusal-keeps-independent-valid-native-work
func TestRunHookSourceRefusalKeepsIndependentNativeWork(t *testing.T) {
	home, repo, badSource, goodSource, outside := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	badDefinition := filepath.Join(badSource, "hooks", "hooks.json")
	goodDefinition := filepath.Join(goodSource, "hooks", "hooks.json")
	badSupport := filepath.Join(badSource, "hooks", "bad.txt")
	goodSupport := filepath.Join(goodSource, "hooks", "good.txt")
	for _, dir := range []string{filepath.Dir(badDefinition), filepath.Dir(goodDefinition)} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(outside, "foreign.txt"), []byte("foreign inert bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(badDefinition, []byte(`{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"inert bad command"}]}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "foreign.txt"), badSupport); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(goodDefinition, []byte(`{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"inert good command"}]}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(goodSupport, []byte("inert good support"), 0o755); err != nil {
		t.Fatal(err)
	}
	badDefinitionBefore, badDefinitionMode, badDefinitionMtime := fileSnapshot(t, badDefinition)
	goodDefinitionBefore, goodDefinitionMode, goodDefinitionMtime := fileSnapshot(t, goodDefinition)
	goodSupportBefore, goodSupportMode, goodSupportMtime := fileSnapshot(t, goodSupport)
	foreignBefore, foreignMode, foreignMtime := fileSnapshot(t, filepath.Join(outside, "foreign.txt"))
	foreignIdentity, err := os.Stat(filepath.Join(outside, "foreign.txt"))
	if err != nil {
		t.Fatal(err)
	}
	badLinkBefore, err := os.Lstat(badSupport)
	if err != nil {
		t.Fatal(err)
	}
	badLinkTarget, err := os.Readlink(badSupport)
	if err != nil {
		t.Fatal(err)
	}
	bad := resolverHook(t, badDefinition, "bad-hook")
	bad.Harnesses = []hcc.Target{hcc.TargetFactory}
	good := resolverHook(t, goodDefinition, "good-hook")
	good.Harnesses = []hcc.Target{hcc.TargetFactory}
	report, err := Run(t.Context(), repo, []resolver.ResolvedArtifact{bad, good})
	if err != nil || report == nil {
		t.Fatalf("bad source did not refuse: %+v %v", report, err)
	}
	requireHookSupportRefusalPair(t, report, hcc.TargetFactory, "bad-hook")
	badBlocked, goodBlocked := false, false
	for _, blocked := range report.BlockedResources {
		badBlocked = badBlocked || blocked.Name == "bad-hook" && blocked.Target == hcc.TargetFactory
		goodBlocked = goodBlocked || blocked.Name == "good-hook" && blocked.Target == hcc.TargetFactory
	}
	if !badBlocked || goodBlocked {
		t.Fatalf("source refusal blocked invalid/valid controls incorrectly: %+v", report.BlockedResources)
	}
	nativePath := filepath.Join(repo, ".factory", "hooks.json")
	if data, err := os.ReadFile(nativePath); err != nil || !bytes.Contains(data, []byte("inert good command")) || bytes.Contains(data, []byte("inert bad command")) {
		t.Fatalf("valid native sibling was suppressed or bad hook emitted: %q %v", data, err)
	}
	supportRoot := filepath.Join(repo, ".factory", "hook-support")
	goodSupportFound, badSupportFound := false, false
	if err := filepath.Walk(supportRoot, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			t.Errorf("repository hook support output is a link: %s", path)
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		goodSupportFound = goodSupportFound || filepath.Base(path) == "good.txt" && bytes.Equal(data, []byte("inert good support"))
		badSupportFound = badSupportFound || filepath.Base(path) == "bad.txt" || bytes.Equal(data, []byte("foreign inert bytes"))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !goodSupportFound || badSupportFound {
		t.Fatalf("scoped support copies: good=%t bad-or-foreign=%t", goodSupportFound, badSupportFound)
	}
	assertFileUnchanged(t, badDefinition, badDefinitionBefore, badDefinitionMode, badDefinitionMtime)
	assertFileUnchanged(t, goodDefinition, goodDefinitionBefore, goodDefinitionMode, goodDefinitionMtime)
	assertFileUnchanged(t, goodSupport, goodSupportBefore, goodSupportMode, goodSupportMtime)
	assertFileUnchanged(t, filepath.Join(outside, "foreign.txt"), foreignBefore, foreignMode, foreignMtime)
	foreignAfter, err := os.Stat(filepath.Join(outside, "foreign.txt"))
	if err != nil || !os.SameFile(foreignIdentity, foreignAfter) {
		t.Fatalf("foreign source identity changed: %v", err)
	}
	badLinkAfter, err := os.Lstat(badSupport)
	if err != nil || !os.SameFile(badLinkBefore, badLinkAfter) || badLinkBefore.Mode() != badLinkAfter.Mode() || !badLinkBefore.ModTime().Equal(badLinkAfter.ModTime()) {
		t.Fatalf("bad source link changed: %v", err)
	}
	if link, err := os.Readlink(badSupport); err != nil || link != badLinkTarget {
		t.Fatalf("bad source link target changed: %q %v", link, err)
	}
}

func frozenGeminiSettingsPath(t *testing.T, repo string) string {
	t.Helper()
	destinations, err := harness.ResolveDestinations(hcc.TargetGemini, false, repo)
	if err != nil {
		t.Fatal(err)
	}
	for _, destination := range destinations {
		if destination.AggregateFile == "settings.json" {
			return filepath.Join(destination.Root, destination.AggregateFile)
		}
	}
	t.Fatal("Gemini layout has no mapped settings aggregate")
	return ""
}

func requireFrozenGeminiSettingsWrite(t *testing.T, report *Report, settingsPath string) {
	t.Helper()
	for _, root := range report.PerRoot {
		if root.Target != hcc.TargetGemini || filepath.Clean(root.OutDir) != filepath.Clean(filepath.Dir(settingsPath)) {
			continue
		}
		for _, written := range root.Written {
			if written == filepath.Base(settingsPath) {
				return
			}
		}
	}
	t.Fatalf("frozen Gemini native consumer did not report a settings write: %+v", report.PerRoot)
}

func frozenGeminiNoSupportFixture(t *testing.T, repo string, hook *ir.Hook) *Report {
	t.Helper()
	compiled, err := hcc.Compile(hook, hcc.TargetGemini, hcc.WithScope(hcc.ScopeProject), hcc.WithManagedBy(managedByField, managedByValue))
	if err != nil || compiled == nil || diag.HasErrors(compiled.Diagnostics) {
		t.Fatalf("compile frozen Gemini native hook: %+v %v", compiled, err)
	}
	if len(compiled.Artifacts) != 1 {
		t.Fatalf("frozen Gemini native hook artifacts = %+v", compiled.Artifacts)
	}
	fragment := compiled.Artifacts[0]
	if fragment.Path != ".gemini/settings.json" || !fragment.Fragment || !fragment.MergeWithExisting || fragment.ExternalReconciliationRequired ||
		fragment.ManagedByField != managedByField || fragment.ManagedByValue != managedByValue {
		t.Fatalf("frozen Gemini marker fragment = %+v", fragment)
	}
	settingsPath := frozenGeminiSettingsPath(t, repo)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	boundary := &destinationBoundary{repoRoot: repo, homeDir: home, roots: []string{filepath.Dir(settingsPath)}}
	report := &Report{PerTarget: make(map[hcc.Target]TargetReport)}
	remaining := materializeNativeAggregates(t.Context(), report, outputKey{target: hcc.TargetGemini}, repo, compiled.Artifacts, nil, false, boundary)
	if len(remaining) != 0 {
		t.Fatalf("frozen Gemini native consumer left artifacts unhandled: %+v", remaining)
	}
	requireFrozenGeminiSettingsWrite(t, report, settingsPath)
	return report
}

// spec: materialization/targets-without-hook-support-lanes-do-not-read-unneeded-sources
func TestGeminiWithoutHookSupportLaneDoesNotReadSources(t *testing.T) {
	home, repo, source, outside := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	settings := frozenGeminiSettingsPath(t, repo)
	if err := os.MkdirAll(filepath.Dir(settings), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settings, []byte(`{"custom":{"keep":"handwritten"}}`), 0o640); err != nil {
		t.Fatal(err)
	}
	helper := filepath.Join(source, "helper.txt")
	if err := os.WriteFile(helper, []byte("inert Gemini support"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(helper, 0o755); err != nil {
		t.Fatal(err)
	}
	helperBefore, helperMode, helperMtime := fileSnapshot(t, helper)
	helperIdentity, err := os.Stat(helper)
	if err != nil {
		t.Fatal(err)
	}
	hook := ir.NewHookWithMatcher("gemini-probe", "PostToolUse", "Edit", "inert Gemini original", "command", nil, ir.SourceRef{File: filepath.Join(source, "hooks.json")})
	report := frozenGeminiNoSupportFixture(t, repo, hook)
	if diag.HasErrors(report.Diags) {
		t.Fatalf("frozen Gemini native control failed: %+v", report)
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
	foreignPath := filepath.Join(outside, "foreign.txt")
	if err := os.Chmod(foreignPath, 0o644); err != nil {
		t.Fatal(err)
	}
	foreignBefore, foreignMode, foreignMtime := fileSnapshot(t, foreignPath)
	foreignIdentity, err := os.Stat(foreignPath)
	if err != nil {
		t.Fatal(err)
	}
	badLink := filepath.Join(source, "bad.txt")
	if err := os.Symlink(foreignPath, badLink); err != nil {
		t.Fatal(err)
	}
	badLinkBefore, err := os.Lstat(badLink)
	if err != nil {
		t.Fatal(err)
	}
	badLinkTarget, err := os.Readlink(badLink)
	if err != nil {
		t.Fatal(err)
	}
	changed := ir.NewHookWithMatcher("gemini-probe", "PostToolUse", "Edit", "inert Gemini changed", "command", nil, ir.SourceRef{File: filepath.Join(source, "hooks.json")})
	report = frozenGeminiNoSupportFixture(t, repo, changed)
	if diag.HasErrors(report.Diags) {
		t.Fatalf("frozen Gemini consumer read unneeded sources: %+v", report)
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
	if _, err := os.Lstat(filepath.Join(repo, ".gemini", "hook-support")); !os.IsNotExist(err) {
		t.Fatalf("frozen no-support consumer created a support lane: %v", err)
	}
	assertFileUnchanged(t, helper, helperBefore, helperMode, helperMtime)
	if after, err := os.Stat(helper); err != nil || !os.SameFile(helperIdentity, after) {
		t.Fatalf("frozen no-support consumer changed source identity: %v", err)
	}
	assertFileUnchanged(t, foreignPath, foreignBefore, foreignMode, foreignMtime)
	if after, err := os.Stat(foreignPath); err != nil || !os.SameFile(foreignIdentity, after) {
		t.Fatalf("frozen no-support consumer changed foreign identity: %v", err)
	}
	if after, err := os.Lstat(badLink); err != nil || !os.SameFile(badLinkBefore, after) || badLinkBefore.Mode() != after.Mode() || !badLinkBefore.ModTime().Equal(after.ModTime()) {
		t.Fatalf("frozen no-support consumer changed source link: %v", err)
	}
	if target, err := os.Readlink(badLink); err != nil || target != badLinkTarget {
		t.Fatalf("frozen no-support consumer changed source link target: %q %v", target, err)
	}
	if _, err := os.Lstat(filepath.Join(source, "hooks.json")); !os.IsNotExist(err) {
		t.Fatalf("frozen no-support consumer created a source definition: %v", err)
	}
}

// spec: materialization/hook-support-source-refusal-preserves-installed-native-content
func TestRunGeminiHookSupportSourceRefusalPreservesNativeContent(t *testing.T) {
	configureSharedGeminiHome(t)
	repo, outside := t.TempDir(), t.TempDir()
	repositoryScript := []byte(nativeSupportScriptBody)
	repositoryPayload := []byte(`{"owner":"repository-support"}`)
	hook, sourceRoot := geminiSupportHook(t, "gemini-probe", config.RepoCommitted, repositoryScript, repositoryPayload)
	nativeRoot := filepath.Join(repo, ".gemini")
	settingsPath := filepath.Join(nativeRoot, "settings.json")
	if err := os.MkdirAll(nativeRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settingsPath, []byte(`{"custom":{"keep":"handwritten"}}`), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(settingsPath, 0o640); err != nil {
		t.Fatal(err)
	}
	modes := config.MaterializationModes{Project: config.MaterializationModeFile, Machine: config.MaterializationModeFile}
	initial, err := Run(t.Context(), repo, []resolver.ResolvedArtifact{hook}, WithMaterializationModes(modes))
	if err != nil || initial == nil || hasErrorDiagnostics(initial.Diags) {
		t.Fatalf("source-bound Gemini initial control failed: %+v %v", initial, err)
	}
	nativeBefore, nativeMode, nativeMtime := fileSnapshot(t, settingsPath)
	nativeIdentity, err := os.Stat(settingsPath)
	if err != nil || !bytes.Contains(nativeBefore, []byte("check.sh")) || !bytes.Contains(nativeBefore, []byte("handwritten")) {
		t.Fatalf("source-bound Gemini native control missing: %q %v", nativeBefore, err)
	}
	store, err := ownership.Open(nativeRoot)
	if err != nil {
		t.Fatal(err)
	}
	proofBefore, err := store.Proof("settings.json")
	if err != nil || len(proofBefore.OwnedIDs()) != 1 {
		t.Fatalf("source-bound Gemini proof = %+v / %v", proofBefore, err)
	}
	proofEntry, ok := proofBefore.Entry(proofBefore.OwnedIDs()[0])
	if !ok || proofEntry.Resource != "gemini-probe" || proofEntry.SourceDigest == "" {
		t.Fatalf("Gemini hook proof lacks source binding: %+v / %t", proofEntry, ok)
	}
	proofBeforeBytes, proofMode, proofMtime := fileSnapshot(t, store.StateFile())
	proofIdentity, err := os.Stat(store.StateFile())
	if err != nil {
		t.Fatal(err)
	}
	plan, _ := nativeHookActivationSupportPath(t, hook, hcc.TargetGemini, false, nativeRoot)
	if len(plan.artifacts) != 2 {
		t.Fatalf("Gemini source support plan = %+v", plan.artifacts)
	}
	type supportSnapshot struct {
		path  string
		data  []byte
		mode  os.FileMode
		mtime time.Time
		info  os.FileInfo
	}
	var installed []supportSnapshot
	for _, support := range plan.artifacts {
		path := filepath.Join(nativeRoot, filepath.FromSlash(support.Path))
		data, mode, mtime := fileSnapshot(t, path)
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || !bytes.Equal(data, support.Bytes) || mode.Perm() != portableArtifactMode(support.Mode) {
			t.Fatalf("Gemini scoped support copy %s = %q / %v / %v", path, data, mode, err)
		}
		installed = append(installed, supportSnapshot{path: path, data: data, mode: mode, mtime: mtime, info: info})
	}
	manifestPath, err := portableManifestPath(repo, false)
	if err != nil {
		t.Fatal(err)
	}
	manifestBefore, manifestMode, manifestMtime := fileSnapshot(t, manifestPath)
	manifestIdentity, err := os.Stat(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	sourceDefinition := filepath.Join(sourceRoot, "hooks", "hooks.json")
	sourceScript := filepath.Join(sourceRoot, "hooks", "scripts", "check.sh")
	sourcePayload := filepath.Join(sourceRoot, "hooks", "scripts", "payload.json")
	sourceDefinitionBefore, sourceDefinitionMode, sourceDefinitionMtime := fileSnapshot(t, sourceDefinition)
	sourceScriptBefore, sourceScriptMode, sourceScriptMtime := fileSnapshot(t, sourceScript)
	sourcePayloadBefore, sourcePayloadMode, sourcePayloadMtime := fileSnapshot(t, sourcePayload)
	foreignPath := filepath.Join(outside, "foreign.txt")
	if err := os.WriteFile(foreignPath, []byte("foreign inert Gemini bytes"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(foreignPath, 0o640); err != nil {
		t.Fatal(err)
	}
	foreignBefore, foreignMode, foreignMtime := fileSnapshot(t, foreignPath)
	foreignIdentity, err := os.Stat(foreignPath)
	if err != nil {
		t.Fatal(err)
	}
	badSupport := filepath.Join(sourceRoot, "hooks", "bad.txt")
	if err := os.Symlink(foreignPath, badSupport); err != nil {
		t.Fatal(err)
	}
	badLinkBefore, err := os.Lstat(badSupport)
	if err != nil {
		t.Fatal(err)
	}
	badLinkTarget, err := os.Readlink(badSupport)
	if err != nil {
		t.Fatal(err)
	}
	report, err := Run(t.Context(), repo, []resolver.ResolvedArtifact{hook}, WithMaterializationModes(modes))
	if err != nil || report == nil {
		t.Fatalf("unsafe source sibling was not refused: %+v %v", report, err)
	}
	requireHookSupportRefusalPair(t, report, hcc.TargetGemini, "gemini-probe")
	blocked := false
	for _, resource := range report.BlockedResources {
		blocked = blocked || resource.Target == hcc.TargetGemini && resource.Name == "gemini-probe"
	}
	if !blocked {
		t.Fatalf("unsafe source sibling did not block its Gemini hook: %+v", report.BlockedResources)
	}
	assertFileUnchanged(t, settingsPath, nativeBefore, nativeMode, nativeMtime)
	assertFileUnchanged(t, store.StateFile(), proofBeforeBytes, proofMode, proofMtime)
	assertFileUnchanged(t, manifestPath, manifestBefore, manifestMode, manifestMtime)
	for _, support := range installed {
		assertFileUnchanged(t, support.path, support.data, support.mode, support.mtime)
		after, err := os.Lstat(support.path)
		if err != nil || !os.SameFile(support.info, after) {
			t.Fatalf("Gemini support identity changed at %s: %v", support.path, err)
		}
	}
	if after, err := os.Stat(settingsPath); err != nil || !os.SameFile(nativeIdentity, after) {
		t.Fatalf("Gemini native identity changed: %v", err)
	}
	if after, err := os.Stat(store.StateFile()); err != nil || !os.SameFile(proofIdentity, after) {
		t.Fatalf("Gemini proof identity changed: %v", err)
	}
	if after, err := os.Stat(manifestPath); err != nil || !os.SameFile(manifestIdentity, after) {
		t.Fatalf("Gemini manifest identity changed: %v", err)
	}
	assertFileUnchanged(t, sourceDefinition, sourceDefinitionBefore, sourceDefinitionMode, sourceDefinitionMtime)
	assertFileUnchanged(t, sourceScript, sourceScriptBefore, sourceScriptMode, sourceScriptMtime)
	assertFileUnchanged(t, sourcePayload, sourcePayloadBefore, sourcePayloadMode, sourcePayloadMtime)
	assertFileUnchanged(t, foreignPath, foreignBefore, foreignMode, foreignMtime)
	foreignAfter, err := os.Stat(foreignPath)
	if err != nil || !os.SameFile(foreignIdentity, foreignAfter) {
		t.Fatalf("foreign payload identity changed: %v", err)
	}
	badLinkAfter, err := os.Lstat(badSupport)
	if err != nil || !os.SameFile(badLinkBefore, badLinkAfter) || badLinkBefore.Mode() != badLinkAfter.Mode() || !badLinkBefore.ModTime().Equal(badLinkAfter.ModTime()) {
		t.Fatalf("bad support source link changed: %v", err)
	}
	if target, err := os.Readlink(badSupport); err != nil || target != badLinkTarget {
		t.Fatalf("bad support source link target changed: %q %v", target, err)
	}
	supportRoot := filepath.Join(nativeRoot, "hook-support")
	if err := filepath.Walk(supportRoot, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		if filepath.Base(path) == "bad.txt" {
			t.Errorf("unsafe Gemini support sibling was emitted: %s", path)
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.Equal(data, []byte("foreign inert Gemini bytes")) {
			t.Errorf("foreign Gemini support payload was copied: %s", path)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
