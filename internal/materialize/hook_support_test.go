package materialize

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// spec: hook-resources/conflicting-support-files-refuse-the-affected-hooks
func TestPrepareHookSupportDetectsSameOutputConflicts(t *testing.T) {
	first := makeHookSupportSource(t, "echo first", 0o644)
	second := makeHookSupportSource(t, "echo second", 0o644)
	work := []skillTarget{
		{skill: resolverHook(first, "first"), target: hcc.TargetFactory},
		{skill: resolverHook(second, "second"), target: hcc.TargetFactory},
	}
	_, conflicts := prepareHookSupport(work)
	if len(conflicts) != 2 || len(conflicts[0]) != 1 || len(conflicts[1]) != 1 {
		t.Fatalf("conflicts = %#v, want one conflict per producer", conflicts)
	}

	equal := makeHookSupportSource(t, "echo same", 0o644)
	equalWork := []skillTarget{{skill: resolverHook(equal, "one"), target: hcc.TargetFactory}, {skill: resolverHook(equal, "two"), target: hcc.TargetFactory}}
	_, conflicts = prepareHookSupport(equalWork)
	if len(conflicts) != 0 {
		t.Fatalf("equal support files conflicted: %#v", conflicts)
	}

	mode := makeHookSupportSource(t, "echo same", 0o600)
	modeWork := []skillTarget{{skill: resolverHook(equal, "one"), target: hcc.TargetFactory}, {skill: resolverHook(mode, "two"), target: hcc.TargetFactory}}
	_, conflicts = prepareHookSupport(modeWork)
	if len(conflicts) != 2 {
		t.Fatalf("mode conflict = %#v, want both producers", conflicts)
	}

	differentTarget := []skillTarget{{skill: resolverHook(first, "one"), target: hcc.TargetFactory}, {skill: resolverHook(second, "two"), target: hcc.TargetWindsurf}}
	_, conflicts = prepareHookSupport(differentTarget)
	if len(conflicts) != 0 {
		t.Fatalf("cross-target support files conflicted: %#v", conflicts)
	}
}

func makeHookSupportSource(t *testing.T, script string, mode os.FileMode) string {
	t.Helper()
	dir := t.TempDir()
	hooks := filepath.Join(dir, "hooks.json")
	if err := os.WriteFile(hooks, []byte(`{"hooks":{"pre_run_command":[{"command":"echo hook"}]}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "shared.sh"), []byte(script), mode); err != nil {
		t.Fatal(err)
	}
	return hooks
}

func resolverHook(path, name string) resolver.ResolvedArtifact {
	hook := ir.NewHookWithMatcher(name, "PreToolUse", "Bash", "echo hook", "command", nil, ir.SourceRef{File: path})
	return resolver.ResolvedArtifact{Entry: catalog.Entry{Name: name, Kind: ir.KindHook, Path: path, Resource: hook}, Source: config.RepoCommitted}
}

func TestHookSupportExclusions(t *testing.T) {
	tests := []struct {
		name          string
		sourcePath    string
		sourceHarness string
		want          map[string]bool
	}{
		{name: "devin", sourcePath: "/tmp/.devin/hooks.v1.json", sourceHarness: "devin", want: map[string]bool{"hooks.v1.json": true, "config.json": true, "mcp_config.json": true}},
		{name: "codex", sourcePath: "/tmp/.codex/hooks.json", sourceHarness: "codex", want: map[string]bool{"hooks.json": true, "config.toml": true}},
		{name: "windsurf", sourcePath: "/tmp/.windsurf/hooks.json", sourceHarness: "windsurf", want: map[string]bool{"hooks.json": true, "mcp_config.json": true}},
		{name: "factory", sourcePath: "/tmp/.factory/hooks/hooks.json", sourceHarness: "factory", want: map[string]bool{"hooks.json": true}},
		{name: "unknown", sourcePath: "/tmp/custom.json", sourceHarness: "synthetic", want: map[string]bool{"custom.json": true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hookSupportExclusions(tt.sourcePath, tt.sourceHarness)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("exclusions = %#v, want %#v", got, tt.want)
			}
		})
	}
}
