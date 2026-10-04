package materialize

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestPrepareHookSupportNamespacesSupportFiles(t *testing.T) {
	first := makeHookSupportSource(t, "echo first", 0o644)
	second := makeHookSupportSource(t, "echo second", 0o644)
	work := []skillTarget{
		{skill: resolverHook(first, "first"), target: hcc.TargetFactory},
		{skill: resolverHook(second, "second"), target: hcc.TargetFactory},
	}
	plans, conflicts := prepareHookSupport(work)
	if len(conflicts) != 0 {
		t.Fatalf("distinct sources must not conflict: %#v", conflicts)
	}
	for i, want := range []string{"first", "second"} {
		plan := plans[i]
		if plan.err != nil {
			t.Fatalf("plan %d err: %v", i, plan.err)
		}
		if !strings.HasPrefix(plan.relBase, "hook-support/") {
			t.Fatalf("plan %d relBase %q not under the hook support dir", i, plan.relBase)
		}
		if !strings.HasSuffix(plan.relBase, "/"+want) {
			t.Fatalf("plan %d relBase %q missing installed name %q", i, plan.relBase, want)
		}
		if len(plan.artifacts) != 1 || plan.artifacts[0].Path != plan.relBase+"/shared.sh" {
			t.Fatalf("plan %d artifacts = %#v", i, plan.artifacts)
		}
	}
	if plans[0].relBase == plans[1].relBase {
		t.Fatalf("distinct sources share relBase %q", plans[0].relBase)
	}
}

func makeHookSupportSource(t *testing.T, script string, mode os.FileMode) string {
	t.Helper()
	dir := t.TempDir()
	hooks := filepath.Join(dir, "hooks.json")
	if err := os.WriteFile(hooks, []byte(`{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"${CLAUDE_PLUGIN_ROOT}/shared.sh"}]}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "shared.sh"), []byte(script), mode); err != nil {
		t.Fatal(err)
	}
	return hooks
}

func resolverHook(path, name string) resolver.ResolvedArtifact {
	root := filepath.Dir(path)
	hook := ir.NewHookWithMatcher(name, "PreToolUse", "Bash", "${CLAUDE_PLUGIN_ROOT}/shared.sh", "command", nil, ir.SourceRef{File: path})
	return resolver.ResolvedArtifact{
		Entry:       catalog.Entry{Name: name, Kind: ir.KindHook, Path: path, Resource: hook},
		RegistryURL: root,
		CatalogKey:  root,
		Source:      config.RepoCommitted,
	}
}

func TestHookSupportExclusions(t *testing.T) {
	tests := []struct {
		name          string
		sourceHarness string
		definitionDir string
		want          map[string]bool
		inventoryDir  bool
	}{
		{name: "devin", sourceHarness: "devin", definitionDir: ".devin", want: map[string]bool{"hooks.v1.json": true, "config.json": true, "mcp_config.json": true}},
		{name: "codex", sourceHarness: "codex", definitionDir: ".codex", want: map[string]bool{"hooks.json": true, "config.toml": true}},
		{name: "windsurf inventory", sourceHarness: "windsurf", definitionDir: ".windsurf/hooks", want: map[string]bool{}, inventoryDir: true},
		{name: "factory legacy directory", sourceHarness: "factory", definitionDir: ".factory/hooks", want: map[string]bool{"hooks.json": true}},
		{name: "unknown", sourceHarness: "synthetic", definitionDir: ".", want: map[string]bool{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := resolver.ResolvedArtifact{Entry: catalog.Entry{SourceHarness: tt.sourceHarness}}
			got, inventoryDir := hookSupportExclusions(res, tt.definitionDir)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("exclusions = %#v, want %#v", got, tt.want)
			}
			if inventoryDir != tt.inventoryDir {
				t.Fatalf("inventory directory = %v, want %v", inventoryDir, tt.inventoryDir)
			}
		})
	}
}
