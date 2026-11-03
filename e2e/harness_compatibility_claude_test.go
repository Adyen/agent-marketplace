package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/jsonc"
	"github.com/creack/pty"
)

// spec: plugin-delivery/claude-probe-fails-when-sync-wrote-claude-owned-plugin-state
func TestClaudePluginStateAssertionRejectsClaudeOwnedState(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name    string
		path    string
		content string
		dir     bool
	}{
		{name: "installed plugin records", path: filepath.Join(".claude", "plugins", "installed_plugins.json"), content: `{}`},
		{name: "known marketplace records", path: filepath.Join(".claude", "plugins", "known_marketplaces.json"), content: `{}`},
		{name: "plugin cache", path: filepath.Join(".claude", "plugins", "cache"), dir: true},
		{name: "workspace trust", path: ".claude.json", content: `{"projects":{"` + workspace + `":{"hasTrustDialogAccepted":true}}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			path := filepath.Join(home, tc.path)
			if tc.dir {
				if err := os.MkdirAll(path, 0o755); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(tc.content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			err := assertClaudePluginStateAbsentAfterSync(home, workspace)
			if err == nil || !strings.Contains(err.Error(), path) {
				t.Fatalf("state assertion error = %v, want it to name %q", err, path)
			}
		})
	}

	if err := assertClaudePluginStateAbsentAfterSync(t.TempDir(), workspace); err != nil {
		t.Fatalf("clean home state assertion failed: %v", err)
	}
}

func TestClaudeMarketplaceRegistrationRequiresInstalledPlugin(t *testing.T) {
	const plugin = "native-kit"
	for _, tc := range []struct {
		name            string
		installedRecord string
		wantRegistered  bool
		wantSnapshot    []string
	}{
		{name: "marketplace without plugin", wantRegistered: false},
		{
			name:            "matching plugin install record",
			installedRecord: `{"version":2,"plugins":{"native-kit@am-test":[{"scope":"user","installPath":"/home/test/.claude/plugins/cache/am-test/native-kit/1","version":"1.0.0","installedAt":"2026-11-02T00:00:00Z","lastUpdated":"2026-11-02T00:00:00Z"}]}}`,
			wantRegistered:  true,
			wantSnapshot:    []string{"known_marketplaces_am_catalog=true", "native-kit@am-test", "scope=user"},
		},
		{
			name:            "different plugin install record",
			installedRecord: `{"version":2,"plugins":{"other-kit@am-test":[{"scope":"user","installPath":"/home/test/.claude/plugins/cache/am-test/other-kit/1","version":"1.0.0","installedAt":"2026-11-02T00:00:00Z","lastUpdated":"2026-11-02T00:00:00Z"}]}}`,
			wantRegistered:  false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			pluginsDir := filepath.Join(home, ".claude", "plugins")
			if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(pluginsDir, "known_marketplaces.json"), []byte(`{"am-test":{}}`), 0o600); err != nil {
				t.Fatal(err)
			}
			if tc.installedRecord != "" {
				if err := os.WriteFile(filepath.Join(pluginsDir, "installed_plugins.json"), []byte(tc.installedRecord), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			registered, _, err := waitForClaudeMarketplaceRegistration(home, plugin, time.Nanosecond)
			if err != nil {
				t.Fatal(err)
			}
			if registered != tc.wantRegistered {
				t.Fatalf("plugin registration = %t, want %t", registered, tc.wantRegistered)
			}
			if len(tc.wantSnapshot) > 0 {
				observed := claudePluginRegistrationSnapshot(home)
				for _, want := range tc.wantSnapshot {
					if !strings.Contains(observed, want) {
						t.Errorf("registration snapshot %q does not contain %q", observed, want)
					}
				}
			}
		})
	}
}

func TestClaudeInteractiveSessionStartsWithCompatibilityProcess(t *testing.T) {
	e := newEnv(t)
	binary := filepath.Join(e.repos, "claude-interactive-session")
	e.WriteFile(binary, `#!/bin/sh
mkdir -p "$HOME/.claude/plugins"
printf '%s\n' '{"am-test":{}}' > "$HOME/.claude/plugins/known_marketplaces.json"
printf '%s\n' '{"plugins":{"native-kit@am-test":[{"scope":"user"}]}}' > "$HOME/.claude/plugins/installed_plugins.json"
trap 'exit 0' INT TERM
while :; do sleep 1; done
`)
	if err := os.Chmod(binary, 0o755); err != nil {
		t.Fatal(err)
	}

	command, registered, _, err := primeClaudeInteractiveSession(e, binary, "native-kit", 5*time.Second)
	if err != nil {
		t.Fatalf("interactive Claude session did not start through the compatibility process: %v\n%+v", err, command)
	}
	if !registered {
		t.Fatalf("interactive Claude session did not observe its install record: %+v", command)
	}
}

func TestClaudePrimingWithoutInstallRecordContinuesToVerdictDiagnostics(t *testing.T) {
	e := newEnv(t)
	binary := filepath.Join(e.repos, "claude-diagnostics")
	e.WriteFile(binary, `#!/bin/sh
if [ "$1" = "plugin" ]; then
	printf '%s\n' 'plugin-list-raw-diagnostic'
	exit 0
fi
if [ "$1" = "--print" ]; then
	IFS= read -r request || exit 70
	case " $* " in
		*" --debug "*)
			printf '%s\n' '{"type":"control_response","response":{"subtype":"success","request_id":"amcompat-initialize","response":{"commands":[],"agents":[]}}}'
			if IFS= read -r request; then exit 72; fi
			;;
		*)
			printf '%s\n' '{"type":"system","subtype":"init","plugins":[{"name":"stream-plugin","path":"/stream/plugin"}]}'
			printf '%s\n' '{"type":"control_response","response":{"subtype":"success","request_id":"amcompat-initialize","response":{"commands":[],"agents":[],"plugins":[{"name":"response-plugin","path":"/response/plugin"}]}}}'
			;;
	esac
	exit 0
