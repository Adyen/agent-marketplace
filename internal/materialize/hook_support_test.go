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
		{skill: resolverHook(t, first, "first"), target: hcc.TargetFactory},
		{skill: resolverHook(t, second, "second"), target: hcc.TargetFactory},
	}
	plans, conflicts := prepareHookSupport(work)
	if len(conflicts) != 0 {
		t.Fatalf("distinct source paths conflicted: %#v", conflicts)
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
		if len(plan.artifacts) != 1 || plan.artifacts[0].Path != plan.relBase+"/hooks/shared.sh" {
			t.Fatalf("plan %d artifacts = %#v", i, plan.artifacts)
		}
	}
	if plans[0].relBase == plans[1].relBase {
		t.Fatalf("distinct sources share relBase %q", plans[0].relBase)
	}

	equal := makeHookSupportSource(t, "echo same", 0o644)
	equalWork := []skillTarget{{skill: resolverHook(t, equal, "one"), target: hcc.TargetFactory}, {skill: resolverHook(t, equal, "two"), target: hcc.TargetFactory}}
	equalPlans, conflicts := prepareHookSupport(equalWork)
	if len(conflicts) != 0 || equalPlans[0].err != nil || equalPlans[1].err != nil || len(equalPlans[0].artifacts) != 1 || len(equalPlans[1].artifacts) != 1 || equalPlans[0].relBase == equalPlans[1].relBase {
		t.Fatalf("same-source installed names were not isolated: plans=%#v conflicts=%#v", equalPlans, conflicts)
	}

	mode := makeHookSupportSource(t, "echo same", 0o600)
	modeWork := []skillTarget{{skill: resolverHook(t, equal, "one"), target: hcc.TargetFactory}, {skill: resolverHook(t, mode, "two"), target: hcc.TargetFactory}}
	modePlans, conflicts := prepareHookSupport(modeWork)
	if len(conflicts) != 0 || modePlans[0].err != nil || modePlans[1].err != nil || len(modePlans[0].artifacts) != 1 || len(modePlans[1].artifacts) != 1 || modePlans[0].artifacts[0].Mode.Perm() != 0o644 || modePlans[1].artifacts[0].Mode.Perm() != 0o600 {
		t.Fatalf("per-source support mode changed: plans=%#v conflicts=%#v", modePlans, conflicts)
	}

	differentTarget := []skillTarget{{skill: resolverHook(t, first, "one"), target: hcc.TargetFactory}, {skill: resolverHook(t, second, "two"), target: hcc.TargetWindsurf}}
	_, conflicts = prepareHookSupport(differentTarget)
	if len(conflicts) != 0 {
		t.Fatalf("cross-target support files conflicted: %#v", conflicts)
	}
}

func makeHookSupportSource(t *testing.T, script string, mode os.FileMode) string {
	t.Helper()
	dir := t.TempDir()
	hooks := filepath.Join(dir, "hooks", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(hooks), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hooks, []byte(`{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"${CLAUDE_PLUGIN_ROOT}/shared.sh"}]}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hooks", "shared.sh"), []byte(script), mode); err != nil {
		t.Fatal(err)
	}
	return hooks
}

func resolverHook(t *testing.T, path, name string) resolver.ResolvedArtifact {
	t.Helper()
	root := filepath.Dir(filepath.Dir(path))
	loaded, err := catalog.Load(root, "claude")
	if err != nil || len(loaded.Entries) != 1 || loaded.Entries[0].Kind != ir.KindHook {
		t.Fatalf("Claude support source ingest = %+v / %v", loaded.Entries, err)
	}
	entry := loaded.Entries[0]
	entry.Name = name
	return resolver.ResolvedArtifact{
		Entry:       entry,
		RegistryURL: root,
		CatalogKey:  root,
		V4Kind:      config.V4SourceDirectory,
		Source:      config.RepoCommitted,
		Harnesses:   []hcc.Target{hcc.TargetFactory},
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
