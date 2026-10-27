package e2e_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/jsonc"
)

type compatibilityNative struct {
	binary        string
	probes        []compatibilityNativeProbe
	trustFixtures bool
}

type compatibilityNativeProbe struct {
	kind     string
	list     []string
	names    []string
	protocol string
	before   compatibilityCommand
}

type compatibilityInventoryRunner struct {
	cache map[string]compatibilityCommand
}

func newCompatibilityInventoryRunner() *compatibilityInventoryRunner {
	return &compatibilityInventoryRunner{cache: make(map[string]compatibilityCommand)}
}

func (r *compatibilityInventoryRunner) key(p compatibilityNativeProbe) string {
	if p.protocol != "" {
		return p.protocol + "\x00"
	}
	return strings.Join(p.list, "\x00")
}

func (r *compatibilityInventoryRunner) run(e *Env, p compatibilityNativeProbe, binary string) compatibilityCommand {
	k := r.key(p)
	if cached, ok := r.cache[k]; ok {
		return cloneCompatibilityCommand(cached)
	}
	cmd := p.run(e, binary)
	r.cache[k] = cmd
	return cloneCompatibilityCommand(cmd)
}

func cloneCompatibilityCommand(c compatibilityCommand) compatibilityCommand {
	cloned := c
	if len(c.Args) > 0 {
		cloned.Args = make([]string, len(c.Args))
		copy(cloned.Args, c.Args)
	}
	if len(c.Observed) > 0 {
		cloned.Observed = make([]string, len(c.Observed))
		copy(cloned.Observed, c.Observed)
	}
	if len(c.Diagnostics) > 0 {
		cloned.Diagnostics = make([]string, len(c.Diagnostics))
		copy(cloned.Diagnostics, c.Diagnostics)
	}
	return cloned
}

func (p compatibilityNativeProbe) run(e *Env, binary string) compatibilityCommand {
	if p.protocol != "" {
		return runCompatibilityProtocol(e, binary, p.protocol, 30*time.Second)
	}
	return runCompatibilityCommand(e, binary, p.list, 20*time.Second)
}

func (p compatibilityNativeProbe) observe(command *compatibilityCommand, e *Env, f compatibilityFixture, scope string, paths []string, absent bool) error {
	if p.protocol != "" {
		return requireCompatibilityProtocolInventory(command, e, f, p, scope, paths, absent)
	}
	return requireCompatibilityObservation(command, absent, p.names...)
}

func observeClaudePluginAbsent(e *Env, binary string, f compatibilityFixture, row *compatibilityResult, scope, plugin, skill string) error {
	commands, found, _, err := claudePluginList(e, binary, plugin+"@")
	row.Commands = append(row.Commands, commands...)
	if err != nil {
		return err
	}
	if found {
		return fmt.Errorf("Claude plugin negative control already listed %s", plugin)
	}
	command := runCompatibilityProtocol(e, binary, "claude-initialize", 30*time.Second)
	probe := compatibilityNativeProbe{kind: "skill", protocol: "claude-initialize", names: []string{plugin + ":" + skill}}
	if err := requireCompatibilityProtocolInventory(&command, e, f, probe, scope, nil, true); err != nil {
		return err
	}
	row.Commands = append(row.Commands, command)
	return nil
}

func observeClaudePluginEnabled(e *Env, binary string, f compatibilityFixture, row *compatibilityResult, scope, plugin, skill string) error {
	relative := "/workspace/.claude/settings.local.json"
	if scope == "user" {
		relative = "/root/.claude/settings.json"
	}
	settingsPath, err := e.resolvePath(relative)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		return fmt.Errorf("read Claude native plugin registration: %w", err)
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
		if strings.HasPrefix(key, plugin+"@") && string(value.Raw()) == "true" {
			pluginID = key
			break
		}
	}
	if pluginID == "" {
		return fmt.Errorf("Claude plugin registration is not enabled for %s", plugin)
	}
	commands, found, active, err := claudePluginList(e, binary, pluginID)
	row.Commands = append(row.Commands, commands...)
	if err != nil {
		return err
	}
	if !found || !active {
		return fmt.Errorf("claude plugin list did not report %s enabled", pluginID)
	}
	command := runCompatibilityProtocol(e, binary, "claude-initialize", 30*time.Second)
	probe := compatibilityNativeProbe{kind: "skill", protocol: "claude-initialize", names: []string{plugin + ":" + skill}}
	if err := requireCompatibilityProtocolInventory(&command, e, f, probe, scope, nil, false); err != nil {
		return err
	}
	row.Commands = append(row.Commands, command)
	row.Status = "native-verified"
	row.Reason = "Claude plugin list and initialize inventory confirmed the native plugin and namespaced skill"
	return nil
}