fi
trap 'exit 0' INT TERM
while :; do sleep 1; done
`)
	if err := os.Chmod(binary, 0o755); err != nil {
		t.Fatal(err)
	}
	e.WriteFile(filepath.Join(e.home, ".claude", "settings.json"), `{"enabledPlugins":{"native-kit@am-test":true}}`)
	e.WriteFile(filepath.Join(e.home, ".claude", "plugins", "installed_plugins.json"), "not-json")

	var row compatibilityResult
	primeClaudePluginSession(e, binary, "native-kit", &row)
	err := observeClaudePluginEnabled(e, binary, compatibilityFixture{prefix: "fixture"}, &row, "user", "native-kit", "fixture-skill")
	if err == nil {
		t.Fatal("Claude verdict passed without the namespaced skill or a valid plugin-list response")
	}
	if len(row.Commands) < 4 {
		t.Fatalf("priming and both verdict diagnostics were not recorded: %+v", row.Commands)
	}
	if row.Commands[2].Args[1] != "--print" || row.Commands[3].Args[1] != "plugin" {
		t.Fatalf("verdict diagnostics ran out of order: init=%v plugin-list=%v", row.Commands[2].Args, row.Commands[3].Args)
	}
	var observed []string
	for _, command := range row.Commands {
		observed = append(observed, command.Observed...)
	}
	got := strings.Join(observed, "\n")
	for _, want := range []string{
		"registration mode=none",
		"Claude namespaced skill native-kit:fixture-skill listed=false",
		"stream-json system init.plugins: [stream-plugin path=/stream/plugin]",
		"initialize control response.plugins: [response-plugin path=/response/plugin]",
		"plugin list --json stdout: plugin-list-raw-diagnostic",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Claude diagnostics %q do not contain %q", got, want)
		}
	}
}

func TestClaudeWorkspaceTrustSetupPreservesOtherState(t *testing.T) {
	home := t.TempDir()
	workspace := filepath.Join(t.TempDir(), "workspace")
	path := filepath.Join(home, ".claude.json")
	before := `{"other":"preserved","projects":{"` + filepath.Join(home, "other-project") + `":{"hasTrustDialogAccepted":false}}}`
	if err := os.WriteFile(path, []byte(before), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := establishClaudeWorkspaceTrust(home, workspace); err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	data, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(data, &state) != nil {
		t.Fatalf("read trusted Claude state: %v\n%s", err, data)
	}
	if state["other"] != "preserved" || state["hasCompletedOnboarding"] != true {
		t.Fatalf("Claude state did not preserve other content and establish onboarding: %s", data)
	}
	projects, ok := state["projects"].(map[string]any)
	if !ok || projects[filepath.Join(home, "other-project")] == nil {
		t.Fatalf("existing project trust state was lost: %s", data)
	}
	project, ok := projects[workspace].(map[string]any)
	if !ok || project["hasTrustDialogAccepted"] != true {
		t.Fatalf("workspace trust precondition was not established: %s", data)
	}
}

func TestMergeClaudeRepoPluginSettingsCopiesOnlyOwnedEntries(t *testing.T) {
	project := []byte(`{
  // preserve project settings
  "enabledPlugins": {"handwritten@manual": true},
  "extraKnownMarketplaces": {"handwritten": {"source": {"path": "/manual"}}},
  "customSetting": "keep"
}`)
	local := []byte(`{
  "enabledPlugins": {
    "native-kit@am-catalog": true,
    "other-kit@am-catalog": true,
    "native-kit@manual": true
  },
  "extraKnownMarketplaces": {
    "am-catalog": {"source": {"path": "/catalog"}},
    "manual": {"source": {"path": "/manual"}}
  }
}`)

	merged, err := mergeClaudeRepoPluginSettings(project, local, "native-kit")
	if err != nil {
		t.Fatalf("merge Claude repo plugin settings: %v", err)
	}
	document, err := jsonc.Parse("settings.json", merged)
	if err != nil {
		t.Fatalf("parse merged Claude settings: %v", err)
	}
	if !strings.Contains(string(merged), "preserve project settings") {
		t.Errorf("merged Claude settings omitted the project comment:\n%s", merged)
	}
	customSetting, ok := document.Root().Member("customSetting")
	var customValue string
	if ok {
		customValue = string(customSetting.Raw())
	}
	if !ok || customValue != `"keep"` {
		t.Errorf("customSetting = %s, want %q", customValue, `"keep"`)
	}
	plugins, ok := document.Root().Member("enabledPlugins")
	if !ok {
		t.Fatalf("merged settings omitted enabledPlugins:\n%s", merged)
	}
	for key, want := range map[string]string{
		"handwritten@manual":    "true",
		"native-kit@am-catalog": "true",
	} {
		got, ok := plugins.Member(key)
		var actual string
		if ok {
			actual = string(got.Raw())
		}
		if !ok || actual != want {
			t.Errorf("enabledPlugins[%q] = %s, want %s", key, actual, want)
		}
	}
	if _, ok := plugins.Member("other-kit@am-catalog"); ok {
		t.Errorf("merged settings copied another plugin registration:\n%s", merged)
	}
	if _, ok := plugins.Member("native-kit@manual"); ok {
		t.Errorf("merged settings copied a non-marketplace registration:\n%s", merged)
	}
	marketplaces, ok := document.Root().Member("extraKnownMarketplaces")
	if !ok {
		t.Fatalf("merged settings omitted extraKnownMarketplaces:\n%s", merged)
	}
	for key, want := range map[string]string{
		"handwritten": `{"source": {"path": "/manual"}}`,
		"am-catalog":  `{"source": {"path": "/catalog"}}`,
	} {
		got, ok := marketplaces.Member(key)
		var actual string
		if ok {
			actual = string(got.Raw())
		}
		if !ok || actual != want {
			t.Errorf("extraKnownMarketplaces[%q] = %s, want %s", key, actual, want)
		}
	}
	if _, ok := marketplaces.Member("manual"); ok {
		t.Errorf("merged settings copied a non-owned marketplace registration:\n%s", merged)
	}
}

func TestClaudePluginJSONIDsListsUniquePluginIDs(t *testing.T) {
	ids, valid := claudePluginJSONIDs(`{"plugins":[{"name":"other-kit@skills-dir"},{"name":"native-kit@am-catalog"},{"name":"native-kit@am-catalog"}]}`)
	if !valid {
		t.Fatal("valid Claude plugin list was rejected")
	}
	if got := strings.Join(ids, ","); got != "native-kit@am-catalog,other-kit@skills-dir" {
		t.Fatalf("Claude plugin IDs = %q, want %q", got, "native-kit@am-catalog,other-kit@skills-dir")
	}
	if _, valid := claudePluginJSONIDs("not-json"); valid {
		t.Fatal("invalid Claude plugin list was accepted")
	}
}

func mergeClaudeRepoPluginSettings(project, local []byte, plugin string) ([]byte, error) {
	projectDoc, err := jsonc.Parse("settings.json", project)
	if err != nil {
		return nil, fmt.Errorf("parse project Claude settings: %w", err)
	}
	localDoc, err := jsonc.Parse("settings.local.json", local)
	if err != nil {
		return nil, fmt.Errorf("parse local Claude settings: %w", err)
	}
	if projectDoc.Root().Kind() != jsonc.KindObject || localDoc.Root().Kind() != jsonc.KindObject {
		return nil, fmt.Errorf("Claude settings roots must be objects")
	}
	localEnabled, ok := localDoc.Root().Member("enabledPlugins")
	if !ok || localEnabled.Kind() != jsonc.KindObject {
		return nil, fmt.Errorf("local Claude settings have no enabledPlugins object")
	}
	localMarketplaces, ok := localDoc.Root().Member("extraKnownMarketplaces")
	if !ok || localMarketplaces.Kind() != jsonc.KindObject {
		return nil, fmt.Errorf("local Claude settings have no extraKnownMarketplaces object")
	}

	ensureObject := func(key string) (*jsonc.Node, error) {
		node, ok := projectDoc.Root().Member(key)
		if !ok {
			if !projectDoc.SetMember(projectDoc.Root(), key, []byte("{}")) {
				return nil, fmt.Errorf("create Claude settings object %q", key)
			}
			node, ok = projectDoc.Root().Member(key)
		}
		if !ok || node.Kind() != jsonc.KindObject {
			return nil, fmt.Errorf("project Claude settings member %q is not an object", key)
		}
		return node, nil
	}
	projectEnabled, err := ensureObject("enabledPlugins")
	if err != nil {
		return nil, err
	}
	projectMarketplaces, err := ensureObject("extraKnownMarketplaces")
	if err != nil {
		return nil, err
	}

	pluginPrefix := plugin + "@am-"
	copied := 0
	for _, key := range localEnabled.Keys() {
		if !strings.HasPrefix(key, pluginPrefix) {
			continue
		}
		marketplaceKey := strings.TrimPrefix(key, plugin+"@")
		pluginValue, _ := localEnabled.Member(key)
		marketplaceValue, ok := localMarketplaces.Member(marketplaceKey)
		if !ok {
			return nil, fmt.Errorf("local Claude settings omit marketplace %q for plugin %q", marketplaceKey, key)
		}
		if !projectDoc.SetMember(projectEnabled, key, pluginValue.Raw()) ||
			!projectDoc.SetMember(projectMarketplaces, marketplaceKey, marketplaceValue.Raw()) {
			return nil, fmt.Errorf("copy Claude plugin registration %q", key)
		}
		copied++
	}
	if copied == 0 {
		return nil, fmt.Errorf("local Claude settings have no Agent Marketplace registration for plugin %q", plugin)
	}
	return projectDoc.Bytes(), nil
}

func runClaudeRepoPluginVariants(e *Env, binary, plugin, skill string, row *compatibilityResult) {
	if !runClaudeRepoCommittedSettingsVariant(e, binary, plugin, skill, row) {
		recordClaudePluginVariantSetupFailure(row, "C-skills-dir", fmt.Errorf("variant B settings could not be restored"))
		return
	}
	runClaudeRepoSkillsDirectoryVariant(e, binary, plugin, skill, row)
}

func runClaudeRepoCommittedSettingsVariant(e *Env, binary, plugin, skill string, row *compatibilityResult) bool {
	const variant = "B-committed-settings"
	localPath := filepath.Join(e.workspace, ".claude", "settings.local.json")
	projectPath := filepath.Join(e.workspace, ".claude", "settings.json")
	localSettings, err := os.ReadFile(localPath)
	if err != nil {
		recordClaudePluginVariantSetupFailure(row, variant, fmt.Errorf("read local Claude settings: %w", err))
		return true
	}
	projectSettings, err := os.ReadFile(projectPath)
	projectExists := err == nil
	projectMode := os.FileMode(0o644)
	if err != nil && !os.IsNotExist(err) {
		recordClaudePluginVariantSetupFailure(row, variant, fmt.Errorf("read project Claude settings: %w", err))
		return true
	}
	if projectExists {
		info, statErr := os.Stat(projectPath)
		if statErr != nil {
			recordClaudePluginVariantSetupFailure(row, variant, fmt.Errorf("stat project Claude settings: %w", statErr))
			return true
		}
		projectMode = info.Mode().Perm()
	} else {
		projectSettings = []byte("{}\n")
	}
	merged, err := mergeClaudeRepoPluginSettings(projectSettings, localSettings, plugin)
	if err != nil {
		recordClaudePluginVariantSetupFailure(row, variant, err)
		return true
	}
	if err := os.MkdirAll(filepath.Dir(projectPath), 0o755); err != nil {
		recordClaudePluginVariantSetupFailure(row, variant, fmt.Errorf("create project Claude settings directory: %w", err))
		return true
	}
	if err := os.WriteFile(projectPath, merged, projectMode); err != nil {
		recordClaudePluginVariantSetupFailure(row, variant, fmt.Errorf("write project Claude settings: %w", err))
		if restoreErr := restoreClaudeProjectSettings(projectPath, projectSettings, projectExists, projectMode); restoreErr != nil {
			recordClaudePluginVariantSetupFailure(row, variant+"-restore", restoreErr)
			return false
		}
		return true
	}

	primeClaudeRepoPluginHeadless(e, binary, plugin, skill, row)
	runClaudePluginVariantDiagnostics(e, binary, variant, plugin+"@am-", false, plugin, skill, row)
	if err := restoreClaudeProjectSettings(projectPath, projectSettings, projectExists, projectMode); err != nil {
		recordClaudePluginVariantSetupFailure(row, variant+"-restore", err)
		return false
	}
	return true
}

func restoreClaudeProjectSettings(path string, original []byte, existed bool, mode os.FileMode) error {
	if existed {
		if err := os.WriteFile(path, original, mode); err != nil {
			return fmt.Errorf("restore project Claude settings: %w", err)
		}
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove temporary project Claude settings: %w", err)
	}
	return nil
}

func primeClaudeRepoPluginHeadless(e *Env, binary, plugin, skill string, row *compatibilityResult) {
	started := time.Now()
	var registered, skillListed, callbackRan bool
	var pollErr error
	headless := runCompatibilityProtocol(e, binary, "claude-initialize", 60*time.Second, func(command *compatibilityCommand) error {
		callbackRan = true
		deadline := time.Now().Add(45 * time.Second)
		skillListed = recordClaudePluginInitialization(command, e, plugin, skill)
		for !skillListed {
			registered, pollErr = claudeMarketplaceRegistered(e.home, plugin)
			if pollErr != nil || registered {
				break
			}
			remaining := time.Until(deadline)
			if remaining <= 0 {
				break
			}
			time.Sleep(min(2*time.Second, remaining))
		}
		return pollErr
	})
	if !callbackRan {
		skillListed = recordClaudePluginInitialization(&headless, e, plugin, skill)
	}
	headless.Observed = append(headless.Observed,
		fmt.Sprintf("registration mode=headless-variant; registered=%t; skill_listed=%t; elapsed=%s",
			registered, skillListed, time.Since(started).Round(time.Millisecond)),
		claudePluginRegistrationSnapshot(e.home))
	if pollErr != nil {
		headless.Observed = append(headless.Observed,
			"registration mode=headless-variant poll error: "+boundedClaudeDiagnosticValue(pollErr.Error(), 500))
	}
	row.Commands = append(row.Commands, headless)
}

func runClaudeRepoSkillsDirectoryVariant(e *Env, binary, plugin, skill string, row *compatibilityResult) {
	variant := "C-skills-dir-symlink"
	generation, err := claudePluginGenerationDirectory(e, plugin)
	if err != nil {
		recordClaudePluginVariantSetupFailure(row, variant, err)
		return
	}
	destination := filepath.Join(e.workspace, ".claude", "skills", plugin)
	if _, err := os.Lstat(destination); err == nil {
		recordClaudePluginVariantSetupFailure(row, variant, fmt.Errorf("skills-directory target already exists: %q", destination))
		return
	} else if !os.IsNotExist(err) {
		recordClaudePluginVariantSetupFailure(row, variant, fmt.Errorf("inspect skills-directory target %q: %w", destination, err))
		return
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		recordClaudePluginVariantSetupFailure(row, variant, fmt.Errorf("create Claude skills directory: %w", err))
		return
	}
	defer os.RemoveAll(destination)

	if err := os.Symlink(generation, destination); err != nil {
		recordClaudePluginVariantSetupFailure(row, variant, fmt.Errorf("link Claude plugin into skills directory: %w", err))
	} else if runClaudePluginVariantDiagnostics(e, binary, variant, plugin+"@skills-dir", true, plugin, skill, row) {
		return
	} else if err := os.Remove(destination); err != nil {
		recordClaudePluginVariantSetupFailure(row, "C-skills-dir-copy", fmt.Errorf("remove skills-directory symlink: %w", err))
		return
	}

	variant = "C-skills-dir-copy"
	if err := copyClaudePluginDirectory(generation, destination); err != nil {
		recordClaudePluginVariantSetupFailure(row, variant, err)
		return
	}
	runClaudePluginVariantDiagnostics(e, binary, variant, plugin+"@skills-dir", true, plugin, skill, row)
}

func claudePluginGenerationDirectory(e *Env, plugin string) (string, error) {
	pattern := filepath.Join(e.home, ".local", "state", "agent-marketplace", "native-plugins", "claude", "*", ".claude-plugin", "marketplace.json")
	manifests, err := filepath.Glob(pattern)
	if err != nil || len(manifests) != 1 {
		return "", fmt.Errorf("Claude plugin catalog manifests = %v / %v, want one", manifests, err)
	}
	catalogRoot := filepath.Dir(filepath.Dir(manifests[0]))
	link := filepath.Join(catalogRoot, "plugins", plugin)
	info, err := os.Lstat(link)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		return "", fmt.Errorf("Claude catalog plugin path %q is not a symlink: %v", link, err)
	}
	generation, err := filepath.EvalSymlinks(link)
	if err != nil {
		return "", fmt.Errorf("resolve Claude plugin generation %q: %w", link, err)
	}
	return generation, nil
}

func copyClaudePluginDirectory(source, destination string) error {
	entries, err := os.ReadDir(source)
	if err != nil {
		return fmt.Errorf("read Claude plugin generation %q: %w", source, err)
	}
	if err := os.MkdirAll(destination, 0o755); err != nil {
		return fmt.Errorf("create copied Claude plugin %q: %w", destination, err)
	}
	for _, entry := range entries {
		from, to := filepath.Join(source, entry.Name()), filepath.Join(destination, entry.Name())
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("inspect Claude plugin entry %q: %w", from, err)
		}
		switch {
		case entry.IsDir():
			if err := copyClaudePluginDirectory(from, to); err != nil {
				return err
			}
		case info.Mode()&os.ModeSymlink != 0:
			target, err := os.Readlink(from)
			if err != nil {
				return fmt.Errorf("read Claude plugin symlink %q: %w", from, err)
			}
			if err := os.Symlink(target, to); err != nil {
				return fmt.Errorf("copy Claude plugin symlink %q: %w", to, err)
			}
		case info.Mode().IsRegular():
			data, err := os.ReadFile(from)
			if err != nil {
				return fmt.Errorf("read Claude plugin file %q: %w", from, err)
			}
			if err := os.WriteFile(to, data, info.Mode().Perm()); err != nil {
				return fmt.Errorf("write Claude plugin file %q: %w", to, err)
			}
		default:
			return fmt.Errorf("unsupported Claude plugin entry %q", from)
		}
	}
	return nil
}

func runClaudePluginVariantDiagnostics(e *Env, binary, variant, pluginID string, pluginListPresenceLoads bool, plugin, skill string, row *compatibilityResult) bool {
	initialization := runCompatibilityProtocol(e, binary, "claude-initialize", 30*time.Second)
	skillListed := recordClaudePluginInitialization(&initialization, e, plugin, skill)
	if err := compatibilityInspectionError(initialization); err != nil {
		initialization.Observed = append(initialization.Observed, "initialization inventory error: "+boundedClaudeDiagnosticValue(err.Error(), 500))
	}
	row.Commands = append(row.Commands, initialization)

	commands, found, enabled, listErr := claudePluginList(e, binary, pluginID, 20*time.Second)
	list := commands[0]
	list.Observed = append(list.Observed,
		"plugin list --json stdout: "+boundedClaudeDiagnosticValue(strings.Join(strings.Fields(list.Stdout), " "), 2048))
	if listErr != nil {
		list.Observed = append(list.Observed, "plugin list error: "+boundedClaudeDiagnosticValue(listErr.Error(), 500))
	}
	loaded := skillListed || found && (enabled || pluginListPresenceLoads)
	recordClaudePluginVariantOutcome(&list, variant, loaded, skillListed, list.Stdout)
	row.Commands = append(row.Commands, list)
	return loaded
}

func recordClaudePluginVariantSetupFailure(row *compatibilityResult, variant string, err error) {
	command := compatibilityCommand{
		Args:     []string{"Claude plugin variant", variant},
		ExitCode: -1,
		Error:    boundedClaudeDiagnosticValue(err.Error(), 500),
	}
	command.Observed = append(command.Observed, "variant setup error: "+command.Error)
	recordClaudePluginVariantOutcome(&command, variant, false, false, "[]")
	row.Commands = append(row.Commands, command)
}

func recordClaudePluginVariantOutcome(command *compatibilityCommand, variant string, loaded, skillListed bool, raw string) {
	ids, valid := claudePluginJSONIDs(raw)
	pluginList := "<invalid-json>"
	if valid {
		shown := make([]string, 0, min(len(ids), 10))
		for _, id := range ids[:min(len(ids), 10)] {
			shown = append(shown, boundedClaudeDiagnosticValue(id, 120))
		}
		if len(ids) > 10 {
			shown = append(shown, fmt.Sprintf("...+%d", len(ids)-10))
		}
		pluginList = "[" + strings.Join(shown, ",") + "]"
	}
	command.Observed = append(command.Observed,
		fmt.Sprintf("variant=%s loaded=%t plugin_list=%s skill_listed=%t", variant, loaded, pluginList, skillListed))
}

func observeClaudePluginAbsent(e *Env, binary string, f compatibilityFixture, row *compatibilityResult, scope, plugin, skill string) error {
	commands, found, _, err := claudePluginList(e, binary, plugin+"@", 20*time.Second)
	row.Commands = append(row.Commands, commands...)
	if err != nil {
		return err
	}
	if found {
		return fmt.Errorf("Claude plugin negative control already listed %s", plugin)
	}
	command := runCompatibilityProtocol(e, binary, "claude-initialize", 30*time.Second)
	row.Commands = append(row.Commands, command)
	probe := compatibilityNativeProbe{kind: "skill", protocol: "claude-initialize", names: []string{plugin + ":" + skill}}
	if err := requireCompatibilityProtocolInventory(&command, e, f, probe, scope, nil, true); err != nil {
		return err
	}
	return nil
}

func assertClaudePluginStateAbsentAfterSync(home, workspace string) error {
	paths := []string{
		filepath.Join(home, ".claude", "plugins", "installed_plugins.json"),
		filepath.Join(home, ".claude", "plugins", "known_marketplaces.json"),
		filepath.Join(home, ".claude", "plugins", "cache"),
	}
	for _, path := range paths {
		if _, err := os.Lstat(path); err == nil {
			return fmt.Errorf("Claude plugin state exists after Agent Marketplace sync: %q", path)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("inspect Claude plugin state %q after sync: %w", path, err)
		}
	}
	trustPath := filepath.Join(home, ".claude.json")
	data, err := os.ReadFile(trustPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect Claude trust state %q after sync: %w", trustPath, err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		return fmt.Errorf("parse Claude trust state %q after sync: %w", trustPath, err)
	}
	projects, _ := state["projects"].(map[string]any)
	project, _ := projects[workspace].(map[string]any)
	if accepted, _ := project["hasTrustDialogAccepted"].(bool); accepted {
		return fmt.Errorf("Claude workspace trust state exists after Agent Marketplace sync: %q", trustPath)
	}
	return nil
}

func establishClaudeWorkspaceTrust(home, workspace string) error {
	path := filepath.Join(home, ".claude.json")
	state := map[string]any{}
	data, err := os.ReadFile(path)
	if err == nil {
		if err := json.Unmarshal(data, &state); err != nil {
			return fmt.Errorf("parse Claude state %q: %w", path, err)
		}
		if state == nil {
			return fmt.Errorf("Claude state %q is not an object", path)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read Claude state %q: %w", path, err)
	}
	projects, exists := state["projects"]
	projectMap, ok := projects.(map[string]any)
	if exists && !ok {
		return fmt.Errorf("Claude projects state in %q is not an object", path)
	}
	if !ok {
		projectMap = map[string]any{}
		state["projects"] = projectMap
	}
	projectValue, exists := projectMap[workspace]
	projectState, ok := projectValue.(map[string]any)
	if exists && !ok {
		return fmt.Errorf("Claude workspace state in %q is not an object", path)
	}
	if !ok {
		projectState = map[string]any{}
		projectMap[workspace] = projectState
	}
	projectState["hasTrustDialogAccepted"] = true
	state["hasCompletedOnboarding"] = true
	data, err = json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode Claude state %q: %w", path, err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write Claude state %q: %w", path, err)
	}
	return nil
}

func primeClaudePluginSession(e *Env, binary, pluginName string, row *compatibilityResult) {
	var headlessRegistered bool
	var headlessElapsed time.Duration
	headlessStarted := time.Now()
	var headlessPollErr error
	headless := runCompatibilityProtocol(e, binary, "claude-initialize", 60*time.Second, func(command *compatibilityCommand) error {
		headlessRegistered, headlessElapsed, headlessPollErr = waitForClaudeMarketplaceRegistration(e.home, pluginName, 45*time.Second)
		command.Observed = append(command.Observed, fmt.Sprintf("registration mode=headless; registered=%t; elapsed=%s", headlessRegistered, headlessElapsed.Round(time.Millisecond)))
		return headlessPollErr
	})
	if headlessElapsed == 0 {
		headlessElapsed = time.Since(headlessStarted)
		headless.Observed = append(headless.Observed, fmt.Sprintf("registration mode=headless; registered=false; elapsed=%s", headlessElapsed.Round(time.Millisecond)))
	}
	if headlessPollErr != nil {
		headless.Observed = append(headless.Observed, "registration mode=headless poll error: "+headlessPollErr.Error())
	}
	headless.Observed = append(headless.Observed, claudePluginRegistrationSnapshot(e.home))
	row.Commands = append(row.Commands, headless)
	if headlessRegistered {
		return
	}

	interactive, interactiveRegistered, interactiveElapsed, err := primeClaudeInteractiveSession(e, binary, pluginName, 60*time.Second)
	interactive.Observed = append(interactive.Observed, claudePluginRegistrationSnapshot(e.home))
	if err != nil {
		interactive.Observed = append(interactive.Observed, "registration mode=interactive error: "+err.Error())
	}
	row.Commands = append(row.Commands, interactive)
	if interactiveRegistered {
		return
	}
	row.Commands[len(row.Commands)-1].Observed = append(
		row.Commands[len(row.Commands)-1].Observed,
		fmt.Sprintf("registration mode=none; headless elapsed=%s; interactive elapsed=%s",
			headlessElapsed.Round(time.Millisecond), interactiveElapsed.Round(time.Millisecond)),
	)
}

func waitForClaudeMarketplaceRegistration(home, pluginName string, timeout time.Duration) (bool, time.Duration, error) {
	started := time.Now()
	deadline := started.Add(timeout)
	for {
		registered, err := claudeMarketplaceRegistered(home, pluginName)
		if err != nil {
			return false, time.Since(started), err
		}
		if registered {
			return true, time.Since(started), nil
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return false, time.Since(started), nil
		}
		time.Sleep(min(2*time.Second, remaining))
	}
}

func claudeMarketplaceRegistered(home, pluginName string) (bool, error) {
	marketplaceRegistered, err := claudeMarketplaceHasAMCatalog(home)
	if err != nil {
		return false, err
	}
	installedPlugins, err := claudeInstalledPluginRecords(home)
	if err != nil {
		return false, err
	}
	for _, installed := range installedPlugins {
		if strings.HasPrefix(installed.name, pluginName+"@am-") {
			return marketplaceRegistered, nil
		}
	}
	return false, nil
}

func claudeMarketplaceHasAMCatalog(home string) (bool, error) {
	path := filepath.Join(home, ".claude", "plugins", "known_marketplaces.json")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read Claude marketplace state %q: %w", path, err)
	}
	return strings.Contains(string(data), `"am-`), nil
}

