package materialize

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/ownership"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

// nativeHookRule is the canonical rule bytes used across the inspection
// tests: valid JSON that canonicalizes to itself.
const nativeHookRule = `{"hooks":[{"command":"./run.sh","type":"command"}],"matcher":"Bash"}`

// nativeHookDoc renders the documented native file body for a layout carrying
// one rule under the PreToolUse event.
func nativeHookDoc(t *testing.T, layout nativeHookLayout, rule string) []byte {
	t.Helper()
	var inner string
	if layout.container == "" {
		inner = fmt.Sprintf(`{"PreToolUse":[%s]}`, rule)
	} else {
		inner = fmt.Sprintf(`{"%s":{"PreToolUse":[%s]}}`, layout.container, rule)
	}
	if layout.requireVersion {
		// Cursor's documented envelope carries version: 1 beside hooks.
		inner = inner[:1] + `"version":1,` + inner[1:]
	}
	return []byte(inner + "\n")
}

// nativeHookDigest returns the hook/v2 identity digest and canonical value
// digest the inspector must derive for the rule, matching the identity the
// future native writer records.
func nativeHookDigest(t *testing.T, event, rule string) (identity, valueDigest string) {
	t.Helper()
	canonical, err := canonicalOwnedJSON([]byte(rule))
	if err != nil {
		t.Fatal(err)
	}
	valueDigest = ownership.Digest(canonical)
	return ownership.HookIdentityDigest(event, valueDigest), valueDigest
}

// nativeHookProofEntry builds the durable record the future native writer
// stores for one emitted rule: the hook/v2 identity over the NATIVE event
// and canonical native rule bytes, the ownership resource name, and the
// version-1 source-hook fingerprint.
func nativeHookProofEntry(t *testing.T, event, rule, resource, sourceDigest string) ownership.Entry {
	t.Helper()
	identity, valueDigest := nativeHookDigest(t, event, rule)
	return ownership.Entry{
		Kind:           ownership.KindHook,
		Container:      event,
		IdentityDigest: identity,
		ValueDigest:    valueDigest,
		Resource:       resource,
		SourceDigest:   sourceDigest,
	}
}

