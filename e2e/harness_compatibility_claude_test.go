package e2e_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/jsonc"
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

// spec: plugin-delivery/claude-probe-confirms-repository-scope-stays-decomposed
func TestClaudeRepositoryPluginAssertionRequiresDecomposedMembersAndNoRegistration(t *testing.T) {
	for _, tc := range []struct {
		name         string
		settings     string
		homeSettings string
		writeSkill   bool
		wantError    bool
	}{
		{
			name:         "unrelated registration and decomposed skill",
			settings:     `{"enabledPlugins":{"other@marketplace":true},"extraKnownMarketplaces":{"other":{}}}`,
			homeSettings: `{"enabledPlugins":{"other@manual":true},"extraKnownMarketplaces":{"manual":{}}}`,
			writeSkill:   true,
		},
		{
			name:       "marketplace registration",
			settings:   `{"extraKnownMarketplaces":{"am-catalog":{"source":{"type":"directory","path":"../catalog"}}}}`,
			writeSkill: true,
			wantError:  true,
		},
		{
			name:       "enabled plugin registration",
			settings:   `{"enabledPlugins":{"native-kit@am-catalog":true}}`,
			writeSkill: true,
			wantError:  true,
		},
		{
			name:         "home marketplace registration",
			homeSettings: `{"extraKnownMarketplaces":{"am-native-kit":{"source":{"type":"directory","path":"../catalog"}}}}`,
			writeSkill:   true,
			wantError:    true,
		},
		{
			name:         "home enabled plugin registration",
			homeSettings: `{"enabledPlugins":{"native-kit@am-catalog":true}}`,
			writeSkill:   true,
			wantError:    true,
		},
		{
			name:      "missing decomposed skill",
			settings:  `{}`,
			wantError: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			workspace := t.TempDir()
			settingsPath := filepath.Join(workspace, ".claude", "settings.local.json")
			if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(settingsPath, []byte(tc.settings), 0o600); err != nil {
				t.Fatal(err)
			}
			if tc.homeSettings != "" {
				homeSettingsPath := filepath.Join(home, ".claude", "settings.json")
				if err := os.MkdirAll(filepath.Dir(homeSettingsPath), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(homeSettingsPath, []byte(tc.homeSettings), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if tc.writeSkill {
				skillPath := filepath.Join(workspace, ".claude", "skills", "native-kit-review", "SKILL.md")
				if err := os.MkdirAll(filepath.Dir(skillPath), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(skillPath, []byte("---\nname: native-kit-review\n---\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			err := assertClaudeRepositoryPluginDecomposed(home, workspace, "native-kit", "review")
			if (err != nil) != tc.wantError {
				t.Fatalf("repository plugin assertion error = %v, want error %t", err, tc.wantError)
			}
		})
	}
}

func TestClaudeMarketplaceRegistrationRequiresInstalledPlugin(t *testing.T) {
	const plugin = "native-kit"
	for _, tc := range []struct {
		name              string
		marketplaceRecord string
		installedRecord   string
		wantRegistered    bool
	}{
		{name: "marketplace without plugin", marketplaceRecord: `{"am-test":{}}`},
		{
			name:              "matching plugin install record",
			marketplaceRecord: `{"am-test":{}}`,
			installedRecord:   `{"version":2,"plugins":{"native-kit@am-test":[{"scope":"user","installPath":"/home/test/.claude/plugins/cache/am-test/native-kit/1","version":"1.0.0","installedAt":"2026-11-02T00:00:00Z","lastUpdated":"2026-11-02T00:00:00Z"}]}}`,
			wantRegistered:    true,
		},
		{
			name:              "install record without marketplace registration",
			marketplaceRecord: `{}`,
			installedRecord:   `{"plugins":{"native-kit@am-test":[{"scope":"user"}]}}`,
		},
		{
			name:              "different plugin install record",
			marketplaceRecord: `{"am-test":{}}`,
			installedRecord:   `{"version":2,"plugins":{"other-kit@am-test":[{"scope":"user","installPath":"/home/test/.claude/plugins/cache/am-test/other-kit/1","version":"1.0.0","installedAt":"2026-11-02T00:00:00Z","lastUpdated":"2026-11-02T00:00:00Z"}]}}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			pluginsDir := filepath.Join(home, ".claude", "plugins")
			if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
				t.Fatal(err)
			}
			if tc.marketplaceRecord != "" {
				if err := os.WriteFile(filepath.Join(pluginsDir, "known_marketplaces.json"), []byte(tc.marketplaceRecord), 0o600); err != nil {
					t.Fatal(err)
				}
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
		})
	}
}

func TestClaudePluginPrimingUsesHeadlessInstallRecordOnce(t *testing.T) {
	e := newEnv(t)
	binary := filepath.Join(e.repos, "claude-headless-session")
	e.WriteFile(binary, `#!/bin/sh
mkdir -p "$HOME/.claude/plugins"
printf '%s\n' '{"am-test":{}}' > "$HOME/.claude/plugins/known_marketplaces.json"
printf '%s\n' '{"plugins":{"native-kit@am-test":[{"scope":"user"}]}}' > "$HOME/.claude/plugins/installed_plugins.json"
IFS= read -r request || exit 70
case "$request" in *'"subtype":"initialize"'*) ;; *) exit 71;; esac
printf '%s\n' '{"type":"control_response","response":{"subtype":"success","request_id":"amcompat-initialize","response":{"commands":[],"agents":[]}}}'
if IFS= read -r request; then exit 72; fi
`)
	if err := os.Chmod(binary, 0o755); err != nil {
		t.Fatal(err)
	}
	var row compatibilityResult
	if err := primeClaudePluginSession(e, binary, "native-kit", &row); err != nil {
		t.Fatalf("headless Claude registration was not observed: %v", err)
	}
	if len(row.Commands) != 1 {
		t.Fatalf("priming ran %d Claude commands, want exactly one headless session", len(row.Commands))
	}
	for _, arg := range row.Commands[0].Args {
		if arg == "--debug" {
			t.Fatalf("headless priming unexpectedly enabled debug output: %v", row.Commands[0].Args)
		}
	}
	if count := strings.Count(strings.Join(row.Commands[0].Observed, "\n"), "registration mode=headless; registered=true; elapsed="); count != 1 {
		t.Fatalf("headless registration mode and elapsed time recorded %d times: %+v", count, row.Commands[0].Observed)
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

func TestClaudePluginJSONIDsListsUniquePluginIDs(t *testing.T) {
	ids, valid := claudePluginJSONIDs(`{"plugins":[{"name":"other-kit@skills-dir"},{"name":"native-kit@am-catalog"},{"name":"native-kit@am-catalog"}]}`)
	if !valid || strings.Join(ids, ",") != "native-kit@am-catalog,other-kit@skills-dir" {
		t.Fatalf("plugin ids = %v, valid=%t", ids, valid)
	}
	if _, valid := claudePluginJSONIDs("not-json"); valid {
		t.Fatal("malformed plugin list accepted")
	}
}

func observeClaudePluginAbsent(e *Env, binary string, f compatibilityFixture, row *compatibilityResult, scope, plugin, skill string) error {
	if scope != "user" {
		return fmt.Errorf("Claude native plugin pre-sync control is user-scope only")
	}
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
	return requireCompatibilityProtocolInventory(&command, e, f, probe, scope, nil, true)
}

func assertClaudePluginStateAbsentAfterSync(home, workspace string) error {
	for _, path := range []string{
		filepath.Join(home, ".claude", "plugins", "installed_plugins.json"),
		filepath.Join(home, ".claude", "plugins", "known_marketplaces.json"),
		filepath.Join(home, ".claude", "plugins", "cache"),
	} {
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

func assertClaudeRepositoryPluginDecomposed(home, workspace, plugin, skill string) error {
	settingsPath := filepath.Join(workspace, ".claude", "settings.local.json")
	data, err := os.ReadFile(settingsPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read repository Claude settings %q: %w", settingsPath, err)
	}
	if err == nil {
		document, parseErr := jsonc.Parse(settingsPath, data)
		if parseErr != nil {
			return parseErr
		}
		for _, field := range []string{"extraKnownMarketplaces", "enabledPlugins"} {
			entries, ok := document.Root().Member(field)
			if !ok {
				continue
			}
			if entries.Kind() != jsonc.KindObject {
				return fmt.Errorf("repository Claude settings %q field %s is not an object", settingsPath, field)
			}
			for _, key := range entries.Keys() {
				if strings.Contains(key, "am-") {
					return fmt.Errorf("repository Claude settings %q contain Agent Marketplace %s entry %q", settingsPath, field, key)
				}
			}
		}
	}
	homeSettingsPath := filepath.Join(home, ".claude", "settings.json")
	homeSettings, err := os.ReadFile(homeSettingsPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read home Claude settings %q: %w", homeSettingsPath, err)
	}
	if err == nil {
		document, parseErr := jsonc.Parse(homeSettingsPath, homeSettings)
		if parseErr != nil {
			return parseErr
		}
		for _, field := range []string{"extraKnownMarketplaces", "enabledPlugins"} {
			entries, ok := document.Root().Member(field)
			if !ok {
				continue
			}
			if entries.Kind() != jsonc.KindObject {
				return fmt.Errorf("home Claude settings %q field %s is not an object", homeSettingsPath, field)
			}
			for _, key := range entries.Keys() {
				registered := field == "extraKnownMarketplaces" && strings.HasPrefix(key, "am-") ||
					field == "enabledPlugins" && strings.HasPrefix(key, plugin+"@am-")
				if registered {
					return fmt.Errorf("home Claude settings %q contain Agent Marketplace %s entry %q", homeSettingsPath, field, key)
				}
			}
		}
	}
	memberName := plugin + "-" + skill
	memberPath := filepath.Join(workspace, ".claude", "skills", memberName, "SKILL.md")
	if _, err := os.Stat(memberPath); err != nil {
		return fmt.Errorf("repository Claude decomposed skill %q is absent at %q: %w", memberName, memberPath, err)
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

func primeClaudePluginSession(e *Env, binary, pluginName string, row *compatibilityResult) error {
	started := time.Now()
	var registered bool
	var elapsed time.Duration
	var pollErr error
	command := runCompatibilityProtocol(e, binary, "claude-initialize", 60*time.Second, func(*compatibilityCommand) error {
		registered, elapsed, pollErr = waitForClaudeMarketplaceRegistration(e.home, pluginName, 45*time.Second)
		return pollErr
	})
	if elapsed == 0 {
		elapsed = time.Since(started)
	}
	command.Observed = append(command.Observed,
		fmt.Sprintf("registration mode=headless; registered=%t; elapsed=%s", registered, elapsed.Round(time.Millisecond)))
	row.Commands = append(row.Commands, command)
	if pollErr != nil {
		return fmt.Errorf("wait for Claude's marketplace and install records: %w", pollErr)
	}
	if err := compatibilityInspectionError(command); err != nil {
		return err
	}
	if !registered {
		return fmt.Errorf("Claude did not write its marketplace and install records")
	}
	return nil
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
	installed, err := claudeInstalledPluginRecords(home)
	if err != nil {
		return false, err
	}
	for _, name := range installed {
		if strings.HasPrefix(name, pluginName+"@am-") {
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
	var state map[string]json.RawMessage
	if err := json.Unmarshal(data, &state); err != nil {
		return false, fmt.Errorf("parse Claude marketplace state %q: %w", path, err)
	}
	for name := range state {
		if strings.HasPrefix(name, "am-") {
			return true, nil
		}
	}
	return false, nil
}

func claudeInstalledPluginRecords(home string) ([]string, error) {
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
	records := make([]string, 0, len(state.Plugins))
	for name := range state.Plugins {
		records = append(records, name)
	}
	sort.Strings(records)
	return records, nil
}

func observeClaudePluginEnabled(e *Env, binary string, f compatibilityFixture, row *compatibilityResult, scope, plugin, skill string) error {
	if scope != "user" {
		return fmt.Errorf("Claude native plugin verdict is user-scope only")
	}
	command := runCompatibilityProtocol(e, binary, "claude-initialize", 30*time.Second)
	skillListed := recordClaudePluginInitialization(&command, e, plugin, skill)
	protocolErr := requireCompatibilityProtocolInventory(
		&command,
		e,
		f,
		compatibilityNativeProbe{kind: "skill", protocol: "claude-initialize", names: []string{plugin + ":" + skill}},
		scope,
		nil,
		false,
	)
	row.Commands = append(row.Commands, command)
	listCommands, found, enabled, listErr := claudePluginList(e, binary, plugin+"@am-", 20*time.Second)
	for i := range listCommands {
		listCommands[i].Observed = append(listCommands[i].Observed, "plugin list --json stdout: "+listCommands[i].Stdout)
	}
	row.Commands = append(row.Commands, listCommands...)
	if protocolErr != nil {
		return fmt.Errorf("Claude initialization did not list the namespaced skill: %w", protocolErr)
	}
	if !skillListed {
		return fmt.Errorf("Claude initialization omitted the namespaced skill %s:%s", plugin, skill)
	}
	if listErr != nil {
		return listErr
	}
	if !found || !enabled {
		return fmt.Errorf("Claude plugin list did not report %s@am-* enabled", plugin)
	}
	row.Status = "native-verified"
	row.Reason = "Claude installed the user-scope plugin, listed it enabled, and initialized its namespaced skill"
	return nil
}

func recordClaudePluginInitialization(command *compatibilityCommand, e *Env, plugin, skill string) bool {
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
			for _, key := range []string{"name", "id", "plugin"} {
				identity, ok := item[key].(string)
				if !ok || identity == "" || identity != match && !strings.HasPrefix(identity, match) {
					continue
				}
				present = true
				if item["enabled"] == true || strings.EqualFold(fmt.Sprint(item["status"]), "enabled") {
					enabled = true
				}
			}
			for key, child := range item {
				if key == match || strings.HasPrefix(key, match) {
					present = true
					enabled = enabled || child == true
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