type claudeInstalledPluginRecord struct {
	name   string
	scopes []string
}

func claudeInstalledPluginRecords(home string) ([]claudeInstalledPluginRecord, error) {
	path := filepath.Join(home, ".claude", "plugins", "installed_plugins.json")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read Claude installed-plugin state %q: %w", path, err)
	}
	var state struct {
		Plugins map[string]json.RawMessage `json:"plugins"`
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parse Claude installed-plugin state %q: %w", path, err)
	}
	records := make([]claudeInstalledPluginRecord, 0, len(state.Plugins))
	for name, entries := range state.Plugins {
		records = append(records, claudeInstalledPluginRecord{name: name, scopes: claudePluginRecordScopes(entries)})
	}
	sort.Slice(records, func(i, j int) bool { return records[i].name < records[j].name })
	return records, nil
}

func claudePluginRecordScopes(entries json.RawMessage) []string {
	var value any
	if json.Unmarshal(entries, &value) != nil {
		return nil
	}
	scopes := map[string]struct{}{}
	var visit func(any)
	visit = func(current any) {
		switch item := current.(type) {
		case map[string]any:
			if scope, ok := item["scope"].(string); ok && scope != "" {
				scopes[scope] = struct{}{}
			}
			for _, child := range item {
				visit(child)
			}
		case []any:
			for _, child := range item {
				visit(child)
			}
		}
	}
	visit(value)
	result := make([]string, 0, len(scopes))
	for scope := range scopes {
		result = append(result, scope)
	}
	sort.Strings(result)
	return result
}