func claudePluginList(e *Env, binary, match string) ([]compatibilityCommand, bool, bool, error) {
	jsonCommand := runCompatibilityCommand(e, binary, []string{"plugin", "list", "--json"}, 20*time.Second)
	if jsonCommand.Error == "" {
		if present, enabled, valid := claudePluginJSONState(jsonCommand.output, match); valid {
			return []compatibilityCommand{jsonCommand}, present, enabled, nil
		}
	}
	textCommand := runCompatibilityCommand(e, binary, []string{"plugin", "list"}, 20*time.Second)
	if textCommand.Error != "" {
		return []compatibilityCommand{jsonCommand, textCommand}, false, false, fmt.Errorf("Claude plugin list failed: %s", textCommand.Error)
	}
	present, enabled := false, false
	for _, line := range strings.Split(textCommand.output, "\n") {
		if !strings.Contains(line, match) {
			continue
		}
		present = true
		lower := strings.ToLower(line)
		enabled = enabled || strings.Contains(lower, "enabled") && !strings.Contains(lower, "disabled")
	}
	return []compatibilityCommand{jsonCommand, textCommand}, present, enabled, nil
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

func prepareCompatibilityNative(e *Env, f compatibilityFixture, row *compatibilityResult, mode, reason, expectedVersion string) *compatibilityNative {
	row.Reason = reason
	if mode == "unavailable" {
		row.Status = "config-only-verified"
		row.Reason = "native inspection unavailable: " + reason + "; this row records a capability gap, not native verification"
		return nil
	}
	row.Status = "blocked"
	if mode == "blocked" {
		return nil
	}
	if err := configureCompatibilityRuntime(e, row.Harness); err != nil {
		row.Reason = "cannot disable native runtime auto-update in isolated configuration"
		return nil
	}
	binary := os.Getenv("HARNESS_COMPAT_NATIVE_BINARY")
	version := runCompatibilityCommand(e, binary, []string{"--version"}, 20*time.Second)
	row.Commands = append(row.Commands, version)
	if version.Error != "" {
		row.Reason = version.Error
		return nil
	}
	row.Version = regexp.MustCompile(`\b[0-9]+\.[0-9]+\.[0-9]+(?:[-+][a-zA-Z0-9.-]+)?\b`).FindString(version.output)
	if row.Version == "" || expectedVersion != "" && row.Version != strings.TrimPrefix(expectedVersion, "v") {
		row.Reason = "version output did not identify the expected pinned version"
		return nil
	}
	native := &compatibilityNative{binary: binary, probes: compatibilityResourceProbes(row.Harness, f), trustFixtures: os.Getenv("HARNESS_COMPAT_TRUST_FIXTURE") == "1"}
	if len(native.probes) == 0 {
		row.Reason = "no native inspection probes are declared for this harness"
		return nil
	}
	runner := newCompatibilityInventoryRunner()
	for i := range native.probes {
		probe := &native.probes[i]
		probe.before = runner.run(e, *probe, binary)
		if err := probe.observe(&probe.before, e, f, row.Scope, nil, true); err != nil {
			probe.before.Error = "native negative control failed: " + err.Error()
		}
		row.Commands = append(row.Commands, probe.before)
	}
	row.Status = "native-verified"
	row.Reason = "pinned executable version only; discovery controls and resource acceptance are reported independently per resource"
	return native
}

func (n *compatibilityNative) verify(t *testing.T, e *Env, f compatibilityFixture, rows map[string]*compatibilityResult, paths map[string][]string) {
	if row, err := n.prepareFixtureTrust(e, f, rows); err != nil {
		row.Status, row.Reason = "failed", err.Error()
		t.Errorf("native trust boundary: %v", err)
	}
	runner := newCompatibilityInventoryRunner()
	for _, probe := range n.probes {
		row := rows[probe.kind]
		if row.Status != "config-only-verified" {
			continue
		}
		row.Commands = append(row.Commands, probe.before)
		if probe.before.Error != "" {
			row.Status = "blocked"
			row.Reason = probe.before.Error
			t.Errorf("native %s inspection blocked: %s", probe.kind, probe.before.Error)
			continue
		}
		ok := t.Run("native-"+probe.kind, func(t *testing.T) {
			row.Status = "failed"
			row.Reason = "native discovery or observable value assertion failed after config verification"
			command := runner.run(e, probe, n.binary)
			err := probe.observe(&command, e, f, row.Scope, paths[probe.kind], false)
			if err != nil && compatibilityTrustBlocksDiscovery(*row, command) {
				row.Status = "blocked"
				row.Reason = "Gemini reported untrusted workspace; repository skill discovery remains gated by native trust"
				if row.Trust != "" {
					row.Status = "failed"
					row.Reason = "native workspace remained untrusted after fixture-only trust setup"
				}
				row.Commands = append(row.Commands, command)
				t.Fatal(row.Reason)
			}
			if err == nil && probe.kind == "mcp" {
				row.Connection = compatibilityMCPConnection(&command, row.Harness)
			}
			if err == nil && row.Harness == "gemini" && probe.kind == "skill" {
				err = requireCompatibilityObservation(&command, false, f.description("skill"))
				if err == nil && regexp.MustCompile(`(?im)^\s*(?:location|path):`).MatchString(command.output) {
					err = requireCompatibilitySource(&command, paths[probe.kind][0])
				}
			}
			row.Commands = append(row.Commands, command)
			if err != nil {
				row.Reason = err.Error()
				t.Fatal(err)
			}
			for _, name := range probe.names {
				var args, values []string
				if row.Harness == "cursor" && probe.kind == "mcp" {
					args = []string{"mcp", "list-tools", name}
					values = []string{"amcompat_echo"}
				} else if row.Harness == "devin" && probe.kind != "mcp" {
					args = []string{"skills", "show", name}
					values = []string{name, f.description(probe.kind)}
				} else if probe.kind == "mcp" && (row.Harness == "devin" || row.Harness == "codex" || row.Harness == "claude") {
					args = []string{"mcp", "get", name}
					values = []string{name}
					if row.Harness != "claude" {
						values = append(values, f.server, "--label="+name, "AMCOMPAT_SETTING")
					}
					if row.Harness == "codex" {
						args = append(args, "--json")
					}
				}
				if len(args) == 0 {
					continue
				}
				detail := runCompatibilityCommand(e, n.binary, args, 20*time.Second)
				err := requireCompatibilityObservation(&detail, false, values...)
				if err == nil && probe.kind != "mcp" {
					err = requireCompatibilitySource(&detail, paths[probe.kind][0])
				}
				row.Commands = append(row.Commands, detail)
				if err != nil {
					row.Reason = err.Error()
					t.Fatal(err)
				}
			}
		})
		if ok {
			if row.Harness == "cursor" && probe.kind == "mcp" {
				row.Connection = &compatibilityConnection{Status: "native-verified", Reason: "native list-tools returned the local fixture tool for both servers; no tool was invoked"}
			}
			row.Status = "native-verified"
			row.Reason = "negative control plus native list confirmed fixture names; only observations recorded per command are verified, not unexposed fields, hook execution, or model behavior"
		}
	}
}

func compatibilityTrustBlocksDiscovery(row compatibilityResult, command compatibilityCommand) bool {
	return row.Harness == "gemini" && row.Resource == "skill" && row.Scope == "repo" && compatibilityInspectionError(command) == nil && slices.Contains(command.Diagnostics, "cli.project-trust-required")
}

func configureCompatibilityRuntime(e *Env, target string) error {
	if target != "devin" {
		return nil
	}
	root, err := os.OpenRoot(e.home)
	if err != nil {
		return err
	}
	defer root.Close()
	const path = ".config/devin/config.json"
	document := make(map[string]any)
	data, err := root.ReadFile(path)
	if err == nil {
		if json.Unmarshal(data, &document) != nil || document == nil {
			return fmt.Errorf("invalid isolated Devin config")
		}
		if enabled, ok := document["auto_update"].(bool); ok && !enabled {
			return nil
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	document["auto_update"] = false
	data, err = json.MarshalIndent(document, "", "  ")
	if err != nil {
		return err
	}
	if err := root.MkdirAll(".config/devin", 0o700); err != nil {
		return err
	}
	return root.WriteFile(path, append(data, '\n'), 0o600)
}

func requireCompatibilitySource(command *compatibilityCommand, path string) error {
	for _, candidate := range []string{path, filepath.Dir(path)} {
		if strings.Contains(command.output, candidate) {
			command.Observed = append(command.Observed, "source: "+candidate)
			return nil
		}
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil && (strings.Contains(command.output, resolved) || strings.Contains(command.output, filepath.Dir(resolved))) {
		command.Observed = append(command.Observed, "resolved source: "+resolved)
		return nil
	}
	return fmt.Errorf("native skill detail did not expose its installed or symlink-resolved source path")
}

func compatibilityMCPConnection(command *compatibilityCommand, target string) *compatibilityConnection {
	if target != "claude" && target != "gemini" {
		if err := assertCompatibilityConnection(command, target); err != nil {
			return &compatibilityConnection{Status: "failed", Reason: err.Error() + "; configuration recognition is separate"}
		}
		return &compatibilityConnection{Status: "not-probed", Reason: "native list/get used for configuration recognition only; transport establishment was not inspected"}
	}
	if err := assertCompatibilityConnection(command, target); err != nil {
		if regexp.MustCompile(`(?i)trust|pending|disabled|approval|not enabled`).MatchString(command.output) {
			return &compatibilityConnection{Status: "blocked", Reason: "connection not established; trust, approval, disabled, or pending state reported; configuration recognition is separate"}
		}
		return &compatibilityConnection{Status: "failed", Reason: err.Error() + "; configuration recognition is separate"}
	}
	return &compatibilityConnection{Status: "native-verified", Reason: "native listing reported both local fixture server connections"}
}

func assertCompatibilityConnection(command *compatibilityCommand, target string) error {
	lower := strings.ToLower(command.output)
	for _, failure := range []string{"failed", "disconnected", "needs authentication", "login required", "not logged in"} {
		if strings.Contains(lower, failure) {
			return fmt.Errorf("native MCP inspection reported %s", failure)
		}
	}
	if target == "claude" || target == "gemini" {
		if len(regexp.MustCompile(`(?i)\bconnected\b`).FindAllString(command.output, -1)) < 2 {
			return fmt.Errorf("native MCP listing did not confirm both local fixture server connections")
		}
		command.Observed = append(command.Observed, "two local stdio fixture connections reported")
	}
	return nil
}
