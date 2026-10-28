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
	Content     string `json:"content"`
	Path        string `json:"path"`
	Scope       string `json:"scope"`
	Enabled     bool   `json:"enabled"`
	Command     string `json:"command"`
	Event       string `json:"eventName"`
	Handler     string `json:"handlerType"`
	Matcher     string `json:"matcher"`
	SourcePath  string `json:"sourcePath"`
	FilePath    string `json:"filePath"`
	Location    string `json:"location"`
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
	if protocol == "factory-skills" {
		key = "skills"
	}
	if protocol == "factory-droids" {
		key = "droids"
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
			event, matcher := "preToolUse", "^Bash$"
			if i == 1 {
				event, matcher = "userPromptSubmit", ""
			}
			if entry.Event != event {
				return fmt.Errorf("native hook event differs from fixture")
			}
			if entry.Handler != "command" {
				return fmt.Errorf("native hook handler differs from fixture")
			}
			if entry.Matcher != matcher {
				return fmt.Errorf("native hook matcher differs from fixture")
			}
			if len(paths) != 1 || filepath.Clean(entry.SourcePath) != filepath.Clean(paths[0]) {
				return fmt.Errorf("native hook source differs from fixture")
			}
			command.Observed = append(command.Observed, entry.Command, "event: "+event, "source: "+entry.SourcePath, "hook discovery only; execution not requested")
			continue
		}
		if probe.protocol == "factory-skills" || probe.protocol == "factory-droids" {
			location := "project"
			if scope == "user" {
				location = "personal"
			}
			if entry.Location != location {
				return fmt.Errorf("native Factory resource location differs from the fixture")
			}
			command.Observed = append(command.Observed, name, "location: "+location)
			if probe.kind == "subagent" {
				continue
			}
			if !entry.Enabled || len(paths) != 1 || !compatibilitySameSource(entry.FilePath, paths[0]) {
				return fmt.Errorf("native Factory skill source or enablement differs from the fixture")
			}
			command.Observed = append(command.Observed, "source: "+entry.FilePath, "enabled: true")
		}
		expectedDescription := f.description(probe.kind)
		descriptionMatches := entry.Description == expectedDescription
		if probe.protocol == "claude-initialize" && probe.kind != "subagent" {
			descriptionMatches = strings.Contains(entry.Description, expectedDescription)
		}
		if !descriptionMatches {
			return fmt.Errorf("native resource description differs from the fixture")
		}
		if probe.protocol == "codex-skills" {
			if !entry.Enabled || entry.Scope != scope || len(paths) != 1 || !compatibilitySameSource(entry.Path, paths[0]) {
				return fmt.Errorf("native skill source, scope, or enablement differs from the fixture")
			}
			command.Observed = append(command.Observed, "source: "+entry.Path, "scope: "+entry.Scope, "enabled: true")
		}
		command.Observed = append(command.Observed, name, expectedDescription)
	}
	return nil
}

func compatibilitySameSource(actual, expected string) bool {
	if actual == "" || expected == "" {
		return false
	}
	if filepath.Clean(actual) == filepath.Clean(expected) {
		return true
	}
	resolved, err := filepath.EvalSymlinks(expected)
	return err == nil && filepath.Clean(actual) == resolved
}

func TestCompatibilityCodexHookInventoryRequiresExactMatcherAndFields(t *testing.T) {
	e := newEnv(t)
	f := compatibilityFixture{prefix: "amcompat-codex", hookScript: filepath.Join(e.repos, "hook.sh")}
	probe := compatibilityNativeProbe{kind: "hook", protocol: "codex-hooks", names: []string{f.name("hook-a"), f.name("hook-b")}}
	source := filepath.Join(e.workspace, ".codex", "hooks.json")
	want := []compatibilityNativeEntry{
		{Name: "PreToolUse-command", Command: f.hookCommand("hook-a"), Event: "preToolUse", Handler: "command", Matcher: "^Bash$", SourcePath: source},
		{Name: "UserPromptSubmit-command", Command: f.hookCommand("hook-b"), Event: "userPromptSubmit", Handler: "command", SourcePath: source},
	}
	output := func(entries []compatibilityNativeEntry) string {
		t.Helper()
		payload := map[string]any{"data": []any{map[string]any{
			"cwd": e.workspace, "skills": []any{}, "hooks": entries, "errors": []any{},
		}}}
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	inspect := func(entries []compatibilityNativeEntry, absent bool) error {
		command := compatibilityCommand{output: output(entries)}
		return requireCompatibilityProtocolInventory(&command, e, f, probe, "repo", []string{source}, absent)
	}
	if err := inspect(want, false); err != nil {
		t.Errorf("exact two-hook Codex inventory rejected: %v", err)
	}
	for _, tc := range []struct {
		name      string
		mutate    func([]compatibilityNativeEntry) []compatibilityNativeEntry
		wantError string
	}{
		{"unanchored Bash matcher", func(entries []compatibilityNativeEntry) []compatibilityNativeEntry {
			entries[0].Matcher = "Bash"
			return entries
		}, "native hook matcher differs from fixture"},
		{"wrong event", func(entries []compatibilityNativeEntry) []compatibilityNativeEntry {
			entries[1].Event = "preToolUse"
			return entries
		}, "native hook event differs from fixture"},
		{"wrong handler", func(entries []compatibilityNativeEntry) []compatibilityNativeEntry {
			entries[1].Handler = "prompt"
			return entries
		}, "native hook handler differs from fixture"},
		{"wrong source", func(entries []compatibilityNativeEntry) []compatibilityNativeEntry {
			entries[1].SourcePath = filepath.Join(e.workspace, ".codex", "other.json")
			return entries
		}, "native hook source differs from fixture"},
		{"missing hook", func(entries []compatibilityNativeEntry) []compatibilityNativeEntry { return entries[:1] }, "native inventory must discover the fixture resource exactly once"},
		{"duplicate hook", func(entries []compatibilityNativeEntry) []compatibilityNativeEntry {
			return append(entries, entries[0])
		}, "native inventory must discover the fixture resource exactly once"},
		{"wrong command", func(entries []compatibilityNativeEntry) []compatibilityNativeEntry {
			entries[0].Command = "inert wrong fixture command"
			return entries
		}, "native inventory must discover the fixture resource exactly once"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entries := append([]compatibilityNativeEntry(nil), want...)
			err := inspect(tc.mutate(entries), false)
			if err == nil || err.Error() != tc.wantError {
				t.Fatalf("Codex discriminator error = %v, want %q", err, tc.wantError)
			}
		})
	}
	if err := inspect([]compatibilityNativeEntry{}, true); err != nil {
		t.Fatalf("initial absent Codex hook inventory rejected: %v", err)
	}
}

func TestCompatibilityClaudeDescriptionRequiresFixtureContent(t *testing.T) {
	e := newEnv(t)
	f := compatibilityFixture{prefix: "amcompat-claude"}
	probe := compatibilityNativeProbe{kind: "skill", protocol: "claude-initialize", names: []string{f.name("skill")}}
	for _, test := range []struct {
		description string
		accepted    bool
	}{{f.description("skill") + " (project)", true}, {"unrelated description", false}} {
		raw, err := json.Marshal(map[string]any{"commands": []map[string]string{{"name": f.name("skill"), "description": test.description}}})
		if err != nil {
			t.Fatal(err)
		}
		command := compatibilityCommand{output: string(raw)}
		err = requireCompatibilityProtocolInventory(&command, e, f, probe, "repo", nil, false)
		if (err == nil) != test.accepted {
			t.Fatalf("CLI presentation must retain the fixture description: %v", err)
		}
	}
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