// seedNativeProof records ownership at the layout's file under root, using
// the same mutation path a writer takes: it writes the file and the durable
// entries, including each entry's source-hook digest, in one apply.
func seedNativeProof(t *testing.T, root string, layout nativeHookLayout, body []byte, entries ...ownership.Entry) {
	t.Helper()
	abs := filepath.Join(root, filepath.FromSlash(layout.rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	store, err := ownership.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	var priorDigest string
	if current, err := ownership.ReadAggregateFile(abs); err == nil {
		priorDigest = ownership.Digest(current)
	}
	_, err = store.Apply(ownership.Mutation{
		Rel:               layout.rel,
		AbsFile:           abs,
		NextBytes:         body,
		ExpectPriorDigest: priorDigest,
		Entries:           entries,
		Reason:            ownership.ReasonSync,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func openNativeStore(t *testing.T, root string) *ownership.Store {
	t.Helper()
	store, err := ownership.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

// allNativeHookLayouts enumerates the full spec table: four targets in both
// scopes.
func allNativeHookLayouts() []struct {
	target       hcc.Target
	machineScope bool
} {
	return []struct {
		target       hcc.Target
		machineScope bool
	}{
		{hcc.TargetClaude, false}, {hcc.TargetClaude, true},
		{hcc.TargetCursor, false}, {hcc.TargetCursor, true},
		{hcc.TargetFactory, false}, {hcc.TargetFactory, true},
		{hcc.TargetWindsurf, false}, {hcc.TargetWindsurf, true},
	}
}

func writeNativeFile(t *testing.T, root string, layout nativeHookLayout, body []byte) string {
	t.Helper()
	abs := filepath.Join(root, filepath.FromSlash(layout.rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, body, 0o644); err != nil {
		t.Fatal(err)
	}
	return abs
}

func TestInspectNativeHooksProvenEntry(t *testing.T) {
	const event = "PreToolUse"
	const sourceDigest = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	for _, tc := range allNativeHookLayouts() {
		name := fmt.Sprintf("%s_machine=%v", tc.target, tc.machineScope)
		t.Run(name, func(t *testing.T) {
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			root := t.TempDir()
			layout, ok := nativeHookLayoutFor(tc.target, tc.machineScope)
			if !ok {
				t.Fatalf("no layout for %s machine=%v", tc.target, tc.machineScope)
			}
			body := nativeHookDoc(t, layout, nativeHookRule)
			seedNativeProof(t, root, layout, body, nativeHookProofEntry(t, event, nativeHookRule, "deploy", sourceDigest))

			entries, err := InspectNativeHooks(openNativeStore(t, root), root, tc.target, tc.machineScope)
			if err != nil {
				t.Fatalf("InspectNativeHooks: %v", err)
			}
			if len(entries) != 1 {
				t.Fatalf("entries = %#v, want exactly the one rule", entries)
			}
			got := entries[0]
			if got.Event != event || got.Status != ownership.StatusOwnedClean {
				t.Fatalf("entry = %#v, want owned-clean under %s", got, event)
			}
			if got.SourceDigest != sourceDigest {
				t.Fatalf("source digest = %q, want recorded proof", got.SourceDigest)
			}
		})
	}
}

// A JSON-irrelevant rewrite — the same rule with reordered keys — must still
// classify owned-clean because identity covers the canonical value, not the
// byte order. Consumer recognition is witnessed by a Run-level test.
func TestInspectNativeHooksReorderedKeysStillProven(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	const event = "PreToolUse"
	const sourceDigest = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	root := t.TempDir()
	layout, ok := nativeHookLayoutFor(hcc.TargetCursor, false)
	if !ok {
		t.Fatal("cursor repo layout missing")
	}
	seedNativeProof(t, root, layout, nativeHookDoc(t, layout, nativeHookRule), nativeHookProofEntry(t, event, nativeHookRule, "deploy", sourceDigest))

	// Rewrite the on-disk file with reordered keys inside the rule; ownership
	// state is untouched, so only canonical equivalence can prove the entry.
	reordered := `{"version":1,"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"./run.sh"}]}]}}` + "\n"
	writeNativeFile(t, root, layout, []byte(reordered))

	entries, err := InspectNativeHooks(openNativeStore(t, root), root, hcc.TargetCursor, false)
	if err != nil {
		t.Fatalf("InspectNativeHooks: %v", err)
	}
	if len(entries) != 1 || entries[0].Status != ownership.StatusOwnedClean || entries[0].SourceDigest != sourceDigest {
		t.Fatalf("reordered entry = %#v err=%v, want owned-clean with proof", entries, err)
	}
}

func TestInspectNativeHooksUnprovenEntries(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	const event = "PreToolUse"
	const sourceDigest = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	root := t.TempDir()
	layout, ok := nativeHookLayoutFor(hcc.TargetClaude, false)
	if !ok {
		t.Fatal("claude repo layout missing")
	}

	// Handwritten file with no ownership record at all.
	writeNativeFile(t, root, layout, nativeHookDoc(t, layout, nativeHookRule))
	entries, err := InspectNativeHooks(openNativeStore(t, root), root, hcc.TargetClaude, false)
	if err != nil {
		t.Fatalf("handwritten inspect: %v", err)
	}
	if len(entries) != 1 || entries[0].Status != ownership.StatusUnmanaged || entries[0].SourceDigest != "" {
		t.Fatalf("handwritten entry = %#v, want unmanaged without proof", entries)
	}

	// A proven entry whose on-disk bytes drifted (a changed command, which is
	// a changed execution field) derives a different hook/v2 identity, so it
	// does not match the recorded proof and its recorded digest is NOT
	// exposed as usable evidence.
	changedRule := `{"hooks":[{"command":"./changed.sh","type":"command"}],"matcher":"Bash"}`
	seedNativeProof(t, root, layout, nativeHookDoc(t, layout, nativeHookRule), nativeHookProofEntry(t, event, nativeHookRule, "deploy", sourceDigest))
	writeNativeFile(t, root, layout, nativeHookDoc(t, layout, changedRule))
	entries, err = InspectNativeHooks(openNativeStore(t, root), root, hcc.TargetClaude, false)
	if err != nil {
		t.Fatalf("changed inspect: %v", err)
	}
	if len(entries) != 1 || entries[0].Status != ownership.StatusUnmanaged || entries[0].SourceDigest != "" {
		t.Fatalf("changed entry = %#v, want no proven match (a changed field is a different identity)", entries)
	}
}

func TestInspectNativeHooksRefusesMalformed(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	layout, ok := nativeHookLayoutFor(hcc.TargetClaude, false)
	if !ok {
		t.Fatal("claude repo layout missing")
	}

	cases := map[string]struct {
		body     string
		wantCode string
	}{
		"malformed_json":     {`{"hooks": {`, diag.CodeHookNativeToleranceRefused},
		"duplicate_key":      {`{"hooks":{"PreToolUse":[],"PreToolUse":[]}}`, diag.CodeJSONDuplicateKey},
		"non_object_root":    {`["not","an","object"]`, diag.CodeHookNativeToleranceRefused},
		"non_object_member":  {`{"hooks":"not-an-object"}`, diag.CodeHookNativeToleranceRefused},
		"non_array_event":    {`{"hooks":{"PreToolUse":{"matcher":"Bash"}}}`, diag.CodeHookNativeToleranceRefused},
		"non_object_element": {`{"hooks":{"PreToolUse":["just-a-string"]}}`, diag.CodeHookNativeToleranceRefused},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			abs := writeNativeFile(t, root, layout, []byte(tc.body))
			before, err := os.ReadFile(abs)
			if err != nil {
				t.Fatal(err)
			}
			entries, err := InspectNativeHooks(openNativeStore(t, root), root, hcc.TargetClaude, false)
			if err == nil {
				t.Fatalf("inspect = %#v entries, want refusal", entries)
			}
			coded, ok := diag.AsCodedError(err)
			if !ok || coded.Code != tc.wantCode {
				t.Fatalf("error = %v, want coded %s", err, tc.wantCode)
			}
			after, readErr := os.ReadFile(abs)
			if readErr != nil || string(after) != string(before) {
				t.Fatalf("native file changed after refusal")
			}
		})
	}

	// A file that is missing or has no hooks member proves nothing and is not
	// an error.
	t.Run("absent_file", func(t *testing.T) {
		root := t.TempDir()
		entries, err := InspectNativeHooks(openNativeStore(t, root), root, hcc.TargetClaude, false)
		if err != nil || entries != nil {
			t.Fatalf("absent file: entries=%#v err=%v, want none", entries, err)
		}
	})
	t.Run("no_hooks_member", func(t *testing.T) {
		root := t.TempDir()
		writeNativeFile(t, root, layout, []byte(`{"otherSetting":true}`+"\n"))
		entries, err := InspectNativeHooks(openNativeStore(t, root), root, hcc.TargetClaude, false)
		if err != nil || len(entries) != 0 {
			t.Fatalf("no hooks member: entries=%#v err=%v, want none", entries, err)
		}
	})

	// A symlinked native file is outside the authorized read boundary.
	t.Run("symlink_escape", func(t *testing.T) {
		root := t.TempDir()
		outside := filepath.Join(t.TempDir(), "settings.json")
		if err := os.WriteFile(outside, nativeHookDoc(t, layout, nativeHookRule), 0o644); err != nil {
			t.Fatal(err)
		}
		abs := filepath.Join(root, filepath.FromSlash(layout.rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, abs); err != nil {
			t.Fatal(err)
		}
		entries, err := InspectNativeHooks(openNativeStore(t, root), root, hcc.TargetClaude, false)
		if err == nil {
			t.Fatalf("symlinked file: entries=%#v, want refusal", entries)
		}
		if coded, ok := diag.AsCodedError(err); !ok || coded.Code != diag.CodeHookNativeToleranceRefused {
			t.Fatalf("symlink error = %v, want %s", err, diag.CodeHookNativeToleranceRefused)
		}
	})

	// A hard-linked final file is ambiguous ownership evidence: the same inode
	// may be edited through a second path this reader cannot see.
	t.Run("hardlinked_file", func(t *testing.T) {
		root := t.TempDir()
		abs := writeNativeFile(t, root, layout, nativeHookDoc(t, layout, nativeHookRule))
		other := filepath.Join(t.TempDir(), "linked.json")
		if err := os.Link(abs, other); err != nil {
			t.Skipf("hard links unsupported: %v", err)
		}
		entries, err := InspectNativeHooks(openNativeStore(t, root), root, hcc.TargetClaude, false)
		if err == nil {
			t.Fatalf("hardlinked file: entries=%#v, want refusal", entries)
		}
		if coded, ok := diag.AsCodedError(err); !ok || coded.Code != diag.CodeHookNativeToleranceRefused {
			t.Fatalf("hardlink error = %v, want %s", err, diag.CodeHookNativeToleranceRefused)
		}
	})
}

func TestInspectNativeHooksCursorVersionEnvelope(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	layout, ok := nativeHookLayoutFor(hcc.TargetCursor, false)
	if !ok {
		t.Fatal("cursor repo layout missing")
	}
	for name, body := range map[string]string{
		"missing_version": `{"hooks":{"PreToolUse":[` + nativeHookRule + `]}}`,
		"wrong_version":   `{"version":2,"hooks":{"PreToolUse":[` + nativeHookRule + `]}}`,
		"string_version":  `{"version":"1","hooks":{"PreToolUse":[` + nativeHookRule + `]}}`,
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writeNativeFile(t, root, layout, []byte(body))
			entries, err := InspectNativeHooks(openNativeStore(t, root), root, hcc.TargetCursor, false)
			if err == nil {
				t.Fatalf("%s: entries=%#v, want envelope refusal", name, entries)
			}
			if coded, ok := diag.AsCodedError(err); !ok || coded.Code != diag.CodeHookNativeToleranceRefused {
				t.Fatalf("%s: error = %v, want %s", name, err, diag.CodeHookNativeToleranceRefused)
			}
		})
	}
}

func TestInspectNativeHooksWritesNothing(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	const event = "PreToolUse"
	const sourceDigest = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	root := t.TempDir()
	layout, ok := nativeHookLayoutFor(hcc.TargetWindsurf, true)
	if !ok {
		t.Fatal("windsurf machine layout missing")
	}
	body := nativeHookDoc(t, layout, nativeHookRule)
	seedNativeProof(t, root, layout, body, nativeHookProofEntry(t, event, nativeHookRule, "deploy", sourceDigest))

	// snapshot the whole root tree before inspection.
	snapshot := map[string]string{}
	if err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		snapshot[rel] = string(data)
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// A second, malformed native file elsewhere must not be touched either,
	// and a corrupt state file must refuse rather than be rewritten.
	store := openNativeStore(t, root)
	if err := os.MkdirAll(filepath.Dir(store.StateFile()), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := InspectNativeHooks(store, root, hcc.TargetWindsurf, true); err != nil {
		t.Fatalf("inspect proven entry: %v", err)
	}
	if err := os.WriteFile(store.StateFile(), []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := InspectNativeHooks(store, root, hcc.TargetWindsurf, true); err == nil {
		t.Fatal("corrupt ownership state must refuse, not be treated as unmanaged")
	} else if coded, ok := diag.AsCodedError(err); !ok || coded.Code != diag.CodeHookNativeToleranceRefused {
		t.Fatalf("corrupt state error = %v, want %s", err, diag.CodeHookNativeToleranceRefused)
	}

	// Restore nothing; verify inspection created no files and changed none of
	// the previously existing ones under the target root.
	if err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		before, ok := snapshot[rel]
		if !ok {
			t.Fatalf("inspection created file %s", rel)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if string(data) != before {
			t.Fatalf("inspection mutated %s", rel)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// Windsurf's machine native file is also the legacy file, so a proven entry
// and a handwritten sibling coexist in one document. Inspection classifies
// each independently and leaves both byte-identical.
func TestInspectNativeHooksWindsurfMachineSharedFile(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	const event = "PreToolUse"
	const sourceDigest = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	root := t.TempDir()
	layout, ok := nativeHookLayoutFor(hcc.TargetWindsurf, true)
	if !ok {
		t.Fatal("windsurf machine layout missing")
	}
	handwrittenRule := `{"hooks":[{"command":"./handwritten.sh","type":"command"}],"matcher":"Read"}`
	body := nativeHookDoc(t, layout, nativeHookRule+","+handwrittenRule)
	abs := writeNativeFile(t, root, layout, body)
	seedNativeProof(t, root, layout, body, nativeHookProofEntry(t, event, nativeHookRule, "deploy", sourceDigest))

	entries, err := InspectNativeHooks(openNativeStore(t, root), root, hcc.TargetWindsurf, true)
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %#v, want the proven rule and the handwritten sibling", entries)
	}
	if entries[0].Status != ownership.StatusOwnedClean || entries[0].SourceDigest != sourceDigest {
		t.Fatalf("proven entry = %#v, want owned-clean with source proof", entries[0])
	}
	if entries[1].Status != ownership.StatusUnmanaged || entries[1].SourceDigest != "" {
		t.Fatalf("sibling = %#v, want unmanaged with no proof", entries[1])
	}
	after, err := os.ReadFile(abs)
	if err != nil || string(after) != string(body) {
		t.Fatalf("shared file changed under inspection")
	}
}

// A symlinked PARENT component is an escape even when the final file itself is
// real: <root>/.devin pointing outside the authorized root must refuse before
// the file's own bytes are ever consulted, leave the outside bytes untouched,
// and create no ownership state.
func TestInspectNativeHooksSymlinkedParentRefused(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	layout, ok := nativeHookLayoutFor(hcc.TargetWindsurf, false)
	if !ok {
		t.Fatal("windsurf repo layout missing")
	}
	if filepath.Dir(layout.rel) == "." {
		t.Fatalf("layout %q has no parent component to symlink", layout.rel)
	}

	root := t.TempDir()
	outsideDir := t.TempDir()
	outsideBody := nativeHookDoc(t, layout, nativeHookRule)
	if err := os.WriteFile(filepath.Join(outsideDir, "hooks.json"), outsideBody, 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, filepath.FromSlash(filepath.Dir(layout.rel)))
	if err := os.Symlink(outsideDir, link); err != nil {
		t.Fatal(err)
	}

	store := openNativeStore(t, root)
	entries, err := InspectNativeHooks(store, root, hcc.TargetWindsurf, false)
	if err == nil {
		t.Fatalf("symlinked parent: entries=%#v, want refusal", entries)
	}
	if coded, ok := diag.AsCodedError(err); !ok || coded.Code != diag.CodeHookNativeToleranceRefused {
		t.Fatalf("symlinked parent error = %v, want %s", err, diag.CodeHookNativeToleranceRefused)
	}
	after, readErr := os.ReadFile(filepath.Join(outsideDir, "hooks.json"))
	if readErr != nil || string(after) != string(outsideBody) {
		t.Fatalf("outside bytes changed after refusal")
	}
	if _, statErr := os.Lstat(store.StateFile()); !os.IsNotExist(statErr) {
		t.Fatalf("inspection created ownership state at %s", store.StateFile())
	}
}

// A native file beyond the aggregate read bound — including one that reports a
// safe size but reads past the limit — is refused without being parsed.
func TestInspectNativeHooksOversizedFileRefused(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	layout, ok := nativeHookLayoutFor(hcc.TargetClaude, false)
	if !ok {
		t.Fatal("claude repo layout missing")
	}
	root := t.TempDir()
	abs := writeNativeFile(t, root, layout, nativeHookDoc(t, layout, nativeHookRule))
	// Grow the file past the 64 MiB aggregate bound sparsely; the read must
	// refuse on size rather than attempting to parse it.
	const limitPlusOne = 64<<20 + 1
	if err := os.Truncate(abs, limitPlusOne); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(abs)
	if err != nil {
		t.Fatal(err)
	}

	store := openNativeStore(t, root)
	entries, err := InspectNativeHooks(store, root, hcc.TargetClaude, false)
	if err == nil {
		t.Fatalf("oversized file: entries=%#v, want refusal", entries)
	}
	if coded, ok := diag.AsCodedError(err); !ok || coded.Code != diag.CodeHookNativeToleranceRefused {
		t.Fatalf("oversized error = %v, want %s", err, diag.CodeHookNativeToleranceRefused)
	}
	after, readErr := os.ReadFile(abs)
	if readErr != nil || !bytes.Equal(after, before) {
		t.Fatalf("oversized file changed after refusal")
	}
	if _, statErr := os.Lstat(store.StateFile()); !os.IsNotExist(statErr) {
		t.Fatalf("inspection created ownership state at %s", store.StateFile())
	}
}

// A refusal for one target must not contaminate an independent target: each
// layout is inspected separately.
func TestInspectNativeHooksIndependentTargets(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	badRoot := t.TempDir()
	badLayout, _ := nativeHookLayoutFor(hcc.TargetFactory, false)
	writeNativeFile(t, badRoot, badLayout, []byte(`{"PreToolUse":"not-an-array"}`))
	if _, err := InspectNativeHooks(openNativeStore(t, badRoot), badRoot, hcc.TargetFactory, false); err == nil {
		t.Fatal("malformed factory file must refuse")
	}

	goodRoot := t.TempDir()
	goodLayout, _ := nativeHookLayoutFor(hcc.TargetClaude, true)
	writeNativeFile(t, goodRoot, goodLayout, nativeHookDoc(t, goodLayout, nativeHookRule))
	entries, err := InspectNativeHooks(openNativeStore(t, goodRoot), goodRoot, hcc.TargetClaude, true)
	if err != nil || len(entries) != 1 {
		t.Fatalf("independent target: entries=%#v err=%v, want one entry", entries, err)
	}
}
