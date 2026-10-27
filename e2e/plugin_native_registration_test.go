//go:build e2e

package e2e_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/jsonc"
)

const nativePluginSource = `---
name: review
description: Review plugin changes
---
Review plugin changes.
`

func nativePluginCatalog(target string) string {
	return fmt.Sprintf(`schema: 1
harness: %s
evidence:
  checked: "2026-10-21"
  refs: [synthetic-e2e-fixture]
accepts: [claude-plugin]
delivery:
  decompose: {status: verified}
  native: {status: verified, format: claude-plugin, probe: e2e-test-probe, refresh: restart-required}
`, target)
}

func newNativePluginEnv(t *testing.T, target, delivery string) (*Env, string) {
	t.Helper()
	env := newEnv(t)
	env.InitGitRepo()
	env.WriteGlobalConfig("apiVersion: agent-marketplace/v4\nharnesses: [" + target + "]\n")
	writeNativePlugin(env, "/workspace/vendor/native-kit", "native-kit")
	env.WriteRepoConfig(nativePluginConfig(delivery))
	catalog := "/workspace/test-catalogs/" + target + ".yaml"
	env.WriteFile(catalog, nativePluginCatalog(target))
	return env, env.mustResolvePath(catalog)
}

func writeNativePlugin(env *Env, root, name string) {
	env.WriteFile(root+"/.claude-plugin/plugin.json", `{"name":"`+name+`"}`)
	env.WriteFile(root+"/skills/review/SKILL.md", nativePluginSource)
	env.WriteFile(root+"/hooks/hooks.json", `{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo `+name+`"}]}]}`)
}

func nativePluginConfig(delivery string) string {
	return "apiVersion: agent-marketplace/v4\nmaterialization: {mode: file}\nsources:\n" +
		"  vendor: {directory: ./vendor/native-kit, format: claude-plugin}\nresources:\n" +
		"  vendor:\n    - {kind: plugin, name: native-kit, delivery: " + delivery + "}\n"
}

func nativePluginDeselectedConfig() string {
	return "apiVersion: agent-marketplace/v4\nmaterialization: {mode: file}\nsources:\n" +
		"  vendor: {directory: ./vendor/native-kit, format: claude-plugin}\nresources:\n" +
		"  vendor: []\n"
}

func nativeCatalogEnv(path string) map[string]string {
	return map[string]string{"AGENT_MARKETPLACE_E2E_PLUGIN_CATALOG": path}
}

func runNativeAM(env *Env, catalogPath string, args ...string) ExecResult {
	env.t.Helper()
	return env.RunAMWithEnv(nativeCatalogEnv(catalogPath), args...)
}

