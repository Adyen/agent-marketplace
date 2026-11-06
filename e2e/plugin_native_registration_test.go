//go:build e2e

package e2e_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	settings := readNativeSettings(t, env, "/workspace/.claude/settings.json")
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
		Plugins []struct {
			Name   string `json:"name"`
			Source struct {
				Source string `json:"source"`
				Path   string `json:"path"`
			} `json:"source"`
		} `json:"plugins"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatalf("decode native catalog: %v\n%s", err, data)
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
		sourcePath, err := filepath.EvalSymlinks(plugin.Source.Path)
		if err != nil {
			t.Fatalf("resolve native package generation %q: %v", plugin.Source.Path, err)
		}
		expectedPath, err := filepath.EvalSymlinks(expectedGeneration)
		if err != nil || sourcePath != expectedPath {
			t.Fatalf("%s catalog source = %q, want target generation %q (resolve: %v)", target, sourcePath, expectedPath, err)
		}
		if _, err := os.Stat(filepath.Join(plugin.Source.Path, ".claude-plugin", "plugin.json")); err != nil {
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
	env.WriteFile("/workspace/.claude/settings.json", "{\n  // preserve this handwritten comment\n  \"enabledPlugins\": {\"handwritten@local\": true},\n  \"customSetting\": \"keep\"\n}\n")
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
	settings := readNativeSettings(t, env, "/workspace/.claude/settings.json")
	for _, want := range []string{"preserve this handwritten comment", "handwritten@local", "customSetting", "extraKnownMarketplaces", "enabledPlugins", "hooks-kit"} {
		if !strings.Contains(string(settings), want) {
			t.Errorf("settings lost or omitted %q:\n%s", want, settings)
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
	settingsPath := env.mustResolvePath("/workspace/.claude/settings.json")
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
	settings := readNativeSettings(t, env, "/workspace/.claude/settings.json")
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
	settingsPath := env.mustResolvePath("/workspace/.claude/settings.json")
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
	settings := readNativeSettings(t, env, "/workspace/.claude/settings.json")
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
				env.WriteFile("/workspace/.claude/settings.json",
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
				settings := readNativeSettings(t, env, "/workspace/.claude/settings.json")
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
