package materialize

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"

	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/resolver"
)

// TestAdversarialPlainSyncPreservesAHandWrittenHookIdentityTwin is the
// executable acceptance test for the total hook/v2 identity.
//
// docs/plans/2026-08-06-ownership-locking-design.md states, as the fact that
// scopes the whole locking design's severity: "User hand-written bytes are **not**
// destroyed by plain sync; that outcome is scoped to `doctor --repair`'s
// conflict-replace."
//
// With the old lossy hook/v1 identity, devinHookIdentity consumed "type",
// "matcher", "command", "prompt" and "timeout" by name and only used the payload
// key matching the type. A hand-written element that differed from a managed one
// only in a member the identity ignored could share the identity digest while
// having a different value digest. Plain sync then classified it OwnedDrifted and
// replaced it from marketplace configuration without confirmation.
//
// The total hook/v2 identity derives the identity from the event name and the
// digest of the whole canonical JSON value, so any byte difference in the value
// produces a different identity. The hand-written twin is therefore unmanaged and
// is preserved; the managed element remains owned clean. Plain sync must leave
// the hand-written element in place.
func TestAdversarialPlainSyncPreservesAHandWrittenHookIdentityTwin(t *testing.T) {
	repoRoot, _ := devinEnv(t)
	root := devinProjectRoot(repoRoot)
	selection := []resolver.ResolvedArtifact{
		devinResolved("beta", ir.NewHook("beta", "PostToolUse", "echo beta", "command", nil, ir.SourceRef{}), false),
	}
	if _, err := Run(context.Background(), repoRoot, selection); err != nil {
		t.Fatalf("seed sync: %v", err)
	}
	hooks := filepath.Join(root, harness.DevinProjectHooksFile)
	// The hand-written element is FIRST, which is the layout AGENTS.md documents.
	body := "{\n  \"PostToolUse\": [\n    " + devinTwinHandElement + ",\n    " + devinTwinManagedElem + "\n  ]\n}\n"
	if err := os.WriteFile(hooks, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Run(context.Background(), repoRoot, selection); err != nil {
		t.Fatalf("second sync: %v", err)
	}
	got := readFileString(t, hooks)
	if !strings.Contains(got, devinTwinHandMarker) {
		t.Fatalf("PLAIN SYNC DELETED A HAND-WRITTEN HOOK ELEMENT with no confirmation:\n%s", got)
	}
	if !strings.Contains(got, devinTwinManagedElem) {
		t.Fatalf("the managed hook element is no longer present in the file:\n%s", got)
	}
	store, err := ownership.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	proof, err := store.Proof(harness.DevinProjectHooksFile)
	if err != nil {
		t.Fatal(err)
	}
	if proof.OwnedCount() != 1 {
		t.Fatalf("the managed hook element is no longer owned clean: %d owned entries", proof.OwnedCount())
	}
}
