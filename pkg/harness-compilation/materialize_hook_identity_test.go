package harnesscompile

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// hookNoOpCase is one aggregate hook file that a previous sync already wrote
// into, plus the desired document that same sync would produce again.
type hookNoOpCase struct {
	name string
	file string
	// existing is the file as it stands after an earlier sync AND after the
	// user edited around our entries: their own rule groups sit before, after,
	// and between ours, each with a comment above it.
	existing string
	desired  string
	// keep lists the user's bytes, comments included, that must survive.
	keep []string
	// commentOwner pins which element each comment sits above. A comment binds
	// to the element that FOLLOWS it (bead agent-marketplace-5exj.26), so an
	// element that moves steals a comment.
	commentOwner map[string]string
	// gone lists the managed payloads that must disappear once the desired
	// document changes, so the control test proves a supersede really removes.
	gone []string
}

// hookNoOpCases is the shared table for the two tests below: the headline
// byte-identical proof and its non-vacuity control. Every one of the four
// targets that compiles hooks into a shared JSON aggregate is represented, in
// the exact shape its compiler emits:
//
//   - Factory writes hooks/hooks.json with NO envelope: the top-level keys are
//     event names and each value is an array of unnamed rule GROUPS
//     ({"matcher": ..., "hooks": [...]}). This is the shape bead
//     agent-marketplace-5exj.28 is about.
//   - Claude and Cursor write hooks/hooks.json with a "hooks" envelope.
//   - Gemini writes the same envelope into settings.json.
//
// In every case the managed entries are already in the file and the user has
// arranged their own entries around them, which is the only situation in which
// the pre-.28 merge could reorder anything.
var hookNoOpCases = []hookNoOpCase{
	{
		name: "factory hooks.json unnamed rule groups",
		file: "hooks/hooks.json",
		existing: "{\n" +
			"  \"PreToolUse\": [\n" +
			"    {\"matcher\": \"Read\", \"hooks\": [{\"type\": \"command\", \"command\": \"gen-read\"}], \"_managed_by\": \"agent-marketplace\"},\n" +
			"    // my own audit hook, must stay right here\n" +
			"    {\"matcher\": \"Bash\",   \"hooks\": [{\"type\": \"command\", \"command\": \"audit.sh\"}]},\n" +
			"    {\"matcher\": \"Write\", \"hooks\": [{\"type\": \"command\", \"command\": \"gen-write\"}], \"_managed_by\": \"agent-marketplace\"}\n" +
			"  ],\n" +
			"  // an event only I use\n" +
			"  \"Stop\": [\n" +
			"    {\"hooks\": [{\"type\": \"command\", \"command\": \"mine.sh\"}]}\n" +
			"  ]\n" +
			"}\n",
		desired: `{"PreToolUse":[{"matcher":"Read","hooks":[{"type":"command","command":"gen-read"}]},` +
			`{"matcher":"Write","hooks":[{"type":"command","command":"gen-write"}]}]}`,
		keep: []string{
			"// my own audit hook, must stay right here",
			"{\"matcher\": \"Bash\",   \"hooks\": [{\"type\": \"command\", \"command\": \"audit.sh\"}]}",
			"// an event only I use",
			"{\"hooks\": [{\"type\": \"command\", \"command\": \"mine.sh\"}]}",
		},
		commentOwner: map[string]string{
			"// my own audit hook, must stay right here": "{\"matcher\": \"Bash\",",
			"// an event only I use":                     "\"Stop\":",
		},
		gone: []string{`"gen-read"`, `"gen-write"`},
	},
	{
		name: "claude hooks.json envelope",
		file: "hooks/hooks.json",
		existing: "{\n" +
			"  \"hooks\": {\n" +
			"    \"PreToolUse\": [\n" +
			"      {\"command\": \"gen-a\", \"matcher\": \"Read\", \"type\": \"command\", \"_managed_by\": \"agent-marketplace\"},\n" +
			"      // handwritten, keep it second\n" +
			"      {\"command\": \"mine\",  \"matcher\": \"Read\", \"type\": \"command\"}\n" +
			"    ]\n" +
			"  }\n" +
			"}\n",
		desired: `{"hooks":{"PreToolUse":[{"command":"gen-a","matcher":"Read","type":"command"}]}}`,
		keep: []string{
			"// handwritten, keep it second",
			"{\"command\": \"mine\",  \"matcher\": \"Read\", \"type\": \"command\"}",
		},
		commentOwner: map[string]string{
			"// handwritten, keep it second": "{\"command\": \"mine\",",
		},
		gone: []string{`"gen-a"`},
	},
	{
		name: "cursor hooks.json envelope with event member",
		file: "hooks/hooks.json",
		existing: "{\n" +
			"  \"hooks\": {\n" +
			"    \"PreToolUse\": [\n" +
			"      {\"command\": \"gen\", \"event\": \"PreToolUse\", \"matcher\": \"Read\", \"type\": \"command\", \"_managed_by\": \"agent-marketplace\"},\n" +
			"      /* mine */\n" +
			"      {\"command\": \"mine\", \"event\": \"PreToolUse\", \"type\": \"command\"}\n" +
			"    ]\n" +
			"  }\n" +
			"}\n",
		desired: `{"hooks":{"PreToolUse":[{"command":"gen","event":"PreToolUse","matcher":"Read","type":"command"}]}}`,
		keep: []string{
			"/* mine */",
			"{\"command\": \"mine\", \"event\": \"PreToolUse\", \"type\": \"command\"}",
		},
		commentOwner: map[string]string{
			"/* mine */": "{\"command\": \"mine\",",
		},
		gone: []string{`"command": "gen"`},
	},
	{
		name: "gemini settings.json envelope",
		file: "settings.json",
		existing: "{\n" +
			"  \"hooks\": {\n" +
			"    \"BeforeTool\": [\n" +
			"      {\"command\": \"gen\", \"event\": \"BeforeTool\", \"type\": \"command\", \"_managed_by\": \"agent-marketplace\"},\n" +
			"      // my own\n" +
			"      {\"command\": \"mine\", \"event\": \"BeforeTool\", \"type\": \"command\"}\n" +
			"    ]\n" +
			"  },\n" +
			"  \"theme\": \"dark\"\n" +
			"}\n",
		desired: `{"hooks":{"BeforeTool":[{"command":"gen","event":"BeforeTool","type":"command"}]}}`,
		keep: []string{
			"// my own",
			"{\"command\": \"mine\", \"event\": \"BeforeTool\", \"type\": \"command\"}",
			"\"theme\": \"dark\"",
		},
		commentOwner: map[string]string{
			"// my own": "{\"command\": \"mine\",",
		},
		gone: []string{`"command": "gen"`},
	},
}

