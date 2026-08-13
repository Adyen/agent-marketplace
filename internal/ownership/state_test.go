package ownership

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

// Missing state must fail CLOSED: nothing is owned, so nothing can be pruned or
// replaced. "Missing or corrupt ownership state makes all existing aggregate
// entries unmanaged."
func TestMissingStateFailsClosed(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate(priorContent)
	st := f.store()

	rec, err := st.Load()
	if rec != nil {
		t.Fatalf("Load returned a record for a root with no state: %+v", rec)
	}
	coded, ok := diag.AsCodedError(err)
	if !ok {
		t.Fatalf("error %T is not a *diag.CodedError", err)
	}
	if coded.Code != diag.CodeOwnershipStateMissing {
		t.Fatalf("code = %q, want %q", coded.Code, diag.CodeOwnershipStateMissing)
	}
	if coded.Severity != diag.SeverityWarning {
		t.Fatalf("severity = %v, want warning: missing state is recoverable and must not fail a sync outright", coded.Severity)
	}
	if !errors.Is(err, ErrStateMissing) {
		t.Fatalf("error = %v, want ErrStateMissing", err)
	}

	proof, _ := st.Proof(f.rel)
	if proof.OwnedCount() != 0 {
		t.Fatalf("owned count = %d, want 0", proof.OwnedCount())
	}
	entry := mcpEntry("handwritten", "some-resource", `{"command":"keep"}`)
	if status := proof.Classify(entry.ID(), entry.ValueDigest); status != StatusUnmanaged {
		t.Fatalf("status = %v, want unmanaged", status)
	}

	// And the drift report must nominate nothing for pruning.
	existing := []EntryValue{{ID: entry.ID(), ValueDigest: entry.ValueDigest}}
	report := Classify(proof, existing, nil)
	if len(report.Stale) != 0 {
		t.Fatalf("stale = %v, want nothing prunable without proof", report.Stale)
	}
	if len(report.Unmanaged) != 1 {
		t.Fatalf("unmanaged = %v, want the existing entry", report.Unmanaged)
	}
}

func TestCorruptStateFailsClosed(t *testing.T) {
	tests := []struct {
		name     string
		mutate   func(t *testing.T, st *Store, rec *Record)
		wantCode string
	}{
		{
			name: "malformed json",
			mutate: func(t *testing.T, st *Store, _ *Record) {
				if err := os.WriteFile(st.StateFile(), []byte("{not json"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
			wantCode: diag.CodeOwnershipStateCorrupt,
		},
		{
			name: "unknown version",
			mutate: func(t *testing.T, st *Store, rec *Record) {
				rec.Version = 99
				writeRecord(t, st, rec)
			},
			wantCode: diag.CodeOwnershipStateCorrupt,
		},
		{
			name: "root key rewritten",
			mutate: func(t *testing.T, st *Store, rec *Record) {
				rec.RootKey = RootKey("/somewhere/else")
				writeRecord(t, st, rec)
			},
			wantCode: diag.CodeOwnershipStateCorrupt,
		},
		{
			name: "canonical root belongs to another root",
			mutate: func(t *testing.T, st *Store, rec *Record) {
				rec.CanonicalRoot = "/somewhere/else"
				writeRecord(t, st, rec)
			},
			wantCode: diag.CodeOwnershipStateRootMismatch,
		},
		{
			name: "entry digest is not a digest",
			mutate: func(t *testing.T, st *Store, rec *Record) {
				fs := rec.Files["mcp_config.json"]
				fs.Entries[0].ValueDigest = "not-a-digest"
				rec.Files["mcp_config.json"] = fs
				writeRecord(t, st, rec)
			},
			wantCode: diag.CodeOwnershipStateCorrupt,
		},
		{
			name: "file key is absolute",
			mutate: func(t *testing.T, st *Store, rec *Record) {
				rec.Files["/etc/passwd"] = rec.Files["mcp_config.json"]
				writeRecord(t, st, rec)
			},
			wantCode: diag.CodeOwnershipStateCorrupt,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			f.writeAggregate(priorContent)
			st := f.store()
			entry := mcpEntry("generated", "generated-server", `{"command":"new"}`)
			mustApply(t, st, Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(nextContent), Entries: []Entry{entry}, Reason: ReasonSync})

			rec, err := st.Load()
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			tt.mutate(t, st, rec)

			got, err := st.Load()
			if got != nil {
				t.Fatal("Load returned a record for corrupt state")
			}
			coded, ok := diag.AsCodedError(err)
			if !ok {
				t.Fatalf("error %T is not a *diag.CodedError", err)
			}
			if coded.Code != tt.wantCode {
				t.Fatalf("code = %q, want %q", coded.Code, tt.wantCode)
			}
			if coded.Severity != diag.SeverityError {
				t.Fatalf("severity = %v, want error", coded.Severity)
			}
			if coded.Source.File != st.StateFile() {
				t.Fatalf("source file = %q, want %q", coded.Source.File, st.StateFile())
			}

			// Fail closed: the previously owned entry is now unmanaged.
			proof, _ := st.Proof(f.rel)
			if status := proof.Classify(entry.ID(), entry.ValueDigest); status != StatusUnmanaged {
				t.Fatalf("status = %v, want unmanaged after corrupt state", status)
			}
		})
	}
}

// A root-mismatched record must never be silently overwritten by a mutation.
func TestApplyRefusesToOverwriteAnotherRootsRecord(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate(priorContent)
	st := f.store()
	mustApply(t, st, Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(nextContent),
		Entries: []Entry{mcpEntry("generated", "g", `{"command":"new"}`)}, Reason: ReasonSync})

	rec, err := st.Load()
	if err != nil {
		t.Fatal(err)
	}
	rec.CanonicalRoot = "/somewhere/else"
	writeRecord(t, st, rec)

	_, err = st.Apply(Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(priorContent), Reason: ReasonSync})
	if !errors.Is(err, ErrStateRootMismatch) {
		t.Fatalf("Apply error = %v, want ErrStateRootMismatch", err)
	}
}

