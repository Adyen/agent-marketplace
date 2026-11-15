package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/devincli"
	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

type doctorNativeRepairCase struct {
	target  hcc.Target
	machine bool
}

var doctorNativeRepairCases = []doctorNativeRepairCase{
	{target: hcc.TargetClaude, machine: true},
	{target: hcc.TargetFactory},
	{target: hcc.TargetCursor, machine: true},
	{target: hcc.TargetWindsurf},
	{target: hcc.TargetGemini, machine: true},
	{target: hcc.TargetOpenCode},
}

func doctorNativeRepairFile(t *testing.T, repoRoot string, target hcc.Target, machine bool) (string, string, string) {
	t.Helper()
	if target == hcc.TargetGemini {
		hookFile, ok := targetmeta.NativeAggregateFileForScope(targetmeta.Target(target), ir.KindHook, machine)
		if !ok {
			t.Fatal("Gemini has no native hook aggregate")
		}
		destinations, err := harness.ResolveDestinations(target, machine, repoRoot)
		if err != nil {
			t.Fatalf("resolve Gemini destinations: %v", err)
		}
		for _, destination := range destinations {
			if len(destination.AggregateRoutes) > 0 && destination.AggregateFile == hookFile.Path {
				return destination.Root, destination.AggregateFile, "mcpServers"
			}
		}
		t.Fatal("Gemini has no settings aggregate destination")
	}
	declaration, ok := targetmeta.NativeAggregateFileForScope(targetmeta.Target(target), ir.KindMcpServer, machine)
	if !ok {
		t.Fatalf("%s has no MCP aggregate declaration", target)
	}
	destinations, err := harness.ResolveDestinations(target, machine, repoRoot)
	if err != nil {
		t.Fatalf("resolve %s destinations: %v", target, err)
	}
	for _, destination := range destinations {
		if len(destination.AggregateRoutes) > 0 {
			for _, route := range destination.AggregateRoutes {
				if route.Container == declaration.Member && filepath.Base(route.ArtifactPath) == filepath.Base(declaration.Path) {
					return destination.Root, destination.AggregateFile, route.Container
				}
			}
		}
	}
	for _, destination := range destinations {
		if len(destination.AggregateRoutes) > 0 {
			continue
		}
		if rel, err := destination.RouteArtifact(declaration.Path); err == nil {
			return destination.Root, rel, declaration.Member
		}
	}
	t.Fatalf("%s has no resolved MCP destination", target)
	return "", "", ""
}

func doctorNativeRepairSelection(target hcc.Target, name, command string, machine bool) []resolver.ResolvedArtifact {
	source := config.RepoCommitted
	if machine {
		source = config.UserPersonal
	}
	resource := ir.NewMcpServer(name, command, nil, nil, nil, ir.SourceRef{})
	return []resolver.ResolvedArtifact{{
		Entry:     catalog.Entry{Name: name, Resource: resource},
		Source:    source,
		Harnesses: []hcc.Target{target},
	}}
}

func writeDoctorNativeConflict(t *testing.T, repoRoot string, tc doctorNativeRepairCase, existing, desired string) (string, string, []resolver.ResolvedArtifact) {
	t.Helper()
	root, rel, container := doctorNativeRepairFile(t, repoRoot, tc.target, tc.machine)
	if err := os.MkdirAll(filepath.Dir(filepath.Join(root, rel)), 0o755); err != nil {
		t.Fatal(err)
	}
	name := "doctor-" + string(tc.target)
	document := map[string]any{container: map[string]any{
		name:          map[string]string{"command": existing},
		"handwritten": map[string]string{"command": "keep-handwritten"},
	}}
	raw, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, rel)
	if err := os.WriteFile(path, append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	return path, name, doctorNativeRepairSelection(tc.target, name, desired, tc.machine)
}

func doctorNativeCommandValue(t *testing.T, path, container, key string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	var entries map[string]json.RawMessage
	if err := json.Unmarshal(document[container], &entries); err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(entries[key], &value); err != nil {
		t.Fatal(err)
	}
	switch command := value["command"].(type) {
	case string:
		return command
	case []any:
		if len(command) == 1 {
			result, _ := command[0].(string)
			return result
		}
	}
	return ""
}

// spec: doctor-repair-planning/native-aggregate-repair-retains-action-safety
func TestDoctorNativeAggregateRepairRequiresPerEntryConsentAndRedactsValues(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	previous := slog.Default()
	var logs bytes.Buffer
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(previous)
	for _, tc := range doctorNativeRepairCases {
		t.Run(string(tc.target), func(t *testing.T) {
			for _, action := range []struct {
				name     string
				terminal bool
				yes      bool
				answer   string
				replaces bool
			}{
				{name: "terminal confirmation", terminal: true, answer: "y\n", replaces: true},
				{name: "off-terminal", answer: "y\n"},
				{name: "yes off-terminal", yes: true, answer: "y\n"},
				{name: "yes on-terminal", terminal: true, yes: true, answer: ""},
			} {
				t.Run(action.name, func(t *testing.T) {
					repoRoot, _ := devinDoctorEnv(t)
					t.Setenv("CLAUDE_CONFIG_DIR", "")
					t.Setenv("CODEX_HOME", filepath.Join(os.Getenv("HOME"), "codex"))
					existing := "existing-mcp-secret-" + string(tc.target)
					desired := "desired-mcp-secret-" + string(tc.target)
					path, name, selection := writeDoctorNativeConflict(t, repoRoot, tc, existing, desired)
					before, err := os.ReadFile(path)
					if err != nil {
						t.Fatal(err)
					}
					var out bytes.Buffer
					logs.Reset()
					err = runDoctor(doctorOptions{
						out:     &out,
						in:      strings.NewReader(action.answer),
						isTTY:   action.terminal,
						repair:  true,
						yes:     action.yes,
						probe:   staticProbe(devincli.Probe{Status: devincli.StatusNotInstalled}),
						resolve: staticResolve(selection),
					})
					if err != nil {
						t.Fatalf("doctor repair: %v\n%s", err, out.String())
					}
					combined := out.String() + logs.String()
					for _, secret := range []string{existing, desired} {
						if strings.Contains(combined, secret) {
							t.Fatalf("repair output leaked MCP value %q:\n%s", secret, combined)
						}
					}
					if action.replaces {
						container := "mcpServers"
						if tc.target == hcc.TargetOpenCode {
							container = "mcp"
						}
						if got := doctorNativeCommandValue(t, path, container, name); got != desired {
							t.Fatalf("terminal confirmation left command %q, want replacement", got)
						}
						if !strings.Contains(out.String(), "[y/N]") {
							t.Fatalf("terminal conflict did not ask for per-entry confirmation:\n%s", out.String())
						}
					} else {
						after, err := os.ReadFile(path)
						if err != nil || !bytes.Equal(before, after) {
							t.Fatalf("non-consenting repair changed the conflict: %v\n%s", err, after)
						}
					}
					if !strings.Contains(out.String(), fmt.Sprintf("replace mcp-server %q", name)) {
						t.Fatalf("repair output omitted the digest-only conflict for %s:\n%s", tc.target, out.String())
					}
				})
			}
		})
	}
}
