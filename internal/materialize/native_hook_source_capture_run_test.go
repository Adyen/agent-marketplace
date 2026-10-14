package materialize

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func nativeSupportPersistentSnapshotWithStableArtifactDirectoryTimes(t *testing.T, repo, home string) map[string]nativeSupportState {
	t.Helper()
	states := nativeSupportPersistentSnapshot(t, repo, home)
	for name, state := range states {
		if state.isDir && filepath.Base(name) == "artifacts" && strings.Contains(filepath.ToSlash(name), "/agent-marketplace/compiled/") {
			state.size = 0
			state.modified = 0
			states[name] = state
		}
	}
	return states
}

func TestRunRejectsStaleIngestedFactoryHookDefinition(t *testing.T) {
	repo, home := additiveSafetyEnv(t)
	factoryFiles := func(event, matcher, scriptName, script string) map[string]string {
		definition, err := json.Marshal(map[string]any{event: []any{map[string]any{
			"matcher": matcher,
			"hooks":   []any{map[string]any{"type": "command", "command": "${DROID_PLUGIN_ROOT}/scripts/" + scriptName}},
		}}})
		if err != nil {
			t.Fatal(err)
		}
		return map[string]string{
			".factory/hooks.json":            string(definition),
			".factory/scripts/" + scriptName: script,
		}
	}
	stale := nativeSupportSource(t, "factory", ".factory/hooks.json", factoryFiles("PreToolUse", "Execute", "stale.sh", "echo stale\n"))
	sibling := nativeSupportSource(t, "factory", ".factory/hooks.json", factoryFiles("PostToolUse", "Read", "sibling.sh", "echo sibling\n"))
	staleHook, ok := stale.Resource.(*ir.Hook)
	if !ok || staleHook.SourceDefinitionDigest() == "" {
		t.Fatalf("factory catalog hook has no observed definition digest: %#v", stale.Resource)
	}
	staleName := stale.Name
	siblingName := sibling.Name
	resolved := nativeSupportResolve(t, hcc.TargetFactory, false, []resolver.ResolvedArtifact{stale, sibling})
	if _, err := Run(context.Background(), repo, resolved); err != nil {
		t.Fatalf("initial Run: %v", err)
	}
	factoryOutput := filepath.Join(repo, ".factory", "hooks.json")
	initialOutput, err := os.ReadFile(factoryOutput)
	if err != nil || !strings.Contains(string(initialOutput), "stale.sh") || !strings.Contains(string(initialOutput), "sibling.sh") {
		t.Fatalf("initial Factory output does not contain both hooks: %v %s", err, initialOutput)
	}
	before := nativeSupportPersistentSnapshotWithStableArtifactDirectoryTimes(t, filepath.Join(repo, ".factory"), home)
	originalCommand := staleHook.Command
	originalDigest := staleHook.SourceDefinitionDigest()
	updated := factoryFiles("PreToolUse", "Execute", "changed.sh", "echo changed\n")
	for rel, body := range updated {
		path := filepath.Join(stale.RegistryURL, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	report, err := Run(context.Background(), repo, resolved)
	if err != nil {
		t.Fatalf("stale-definition Run: %v", err)
	}
	var staleErrors int
	for _, diagnostic := range report.Diags {
		if diagnostic.Code == "materialize.compile.error" {
			if diagnostic.Resource != staleName {
				t.Fatalf("independent sibling was refused: %+v", diagnostic)
			}
			staleErrors++
		}
	}
	if staleErrors != 1 {
		t.Fatalf("materialize.compile.error count = %d, want one stale-resource refusal: %#v", staleErrors, report.Diags)
	}
	var staleBlocked, siblingBlocked bool
	for _, blocked := range report.BlockedResources {
		staleBlocked = staleBlocked || blocked.Name == staleName
		siblingBlocked = siblingBlocked || blocked.Name == siblingName
	}
	if !staleBlocked || siblingBlocked {
		t.Fatalf("blocked resources = %#v; want stale hook only (stale=%q sibling=%q)", report.BlockedResources, staleName, siblingName)
	}
	if staleHook.Command != originalCommand || staleHook.SourceDefinitionDigest() != originalDigest {
		t.Fatal("Run mutated the retained ingested hook")
	}
	assertNativeSupportSnapshot(t, before, nativeSupportPersistentSnapshotWithStableArtifactDirectoryTimes(t, filepath.Join(repo, ".factory"), home))

	freshCatalog, err := catalog.Load(stale.RegistryURL, "factory")
	if err != nil {
		t.Fatalf("fresh Factory catalog: %v", err)
	}
	if len(freshCatalog.Entries) != 1 || freshCatalog.Entries[0].Kind != ir.KindHook {
		t.Fatalf("fresh Factory catalog entries = %#v, want one hook", freshCatalog.Entries)
	}
	fresh := stale
	fresh.Entry = freshCatalog.Entries[0]
	freshResolved := nativeSupportResolve(t, hcc.TargetFactory, false, []resolver.ResolvedArtifact{fresh, sibling})
	freshReport, err := Run(context.Background(), repo, freshResolved)
	if err != nil || diag.HasErrors(freshReport.Diags) {
		t.Fatalf("fresh definition Run: %v %v", err, freshReport)
	}
	freshOutput, err := os.ReadFile(factoryOutput)
	if err != nil || !strings.Contains(string(freshOutput), "changed.sh") || !strings.Contains(string(freshOutput), "sibling.sh") || strings.Contains(string(freshOutput), "stale.sh") {
		t.Fatalf("fresh Factory output = %v %s", err, freshOutput)
	}
	for _, want := range []struct{ name, data string }{{"changed.sh", "echo changed\n"}, {"sibling.sh", "echo sibling\n"}} {
		matches, err := filepath.Glob(filepath.Join(repo, ".factory", "hook-support", "*", "*", "scripts", want.name))
		if err != nil || len(matches) != 1 {
			t.Fatalf("fresh support %s matches=%v err=%v", want.name, matches, err)
		}
		data, err := os.ReadFile(matches[0])
		if err != nil || string(data) != want.data {
			t.Fatalf("fresh support %s bytes=%q err=%v, want %q", want.name, data, err, want.data)
		}
		info, err := os.Stat(matches[0])
		if err != nil {
			t.Fatalf("fresh support %s stat: %v", want.name, err)
		}
		if info.Mode().Perm() != 0o644 {
			t.Fatalf("fresh support %s mode=%v, want 0644", want.name, info.Mode().Perm())
		}
	}
	freshHook, ok := fresh.Resource.(*ir.Hook)
	if !ok || freshHook.Command != "${DROID_PLUGIN_ROOT}/scripts/changed.sh" || freshHook.SourceDefinitionDigest() == originalDigest {
		t.Fatalf("fresh ingested hook = %#v, want updated command and definition proof", fresh.Resource)
	}
}

func TestRunRejectsExcludedNativePeerDirectories(t *testing.T) {
	for _, tc := range []struct {
		name     string
		nonempty bool
	}{
		{name: "empty directory"},
		{name: "nonempty directory", nonempty: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, home := additiveSafetyEnv(t)
			provider := nativeSupportSource(t, "devin", ".devin/hooks.v1.json", map[string]string{
				".devin/hooks.v1.json":   `{"PreToolUse":[{"matcher":"exec","hooks":[{"type":"command","command":"./run.sh"}]}]}`,
				".devin/config.json":     `{"hooks":{"PreToolUse":[]}}`,
				".devin/mcp_config.json": `{"mcpServers":{}}`,
				".devin/run.sh":          "peer directory control\n",
			})
			resolved := nativeSupportResolve(t, hcc.TargetDevin, false, []resolver.ResolvedArtifact{provider})
			if _, err := Run(context.Background(), repo, resolved); err != nil {
				t.Fatalf("seed prior installation: %v", err)
			}
			beforeInstalled := work19PersistentHookSnapshot(t, repo, home, ".devin")
			peer := filepath.Join(provider.RegistryURL, ".devin", "config.json")
			if err := os.Remove(peer); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(peer, 0o755); err != nil {
				t.Fatal(err)
			}
			if tc.nonempty {
				if err := os.WriteFile(filepath.Join(peer, "nested.json"), []byte("preserve peer directory payload\n"), 0o640); err != nil {
					t.Fatal(err)
				}
			}
			beforePeer := nativeSupportStableSnapshot(t, peer)
			plans, conflicts := prepareHookSupport([]skillTarget{{skill: resolved[0], target: hcc.TargetDevin}})
			report, err := Run(context.Background(), repo, resolved)
			if err != nil {
				t.Fatalf("peer-directory Run: %v", err)
			}
			requireNativeSupportCompileError(t, report, "")
			if plans[0].err == nil || plans[0].sourceDigest != "" || len(plans[0].artifacts) != 0 || len(conflicts) != 0 {
				t.Fatalf("excluded peer directory produced proof/support: plan=%+v conflicts=%v", plans[0], conflicts)
			}
			assertNativeSupportSnapshot(t, beforeInstalled, work19PersistentHookSnapshot(t, repo, home, ".devin"))
			assertNativeSupportSnapshot(t, beforePeer, nativeSupportStableSnapshot(t, peer))
		})
	}
}

func work19PersistentHookSnapshot(t *testing.T, repo, home, targetRoot string) map[string]nativeSupportState {
	t.Helper()
	states := nativeSupportStableSnapshot(t, filepath.Join(repo, targetRoot), filepath.Join(home, "state", "agent-marketplace"))
	locks := filepath.Join(home, "state", "agent-marketplace", "locks")
	for name, state := range states {
		rel, err := filepath.Rel(locks, name)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			state.size = 0
			state.modified = 0
			state.data = nil
			states[name] = state
		}
	}
	return states
}
