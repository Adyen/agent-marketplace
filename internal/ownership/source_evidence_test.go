package ownership

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

// A native writer that emits one canonical native entry for several selected
// sources records each contributing (resource, source fingerprint) pair on
// that entry: the first pair stays in the scalar fields, the rest land in
// source_evidence. HasHookSource must match either form exactly and prove
// nothing else.
// spec: hook-native-location-tolerance/several-selected-sources-share-one-clean-native-entry
// spec: hook-native-location-tolerance/scalar-source-evidence-remains-compatible
func TestHookSourceEvidenceRoundTrip(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate(`{"hooks":{"PreToolUse":[]}}`)
	st := f.store()

	fpA := Digest([]byte("alpha fingerprint material"))
	fpB := Digest([]byte("beta fingerprint material"))

	entry := hookEntry("PreToolUse", "alpha", `{"type":"command"}`)
	entry.SourceDigest = fpA
	entry.SourceEvidence = []HookSourceEvidence{{Resource: "beta", SourceDigest: fpB}}
	mustApply(t, st, Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(`{"hooks":{"PreToolUse":[]}}`), Entries: []Entry{entry}, Reason: ReasonSync})

	proof, err := st.Proof(f.rel)
	if err != nil {
		t.Fatalf("Proof: %v", err)
	}
	for _, tc := range []struct {
		resource, digest string
		want             bool
	}{
		{"alpha", fpA, true},  // scalar pair
		{"beta", fpB, true},   // evidence tuple
		{"alpha", fpB, false}, // cross pairs never match
		{"beta", fpA, false},
		{"gamma", fpA, false}, // unknown resource
		{"alpha", Digest([]byte("other")), false},
		{"alpha", "", false}, // absent digest proves nothing
	} {
		if got := proof.HasHookSource(entry.ID(), tc.resource, tc.digest); got != tc.want {
			t.Fatalf("HasHookSource(%q, %q) = %v, want %v", tc.resource, tc.digest, got, tc.want)
		}
	}
	var zero Proof
	if zero.HasHookSource(entry.ID(), "alpha", fpA) {
		t.Fatal("zero Proof must not prove a source")
	}

	// Scalar-only state written by an older reader stays readable and matches
	// exactly as before; reading it must not add tuples.
	f2 := newFixture(t)
	f2.writeAggregate(`{"hooks":{"PreToolUse":[]}}`)
	st2 := f2.store()
	old := hookEntry("PreToolUse", "alpha", `{"type":"command"}`)
	old.SourceDigest = fpA
	mustApply(t, st2, Mutation{Rel: f2.rel, AbsFile: f2.absFile, NextBytes: []byte(`{"hooks":{"PreToolUse":[]}}`), Entries: []Entry{old}, Reason: ReasonSync})
	proof2, err := st2.Proof(f2.rel)
	if err != nil {
		t.Fatalf("Proof: %v", err)
	}
	if !proof2.HasHookSource(old.ID(), "alpha", fpA) {
		t.Fatal("scalar evidence on a tuple-free entry no longer satisfies")
	}
	if proof2.HasHookSource(old.ID(), "beta", fpB) {
		t.Fatal("scalar-only entry proved an unrecorded contributor")
	}
	raw, err := os.ReadFile(st2.StateFile())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "source_evidence") {
		t.Fatal("reading scalar evidence rewrote state with tuples")
	}

	// An entry with no source evidence at all proves no source.
	f3 := newFixture(t)
	f3.writeAggregate(`{"hooks":{"PreToolUse":[]}}`)
	st3 := f3.store()
	plain := hookEntry("PreToolUse", "alpha", `{"type":"command"}`)
	mustApply(t, st3, Mutation{Rel: f3.rel, AbsFile: f3.absFile, NextBytes: []byte(`{"hooks":{"PreToolUse":[]}}`), Entries: []Entry{plain}, Reason: ReasonSync})
	proof3, err := st3.Proof(f3.rel)
	if err != nil {
		t.Fatalf("Proof: %v", err)
	}
	if proof3.HasHookSource(plain.ID(), "alpha", fpA) {
		t.Fatal("entry without any source evidence proved a source")
	}
	if proof3.HasHookSource(plain.ID(), "alpha", "") {
		t.Fatal("absent digest must never prove a source")
	}
}

