package materialize

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

func seedOwnedFactoryRetiredHooksJSON(t *testing.T, factoryRoot, resourceName, sourceDigest, rawDoc string, ownedRules map[string][]string) string {
	t.Helper()
	hooksPath := filepath.Join(factoryRoot, "hooks.json")
	additiveSafetyWrite(t, hooksPath, rawDoc)
	if err := os.Chmod(hooksPath, 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := ownership.Open(factoryRoot)
	if err != nil {
		t.Fatalf("open ownership store: %v", err)
	}
	var entries []ownership.Entry
	for event, rules := range ownedRules {
		for _, rule := range rules {
			entries = append(entries, nativeHookProofEntry(t, event, rule, resourceName, sourceDigest))
		}
	}
	if _, err := store.Apply(ownership.Mutation{
		Rel:               "hooks.json",
		AbsFile:           hooksPath,
		NextBytes:         []byte(rawDoc),
		Entries:           entries,
		Reason:            ownership.ReasonSync,
		ExpectPriorDigest: ownership.Digest([]byte(rawDoc)),
	}); err != nil {
		t.Fatalf("seed retired hooks.json ownership: %v", err)
	}
	return hooksPath
}

// spec: hook-resources/factory-droid-machine-scope-hooks-materialize-into-settings-local-json
func TestFactoryMachineHooksWriteSettingsLocalWithoutShadowingSettingsJSON(t *testing.T) {
	repo, home := additiveSafetyEnv(t)
	factoryRoot := filepath.Join(home, ".factory")
	settingsPath := filepath.Join(factoryRoot, "settings.json")
	settingsBody := "{\n  \"hooks\": {\n    \"PreToolUse\": [\n      {\n        \"matcher\": \"Execute\",\n        \"hooks\": [\n          {\"type\": \"command\", \"command\": \"echo existing-settings-hook\"}\n        ]\n      }\n    ]\n  }\n}\n"
	additiveSafetyWrite(t, settingsPath, settingsBody)
	settingsBefore, settingsMode, settingsMtime := fileSnapshot(t, settingsPath)

	provider := additiveSafetyProvider(t, hcc.TargetFactory, true, "echo managed-machine-hook", "")
	report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{provider})
	if err != nil || hasErrorDiagnostics(report.Diags) {
		t.Fatalf("Run: %v / %v", err, report)
	}

	settingsLocalPath := filepath.Join(factoryRoot, "settings.local.json")
	data, err := os.ReadFile(settingsLocalPath)
	if err != nil {
		t.Fatalf("read settings.local.json: %v", err)
	}
	if bytes.Contains(data, []byte("_managed_by")) {
		t.Fatalf("settings.local.json carries _managed_by marker:\n%s", data)
	}
	if got := additiveSafetyCommands(t, settingsLocalPath, "hooks", "PreToolUse"); !equalStrings(got, []string{"echo managed-machine-hook"}) {
		t.Fatalf("settings.local.json PreToolUse commands = %v, want [echo managed-machine-hook]", got)
	}
	if _, err := os.Lstat(filepath.Join(factoryRoot, "hooks.json")); !os.IsNotExist(err) {
		t.Fatalf("hooks.json should not be created: %v", err)
	}
	assertFileUnchanged(t, settingsPath, settingsBefore, settingsMode, settingsMtime)

	// Repeat sync is byte-identical.
	localBefore, localMode, localMtime := fileSnapshot(t, settingsLocalPath)
	report, err = Run(context.Background(), repo, []resolver.ResolvedArtifact{provider})
	if err != nil || hasErrorDiagnostics(report.Diags) {
		t.Fatalf("repeat Run: %v / %v", err, report)
	}
	assertFileUnchanged(t, settingsLocalPath, localBefore, localMode, localMtime)
}

