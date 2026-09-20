package artifact

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/internal/testutil"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/jsonc"
)

// mergeFakeSecret stands in for a credential a user really keeps in an MCP env
// block, an auth header, or an OAuth member. Every assertion below checks it
// survives a merge byte-for-byte and never reaches an error message.
const mergeFakeSecret = testutil.FakeSecret

const (
	markerField = "_managed_by"
	markerValue = "agent-marketplace"
)

// mergeOnce is the shared call under test. Every table below drives the real
// public entry point, not an internal helper.
func mergeOnce(t *testing.T, existing, desired string) ([]byte, ExistingMergeReport) {
	t.Helper()
	out, report, err := MergeWithExistingJSON([]byte(existing), []byte(desired), markerField, markerValue)
	if err != nil {
		t.Fatalf("MergeWithExistingJSON: %v", err)
	}
	return out, report
}

// TestMergeWithExistingJSON_PreservesConcreteSyntax is the core guarantee of
// bead agent-marketplace-5exj.26: an aggregate WRITE that merges into an
// existing file preserves the comments, the key order, and the handwritten
// formatting of every member it does not own.
//
// The cases cover the real aggregate shapes of all five marker-merge targets
// and every merge shape in merge.go: the keyed-object container (Claude/Cursor/
// Windsurf mcp files), the array-of-objects container (Factory plugin.json,
// Gemini gemini-extension.json), the map-of-arrays-of-objects container
// (hooks.json / settings.json hooks), and wholesale replacement of a scalar.
func TestMergeWithExistingJSON_PreservesConcreteSyntax(t *testing.T) {
	tests := []struct {
		name     string
		existing string
		desired  string
		want     string
	}{
		{
			// Claude .mcp.json: keyed-object container. The user's comment,
			// their key order (zzz before aaa), their inline spacing, and their
			// secret all survive; our entry is appended at the end.
			name: "claude mcp keyed object",
			existing: "{\n" +
				"  // my own servers, hands off\n" +
				"  \"mcpServers\": {\n" +
				"    // the important one\n" +
				"    \"zzz-user\": {\"command\":\"user\",   \"env\": {\"TOKEN\": \"" + mergeFakeSecret + "\"}},\n" +
				"    \"aaa-user\": {\"command\": \"other\"}\n" +
				"  }\n" +
				"}\n",
			desired: `{"mcpServers":{"generated":{"command":"gen"}}}`,
			want: "{\n" +
				"  // my own servers, hands off\n" +
				"  \"mcpServers\": {\n" +
				"    // the important one\n" +
				"    \"zzz-user\": {\"command\":\"user\",   \"env\": {\"TOKEN\": \"" + mergeFakeSecret + "\"}},\n" +
				"    \"aaa-user\": {\"command\": \"other\"},\n" +
				"    \"generated\": {\n" +
				"      \"_managed_by\": \"agent-marketplace\",\n" +
				"      \"command\": \"gen\"\n" +
				"    }\n" +
				"  }\n" +
				"}\n",
		},
		{
			// Cursor mcp.json: an existing managed entry is updated in place. It
			// keeps its position between two user entries AND its one-line
			// spelling, because the patch reaches the one leaf that changed
			// instead of re-rendering the entry.
			name: "cursor mcp updates managed entry in place",
			existing: "{\n" +
				"  \"mcpServers\": {\n" +
				"    \"user-first\": {\"command\": \"a\"},\n" +
				"    \"generated\": {\"command\": \"v1\", \"_managed_by\": \"agent-marketplace\"},\n" +
				"    // trailing user server\n" +
				"    \"user-last\": {\"command\": \"z\"}\n" +
				"  }\n" +
				"}\n",
			desired: `{"mcpServers":{"generated":{"command":"v2"}}}`,
			want: "{\n" +
				"  \"mcpServers\": {\n" +
				"    \"user-first\": {\"command\": \"a\"},\n" +
				"    \"generated\": {\"command\": \"v2\", \"_managed_by\": \"agent-marketplace\"},\n" +
				"    // trailing user server\n" +
				"    \"user-last\": {\"command\": \"z\"}\n" +
				"  }\n" +
				"}\n",
		},
		{
			// Windsurf mcp_config.json: a stale managed entry is removed and the
			// user's neighbours keep their comments and their exact bytes.
			name: "windsurf mcp_config removes stale managed entry",
			existing: "{\n" +
				"  \"mcpServers\": {\n" +
				"    // keep\n" +
				"    \"user\": {\"command\": \"u\"},\n" +
				"    \"stale\": {\"command\": \"old\", \"_managed_by\": \"agent-marketplace\"}\n" +
				"  }\n" +
				"}\n",
			desired: `{"mcpServers":{}}`,
			want: "{\n" +
				"  \"mcpServers\": {\n" +
				"    // keep\n" +
				"    \"user\": {\"command\": \"u\"}\n" +
				"  }\n" +
				"}\n",
		},
		{
			// Factory plugin.json: array-of-objects container. The handwritten
			// element keeps its bytes AND its comment because it never moves.
			name: "factory plugin array container",
			existing: "{\n" +
				"  \"name\": \"my-plugin\",\n" +
				"  \"skills\": [\n" +
				"    // handwritten, keep the odd spacing\n" +
				"    {\"name\": \"user-skill\",  \"description\": \"keep me\"}\n" +
				"  ]\n" +
				"}\n",
			desired: `{"skills":[{"name":"gen-skill","description":"generated"}]}`,
			want: "{\n" +
				"  \"name\": \"my-plugin\",\n" +
				"  \"skills\": [\n" +
				"    // handwritten, keep the odd spacing\n" +
				"    {\"name\": \"user-skill\",  \"description\": \"keep me\"},\n" +
				"    {\n" +
				"      \"_managed_by\": \"agent-marketplace\",\n" +
				"      \"description\": \"generated\",\n" +
				"      \"name\": \"gen-skill\"\n" +
				"    }\n" +
				"  ]\n" +
				"}\n",
		},
		{
			// Gemini gemini-extension.json: the same array shape under a
			// different container key, with a sibling scalar the merge must not
			// touch at all.
			name: "gemini extension array container keeps siblings",
			existing: "{\n" +
				"  \"name\": \"demo\",\n" +
				"  /* block comment before the version */\n" +
				"  \"version\": \"1.0.0\",\n" +
				"  \"contextFileName\": \"GEMINI.md\"\n" +
				"}\n",
			desired: `{"skills":[{"name":"gen"}]}`,
			want: "{\n" +
				"  \"name\": \"demo\",\n" +
				"  /* block comment before the version */\n" +
				"  \"version\": \"1.0.0\",\n" +
				"  \"contextFileName\": \"GEMINI.md\",\n" +
				"  \"skills\": [\n" +
				"    {\n" +
				"      \"_managed_by\": \"agent-marketplace\",\n" +
				"      \"name\": \"gen\"\n" +
				"    }\n" +
				"  ]\n" +
				"}\n",
		},
		{
			// Factory settings.json: map-of-arrays-of-objects. The user's hook
			// stays first and keeps its comment; ours is appended.
			name: "factory settings hooks map of arrays",
			existing: "{\n" +
				"  \"hooks\": {\n" +
				"    \"PostToolUse\": [\n" +
				"      // mine\n" +
				"      {\"matcher\": \"Write\", \"command\": \"user-hook\"}\n" +
				"    ]\n" +
				"  }\n" +
				"}\n",
			desired: `{"hooks":{"PostToolUse":[{"matcher":"Edit","command":"gen-hook"}]}}`,
			want: "{\n" +
				"  \"hooks\": {\n" +
				"    \"PostToolUse\": [\n" +
				"      // mine\n" +
				"      {\"matcher\": \"Write\", \"command\": \"user-hook\"},\n" +
				"      {\n" +
				"        \"_managed_by\": \"agent-marketplace\",\n" +
				"        \"command\": \"gen-hook\",\n" +
				"        \"matcher\": \"Edit\"\n" +
				"      }\n" +
				"    ]\n" +
				"  }\n" +
				"}\n",
		},
		{
			// A hook event that is no longer desired keeps the user's entry and
			// loses only ours; the untouched sibling event is byte-identical.
			name: "hook event dropped from desired keeps user entries",
			existing: "{\n" +
				"  \"hooks\": {\n" +
				"    \"PreToolUse\": [\n" +
				"      {\"matcher\": \"mine\"},\n" +
				"      {\"matcher\": \"ours\", \"_managed_by\": \"agent-marketplace\"}\n" +
				"    ],\n" +
				"    \"PostToolUse\": [{\"matcher\": \"user-only\"}]\n" +
				"  }\n" +
				"}\n",
			desired: `{"hooks":{}}`,
			want: "{\n" +
				"  \"hooks\": {\n" +
				"    \"PreToolUse\": [\n" +
				"      {\"matcher\": \"mine\"}\n" +
				"    ],\n" +
				"    \"PostToolUse\": [{\"matcher\": \"user-only\"}]\n" +
				"  }\n" +
				"}\n",
		},
		{
			// Wholesale replacement: a desired container that is neither an
			// object map nor an array-of-objects overwrites the existing value
			// and leaves every sibling alone.
			name: "scalar container is replaced wholesale",
			existing: "{\n" +
				"  // keep this\n" +
				"  \"user\": {\"a\": 1},\n" +
				"  \"version\": \"1\"\n" +
				"}\n",
			desired: `{"version":"2"}`,
			want: "{\n" +
				"  // keep this\n" +
				"  \"user\": {\"a\": 1},\n" +
				"  \"version\": \"2\"\n" +
				"}\n",
		},
		{
			// Nothing to do: the destination already says exactly what we want,
			// so not a single byte moves — including the comments and the
			// four-space indentation the user chose.
			name: "idempotent re-sync is byte-identical",
			existing: "{\n" +
				"    \"mcpServers\": {\n" +
				"        // ours, already correct\n" +
				"        \"generated\": {\"command\": \"gen\", \"_managed_by\": \"agent-marketplace\"}\n" +
				"    }\n" +
				"}\n",
			desired: `{"mcpServers":{"generated":{"command":"gen"}}}`,
			want: "{\n" +
				"    \"mcpServers\": {\n" +
				"        // ours, already correct\n" +
				"        \"generated\": {\"command\": \"gen\", \"_managed_by\": \"agent-marketplace\"}\n" +
				"    }\n" +
				"}\n",
		},
		{
			// A user file with four-space indentation gets our new entry
			// indented to match its own shape, not to a hard-coded two spaces.
			name: "inserted entry follows the file's indentation",
			existing: "{\n" +
				"    \"mcpServers\": {\n" +
				"        \"user\": {\"command\": \"u\"}\n" +
				"    }\n" +
				"}\n",
			desired: `{"mcpServers":{"gen":{"command":"g"}}}`,
			want: "{\n" +
				"    \"mcpServers\": {\n" +
				"        \"user\": {\"command\": \"u\"},\n" +
				"        \"gen\": {\n" +
				"          \"_managed_by\": \"agent-marketplace\",\n" +
				"          \"command\": \"g\"\n" +
				"        }\n" +
				"    }\n" +
				"}\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := mergeOnce(t, tt.existing, tt.desired)
			if string(got) != tt.want {
				t.Fatalf("merged =\n%q\nwant\n%q", got, tt.want)
			}
			if !strings.Contains(tt.existing, mergeFakeSecret) {
				return
			}
			if !bytes.Contains(got, []byte(mergeFakeSecret)) {
				t.Fatal("the user's value did not survive the merge")
			}
		})
	}
}