func claudePluginRegistrationSnapshot(home string) string {
	marketplace, marketplaceErr := claudeMarketplaceHasAMCatalog(home)
	plugins, pluginsErr := claudeInstalledPluginRecords(home)
	if marketplaceErr != nil {
		return fmt.Sprintf("registration snapshot: known_marketplaces_am_catalog=error(%v); installed_plugins_error=%v", marketplaceErr, pluginsErr)
	}
	if pluginsErr != nil {
		return fmt.Sprintf("registration snapshot: known_marketplaces_am_catalog=%t; installed_plugins_error=%v", marketplace, pluginsErr)
	}
	entries := make([]string, 0, len(plugins))
	for _, plugin := range plugins {
		entry := plugin.name
		if len(plugin.scopes) > 0 {
			entry += "(scope=" + strings.Join(plugin.scopes, ",") + ")"
		}
		if len(entry) > 120 {
			entry = entry[:117] + "..."
		}
		entries = append(entries, entry)
		if len(entries) == 8 {
			break
		}
	}
	if len(plugins) > len(entries) {
		entries = append(entries, fmt.Sprintf("...+%d", len(plugins)-len(entries)))
	}
	return fmt.Sprintf("registration snapshot: known_marketplaces_am_catalog=%t; installed_plugins=[%s]",
		marketplace, strings.Join(entries, ","))
}