// writeHookFixture lays the existing file down, creating the nested directory
// hooks/ that two of the four targets use.
func writeHookFixture(t *testing.T, outDir, file, contents string) string {
	t.Helper()
	path := filepath.Join(outDir, filepath.FromSlash(file))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestMaterialize_NoOpHookSyncIsByteIdentical is the acceptance criterion of
// bead agent-marketplace-5exj.28: syncing the same hooks twice into a file that
// already contains them leaves the file byte-identical, and the managed rule
// groups keep the slots they already occupy.
//
// The first Materialize here is ALREADY a no-op sync — the destination is what
// an earlier sync plus a user edit produced — so the check is the strict one:
// not "the second run matches the first" but "no run ever changes a byte".
// Before .28 the first run rewrote every one of these files, because a managed
// entry with no "name" had no identity: all of them were dropped and re-added
// at the end of the array, which shuffled the user's entries forward and, since
// a comment binds to the element that follows it, moved the user's comments
// onto our generated entries.
// spec: hook-resources/repeat-sync-is-byte-identical
func TestMaterialize_NoOpHookSyncIsByteIdentical(t *testing.T) {
	for _, tt := range hookNoOpCases {
		t.Run(tt.name, func(t *testing.T) {
			outDir := t.TempDir()
			path := writeHookFixture(t, outDir, tt.file, tt.existing)
			original := []byte(tt.existing)

			// Non-vacuity: the fixture really does contain entries we own, so
			// the merge below has something to reconcile rather than trivially
			// nothing to do.
			if !strings.Contains(tt.existing, "_managed_by") {
				t.Fatalf("fixture has no managed entry; the case would be vacuous")
			}

			for run := 1; run <= 2; run++ {
				report, err := Materialize(mergeArtifact(tt.file, tt.desired), outDir, WithWriteMode(WriteModeOverwrite))
				if err != nil {
					t.Fatalf("Materialize run %d: %v", run, err)
				}
				if len(report.Written) != 0 {
					t.Errorf("run %d rewrote %v, want no write: the merge output is not a fixed point", run, report.Written)
				}
				onDisk, err := os.ReadFile(path)
				if err != nil {
					t.Fatalf("read back: %v", err)
				}
				if !bytes.Equal(onDisk, original) {
					t.Fatalf("run %d changed the file:\nbefore:\n%s\nafter:\n%s", run, original, onDisk)
				}
				for _, want := range tt.keep {
					if !strings.Contains(string(onDisk), want) {
						t.Fatalf("run %d lost the user's bytes %q:\n%s", run, want, onDisk)
					}
				}
				for comment, owner := range tt.commentOwner {
					assertCommentPrecedes(t, string(onDisk), comment, owner)
				}
				assertNoStagingResidue(t, outDir)
			}
		})
	}
}

// assertCommentPrecedes fails unless comment is immediately above owner, with
// nothing but whitespace between them. That is the invariant a reorder breaks:
// the comment stays where it is and a different element slides under it.
func assertCommentPrecedes(t *testing.T, document, comment, owner string) {
	t.Helper()
	at := strings.Index(document, comment)
	if at < 0 {
		t.Fatalf("comment %q is gone:\n%s", comment, document)
	}
	rest := document[at+len(comment):]
	if strings.TrimLeft(rest, " \t\r\n") == "" || !strings.HasPrefix(strings.TrimLeft(rest, " \t\r\n"), owner) {
		t.Fatalf("comment %q no longer sits above %q:\n%s", comment, owner, document)
	}
}

// TestMaterialize_ChangedHookSyncStillWrites is the non-vacuity control for the
// test above: the same fixtures with a genuinely different desired document
// must be rewritten. Without it, a merge that silently refused to write
// anything at all would pass the byte-identical assertion.
func TestMaterialize_ChangedHookSyncStillWrites(t *testing.T) {
	for _, tt := range hookNoOpCases {
		t.Run(tt.name, func(t *testing.T) {
			outDir := t.TempDir()
			path := writeHookFixture(t, outDir, tt.file, tt.existing)
			changed := strings.ReplaceAll(tt.desired, "gen", "gen-v2")

			report, err := Materialize(mergeArtifact(tt.file, changed), outDir, WithWriteMode(WriteModeOverwrite))
			if err != nil {
				t.Fatalf("Materialize: %v", err)
			}
			if len(report.Written) != 1 {
				t.Fatalf("Written = %v, want the changed file to be rewritten", report.Written)
			}
			onDisk, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read back: %v", err)
			}
			if !strings.Contains(string(onDisk), "gen-v2") {
				t.Fatalf("the new hook was not written:\n%s", onDisk)
			}
			// The superseded managed entries are gone, and the user's entries
			// and comments are still there.
			for _, stale := range tt.gone {
				if strings.Contains(string(onDisk), stale) {
					t.Fatalf("a superseded managed entry %s survived:\n%s", stale, onDisk)
				}
			}
			for _, want := range tt.keep {
				if !strings.Contains(string(onDisk), want) {
					t.Fatalf("the user's bytes %q did not survive:\n%s", want, onDisk)
				}
			}
			for comment, owner := range tt.commentOwner {
				assertCommentPrecedes(t, string(onDisk), comment, owner)
			}
			assertNoStagingResidue(t, outDir)
		})
	}
}