// TestMergeWithExistingJSON_IsAFixedPoint proves the patched bytes are stable:
// merging the same desired document into its own output changes nothing, so a
// sync cannot oscillate and Materialize's "bytes already match" check skips the
// write entirely on the second run.
func TestMergeWithExistingJSON_IsAFixedPoint(t *testing.T) {
	cases := map[string]struct{ existing, desired string }{
		"keyed object": {
			existing: "{\n  // user\n  \"mcpServers\": {\"user\": {\"command\": \"u\"}}\n}\n",
			desired:  `{"mcpServers":{"gen":{"command":"g"}}}`,
		},
		"array container": {
			existing: "{\n  \"skills\": [\n    {\"name\": \"user\"}\n  ]\n}\n",
			desired:  `{"skills":[{"name":"gen"}]}`,
		},
		"map of arrays": {
			existing: "{\n  \"hooks\": {\"PostToolUse\": [{\"matcher\": \"u\"}]}\n}\n",
			desired:  `{"hooks":{"PostToolUse":[{"matcher":"g"}]}}`,
		},
		// Factory's hooks/hooks.json: no envelope, and the array elements are
		// unnamed rule groups (bead agent-marketplace-5exj.28).
		"unnamed array elements": {
			existing: "{\n  \"PostToolUse\": [\n    // mine\n    {\"matcher\": \"u\", \"hooks\": [{\"type\": \"command\", \"command\": \"u\"}]}\n  ]\n}\n",
			desired:  `{"PostToolUse":[{"matcher":"g","hooks":[{"type":"command","command":"g"}]}]}`,
		},
		"first write": {
			existing: "",
			desired:  `{"mcpServers":{"gen":{"command":"g"}}}`,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			first, _ := mergeOnce(t, tc.existing, tc.desired)
			second, _ := mergeOnce(t, string(first), tc.desired)
			if !bytes.Equal(first, second) {
				t.Fatalf("merge is not a fixed point:\nfirst:  %q\nsecond: %q", first, second)
			}
			third, _ := mergeOnce(t, string(second), tc.desired)
			if !bytes.Equal(second, third) {
				t.Fatalf("merge oscillates on the third pass:\n%q\nvs\n%q", second, third)
			}
		})
	}
}

