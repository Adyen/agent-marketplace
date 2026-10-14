package materialize

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/jsonc"
)

func TestDevinOwnedLegacyHooksUpgradeWithoutChangingHandwrittenGroups(t *testing.T) {
	for _, machine := range []bool{false, true} {
		name := "project"
		if machine {
			name = "user"
		}
		t.Run(name, func(t *testing.T) {
			repo, home := devinEnv(t)
			root, rel := devinProjectRoot(repo), harness.DevinProjectHooksFile
			legacy := `{"PostToolUse":[{"command":"echo managed","type":"command"}]}`
			if machine {
				root, rel = devinUserRoot(home), harness.DevinUserConfigFile
				legacy = `{"hooks":` + legacy + `}`
			}
			entries, err := devinFragmentEntries(rel, []byte(legacy), machine, "managed-hook")
			if err != nil {
				t.Fatal(err)
			}
			work := &ownedJSONAggregateWork{}
			for _, entry := range entries {
				work.add(entry)
			}
			key := outputKey{target: hcc.TargetDevin, machineScope: machine}
			seed := reconcileDevinAggregates(context.Background(), &Report{PerTarget: map[hcc.Target]TargetReport{}}, key, root, work, false)
			requireNoErrorDiags(t, seed.Diags)
			const handwritten = `{ "hooks": [{ "type": "command", "command": "echo handwritten" }] }`
			mixed := "{\n  // handwritten group stays first\n  \"PostToolUse\": [\n    " + handwritten + ",\n    {\"command\":\"echo managed\",\"type\":\"command\"}\n  ]\n}\n"
			if machine {
				mixed = "{\n  \"auto_update\": false,\n  \"hooks\": " + mixed + "}\n"
			}
			path := filepath.Join(root, rel)
			if err := os.WriteFile(path, []byte(mixed), 0o600); err != nil {
				t.Fatal(err)
			}
			selection := []resolver.ResolvedArtifact{devinResolved(t, "managed-hook", ir.NewHook("managed-hook", "PostToolUse", "echo managed", "command", nil, ir.SourceRef{}), machine)}
			report, err := Run(context.Background(), repo, selection)
			if err != nil {
				t.Fatal(err)
			}
			requireNoErrorDiags(t, report.Diags)
			got := readFileString(t, path)
			if !strings.Contains(got, handwritten) || !strings.Contains(got, "// handwritten group stays first") || strings.Index(got, "echo handwritten") >= strings.Index(got, "echo managed") {
				t.Fatalf("legacy upgrade changed handwritten bytes or order: %s", got)
			}
			var document map[string]json.RawMessage
			if err := jsonc.Unmarshal(rel, []byte(got), &document); err != nil {
				t.Fatal(err)
			}
			if machine {
				if string(document["auto_update"]) != "false" {
					t.Fatal("unrelated user configuration changed")
				}
				if err := json.Unmarshal(document["hooks"], &document); err != nil {
					t.Fatal(err)
				}
			}
			var groups []map[string]json.RawMessage
			if err := json.Unmarshal(document["PostToolUse"], &groups); err != nil || len(groups) != 2 {
				t.Fatalf("expected handwritten and upgraded managed groups: %v: %s", err, got)
			}
			for _, group := range groups {
				var commands []struct{ Type, Command string }
				if err := json.Unmarshal(group["hooks"], &commands); err != nil || len(commands) != 1 || commands[0].Type != "command" || commands[0].Command == "" {
					t.Fatalf("a legacy flat hook survived the owned upgrade: %s", got)
				}
			}
			second, err := Run(context.Background(), repo, selection)
			if err != nil {
				t.Fatal(err)
			}
			requireNoErrorDiags(t, second.Diags)
			if again := readFileString(t, path); again != got {
				t.Fatal("upgraded native hook groups are not idempotent")
			}
		})
	}
}

func devinSnapshotEnv(t *testing.T) (string, string) {
	t.Helper()
	repo, home := devinEnv(t)
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	return repo, home
}

func devinHookAggregatePath(repo, home string, machine bool) (string, string) {
	if machine {
		return devinUserRoot(home), harness.DevinUserConfigFile
	}
	return devinProjectRoot(repo), harness.DevinProjectHooksFile
}

func devinFlatHookDocument(machine bool, rule string) []byte {
	document := `{"PostToolUse":[` + rule + `]}`
	if machine {
		document = `{"hooks":` + document + `}`
	}
	return []byte(document)
}

func requireDevinShapeRefusal(t *testing.T, report *Report) {
	t.Helper()
	if report == nil || !diag.HasCode(report.Diags, diag.CodeMaterializeDevinAggregateShape) || !diag.HasErrors(report.Diags) {
		t.Fatalf("Devin shape refusal diagnostics = %+v", report)
	}
	for _, diagnostic := range report.Diags {
		if diagnostic.Code == "materialize.compile.error" {
			t.Fatalf("valid selected hook failed to compile: %+v", report.Diags)
		}
	}
}

