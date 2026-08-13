package materialize

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"

	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/resolver"
)

// Adversarial review of bead agent-marketplace-npwr (B4).
//
// B4 put each of the three repair mutators' whole read-modify-commit inside one
// held write session. These are the only code paths in the product that DELETE
// user-written content, so the review question is not "is the section wide
// enough" (it is) but "does a confirmed action destroy the entry the user was
// shown, and nothing else". The section makes the RE-DERIVATION trustworthy;
// this file attacks what happens AFTER it — the step that turns a re-derived
// candidate back into an edit of the document.
//
// The headline finding is below: for the hook shape the translation is done by
// IDENTITY alone, while nomination is done by identity AND value digest. With the
// old lossy hook/v1 identity, two hook elements could share an identity digest
// and differ in value, so a confirmed removal could delete the wrong one. The
// total hook/v2 identity fixes the root cause, but the value digest is still
// required to remove exactly one of two byte-identical hooks.

// devinHookIdentityTwins is the fixture at the centre of this file.
//
// The two elements below are intentionally SIMILAR but not identical: the hand-
// written one carries an extra "prompt" member the managed one does not. With a
// total hook identity they have DIFFERENT identity digests and DIFFERENT value
// digests, so only the managed element (which has retained safety-backup
// evidence) is nominated for removal. The value-digest check still ensures the
// right element is deleted.
//
// The order matters and is the documented normal layout, not a contrivance:
// AGENTS.md states "handwritten entries, comments, key order, and hook order
// survive; handwritten hooks precede generated ones". So the hand-written
// element sits FIRST and the managed element sits second.
const (
	devinTwinHandMarker  = "USER-WROTE-THIS"
	devinTwinHandElement = `{"type":"command","command":"echo beta","prompt":"` + devinTwinHandMarker + `"}`
	devinTwinManagedElem = `{"command":"echo beta","type":"command"}`
)

// prepareDevinHookIdentityTwins produces a project hooks.v1.json holding two
// similar hook elements, with retained safety-backup evidence for the MANAGED
// one only, and no ownership record. With a total hook identity only the
// managed element is an evidence-backed orphan candidate.
func prepareDevinHookIdentityTwins(t *testing.T, repoRoot string) (hooksPath string, file *RepairFile) {
	t.Helper()
	root := devinProjectRoot(repoRoot)
	selection := []resolver.ResolvedArtifact{
		devinResolved("beta", ir.NewHook("beta", "PostToolUse", "echo beta", "command", nil, ir.SourceRef{}), false),
	}
	if _, err := Run(context.Background(), repoRoot, selection); err != nil {
		t.Fatalf("seed sync: %v", err)
	}
	hooksPath = filepath.Join(root, harness.DevinProjectHooksFile)

	body := "{\n  \"PostToolUse\": [\n    " + devinTwinHandElement + ",\n    " + devinTwinManagedElem + "\n  ]\n}\n"
	if err := os.WriteFile(hooksPath, []byte(body), 0o644); err != nil {
		t.Fatalf("hand-edit the aggregate: %v", err)
	}
	// No record, so both elements are unmanaged; the retained backup manifest of
	// the generation that wrote the managed element is the only evidence, and it
	// records the managed element's value digest.
	dropOwnershipState(t, root)

	// Nothing is desired any more, so the managed element is an undesired,
	// unmanaged, evidence-backed orphan candidate.
	preview, err := DevinRepairPreview(context.Background(), repoRoot, nil)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	file = repairFileFor(t, preview, harness.DevinProjectHooksFile)
	if file.ExistingEntries != 2 {
		t.Fatalf("the fixture has %d existing hook entries, want 2", file.ExistingEntries)
	}
	if len(file.Orphans) != 1 {
		t.Fatalf("the fixture produced %d orphan candidates, want exactly 1 (the managed element); "+
			"the hand-written twin must not be nominated, because no evidence records its value digest",
			len(file.Orphans))
	}
	return hooksPath, file
}