// TestMergeWithExistingJSON_FirstWriteAndEmptyDestination covers the cases where
// there is nothing to preserve. A preservation-focused patcher can easily emit
// something odd here (a closing brace glued to the last member, because the
// trivia that should precede it never existed in the source), so each case pins
// the exact bytes.
func TestMergeWithExistingJSON_FirstWriteAndEmptyDestination(t *testing.T) {
	desired := `{"mcpServers":{"gen":{"command":"g"}}}`
	conventional := "{\n" +
		"  \"mcpServers\": {\n" +
		"    \"gen\": {\n" +
		"      \"_managed_by\": \"agent-marketplace\",\n" +
		"      \"command\": \"g\"\n" +
		"    }\n" +
		"  }\n" +
		"}\n"

	tests := []struct {
		name     string
		existing string
		want     string
	}{
		{name: "no destination at all", existing: "", want: conventional},
		{name: "blank destination", existing: "   \n\t\n", want: conventional},
		// A destination that exists but has an empty root object has no member
		// to preserve, so it is rendered conventionally too — with the file's
		// own trailing newline kept.
		{name: "empty root object", existing: "{}\n", want: conventional},
		{name: "empty root object with whitespace", existing: "{\n}\n", want: conventional},
		// A JSON null is not an object; it is replaced rather than crashed on.
		{name: "null document", existing: "null\n", want: conventional},
		// An existing but EMPTY container is replaced instead of patched, for
		// the same reason: appending into `{}` would put the closing brace on
		// the last member's line.
		{
			name:     "empty container",
			existing: "{\n  \"mcpServers\": {}\n}\n",
			want: "{\n" +
				"  \"mcpServers\": {\n" +
				"    \"gen\": {\n" +
				"      \"_managed_by\": \"agent-marketplace\",\n" +
				"      \"command\": \"g\"\n" +
				"    }\n" +
				"  }\n" +
				"}\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := mergeOnce(t, tt.existing, desired)
			if string(got) != tt.want {
				t.Fatalf("merged =\n%q\nwant\n%q", got, tt.want)
			}
			// Non-vacuous: the result must be JSON we can read back.
			var probe map[string]any
			if err := json.Unmarshal(got, &probe); err != nil {
				t.Fatalf("result is not valid JSON: %v", err)
			}
		})
	}
}

