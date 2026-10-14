package materialize

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/jsonc"
)

func TestNativeHookDeselectionPrunesBesideEventRefusedSibling(t *testing.T) {
	repo, _ := additiveSafetyEnv(t)
	valid := nativeSupportSource(t, "claude", "hooks/hooks.json", nativeSupportClaudeFiles("echo valid-sibling"), "valid")
	refused := nativeSupportSource(t, "claude", "hooks/hooks.json", map[string]string{
		"hooks/hooks.json": `{"SubagentStop":[{"hooks":[{"type":"command","command":"echo refused"}]}]}`,
	}, "refused")
	loc := nativeSupportLocationFor(t, hcc.TargetWindsurf)
	hookFile := filepath.Join(repo, filepath.FromSlash(loc.repoFile))

	if _, err := Run(context.Background(), repo, nativeSupportResolve(t, hcc.TargetWindsurf, false, []resolver.ResolvedArtifact{valid, refused})); err != nil {
		t.Fatalf("first sync: %v", err)
	}
	if data, _ := os.ReadFile(hookFile); !strings.Contains(string(data), "echo valid-sibling") {
		t.Fatalf("first sync did not install the valid hook:\n%s", data)
	}

	report, err := Run(context.Background(), repo, nativeSupportResolve(t, hcc.TargetWindsurf, false, []resolver.ResolvedArtifact{refused}))
	if err != nil {
		t.Fatalf("deselecting sync: %v", err)
	}
	refusedEvent := false
	for _, d := range report.Diags {
		refusedEvent = refusedEvent || d.Code == "hook.windsurf.event-unmappable"
	}
	if !refusedEvent {
		t.Fatalf("sibling was not refused for its event: %v", report.Diags)
	}
	if data, _ := os.ReadFile(hookFile); strings.Contains(string(data), "echo valid-sibling") {
		t.Fatalf("deselected valid hook survived beside the event-refused sibling:\n%s", data)
	}
}

const selectivePruneFingerprint = "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"

func selectivePruneRule(command string) string {
	return nativeHookJSONGroup(command, "Bash", "")
}

