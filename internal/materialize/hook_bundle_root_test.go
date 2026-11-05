package materialize

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// bundleRootHookFiles renders a Claude-layout hook definition plus one
// support script; command chooses the plugin-root reference form.
func bundleRootHookFiles(command string, extra map[string]string) map[string]string {
	files := map[string]string{
		"hooks/hooks.json":       `{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":` + jsonString(command) + `}]}]}`,
		"hooks/scripts/check.sh": "#!/bin/sh\necho bundle support\n",
	}
	for rel, body := range extra {
		files[rel] = body
	}
	return files
}

func jsonString(s string) string {
	data, _ := json.Marshal(s)
	return string(data)
}

func reportHasDiagCode(report *Report, code string, severity diag.Severity) bool {
	if report == nil {
		return false
	}
	for _, d := range report.Diags {
		if d.Code == code && d.Severity == severity {
			return true
		}
	}
	return false
}

func hookSupportGlob(t *testing.T, root string, parts ...string) []string {
	t.Helper()
	elements := append([]string{root}, parts...)
	matches, err := filepath.Glob(filepath.Join(elements...))
	if err != nil {
		t.Fatal(err)
	}
	return matches
}

// spec: hook-resources/bundle-root-references-resolve-on-every-source-path
// spec: hook-resources/support-files-keep-their-bundle-relative-paths
func TestHookBundleRootReferencesResolveFromDirectorySource(t *testing.T) {
	for _, machine := range []bool{false, true} {
		t.Run(map[bool]string{false: "repo", true: "machine"}[machine], func(t *testing.T) {
			repo, home := additiveSafetyEnv(t)
			provider := nativeSupportSource(t, "claude", "hooks/hooks.json", bundleRootHookFiles("${CLAUDE_PLUGIN_ROOT}/hooks/scripts/check.sh", map[string]string{
				"hooks/scripts/nested/deep.sh": "echo deep\n",
			}))
			resolved := nativeSupportResolve(t, hcc.TargetFactory, machine, []resolver.ResolvedArtifact{provider})
			modes := config.MaterializationModes{Project: config.MaterializationModeFile, Machine: config.MaterializationModeFile}
			report, err := Run(context.Background(), repo, resolved, WithMaterializationModes(modes))
			if err != nil || report == nil {
				t.Fatalf("run: %v", err)
			}
			if hasErrorDiagnostics(report.Diags) {
				t.Fatalf("unexpected error diagnostics: %v", report.Diags)
			}
			for _, d := range report.Diags {
				if strings.HasPrefix(d.Code, "hook.") {
					t.Fatalf("unexpected hook diagnostic: %+v", d)
				}
			}
			base := repo
			file := ".factory/hooks.json"
			supportRoot := filepath.Join(repo, ".factory", "hook-support")
			if machine {
				base = home
				supportRoot = filepath.Join(home, ".factory", "hook-support")
			}
			loc := nativeSupportLocationFor(t, hcc.TargetFactory)
			commands := nativeSupportCommands(t, loc, filepath.Join(base, file), 1)
			installed, _ := loc.installedRef(t, commands[0], repo, home, machine)
			if filepath.Base(installed) != "check.sh" || filepath.Base(filepath.Dir(installed)) != "scripts" ||
				filepath.Base(filepath.Dir(filepath.Dir(installed))) != "hooks" {
				t.Fatalf("installed command %q is not <support>/<source>/<hook>/hooks/scripts/check.sh", commands[0])
			}
			data, err := os.ReadFile(installed)
			if err != nil || string(data) != "#!/bin/sh\necho bundle support\n" {
				t.Fatalf("installed support = %q / %v", data, err)
			}
			nested := hookSupportGlob(t, supportRoot, "*", "*", "hooks", "scripts", "nested", "deep.sh")
			if len(nested) != 1 {
				t.Fatalf("nested bundle-relative support = %v, want one", nested)
			}
		})
	}
}

