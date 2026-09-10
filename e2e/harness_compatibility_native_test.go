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
)

type compatibilityNative struct {
	binary        string
	probes        []compatibilityNativeProbe
	trustFixtures bool
}

type compatibilityNativeProbe struct {
	kind   string
	list   []string
	names  []string
	before compatibilityCommand
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
	native := &compatibilityNative{binary: binary, trustFixtures: os.Getenv("HARNESS_COMPAT_TRUST_FIXTURE") == "1"}
	switch row.Harness {
	case "devin":
		native.probes = append(native.probes,
			compatibilityNativeProbe{kind: "skill", list: []string{"skills", "list"}, names: []string{f.name("skill")}},
			compatibilityNativeProbe{kind: "command", list: []string{"skills", "list"}, names: []string{f.name("command")}},
		)
	case "gemini":
		native.probes = append(native.probes, compatibilityNativeProbe{kind: "skill", list: []string{"skills", "list"}, names: []string{f.name("skill")}})
	case "claude", "codex", "factory":
	case "cursor":
		row.Reason = "Cursor agent mcp list is interactive; no verified noninteractive credential-free inspection command is enabled"
		return nil
	case "windsurf":
		row.Reason = "no verified credential-free Windsurf CLI inspection surface"
		return nil
	}
	list := []string{"mcp", "list"}
	if row.Harness == "codex" {
		list = append(list, "--json")
	}
	native.probes = append(native.probes, compatibilityNativeProbe{kind: "mcp", list: list, names: []string{f.name("mcp-a"), f.name("mcp-b")}})
	for i := range native.probes {
		probe := &native.probes[i]
		probe.before = runCompatibilityCommand(e, binary, probe.list, 20*time.Second)
		if err := requireCompatibilityObservation(&probe.before, true, probe.names...); err != nil {
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
			command := runCompatibilityCommand(e, n.binary, probe.list, 20*time.Second)
			err := requireCompatibilityObservation(&command, false, probe.names...)
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
				if row.Harness == "devin" && probe.kind != "mcp" {
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