// spec: hook-resources/owned-machine-scope-factory-hooks-migrate-out-of-hooks-json
func TestFactoryMachineOwnedRetiredHooksJSONMigratesAndDeletesWhenEmpty(t *testing.T) {
	repo, home := additiveSafetyEnv(t)
	factoryRoot := filepath.Join(home, ".factory")
	ownedRule := `{"hooks":[{"command":"echo old-owned-hook","type":"command"}],"matcher":"Execute"}`
	retiredDoc := "{\n  \"PreToolUse\": [" + ownedRule + "]\n}\n"
	retiredPath := seedOwnedFactoryRetiredHooksJSON(t, factoryRoot, "PreToolUse-command", "sha256:"+strings.Repeat("a", 64), retiredDoc, map[string][]string{
		"PreToolUse": {ownedRule},
	})
	retiredBefore, _, _ := fileSnapshot(t, retiredPath)

	provider := additiveSafetyProvider(t, hcc.TargetFactory, true, "echo migrated-hook", "")
	report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{provider})
	if err != nil || hasErrorDiagnostics(report.Diags) {
		t.Fatalf("Run: %v / %v", err, report)
	}

	settingsLocalPath := filepath.Join(factoryRoot, "settings.local.json")
	if got := additiveSafetyCommands(t, settingsLocalPath, "hooks", "PreToolUse"); !equalStrings(got, []string{"echo migrated-hook"}) {
		t.Fatalf("settings.local.json PreToolUse commands = %v, want [echo migrated-hook]", got)
	}
	if _, err := os.Lstat(retiredPath); !os.IsNotExist(err) {
		t.Fatalf("empty retired hooks.json was not deleted: %v", err)
	}
	canonicalFactoryRoot, err := ownership.CanonicalRoot(factoryRoot)
	if err != nil {
		t.Fatalf("canonical factory root: %v", err)
	}
	assertNativeHookMigrationBackup(t, canonicalFactoryRoot, retiredPath, retiredBefore)

	store, err := ownership.Open(factoryRoot)
	if err != nil {
		t.Fatalf("open ownership store: %v", err)
	}
	retiredProof, err := store.Proof("hooks.json")
	if err != nil {
		t.Fatalf("Proof(hooks.json): %v", err)
	}
	if retiredProof.OwnedCount() != 0 || len(retiredProof.HookSatisfactions()) != 0 {
		t.Fatalf("retired hooks.json still has ownership records: owned=%d sats=%d", retiredProof.OwnedCount(), len(retiredProof.HookSatisfactions()))
	}
	activeProof, err := store.Proof("settings.local.json")
	if err != nil || activeProof.OwnedCount() != 1 {
		t.Fatalf("settings.local.json ownership proof = %d / %v, want 1", activeProof.OwnedCount(), err)
	}
}

// spec: hook-resources/handwritten-entries-in-retired-factory-hooks-json-stay-intact-without-empty-event-arrays
func TestFactoryMachineRetiredHooksJSONPreservesHandwrittenAndRemovesEmptyEvents(t *testing.T) {
	repo, home := additiveSafetyEnv(t)
	factoryRoot := filepath.Join(home, ".factory")
	ownedPre := `{"hooks":[{"command":"echo owned-pre","type":"command"}],"matcher":"Execute"}`
	handwrittenStop := `{ "hooks" : [ { "type" : "command" , "command" : "echo handwritten-stop" } ] }`
	retiredDoc := "{\n  \"PreToolUse\": [\n    " + ownedPre + "\n  ],\n  // keep handwritten hooks.json comment\n  \"Stop\": [\n    " + handwrittenStop + "\n  ]\n}\n"
	retiredPath := seedOwnedFactoryRetiredHooksJSON(t, factoryRoot, "PreToolUse-command", "sha256:"+strings.Repeat("b", 64), retiredDoc, map[string][]string{
		"PreToolUse": {ownedPre},
	})

	provider := additiveSafetyProvider(t, hcc.TargetFactory, true, "echo migrated-pre", "")
	report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{provider})
	if err != nil || hasErrorDiagnostics(report.Diags) {
		t.Fatalf("Run: %v / %v", err, report)
	}

	settingsLocalPath := filepath.Join(factoryRoot, "settings.local.json")
	if got := additiveSafetyCommands(t, settingsLocalPath, "hooks", "PreToolUse"); !equalStrings(got, []string{"echo migrated-pre"}) {
		t.Fatalf("settings.local.json PreToolUse commands = %v, want [echo migrated-pre]", got)
	}
	retiredAfter, err := os.ReadFile(retiredPath)
	if err != nil {
		t.Fatalf("read retired hooks.json: %v", err)
	}
	if !bytes.Contains(retiredAfter, []byte(handwrittenStop)) || !bytes.Contains(retiredAfter, []byte("// keep handwritten hooks.json comment")) {
		t.Fatalf("handwritten entry or comment lost from hooks.json:\n%s", retiredAfter)
	}
	if bytes.Contains(retiredAfter, []byte("PreToolUse")) {
		t.Fatalf("empty PreToolUse event key must be removed from hooks.json so it does not clear settings.json:\n%s", retiredAfter)
	}
}