// spec: hook-resources/git-and-mirror-delivery-produce-the-same-support-layout
func TestHookBundleRootGitAndMirrorProduceSameSupportLayout(t *testing.T) {
	body := "#!/bin/sh\necho flat sibling\n"
	command := "${CLAUDE_PLUGIN_ROOT}/hooks/check.sh"
	files := map[string]string{
		"hooks/hooks.json": `{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":` + jsonString(command) + `}]}]}`,
		"hooks/check.sh":   body,
	}
	gitProvider := nativeSupportSource(t, "claude", "hooks/hooks.json", files)
	hook, ok := gitProvider.Resource.(*ir.Hook)
	if !ok {
		t.Fatal("provider resource is not a hook")
	}
	clone := *hook
	clone.SupportFiles = map[string]string{
		"hooks/check.sh": artifact.SupportFileDigest(artifact.SupportFile{Data: []byte(body), Executable: true}),
	}
	mirror := resolver.ResolvedArtifact{
		Entry:                   gitProvider.Entry,
		RegistryURL:             "https://example.invalid/registry.git",
		Ref:                     "main",
		Source:                  config.RepoCommitted,
		IsMarketplace:           true,
		MarketplaceAcquisition:  config.MarketplaceAcquisitionMirror,
		MarketplaceInstanceID:   "mp",
		MarketplaceRegistryID:   "registry",
		MarketplaceResourceName: "PreToolUse-command",
		InstalledName:           "PreToolUse-command",
		MarketplaceDigest:       "sha256:" + strings.Repeat("a", 64),
		MarketplaceHookSupport:  []artifact.Artifact{{Path: "hooks/check.sh", Bytes: []byte(body), Mode: 0o755}},
		Harnesses:               []hcc.Target{hcc.TargetFactory},
	}
	mirrorEntry := mirror.Entry
	mirrorEntry.Resource = &clone
	mirror.Entry = mirrorEntry

	modes := config.MaterializationModes{Project: config.MaterializationModeFile, Machine: config.MaterializationModeFile}
	loc := nativeSupportLocationFor(t, hcc.TargetFactory)
	var commands []string
	var layouts []string
	for _, provider := range []resolver.ResolvedArtifact{gitProvider, mirror} {
		repo, home := additiveSafetyEnv(t)
		resolved := []resolver.ResolvedArtifact{provider}
		if !provider.IsMarketplace {
			resolved = nativeSupportResolve(t, hcc.TargetFactory, false, resolved)
		}
		report, err := Run(context.Background(), repo, resolved, WithMaterializationModes(modes))
		if err != nil || report == nil || hasErrorDiagnostics(report.Diags) {
			t.Fatalf("run: %v / %v", err, report)
		}
		got := nativeSupportCommands(t, loc, filepath.Join(repo, ".factory", "hooks.json"), 1)
		commands = append(commands, got[0])
		installed, _ := loc.installedRef(t, got[0], repo, home, false)
		data, err := os.ReadFile(installed)
		if err != nil {
			t.Fatalf("commanded support file %q is not installed: %v", installed, err)
		}
		if string(data) != body {
			t.Fatalf("commanded support file %q = %q, want shipped sibling %q", installed, data, body)
		}
		rel, err := filepath.Rel(filepath.Join(repo, ".factory", "hook-support"), installed)
		if err != nil {
			t.Fatal(err)
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) != 4 || parts[2] != "hooks" || parts[3] != "check.sh" {
			t.Fatalf("installed support rel = %q, want <source>/<hook>/hooks/check.sh", rel)
		}
		layouts = append(layouts, strings.Join(parts[2:], "/"))
	}
	if layouts[0] != layouts[1] {
		t.Fatalf("git layout %q != mirror layout %q", layouts[0], layouts[1])
	}
	gitParts := strings.Split(commands[0], "/")
	mirrorParts := strings.Split(commands[1], "/")
	if len(gitParts) != len(mirrorParts) {
		t.Fatalf("git command %q vs mirror command %q differ in shape", commands[0], commands[1])
	}
	for i := range gitParts {
		if i == len(gitParts)-4 {
			continue // source key
		}
		if gitParts[i] != mirrorParts[i] {
			t.Fatalf("git command %q vs mirror command %q differ beyond the source key", commands[0], commands[1])
		}
	}
}

// bundleRootHookProviders catalogs a source tree and returns one provider per
// hook entry, unlike nativeSupportSource which keeps only the first.
func bundleRootHookProviders(t *testing.T, sourceHarness, defRel string, files map[string]string) []resolver.ResolvedArtifact {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		additiveSafetyWrite(t, filepath.Join(root, filepath.FromSlash(rel)), body)
	}
	cat, err := catalog.Load(root, sourceHarness)
	if err != nil {
		t.Fatalf("catalog %s: %v", sourceHarness, err)
	}
	var providers []resolver.ResolvedArtifact
	for _, entry := range cat.Entries {
		if entry.Kind == ir.KindHook && strings.HasSuffix(filepath.ToSlash(entry.Path), defRel) {
			providers = append(providers, resolver.ResolvedArtifact{Entry: entry, RegistryURL: root, CatalogKey: fmt.Sprintf("%s#%d", root, len(providers))})
		}
	}
	if len(providers) == 0 {
		t.Fatalf("catalog %s produced no hooks for %s", sourceHarness, defRel)
	}
	return providers
}