func seedDevinOwnedFlatHook(t *testing.T, root, rel string, machine bool, document []byte) *ownership.Store {
	t.Helper()
	entries, err := devinFragmentEntries(rel, document, machine, "managed-hook")
	if err != nil {
		t.Fatalf("parse legacy Devin hook fixture: %v", err)
	}
	work := &ownedJSONAggregateWork{}
	for _, entry := range entries {
		work.add(entry)
	}
	key := outputKey{target: hcc.TargetDevin, machineScope: machine}
	seed := reconcileDevinAggregates(context.Background(), &Report{PerTarget: map[hcc.Target]TargetReport{}}, key, root, work, false)
	requireNoErrorDiags(t, seed.Diags)
	store, err := ownership.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func TestDevinUnownedFlatHooksRefuseWithoutMutation(t *testing.T) {
	for _, machine := range []bool{false, true} {
		name := "project"
		if machine {
			name = "user"
		}
		t.Run(name, func(t *testing.T) {
			repo, home := devinSnapshotEnv(t)
			root, rel := devinHookAggregatePath(repo, home, machine)
			path := filepath.Join(root, rel)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			flat := devinFlatHookDocument(machine, `{"command":"echo unmanaged","type":"command"}`)
			if err := os.WriteFile(path, flat, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, 0o600); err != nil {
				t.Fatal(err)
			}
			resolved := devinResolved(t, "managed-hook", ir.NewHook("managed-hook", "PostToolUse", "echo managed", "command", nil, ir.SourceRef{}), machine)
			store, err := ownership.Open(root)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := os.Lstat(store.StateFile()); !os.IsNotExist(err) {
				t.Fatalf("flat fixture unexpectedly has ownership state: %v", err)
			}
			seedNativeHookJSONLock(t, root)
			before, mode, mtime := fileSnapshot(t, path)
			stateBefore := nativeSupportPersistentSnapshot(t, repo, home)

			report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{resolved})
			if err != nil {
				t.Fatalf("Run unowned flat hook: %v", err)
			}
			requireDevinShapeRefusal(t, report)
			assertFileUnchanged(t, path, before, mode, mtime)
			assertNativeSupportSnapshot(t, stateBefore, nativeSupportPersistentSnapshot(t, repo, home))
			if _, err := os.Lstat(store.StateFile()); !os.IsNotExist(err) {
				t.Fatalf("unowned flat refusal wrote ownership state: %v", err)
			}
		})
	}
}

func TestDevinOwnedFlatHookWithMalformedHooksMemberRefuses(t *testing.T) {
	for _, machine := range []bool{false, true} {
		name := "project"
		if machine {
			name = "user"
		}
		t.Run(name, func(t *testing.T) {
			repo, home := devinSnapshotEnv(t)
			root, rel := devinHookAggregatePath(repo, home, machine)
			flat := devinFlatHookDocument(machine, `{"command":"echo managed","type":"command","hooks":null}`)
			store := seedDevinOwnedFlatHook(t, root, rel, machine, flat)
			proofBefore, err := store.Proof(rel)
			if err != nil || proofBefore.OwnedCount() != 1 || len(proofBefore.OwnedIDs()) != 1 || len(proofBefore.HookSatisfactions()) != 0 {
				t.Fatalf("malformed legacy fixture proof = %v / %+v", err, proofBefore.OwnedIDs())
			}
			id := proofBefore.OwnedIDs()[0]
			entryBefore, ok := proofBefore.Entry(id)
			if !ok || proofBefore.Classify(id, entryBefore.ValueDigest) != ownership.StatusOwnedClean {
				t.Fatalf("malformed legacy fixture is not owned-clean: %+v", entryBefore)
			}
			path := filepath.Join(root, rel)
			before, mode, mtime := fileSnapshot(t, path)
			resolved := devinResolved(t, "managed-hook", ir.NewHook("managed-hook", "PostToolUse", "echo managed", "command", nil, ir.SourceRef{}), machine)
			stateBefore := nativeSupportPersistentSnapshot(t, repo, home)

			report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{resolved})
			if err != nil {
				t.Fatalf("Run malformed owned flat hook: %v", err)
			}
			requireDevinShapeRefusal(t, report)
			assertFileUnchanged(t, path, before, mode, mtime)
			assertNativeSupportSnapshot(t, stateBefore, nativeSupportPersistentSnapshot(t, repo, home))
			proofAfter, err := store.Proof(rel)
			if err != nil || proofAfter.Generation() != proofBefore.Generation() || proofAfter.Classify(id, entryBefore.ValueDigest) != ownership.StatusOwnedClean {
				t.Fatalf("malformed owned hook proof changed: %v / generation %d", err, proofAfter.Generation())
			}
			entryAfter, ok := proofAfter.Entry(id)
			if !ok || entryAfter.ValueDigest != entryBefore.ValueDigest || entryAfter.Resource != entryBefore.Resource {
				t.Fatalf("malformed owned hook record changed: %+v", entryAfter)
			}
		})
	}
}