// spec: hook-resources/deselecting-all-machine-scope-factory-hooks-still-prunes-retired-hooks-json
func TestFactoryMachineDeselectAllHooksPrunesRetiredHooksJSON(t *testing.T) {
	repo, home := additiveSafetyEnv(t)
	factoryRoot := filepath.Join(home, ".factory")
	ownedRule := `{"hooks":[{"command":"echo only-owned-hook","type":"command"}],"matcher":"Execute"}`
	retiredDoc := "{\n  \"PreToolUse\": [" + ownedRule + "]\n}\n"
	retiredPath := seedOwnedFactoryRetiredHooksJSON(t, factoryRoot, "PreToolUse-command", "sha256:"+strings.Repeat("c", 64), retiredDoc, map[string][]string{
		"PreToolUse": {ownedRule},
	})

	report, err := Run(context.Background(), repo, nil)
	if err != nil || hasErrorDiagnostics(report.Diags) {
		t.Fatalf("Run with empty selections: %v / %v", err, report)
	}
	if _, err := os.Lstat(retiredPath); !os.IsNotExist(err) {
		t.Fatalf("retired hooks.json still exists after deselecting all hooks: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(factoryRoot, "settings.local.json")); !os.IsNotExist(err) {
		t.Fatalf("settings.local.json should not be created when no hooks are selected: %v", err)
	}
}

// spec: hook-resources/malformed-retired-factory-hooks-json-fails-closed
func TestFactoryMachineMalformedRetiredHooksJSONFailsClosed(t *testing.T) {
	t.Run("malformed json", func(t *testing.T) {
		repo, home := additiveSafetyEnv(t)
		factoryRoot := filepath.Join(home, ".factory")
		retiredPath := filepath.Join(factoryRoot, "hooks.json")
		badDoc := "{\"PreToolUse\": [broken}\n"
		additiveSafetyWrite(t, retiredPath, badDoc)
		before, mode, mtime := fileSnapshot(t, retiredPath)

		provider := additiveSafetyProvider(t, hcc.TargetFactory, true, "echo should-not-write", "")
		report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{provider})
		if err != nil {
			t.Fatalf("unexpected fatal error: %v", err)
		}
		if !diag.HasCode(report.Diags, diag.CodeHookNativeToleranceRefused) {
			t.Fatalf("diagnostics = %+v, want %s", report.Diags, diag.CodeHookNativeToleranceRefused)
		}
		assertFileUnchanged(t, retiredPath, before, mode, mtime)
		if _, err := os.Lstat(filepath.Join(factoryRoot, "settings.local.json")); !os.IsNotExist(err) {
			t.Fatalf("settings.local.json was created despite malformed retired hooks.json: %v", err)
		}
	})

	t.Run("corrupt ownership state", func(t *testing.T) {
		repo, home := additiveSafetyEnv(t)
		factoryRoot := filepath.Join(home, ".factory")
		retiredPath := filepath.Join(factoryRoot, "hooks.json")
		retiredDoc := "{\"PreToolUse\":[{\"matcher\":\"Execute\",\"hooks\":[{\"type\":\"command\",\"command\":\"echo keep-me\"}]}]}\n"
		additiveSafetyWrite(t, retiredPath, retiredDoc)
		before, mode, mtime := fileSnapshot(t, retiredPath)

		store, err := ownership.Open(factoryRoot)
		if err != nil {
			t.Fatalf("open ownership store: %v", err)
		}
		additiveSafetyWrite(t, store.StateFile(), "{corrupt ownership json")

		provider := additiveSafetyProvider(t, hcc.TargetFactory, true, "echo should-not-write", "")
		report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{provider})
		if err != nil {
			t.Fatalf("unexpected fatal error: %v", err)
		}
		if !hasErrorDiagnostics(report.Diags) {
			t.Fatalf("expected error diagnostic on corrupt ownership state, got: %+v", report.Diags)
		}
		assertFileUnchanged(t, retiredPath, before, mode, mtime)
		if _, err := os.Lstat(filepath.Join(factoryRoot, "settings.local.json")); !os.IsNotExist(err) {
			t.Fatalf("settings.local.json was created despite corrupt ownership state: %v", err)
		}
	})
}