// TestAdversarialOrphanRemovalMustNotDeleteTheIdentityTwinTheUserDidNotConfirm is
// the headline finding of this review.
//
// The user is shown ONE orphan candidate, backed by a retained safety-backup
// manifest, and answers y to it. The entry that is deleted must be exactly the
// element that was described: the hand-written one, which has no evidence, was
// never nominated, was never previewed, and was never described to the user, must
// survive. With the old lossy hook/v1 identity two different values could share
// an identity digest, and removeDevinEntry's then-identity-only match deleted the
// first such element (the hand-written one) instead of the confirmed one.
//
// The total hook/v2 identity removes the root cause by making identity a
// function of the whole canonical value, but the value-digest check in
// removeDevinEntry is still required to remove exactly one of two byte-identical
// hooks and to fail closed if the described element is no longer present.
func TestAdversarialOrphanRemovalMustNotDeleteTheIdentityTwinTheUserDidNotConfirm(t *testing.T) {
	repoRoot, _ := devinEnv(t)
	hooksPath, file := prepareDevinHookIdentityTwins(t, repoRoot)

	confirmed := file.Orphans[0]
	if got := confirmed.Candidate.ValueDigest(); got == ownership.Digest([]byte(devinTwinHandElement)) {
		t.Fatalf("the fixture nominated the hand-written element (%s)", got)
	}

	if _, err := file.RemoveOrphan(0, true); err != nil {
		t.Fatalf("remove the confirmed orphan: %v", err)
	}

	after := readFileString(t, hooksPath)
	if !strings.Contains(after, devinTwinHandMarker) {
		t.Fatalf("a confirmed orphan removal DELETED THE WRONG HOOK ELEMENT.\n"+
			"The user was shown one candidate — the evidence-backed managed element, evidence %q — "+
			"and answered y to it. The element that disappeared is the hand-written element, which was "+
			"never nominated, never previewed, and never described. removeDevinEntry must therefore "+
			"still delete the byte-identical element that matches both the identity and the value digest, "+
			"not the hand-written one.\n"+
			"--- aggregate after the removal ---\n%s", confirmed.Evidence(), after)
	}
	if strings.Contains(after, devinTwinManagedElem) {
		t.Fatalf("the confirmed orphan candidate survived its own removal:\n%s", after)
	}
}

