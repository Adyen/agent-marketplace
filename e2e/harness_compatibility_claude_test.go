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

func TestClaudeWorkspaceTrustRealPathCoversLiteralAndCanonicalPaths(t *testing.T) {
	home := t.TempDir()
	workspace := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "workspace-link")
	if err := os.Symlink(workspace, alias); err != nil {
		t.Fatal(err)
	}

	if err := establishClaudeWorkspaceTrustAtRealPath(home, alias); err != nil {
		t.Fatalf("establish Claude trust for literal and real paths: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(home, ".claude.json"))
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatalf("decode Claude trust state: %v", err)
	}
	projects, ok := state["projects"].(map[string]any)
	if !ok {
		t.Fatalf("Claude project trust entries missing: %s", data)
	}
	realPath, err := filepath.EvalSymlinks(alias)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{alias, realPath} {
		project, ok := projects[path].(map[string]any)
		if !ok || project["hasTrustDialogAccepted"] != true {
			t.Errorf("Claude project trust for %q = %v, want accepted", path, projects[path])
		}
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

func TestMergeClaudeRepoPluginSettingsReplacesNonObjectOwnedMembers(t *testing.T) {
	project := []byte(`{
  "enabledPlugins": false,
  "extraKnownMarketplaces": [],
  "customSetting": "keep"
}`)
	local := []byte(`{
  "enabledPlugins": {"native-kit@am-catalog": true},
  "extraKnownMarketplaces": {"am-catalog": {"source": {"type": "directory", "path": "../catalog"}}}
}`)

	merged, err := mergeClaudeRepoPluginSettings(project, local, "native-kit")
	if err != nil {
		t.Fatalf("merge Claude repo plugin settings into existing project settings: %v", err)
	}
	document, err := jsonc.Parse("settings.json", merged)
	if err != nil {
		t.Fatalf("parse merged Claude settings: %v", err)
	}
	plugins, ok := document.Root().Member("enabledPlugins")
	if !ok {
		t.Fatalf("merged settings omitted enabledPlugins:\n%s", merged)
	}
	if value, ok := plugins.Member("native-kit@am-catalog"); !ok || string(value.Raw()) != "true" {
		t.Errorf("merged plugin registration = %v, want enabled native-kit@am-catalog", value)
	}
	marketplaces, ok := document.Root().Member("extraKnownMarketplaces")
	if !ok {
		t.Fatalf("merged settings omitted extraKnownMarketplaces:\n%s", merged)
	}
	if entry, ok := marketplaces.Member("am-catalog"); !ok ||
		!strings.Contains(string(entry.Raw()), `"path": "../catalog"`) {
		t.Errorf("merged marketplace registration = %v, want the owned relative catalog entry", entry)
	}
	custom, ok := document.Root().Member("customSetting")
	if !ok || string(custom.Raw()) != `"keep"` {
		t.Errorf("custom setting = %v, want preserved value %q", custom, `"keep"`)
	}
}

func TestClaudeScopeEvidenceCapturesSettingsMarketplaceAndTrust(t *testing.T) {
	e := newEnv(t)
	homeSettings := filepath.Join(e.home, ".claude", "settings.json")
	localSettings := filepath.Join(e.workspace, ".claude", "settings.local.json")
	projectSettings := filepath.Join(e.workspace, ".claude", "settings.json")
	e.WriteFile(homeSettings, `{"enabledPlugins":{"native-kit@am-user":true},"extraKnownMarketplaces":{"am-user":{"source":{"type":"directory","path":"/catalog/user"}}}}`)
	e.WriteFile(localSettings, `{"enabledPlugins":{"native-kit@am-repo":true},"extraKnownMarketplaces":{"am-repo":{"source":{"type":"directory","path":"../catalog"}}}}`)
	e.WriteFile(projectSettings, `{"enabledPlugins":{"handwritten@manual":true},"extraKnownMarketplaces":{"manual":{"source":{"type":"github","repo":"owner/repo"}}}}`)
	e.WriteFile(filepath.Join(e.home, ".claude", "plugins", "known_marketplaces.json"),
		`{"am-repo":{"source":{"type":"directory","path":"../catalog"},"installLocation":"/home/.claude/plugins/marketplaces/am-repo"}}`)
	e.WriteFile(filepath.Join(e.home, ".claude.json"),
		`{"projects":{"`+e.workspace+`":{"hasTrustDialogAccepted":true},"/tmp/claude-realpath":{"hasTrustDialogAccepted":true}}}`)

	var repo compatibilityCommand
	recordClaudeScopeEvidence(&repo, e, "repo")
	repoObserved := strings.Join(repo.Observed, "\n")
	for _, want := range []string{
		"scope=repo file=settings.local.json",
		`enabledPlugins.keys=[native-kit@am-repo]`,
		`am-repo={"source":{"type":"directory","path":"../catalog"}}`,
		"same_file=true",
		"scope=repo file=settings.json",
		`enabledPlugins.keys=[handwritten@manual]`,
		"same_file=true",
		`am-repo source={"path":"../catalog","type":"directory"}`,
		`installLocation="/home/.claude/plugins/marketplaces/am-repo"`,
		"Claude trust projects.keys=[",
		`"/tmp/claude-realpath"`,
		"workspace realpath=",
		"os.Getwd=",
	} {
		if !strings.Contains(repoObserved, want) {
			t.Errorf("repo evidence %q does not contain %q", repoObserved, want)
		}
	}

	var user compatibilityCommand
	recordClaudeScopeEvidence(&user, e, "user")
	userObserved := strings.Join(user.Observed, "\n")
	for _, want := range []string{
		"scope=user file=settings.json",
		`enabledPlugins.keys=[native-kit@am-user]`,
		`am-user={"source":{"type":"directory","path":"/catalog/user"}}`,
		"same_file=true",
	} {
		if !strings.Contains(userObserved, want) {
			t.Errorf("user evidence %q does not contain %q", userObserved, want)
		}
	}
}

func TestClaudeRepoVariantsRecordA_B_DOutcomesFromProjectRoot(t *testing.T) {
	e := newEnv(t)
	const plugin, skill = "native-kit", "review"
	binary := filepath.Join(e.repos, "internal-fake-claude")
	e.WriteFile(binary, `#!/bin/sh
pwd -P >> "$HOME/claude-cwds"
if [ "$1" = "plugin" ]; then
	count=0
	if [ -f "$HOME/plugin-list-count" ]; then count=$(cat "$HOME/plugin-list-count"); fi
	count=$((count + 1))
	printf '%s\n' "$count" > "$HOME/plugin-list-count"
	if [ "$count" -eq 1 ]; then
		printf '%s\n' 'not-json'
	else
		printf '%s\n' '{"plugins":[{"name":"native-kit@am-catalog","enabled":true}]}'
	fi
	exit 0
fi
IFS= read -r request || exit 70
case "$request" in *'"subtype":"initialize"'*) ;; *) exit 71;; esac
printf '{"type":"system","subtype":"init","cwd":"%s","plugins":[]}\n' "$(pwd -P)"
case " $* " in
	*" --debug "*)
		printf '%s\n' '{"type":"control_response","response":{"subtype":"success","request_id":"amcompat-initialize","response":{"commands":[{"name":"native-kit:review","description":"Compatibility fixture native-kit:review-skill"}],"agents":[]}}}'
		;;
	*)
		printf '%s\n' '{"type":"control_response","response":{"subtype":"success","request_id":"amcompat-initialize","response":{"commands":[],"agents":[]}}}'
		;;
esac
if IFS= read -r request; then exit 72; fi
`)
	if err := os.Chmod(binary, 0o755); err != nil {
		t.Fatal(err)
	}
	localSettings := []byte(`{"enabledPlugins":{"native-kit@am-catalog":true},"extraKnownMarketplaces":{"am-catalog":{"source":{"type":"directory","path":"../catalog"}}}}`)
	projectSettings := []byte(`{"enabledPlugins":false,"extraKnownMarketplaces":[],"customSetting":"keep"}`)
	e.WriteFile(filepath.Join(e.workspace, ".claude", "settings.local.json"), string(localSettings))
	e.WriteFile(filepath.Join(e.workspace, ".claude", "settings.json"), string(projectSettings))
	e.WriteFile(filepath.Join(e.home, ".claude", "plugins", "known_marketplaces.json"),
		`{"am-catalog":{"source":{"type":"directory","path":"../catalog"},"installLocation":"/catalog"}}`)
	e.WriteFile(filepath.Join(e.home, ".claude", "plugins", "installed_plugins.json"),
		`{"plugins":{"native-kit@am-catalog":[{"scope":"repo"}]}}`)

	var row compatibilityResult
	fixture := compatibilityFixture{prefix: plugin + ":" + skill}
	if err := observeClaudePluginEnabled(e, binary, fixture, &row, "repo", plugin, skill); err == nil {
		t.Fatal("variant A unexpectedly passed its failure witness")
	}
	runClaudeRepoPluginVariants(e, binary, plugin, skill, &row)
	observed := []string{}
	for _, command := range row.Commands {
		observed = append(observed, command.Observed...)
	}
	report := strings.Join(observed, "\n")
	for _, want := range []string{
		"variant=A-repo-settings-local loaded=false plugin_list=<invalid-json> skill_listed=false",
		"variant=B-committed-settings loaded=true plugin_list=[native-kit@am-catalog] skill_listed=false",
		"variant=D-realpath-trust loaded=true plugin_list=[native-kit@am-catalog] skill_listed=false",
		`Claude settings scope=repo file=settings.json enabledPlugins.keys=[native-kit@am-catalog] extraKnownMarketplaces=[am-catalog={"source":{"type":"directory","path":"../catalog"}}]`,
		"Claude initialization cwd: ",
	} {
		if !strings.Contains(report, want) {
			t.Errorf("repo variant report %q does not contain %q", report, want)
		}
	}
	restored, err := os.ReadFile(filepath.Join(e.workspace, ".claude", "settings.json"))
	if err != nil || string(restored) != string(projectSettings) {
		t.Errorf("committed Claude settings after variant B = %q, want original %q (error %v)",
			restored, projectSettings, err)
	}
	realWorkspace, err := filepath.EvalSymlinks(e.workspace)
	if err != nil {
		t.Fatal(err)
	}
	var trust struct {
		Projects map[string]map[string]any `json:"projects"`
	}
	trustBytes, err := os.ReadFile(filepath.Join(e.home, ".claude.json"))
	if err != nil || json.Unmarshal(trustBytes, &trust) != nil {
		t.Fatalf("read variant D trust state: %v\n%s", err, trustBytes)
	}
	for _, path := range []string{e.workspace, realWorkspace} {
		if trust.Projects[path]["hasTrustDialogAccepted"] != true {
			t.Errorf("variant D trust state for %q = %v, want accepted", path, trust.Projects[path])
		}
	}
	cwdLog, err := os.ReadFile(filepath.Join(e.home, "claude-cwds"))
	if err != nil {
		t.Fatal(err)
	}
	for _, cwd := range strings.Fields(string(cwdLog)) {
		if cwd != realWorkspace {
			t.Errorf("Claude cwd = %q, want project root %q", cwd, realWorkspace)
		}
	}
	if len(strings.Fields(string(cwdLog))) == 0 {
		t.Fatal("repo variants did not launch any Claude process")
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
		} else if node.Kind() != jsonc.KindObject {
			if !projectDoc.SetMember(projectDoc.Root(), key, []byte("{}")) {
				return nil, fmt.Errorf("replace Claude settings object %q", key)
			}
		}
		if !ok || node.Kind() != jsonc.KindObject {
			projectDoc, err = jsonc.Parse("settings.json", projectDoc.Bytes())
			if err != nil {
				return nil, fmt.Errorf("reparse Claude settings after creating %q: %w", key, err)
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
	projectEnabled, _ = projectDoc.Root().Member("enabledPlugins")
	projectMarketplaces, _ = projectDoc.Root().Member("extraKnownMarketplaces")

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
		recordClaudePluginVariantSetupFailure(row, "D-realpath-trust", fmt.Errorf("variant B settings could not be restored"))
		return
	}
	runClaudeRepoRealpathTrustVariant(e, binary, plugin, skill, row)
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
		recordClaudePluginVariantSetupFailure(row, variant, err)
		return false
	}
	return true
}

func runClaudeRepoRealpathTrustVariant(e *Env, binary, plugin, skill string, row *compatibilityResult) {
	const variant = "D-realpath-trust"
	if err := establishClaudeWorkspaceTrustAtRealPath(e.home, e.workspace); err != nil {
		recordClaudePluginVariantSetupFailure(row, variant, err)
		return
	}
	primeClaudeRepoPluginHeadless(e, binary, plugin, skill, row)
	runClaudePluginVariantDiagnostics(e, binary, variant, plugin+"@am-", false, plugin, skill, row)
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
	recordClaudeScopeEvidence(&list, e, "repo")
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

func recordClaudeScopeEvidence(command *compatibilityCommand, e *Env, scope string) {
	settings := []struct {
		label string
		path  string
	}{}
	if scope == "repo" {
		settings = append(settings,
			struct {
				label string
				path  string
			}{"repo", filepath.Join(e.workspace, ".claude", "settings.local.json")},
			struct {
				label string
				path  string
			}{"repo", filepath.Join(e.workspace, ".claude", "settings.json")})
	} else {
		settings = append(settings, struct {
			label string
			path  string
		}{"user", filepath.Join(e.home, ".claude", "settings.json")})
	}
	for _, file := range settings {
		command.Observed = append(command.Observed, claudeSettingsRegistrationEvidence(file.label, file.path))
	}
	command.Observed = append(command.Observed,
		claudeKnownMarketplaceEvidence(e.home),
		claudeWorkspaceTrustEvidence(e.home, e.workspace))
}

func claudeSettingsRegistrationEvidence(scope, path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Sprintf("Claude settings scope=%s file=%s error=%s",
			scope, filepath.Base(path), boundedClaudeDiagnosticValue(err.Error(), 300))
	}
	document, err := jsonc.Parse(path, data)
	if err != nil || document.Root().Kind() != jsonc.KindObject {
		if err == nil {
			err = fmt.Errorf("settings root is not an object")
		}
		return fmt.Sprintf("Claude settings scope=%s file=%s error=%s",
			scope, filepath.Base(path), boundedClaudeDiagnosticValue(err.Error(), 300))
	}

	enabled, enabledOK := document.Root().Member("enabledPlugins")
	enabledKeys := []string{}
	if enabledOK && enabled.Kind() == jsonc.KindObject {
		enabledKeys = enabled.Keys()
		sort.Strings(enabledKeys)
	} else if enabledOK {
		enabledKeys = []string{"<non-object>"}
	} else {
		enabledKeys = []string{"<missing>"}
	}
	marketplaces, marketplacesOK := document.Root().Member("extraKnownMarketplaces")
	marketplaceEntries := []string{}
	if marketplacesOK && marketplaces.Kind() == jsonc.KindObject {
		for _, key := range marketplaces.Keys() {
			if !strings.HasPrefix(key, "am-") {
				continue
			}
			entry, ok := marketplaces.Member(key)
			if ok {
				marketplaceEntries = append(marketplaceEntries,
					key+"="+boundedClaudeDiagnosticValue(strings.Join(strings.Fields(string(entry.Raw())), " "), 500))
			}
		}
		sort.Strings(marketplaceEntries)
	}
	if !marketplacesOK {
		marketplaceEntries = []string{"<missing>"}
	} else if marketplaces.Kind() != jsonc.KindObject {
		marketplaceEntries = []string{"<non-object>"}
	} else if len(marketplaceEntries) == 0 {
		marketplaceEntries = []string{"<no-am-entry>"}
	}
	sameFile := enabledOK && enabled.Kind() == jsonc.KindObject &&
		marketplacesOK && marketplaces.Kind() == jsonc.KindObject
	return fmt.Sprintf("Claude settings scope=%s file=%s enabledPlugins.keys=%s extraKnownMarketplaces=%s same_file=%t",
		scope, filepath.Base(path), boundedClaudeEvidenceList(enabledKeys, 20, 160),
		boundedClaudeEvidenceList(marketplaceEntries, 10, 520), sameFile)
}

func claudeKnownMarketplaceEvidence(home string) string {
	path := filepath.Join(home, ".claude", "plugins", "known_marketplaces.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return "Claude known_marketplaces am entries: " + boundedClaudeDiagnosticValue(err.Error(), 300)
	}
	var marketplaces map[string]json.RawMessage
	if err := json.Unmarshal(data, &marketplaces); err != nil {
		return "Claude known_marketplaces am entries: invalid-json"
	}
	keys := make([]string, 0, len(marketplaces))
	for key := range marketplaces {
		if strings.HasPrefix(key, "am-") {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	entryLimit := min(len(keys), 9)
	entries := make([]string, 0, entryLimit+1)
	for _, key := range keys[:entryLimit] {
		var entry struct {
			Source          json.RawMessage `json:"source"`
			InstallLocation json.RawMessage `json:"installLocation"`
		}
		if err := json.Unmarshal(marketplaces[key], &entry); err != nil {
			entries = append(entries, key+"=<invalid-entry>")
			continue
		}
		entries = append(entries, key+" source="+compactClaudeJSON(entry.Source)+
			" installLocation="+compactClaudeJSON(entry.InstallLocation))
	}
	if len(keys) > entryLimit {
		entries = append(entries, fmt.Sprintf("...+%d", len(keys)-entryLimit))
	}
	if len(entries) == 0 {
		entries = []string{"<none>"}
	}
	return "Claude known_marketplaces am entries: " + boundedClaudeEvidenceList(entries, 10, 900)
}

func claudeWorkspaceTrustEvidence(home, workspace string) string {
	path := filepath.Join(home, ".claude.json")
	data, readErr := os.ReadFile(path)
	projects := map[string]json.RawMessage{}
	if readErr == nil {
		var state struct {
			Projects map[string]json.RawMessage `json:"projects"`
		}
		if err := json.Unmarshal(data, &state); err != nil {
			readErr = err
		} else if state.Projects != nil {
			projects = state.Projects
		}
	} else if !os.IsNotExist(readErr) {
		readErr = fmt.Errorf("read %s: %w", filepath.Base(path), readErr)
	}
	keys := make([]string, 0, len(projects))
	for key := range projects {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	shownKeys := keys[:min(len(keys), 20)]
	keyJSON, _ := json.Marshal(shownKeys)
	entries := make([]string, 0, len(shownKeys))
	for _, key := range shownKeys {
		entries = append(entries, key+"="+compactClaudeJSON(projects[key]))
	}
	entryList := boundedClaudeEvidenceList(entries, 10, 300)
	cwd, cwdErr := os.Getwd()
	if cwdErr != nil {
		cwd = "error(" + boundedClaudeDiagnosticValue(cwdErr.Error(), 200) + ")"
	}
	realWorkspace, realErr := filepath.EvalSymlinks(workspace)
	if realErr != nil {
		realWorkspace = "error(" + boundedClaudeDiagnosticValue(realErr.Error(), 200) + ")"
	}
	readState := ""
	if readErr != nil {
		readState = "; state_error=" + boundedClaudeDiagnosticValue(readErr.Error(), 200)
	}
	return boundedClaudeDiagnosticValue(fmt.Sprintf(
		"Claude trust projects.keys=%s projects.entries=%s; os.Getwd=%s; workspace=%s; workspace realpath=%s%s",
		keyJSON, entryList, cwd, workspace, realWorkspace, readState), 1800)
}

func compactClaudeJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "<missing>"
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return boundedClaudeDiagnosticValue(strings.Join(strings.Fields(string(raw)), " "), 500)
	}
	data, err := json.Marshal(value)
	if err != nil {
		return "<unavailable>"
	}
	return boundedClaudeDiagnosticValue(string(data), 500)
}

func boundedClaudeEvidenceList(values []string, maxItems, maxItemLength int) string {
	shown := make([]string, 0, min(len(values), maxItems))
	for _, value := range values[:min(len(values), maxItems)] {
		shown = append(shown, boundedClaudeDiagnosticValue(value, maxItemLength))
	}
	if len(values) > maxItems {
		shown = append(shown, fmt.Sprintf("...+%d", len(values)-maxItems))
	}
	return "[" + strings.Join(shown, ",") + "]"
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
	return updateClaudeWorkspaceTrust(home, workspace)
}

func establishClaudeWorkspaceTrustAtRealPath(home, workspace string) error {
	realPath, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		return fmt.Errorf("resolve Claude workspace %q: %w", workspace, err)
	}
	return updateClaudeWorkspaceTrust(home, workspace, realPath)
}

func updateClaudeWorkspaceTrust(home string, workspaces ...string) error {
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
	for _, workspace := range workspaces {
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
	}
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
	for i := len(row.Commands) - 1; i >= 0; i-- {
		list := &row.Commands[i]
		if len(list.Args) < 4 || list.Args[1] != "plugin" || list.Args[2] != "list" {
			continue
		}
		if scope == "repo" {
			found, active, valid := claudePluginJSONState(list.Stdout, pluginID)
			recordClaudePluginVariantOutcome(list, "A-repo-settings-local", valid && found && active || skillListed, skillListed, list.Stdout)
		}
		recordClaudeScopeEvidence(list, e, scope)
		break
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
		command.Observed = append(command.Observed, "Claude initialization cwd: unavailable")
	} else {
		recordClaudePluginListField(command, "stream-json system init.plugins", fields[claudeSystemInitPluginsField])
		recordClaudePluginListField(command, "initialize control response.plugins", fields["plugins"])
		cwd := fields[claudeSystemInitCWDField]
		if len(cwd) == 0 {
			cwd = fields["cwd"]
		}
		if len(cwd) == 0 {
			command.Observed = append(command.Observed, "Claude initialization cwd: field absent")
		} else {
			var value string
			if err := json.Unmarshal(cwd, &value); err != nil {
				command.Observed = append(command.Observed, "Claude initialization cwd: field is not a string")
			} else {
				command.Observed = append(command.Observed,
					"Claude initialization cwd: "+boundedClaudeDiagnosticValue(value, 500))
			}
		}
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