// spec: hook-resources/legacy-marked-factory-machine-hooks-migrate-to-settings-local-json
func TestFactoryMachineLegacyMarkedTopLevelHooksMigrateToSettingsLocal(t *testing.T) {
	repo, home := additiveSafetyEnv(t)
	factoryRoot := filepath.Join(home, ".factory")
	legacyPath := filepath.Join(factoryRoot, "hooks", "hooks.json")
	legacyDoc, handwritten := nativeHookLegacyDocument(hcc.TargetFactory, "", "PreToolUse", "Execute", "echo legacy-v4", true, true)
	additiveSafetyWrite(t, legacyPath, string(legacyDoc))

	provider := additiveSafetyProvider(t, hcc.TargetFactory, true, "echo legacy-v4", "")
	report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{provider})
	if err != nil || hasErrorDiagnostics(report.Diags) {
		t.Fatalf("Run: %v / %v", err, report)
	}

	settingsLocalPath := filepath.Join(factoryRoot, "settings.local.json")
	if got := additiveSafetyCommands(t, settingsLocalPath, "hooks", "PreToolUse"); !equalStrings(got, []string{"echo legacy-v4"}) {
		t.Fatalf("settings.local.json PreToolUse commands = %v, want [echo legacy-v4]", got)
	}
	legacyAfter, err := os.ReadFile(legacyPath)
	if err != nil {
		t.Fatalf("read legacy hooks/hooks.json: %v", err)
	}
	if !bytes.Contains(legacyAfter, []byte(handwritten)) || bytes.Contains(legacyAfter, []byte("_managed_by")) {
		t.Fatalf("legacy hooks/hooks.json did not strip marked entry while keeping handwritten sibling:\n%s", legacyAfter)
	}
}