// TestMergeWithExistingJSON_EmptiedContainer records the emptied-container
// behaviour bead agent-marketplace-5exj.18 first hit and .26 inherits: when the
// last member of a container is deleted, jsonc leaves the trivia the user wrote
// before the closing delimiter, so the result is `{\n  }`-shaped rather than a
// re-rendered `{}`.
//
// That is the deliberate choice, not an oversight, and it is why the assertion
// is on the SHAPE (the container is empty, the document still parses, trivia
// that belongs to nobody survives) rather than on a literal `{}` substring:
//
//   - Re-rendering `{}` would delete a comment the user left in the container
//     but not attached to any entry. The subtest below proves that comment
//     survives, so this is a real property, not a loosened assertion.
//   - A comment attached to a DELETED entry goes with it. That is correct
//     association, not loss: the comment described the entry that is gone.
func TestMergeWithExistingJSON_EmptiedContainer(t *testing.T) {
	t.Run("trivia the deleted entry did not own survives", func(t *testing.T) {
		existing := "{\n" +
			"  \"mcpServers\": {\n" +
			"    \"gen\": {\"command\": \"g\", \"_managed_by\": \"agent-marketplace\"}\n" +
			"    /* I keep my servers here */\n" +
			"  }\n" +
			"}\n"
		got, report := mergeOnce(t, existing, `{"mcpServers":{}}`)

		assertEmptyContainer(t, got, "mcpServers")
		if !strings.Contains(string(got), "I keep my servers here") {
			t.Fatalf("trivia not owned by the deleted entry was dropped:\n%s", got)
		}
		if strings.Join(report.Removed, ",") != "gen" {
			t.Fatalf("Removed = %v, want [gen]", report.Removed)
		}
	})

	t.Run("a comment above the deleted entry goes with it", func(t *testing.T) {
		existing := "{\n" +
			"  \"mcpServers\": {\n" +
			"    // generated, do not edit\n" +
			"    \"gen\": {\"command\": \"g\", \"_managed_by\": \"agent-marketplace\"}\n" +
			"  }\n" +
			"}\n"
		got, _ := mergeOnce(t, existing, `{"mcpServers":{}}`)

		assertEmptyContainer(t, got, "mcpServers")
		if strings.Contains(string(got), "do not edit") {
			t.Fatalf("a comment belonging to the deleted entry survived it:\n%s", got)
		}
	})

	t.Run("an emptied container is still a fixed point", func(t *testing.T) {
		existing := "{\n  \"mcpServers\": {\n    \"gen\": {\"_managed_by\": \"agent-marketplace\"}\n  }\n}\n"
		first, _ := mergeOnce(t, existing, `{"mcpServers":{}}`)
		second, _ := mergeOnce(t, string(first), `{"mcpServers":{}}`)
		if !bytes.Equal(first, second) {
			t.Fatalf("emptied container is not stable:\n%q\nvs\n%q", first, second)
		}
	})
}

// assertEmptyContainer checks that key names a container that is empty and that
// the surrounding document is still parseable JSONC.
func assertEmptyContainer(t *testing.T, got []byte, key string) {
	t.Helper()
	var merged map[string]any
	if err := json.Unmarshal(mustCanonical(t, got), &merged); err != nil {
		t.Fatalf("result is not valid JSON: %v\n%s", err, got)
	}
	container, ok := merged[key].(map[string]any)
	if !ok {
		t.Fatalf("%s is not an object: %T", key, merged[key])
	}
	if len(container) != 0 {
		t.Fatalf("%s = %v, want empty", key, container)
	}
}