func selectivePruneFixture(t *testing.T, owned []ownership.Entry, rules []string) (string, ownedJSONFileSpec, []byte, ownership.Proof, []ownedJSONExistingEntry, ownership.DriftReport) {
	t.Helper()
	repoRoot, home, codexHome := nativeHookJSONTestEnvironment(t)
	tc := nativeHookJSONCaseByName(t, "factory_repo")
	_, spec, err := nativeHookJSONLayout(outputKey{target: tc.target, machineScope: tc.machineScope}, repoRoot)
	if err != nil {
		t.Fatal("native hook layout did not resolve")
	}
	root, err := ownership.CanonicalRoot(tc.root(repoRoot, home, codexHome))
	if err != nil {
		t.Fatal("expected root could not be canonicalized")
	}
	doc := []byte(`{` + nativeHookJSONJSONString("PreToolUse") + `:[` + strings.Join(rules, ",") + `]}` + "\n")
	path := filepath.Join(root, filepath.FromSlash(spec.rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal("create aggregate parent")
	}
	if err := os.WriteFile(path, doc, 0o640); err != nil {
		t.Fatal("seed native aggregate")
	}
	store := writeNativeSatisfactionState(t, root, nativeHookLayout{rel: spec.rel}, doc, owned, nil)
	proof, err := store.Proof(spec.rel)
	if err != nil {
		t.Fatalf("Proof: %v", err)
	}
	parsed, err := jsonc.Parse(spec.rel, doc)
	if err != nil {
		t.Fatalf("parse fixture aggregate: %v", err)
	}
	container, _, err := ownedJSONContainerNode(parsed, spec)
	if err != nil || container == nil {
		t.Fatalf("fixture container missing: %v", err)
	}
	existing, err := ownedJSONExistingEntries(spec, container)
	if err != nil {
		t.Fatalf("fixture existing entries: %v", err)
	}
	values := make([]ownership.EntryValue, 0, len(existing))
	for _, e := range existing {
		values = append(values, e.value)
	}
	return root, spec, doc, proof, existing, ownership.Classify(proof, values, nil)
}

func TestNativeHookSelectivePruneOnIncompleteFile(t *testing.T) {
	validRule := selectivePruneRule("echo valid")
	refusedRule := selectivePruneRule("echo refused")

	t.Run("valid_removed_while_refused_sibling_selected", func(t *testing.T) {
		valid := nativeHookProofEntry(t, "PreToolUse", validRule, "valid", selectivePruneFingerprint)
		root, spec, doc, proof, existing, drift := selectivePruneFixture(t, []ownership.Entry{valid}, []string{validRule})
		before, _ := proof.Entry(valid.ID())

		patch := ownedJSONPatchPlan(root, spec, nil, existing, drift, proof, true, map[string]bool{"refused": true})
		if len(patch.prune) != 1 || patch.prune[0] != valid.ID() {
			t.Fatalf("prune = %#v, want the deselected valid hook", patch.prune)
		}
		if len(patch.prunePostponed) != 0 || patch.pruneSuppressed {
			t.Fatalf("postponed = %#v suppressed = %v, want no postponed prune", patch.prunePostponed, patch.pruneSuppressed)
		}
		if !reflect.DeepEqual(patch.deleteHookEvent, []string{"PreToolUse"}) {
			t.Fatalf("deleteHookEvent = %#v, want PreToolUse", patch.deleteHookEvent)
		}
		for _, owned := range patch.owned {
			if owned.ID() == valid.ID() {
				t.Fatal("pruned hook was retained as owned")
			}
		}
		if got, _ := os.ReadFile(filepath.Join(root, filepath.FromSlash(spec.rel))); !bytes.Equal(got, doc) {
			t.Fatal("planning changed the user's aggregate bytes")
		}
		if after, ok := proof.Entry(valid.ID()); !ok || !reflect.DeepEqual(after, before) {
			t.Fatal("planning changed the proof state")
		}
	})

	t.Run("selected_refused_entry_retained", func(t *testing.T) {
		valid := nativeHookProofEntry(t, "PreToolUse", validRule, "valid", selectivePruneFingerprint)
		refused := nativeHookProofEntry(t, "PreToolUse", refusedRule, "refused", selectivePruneFingerprint)
		root, spec, _, proof, existing, drift := selectivePruneFixture(t, []ownership.Entry{valid, refused}, []string{validRule, refusedRule})

		patch := ownedJSONPatchPlan(root, spec, nil, existing, drift, proof, true, map[string]bool{"refused": true})
		if len(patch.prune) != 1 || patch.prune[0] != valid.ID() {
			t.Fatalf("prune = %#v, want only the deselected valid hook", patch.prune)
		}
		if len(patch.prunePostponed) != 1 || patch.prunePostponed[0] != refused.ID() || !patch.pruneSuppressed {
			t.Fatalf("postponed = %#v suppressed = %v, want the selected refused hook postponed", patch.prunePostponed, patch.pruneSuppressed)
		}
		if len(patch.owned) != 1 || !reflect.DeepEqual(patch.owned[0], refused) {
			t.Fatalf("owned = %#v, want the postponed refused entry retained", patch.owned)
		}
		if elements := patch.hookElements["PreToolUse"]; len(elements) != 1 || string(elements[0]) != refusedRule {
			t.Fatal("the postponed refused element bytes were dropped")
		}
	})

	t.Run("shared_contributor_still_selected_retains", func(t *testing.T) {
		shared := nativeHookProofEntry(t, "PreToolUse", validRule, "valid", selectivePruneFingerprint)
		shared.SourceEvidence = []ownership.HookSourceEvidence{{Resource: "sibling", SourceDigest: selectivePruneFingerprint}}
		root, spec, _, proof, existing, drift := selectivePruneFixture(t, []ownership.Entry{shared}, []string{validRule})

		patch := ownedJSONPatchPlan(root, spec, nil, existing, drift, proof, true, map[string]bool{"sibling": true})
		if len(patch.prune) != 0 || len(patch.prunePostponed) != 1 || patch.prunePostponed[0] != shared.ID() {
			t.Fatalf("prune = %#v postponed = %#v, want the shared entry retained", patch.prune, patch.prunePostponed)
		}
	})

	t.Run("shared_contributors_all_deselected_prune", func(t *testing.T) {
		shared := nativeHookProofEntry(t, "PreToolUse", validRule, "valid", selectivePruneFingerprint)
		shared.SourceEvidence = []ownership.HookSourceEvidence{{Resource: "sibling", SourceDigest: selectivePruneFingerprint}}
		root, spec, _, proof, existing, drift := selectivePruneFixture(t, []ownership.Entry{shared}, []string{validRule})

		patch := ownedJSONPatchPlan(root, spec, nil, existing, drift, proof, true, map[string]bool{"other": true})
		if len(patch.prune) != 1 || patch.prune[0] != shared.ID() {
			t.Fatalf("prune = %#v, want the shared entry pruned when no contributor is selected", patch.prune)
		}
	})

	t.Run("missing_selection_keeps_blanket_suppression", func(t *testing.T) {
		valid := nativeHookProofEntry(t, "PreToolUse", validRule, "valid", selectivePruneFingerprint)
		root, spec, _, proof, existing, drift := selectivePruneFixture(t, []ownership.Entry{valid}, []string{validRule})

		for _, selected := range []map[string]bool{nil, {}} {
			patch := ownedJSONPatchPlan(root, spec, nil, existing, drift, proof, true, selected)
			if len(patch.prune) != 0 || len(patch.prunePostponed) != 1 || !patch.pruneSuppressed {
				t.Fatalf("selection %#v: prune = %#v postponed = %#v, want blanket suppression", selected, patch.prune, patch.prunePostponed)
			}
		}
	})

	t.Run("unknown_kind_forbids_selective_prune", func(t *testing.T) {
		mcp := ownership.EntryID{Kind: ownership.KindMcpServer, Container: "mcpServers", Key: "server", IdentityDigest: ownership.McpIdentityDigest("server")}
		if selectivePruneProven(mcp, ownership.Proof{}, map[string]bool{"other": true}) {
			t.Fatal("non-hook identity was selectively pruned")
		}
		hook := ownership.EntryID{Kind: ownership.KindHook, Container: "PreToolUse", IdentityDigest: ownership.HookIdentityDigest("PreToolUse", selectivePruneFingerprint)}
		if selectivePruneProven(hook, ownership.Proof{}, map[string]bool{"other": true}) {
			t.Fatal("unproven hook identity was selectively pruned")
		}
	})
}
