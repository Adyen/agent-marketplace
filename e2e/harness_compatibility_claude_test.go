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
	recordClaudePluginInitialization(&command, e, plugin, skill)
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

func recordClaudePluginInitialization(command *compatibilityCommand, e *Env, plugin, skill string) {
	var fields map[string]json.RawMessage
	if json.Unmarshal([]byte(command.output), &fields) != nil || fields == nil {
		command.Observed = append(command.Observed, "Claude initialization plugins: response metadata unavailable")
	} else {
		recordClaudePluginListField(command, "stream-json system init.plugins", fields[claudeSystemInitPluginsField])
		recordClaudePluginListField(command, "initialize control response.plugins", fields["plugins"])
	}
	listed := false
	entries, err := compatibilityProtocolInventory(command.output, "claude-initialize", "skill", e.workspace)
	if err == nil {
		for _, entry := range entries {
			listed = listed || entry.Name == plugin+":"+skill
		}
	}
	command.Observed = append(command.Observed, fmt.Sprintf("Claude namespaced skill %s:%s listed=%t", plugin, skill, listed))
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
