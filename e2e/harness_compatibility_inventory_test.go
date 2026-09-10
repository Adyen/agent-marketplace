package e2e_test

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

type compatibilityNativeEntry struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Path        string `json:"path"`
	Scope       string `json:"scope"`
	Enabled     bool   `json:"enabled"`
	Command     string `json:"command"`
	Event       string `json:"eventName"`
	Handler     string `json:"handlerType"`
	Matcher     string `json:"matcher"`
	SourcePath  string `json:"sourcePath"`
}

func compatibilityProtocolInventory(raw, protocol, kind, workspace string) ([]compatibilityNativeEntry, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &fields); err != nil || fields == nil {
		return nil, fmt.Errorf("native metadata is not a JSON object")
	}
	key := "commands"
	if kind == "subagent" {
		key = "agents"
	}
	if strings.HasPrefix(protocol, "codex-") {
		var data []struct {
			Cwd    string            `json:"cwd"`
			Skills json.RawMessage   `json:"skills"`
			Hooks  json.RawMessage   `json:"hooks"`
			Errors []json.RawMessage `json:"errors"`
		}
		if err := json.Unmarshal(fields["data"], &data); err != nil || len(data) != 1 || data[0].Cwd != workspace || data[0].Errors == nil {
			return nil, fmt.Errorf("native metadata did not identify the requested workspace and error inventory")
		}
		if len(data[0].Errors) != 0 {
			return nil, fmt.Errorf("native metadata reports resource loading errors")
		}
		fields = map[string]json.RawMessage{"skills": data[0].Skills, "hooks": data[0].Hooks}
		key = "skills"
		if kind == "hook" {
			key = "hooks"
		}
	}
	var entries []compatibilityNativeEntry
	if err := json.Unmarshal(fields[key], &entries); err != nil || entries == nil {
		return nil, fmt.Errorf("native metadata omitted the resource inventory")
	}
	return entries, nil
}

func requireCompatibilityProtocolInventory(command *compatibilityCommand, e *Env, f compatibilityFixture, probe compatibilityNativeProbe, scope string, paths []string, absent bool) error {
	if err := compatibilityInspectionError(*command); err != nil {
		return err
	}
	entries, err := compatibilityProtocolInventory(command.output, probe.protocol, probe.kind, e.workspace)
	if err != nil {
		return err
	}
	for i, name := range probe.names {
		var matches []compatibilityNativeEntry
		for _, entry := range entries {
			if probe.kind == "hook" && entry.Command == f.hookCommand(fmt.Sprintf("hook-%c", 'a'+i)) || probe.kind != "hook" && entry.Name == name {
				matches = append(matches, entry)
			}
		}
		if absent {
			if len(matches) != 0 {
				return fmt.Errorf("native negative control already discovered the fixture resource")
			}
			command.Observed = append(command.Observed, "absent: "+name)
			continue
		}
		if len(matches) != 1 {
			return fmt.Errorf("native inventory must discover the fixture resource exactly once")
		}
		entry := matches[0]
		if probe.kind == "hook" {
			event, matcher := "preToolUse", "Bash"
			if i == 1 {
				event, matcher = "userPromptSubmit", ""
			}
			if entry.Event != event || entry.Handler != "command" || entry.Matcher != matcher || len(paths) != 1 || filepath.Clean(entry.SourcePath) != filepath.Clean(paths[0]) {
				return fmt.Errorf("native hook event, handler, matcher, or source differs from the fixture")
			}
			command.Observed = append(command.Observed, entry.Command, "event: "+event, "source: "+entry.SourcePath, "hook discovery only; execution not requested")
			continue
		}
		if entry.Description != f.description(probe.kind) {
			return fmt.Errorf("native resource description differs from the fixture")
		}
		if probe.protocol == "codex-skills" {
			if !entry.Enabled || entry.Scope != scope || len(paths) != 1 || filepath.Clean(entry.Path) != filepath.Clean(paths[0]) {
				return fmt.Errorf("native skill source, scope, or enablement differs from the fixture")
			}
			command.Observed = append(command.Observed, "source: "+entry.Path, "scope: "+entry.Scope, "enabled: true")
		}
		command.Observed = append(command.Observed, name, entry.Description)
	}
	return nil
}

func TestCompatibilityProtocolInventoryRejectsNamesOutsideDiscoveredResources(t *testing.T) {
	for _, raw := range []string{
		`{"data":[{"cwd":"workspace","skills":[],"errors":[{"path":"fixture-skill","message":"invalid"}]}]}`,
		`{"data":[{"cwd":"different-workspace","skills":[],"errors":[]}]}`,
		`{"data":[{"cwd":"workspace","errors":[]}]}`,
		`{"data":[{"cwd":"workspace","skills":null,"errors":[]}]}`,
	} {
		if _, err := compatibilityProtocolInventory(raw, "codex-skills", "skill", "workspace"); err == nil {
			t.Fatal("invalid inventory accepted")
		}
	}
	entries, err := compatibilityProtocolInventory(`{"data":[{"cwd":"workspace","skills":[{"name":"fixture-skill","description":"Fixture","enabled":true,"scope":"repo","path":"fixture"}],"errors":[]}]}`, "codex-skills", "skill", "workspace")
	if err != nil || len(entries) != 1 || entries[0].Name != "fixture-skill" {
		t.Fatal("valid discovered resource rejected")
	}
}