// The cache is not authoritative: "Cache deletion has no ownership effect."
func TestCacheDeletionIsHarmless(t *testing.T) {
	// Isolate HOME: this test deletes the cache tree, and the real one must
	// never be a candidate.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())

	f := newFixture(t)
	f.writeAggregate(priorContent)
	st := f.store()
	entry := mcpEntry("generated", "generated-server", `{"command":"new"}`)
	mustApply(t, st, Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(nextContent), Entries: []Entry{entry}, Reason: ReasonSync})

	// Populate and then delete the registry cache, which lives under HOME and is
	// completely disjoint from the XDG state root the store uses.
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	cache := filepath.Join(home, ".cache", "agent-marketplace")
	if err := os.MkdirAll(filepath.Join(cache, "some-registry", "main"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(cache); err != nil {
		t.Fatal(err)
	}

	proof, err := f.store().Proof(f.rel)
	if err != nil {
		t.Fatalf("Proof after cache deletion: %v", err)
	}
	if status := proof.Classify(entry.ID(), entry.ValueDigest); status != StatusOwnedClean {
		t.Fatalf("status = %v, want owned-clean: cache deletion must not affect ownership", status)
	}
	if proof.Generation() != 1 {
		t.Fatalf("generation = %d, want 1", proof.Generation())
	}
	// And nothing the store touches lives under the cache root.
	if strings.HasPrefix(st.StateRoot(), cache) {
		t.Fatalf("state root %q lives under the cache root %q", st.StateRoot(), cache)
	}
}

// The state-side equivalent of TestMcpDiagnosticsNeverQuoteValues and
// TestHookDiagnosticsNeverQuotePayloads: no sensitive value may reach ownership
// state, the journal, or backup metadata. "Secret values must never appear in
// structured logs, diagnostics, ownership state, journals, or backup metadata."
func TestNoSensitiveValueEntersStateJournalOrBackupMetadata(t *testing.T) {
	f := newFixture(t)
	// The aggregate itself is full of sensitive values, exactly as a real one is.
	prior := `{"mcpServers":{"docs":{"command":"` + fakeCommand + `","headers":{"Authorization":"` + fakeHeader + `"}}}}`
	next := `{"mcpServers":{"docs":{"command":"` + fakeCommand + `"}},"hooks":{"PreToolUse":[{"matcher":"` + fakeMatcher + `","prompt":"` + fakePrompt + `"}]}}`
	f.writeAggregate(prior)

	entries := []Entry{
		mcpEntry("docs", "docs-server", `{"command":"`+fakeCommand+`"}`),
		hookEntry("PreToolUse", "guard-hook", `{"matcher":"`+fakeMatcher+`","prompt":"`+fakePrompt+`"}`),
	}

	// Crash after the journal is written so the journal is on disk to inspect,
	// then complete a real generation so state and backup metadata exist too.
	crashing := f.store(WithCrasher(CrashAt(CrashAfterJournalPrepared)))
	if _, err := crashing.Apply(Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(next), Entries: entries, Reason: ReasonSync}); !errors.Is(err, ErrInjectedCrash) {
		t.Fatalf("Apply error = %v, want an injected crash", err)
	}
	journalBytes, err := os.ReadFile(crashing.JournalFile())
	if err != nil {
		t.Fatal(err)
	}
	st := f.store()
	if _, err := st.Recover(); err != nil {
		t.Fatalf("Recover: %v", err)
	}
	mustApply(t, st, Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(next), Entries: entries, Reason: ReasonSync})

	stateBytes, err := os.ReadFile(st.StateFile())
	if err != nil {
		t.Fatal(err)
	}
	backups, err := st.SafetyBackups(f.rel)
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) == 0 {
		t.Fatal("no safety backups to inspect")
	}
	metaBytes, err := os.ReadFile(metaPathFor(backups[len(backups)-1].Path))
	if err != nil {
		t.Fatal(err)
	}

	forbidden := []string{fakeSecret, fakeCommand, fakePrompt, fakeMatcher, fakeHeader, "Authorization", "leak-me"}
	for _, artifact := range []struct {
		name  string
		bytes []byte
	}{
		{"ownership state", stateBytes},
		{"journal", journalBytes},
		{"backup metadata", metaBytes},
	} {
		for _, secret := range forbidden {
			if strings.Contains(string(artifact.bytes), secret) {
				t.Fatalf("%s leaks %q:\n%s", artifact.name, secret, artifact.bytes)
			}
		}
	}

	// Backup FILENAMES must not expose values either.
	for _, b := range backups {
		for _, secret := range forbidden {
			if strings.Contains(b.Path, secret) {
				t.Fatalf("backup path %q leaks %q", b.Path, secret)
			}
		}
	}

	// The non-secret identities that ARE recorded must still be present, or the
	// test above would pass vacuously.
	for _, want := range []string{"mcpServers", "docs", "PreToolUse", "docs-server", "guard-hook", "sha256:"} {
		if !strings.Contains(string(stateBytes), want) {
			t.Fatalf("ownership state is missing the non-secret identity %q:\n%s", want, stateBytes)
		}
	}
}