func primeClaudeInteractiveSession(e *Env, binary, pluginName string, timeout time.Duration) (compatibilityCommand, bool, time.Duration, error) {
	command := compatibilityCommand{Args: []string{binary}, ExitCode: -1}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	process, err := compatibilityProcess(ctx, e, binary, nil)
	if err != nil {
		command.Error = err.Error()
		return command, false, 0, err
	}
	process.SysProcAttr.Setpgid = false
	terminal, err := pty.Start(process)
	if err != nil {
		command.Error = "interactive Claude session could not be started"
		return command, false, 0, err
	}
	var output compatibilityOutput
	outputDone := make(chan struct{})
	go func() {
		defer close(outputDone)
		_, _ = io.Copy(&output, terminal)
	}()
	processDone := make(chan error, 1)
	go func() { processDone <- process.Wait() }()

	started := time.Now()
	deadline := started.Add(timeout)
	registered, processFinished := false, false
	var waitErr, pollErr error
	for !registered && !processFinished && pollErr == nil {
		registered, pollErr = claudeMarketplaceRegistered(e.home, pluginName)
		if registered || pollErr != nil {
			break
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			command.TimedOut = true
			break
		}
		timer := time.NewTimer(min(2*time.Second, remaining))
		select {
		case waitErr = <-processDone:
			processFinished = true
		case <-timer.C:
		case <-ctx.Done():
			command.TimedOut = ctx.Err() == context.DeadlineExceeded
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		if command.TimedOut {
			break
		}
	}
	elapsed := time.Since(started)
	if registered && !processFinished {
		_, _ = terminal.Write([]byte{3})
		select {
		case waitErr = <-processDone:
			processFinished = true
		case <-time.After(2 * time.Second):
		}
	}
	if !processFinished {
		cancel()
		waitErr = <-processDone
	}
	_ = terminal.Close()
	<-outputDone
	command.Stdout = string(output.data)
	command.output = command.Stdout
	command.OutputBytes = output.total
	command.OutputTruncated = output.total > compatibilityOutputLimit
	command.Diagnostics = compatibilityDiagnostics(command.Stdout)
	if process.ProcessState != nil {
		command.ExitCode = process.ProcessState.ExitCode()
	}
	command.Observed = append(command.Observed,
		fmt.Sprintf("registration mode=interactive; registered=%t; elapsed=%s", registered, elapsed.Round(time.Millisecond)))
	switch {
	case pollErr != nil:
		command.Error = pollErr.Error()
	case command.OutputTruncated:
		command.Error = "interactive Claude session output exceeded bounded capture limit"
	case command.TimedOut && !registered:
		command.Error = "interactive Claude session timed out waiting for marketplace registration"
	case !registered && processFinished:
		if waitErr != nil {
			command.Error = "interactive Claude session exited before marketplace registration"
		} else {
			command.Error = "interactive Claude session ended before marketplace registration"
		}
	}
	if command.Error != "" {
		return command, registered, elapsed, fmt.Errorf("%s", command.Error)
	}
	return command, registered, elapsed, nil
}

func observeClaudePluginEnabled(e *Env, binary string, f compatibilityFixture, row *compatibilityResult, scope, plugin, skill string) error {
	settingsPath := filepath.Join(e.home, ".claude", "settings.json")
	if scope == "repo" {
		settingsPath = filepath.Join(e.workspace, ".claude", "settings.local.json")
	}
	data, err := os.ReadFile(settingsPath)
	pluginID := ""
	var registrationErr error
	if err != nil {
		registrationErr = fmt.Errorf("read Claude native plugin registration %q: %w", settingsPath, err)
	} else if document, parseErr := jsonc.Parse(settingsPath, data); parseErr != nil {
		registrationErr = parseErr
	} else if enabled, ok := document.Root().Member("enabledPlugins"); !ok {
		registrationErr = fmt.Errorf("Claude plugin registration has no enabledPlugins")
	} else {
		for _, key := range enabled.Keys() {
			value, _ := enabled.Member(key)
			if strings.HasPrefix(key, plugin+"@am-") && string(value.Raw()) == "true" {
				pluginID = key
				break
			}
		}
		if pluginID == "" {
			registrationErr = fmt.Errorf("Claude plugin registration is not enabled for %s@am-*", plugin)
		}
	}
	if pluginID == "" {
		pluginID = plugin + "@am-"
	}
	command := runCompatibilityProtocol(e, binary, "claude-initialize", 30*time.Second)
	skillListed := recordClaudePluginInitialization(&command, e, plugin, skill)
	initErr := requireCompatibilityProtocolInventory(
		&command,
		e,
		f,
		compatibilityNativeProbe{kind: "skill", protocol: "claude-initialize", names: []string{plugin + ":" + skill}},
		scope,
		nil,
		false,
	)
	row.Commands = append(row.Commands, command)
	pluginListErr := waitForClaudePluginEnabled(e, binary, pluginID, row)
	if scope == "repo" {
		for i := len(row.Commands) - 1; i >= 0; i-- {
			list := &row.Commands[i]
			if len(list.Args) < 4 || list.Args[1] != "plugin" || list.Args[2] != "list" {
				continue
			}
			found, active, valid := claudePluginJSONState(list.Stdout, pluginID)
			recordClaudePluginVariantOutcome(list, "A-repo-settings-local", valid && found && active || skillListed, skillListed, list.Stdout)
			break
		}
	}
	var verdictErrors []string
	for _, verdictErr := range []error{registrationErr, initErr, pluginListErr} {
		if verdictErr != nil {
			verdictErrors = append(verdictErrors, verdictErr.Error())
		}
	}
	if len(verdictErrors) > 0 {
		return fmt.Errorf("Claude plugin verdict failed: %s", strings.Join(verdictErrors, "; "))
	}
	row.Status = "native-verified"
	row.Reason = "Claude plugin list and initialization inventory confirmed the native plugin and namespaced skill"
	return nil
}

func recordClaudePluginInitialization(command *compatibilityCommand, e *Env, plugin, skill string) bool {
	var fields map[string]json.RawMessage
	if json.Unmarshal([]byte(command.output), &fields) != nil || fields == nil {
		command.Observed = append(command.Observed, "Claude initialization plugins: response metadata unavailable")
	} else {
		recordClaudePluginListField(command, "stream-json system init.plugins", fields[claudeSystemInitPluginsField])
		recordClaudePluginListField(command, "initialize control response.plugins", fields["plugins"])
	}
	listed := false
	entries, err := compatibilityProtocolInventory(command.output, "claude-initialize", "skill", e.workspace)
	if err == nil && compatibilityInspectionError(*command) == nil {
		for _, entry := range entries {
			listed = listed || entry.Name == plugin+":"+skill
		}
	}
	command.Observed = append(command.Observed, fmt.Sprintf("Claude namespaced skill %s:%s listed=%t", plugin, skill, listed))
	return listed
}

func recordClaudePluginListField(command *compatibilityCommand, field string, raw json.RawMessage) {
	if len(raw) == 0 {
		command.Observed = append(command.Observed, "Claude "+field+": field absent")
		return
	}
	var plugins []struct {
		Name string `json:"name"`
		Path string `json:"path"`
	}
	if json.Unmarshal(raw, &plugins) != nil {
		command.Observed = append(command.Observed, "Claude "+field+": present but not a plugin list")
		return
	}
	entries := make([]string, 0, min(len(plugins), 20))
	for _, plugin := range plugins[:min(len(plugins), 20)] {
		name := boundedClaudeDiagnosticValue(plugin.Name, 120)
		path := boundedClaudeDiagnosticValue(plugin.Path, 180)
		if name == "" {
			continue
		}
		if path != "" {
			entries = append(entries, name+" path="+path)
		} else {
			entries = append(entries, name)
		}
	}
	if len(plugins) > 20 {
		entries = append(entries, fmt.Sprintf("...+%d", len(plugins)-20))
	}
	command.Observed = append(command.Observed, "Claude "+field+": ["+strings.Join(entries, ",")+"]")
}

func boundedClaudeDiagnosticValue(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	return value[:limit-3] + "..."
}

func waitForClaudePluginEnabled(e *Env, binary, pluginID string, row *compatibilityResult) error {
	deadline := time.Now().Add(60 * time.Second)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return fmt.Errorf("Claude plugin list did not report %s enabled within 60 seconds", pluginID)
		}
		commands, found, active, err := claudePluginList(e, binary, pluginID, min(30*time.Second, remaining))
		for i := range commands {
			commands[i].Observed = append(commands[i].Observed, "plugin list --json stdout: "+commands[i].Stdout)
		}
		row.Commands = append(row.Commands, commands...)
		if err != nil {
			return err
		}
		if found && active {
			return nil
		}
		time.Sleep(min(time.Second, time.Until(deadline)))
	}
}