// TestAdversarialOrphanRemovalStillRemovesExactlyOneOfTwoIdenticalHooks is the
// guard on the fix for the test above.
//
// Two BYTE-IDENTICAL hook elements share both digests, so the user genuinely
// confirmed "one of these two". removeDevinEntry's existing rule — "exactly one
// element is removed per confirmation, even if the file happens to contain two
// byte-identical hooks" — must survive any tightening of the match, so it is
// pinned here rather than left to the fix's author to remember.
func TestAdversarialOrphanRemovalStillRemovesExactlyOneOfTwoIdenticalHooks(t *testing.T) {
	repoRoot, _ := devinEnv(t)
	root := devinProjectRoot(repoRoot)
	selection := []resolver.ResolvedArtifact{
		devinResolved("beta", ir.NewHook("beta", "PostToolUse", "echo beta", "command", nil, ir.SourceRef{}), false),
	}
	if _, err := Run(context.Background(), repoRoot, selection); err != nil {
		t.Fatalf("seed sync: %v", err)
	}
	hooksPath := filepath.Join(root, harness.DevinProjectHooksFile)
	body := "{\n  \"PostToolUse\": [\n    " + devinTwinManagedElem + ",\n    " + devinTwinManagedElem + "\n  ]\n}\n"
	if err := os.WriteFile(hooksPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	dropOwnershipState(t, root)

	preview, err := DevinRepairPreview(context.Background(), repoRoot, nil)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	file := repairFileFor(t, preview, harness.DevinProjectHooksFile)
	if len(file.Orphans) != 2 {
		t.Fatalf("orphan candidates = %d, want 2 (both elements match the evidence exactly)", len(file.Orphans))
	}
	if _, err := file.RemoveOrphan(0, true); err != nil {
		t.Fatalf("remove one of two identical hooks: %v", err)
	}
	if got := strings.Count(readFileString(t, hooksPath), `"echo beta"`); got != 1 {
		t.Fatalf("one confirmation removed %d of the two byte-identical hooks, want exactly 1:\n%s",
			got, readFileString(t, hooksPath))
	}
}

// TestAdversarialConflictReplaceIsNotFooledByAReorderedOrSwappedFile attacks the
// index-based confirmation from the other direction: the user confirms
// Conflicts[0], and before Apply runs a foreign writer REORDERS the members and
// SWAPS the two hand-written values.
//
// A preview slice is frozen at preview time and the re-derivation compares both
// digests by identity, so index 0 cannot come to mean the other entry. The swap
// must be detected (ErrRepairCandidateStale) and BOTH hand-written values must
// survive: the user consented to a described difference, not to whatever now
// occupies that key.
//
// This is a NEGATIVE result — the code is correct — and it is written down
// because "index into a re-derived list" is the classic shape of a wrong-entry
// deletion and a future refactor that re-derives f.Conflicts in place would make
// it fail.
func TestAdversarialConflictReplaceIsNotFooledByAReorderedOrSwappedFile(t *testing.T) {
	repoRoot, _ := devinEnv(t)
	path := writeDevinMcp(t, repoRoot, "{\n  \"mcpServers\": {\n"+
		"    \"aaa\": { \"command\": \"/hand/aaa\" },\n"+
		"    \"zzz\": { \"command\": \"/hand/zzz\" }\n  }\n}\n")
	selection := devinMcpSelection("aaa", "zzz")
	if _, err := Run(context.Background(), repoRoot, selection); err != nil {
		t.Fatalf("seed sync: %v", err)
	}
	_, file := conflictPreviewFor(t, repoRoot, selection)
	if file == nil || len(file.Conflicts) != 2 {
		t.Fatal("the fixture did not produce two conflict candidates")
	}
	if file.Conflicts[0].Identity() != "aaa" {
		t.Fatalf("candidate 0 is %q, want the sorted-first key; the rest of this test indexes on it",
			file.Conflicts[0].Identity())
	}

	// A foreign writer reorders the members AND swaps the two values.
	if err := os.WriteFile(path, []byte("{\n  \"mcpServers\": {\n"+
		"    \"zzz\": { \"command\": \"/hand/aaa\" },\n"+
		"    \"aaa\": { \"command\": \"/hand/zzz\" }\n  }\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := readFileString(t, path)

	if _, err := file.ReplaceConflict(0, true); !errors.Is(err, ErrRepairCandidateStale) {
		t.Fatalf("a replacement confirmed against one value was applied to another: err = %v, "+
			"want ErrRepairCandidateStale", err)
	}
	if got := readFileString(t, path); got != before {
		t.Fatalf("a refused replacement changed the aggregate:\n--- want ---\n%s\n--- got ---\n%s", before, got)
	}
}

// TestAdversarialASecondRepairCannotRedoACommittedReplacement is attack 2 of the
// review brief: two `doctor --repair` runs preview the same root, the first
// commits a confirmed replacement, and the second's user then answers y to a
// preview that is now out of date.
//
// The second action must be refused as stale rather than replace anything: after
// the first commit the entry is proven owned at the desired digest, so it is no
// longer a conflict candidate at all. Nothing may be destroyed twice, and the
// second run must not report success for an action it did not take.
func TestAdversarialASecondRepairCannotRedoACommittedReplacement(t *testing.T) {
	repoRoot, _ := devinEnv(t)
	path := writeDevinMcp(t, repoRoot, "{\n  \"mcpServers\": {\n"+
		"    \"aaa\": { \"command\": \"/hand/aaa\" }\n  }\n}\n")
	selection := devinMcpSelection("aaa")
	if _, err := Run(context.Background(), repoRoot, selection); err != nil {
		t.Fatalf("seed sync: %v", err)
	}
	_, first := conflictPreviewFor(t, repoRoot, selection)
	_, second := conflictPreviewFor(t, repoRoot, selection)
	if first == nil || second == nil || len(first.Conflicts) != 1 || len(second.Conflicts) != 1 {
		t.Fatal("the fixture did not produce one conflict candidate in each preview")
	}

	if _, err := first.ReplaceConflict(0, true); err != nil {
		t.Fatalf("the first confirmed replacement must succeed: %v", err)
	}
	committed := readFileString(t, path)

	if _, err := second.ReplaceConflict(0, true); !errors.Is(err, ErrRepairCandidateStale) {
		t.Fatalf("a second repair re-applied an action the first had already committed: err = %v, "+
			"want ErrRepairCandidateStale", err)
	}
	if got := readFileString(t, path); got != committed {
		t.Fatalf("the second repair rewrote the first one's committed bytes:\n--- want ---\n%s\n--- got ---\n%s",
			committed, got)
	}
}

// TestAdversarialAFailedRepairActionReleasesTheRootLock is attack 5 of the brief:
// wedge the machine.
//
// The lock is per canonical root and one of Devin's two roots is the
// machine-scope ~/.config/devin, so a mutator that returned without releasing
// would block every sync in every repository on the machine until the process
// exits. The reachable failure inside a section is an action that acquires and
// then fails: ownership.Apply refuses an unconfirmed destructive mutation with
// ErrConfirmationRequired, which returns an error out of the middle of the
// session.
//
// Afterwards the lock must be free to a FAIL-FAST probe — one non-blocking
// attempt, nothing timed — the Store must report depth zero, and the aggregate
// must be byte-identical: a refused removal writes nothing at all, not even a
// backup.
func TestAdversarialAFailedRepairActionReleasesTheRootLock(t *testing.T) {
	repoRoot, _ := devinEnv(t)
	root := devinProjectRoot(repoRoot)
	if _, err := Run(context.Background(), repoRoot, devinMcpSelection("managed")); err != nil {
		t.Fatalf("seed sync: %v", err)
	}
	dropOwnershipState(t, root)
	path := filepath.Join(root, harness.DevinMcpConfigFile)
	before := readFileString(t, path)

	preview, err := DevinRepairPreview(context.Background(), repoRoot, nil)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	file := repairFileFor(t, preview, harness.DevinMcpConfigFile)
	if len(file.Orphans) != 1 {
		t.Fatalf("orphan candidates = %d, want 1", len(file.Orphans))
	}

	if _, err := file.RemoveOrphan(0, false); !errors.Is(err, ownership.ErrConfirmationRequired) {
		t.Fatalf("an unconfirmed orphan removal: err = %v, want ownership.ErrConfirmationRequired", err)
	}
	if got := readFileString(t, path); got != before {
		t.Fatalf("an unconfirmed removal changed the aggregate:\n--- want ---\n%s\n--- got ---\n%s", before, got)
	}
	if depth := file.store.LockDepth(); depth != 0 {
		t.Fatalf("the repair store still holds %d lock levels after a failed action", depth)
	}

	// A separate Store, therefore a separate open file description: a flock
	// belongs to the description, so this contends for real.
	t.Setenv(ownership.LockTimeoutEnvVar, "0")
	probe, err := ownership.Open(root, ownership.WithLockTimeout(0))
	if err != nil {
		t.Fatal(err)
	}
	release, err := probe.Lock()
	if err != nil {
		t.Fatalf("a repair action that failed inside its section LEAKED the root lock: %v.\n"+
			"For the machine-scope root ~/.config/devin that blocks every sync in every repository "+
			"on the machine until the process exits.", err)
	}
	release()
}