// The Entry type has nowhere to put a value, and validateEntry rejects anything
// that is not a non-secret identity.
func TestValidateEntryRefusesUnsafeValues(t *testing.T) {
	valid := mcpEntry("docs", "docs-server", "{}")
	tests := []struct {
		name  string
		entry Entry
	}{
		{"command as key", func() Entry { e := valid; e.Key = fakeCommand; return e }()},
		{"prompt as container", func() Entry { e := valid; e.Container = fakePrompt; return e }()},
		{"matcher as resource", func() Entry { e := valid; e.Resource = fakeMatcher; return e }()},
		{"raw value as identity digest", func() Entry { e := valid; e.IdentityDigest = fakeSecret; return e }()},
		{"raw value as value digest", func() Entry { e := valid; e.ValueDigest = fakeSecret; return e }()},
		{"empty key on an mcp entry", func() Entry { e := valid; e.Key = ""; return e }()},
		{"key on a hook entry", func() Entry { e := valid; e.Kind = KindHook; return e }()},
		{"unknown kind", func() Entry { e := valid; e.Kind = EntryKind("rule"); return e }()},
		{"overlong key", func() Entry { e := valid; e.Key = strings.Repeat("a", 200); return e }()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateEntry(tt.entry)
			if !errors.Is(err, ErrUnsafeStateValue) {
				t.Fatalf("validateEntry error = %v, want ErrUnsafeStateValue", err)
			}
			// The refusal itself must not echo the value it refused.
			for _, secret := range []string{fakeSecret, fakeCommand, fakePrompt, fakeMatcher} {
				if strings.Contains(err.Error(), secret) {
					t.Fatalf("error %q echoes the refused value", err.Error())
				}
			}
		})
	}
	if err := validateEntry(valid); err != nil {
		t.Fatalf("validateEntry rejected a valid entry: %v", err)
	}
	validHook := hookEntry("PreToolUse", "guard", `{}`)
	if err := validateEntry(validHook); err != nil {
		t.Fatalf("validateEntry rejected a valid hook entry: %v", err)
	}
}

