package ownership

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A hook entry may carry an optional version-1 source-hook fingerprint. The
// digest must round-trip through durable state and be reachable through the
// proof accessor so the legacy reader can compare it with a selected source.
func TestHookSourceDigestRoundTrip(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate(`{"hooks":{"PreToolUse":[]}}`)
	st := f.store()

	entry := hookEntry("PreToolUse", "deploy", `{"type":"command"}`)
	entry.SourceDigest = Digest([]byte("hook-source/v1 fingerprint material"))
	mustApply(t, st, Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(`{"hooks":{"PreToolUse":[]}}`), Entries: []Entry{entry}, Reason: ReasonSync})

	proof, err := st.Proof(f.rel)
	if err != nil {
		t.Fatalf("Proof: %v", err)
	}
	got, ok := proof.SourceDigest(entry.ID())
	if !ok || got != entry.SourceDigest {
		t.Fatalf("SourceDigest = %q, %v; want %q, true", got, ok, entry.SourceDigest)
	}
}

// State written by a client that predates the source-digest field stays
// readable; the absent digest simply proves nothing.
func TestHookSourceDigestAbsentInOldState(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate(`{"hooks":{"PreToolUse":[]}}`)
	st := f.store()

	entry := hookEntry("PreToolUse", "deploy", `{"type":"command"}`)
	mustApply(t, st, Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(`{"hooks":{"PreToolUse":[]}}`), Entries: []Entry{entry}, Reason: ReasonSync})

	// Simulate an older writer: strip the optional field entirely from disk.
	var rec Record
	data, err := os.ReadFile(st.StateFile())
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &rec); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "source_digest") {
		t.Fatal("state unexpectedly records a source digest for an entry that has none")
	}
	if _, err := st.Load(); err != nil {
		t.Fatalf("old state without source_digest must stay readable: %v", err)
	}
	proof, err := st.Proof(f.rel)
	if err != nil {
		t.Fatalf("Proof: %v", err)
	}
	if got, ok := proof.SourceDigest(entry.ID()); ok || got != "" {
		t.Fatalf("SourceDigest = %q, %v; want \"\", false for an entry with no digest", got, ok)
	}
}

// A source digest that is not a sha256 digest, or one attached to a non-hook
// entry, can never establish compatibility: writes are refused and corrupt
// state fails closed.
func TestHookSourceDigestValidation(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate(`{"hooks":{"PreToolUse":[]}}`)
	st := f.store()

	valid := Digest([]byte("fingerprint"))

	badMcp := mcpEntry("server", "res", `{"command":"x"}`)
	badMcp.SourceDigest = valid
	if _, err := st.Apply(Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(`{}`), Entries: []Entry{badMcp}, Reason: ReasonSync}); !errors.Is(err, ErrUnsafeStateValue) {
		t.Fatalf("mcp entry with source digest: err = %v, want ErrUnsafeStateValue", err)
	}

	for _, digest := range []string{"not-a-digest", "sha256:zzzz", strings.Repeat("a", 64)} {
		bad := hookEntry("PreToolUse", "res", `{"type":"command"}`)
		bad.SourceDigest = digest
		if _, err := st.Apply(Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(`{}`), Entries: []Entry{bad}, Reason: ReasonSync}); !errors.Is(err, ErrUnsafeStateValue) {
			t.Fatalf("source digest %q: err = %v, want ErrUnsafeStateValue", digest, err)
		}
	}

	// A corrupt on-disk digest must poison the whole record, not be skipped.
	good := hookEntry("PreToolUse", "res", `{"type":"command"}`)
	good.SourceDigest = valid
	mustApply(t, st, Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(`{"hooks":{"PreToolUse":[]}}`), Entries: []Entry{good}, Reason: ReasonSync})
	data, err := os.ReadFile(st.StateFile())
	if err != nil {
		t.Fatal(err)
	}
	corrupt := strings.Replace(string(data), valid, "sha256:not-hex", 1)
	if err := os.WriteFile(st.StateFile(), []byte(corrupt), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Load(); !errors.Is(err, ErrStateCorrupt) {
		t.Fatalf("corrupt source digest: err = %v, want ErrStateCorrupt", err)
	}
}

// The digest, never the underlying hook content, is what reaches state: a hook
// carrying a recognizable command/prompt/matcher must not leak it through the
// new field.
func TestSourceDigestStoresNoPlaintext(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate(`{"hooks":{"PreToolUse":[]}}`)
	st := f.store()

	entry := hookEntry("PreToolUse", "deploy", `{"matcher":"`+fakeMatcher+`","command":"`+fakeCommand+`","prompt":"`+fakePrompt+`"}`)
	entry.SourceDigest = Digest([]byte("definition bytes and support files"))
	mustApply(t, st, Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(`{"hooks":{"PreToolUse":[]}}`), Entries: []Entry{entry}, Reason: ReasonSync})

	data, err := os.ReadFile(st.StateFile())
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{fakeSecret, "leak-me", "rm -rf", "password"} {
		if strings.Contains(string(data), leak) {
			t.Fatalf("state contains hook plaintext %q", leak)
		}
	}
	// Walk the whole record dir too (journal may persist after Apply).
	root := filepath.Dir(st.StateFile())
	_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		if strings.Contains(string(b), fakeSecret) {
			t.Fatalf("%s contains hook plaintext", p)
		}
		return nil
	})
}