// TestMergeWithExistingJSON_RefusesDuplicateKeys is the fail-closed contract.
// The cases cover the real aggregate shapes of all five marker-merge targets;
// each one must be refused with the shared json.duplicate-key code, name the
// line, and never echo the duplicated value.
func TestMergeWithExistingJSON_RefusesDuplicateKeys(t *testing.T) {
	tests := []struct {
		name     string
		existing string
		desired  string
		wantPath string
		wantKey  string
		wantLine int
	}{
		{
			name: "claude mcp headers duplicate",
			existing: "{\n" +
				"  \"mcpServers\": {\n" +
				"    \"user\": {\n" +
				"      \"headers\": {\n" +
				"        \"Authorization\": \"" + mergeFakeSecret + "\",\n" +
				"        \"Authorization\": \"" + mergeFakeSecret + "\"\n" +
				"      }\n" +
				"    }\n" +
				"  }\n" +
				"}\n",
			desired:  `{"mcpServers":{"gen":{"command":"g"}}}`,
			wantPath: "$.mcpServers.user.headers",
			wantKey:  "Authorization",
			wantLine: 6,
		},
		{
			name: "factory settings top-level duplicate",
			existing: "{\n" +
				"  \"hooks\": {\"PostToolUse\": [{\"matcher\": \"a\"}]},\n" +
				"  \"hooks\": {}\n" +
				"}\n",
			desired:  `{"hooks":{"PostToolUse":[{"matcher":"g"}]}}`,
			wantPath: "$",
			wantKey:  "hooks",
			wantLine: 3,
		},
		{
			name: "cursor mcp env duplicate",
			existing: "{\n" +
				"  \"mcpServers\": {\n" +
				"    \"user\": {\"env\": {\"TOKEN\": \"" + mergeFakeSecret + "\", \"TOKEN\": \"other\"}}\n" +
				"  }\n" +
				"}\n",
			desired:  `{"mcpServers":{"gen":{"command":"g"}}}`,
			wantPath: "$.mcpServers.user.env",
			wantKey:  "TOKEN",
			wantLine: 3,
		},
		{
			name: "windsurf duplicate server key",
			existing: "{\n" +
				"  \"mcpServers\": {\n" +
				"    \"docs\": {\"command\": \"safe\"},\n" +
				"    \"docs\": {\"command\": \"rm -rf /\"}\n" +
				"  }\n" +
				"}\n",
			desired:  `{"mcpServers":{"gen":{"command":"g"}}}`,
			wantPath: "$.mcpServers",
			wantKey:  "docs",
			wantLine: 4,
		},
		{
			name: "gemini array element duplicate",
			existing: "{\n" +
				"  \"skills\": [\n" +
				"    {\"name\": \"user\", \"name\": \"user-renamed\"}\n" +
				"  ]\n" +
				"}\n",
			desired:  `{"skills":[{"name":"gen"}]}`,
			wantPath: "$.skills[0]",
			wantKey:  "name",
			wantLine: 3,
		},
		{
			// A duplicate of the ownership marker itself must not decide
			// ownership by last-value-wins either.
			name: "duplicate managed-by marker",
			existing: "{\n" +
				"  \"mcpServers\": {\n" +
				"    \"entry\": {\n" +
				"      \"_managed_by\": \"someone-else\",\n" +
				"      \"_managed_by\": \"agent-marketplace\"\n" +
				"    }\n" +
				"  }\n" +
				"}\n",
			desired:  `{"mcpServers":{}}`,
			wantPath: "$.mcpServers.entry",
			wantKey:  "_managed_by",
			wantLine: 5,
		},
		{
			// A commented-out key is NOT a duplicate: the tokenizer drops
			// comments before the scan, so this must be accepted. Without this
			// case the table above would pass for a parser that simply refuses
			// everything containing the word twice.
			name: "commented-out key is not a duplicate",
			existing: "{\n" +
				"  \"mcpServers\": {\n" +
				"    // \"docs\": {\"command\": \"old\"},\n" +
				"    \"docs\": {\"command\": \"new\"}\n" +
				"  }\n" +
				"}\n",
			desired: `{"mcpServers":{}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _, err := MergeWithExistingJSON([]byte(tt.existing), []byte(tt.desired), markerField, markerValue)
			if tt.wantKey == "" {
				if err != nil {
					t.Fatalf("valid document was refused: %v", err)
				}
				if !strings.Contains(string(got), "// \"docs\"") {
					t.Fatalf("the commented-out key was not preserved:\n%s", got)
				}
				return
			}
			testutil.AssertDuplicateKeyDiagnostic(t, err, "", tt.wantPath, tt.wantKey)
			if got != nil {
				t.Fatalf("refused merge still returned bytes: %q", got)
			}
			coded, _ := diag.AsCodedError(err)
			if coded.Source.Line != tt.wantLine {
				t.Fatalf("source line = %d, want %d", coded.Source.Line, tt.wantLine)
			}
		})
	}
}

// TestMergeWithExistingJSON_RefusesDuplicateKeysInDesired keeps the desired side
// fail-closed too. Compiler output should never contain a duplicate, so this is
// a regression fence rather than a user-facing path.
func TestMergeWithExistingJSON_RefusesDuplicateKeysInDesired(t *testing.T) {
	desired := `{"mcpServers":{"gen":{"env":{"TOKEN":"` + mergeFakeSecret + `","TOKEN":"other"}}}}`
	got, _, err := MergeWithExistingJSON([]byte(`{"mcpServers":{}}`), []byte(desired), markerField, markerValue)
	testutil.AssertDuplicateKeyDiagnostic(t, err, "", "$.mcpServers.gen.env", "TOKEN")
	if got != nil {
		t.Fatalf("refused merge still returned bytes: %q", got)
	}
}

// TestMergeWithExistingJSON_RejectsMalformedExisting keeps the malformed case
// fail-closed as well: the write path must not fall back to overwriting a file
// it could not read.
func TestMergeWithExistingJSON_RejectsMalformedExisting(t *testing.T) {
	for _, existing := range []string{"{", "{\"a\":}", "[1,2]", "{\"a\":1,}", "{\"a\":1} {\"b\":2}"} {
		if _, _, err := MergeWithExistingJSON([]byte(existing), []byte(`{"mcpServers":{}}`), markerField, markerValue); err == nil {
			t.Fatalf("malformed existing JSON %q was accepted", existing)
		}
	}
}

// TestMergeWithExistingJSON_ReportsSurviveThePatch proves the report the caller
// renders is unaffected by the move to concrete-syntax patching, for every
// merge shape.
func TestMergeWithExistingJSON_ReportsSurviveThePatch(t *testing.T) {
	tests := []struct {
		name                                string
		existing, desired                   string
		wantAdded, wantRemoved, wantSameSet string
	}{
		{
			name:        "keyed object",
			existing:    `{"mcpServers":{"user":{"command":"u"},"stale":{"_managed_by":"agent-marketplace"},"same":{"command":"s","_managed_by":"agent-marketplace"}}}`,
			desired:     `{"mcpServers":{"same":{"command":"s"},"fresh":{"command":"f"},"user":{"command":"other"}}}`,
			wantAdded:   "fresh",
			wantRemoved: "stale",
			wantSameSet: "same,user",
		},
		{
			name:        "array container",
			existing:    `{"skills":[{"name":"user"},{"name":"stale","_managed_by":"agent-marketplace"},{"name":"same","_managed_by":"agent-marketplace"}]}`,
			desired:     `{"skills":[{"name":"same"},{"name":"fresh"},{"name":"user"}]}`,
			wantAdded:   "fresh",
			wantRemoved: "stale",
			wantSameSet: "same,user",
		},
		{
			name:        "map of arrays",
			existing:    `{"hooks":{"PostToolUse":[{"matcher":"g","_managed_by":"agent-marketplace"}],"PreToolUse":[{"matcher":"old","_managed_by":"agent-marketplace"}]}}`,
			desired:     `{"hooks":{"PostToolUse":[{"matcher":"g"}],"Stop":[{"matcher":"new"}]}}`,
			wantAdded:   "Stop",
			wantRemoved: "PreToolUse",
			wantSameSet: "PostToolUse",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, report := mergeOnce(t, tt.existing, tt.desired)
			if got := strings.Join(report.Added, ","); got != tt.wantAdded {
				t.Errorf("Added = %q, want %q", got, tt.wantAdded)
			}
			if got := strings.Join(report.Removed, ","); got != tt.wantRemoved {
				t.Errorf("Removed = %q, want %q", got, tt.wantRemoved)
			}
			if got := strings.Join(report.Unchanged, ","); got != tt.wantSameSet {
				t.Errorf("Unchanged = %q, want %q", got, tt.wantSameSet)
			}
		})
	}
}

// mustCanonical strips comments so a preserved document can be handed to
// encoding/json in an assertion.
func mustCanonical(t *testing.T, data []byte) []byte {
	t.Helper()
	out, err := jsonc.Canonicalize("", data)
	if err != nil {
		t.Fatalf("canonicalize %q: %v", data, err)
	}
	return out
}

// factoryGroup renders one Factory rule group the way a user writes it by hand:
// one line, matcher first, optionally carrying our ownership marker.
func factoryGroup(matcher, command string, managed bool) string {
	group := "{\"matcher\": \"" + matcher + "\", \"hooks\": [{\"type\": \"command\", \"command\": \"" + command + "\"}]"
	if managed {
		group += ", \"_managed_by\": \"agent-marketplace\""
	}
	return group + "}"
}

// factoryDesired renders the desired Factory hooks document for one event.
func factoryDesired(event string, groups ...string) string {
	return "{\"" + event + "\":[" + strings.Join(groups, ",") + "]}"
}

// factoryWanted renders one rule group the way renderJSON emits it, which is
// what a group we generate looks like once it is written out.
func factoryWanted(indent, matcher, command string) string {
	return "{\n" +
		indent + "  \"_managed_by\": \"agent-marketplace\",\n" +
		indent + "  \"hooks\": [\n" +
		indent + "    {\n" +
		indent + "      \"command\": \"" + command + "\",\n" +
		indent + "      \"type\": \"command\"\n" +
		indent + "    }\n" +
		indent + "  ],\n" +
		indent + "  \"matcher\": \"" + matcher + "\"\n" +
		indent + "}"
}

// TestMergeWithExistingJSON_UnnamedEntriesHaveAStableIdentity is bead
// agent-marketplace-5exj.28. Factory's hooks/hooks.json holds arrays of rule
// GROUPS, which have no "name" and nothing else a compiler could stamp on them,
// so before .28 every managed group was dropped and re-appended at the end of
// its array on the next sync. The identity is now the group's behavior tuple —
// the canonical bytes of the group without our marker — exactly as
// compile/devin/hook.go identifies a Devin hook.
//
// Each case pins the EXACT output bytes: an unchanged group must not move, a
// changed group must be rewritten where it already is, a new group must land
// after everything that survived, and a superseded group must go.
func TestMergeWithExistingJSON_UnnamedEntriesHaveAStableIdentity(t *testing.T) {
	// The shared destination: our group, the user's group with a comment above
	// it, then our second group. Only a merge that can identify an unnamed
	// group leaves this arrangement alone.
	existing := "{\n" +
		"  \"PreToolUse\": [\n" +
		"    " + factoryGroup("Read", "gen-read", true) + ",\n" +
		"    // mine, keep it in the middle\n" +
		"    " + factoryGroup("Bash", "audit.sh", false) + ",\n" +
		"    " + factoryGroup("Write", "gen-write", true) + "\n" +
		"  ]\n" +
		"}\n"
	head := "{\n  \"PreToolUse\": [\n"
	tail := "\n  ]\n}\n"
	userGroup := "    // mine, keep it in the middle\n    " + factoryGroup("Bash", "audit.sh", false)

	tests := []struct {
		name     string
		existing string
		desired  string
		want     string
	}{
		{
			name:     "unchanged groups keep their slots",
			existing: existing,
			desired: factoryDesired("PreToolUse",
				`{"matcher":"Read","hooks":[{"type":"command","command":"gen-read"}]}`,
				`{"matcher":"Write","hooks":[{"type":"command","command":"gen-write"}]}`),
			want: existing,
		},
		{
			// A changed payload is a different behavior, so the old group is
			// superseded — but it is rewritten IN PLACE, in the slot it already
			// held, so the user's group does not slide up under our comment.
			name:     "a changed group is rewritten in its own slot",
			existing: existing,
			desired: factoryDesired("PreToolUse",
				`{"matcher":"Read","hooks":[{"type":"command","command":"gen-read-v2"}]}`,
				`{"matcher":"Write","hooks":[{"type":"command","command":"gen-write"}]}`),
			want: head +
				"    " + factoryWanted("    ", "Read", "gen-read-v2") + ",\n" +
				userGroup + ",\n" +
				"    " + factoryGroup("Write", "gen-write", true) +
				tail,
		},
		{
			// Nothing of ours is superseded, so there is no slot to reuse and
			// the new group goes last — after the user's entries, never before.
			name:     "a new group is appended after everything that survives",
			existing: existing,
			desired: factoryDesired("PreToolUse",
				`{"matcher":"Read","hooks":[{"type":"command","command":"gen-read"}]}`,
				`{"matcher":"Write","hooks":[{"type":"command","command":"gen-write"}]}`,
				`{"matcher":"Edit","hooks":[{"type":"command","command":"gen-edit"}]}`),
			want: head +
				"    " + factoryGroup("Read", "gen-read", true) + ",\n" +
				userGroup + ",\n" +
				"    " + factoryGroup("Write", "gen-write", true) + ",\n" +
				"    " + factoryWanted("    ", "Edit", "gen-edit") +
				tail,
		},
		{
			name:     "a superseded group is removed and the rest stay put",
			existing: existing,
			desired: factoryDesired("PreToolUse",
				`{"matcher":"Read","hooks":[{"type":"command","command":"gen-read"}]}`),
			want: head +
				"    " + factoryGroup("Read", "gen-read", true) + ",\n" +
				userGroup +
				tail,
		},
		{
			// Two byte-identical managed groups are two slots under one
			// identity. One desired copy claims the first; the second is not
			// desired any more and goes.
			name: "duplicate managed groups are consumed one desired entry at a time",
			existing: head +
				"    " + factoryGroup("Read", "gen-read", true) + ",\n" +
				"    " + factoryGroup("Read", "gen-read", true) +
				tail,
			desired: factoryDesired("PreToolUse",
				`{"matcher":"Read","hooks":[{"type":"command","command":"gen-read"}]}`),
			want: head + "    " + factoryGroup("Read", "gen-read", true) + tail,
		},
		{
			// The user's own unnamed groups are never identified as ours, so a
			// desired group that happens to look exactly like one is added
			// rather than silently adopting the user's line.
			name: "a user group that looks like ours is not adopted",
			existing: head +
				"    " + factoryGroup("Read", "gen-read", false) +
				tail,
			desired: factoryDesired("PreToolUse",
				`{"matcher":"Read","hooks":[{"type":"command","command":"gen-read"}]}`),
			want: head +
				"    " + factoryGroup("Read", "gen-read", false) + ",\n" +
				"    " + factoryWanted("    ", "Read", "gen-read") +
				tail,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := mergeOnce(t, tt.existing, tt.desired)
			if string(got) != tt.want {
				t.Fatalf("merged bytes =\n%s\nwant\n%s", got, tt.want)
			}
			// Whatever the case did, doing it again must change nothing.
			again, _ := mergeOnce(t, string(got), tt.desired)
			if !bytes.Equal(got, again) {
				t.Fatalf("not a fixed point:\n%s\nvs\n%s", got, again)
			}
		})
	}
}

// TestMergeWithExistingJSON_HookEventArraysKeepManagedOrder covers the OTHER
// hook shape — the {"hooks": {event: [...]}} envelope Claude, Cursor, and
// Gemini emit — which bead agent-marketplace-5exj.26 reported as the same class
// of churn: mergeMapOfObjectArrays moved every managed entry to the end of its
// event array on every sync. Both shapes now share one implementation, so the
// guarantee is the same one: surviving entries keep their slots.
func TestMergeWithExistingJSON_HookEventArraysKeepManagedOrder(t *testing.T) {
	existing := "{\n" +
		"  \"hooks\": {\n" +
		"    \"PreToolUse\": [\n" +
		"      {\"command\": \"gen\", \"type\": \"command\", \"_managed_by\": \"agent-marketplace\"},\n" +
		"      // mine\n" +
		"      {\"command\": \"mine\", \"type\": \"command\"}\n" +
		"    ]\n" +
		"  }\n" +
		"}\n"

	tests := []struct {
		name    string
		desired string
		want    string
	}{
		{
			name:    "unchanged entry keeps the first slot",
			desired: `{"hooks":{"PreToolUse":[{"command":"gen","type":"command"}]}}`,
			want:    existing,
		},
		{
			name:    "changed entry is rewritten in the first slot",
			desired: `{"hooks":{"PreToolUse":[{"command":"gen2","type":"command"}]}}`,
			want: "{\n" +
				"  \"hooks\": {\n" +
				"    \"PreToolUse\": [\n" +
				"      {\n" +
				"        \"_managed_by\": \"agent-marketplace\",\n" +
				"        \"command\": \"gen2\",\n" +
				"        \"type\": \"command\"\n" +
				"      },\n" +
				"      // mine\n" +
				"      {\"command\": \"mine\", \"type\": \"command\"}\n" +
				"    ]\n" +
				"  }\n" +
				"}\n",
		},
		{
			name:    "an emptied event keeps the user entry and drops ours",
			desired: `{"hooks":{"PreToolUse":[]}}`,
			want: "{\n" +
				"  \"hooks\": {\n" +
				"    \"PreToolUse\": [\n" +
				"      // mine\n" +
				"      {\"command\": \"mine\", \"type\": \"command\"}\n" +
				"    ]\n" +
				"  }\n" +
				"}\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := mergeOnce(t, existing, tt.desired)
			if string(got) != tt.want {
				t.Fatalf("merged bytes =\n%s\nwant\n%s", got, tt.want)
			}
			again, _ := mergeOnce(t, string(got), tt.desired)
			if !bytes.Equal(got, again) {
				t.Fatalf("not a fixed point:\n%s\nvs\n%s", got, again)
			}
		})
	}
}

func TestMergeWithExistingJSONPreservesUnselectedHookMembers(t *testing.T) {
	for _, value := range []string{`"keep"`, `false`, `1.2300`, `null`, `{"owner" : "me"}`, `[]`} {
		t.Run(value, func(t *testing.T) {
			member := `"opaque" : ` + value
			existing := "{\n  \"hooks\": {\n    // handwritten metadata\n    " + member + ",\n" + `"PreToolUse":[{"command":"old","_managed_by":"agent-marketplace"}],` + `"PostToolUse":[{"command":"manual"}],` + `"Stop":[{"command":"stale","_managed_by":"agent-marketplace"}]` + "\n  }\n}\n"
			desired := `{"hooks":{"PreToolUse":[{"command":"new"}]}}`
			got, report := mergeOnce(t, existing, desired)
			if !bytes.Contains(got, []byte(member)) || !bytes.Contains(got, []byte("// handwritten metadata")) {
				t.Errorf("unselected user member or concrete syntax lost: %s", got)
			}
			for _, want := range []string{`"new"`, `"manual"`} {
				if !bytes.Contains(got, []byte(want)) {
					t.Errorf("required content %s missing: %s", want, got)
				}
			}
			for _, removed := range []string{`"old"`, `"stale"`} {
				if bytes.Contains(got, []byte(removed)) {
					t.Errorf("managed update/prune did not occur: %s", got)
				}
			}
			if strings.Join(report.Removed, ",") != "Stop" {
				t.Errorf("wrong removal report: %+v", report)
			}
			again, second := mergeOnce(t, string(got), desired)
			if !bytes.Equal(got, again) || len(second.Added) != 0 || len(second.Removed) != 0 {
				t.Fatalf("second merge was not byte-idempotent: %+v\n%s", second, again)
			}
		})
	}
}
