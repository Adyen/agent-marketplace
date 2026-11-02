package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
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

// spec: plugin-delivery/claude-probe-loads-the-registered-plugin-at-both-scopes
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

func primeClaudePluginSession(e *Env, binary string, row *compatibilityResult) error {
	var headlessRegistered bool
	var headlessElapsed time.Duration
	headlessStarted := time.Now()
	var headlessPollErr error
	headless := runCompatibilityProtocol(e, binary, "claude-initialize", 60*time.Second, func(command *compatibilityCommand) error {
		headlessRegistered, headlessElapsed, headlessPollErr = waitForClaudeMarketplaceRegistration(e.home, 45*time.Second)
		command.Observed = append(command.Observed, fmt.Sprintf("registration mode=headless; registered=%t; elapsed=%s", headlessRegistered, headlessElapsed.Round(time.Millisecond)))
		return headlessPollErr
	})
	if headlessElapsed == 0 {
		headlessElapsed = time.Since(headlessStarted)
		headless.Observed = append(headless.Observed, fmt.Sprintf("registration mode=headless; registered=false; elapsed=%s", headlessElapsed.Round(time.Millisecond)))
	}
	row.Commands = append(row.Commands, headless)
	if headlessPollErr != nil {
		return fmt.Errorf("headless Claude marketplace polling failed: %w", headlessPollErr)
	}
	if headlessRegistered {
		return nil
	}

	interactive, interactiveRegistered, interactiveElapsed, err := primeClaudeInteractiveSession(e, binary, 60*time.Second)
	row.Commands = append(row.Commands, interactive)
	if err != nil {
		return fmt.Errorf("interactive Claude marketplace session failed: %w", err)
	}
	if interactiveRegistered {
		return nil
	}
	row.Commands[len(row.Commands)-1].Observed = append(
		row.Commands[len(row.Commands)-1].Observed,
		fmt.Sprintf("registration mode=none; headless elapsed=%s; interactive elapsed=%s",
			headlessElapsed.Round(time.Millisecond), interactiveElapsed.Round(time.Millisecond)),
	)
	return fmt.Errorf("Claude registered no Agent Marketplace catalog in headless (%s) or interactive (%s) session",
		headlessElapsed.Round(time.Millisecond), interactiveElapsed.Round(time.Millisecond))
}

func waitForClaudeMarketplaceRegistration(home string, timeout time.Duration) (bool, time.Duration, error) {
	started := time.Now()
	deadline := started.Add(timeout)
	for {
		registered, err := claudeMarketplaceRegistered(home)
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

func claudeMarketplaceRegistered(home string) (bool, error) {
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

func primeClaudeInteractiveSession(e *Env, binary string, timeout time.Duration) (compatibilityCommand, bool, time.Duration, error) {
	command := compatibilityCommand{Args: []string{binary}, ExitCode: -1}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	process, err := compatibilityProcess(ctx, e, binary, nil)
	if err != nil {
		command.Error = err.Error()
		return command, false, 0, err
	}
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
		registered, pollErr = claudeMarketplaceRegistered(e.home)
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
	if err != nil {
		return fmt.Errorf("read Claude native plugin registration %q: %w", settingsPath, err)
	}
	document, err := jsonc.Parse(settingsPath, data)
	if err != nil {
		return err
	}
	enabled, ok := document.Root().Member("enabledPlugins")
	if !ok {
		return fmt.Errorf("Claude plugin registration has no enabledPlugins")
	}
	pluginID := ""
	for _, key := range enabled.Keys() {
		value, _ := enabled.Member(key)
		if strings.HasPrefix(key, plugin+"@am-") && string(value.Raw()) == "true" {
			pluginID = key
			break
		}
	}
	if pluginID == "" {
		return fmt.Errorf("Claude plugin registration is not enabled for %s@am-*", plugin)
	}
	if err := waitForClaudePluginEnabled(e, binary, pluginID, row); err != nil {
		return err
	}
	command := runCompatibilityProtocol(e, binary, "claude-initialize", 30*time.Second)
	row.Commands = append(row.Commands, command)
	probe := compatibilityNativeProbe{kind: "skill", protocol: "claude-initialize", names: []string{plugin + ":" + skill}}
	if err := requireCompatibilityProtocolInventory(&command, e, f, probe, scope, nil, false); err != nil {
		return err
	}
	row.Status = "native-verified"
	row.Reason = "Claude plugin list and initialization inventory confirmed the native plugin and namespaced skill"
	return nil
}

func waitForClaudePluginEnabled(e *Env, binary, pluginID string, row *compatibilityResult) error {
	deadline := time.Now().Add(60 * time.Second)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return fmt.Errorf("Claude plugin list did not report %s enabled within 60 seconds", pluginID)
		}
		commands, found, active, err := claudePluginList(e, binary, pluginID, min(20*time.Second, remaining))
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