// fileSnapshot returns a file's bytes and its stat record for byte/mode/mtime
// comparisons across an operation.
func fileSnapshot(t *testing.T, path string) ([]byte, os.FileInfo) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data, info
}

// writeRawState replaces the store's durable record with verbatim JSON so
// tests can seed shapes Apply would refuse to write (duplicates, aliases,
// wrong member types).
func writeRawState(t *testing.T, st *Store, rel, entriesJSON string) {
	t.Helper()
	writeRawFileState(t, st, rel, `{"content_digest":"","entries":`+entriesJSON+`}`)
}

func writeRawFileState(t *testing.T, st *Store, rel, fileStateJSON string) {
	t.Helper()
	rec := fmt.Sprintf(`{"version":1,"canonical_root":%q,"root_key":%q,"generation":1,"updated_at":"2026-10-01T00:00:00Z","files":{%q:%s}}`,
		st.CanonicalRoot(), st.RootKey(), rel, fileStateJSON)
	if err := os.MkdirAll(filepath.Dir(st.StateFile()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(st.StateFile(), []byte(rec), 0o600); err != nil {
		t.Fatal(err)
	}
}

// hookEntryJSON renders one entry's JSON member, optionally with extra
// verbatim text appended inside the object so duplicate keys and case aliases
// can be expressed.
func hookEntryJSON(t *testing.T, e Entry, extra string) string {
	t.Helper()
	data, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	if extra == "" {
		return string(data)
	}
	return string(data[:len(data)-1]) + "," + extra + "}"
}

// Malformed, wrongly-typed, misplaced, or ambiguous source evidence can never
// establish proof: writes carrying invalid tuples are refused, and corrupt
// on-disk evidence poisons the whole record with the ownership.state-corrupt
// coded error, leaks no rejected value, and writes nothing.
// spec: hook-native-location-tolerance/invalid-additional-evidence-refuses-without-mutation
func TestInvalidHookSourceEvidence(t *testing.T) {
	fpA := Digest([]byte("alpha fingerprint material"))

	// Apply-time refusals: every invalid tuple shape is rejected before any
	// state lands on disk, and the aggregate is untouched.
	t.Run("apply_refuses_invalid_tuples", func(t *testing.T) {
		for name, mutate := range map[string]func(*Entry){
			"evidence_on_mcp": func(e *Entry) {
				*e = mcpEntry("server", "res", `{"command":"x"}`)
				e.SourceEvidence = []HookSourceEvidence{{Resource: "beta", SourceDigest: fpA}}
			},
			"empty_resource": func(e *Entry) {
				e.SourceEvidence = []HookSourceEvidence{{Resource: "", SourceDigest: fpA}}
			},
			"unsafe_resource": func(e *Entry) {
				e.SourceEvidence = []HookSourceEvidence{{Resource: fakeCommand, SourceDigest: fpA}}
			},
			"empty_digest": func(e *Entry) {
				e.SourceEvidence = []HookSourceEvidence{{Resource: "beta", SourceDigest: ""}}
			},
			"malformed_digest": func(e *Entry) {
				e.SourceEvidence = []HookSourceEvidence{{Resource: "beta", SourceDigest: "sha256:zz"}}
			},
			"second_tuple_bad": func(e *Entry) {
				e.SourceEvidence = []HookSourceEvidence{
					{Resource: "beta", SourceDigest: fpA},
					{Resource: "gamma", SourceDigest: "not-a-digest"},
				}
			},
		} {
			t.Run(name, func(t *testing.T) {
				f := newFixture(t)
				f.writeAggregate(`{"hooks":{"PreToolUse":[]}}`)
				st := f.store()
				e := hookEntry("PreToolUse", "alpha", `{"type":"command"}`)
				mutate(&e)
				_, err := st.Apply(Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(`{"hooks":{"PreToolUse":[{"type":"command"}]}}`), Entries: []Entry{e}, Reason: ReasonSync})
				if !errors.Is(err, ErrUnsafeStateValue) {
					t.Fatalf("err = %v, want ErrUnsafeStateValue", err)
				}
				if strings.Contains(err.Error(), fakeCommand) {
					t.Fatal("error leaks a rejected value")
				}
				if body, _ := f.readAggregate(); body != `{"hooks":{"PreToolUse":[]}}` {
					t.Fatalf("aggregate changed under refused mutation: %q", body)
				}
				if _, statErr := os.Stat(st.StateFile()); !os.IsNotExist(statErr) {
					t.Fatal("refused mutation wrote a state file")
				}
			})
		}
	})

	// On-disk corruption: each raw shape must fail Load with exactly the
	// ownership.state-corrupt coded error that wraps ErrStateCorrupt, carry no
	// rejected value, and leave record and aggregate bytes plus metadata
	// untouched.
	validTuple := `"source_evidence":[{"resource":"beta","source_digest":"` + fpA + `"}]`
	cases := map[string]func(t *testing.T, e Entry) string{
		"tuple_on_mcp": func(t *testing.T, e Entry) string {
			mcp := mcpEntry("server", "res", `{"command":"x"}`)
			return "[" + hookEntryJSON(t, mcp, validTuple) + "]"
		},
		"empty_resource": func(t *testing.T, e Entry) string {
			return "[" + hookEntryJSON(t, e, `"source_evidence":[{"resource":"","source_digest":"`+fpA+`"}]`) + "]"
		},
		"unsafe_resource": func(t *testing.T, e Entry) string {
			return "[" + hookEntryJSON(t, e, `"source_evidence":[{"resource":`+fmt.Sprintf("%q", fakeCommand)+`,"source_digest":"`+fpA+`"}]`) + "]"
		},
		"malformed_digest": func(t *testing.T, e Entry) string {
			return "[" + hookEntryJSON(t, e, `"source_evidence":[{"resource":"beta","source_digest":"sha256:zz"}]`) + "]"
		},
		"absent_digest": func(t *testing.T, e Entry) string {
			return "[" + hookEntryJSON(t, e, `"source_evidence":[{"resource":"beta"}]`) + "]"
		},
		"evidence_not_array": func(t *testing.T, e Entry) string {
			return "[" + hookEntryJSON(t, e, `"source_evidence":"beta"`) + "]"
		},
		"tuple_member_wrong_type": func(t *testing.T, e Entry) string {
			return "[" + hookEntryJSON(t, e, `"source_evidence":[{"resource":5,"source_digest":"`+fpA+`"}]`) + "]"
		},
		"tuple_not_object": func(t *testing.T, e Entry) string {
			return "[" + hookEntryJSON(t, e, `"source_evidence":["beta"]`) + "]"
		},
		"literal_duplicate_member": func(t *testing.T, e Entry) string {
			return "[" + hookEntryJSON(t, e, validTuple+","+validTuple) + "]"
		},
		"case_alias_member": func(t *testing.T, e Entry) string {
			return "[" + hookEntryJSON(t, e, validTuple+`,"Source_Evidence":[]`) + "]"
		},
		"case_alias_member_reversed": func(t *testing.T, e Entry) string {
			return "[" + hookEntryJSON(t, e, `"Source_Evidence":[],`+validTuple) + "]"
		},
		"nested_resource_alias": func(t *testing.T, e Entry) string {
			return "[" + hookEntryJSON(t, e, `"source_evidence":[{"resource":"beta","Resource":"gamma","source_digest":"`+fpA+`"}]`) + "]"
		},
		"nested_resource_alias_reversed": func(t *testing.T, e Entry) string {
			return "[" + hookEntryJSON(t, e, `"source_evidence":[{"Resource":"beta","resource":"gamma","source_digest":"`+fpA+`"}]`) + "]"
		},
		"nested_digest_alias": func(t *testing.T, e Entry) string {
			return "[" + hookEntryJSON(t, e, `"source_evidence":[{"resource":"beta","source_digest":"`+fpA+`","Source_Digest":"`+fpA+`"}]`) + "]"
		},
		"nested_digest_alias_reversed": func(t *testing.T, e Entry) string {
			return "[" + hookEntryJSON(t, e, `"source_evidence":[{"resource":"beta","Source_Digest":"`+fpA+`","source_digest":"`+fpA+`"}]`) + "]"
		},
		"nested_alias_empty_typed": func(t *testing.T, e Entry) string {
			return "[" + hookEntryJSON(t, e, `"source_evidence":[{"resource":"","Resource":"beta","source_digest":"`+fpA+`"}]`) + "]"
		},
		"nested_alias_empty_typed_reversed": func(t *testing.T, e Entry) string {
			return "[" + hookEntryJSON(t, e, `"source_evidence":[{"Resource":"beta","resource":"","source_digest":"`+fpA+`"}]`) + "]"
		},
	}
	for name, entries := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.writeAggregate(`{"hooks":{"PreToolUse":[]}}`)
			st := f.store()
			e := hookEntry("PreToolUse", "alpha", `{"type":"command"}`)
			e.SourceDigest = fpA
			writeRawState(t, st, f.rel, entries(t, e))
			stateBefore, stateInfo := fileSnapshot(t, st.StateFile())

			rec, err := st.Load()
			if rec != nil {
				t.Fatalf("corrupt evidence returned a record: %+v", rec)
			}
			if err == nil {
				t.Fatal("corrupt evidence loaded cleanly")
			}
			coded, ok := diag.AsCodedError(err)
			if !ok {
				t.Fatalf("error %T is not a *diag.CodedError", err)
			}
			if coded.Code != diag.CodeOwnershipStateCorrupt {
				t.Fatalf("code = %q, want %q", coded.Code, diag.CodeOwnershipStateCorrupt)
			}
			if !errors.Is(err, ErrStateCorrupt) {
				t.Fatalf("error %v does not wrap ErrStateCorrupt", err)
			}
			if strings.Contains(err.Error(), fakeCommand) {
				t.Fatal("error leaks a rejected value")
			}

			stateAfter, stateInfoAfter := fileSnapshot(t, st.StateFile())
			if string(stateAfter) != string(stateBefore) || stateInfoAfter.Mode() != stateInfo.Mode() || !stateInfoAfter.ModTime().Equal(stateInfo.ModTime()) {
				t.Fatal("refused Load changed the state file bytes, mode, or mtime")
			}
			if body, _ := f.readAggregate(); body != `{"hooks":{"PreToolUse":[]}}` {
				t.Fatalf("aggregate changed: %q", body)
			}
		})
	}

	// Valid controls: a well-formed tuple loads, and a singleton case variant
	// of the member name is consumed by the same case-insensitive field match
	// without ambiguity.
	for name, extra := range map[string]string{
		"valid_tuple":           validTuple,
		"valid_singleton_alias": `"Source_Evidence":[{"resource":"beta","source_digest":"` + fpA + `"}]`,
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.writeAggregate(`{"hooks":{"PreToolUse":[]}}`)
			st := f.store()
			e := hookEntry("PreToolUse", "alpha", `{"type":"command"}`)
			e.SourceDigest = fpA
			writeRawState(t, st, f.rel, "["+hookEntryJSON(t, e, extra)+"]")
			if _, err := st.Load(); err != nil {
				t.Fatalf("valid evidence refused: %v", err)
			}
			proof, err := st.Proof(f.rel)
			if err != nil {
				t.Fatalf("Proof: %v", err)
			}
			if !proof.HasHookSource(e.ID(), "beta", fpA) {
				t.Fatal("valid tuple not visible through proof")
			}
		})
	}
}