func claudePluginList(e *Env, binary, match string, timeout time.Duration) ([]compatibilityCommand, bool, bool, error) {
	command := runCompatibilityCommandWithSeparateOutput(e, binary, []string{"plugin", "list", "--json"}, timeout)
	if err := compatibilityInspectionError(command); err != nil {
		return []compatibilityCommand{command}, false, false, fmt.Errorf("Claude plugin list --json failed: %w", err)
	}
	present, enabled, valid := claudePluginJSONState(command.Stdout, match)
	if !valid {
		return []compatibilityCommand{command}, false, false, fmt.Errorf("Claude plugin list --json returned invalid JSON")
	}
	return []compatibilityCommand{command}, present, enabled, nil
}

func claudePluginJSONState(raw, match string) (bool, bool, bool) {
	var value any
	if json.Unmarshal([]byte(raw), &value) != nil {
		return false, false, false
	}
	present, enabled := false, false
	var visit func(any)
	visit = func(current any) {
		switch item := current.(type) {
		case map[string]any:
			identity := ""
			for _, key := range []string{"name", "id", "plugin"} {
				if candidate, ok := item[key].(string); ok && candidate != "" {
					identity = candidate
					break
				}
			}
			if identity != "" && (identity == match || strings.HasPrefix(identity, match)) {
				present = true
				if state, ok := item["enabled"].(bool); ok {
					enabled = enabled || state
				}
				if state, ok := item["status"].(string); ok && strings.EqualFold(state, "enabled") {
					enabled = true
				}
			}
			for key, child := range item {
				if key == match || strings.HasPrefix(key, match) {
					present = true
					if state, ok := child.(bool); ok {
						enabled = enabled || state
					}
				}
				visit(child)
			}
		case []any:
			for _, child := range item {
				visit(child)
			}
		}
	}
	visit(value)
	return present, enabled, value != nil
}

func claudePluginJSONIDs(raw string) ([]string, bool) {
	var value any
	if json.Unmarshal([]byte(raw), &value) != nil || value == nil {
		return nil, false
	}
	switch value.(type) {
	case map[string]any, []any:
	default:
		return nil, false
	}
	found := make(map[string]struct{})
	var visit func(any)
	visit = func(current any) {
		switch item := current.(type) {
		case map[string]any:
			for _, key := range []string{"name", "id", "plugin"} {
				if id, ok := item[key].(string); ok && strings.Contains(id, "@") {
					found[id] = struct{}{}
				}
			}
			for key, child := range item {
				if strings.Contains(key, "@") {
					found[key] = struct{}{}
				}
				visit(child)
			}
		case []any:
			for _, child := range item {
				visit(child)
			}
		}
	}
	visit(value)
	ids := make([]string, 0, len(found))
	for id := range found {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, true
}