// spec: hook-resources/a-reference-to-a-missing-support-file-refuses-the-hook
func TestHookBundleRootMissingReferenceRefusesHook(t *testing.T) {
	repo, _ := additiveSafetyEnv(t)
	definition := `{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"${CLAUDE_PLUGIN_ROOT}/hooks/scripts/absent.sh"}]}],"PostToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"${CLAUDE_PLUGIN_ROOT}/hooks/scripts/check.sh"}]}]}`
	providers := bundleRootHookProviders(t, "claude", "hooks/hooks.json", map[string]string{
		"hooks/hooks.json":       definition,
		"hooks/scripts/check.sh": "#!/bin/sh\necho sibling\n",
	})
	resolved := nativeSupportResolve(t, hcc.TargetFactory, false, providers)
	modes := config.MaterializationModes{Project: config.MaterializationModeFile, Machine: config.MaterializationModeFile}
	report, err := Run(context.Background(), repo, resolved, WithMaterializationModes(modes))
	if err != nil || report == nil {
		t.Fatalf("run: %v", err)
	}
	var missing *diag.Diagnostic
	for i, d := range report.Diags {
		if d.Code == "hook.support-reference-missing" {
			missing = &report.Diags[i]
		}
	}
	if missing == nil || missing.Severity != diag.SeverityError {
		t.Fatalf("missing-reference diagnostic = %+v, want hook.support-reference-missing error", report.Diags)
	}
	for _, protected := range []string{"absent.sh", "CLAUDE_PLUGIN_ROOT", "scripts/check.sh"} {
		if strings.Contains(missing.Message, protected) || strings.Contains(missing.Hint, protected) {
			t.Fatalf("diagnostic leaks a command or path value %q: %+v", protected, *missing)
		}
	}
	nativePath := filepath.Join(repo, ".factory", "hooks.json")
	data, err := os.ReadFile(nativePath)
	if err != nil {
		t.Fatalf("sibling hook did not install: %v", err)
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	if _, ok := document["PostToolUse"]; !ok {
		t.Fatalf("sibling event missing from %s: %s", nativePath, data)
	}
	if _, ok := document["PreToolUse"]; ok {
		t.Fatalf("refused hook event present in %s: %s", nativePath, data)
	}
}

// spec: hook-resources/bare-and-dynamic-plugin-root-references-are-not-checked
func TestHookBundleRootBareAndDynamicReferencesUnchecked(t *testing.T) {
	repo, _ := additiveSafetyEnv(t)
	definition := `{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"cd \"${CLAUDE_PLUGIN_ROOT}\" && \"${CLAUDE_PLUGIN_ROOT}/hooks/$SCRIPT\" && ${CLAUDE_PLUGIN_ROOT}/hooks/*.sh && ${CLAUDE_PLUGIN_ROOT}/missing/$SCRIPT"}]}]}`
	providers := bundleRootHookProviders(t, "claude", "hooks/hooks.json", map[string]string{
		"hooks/hooks.json":       definition,
		"hooks/scripts/check.sh": "#!/bin/sh\necho support\n",
	})
	resolved := nativeSupportResolve(t, hcc.TargetFactory, false, providers)
	modes := config.MaterializationModes{Project: config.MaterializationModeFile, Machine: config.MaterializationModeFile}
	report, err := Run(context.Background(), repo, resolved, WithMaterializationModes(modes))
	if err != nil || report == nil {
		t.Fatalf("run: %v", err)
	}
	if reportHasDiagCode(report, "hook.support-reference-missing", diag.SeverityError) {
		t.Fatalf("bare or dynamic references were checked: %+v", report.Diags)
	}
	loc := nativeSupportLocationFor(t, hcc.TargetFactory)
	commands := nativeSupportCommands(t, loc, filepath.Join(repo, ".factory", "hooks.json"), 1)
	command := commands[0]
	if strings.Contains(command, "PLUGIN_ROOT") {
		t.Fatalf("plugin-root token survived in %q", command)
	}
	if !strings.HasPrefix(command, "cd ") || !strings.Contains(command, "/$SCRIPT") || !strings.Contains(command, "/*.sh") {
		t.Fatalf("command %q did not rewrite the token and keep the dynamic paths", command)
	}
}

// spec: hook-resources/legacy-folder-relative-references-keep-working-with-a-warning
func TestHookBundleRootLegacyReferenceRewrittenWithWarning(t *testing.T) {
	repo, home := additiveSafetyEnv(t)
	provider := nativeSupportSource(t, "claude", "hooks/hooks.json", bundleRootHookFiles("${CLAUDE_PLUGIN_ROOT}/scripts/check.sh", nil))
	resolved := nativeSupportResolve(t, hcc.TargetFactory, false, []resolver.ResolvedArtifact{provider})
	modes := config.MaterializationModes{Project: config.MaterializationModeFile, Machine: config.MaterializationModeFile}
	report, err := Run(context.Background(), repo, resolved, WithMaterializationModes(modes))
	if err != nil || report == nil {
		t.Fatalf("run: %v", err)
	}
	var legacy *diag.Diagnostic
	for i, d := range report.Diags {
		if d.Code == "hook.support-reference-legacy" {
			legacy = &report.Diags[i]
		}
	}
	if legacy == nil || legacy.Severity != diag.SeverityWarning {
		t.Fatalf("legacy diagnostic = %+v, want hook.support-reference-legacy warning", report.Diags)
	}
	for _, protected := range []string{"check.sh", "CLAUDE_PLUGIN_ROOT"} {
		if strings.Contains(legacy.Message, protected) || strings.Contains(legacy.Hint, protected) {
			t.Fatalf("legacy diagnostic leaks a command or path value %q: %+v", protected, *legacy)
		}
	}
	loc := nativeSupportLocationFor(t, hcc.TargetFactory)
	commands := nativeSupportCommands(t, loc, filepath.Join(repo, ".factory", "hooks.json"), 1)
	installed, _ := loc.installedRef(t, commands[0], repo, home, false)
	if filepath.Base(installed) != "check.sh" || filepath.Base(filepath.Dir(installed)) != "scripts" ||
		filepath.Base(filepath.Dir(filepath.Dir(installed))) != "hooks" {
		t.Fatalf("legacy command %q is not <support>/hooks/scripts/check.sh", commands[0])
	}
	if _, err := os.Stat(installed); err != nil {
		t.Fatalf("legacy command %q does not reference an installed file: %v", commands[0], err)
	}
	if !strings.Contains(legacy.Hint, "hooks/") {
		t.Fatalf("legacy hint %q does not name the hooks/ bundle prefix", legacy.Hint)
	}

	workspace, _ := additiveSafetyEnv(t)
	workspaceProvider := nativeSupportSource(t, "claude", ".claude/settings.json", map[string]string{
		".claude/settings.json":    `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"${CLAUDE_PLUGIN_ROOT}/scripts/check.sh"}]}]}}`,
		".claude/scripts/check.sh": "#!/bin/sh\necho workspace support\n",
	})
	workspaceResolved := nativeSupportResolve(t, hcc.TargetFactory, false, []resolver.ResolvedArtifact{workspaceProvider})
	workspaceReport, err := Run(context.Background(), workspace, workspaceResolved, WithMaterializationModes(modes))
	if err != nil || workspaceReport == nil {
		t.Fatalf("run workspace: %v", err)
	}
	workspaceLegacy := findDiagnostic(workspaceReport, "hook.support-reference-legacy")
	if workspaceLegacy == nil || workspaceLegacy.Severity != diag.SeverityWarning {
		t.Fatalf("workspace legacy diagnostic = %+v, want hook.support-reference-legacy warning", workspaceReport.Diags)
	}
	if !strings.Contains(workspaceLegacy.Hint, ".claude/") {
		t.Fatalf("workspace legacy hint %q does not name the .claude/ bundle prefix", workspaceLegacy.Hint)
	}
}

func findDiagnostic(report *Report, code string) *diag.Diagnostic {
	for i, d := range report.Diags {
		if d.Code == code {
			return &report.Diags[i]
		}
	}
	return nil
}

// spec: hook-resources/quoting-only-the-plugin-root-token-keeps-the-reference-checked
func TestHookBundleRootQuotedTokenKeepsReferenceChecked(t *testing.T) {
	repo, home := additiveSafetyEnv(t)
	definition := `{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"\"${CLAUDE_PLUGIN_ROOT}\"/hooks/scripts/check.sh"}]}],` +
		`"PostToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"\"${CLAUDE_PLUGIN_ROOT}\"/scripts/check.sh"}]}],` +
		`"Stop":[{"hooks":[{"type":"command","command":"\"${CLAUDE_PLUGIN_ROOT}\"/hooks/scripts/absent.sh"}]}]}`
	providers := bundleRootHookProviders(t, "claude", "hooks/hooks.json", map[string]string{
		"hooks/hooks.json":       definition,
		"hooks/scripts/check.sh": "#!/bin/sh\necho quoted\n",
	})
	resolved := nativeSupportResolve(t, hcc.TargetFactory, false, providers)
	modes := config.MaterializationModes{Project: config.MaterializationModeFile, Machine: config.MaterializationModeFile}
	report, err := Run(context.Background(), repo, resolved, WithMaterializationModes(modes))
	if err != nil || report == nil {
		t.Fatalf("run: %v", err)
	}
	if !reportHasDiagCode(report, "hook.support-reference-missing", diag.SeverityError) {
		t.Fatalf("quoted missing reference not refused: %+v", report.Diags)
	}
	if !reportHasDiagCode(report, "hook.support-reference-legacy", diag.SeverityWarning) {
		t.Fatalf("quoted legacy reference not warned: %+v", report.Diags)
	}
	nativePath := filepath.Join(repo, ".factory", "hooks.json")
	data, err := os.ReadFile(nativePath)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string][]struct {
		Hooks []struct {
			Command string `json:"command"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	if _, ok := document["Stop"]; ok {
		t.Fatalf("refused sibling hook present in %s: %s", nativePath, data)
	}
	loc := nativeSupportLocationFor(t, hcc.TargetFactory)
	for _, event := range []string{"PreToolUse", "PostToolUse"} {
		groups := document[event]
		if len(groups) != 1 || len(groups[0].Hooks) != 1 {
			t.Fatalf("event %s = %+v, want one command", event, groups)
		}
		installed, _ := loc.installedRef(t, groups[0].Hooks[0].Command, repo, home, false)
		if !strings.HasSuffix(filepath.ToSlash(installed), "hooks/scripts/check.sh") {
			t.Fatalf("event %s command %q does not reference hooks/scripts/check.sh", event, groups[0].Hooks[0].Command)
		}
		if _, err := os.Stat(installed); err != nil {
			t.Fatalf("event %s command %q does not reference an installed file: %v", event, groups[0].Hooks[0].Command, err)
		}
	}
}

// spec: hook-resources/references-that-resolve-under-both-meanings-use-the-bundle-root
func TestHookBundleRootBothMeaningsUseBundleRoot(t *testing.T) {
	repo, home := additiveSafetyEnv(t)
	provider := nativeSupportSource(t, "claude", "hooks/hooks.json", map[string]string{
		"hooks/hooks.json":     `{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"${CLAUDE_PLUGIN_ROOT}/hooks/check.sh"}]}]}`,
		"hooks/check.sh":       "echo bundle\n",
		"hooks/hooks/check.sh": "echo legacy\n",
	})
	resolved := nativeSupportResolve(t, hcc.TargetFactory, false, []resolver.ResolvedArtifact{provider})
	modes := config.MaterializationModes{Project: config.MaterializationModeFile, Machine: config.MaterializationModeFile}
	report, err := Run(context.Background(), repo, resolved, WithMaterializationModes(modes))
	if err != nil || report == nil {
		t.Fatalf("run: %v", err)
	}
	if reportHasDiagCode(report, "hook.support-reference-legacy", diag.SeverityWarning) {
		t.Fatalf("bundle-resolving reference reported legacy: %+v", report.Diags)
	}
	loc := nativeSupportLocationFor(t, hcc.TargetFactory)
	commands := nativeSupportCommands(t, loc, filepath.Join(repo, ".factory", "hooks.json"), 1)
	installed, _ := loc.installedRef(t, commands[0], repo, home, false)
	data, err := os.ReadFile(installed)
	if err != nil || string(data) != "echo bundle\n" {
		t.Fatalf("command %q references %q / %v, want the bundle-meaning file", commands[0], data, err)
	}
	if filepath.Base(filepath.Dir(installed)) != "hooks" && filepath.Base(filepath.Dir(filepath.Dir(installed))) != "hooks" {
		t.Fatalf("command %q resolved under the legacy meaning", commands[0])
	}
}

// regressSupportLayout simulates support installed by an earlier sync: it
// moves newFile back to the folder-relative oldFile and repoints the portable
// manifest entry.
func regressSupportLayout(t *testing.T, repo, factoryRoot, supportDir, newFile, oldFile string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(oldFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(newFile, oldFile); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(supportDir, "hooks")); err != nil {
		t.Fatal(err)
	}
	manifest, err := loadPortableState(repo, false)
	if err != nil {
		t.Fatal(err)
	}
	rel := func(p string) string {
		r, err := filepath.Rel(factoryRoot, p)
		if err != nil {
			t.Fatal(err)
		}
		return filepath.ToSlash(r)
	}
	entry, ok := manifest.entry(hcc.TargetFactory, rel(newFile))
	if !ok {
		t.Fatalf("seed manifest lacks %q", rel(newFile))
	}
	manifest.add(hcc.TargetFactory, rel(oldFile), entry.Digest)
	manifest.remove(hcc.TargetFactory, rel(newFile))
	if err := manifest.save(false); err != nil {
		t.Fatal(err)
	}
}

// spec: hook-resources/folder-relative-support-from-an-earlier-sync-moves-in-one-sync
func TestHookBundleRootFolderRelativeSupportMovesInOneSync(t *testing.T) {
	repo, _ := additiveSafetyEnv(t)
	provider := nativeSupportSource(t, "claude", "hooks/hooks.json", bundleRootHookFiles("${CLAUDE_PLUGIN_ROOT}/hooks/scripts/check.sh", nil))
	resolved := nativeSupportResolve(t, hcc.TargetFactory, false, []resolver.ResolvedArtifact{provider})
	modes := config.MaterializationModes{Project: config.MaterializationModeFile, Machine: config.MaterializationModeFile}
	report, err := Run(context.Background(), repo, resolved, WithMaterializationModes(modes))
	if err != nil || report == nil || hasErrorDiagnostics(report.Diags) {
		t.Fatalf("seed run: %v / %v", err, report)
	}
	factoryRoot := filepath.Join(repo, ".factory")
	nativePath := filepath.Join(factoryRoot, "hooks.json")
	supportDirs := hookSupportGlob(t, factoryRoot, "hook-support", "*", "*")
	if len(supportDirs) != 1 {
		t.Fatalf("support dirs = %v, want one", supportDirs)
	}
	supportDir := supportDirs[0]
	newFile := filepath.Join(supportDir, "hooks", "scripts", "check.sh")
	oldFile := filepath.Join(supportDir, "scripts", "check.sh")
	if _, err := os.Stat(newFile); err != nil {
		t.Fatalf("seed support missing at %s: %v", newFile, err)
	}
	unownedPath := filepath.Join(supportDir, "unowned.txt")
	additiveSafetyWrite(t, unownedPath, "keep me\n")

	regressSupportLayout(t, repo, factoryRoot, supportDir, newFile, oldFile)
	nativeBytes, err := os.ReadFile(nativePath)
	if err != nil {
		t.Fatal(err)
	}
	oldBytes := bytes.Replace(nativeBytes, []byte("/hooks/scripts/check.sh"), []byte("/scripts/check.sh"), 1)
	if bytes.Equal(oldBytes, nativeBytes) {
		t.Fatal("native command did not contain the bundle-relative support path")
	}
	var document struct {
		PreToolUse []json.RawMessage `json:"PreToolUse"`
	}
	if err := json.Unmarshal(oldBytes, &document); err != nil || len(document.PreToolUse) != 1 {
		t.Fatalf("decode native file: %v / %d", err, len(document.PreToolUse))
	}
	rule, err := canonicalOwnedJSON(document.PreToolUse[0])
	if err != nil {
		t.Fatal(err)
	}
	plans, _ := prepareHookSupport([]skillTarget{{skill: resolved[0], target: hcc.TargetFactory}})
	store, err := ownership.Open(factoryRoot)
	if err != nil {
		t.Fatal(err)
	}
	record, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Apply(ownership.Mutation{
		Rel: "hooks.json", AbsFile: nativePath, NextBytes: oldBytes,
		Entries:               []ownership.Entry{nativeHookProofEntry(t, "PreToolUse", string(rule), resolved[0].Name, plans[0].sourceDigest)},
		Reason:                ownership.ReasonSync,
		ExpectPriorGeneration: record.Generation, ExpectPriorDigest: ownership.Digest(nativeBytes),
	}); err != nil {
		t.Fatal(err)
	}

	check, err := Run(context.Background(), repo, resolved, WithMaterializationModes(modes), WithDryRun())
	if err != nil || check == nil {
		t.Fatalf("check run: %v", err)
	}
	if !reportHasHookSupportWrite(check) && len(check.HookChanges) == 0 {
		t.Fatalf("check reported no pending change: %+v", check)
	}
	if _, err := os.Lstat(oldFile); err != nil {
		t.Fatalf("check removed %s: %v", oldFile, err)
	}
	if _, err := os.Lstat(newFile); !os.IsNotExist(err) {
		t.Fatalf("check wrote %s", newFile)
	}
	after, err := os.ReadFile(nativePath)
	if err != nil || !bytes.Equal(after, oldBytes) {
		t.Fatalf("check rewrote the native file: %v", err)
	}

	report, err = Run(context.Background(), repo, resolved, WithMaterializationModes(modes))
	if err != nil || report == nil || hasErrorDiagnostics(report.Diags) {
		t.Fatalf("move run: %v / %v", err, report)
	}
	if _, err := os.Lstat(oldFile); !os.IsNotExist(err) {
		t.Fatalf("folder-relative support %s was not pruned", oldFile)
	}
	data, err := os.ReadFile(newFile)
	if err != nil || string(data) != "#!/bin/sh\necho bundle support\n" {
		t.Fatalf("bundle-relative support = %q / %v", data, err)
	}
	if got, err := os.ReadFile(unownedPath); err != nil || string(got) != "keep me\n" {
		t.Fatalf("unowned support file changed: %q / %v", got, err)
	}
	var reviewed bool
	for _, change := range report.HookChanges {
		if bytes.Contains(change.Before, []byte("/scripts/check.sh")) && !bytes.Contains(change.Before, []byte("/hooks/scripts/check.sh")) &&
			bytes.Contains(change.After, []byte("/hooks/scripts/check.sh")) {
			reviewed = true
		}
	}
	if !reviewed {
		t.Fatalf("hook review did not report the command change: %+v", report.HookChanges)
	}
	commands := nativeSupportCommands(t, nativeSupportLocationFor(t, hcc.TargetFactory), nativePath, 1)
	if !strings.HasSuffix(commands[0], "/hooks/scripts/check.sh") {
		t.Fatalf("command after move = %q, want bundle-relative installed path", commands[0])
	}
}

// spec: hook-resources/folder-relative-support-from-an-earlier-sync-moves-in-one-sync
func TestHookBundleRootInterruptedSupportMoveRecovers(t *testing.T) {
	repo, _ := additiveSafetyEnv(t)
	provider := nativeSupportSource(t, "claude", "hooks/hooks.json", bundleRootHookFiles("${CLAUDE_PLUGIN_ROOT}/hooks/scripts/check.sh", nil))
	resolved := nativeSupportResolve(t, hcc.TargetFactory, false, []resolver.ResolvedArtifact{provider})
	modes := config.MaterializationModes{Project: config.MaterializationModeFile, Machine: config.MaterializationModeFile}
	report, err := Run(context.Background(), repo, resolved, WithMaterializationModes(modes))
	if err != nil || report == nil || hasErrorDiagnostics(report.Diags) {
		t.Fatalf("seed run: %v / %v", err, report)
	}
	factoryRoot := filepath.Join(repo, ".factory")
	supportDirs := hookSupportGlob(t, factoryRoot, "hook-support", "*", "*")
	if len(supportDirs) != 1 {
		t.Fatalf("support dirs = %v", supportDirs)
	}
	supportDir := supportDirs[0]
	newFile := filepath.Join(supportDir, "hooks", "scripts", "check.sh")
	oldFile := filepath.Join(supportDir, "scripts", "check.sh")
	regressSupportLayout(t, repo, factoryRoot, supportDir, newFile, oldFile)

	stateDir, err := dependencytxn.StateDir()
	if err != nil {
		t.Fatal(err)
	}
	interrupted := errors.New("interrupted support move")
	var faultCalled bool
	fault := func(point dependencytxn.FaultPoint) error {
		if point.Stage != dependencytxn.FaultAfterJournal {
			return nil
		}
		faultCalled = true
		return interrupted
	}
	report, err = Run(context.Background(), repo, resolved, WithMaterializationModes(modes), withNativeHookSupportCommitFault(fault))
	if err == nil && (report == nil || !hasErrorDiagnostics(report.Diags)) {
		t.Fatalf("interrupted move = %v / %v, want failure", err, report)
	}
	if !faultCalled {
		t.Fatal("dependency transaction fault point was not reached by the support move")
	}
	readOnly, err := Run(context.Background(), repo, resolved, WithMaterializationModes(modes), WithDryRun())
	if err == nil && (readOnly == nil || !hasErrorDiagnostics(readOnly.Diags)) {
		t.Fatalf("read-only run during pending move = %v / %v, want refusal", err, readOnly)
	}
	if _, err := os.Lstat(oldFile); err != nil {
		t.Fatalf("interrupted move lost %s: %v", oldFile, err)
	}
	report, err = Run(context.Background(), repo, resolved, WithMaterializationModes(modes))
	if err != nil || report == nil || hasErrorDiagnostics(report.Diags) {
		t.Fatalf("recovering move run: %v / %v", err, report)
	}
	if err := dependencytxn.CheckPending(stateDir); err != nil {
		t.Fatalf("recovered move still pending: %v", err)
	}
	if _, err := os.Lstat(oldFile); !os.IsNotExist(err) {
		t.Fatalf("folder-relative support %s survived recovery", oldFile)
	}
	if _, err := os.Stat(newFile); err != nil {
		t.Fatalf("bundle-relative support missing after recovery: %v", err)
	}
	var staging []string
	supportRoot := filepath.Join(repo, ".factory", "hook-support")
	if err := filepath.WalkDir(supportRoot, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() {
			return walkErr
		}
		if strings.HasPrefix(d.Name(), ".dependencytxn-") && strings.HasSuffix(d.Name(), ".tmp") {
			staging = append(staging, p)
		}
		return nil
	}); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if len(staging) != 0 {
		t.Fatalf("staging residue after recovery: %v", staging)
	}
}

// spec: hook-resources/a-reference-to-a-missing-support-file-refuses-the-hook
// spec: hook-resources/legacy-folder-relative-references-keep-working-with-a-warning
func TestHookBundleRootResourceFileLaneReferenceDiagnostics(t *testing.T) {
	repo := openCodeSandbox(t)
	missing := additiveSafetyProvider(t, hcc.TargetOpenCode, false, `${DROID_PLUGIN_ROOT}/absent.sh`, "#!/bin/sh\necho support\n")
	missing.Name = "missing-guard"
	legacy := additiveSafetyProvider(t, hcc.TargetOpenCode, false, `${DROID_PLUGIN_ROOT}/shared.sh`, "#!/bin/sh\necho support\n")
	legacy.Name = "legacy-guard"
	report, err := Run(t.Context(), repo, []resolver.ResolvedArtifact{missing, legacy}, WithMaterializationModes(config.MaterializationModes{Project: config.MaterializationModeFile, Machine: config.MaterializationModeFile}))
	if err != nil || report == nil {
		t.Fatalf("run: %v", err)
	}
	var missingDiag, legacyDiag *diag.Diagnostic
	for i, d := range report.Diags {
		switch d.Code {
		case "hook.support-reference-missing":
			missingDiag = &report.Diags[i]
		case "hook.support-reference-legacy":
			legacyDiag = &report.Diags[i]
		}
	}
	if missingDiag == nil || missingDiag.Severity != diag.SeverityError || missingDiag.Resource != "missing-guard" {
		t.Fatalf("missing-reference diagnostic = %+v, want hook.support-reference-missing error on missing-guard", report.Diags)
	}
	if legacyDiag == nil || legacyDiag.Severity != diag.SeverityWarning || legacyDiag.Resource != "legacy-guard" {
		t.Fatalf("legacy diagnostic = %+v, want hook.support-reference-legacy warning on legacy-guard", report.Diags)
	}
	for _, protected := range []string{"absent.sh", "shared.sh", "DROID_PLUGIN_ROOT"} {
		for _, d := range []*diag.Diagnostic{missingDiag, legacyDiag} {
			if strings.Contains(d.Message, protected) || strings.Contains(d.Hint, protected) {
				t.Fatalf("diagnostic leaks a command or path value %q: %+v", protected, *d)
			}
		}
	}
	root := openCodeSupportRoot(t, repo, false)
	declarations, err := filepath.Glob(filepath.Join(root, "hooks", "agent-marketplace", "*.json"))
	if err != nil || len(declarations) != 1 {
		t.Fatalf("installed declarations = %v %v, want the sibling only", declarations, err)
	}
	declaration := openCodeOnlyDeclaration(t, root)
	if !strings.Contains(declaration.Command, "/hooks/shared.sh") {
		t.Fatalf("sibling command %q did not rewrite the legacy reference to the bundle-root path", declaration.Command)
	}
	files := openCodeSupportFiles(t, root)
	var supportRel string
	for rel := range files {
		supportRel = rel
	}
	if !strings.HasSuffix(supportRel, "/hooks/shared.sh") {
		t.Fatalf("installed support = %q, want .../hooks/shared.sh", supportRel)
	}
}