func seedForwardFactoryRepoHook(t *testing.T, repoRoot string, provider resolver.ResolvedArtifact, command, supportRel string, supportBytes []byte, supportMode os.FileMode) string {
	t.Helper()
	factoryRoot := filepath.Join(repoRoot, ".factory")
	if supportRel != "" {
		supportPath := filepath.Join(factoryRoot, filepath.FromSlash(supportRel))
		if err := os.MkdirAll(filepath.Dir(supportPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(supportPath, supportBytes, supportMode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(supportPath, supportMode); err != nil {
			t.Fatal(err)
		}
	}
	doc := map[string]any{
		"hooks": map[string]any{
			"PreToolUse": []any{
				map[string]any{
					"matcher": "Execute",
					"hooks": []any{
						map[string]any{
							"type":    "command",
							"command": command,
						},
					},
				},
			},
		},
	}
	encoded, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	encoded = append(encoded, '\n')
	settingsLocalPath := filepath.Join(factoryRoot, "settings.local.json")
	additiveSafetyWrite(t, settingsLocalPath, string(encoded))
	if err := os.Chmod(settingsLocalPath, 0o600); err != nil {
		t.Fatal(err)
	}
	return settingsLocalPath
}

// spec: hook-resources/factory-droid-repository-scope-hooks-materialize-into-settings-local-json
func TestFactoryRepoHooksWriteSettingsLocalAndPreservePluginRegistrations(t *testing.T) {
	repo, _ := additiveSafetyEnv(t)
	factoryRoot := filepath.Join(repo, ".factory")
	settingsPath := filepath.Join(factoryRoot, "settings.json")
	settingsBody := "{\n  \"hooks\": {\n    \"PreToolUse\": [\n      {\n        \"matcher\": \"Execute\",\n        \"hooks\": [\n          {\"type\": \"command\", \"command\": \"echo repo-settings-hook\"}\n        ]\n      }\n    ]\n  }\n}\n"
	additiveSafetyWrite(t, settingsPath, settingsBody)
	settingsBefore, settingsMode, settingsMtime := fileSnapshot(t, settingsPath)

	settingsLocalPath := filepath.Join(factoryRoot, "settings.local.json")
	existingLocal := "{\n  \"enabledPlugins\": {\n    \"demo@local\": true\n  },\n  \"extraKnownMarketplaces\": {\n    \"local\": {\"source\": {\"source\": \"directory\", \"path\": \"/tmp/local\"}}\n  }\n}\n"
	additiveSafetyWrite(t, settingsLocalPath, existingLocal)

	provider := additiveSafetyProvider(t, hcc.TargetFactory, false, "echo managed-repo-hook", "")
	report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{provider})
	if err != nil || hasErrorDiagnostics(report.Diags) {
		t.Fatalf("Run: %v / %v", err, report)
	}

	data, err := os.ReadFile(settingsLocalPath)
	if err != nil {
		t.Fatalf("read .factory/settings.local.json: %v", err)
	}
	if bytes.Contains(data, []byte("_managed_by")) {
		t.Fatalf(".factory/settings.local.json carries _managed_by marker:\n%s", data)
	}
	if !bytes.Contains(data, []byte(`"demo@local"`)) || !bytes.Contains(data, []byte(`"extraKnownMarketplaces"`)) {
		t.Fatalf(".factory/settings.local.json lost plugin registrations:\n%s", data)
	}
	if got := additiveSafetyCommands(t, settingsLocalPath, "hooks", "PreToolUse"); !equalStrings(got, []string{"echo managed-repo-hook"}) {
		t.Fatalf(".factory/settings.local.json PreToolUse commands = %v, want [echo managed-repo-hook]", got)
	}
	if _, err := os.Lstat(filepath.Join(factoryRoot, "hooks.json")); !os.IsNotExist(err) {
		t.Fatalf(".factory/hooks.json should not be created: %v", err)
	}
	assertFileUnchanged(t, settingsPath, settingsBefore, settingsMode, settingsMtime)
}

// spec: hook-resources/owned-repository-scope-factory-hooks-migrate-out-of-hooks-json
func TestFactoryRepoOwnedRetiredHooksJSONMigratesAndDeletesWhenEmpty(t *testing.T) {
	repo, _ := additiveSafetyEnv(t)
	factoryRoot := filepath.Join(repo, ".factory")
	ownedRule := `{"hooks":[{"command":"echo old-repo-owned","type":"command"}],"matcher":"Execute"}`
	retiredDoc := "{\n  \"PreToolUse\": [" + ownedRule + "]\n}\n"
	retiredPath := seedOwnedFactoryRetiredHooksJSON(t, factoryRoot, "PreToolUse-command", "sha256:"+strings.Repeat("d", 64), retiredDoc, map[string][]string{
		"PreToolUse": {ownedRule},
	})
	retiredBefore, _, _ := fileSnapshot(t, retiredPath)

	provider := additiveSafetyProvider(t, hcc.TargetFactory, false, "echo migrated-repo-hook", "")
	report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{provider})
	if err != nil || hasErrorDiagnostics(report.Diags) {
		t.Fatalf("Run: %v / %v", err, report)
	}

	settingsLocalPath := filepath.Join(factoryRoot, "settings.local.json")
	if got := additiveSafetyCommands(t, settingsLocalPath, "hooks", "PreToolUse"); !equalStrings(got, []string{"echo migrated-repo-hook"}) {
		t.Fatalf(".factory/settings.local.json PreToolUse commands = %v, want [echo migrated-repo-hook]", got)
	}
	if _, err := os.Lstat(retiredPath); !os.IsNotExist(err) {
		t.Fatalf("empty retired .factory/hooks.json was not deleted: %v", err)
	}
	canonicalFactoryRoot, err := ownership.CanonicalRoot(factoryRoot)
	if err != nil {
		t.Fatalf("canonical factory root: %v", err)
	}
	assertNativeHookMigrationBackup(t, canonicalFactoryRoot, retiredPath, retiredBefore)
}

// spec: hook-resources/complete-factory-settings-local-json-hooks-satisfy-repository-sync-without-writing-hooks-json
func TestFactoryRepoForwardSettingsLocalSatisfiesWithoutWritingHooksJSON(t *testing.T) {
	repo, _ := additiveSafetyEnv(t)
	provider := nativeSupportSource(t, "claude", "hooks/hooks.json", nativeSupportClaudeFiles("${CLAUDE_PLUGIN_ROOT}/scripts/check.sh"))
	if err := os.Chmod(filepath.Join(provider.RegistryURL, "hooks", "scripts", "check.sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	resolved := nativeSupportResolve(t, hcc.TargetFactory, false, []resolver.ResolvedArtifact{provider})
	plan, _ := nativeHookActivationSupportPath(t, resolved[0], hcc.TargetFactory, false, filepath.Join(repo, ".factory"))
	if len(plan.artifacts) == 0 {
		t.Fatal("expected support artifacts for source-backed hook")
	}
	factoryRoot := filepath.Join(repo, ".factory")
	for _, art := range plan.artifacts {
		full := filepath.Join(factoryRoot, filepath.FromSlash(art.Path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, art.Bytes, art.Mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(full, art.Mode); err != nil {
			t.Fatal(err)
		}
	}
	commandBase, err := hookSupportCommandBase(skillTarget{skill: resolved[0], target: hcc.TargetFactory}, repo, plan.relBase)
	if err != nil {
		t.Fatalf("hookSupportCommandBase: %v", err)
	}
	expectedCommand := `"$FACTORY_PROJECT_DIR"/` + commandBase + "/hooks/scripts/check.sh"
	settingsLocalPath := seedForwardFactoryRepoHook(t, repo, resolved[0], expectedCommand, "", nil, 0)
	localBefore, localMode, localMtime := fileSnapshot(t, settingsLocalPath)

	report, runErr := Run(context.Background(), repo, resolved, WithDryRun())
	if runErr != nil || hasErrorDiagnostics(report.Diags) {
		t.Fatalf("Run(dryRun=true): %v / %v", runErr, report)
	}
	if _, err := os.Lstat(filepath.Join(factoryRoot, "hooks.json")); !os.IsNotExist(err) {
		t.Fatalf("created .factory/hooks.json: %v", err)
	}
	assertFileUnchanged(t, settingsLocalPath, localBefore, localMode, localMtime)
}

func TestFactoryRepoForwardSettingsLocalPreservesManifestTrackedSupportFiles(t *testing.T) {
	repo, _ := additiveSafetyEnv(t)
	provider := nativeSupportSource(t, "claude", "hooks/hooks.json", nativeSupportClaudeFiles("${CLAUDE_PLUGIN_ROOT}/scripts/check.sh"))
	if err := os.Chmod(filepath.Join(provider.RegistryURL, "hooks", "scripts", "check.sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	resolved := nativeSupportResolve(t, hcc.TargetFactory, false, []resolver.ResolvedArtifact{provider})
	plan, _ := nativeHookActivationSupportPath(t, resolved[0], hcc.TargetFactory, false, filepath.Join(repo, ".factory"))
	if len(plan.artifacts) == 0 {
		t.Fatal("expected support artifacts for source-backed hook")
	}
	factoryRoot := filepath.Join(repo, ".factory")
	portableState, err := loadPortableState(repo, false)
	if err != nil {
		t.Fatalf("loadPortableState: %v", err)
	}
	var supportPath string
	for _, art := range plan.artifacts {
		full := filepath.Join(factoryRoot, filepath.FromSlash(art.Path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, art.Bytes, art.Mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(full, art.Mode); err != nil {
			t.Fatal(err)
		}
		digest, err := portablePathDigest(full)
		if err != nil {
			t.Fatalf("portablePathDigest(%s): %v", full, err)
		}
		portableState.add(hcc.TargetFactory, filepath.ToSlash(art.Path), digest)
		supportPath = full
	}
	if err := portableState.save(false); err != nil {
		t.Fatalf("save portable manifest: %v", err)
	}
	supportBefore, supportMode, supportMtime := fileSnapshot(t, supportPath)

	commandBase, err := hookSupportCommandBase(skillTarget{skill: resolved[0], target: hcc.TargetFactory}, repo, plan.relBase)
	if err != nil {
		t.Fatalf("hookSupportCommandBase: %v", err)
	}
	expectedCommand := `"$FACTORY_PROJECT_DIR"/` + commandBase + "/hooks/scripts/check.sh"
	settingsLocalPath := seedForwardFactoryRepoHook(t, repo, resolved[0], expectedCommand, "", nil, 0)
	localBefore, localMode, localMtime := fileSnapshot(t, settingsLocalPath)

	report, err := Run(context.Background(), repo, resolved, WithMaterializationModes(config.MaterializationModes{
		Project: config.MaterializationModeFile,
		Machine: config.MaterializationModeFile,
	}))
	if err != nil || hasErrorDiagnostics(report.Diags) {
		t.Fatalf("Run: %v / %v", err, report)
	}
	if _, err := os.Lstat(filepath.Join(factoryRoot, "hooks.json")); !os.IsNotExist(err) {
		t.Fatalf("created .factory/hooks.json despite forward satisfaction: %v", err)
	}
	assertFileUnchanged(t, settingsLocalPath, localBefore, localMode, localMtime)
	assertFileUnchanged(t, supportPath, supportBefore, supportMode, supportMtime)
}

// spec: hook-resources/mismatched-factory-settings-local-json-hooks-do-not-suppress-repository-hooks-json
func TestFactoryRepoForwardSettingsLocalMismatchFallsBackToHooksJSON(t *testing.T) {
	repo, _ := additiveSafetyEnv(t)
	provider := additiveSafetyProvider(t, hcc.TargetFactory, false, "echo expected-repo-hook", "")
	settingsLocalPath := seedForwardFactoryRepoHook(t, repo, provider, "echo different-command", "", nil, 0)

	report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{provider})
	if err != nil || hasErrorDiagnostics(report.Diags) {
		t.Fatalf("Run: %v / %v", err, report)
	}
	if got := additiveSafetyCommands(t, settingsLocalPath, "hooks", "PreToolUse"); !equalStrings(got, []string{"echo different-command", "echo expected-repo-hook"}) {
		t.Fatalf(".factory/settings.local.json commands = %v, want handwritten + managed", got)
	}
	if _, err := os.Lstat(filepath.Join(repo, ".factory", "hooks.json")); !os.IsNotExist(err) {
		t.Fatalf(".factory/hooks.json should not be created: %v", err)
	}
}

// spec: hook-resources/malformed-factory-settings-local-json-fails-closed-during-repository-forward-check
func TestFactoryRepoForwardMalformedSettingsLocalFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name     string
		body     string
		wantCode string
	}{
		{name: "malformed json", body: `{"hooks": {"PreToolUse": [broken}`, wantCode: diag.CodeJSONMalformed},
		{name: "duplicate key", body: `{"hooks": {"PreToolUse": []}, "hooks": {"PreToolUse": []}}`, wantCode: diag.CodeJSONDuplicateKey},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, _ := additiveSafetyEnv(t)
			settingsLocalPath := filepath.Join(repo, ".factory", "settings.local.json")
			additiveSafetyWrite(t, settingsLocalPath, tc.body)
			before, mode, mtime := fileSnapshot(t, settingsLocalPath)

			provider := additiveSafetyProvider(t, hcc.TargetFactory, false, "echo should-not-write", "")
			for _, dryRun := range []bool{true, false} {
				var opts []RunOption
				if dryRun {
					opts = append(opts, WithDryRun())
				}
				report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{provider}, opts...)
				if err != nil {
					t.Fatalf("unexpected fatal error: %v", err)
				}
				if !diag.HasCode(report.Diags, tc.wantCode) {
					t.Fatalf("dryRun=%t diagnostics = %+v, want %s", dryRun, report.Diags, tc.wantCode)
				}
				assertFileUnchanged(t, settingsLocalPath, before, mode, mtime)
				if _, err := os.Lstat(filepath.Join(repo, ".factory", "hooks.json")); !os.IsNotExist(err) {
					t.Fatalf("dryRun=%t created .factory/hooks.json on malformed settings.local.json: %v", dryRun, err)
				}
			}
		})
	}
}
