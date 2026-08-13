package ownership

import (
	"encoding/json"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/jsonc"
)

// This file is the executable specification of the sequence D10 must call. It
// composes the two halves D9 delivers — the syntax-preserving patcher in
// pkg/harness-compilation/jsonc and the durable ownership store here — and
// asserts the plan's user-visible guarantee: "Handwritten aggregate values,
// associated comments, key order, and hook order survive reconciliation".

const handwrittenMcp = `{
  // our internal docs server, do not touch
  "mcpServers": {
    "docs": {
      "command": "docs-server",
      // staging only
      "args": ["--stage"]
    }
  },
  "unrelatedSetting": true
}
`

func TestSyncPreservesCommentsKeyOrderAndOwnership(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate(handwrittenMcp)
	st := f.store()

	// 1. Recover any interrupted generation before touching anything.
	if _, err := st.Recover(); err != nil {
		t.Fatalf("Recover: %v", err)
	}

	// 2. Parse the aggregate with the one sanctioned concrete-syntax parser.
	//    Comments are ignored by the tokenizer and duplicate keys are rejected in
	//    the same pass.
	doc, err := jsonc.Parse(f.rel, []byte(handwrittenMcp))
	if err != nil {
		t.Fatalf("jsonc.Parse: %v", err)
	}
	servers, ok := doc.Root().Member("mcpServers")
	if !ok {
		t.Fatal("mcpServers is missing")
	}

	// 3. Ask the durable store what is ours. Nothing is, on a first sync.
	proof, err := st.Proof(f.rel)
	if err == nil {
		t.Fatal("Proof returned no error for a root with no state")
	}
	existing := make([]EntryValue, 0, len(servers.Keys()))
	for _, key := range servers.Keys() {
		value, _ := servers.Member(key)
		existing = append(existing, mcpEV(key, string(value.Raw())))
	}
	desiredValue := `{"command":"generated"}`
	desired := []EntryValue{mcpEV("generated", desiredValue)}
	report := Classify(proof, existing, desired)
	if len(report.Unmanaged) != 1 || report.Unmanaged[0].Key != "docs" {
		t.Fatalf("unmanaged = %+v, want the handwritten docs entry", report.Unmanaged)
	}
	if len(report.Collisions) != 0 || len(report.Stale) != 0 {
		t.Fatalf("report = %+v, want no collisions and nothing prunable", report)
	}

	// 4. Patch, writing only our own entry.
	raw, err := json.Marshal(map[string]string{"command": "generated"})
	if err != nil {
		t.Fatal(err)
	}
	if !doc.SetMember(servers, "generated", raw) {
		t.Fatal("SetMember failed")
	}
	if !doc.Changed() {
		t.Fatal("Changed() = false after a patch")
	}

	// 5. Apply the patched bytes as one journaled generation.
	entry := mcpEntry("generated", "generated-server", string(raw))
	res := mustApply(t, st, Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: doc.Bytes(),
		Entries: []Entry{entry}, Reason: ReasonSync})
	if res.Generation != 1 {
		t.Fatalf("generation = %d, want 1", res.Generation)
	}

	want := `{
  // our internal docs server, do not touch
  "mcpServers": {
    "docs": {
      "command": "docs-server",
      // staging only
      "args": ["--stage"]
    },
    "generated": {"command":"generated"}
  },
  "unrelatedSetting": true
}
`
	got, exists := f.readAggregate()
	if !exists {
		t.Fatal("the aggregate is missing")
	}
	if got != want {
		t.Fatalf("aggregate =\n%s\nwant\n%s", got, want)
	}

	// A second identical reconciliation is a no-op: the patcher reports no
	// change, so no generation, no backup, and no rewrite happen.
	doc2, err := jsonc.Parse(f.rel, []byte(got))
	if err != nil {
		t.Fatalf("jsonc.Parse: %v", err)
	}
	servers2, _ := doc2.Root().Member("mcpServers")
	doc2.SetMember(servers2, "generated", raw)
	if doc2.Changed() {
		t.Fatal("an idempotent reconciliation reported a change")
	}

	// And the entry is now proven owned and clean.
	proof, err = st.Proof(f.rel)
	if err != nil {
		t.Fatalf("Proof: %v", err)
	}
	if status := proof.Classify(entry.ID(), entry.ValueDigest); status != StatusOwnedClean {
		t.Fatalf("status = %v, want owned-clean", status)
	}
}

