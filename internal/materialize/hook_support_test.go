package materialize

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

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