func TestApplyRefusesAnUnsafeEntry(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate(priorContent)
	_, err := f.store().Apply(Mutation{
		Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(nextContent),
		Entries: []Entry{{Kind: KindMcpServer, Container: "mcpServers", Key: fakeCommand,
			IdentityDigest: Digest(nil), ValueDigest: Digest(nil), Resource: "r"}},
		Reason: ReasonSync,
	})
	coded, ok := diag.AsCodedError(err)
	if !ok || coded.Code != diag.CodeOwnershipStateUnsafeValue {
		t.Fatalf("Apply error = %v, want code %q", err, diag.CodeOwnershipStateUnsafeValue)
	}
	if _, exists := f.readAggregate(); !exists {
		t.Fatal("the aggregate disappeared")
	}
	if got, _ := f.readAggregate(); got != priorContent {
		t.Fatalf("aggregate = %q, want it untouched", got)
	}
}

// A hook's identity is derived from the event name and the digest of the whole
// canonical value, so any change to either produces a different identity.
func TestHookIdentityDigestIsEventAndValueSensitiveAndLengthPrefixed(t *testing.T) {
	baseEvent := "PreToolUse"
	baseValue := `{"command":"echo hi","matcher":"Bash","timeout":"30","type":"command"}`
	baseValueDigest := Digest([]byte(baseValue))
	baseDigest := HookIdentityDigest(baseEvent, baseValueDigest)

	variants := map[string]string{
		"event":       "PostToolUse",
		"value":       `{"command":"echo hi","matcher":"Bash","timeout":"30","type":"prompt"}`,
		"extra field": `{"command":"echo hi","matcher":"Bash","timeout":"30","type":"command","x":1}`,
	}
	for name, v := range variants {
		var changed string
		switch name {
		case "event":
			changed = HookIdentityDigest(v, baseValueDigest)
		default:
			changed = HookIdentityDigest(baseEvent, Digest([]byte(v)))
		}
		if changed == baseDigest {
			t.Fatalf("changing %s did not change the identity digest", name)
		}
	}

	// Field boundaries must not be ambiguous: concatenating the length-prefixed
	// event and value digest cannot let the event tail spill into the value.
	a := HookIdentityDigest("ab", Digest([]byte("c")))
	b := HookIdentityDigest("a", Digest([]byte("bc")))
	if a == b {
		t.Fatal("hook identity digest is not length-prefixed: field boundaries collide")
	}

	// Recomputing from the same inputs must be byte-identical.
	again := HookIdentityDigest(baseEvent, baseValueDigest)
	if again != baseDigest {
		t.Fatal("hook identity digest is not deterministic")
	}

	// The digest cannot contain the payload.
	secret := HookIdentityDigest("PreToolUse", Digest([]byte(fakePrompt)))
	if strings.Contains(secret, fakeSecret) {
		t.Fatal("hook identity digest contains the payload")
	}

	if !digestPattern.MatchString(baseDigest) {
		t.Fatal("hook identity digest is not a sha256 digest")
	}
}

func TestMcpIdentityDigestDistinguishesKeys(t *testing.T) {
	if McpIdentityDigest("docs") == McpIdentityDigest("docs2") {
		t.Fatal("MCP identity digests collided")
	}
	if !digestPattern.MatchString(McpIdentityDigest("docs")) {
		t.Fatal("MCP identity digest is not a sha256 digest")
	}
}

func TestStatusStrings(t *testing.T) {
	for status, want := range map[Status]string{
		StatusUnmanaged:    "unmanaged",
		StatusOwnedClean:   "owned-clean",
		StatusOwnedDrifted: "owned-drifted",
	} {
		if got := status.String(); got != want {
			t.Fatalf("Status(%d).String() = %q, want %q", status, got, want)
		}
	}
}

func writeRecord(t *testing.T, st *Store, rec *Record) {
	t.Helper()
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(st.StateFile(), append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}