const handwrittenHooks = `{
  "PreToolUse": [
    // first handwritten hook: must keep running first
    {"type": "command", "command": "first.sh"},
    // second handwritten hook
    {"type": "command", "command": "second.sh"}
  ]
}
`

// "Handwritten hooks keep their relative order and execute before generated
// hooks. Generated hooks use deterministic resource ordering after handwritten
// hooks."
func TestHandwrittenHookOrderSurvivesAndGeneratedHooksFollow(t *testing.T) {
	f := newFixture(t)
	rel := "hooks.v1.json"
	abs := f.targetRoot + "/" + rel
	if err := writeFile(abs, handwrittenHooks); err != nil {
		t.Fatal(err)
	}
	st := f.store()

	doc, err := jsonc.Parse(rel, []byte(handwrittenHooks))
	if err != nil {
		t.Fatalf("jsonc.Parse: %v", err)
	}
	arr, ok := doc.Root().Member("PreToolUse")
	if !ok {
		t.Fatal("PreToolUse is missing")
	}

	// Preserve every existing element, in order, then append the generated ones
	// in deterministic resource order.
	raws := make([][]byte, 0, arr.Len()+2)
	for i := 0; i < arr.Len(); i++ {
		el, _ := arr.Element(i)
		raws = append(raws, el.Raw())
	}
	raws = append(raws, []byte(`{"command":"generated-a.sh","type":"command"}`))
	raws = append(raws, []byte(`{"command":"generated-b.sh","type":"command"}`))
	if !doc.SetElements(arr, raws) {
		t.Fatal("SetElements failed")
	}

	entries := []Entry{
		hookEntry("PreToolUse", "hook-a", `{"command":"generated-a.sh","type":"command"}`),
		hookEntry("PreToolUse", "hook-b", `{"command":"generated-b.sh","type":"command"}`),
	}
	mustApply(t, st, Mutation{Rel: rel, AbsFile: abs, NextBytes: doc.Bytes(), Entries: entries, Reason: ReasonSync})

	want := `{
  "PreToolUse": [
    // first handwritten hook: must keep running first
    {"type": "command", "command": "first.sh"},
    // second handwritten hook
    {"type": "command", "command": "second.sh"},
    {"command":"generated-a.sh","type":"command"},
    {"command":"generated-b.sh","type":"command"}
  ]
}
`
	got, err := readFile(abs)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("hooks file =\n%s\nwant\n%s", got, want)
	}

	// The two generated hooks have distinct identities, so neither collapses onto
	// the other and both are individually prunable later.
	if entries[0].IdentityDigest == entries[1].IdentityDigest {
		t.Fatal("two different generated hooks share one identity digest")
	}
	proof, err := st.Proof(rel)
	if err != nil {
		t.Fatal(err)
	}
	if proof.OwnedCount() != 2 {
		t.Fatalf("owned count = %d, want 2", proof.OwnedCount())
	}
	// A handwritten hook is never owned: it has no recorded identity at all.
	handwritten := `{"command":"first.sh","type":"command"}`
	unowned := EntryID{Kind: KindHook, Container: "PreToolUse", IdentityDigest: HookIdentityDigest("PreToolUse", Digest([]byte(handwritten)))}
	if status := proof.Classify(unowned, Digest([]byte(handwritten))); status != StatusUnmanaged {
		t.Fatalf("handwritten hook status = %v, want unmanaged", status)
	}
}