func readNativeSettings(t *testing.T, env *Env, rel string) []byte {
	t.Helper()
	data, err := os.ReadFile(env.mustResolvePath(rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return data
}

func assertClaudeNativeRegistration(t *testing.T, env *Env) []byte {
	t.Helper()
	settings := readNativeSettings(t, env, "/workspace/.claude/settings.local.json")
	for _, want := range []string{"extraKnownMarketplaces", "enabledPlugins", "native-kit", filepath.Join(env.home, ".local", "state", "agent-marketplace")} {
		if !strings.Contains(string(settings), want) {
			t.Fatalf("Claude native registration omitted %q:\n%s", want, settings)
		}
	}
	return settings
}

func assertNativePluginCatalog(t *testing.T, env *Env, pluginName, target string) {
	t.Helper()
	stateRoot := filepath.Join(env.home, ".local", "state", "agent-marketplace")
	manifestRel := ".claude-plugin/marketplace.json"
	if target == "codex" {
		manifestRel = filepath.Join(".agents", "plugins", "marketplace.json")
	}
	paths, err := filepath.Glob(filepath.Join(stateRoot, "native-plugins", target, "*", manifestRel))
	if err != nil || len(paths) != 1 {
		t.Fatalf("native %s catalogs = %v / %v, want one", target, paths, err)
	}
	otherManifest := filepath.Join(stateRoot, "native-plugins", target, "*", ".claude-plugin", "marketplace.json")
	if target == "claude" {
		otherManifest = filepath.Join(stateRoot, "native-plugins", target, "*", ".agents", "plugins", "marketplace.json")
	}
	otherPaths, err := filepath.Glob(otherManifest)
	if err != nil || len(otherPaths) != 0 {
		t.Fatalf("%s catalog also contains the other harness manifest: %v / %v", target, otherPaths, err)
	}
	data, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Owner struct {
			Name string `json:"name"`
		} `json:"owner"`
		Plugins []struct {
			Name   string          `json:"name"`
			Source json.RawMessage `json:"source"`
		} `json:"plugins"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatalf("decode native catalog: %v\n%s", err, data)
	}
	if target == "claude" && document.Owner.Name != "Agent Marketplace" {
		t.Fatalf("Claude catalog owner = %q, want Agent Marketplace", document.Owner.Name)
	}
	instanceDir := filepath.Join(stateRoot, "ownership", "plugin-packages")
	instanceFiles, err := filepath.Glob(filepath.Join(instanceDir, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	generationKey := ""
	for _, statePath := range instanceFiles {
		stateBytes, err := os.ReadFile(statePath)
		if err != nil {
			t.Fatal(err)
		}
		var state struct {
			Instance struct {
				Name          string `json:"name"`
				Target        string `json:"target"`
				GenerationKey string `json:"generation_key"`
			} `json:"instance"`
		}
		if err := json.Unmarshal(stateBytes, &state); err != nil {
			t.Fatal(err)
		}
		if state.Instance.Name == pluginName && state.Instance.Target == target {
			generationKey = state.Instance.GenerationKey
			break
		}
	}
	if generationKey == "" {
		t.Fatalf("no %s plugin generation state for %s", target, pluginName)
	}
	expectedGeneration := filepath.Join(stateRoot, "packages", generationKey)
	for _, plugin := range document.Plugins {
		if plugin.Name != pluginName {
			continue
		}
		var sourcePath string
		if target == "claude" {
			if err := json.Unmarshal(plugin.Source, &sourcePath); err != nil {
				t.Fatalf("decode Claude plugin source: %v", err)
			}
			wantSource := "./plugins/" + pluginName
			if sourcePath != wantSource {
				t.Fatalf("Claude plugin source = %q, want %q", sourcePath, wantSource)
			}
			catalogRoot := filepath.Dir(filepath.Dir(paths[0]))
			sourcePath = filepath.Join(catalogRoot, filepath.FromSlash(sourcePath))
			info, err := os.Lstat(sourcePath)
			if err != nil || info.Mode()&os.ModeSymlink == 0 {
				t.Fatalf("Claude plugin source %q is not a symlink: info=%v err=%v", sourcePath, info, err)
			}
		} else {
			var source struct {
				Source string `json:"source"`
				Path   string `json:"path"`
			}
			if err := json.Unmarshal(plugin.Source, &source); err != nil {
				t.Fatalf("decode %s plugin source: %v", target, err)
			}
			sourcePath = source.Path
		}
		sourcePath, err = filepath.EvalSymlinks(sourcePath)
		if err != nil {
			t.Fatalf("resolve native package generation %q: %v", sourcePath, err)
		}
		expectedPath, err := filepath.EvalSymlinks(expectedGeneration)
		if err != nil || sourcePath != expectedPath {
			t.Fatalf("%s catalog source = %q, want target generation %q (resolve: %v)", target, sourcePath, expectedPath, err)
		}
		if _, err := os.Stat(filepath.Join(sourcePath, ".claude-plugin", "plugin.json")); err != nil {
			t.Fatalf("native plugin catalog target is not a package generation: %v", err)
		}
		return
	}
	t.Fatalf("native catalog omitted plugin %s:\n%s", pluginName, data)
}

func removeNativePluginState(t *testing.T, env *Env, pluginName string) {
	t.Helper()
	stateDir := filepath.Join(env.home, ".local", "state", "agent-marketplace", "ownership", "plugin-packages")
	paths, err := filepath.Glob(filepath.Join(stateDir, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	removed := false
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var state struct {
			Instance struct {
				Name string `json:"name"`
			} `json:"instance"`
		}
		if err := json.Unmarshal(data, &state); err != nil {
			t.Fatal(err)
		}
		if state.Instance.Name == pluginName {
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			removed = true
			break
		}
	}
	if !removed {
		t.Fatalf("no plugin instance state for %s in %s", pluginName, stateDir)
	}
	rootStateDir := filepath.Join(filepath.Dir(stateDir), "*", "state.json")
	rootStates, err := filepath.Glob(rootStateDir)
	if err != nil {
		t.Fatal(err)
	}
	targetRoot, err := filepath.EvalSymlinks(env.mustResolvePath("/workspace/.claude"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range rootStates {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var state struct {
			CanonicalRoot string `json:"canonical_root"`
		}
		if err := json.Unmarshal(data, &state); err != nil {
			t.Fatal(err)
		}
		if state.CanonicalRoot == targetRoot {
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func assertNoStagingResidue(t *testing.T, root string) {
	t.Helper()
	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		name := info.Name()
		if strings.HasSuffix(name, ".tmp") || strings.HasPrefix(name, ".dependencytxn-") || strings.HasPrefix(name, ".packagegen-") {
			t.Errorf("staging residue at %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
}

// spec: plugin-delivery/auto-decomposes-where-native-delivery-is-unverified
func TestPluginNativeAutoDeliveryIsChosenPerTarget(t *testing.T) {
	env, catalogPath := newNativePluginEnv(t, "claude", "auto")
	env.WriteGlobalConfig("apiVersion: agent-marketplace/v4\nharnesses: [claude, cursor]\n")
	result := runNativeAM(env, catalogPath, "sync", "--no-backup")
	if result.ExitCode != 0 {
		t.Fatalf("sync failed (exit %d):\n%s", result.ExitCode, result.Combined)
	}
	if !strings.Contains(result.Combined, "claude: native") || !strings.Contains(result.Combined, "cursor: decompose") {
		t.Fatalf("sync did not report both target delivery choices:\n%s", result.Combined)
	}
	assertClaudeNativeRegistration(t, env)
	assertNativePluginCatalog(t, env, "native-kit", "claude")
	if _, err := os.Stat(env.mustResolvePath("/workspace/.claude/skills/native-kit-review/SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("Claude native delivery also wrote decomposed members: %v", err)
	}
	if _, err := os.Stat(env.mustResolvePath("/workspace/.cursor/skills/native-kit-review/SKILL.md")); err != nil {
		t.Fatalf("Cursor did not receive decomposed members: %v", err)
	}
}

// spec: plugin-delivery/native-delivery-never-falls-back-to-decomposed-output
func TestPluginNativeUnverifiedDeliveryFailsWithoutDecomposing(t *testing.T) {
	env, _ := newNativePluginEnv(t, "cursor", "native")
	result := env.RunAM("sync", "--no-backup")
	if result.ExitCode == 0 || !strings.Contains(result.Combined, "plugin.delivery.native-unverified") {
		t.Fatalf("native request to unverified Cursor did not fail closed (exit %d):\n%s", result.ExitCode, result.Combined)
	}
	if _, err := os.Lstat(env.mustResolvePath("/workspace/.cursor/skills/native-kit-review")); !os.IsNotExist(err) {
		t.Fatalf("unverified native request wrote decomposed members: %v", err)
	}
	generations, err := filepath.Glob(filepath.Join(env.home, ".local", "state", "agent-marketplace", "packages", "*"))
	if err != nil || len(generations) != 0 {
		t.Fatalf("unverified native request published package generations: %v / %v", generations, err)
	}
}

// spec: plugin-policy-catalogs/shipped-claude-catalog-delivers-claude-plugin-packages-natively
func TestShippedClaudeCatalogUsesNativeDeliveryForAuto(t *testing.T) {
	env, _ := newNativePluginEnv(t, "claude", "auto")
	result := env.RunAM("sync", "--no-backup")
	if result.ExitCode != 0 {
		t.Fatalf("sync with shipped Claude catalog failed (exit %d):\n%s", result.ExitCode, result.Combined)
	}
	if !strings.Contains(result.Combined, "claude: native") {
		t.Fatalf("shipped Claude catalog did not select native delivery:\n%s", result.Combined)
	}
	assertClaudeNativeRegistration(t, env)
	assertNativePluginCatalog(t, env, "native-kit", "claude")
	if _, err := os.Stat(env.mustResolvePath("/workspace/.claude/skills/native-kit-review/SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("shipped Claude native delivery also wrote decomposed members: %v", err)
	}
}

// spec: plugin-delivery/claude-registration-preserves-handwritten-settings
func TestClaudeNativeRegistrationPreservesHandwrittenSettingsAndHooks(t *testing.T) {
	env, catalogPath := newNativePluginEnv(t, "claude", "decompose")
	env.WriteFile("/workspace/vendor/hooks-kit/.claude-plugin/plugin.json", `{"name":"hooks-kit"}`)
	env.WriteFile("/workspace/vendor/hooks-kit/hooks/hooks.json", `{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo hooks-kit"}]}]}`)
	env.WriteRepoConfig("apiVersion: agent-marketplace/v4\nmaterialization: {mode: file}\nsources:\n" +
		"  vendor: {directory: ./vendor/native-kit, format: claude-plugin}\n" +
		"  hooks: {directory: ./vendor/hooks-kit, format: claude-plugin}\n" +
		"resources:\n  vendor:\n    - {kind: plugin, name: native-kit, delivery: decompose}\n" +
		"  hooks:\n    - {kind: plugin, name: hooks-kit, delivery: decompose}\n")
	env.WriteFile("/workspace/.claude/settings.local.json", "{\n  // preserve this handwritten comment\n  \"enabledPlugins\": {\"handwritten@local\": true},\n  \"customSetting\": \"keep\"\n}\n")
	first := env.RunAM("sync", "--no-backup")
	if first.ExitCode != 0 {
		t.Fatalf("initial decomposed setup failed (exit %d):\n%s", first.ExitCode, first.Combined)
	}
	env.WriteRepoConfig("apiVersion: agent-marketplace/v4\nmaterialization: {mode: file}\nsources:\n" +
		"  vendor: {directory: ./vendor/native-kit, format: claude-plugin}\n" +
		"  hooks: {directory: ./vendor/hooks-kit, format: claude-plugin}\n" +
		"resources:\n  vendor:\n    - {kind: plugin, name: native-kit, delivery: native}\n" +
		"  hooks:\n    - {kind: plugin, name: hooks-kit, delivery: decompose}\n")
	result := runNativeAM(env, catalogPath, "sync", "--no-backup")
	if result.ExitCode != 0 {
		t.Fatalf("native registration failed (exit %d):\n%s", result.ExitCode, result.Combined)
	}
	localSettings := readNativeSettings(t, env, "/workspace/.claude/settings.local.json")
	for _, want := range []string{"preserve this handwritten comment", "handwritten@local", "customSetting", "extraKnownMarketplaces", "enabledPlugins", "native-kit"} {
		if !strings.Contains(string(localSettings), want) {
			t.Errorf("settings.local.json lost or omitted %q:\n%s", want, localSettings)
		}
	}
	projectSettings := readNativeSettings(t, env, "/workspace/.claude/settings.json")
	if !strings.Contains(string(projectSettings), "hooks-kit") {
		t.Fatalf("settings.json lost the owned hook:\n%s", projectSettings)
	}
	for _, forbidden := range []string{"extraKnownMarketplaces", "native-kit@"} {
		if strings.Contains(string(projectSettings), forbidden) {
			t.Fatalf("settings.json contains repository plugin registration %q:\n%s", forbidden, projectSettings)
		}
	}
}

// spec: plugin-delivery/registration-without-ownership-state-is-not-pruned
func TestUnownedNativeRegistrationIsPreservedAndReported(t *testing.T) {
	env, catalogPath := newNativePluginEnv(t, "claude", "native")
	first := runNativeAM(env, catalogPath, "sync", "--no-backup")
	if first.ExitCode != 0 {
		t.Fatalf("initial native registration failed (exit %d):\n%s", first.ExitCode, first.Combined)
	}
	settingsPath := env.mustResolvePath("/workspace/.claude/settings.local.json")
	before, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	removeNativePluginState(t, env, "native-kit")
	result := runNativeAM(env, catalogPath, "sync", "--no-backup")
	if !strings.Contains(result.Combined, "plugin.registration.collision") {
		t.Fatalf("sync did not report an unowned registration collision (exit %d):\n%s", result.ExitCode, result.Combined)
	}
	after, err := os.ReadFile(settingsPath)
	if err != nil || string(after) != string(before) {
		t.Fatalf("sync changed an unowned registration: read=%v\nbefore=%s\nafter=%s", err, before, after)
	}
}

// spec: plugin-delivery/claude-registration-moves-out-of-shared-project-settings
func TestClaudeRegistrationMovesFromSharedSettingsInOneSync(t *testing.T) {
	env, catalogPath := newNativePluginEnv(t, "claude", "native")
	env.WriteFile("/workspace/vendor/hooks-kit/.claude-plugin/plugin.json", `{"name":"hooks-kit"}`)
	env.WriteFile("/workspace/vendor/hooks-kit/hooks/hooks.json",
		`{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo hooks-kit"}]}]}`)
	env.WriteRepoConfig("apiVersion: agent-marketplace/v4\nmaterialization: {mode: file}\nsources:\n" +
		"  vendor: {directory: ./vendor/native-kit, format: claude-plugin}\n" +
		"  hooks: {directory: ./vendor/hooks-kit, format: claude-plugin}\n" +
		"resources:\n  vendor:\n    - {kind: plugin, name: native-kit, delivery: native}\n" +
		"  hooks:\n    - {kind: plugin, name: hooks-kit, delivery: decompose}\n")
	env.WriteFile("/workspace/.claude/settings.json", "{\"enabledPlugins\":{},\"extraKnownMarketplaces\":{}}\n")
	first := runNativeAM(env, catalogPath, "sync", "--no-backup")
	if first.ExitCode != 0 {
		t.Fatalf("initial native sync failed (exit %d):\n%s", first.ExitCode, first.Combined)
	}
	seedPriorClaudeRegistrationInSharedSettings(t, env)
	projectBefore := readNativeSettings(t, env, "/workspace/.claude/settings.json")
	projectDoc, err := jsonc.Parse("settings.json", projectBefore)
	if err != nil {
		t.Fatal(err)
	}
	hooksBefore, ok := projectDoc.Root().Member("hooks")
	if !ok {
		t.Fatalf("seeded settings.json has no owned hooks:\n%s", projectBefore)
	}
	hooksRaw := append([]byte(nil), hooksBefore.Raw()...)
	projectEnabledBefore, ok := projectDoc.Root().Member("enabledPlugins")
	if !ok {
		t.Fatalf("seeded settings.json has no enabledPlugins:\n%s", projectBefore)
	}
	unownedBefore, ok := projectEnabledBefore.Member("am-handwritten@manual")
	if !ok {
		t.Fatalf("seeded settings.json has no unowned registration:\n%s", projectBefore)
	}
	ownedKeys := func(doc *jsonc.Document, container, prefix string) []string {
		node, ok := doc.Root().Member(container)
		if !ok {
			return nil
		}
		var keys []string
		for _, key := range node.Keys() {
			if strings.HasPrefix(key, prefix) {
				keys = append(keys, key)
			}
		}
		return keys
	}
	oldMarketplaceKeys := ownedKeys(projectDoc, "extraKnownMarketplaces", "am-")
	oldPluginKeys := ownedKeys(projectDoc, "enabledPlugins", "native-kit@")
	if len(oldMarketplaceKeys) != 1 || len(oldPluginKeys) != 1 {
		t.Fatalf("migration fixture does not have exactly one owned registration: marketplaces=%v plugins=%v\n%s", oldMarketplaceKeys, oldPluginKeys, projectBefore)
	}

	result := runNativeAM(env, catalogPath, "sync", "--no-backup")
	if result.ExitCode != 0 {
		t.Fatalf("registration migration sync failed (exit %d):\n%s", result.ExitCode, result.Combined)
	}
	localSettings := readNativeSettings(t, env, "/workspace/.claude/settings.local.json")
	projectAfter := readNativeSettings(t, env, "/workspace/.claude/settings.json")
	localDoc, err := jsonc.Parse("settings.local.json", localSettings)
	if err != nil {
		t.Fatal(err)
	}
	projectDoc, err = jsonc.Parse("settings.json", projectAfter)
	if err != nil {
		t.Fatal(err)
	}
	newMarketplaceKeys := ownedKeys(localDoc, "extraKnownMarketplaces", "am-")
	newPluginKeys := ownedKeys(localDoc, "enabledPlugins", "native-kit@")
	if len(newMarketplaceKeys) != 1 || newMarketplaceKeys[0] != oldMarketplaceKeys[0] ||
		len(newPluginKeys) != 1 || newPluginKeys[0] != oldPluginKeys[0] {
		t.Fatalf("settings.local.json does not contain each owned registration exactly once: marketplaces=%v plugins=%v\n%s", newMarketplaceKeys, newPluginKeys, localSettings)
	}
	if projectKeys := ownedKeys(projectDoc, "extraKnownMarketplaces", "am-"); len(projectKeys) != 0 {
		t.Fatalf("settings.json retained an owned marketplace registration after migration: %v\n%s", projectKeys, projectAfter)
	}
	if projectKeys := ownedKeys(projectDoc, "enabledPlugins", "native-kit@"); len(projectKeys) != 0 {
		t.Fatalf("settings.json retained an owned enabled plugin after migration: %v\n%s", projectKeys, projectAfter)
	}
	projectEnabledAfter, ok := projectDoc.Root().Member("enabledPlugins")
	if !ok {
		t.Fatalf("migration removed the seeded enabledPlugins container:\n%s", projectAfter)
	}
	unownedAfter, ok := projectEnabledAfter.Member("am-handwritten@manual")
	if !ok || !bytes.Equal(unownedAfter.Raw(), unownedBefore.Raw()) {
		t.Fatalf("migration changed the unowned registration: before=%s after=%s", unownedBefore.Raw(), projectAfter)
	}
	hooksAfter, hasHooks := projectDoc.Root().Member("hooks")
	if !hasHooks || !bytes.Equal(hooksAfter.Raw(), hooksRaw) {
		t.Fatalf("migration changed owned hooks in settings.json: before=%s after=%s", hooksRaw, projectAfter)
	}
}

func seedPriorClaudeRegistrationInSharedSettings(t *testing.T, env *Env) {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", filepath.Join(env.home, ".local", "state"))
	root := env.mustResolvePath("/workspace/.claude")
	stateRoot := filepath.Join(env.home, ".local", "state", "agent-marketplace")
	store, err := ownership.Open(root, ownership.WithStateRoot(stateRoot))
	if err != nil {
		t.Fatal(err)
	}
	record, err := store.Load()
	if err != nil {
		t.Fatalf("load native registration ownership: %v", err)
	}
	const projectRel = "settings.json"
	const localRel = "settings.local.json"
	paths := map[string]string{
		projectRel: filepath.Join(root, projectRel),
		localRel:   filepath.Join(root, localRel),
	}
	documents := make(map[string]*jsonc.Document, 2)
	originalBytes := make(map[string][]byte, 2)
	present := make(map[string]bool, 2)
	for _, rel := range []string{projectRel, localRel} {
		raw, readErr := os.ReadFile(paths[rel])
		if os.IsNotExist(readErr) && rel == localRel {
			raw = []byte("{}\n")
		} else if readErr != nil {
			t.Fatalf("read %s before migration fixture: %v", rel, readErr)
		} else {
			present[rel] = true
		}
		originalBytes[rel] = append([]byte(nil), raw...)
		documents[rel], err = jsonc.Parse(rel, raw)
		if err != nil {
			t.Fatalf("parse %s before migration fixture: %v", rel, err)
		}
	}

	projectEntries := append([]ownership.Entry(nil), record.Files[projectRel].Entries...)
	localEntries := append([]ownership.Entry(nil), record.Files[localRel].Entries...)
	registrationCount := 0
	for _, rel := range []string{projectRel, localRel} {
		fileState := record.Files[rel]
		for _, entry := range fileState.Entries {
			if entry.Kind != ownership.KindPluginRegistration {
				continue
			}
			sourceContainer, ok := documents[rel].Root().Member(entry.Container)
			if !ok {
				t.Fatalf("%s is missing registration container %q", rel, entry.Container)
			}
			value, ok := sourceContainer.Member(entry.Key)
			if !ok {
				t.Fatalf("%s is missing owned registration %q", rel, entry.Key)
			}
			projectContainer := ensureJSONCObject(t, documents[projectRel], entry.Container)
			if !documents[projectRel].SetMember(projectContainer, entry.Key, value.Raw()) {
				t.Fatalf("seed shared settings registration %q", entry.Key)
			}
			if rel != projectRel {
				if !documents[rel].DeleteMember(sourceContainer, entry.Key) {
					t.Fatalf("remove current-file registration %q", entry.Key)
				}
				projectEntries = append(projectEntries, entry)
				localEntries = withoutPluginRegistrations(localEntries)
			}
			registrationCount++
		}
	}
	if registrationCount == 0 {
		t.Fatal("migration fixture has no owned Claude registration entries")
	}
	enabled := ensureJSONCObject(t, documents[projectRel], "enabledPlugins")
	if !documents[projectRel].SetMember(enabled, "am-handwritten@manual", []byte("true")) {
		t.Fatal("seed unowned am- registration")
	}
	projectBytes := documents[projectRel].Bytes()
	if _, err := store.Apply(ownership.Mutation{
		Rel: projectRel, AbsFile: paths[projectRel], NextBytes: projectBytes, Entries: projectEntries, Reason: ownership.ReasonSync,
		ExpectPriorGeneration: record.Generation, ExpectPriorDigest: ownership.Digest(originalBytes[projectRel]),
	}); err != nil {
		t.Fatalf("seed nvlx.7 ownership in settings.json: %v", err)
	}
	if present[localRel] {
		if _, err := store.Apply(ownership.Mutation{
			Rel: localRel, AbsFile: paths[localRel], NextBytes: documents[localRel].Bytes(), Entries: localEntries, Reason: ownership.ReasonSync,
			ExpectPriorGeneration: record.Generation + 1, ExpectPriorDigest: ownership.Digest(originalBytes[localRel]),
		}); err != nil {
			t.Fatalf("remove current registration ownership from settings.local.json: %v", err)
		}
	}
}

func withoutPluginRegistrations(entries []ownership.Entry) []ownership.Entry {
	filtered := entries[:0]
	for _, entry := range entries {
		if entry.Kind != ownership.KindPluginRegistration {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

func ensureJSONCObject(t *testing.T, doc *jsonc.Document, name string) *jsonc.Node {
	t.Helper()
	node, ok := doc.Root().Member(name)
	if !ok {
		if !doc.SetMember(doc.Root(), name, []byte("{}")) {
			t.Fatalf("create JSONC object %q", name)
		}
		node, ok = doc.Root().Member(name)
	}
	if !ok || node.Kind() != jsonc.KindObject {
		t.Fatalf("JSONC member %q is not an object:\n%s", name, doc.Bytes())
	}
	return node
}

// spec: plugin-delivery/codex-registration-writes-no-trust
func TestCodexNativeRegistrationDoesNotWriteTrust(t *testing.T) {
	env, catalogPath := newNativePluginEnv(t, "codex", "native")
	result := runNativeAM(env, catalogPath, "sync", "--no-backup")
	if result.ExitCode != 0 {
		t.Fatalf("Codex native registration failed (exit %d):\n%s", result.ExitCode, result.Combined)
	}
	config, err := os.ReadFile(env.mustResolvePath("/workspace/.codex/config.toml"))
	if err != nil {
		t.Fatalf("read Codex config: %v\n%s", err, result.Combined)
	}
	for _, want := range []string{"[marketplaces.", "[plugins.", "native-kit"} {
		if !strings.Contains(string(config), want) {
			t.Errorf("Codex config omitted %q:\n%s", want, config)
		}
	}
	for _, forbidden := range []string{"[projects.", "trust_level", "trusted"} {
		if strings.Contains(string(config), forbidden) {
			t.Errorf("Codex registration wrote trust configuration %q:\n%s", forbidden, config)
		}
	}
	assertNativePluginCatalog(t, env, "native-kit", "codex")
}

// spec: plugin-delivery/reload-requirement-is-reported
func TestNativePluginGenerationReloadRequirementIsReported(t *testing.T) {
	env, catalogPath := newNativePluginEnv(t, "claude", "native")
	first := runNativeAM(env, catalogPath, "sync", "--no-backup")
	if first.ExitCode != 0 {
		t.Fatalf("initial native sync failed (exit %d):\n%s", first.ExitCode, first.Combined)
	}
	env.WriteFile("/workspace/vendor/native-kit/skills/review/SKILL.md", strings.ReplaceAll(nativePluginSource, "Review plugin changes.", "Review updated plugin changes."))
	sync := runNativeAM(env, catalogPath, "sync", "--no-backup")
	if sync.ExitCode != 0 || !strings.Contains(sync.Combined, "reload-required") {
		t.Fatalf("sync did not report the changed native generation requires reload (exit %d):\n%s", sync.ExitCode, sync.Combined)
	}
	status := runNativeAM(env, catalogPath, "status")
	if status.ExitCode != 0 || !strings.Contains(status.Combined, "reload-required") {
		t.Fatalf("status did not report the native reload requirement (exit %d):\n%s", status.ExitCode, status.Combined)
	}
	nextSync := runNativeAM(env, catalogPath, "sync", "--no-backup")
	if nextSync.ExitCode != 0 || strings.Contains(nextSync.Combined, "reload-required") {
		t.Fatalf("unchanged native generation retained reload-required (exit %d):\n%s", nextSync.ExitCode, nextSync.Combined)
	}
}

// spec: plugin-delivery/claude-catalog-is-a-marketplace-claude-code-accepts
func TestClaudeNativeCatalogLinksRetargetAndRetire(t *testing.T) {
	env, catalogPath := newNativePluginEnv(t, "claude", "native")
	first := runNativeAM(env, catalogPath, "sync", "--no-backup")
	if first.ExitCode != 0 {
		t.Fatalf("initial native sync failed (exit %d):\n%s", first.ExitCode, first.Combined)
	}
	manifests, err := filepath.Glob(filepath.Join(env.home, ".local", "state", "agent-marketplace", "native-plugins", "claude", "*", ".claude-plugin", "marketplace.json"))
	if err != nil || len(manifests) != 1 {
		t.Fatalf("Claude marketplace manifests = %v / %v, want one", manifests, err)
	}
	catalogRoot := filepath.Dir(filepath.Dir(manifests[0]))
	link := filepath.Join(catalogRoot, "plugins", "native-kit")
	firstTarget, err := filepath.EvalSymlinks(link)
	if err != nil {
		t.Fatalf("resolve first plugin generation link %q: %v", link, err)
	}
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("Claude catalog plugin path is not a symlink: info=%v err=%v", info, err)
	}

	env.WriteFile("/workspace/vendor/native-kit/skills/review/SKILL.md",
		strings.ReplaceAll(nativePluginSource, "Review plugin changes.", "Review updated plugin changes."))
	updated := runNativeAM(env, catalogPath, "sync", "--no-backup")
	if updated.ExitCode != 0 {
		t.Fatalf("sync with changed generation failed (exit %d):\n%s", updated.ExitCode, updated.Combined)
	}
	secondTarget, err := filepath.EvalSymlinks(link)
	if err != nil || secondTarget == firstTarget {
		t.Fatalf("generation update did not retarget catalog link: first=%q second=%q err=%v", firstTarget, secondTarget, err)
	}
	assertNativePluginCatalog(t, env, "native-kit", "claude")

	env.WriteRepoConfig(nativePluginConfig("decompose"))
	removed := runNativeAM(env, catalogPath, "sync", "--no-backup")
	if removed.ExitCode != 0 {
		t.Fatalf("sync removing native catalog entry failed (exit %d):\n%s", removed.ExitCode, removed.Combined)
	}
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Fatalf("retired Claude catalog entry left plugin link %q (err=%v)", link, err)
	}
}

// spec: plugin-delivery/switching-to-decompose-removes-the-native-registration
func TestSwitchingNativePluginToDecomposeRemovesRegistration(t *testing.T) {
	env, catalogPath := newNativePluginEnv(t, "claude", "native")
	first := runNativeAM(env, catalogPath, "sync", "--no-backup")
	if first.ExitCode != 0 {
		t.Fatalf("initial native sync failed (exit %d):\n%s", first.ExitCode, first.Combined)
	}
	assertClaudeNativeRegistration(t, env)
	env.WriteRepoConfig(nativePluginConfig("decompose"))
	result := runNativeAM(env, catalogPath, "sync", "--no-backup")
	if result.ExitCode != 0 {
		t.Fatalf("switch to decomposed delivery failed (exit %d):\n%s", result.ExitCode, result.Combined)
	}
	settings := readNativeSettings(t, env, "/workspace/.claude/settings.local.json")
	if strings.Contains(string(settings), "native-kit@") {
		t.Fatalf("native registration remained after switching to decompose:\n%s", settings)
	}
	if _, err := os.Stat(env.mustResolvePath("/workspace/.claude/skills/native-kit-review/SKILL.md")); err != nil {
		t.Fatalf("decomposed member was not installed: %v", err)
	}
}

// spec: plugin-delivery/interrupted-delivery-switch-recovers-without-duplicates
func TestInterruptedNativePluginDeliverySwitchRecoversWithoutDuplicates(t *testing.T) {
	env, catalogPath := newNativePluginEnv(t, "claude", "native")
	first := runNativeAM(env, catalogPath, "sync", "--no-backup")
	if first.ExitCode != 0 {
		t.Fatalf("initial native sync failed (exit %d):\n%s", first.ExitCode, first.Combined)
	}
	assertClaudeNativeRegistration(t, env)
	env.WriteRepoConfig(nativePluginConfig("decompose"))
	crash := env.RunAMWithEnv(map[string]string{
		"AGENT_MARKETPLACE_E2E_PLUGIN_CATALOG": catalogPath,
		"AGENT_MARKETPLACE_E2E_CRASH_AT":       "after-publish:0",
	}, "sync", "--no-backup")
	if crash.ExitCode != 137 {
		t.Fatalf("injected delivery transition exited %d instead of crashing:\n%s", crash.ExitCode, crash.Combined)
	}
	settingsPath := env.mustResolvePath("/workspace/.claude/settings.local.json")
	memberPath := env.mustResolvePath("/workspace/.claude/skills/native-kit-review/SKILL.md")
	readSnapshot := func(path string) []byte {
		t.Helper()
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		return data
	}
	beforeSettings, beforeMember := readSnapshot(settingsPath), readSnapshot(memberPath)
	for _, args := range [][]string{{"sync", "--check"}, {"status"}} {
		result := env.RunAM(args...)
		if result.ExitCode == 0 || !strings.Contains(result.Combined, "dependency.recovery-required") {
			t.Fatalf("read-only %v did not refuse pending transition (exit %d):\n%s", args, result.ExitCode, result.Combined)
		}
		if string(readSnapshot(settingsPath)) != string(beforeSettings) || string(readSnapshot(memberPath)) != string(beforeMember) {
			t.Fatalf("read-only %v replayed delivery transition participants", args)
		}
	}
	recovery := runNativeAM(env, catalogPath, "sync", "--no-backup")
	if recovery.ExitCode != 0 {
		t.Fatalf("recovery sync failed (exit %d):\n%s", recovery.ExitCode, recovery.Combined)
	}
	settings := readNativeSettings(t, env, "/workspace/.claude/settings.local.json")
	if strings.Contains(string(settings), "native-kit@") {
		t.Fatalf("native registration remained after recovery:\n%s", settings)
	}
	if _, err := os.Stat(memberPath); err != nil {
		t.Fatalf("recovery did not install decomposed member: %v", err)
	}
	assertNoStagingResidue(t, filepath.Join(env.home, ".local", "state", "agent-marketplace"))
	assertNoStagingResidue(t, env.mustResolvePath("/workspace/.claude"))
}

func TestVerifiedNativeDeliveryWithoutRegistrationLayoutFailsClosed(t *testing.T) {
	env, catalogPath := newNativePluginEnv(t, "devin", "native")
	result := runNativeAM(env, catalogPath, "sync", "--no-backup")
	if result.ExitCode == 0 || !strings.Contains(result.Combined, "plugin.registration.unsupported") {
		t.Fatalf("verified native delivery to Devin did not fail closed (exit %d):\n%s", result.ExitCode, result.Combined)
	}
	if _, err := os.Lstat(env.mustResolvePath("/workspace/.devin")); !os.IsNotExist(err) {
		t.Fatalf("unsupported native delivery wrote harness output: %v", err)
	}
	generations, err := filepath.Glob(filepath.Join(env.home, ".local", "state", "agent-marketplace", "packages", "*"))
	if err != nil || len(generations) != 0 {
		t.Fatalf("unsupported native delivery published a generation: %v / %v", generations, err)
	}
}

func TestRemovingNativePluginRetiresOnlyOwnedRegistrations(t *testing.T) {
	for _, target := range []string{"claude", "codex"} {
		t.Run(target, func(t *testing.T) {
			env, catalogPath := newNativePluginEnv(t, target, "native")
			if target == "claude" {
				env.WriteFile("/workspace/.claude/settings.local.json",
					"{\n  // handwritten setting\n  \"enabledPlugins\": {\"handwritten@local\": true},\n  \"handwrittenSetting\": true\n}\n")
			} else {
				env.WriteFile("/workspace/.codex/config.toml",
					"[marketplaces.\"handwritten\"]\nsource = { source = \"local\", path = \"/handwritten\" }\n"+
						"[plugins.\"handwritten@handwritten\"]\nenabled = true\n")
			}
			first := runNativeAM(env, catalogPath, "sync", "--no-backup")
			if first.ExitCode != 0 {
				t.Fatalf("initial native sync failed (exit %d):\n%s", first.ExitCode, first.Combined)
			}
			assertNativePluginCatalog(t, env, "native-kit", target)

			env.WriteRepoConfig(nativePluginDeselectedConfig())
			removed := runNativeAM(env, catalogPath, "sync", "--no-backup")
			if removed.ExitCode != 0 {
				t.Fatalf("sync after deselection failed (exit %d):\n%s", removed.ExitCode, removed.Combined)
			}
			if target == "claude" {
				settings := readNativeSettings(t, env, "/workspace/.claude/settings.local.json")
				if strings.Contains(string(settings), "native-kit@") || strings.Contains(string(settings), "am-") {
					t.Fatalf("native Claude registration remained after removal:\n%s", settings)
				}
				if !strings.Contains(string(settings), "handwritten@local") || !strings.Contains(string(settings), "handwrittenSetting") {
					t.Fatalf("removal changed handwritten Claude settings:\n%s", settings)
				}
			} else {
				config, err := os.ReadFile(env.mustResolvePath("/workspace/.codex/config.toml"))
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(string(config), "native-kit@") || strings.Contains(string(config), "am-") {
					t.Fatalf("native Codex registration remained after removal:\n%s", config)
				}
				if !strings.Contains(string(config), "handwritten@handwritten") || !strings.Contains(string(config), "handwritten") {
					t.Fatalf("removal changed handwritten Codex entries:\n%s", config)
				}
			}
			assertNativePluginCatalogEmpty(t, env, target)
		})
	}
}

func assertNativePluginCatalogEmpty(t *testing.T, env *Env, target string) {
	t.Helper()
	manifestRel := filepath.Join(".claude-plugin", "marketplace.json")
	if target == "codex" {
		manifestRel = filepath.Join(".agents", "plugins", "marketplace.json")
	}
	stateRoot := filepath.Join(env.home, ".local", "state", "agent-marketplace")
	paths, err := filepath.Glob(filepath.Join(stateRoot, "native-plugins", target, "*", manifestRel))
	if err != nil || len(paths) != 1 {
		t.Fatalf("native %s catalog manifests = %v / %v, want one", target, paths, err)
	}
	data, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Plugins []struct {
			Name string `json:"name"`
		} `json:"plugins"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("decode empty native catalog: %v\n%s", err, data)
	}
	if len(manifest.Plugins) != 0 {
		t.Fatalf("native catalog still lists removed plugins: %+v", manifest.Plugins)
	}
}
