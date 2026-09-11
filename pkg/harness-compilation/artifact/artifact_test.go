package artifact

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/internal/testutil"
)

func TestValidatePath(t *testing.T) {
	tests := []struct {
		path    string
		wantErr bool
	}{
		{"skills/my-skill/SKILL.md", false},
		{"commands/build.md", false},
		{".windsurf/workflows/test.md", false},
		{"", true},                  // empty
		{"/absolute/path", true},    // absolute
		{"../escape", true},         // parent traversal
		{"foo/../../bar", true},     // double parent
		{"foo/../bar", true},        // single parent component
		{"a/b/c", false},            // normal nested
		{"hooks/hooks.json", false}, // normal flat
	}

	for _, tt := range tests {
		err := ValidatePath(tt.path)
		if (err != nil) != tt.wantErr {
			t.Errorf("ValidatePath(%q) = %v, wantErr %v", tt.path, err, tt.wantErr)
		}
	}
}

func TestArtifactValidate(t *testing.T) {
	a := Artifact{Path: "valid/path.txt", Bytes: []byte("hello")}
	if err := a.Validate(); err != nil {
		t.Errorf("valid artifact: %v", err)
	}

	b := Artifact{Path: "", Bytes: []byte("hello")}
	if err := b.Validate(); err == nil {
		t.Error("empty-path artifact should fail validation")
	}

	c := Artifact{Path: "../bad", Bytes: []byte("hello")}
	if err := c.Validate(); err == nil {
		t.Error("parent-traversal artifact should fail validation")
	}
}

func TestReconciliationPolicyValidation(t *testing.T) {
	tests := []struct {
		name     string
		artifact Artifact
		wantErr  bool
	}{
		{name: "replace", artifact: Artifact{Path: "out.txt"}},
		{name: "marker", artifact: Artifact{Path: "out.json", Fragment: true, Reconciliation: ReconciliationPolicy{Mode: ReconciliationMarker, MarkerField: "_managed_by", MarkerValue: "marketplace"}}},
		{name: "external", artifact: Artifact{Path: "shared.json", Fragment: true, Reconciliation: ReconciliationPolicy{Mode: ReconciliationExternal}}},
		{name: "marker without fragment", artifact: Artifact{Path: "out.json", Reconciliation: ReconciliationPolicy{Mode: ReconciliationMarker, MarkerField: "_managed_by", MarkerValue: "marketplace"}}, wantErr: true},
		{name: "external with marker", artifact: Artifact{Path: "shared.json", Fragment: true, Reconciliation: ReconciliationPolicy{Mode: ReconciliationExternal, MarkerField: "_managed_by", MarkerValue: "marketplace"}}, wantErr: true},
		{name: "partial marker metadata", artifact: Artifact{Path: "out.json", Fragment: true, Reconciliation: ReconciliationPolicy{Mode: ReconciliationMarker, MarkerField: "_managed_by"}}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.artifact.ValidatePolicy()
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidatePolicy() = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestMergeFragments_PassThrough(t *testing.T) {
	input := []Artifact{
		{Path: "skills/a.md", Bytes: []byte("skill-a")},
		{Path: "skills/b.md", Bytes: []byte("skill-b")},
	}
	result, err := MergeFragments(input)
	if err != nil {
		t.Fatalf("MergeFragments: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 artifacts, got %d", len(result))
	}
}

func TestMergeFragmentsRejectsDuplicateNestedJSONKeys(t *testing.T) {
	input := []Artifact{{Path: "hooks.json", Fragment: true, FragmentKey: "hooks", Bytes: []byte(`{"hooks":{"PreCommit":[{"metadata":{"name":"first","name":"second"}}]}}`)}}
	_, err := MergeFragments(input)
	testutil.AssertDuplicateKeyDiagnostic(t, err, "hooks.json", "$.hooks.PreCommit[0].metadata", "name")
}

func TestMergeFragments_JSONFragments(t *testing.T) {
	frag1 := map[string]any{"mcpServers": map[string]any{"server1": map[string]any{"command": "npx"}}}
	frag2 := map[string]any{"mcpServers": map[string]any{"server2": map[string]any{"command": "node"}}}
	b1, _ := json.Marshal(frag1)
	b2, _ := json.Marshal(frag2)

	input := []Artifact{
		{Path: "mcp.json", Bytes: b1, Fragment: true, FragmentKey: "mcp"},
		{Path: "mcp.json", Bytes: b2, Fragment: true, FragmentKey: "mcp"},
	}

	result, err := MergeFragments(input)
	if err != nil {
		t.Fatalf("MergeFragments: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 merged artifact, got %d", len(result))
	}

	var merged map[string]any
	if err := json.Unmarshal(result[0].Bytes, &merged); err != nil {
		t.Fatalf("unmarshal merged: %v", err)
	}

	servers, ok := merged["mcpServers"].(map[string]any)
	if !ok {
		t.Fatal("mcpServers not a map")
	}
	if _, ok := servers["server1"]; !ok {
		t.Error("server1 missing from merged output")
	}
	if _, ok := servers["server2"]; !ok {
		t.Error("server2 missing from merged output")
	}
}

func TestMergeFragments_TextFragments(t *testing.T) {
	input := []Artifact{
		{Path: "GEMINI.md", Bytes: []byte("# Section A"), Fragment: true, FragmentKey: "gemini"},
		{Path: "GEMINI.md", Bytes: []byte("# Section B"), Fragment: true, FragmentKey: "gemini"},
	}

	result, err := MergeFragments(input)
	if err != nil {
		t.Fatalf("MergeFragments: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 merged artifact, got %d", len(result))
	}

	expected := "# Section A\n# Section B"
	if string(result[0].Bytes) != expected {
		t.Errorf("merged text = %q, want %q", string(result[0].Bytes), expected)
	}
}

func TestMergeFragments_Mixed(t *testing.T) {
	// One fragment group + one non-fragment artifact.
	frag := map[string]any{"hooks": []any{"pre-commit"}}
	b1, _ := json.Marshal(frag)

	input := []Artifact{
		{Path: "skills/a.md", Bytes: []byte("skill"), Fragment: false},
		{Path: "hooks.json", Bytes: b1, Fragment: true, FragmentKey: "hooks"},
	}

	result, err := MergeFragments(input)
	if err != nil {
		t.Fatalf("MergeFragments: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 artifacts, got %d", len(result))
	}
}

func TestMergeFragments_EmptyInput(t *testing.T) {
	result, err := MergeFragments(nil)
	if err != nil {
		t.Fatalf("MergeFragments(nil): %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected 0 artifacts, got %d", len(result))
	}
}

func TestMergeFragments_RejectsDifferentFragmentKeys(t *testing.T) {
	_, err := MergeFragments([]Artifact{
		{Path: "settings.json", Bytes: []byte(`{"a":1}`), Fragment: true, FragmentKey: "hooks"},
		{Path: "settings.json", Bytes: []byte(`{"b":2}`), Fragment: true, FragmentKey: "manifest"},
	})
	if err == nil || !strings.Contains(err.Error(), "different FragmentKey") {
		t.Fatalf("MergeFragments error = %v, want different FragmentKey error", err)
	}
}

func TestMergeFragments_RejectsMixedJSONAndText(t *testing.T) {
	_, err := MergeFragments([]Artifact{
		{Path: "settings.json", Bytes: []byte(`{"a":1}`), Fragment: true, FragmentKey: "settings"},
		{Path: "settings.json", Bytes: []byte("not json"), Fragment: true, FragmentKey: "settings"},
	})
	if err == nil || !strings.Contains(err.Error(), "cannot mix JSON and text") {
		t.Fatalf("MergeFragments error = %v, want mixed-strategy error", err)
	}
}

func TestMergeFragments_DeduplicatesIdenticalMCPServerDefinitions(t *testing.T) {
	input := []Artifact{
		{Path: "mcp.json", Bytes: []byte(`{"mcpServers":{"docs":{"command":"npx","args":["-y","docs"]}}}`), Fragment: true, FragmentKey: "mcp"},
		{Path: "mcp.json", Bytes: []byte(`{"mcpServers":{"docs":{"command":"npx","args":["-y","docs"]}}}`), Fragment: true, FragmentKey: "mcp"},
	}
	result, err := MergeFragments(input)
	if err != nil {
		t.Fatalf("MergeFragments: %v", err)
	}
	var merged map[string]any
	if err := json.Unmarshal(result[0].Bytes, &merged); err != nil {
		t.Fatalf("unmarshal merged: %v", err)
	}
	servers := merged["mcpServers"].(map[string]any)
	if len(servers) != 1 {
		t.Fatalf("MCP server count = %d, want 1", len(servers))
	}
}

// TestMergeFragments_DedupeFragmentEntries covers the opt-in entry collapse
// that Devin hooks rely on: semantically identical object entries collapse,
// entries that differ in any member do not, and the flag is off by default so
// no existing target's accumulation behavior changes.
func TestMergeFragments_DedupeFragmentEntries(t *testing.T) {
	// Deliberately different key ORDER in the two identical entries: the
	// comparison must be semantic, not textual.
	entryA := `{"E":[{"command":"guard.sh","type":"command"}]}`
	entryAReordered := `{"E":[{"type":"command","command":"guard.sh"}]}`
	entryB := `{"E":[{"command":"other.sh","type":"command"}]}`

	fragment := func(body string, dedupe bool) Artifact {
		return Artifact{Path: "hooks.v1.json", Bytes: []byte(body), Fragment: true, FragmentKey: "hooks.v1.json", DedupeFragmentEntries: dedupe}
	}

	tests := []struct {
		name  string
		input []Artifact
		want  string
	}{
		{
			name:  "identical entries collapse when dedupe is on",
			input: []Artifact{fragment(entryA, true), fragment(entryAReordered, true)},
			want:  "{\n  \"E\": [\n    {\n      \"command\": \"guard.sh\",\n      \"type\": \"command\"\n    }\n  ]\n}",
		},
		{
			name:  "differing entries are both kept",
			input: []Artifact{fragment(entryA, true), fragment(entryB, true)},
			want:  "{\n  \"E\": [\n    {\n      \"command\": \"guard.sh\",\n      \"type\": \"command\"\n    },\n    {\n      \"command\": \"other.sh\",\n      \"type\": \"command\"\n    }\n  ]\n}",
		},
		{
			name:  "duplicates accumulate when dedupe is off",
			input: []Artifact{fragment(entryA, false), fragment(entryAReordered, false)},
			want:  "{\n  \"E\": [\n    {\n      \"command\": \"guard.sh\",\n      \"type\": \"command\"\n    },\n    {\n      \"command\": \"guard.sh\",\n      \"type\": \"command\"\n    }\n  ]\n}",
		},
		{
			name: "the flag is honored for the whole group when any fragment sets it",
			// Prevents the merge from depending on which fragment came first.
			input: []Artifact{fragment(entryA, false), fragment(entryAReordered, true)},
			want:  "{\n  \"E\": [\n    {\n      \"command\": \"guard.sh\",\n      \"type\": \"command\"\n    }\n  ]\n}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := MergeFragments(tt.input)
			if err != nil {
				t.Fatalf("MergeFragments: %v", err)
			}
			if len(result) != 1 {
				t.Fatalf("artifacts = %d, want 1", len(result))
			}
			if got := string(result[0].Bytes); got != tt.want {
				t.Errorf("merged bytes =\n%s\nwant\n%s", got, tt.want)
			}
		})
	}
}

func TestMergeFragments_SortedOutput(t *testing.T) {
	input := []Artifact{
		{Path: "z-last.md", Bytes: []byte("z")},
		{Path: "a-first.md", Bytes: []byte("a")},
		{Path: "m-middle.md", Bytes: []byte("m")},
	}

	result, err := MergeFragments(input)
	if err != nil {
		t.Fatalf("MergeFragments: %v", err)
	}

	for i := 1; i < len(result); i++ {
		if result[i-1].Path > result[i].Path {
			t.Errorf("output not sorted: %q > %q", result[i-1].Path, result[i].Path)
		}
	}
}

func TestMergeFragments_JSONOverride(t *testing.T) {
	frag1 := map[string]any{"key": "old"}
	frag2 := map[string]any{"key": "new"}
	b1, _ := json.Marshal(frag1)
	b2, _ := json.Marshal(frag2)

	input := []Artifact{
		{Path: "out.json", Bytes: b1, Fragment: true, FragmentKey: "out"},
		{Path: "out.json", Bytes: b2, Fragment: true, FragmentKey: "out"},
	}

	result, err := MergeFragments(input)
	if err != nil {
		t.Fatalf("MergeFragments: %v", err)
	}

	var merged map[string]any
	if err := json.Unmarshal(result[0].Bytes, &merged); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if merged["key"] != "new" {
		t.Errorf("expected later fragment to override, got %v", merged["key"])
	}
}

func TestMergeFragments_ConflictingMCPServerDefinitionsRejected(t *testing.T) {
	// Two MCP server fragments define the same server name with different
	// definitions. Silently choosing one would make the result order-dependent.
	frag1 := map[string]any{
		"mcpServers": map[string]any{
			"docs": map[string]any{
				"command": "npx",
				"args":    []any{"-y", "old-server"},
			},
		},
	}
	frag2 := map[string]any{
		"mcpServers": map[string]any{
			"docs": map[string]any{
				"command": "npx",
				"args":    []any{"-y", "new-server"},
			},
		},
	}
	b1, _ := json.Marshal(frag1)
	b2, _ := json.Marshal(frag2)

	input := []Artifact{
		{Path: "mcp.json", Bytes: b1, Fragment: true, FragmentKey: "mcp"},
		{Path: "mcp.json", Bytes: b2, Fragment: true, FragmentKey: "mcp"},
	}

	if _, err := MergeFragments(input); err == nil {
		t.Fatal("MergeFragments should reject conflicting MCP server definitions")
	} else if !strings.Contains(err.Error(), `conflicting MCP server definition for "docs"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMergeFragments_ObjectArrayAccumulates(t *testing.T) {
	// Two plugin.json fragments each contributing one skill entry.
	// The "skills" array is an array of objects, so entries should
	// accumulate (concatenate) rather than last-wins.
	frag1 := map[string]any{"skills": []any{map[string]any{"name": "alpha"}}}
	frag2 := map[string]any{"skills": []any{map[string]any{"name": "beta"}}}
	b1, _ := json.Marshal(frag1)
	b2, _ := json.Marshal(frag2)

	input := []Artifact{
		{Path: "plugin.json", Bytes: b1, Fragment: true, FragmentKey: "plugin"},
		{Path: "plugin.json", Bytes: b2, Fragment: true, FragmentKey: "plugin"},
	}

	result, err := MergeFragments(input)
	if err != nil {
		t.Fatalf("MergeFragments: %v", err)
	}

	var merged map[string]any
	if err := json.Unmarshal(result[0].Bytes, &merged); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	skills := merged["skills"].([]any)
	if len(skills) != 2 {
		t.Fatalf("skills length = %d, want 2 (accumulated)", len(skills))
	}
}

func TestMergeWithExistingJSONRejectsDuplicateNestedKeys(t *testing.T) {
	duplicate := []byte(`{"mcpServers":{"server":{"env":{"TOKEN":"first","TOKEN":"second"}}}}`)
	_, _, err := MergeWithExistingJSON(duplicate, []byte(`{"mcpServers":{}}`), "_managed_by", "agent-marketplace")
	testutil.AssertDuplicateKeyDiagnostic(t, err, "", "$.mcpServers.server.env", "TOKEN")
	_, _, err = MergeWithExistingJSON([]byte(`{"mcpServers":{}}`), duplicate, "_managed_by", "agent-marketplace")
	testutil.AssertDuplicateKeyDiagnostic(t, err, "", "$.mcpServers.server.env", "TOKEN")
}

func TestMergeWithExistingJSON_PreservesUserEntries(t *testing.T) {
	existing := []byte(`{
  "mcpServers": {
    "user-server": {"command": "user-cmd", "env": {"TOKEN": "real-secret"}},
    "old-managed": {"command": "old", "_managed_by": "agent-marketplace"}
  }
}`)

	// Desired wants to add "new-managed" and "user-server" (same name as user entry).
	desired := []byte(`{
  "mcpServers": {
    "user-server": {"command": "managed-cmd", "env": {"TOKEN": "placeholder"}},
    "new-managed": {"command": "new-cmd"}
  }
}`)

	result, report, err := MergeWithExistingJSON(existing, desired, "_managed_by", "agent-marketplace")
	if err != nil {
		t.Fatalf("MergeWithExistingJSON: %v", err)
	}

	var merged map[string]any
	if err := json.Unmarshal(result, &merged); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	servers := merged["mcpServers"].(map[string]any)

	// User entry must be preserved with original values — NOT replaced by managed version.
	userEntry := servers["user-server"].(map[string]any)
	if userEntry["command"] != "user-cmd" {
		t.Errorf("user-server command = %v, want user-cmd (user entry must not be overwritten)", userEntry["command"])
	}
	if userEntry["_managed_by"] != nil {
		t.Error("user-server should NOT have _managed_by marker — it's a user entry")
	}

	// Stale managed entry must be removed.
	if _, ok := servers["old-managed"]; ok {
		t.Error("old-managed should be pruned (stale managed entry)")
	}

	// New managed entry must be added with marker.
	newEntry := servers["new-managed"].(map[string]any)
	if newEntry["_managed_by"] != "agent-marketplace" {
		t.Error("new-managed should have _managed_by marker")
	}
	if newEntry["command"] != "new-cmd" {
		t.Errorf("new-managed command = %v, want new-cmd", newEntry["command"])
	}

	// Report: user-server unchanged (preserved), new-managed added, old-managed removed.
	if !slices.Contains(report.Unchanged, "user-server") {
		t.Errorf("expected user-server in Unchanged, got %v", report.Unchanged)
	}
	if !slices.Contains(report.Added, "new-managed") {
		t.Errorf("expected new-managed in Added, got %v", report.Added)
	}
	if !slices.Contains(report.Removed, "old-managed") {
		t.Errorf("expected old-managed in Removed, got %v", report.Removed)
	}
}

func TestMergeWithExistingJSON_ArrayContainer_MergesByName(t *testing.T) {
	// plugin.json uses array-of-objects containers keyed by "name".
	existing := []byte(`{
  "skills": [
    {"name": "user-skill", "description": "hand-written"},
    {"name": "stale-managed", "description": "old", "_managed_by": "agent-marketplace"},
    {"name": "keep-managed", "description": "v1", "_managed_by": "agent-marketplace"}
  ]
}`)
	desired := []byte(`{
  "skills": [
    {"name": "new-managed", "description": "fresh"},
    {"name": "keep-managed", "description": "v2"},
    {"name": "user-skill", "description": "should-not-overwrite"}
  ]
}`)

	result, report, err := MergeWithExistingJSON(existing, desired, "_managed_by", "agent-marketplace")
	if err != nil {
		t.Fatalf("MergeWithExistingJSON: %v", err)
	}

	var merged map[string]any
	if err := json.Unmarshal(result, &merged); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	byName := map[string]map[string]any{}
	for _, el := range merged["skills"].([]any) {
		m := el.(map[string]any)
		byName[m["name"].(string)] = m
	}

	// User entry preserved verbatim, no marker added.
	if got := byName["user-skill"]["description"]; got != "hand-written" {
		t.Errorf("user-skill description = %v, want hand-written (must not overwrite)", got)
	}
	if byName["user-skill"]["_managed_by"] != nil {
		t.Error("user-skill must NOT carry _managed_by marker")
	}
	// Stale managed entry removed.
	if _, ok := byName["stale-managed"]; ok {
		t.Error("stale-managed should be pruned")
	}
	// Existing managed entry updated to v2, marker retained.
	if got := byName["keep-managed"]["description"]; got != "v2" {
		t.Errorf("keep-managed description = %v, want v2", got)
	}
	if byName["keep-managed"]["_managed_by"] != "agent-marketplace" {
		t.Error("keep-managed should retain marker")
	}
	// New managed entry added with marker.
	if byName["new-managed"]["_managed_by"] != "agent-marketplace" {
		t.Error("new-managed should carry marker")
	}

	if !slices.Contains(report.Removed, "stale-managed") {
		t.Errorf("expected stale-managed in Removed, got %v", report.Removed)
	}
	if !slices.Contains(report.Added, "new-managed") {
		t.Errorf("expected new-managed in Added, got %v", report.Added)
	}
	if !slices.Contains(report.Unchanged, "user-skill") {
		t.Errorf("expected user-skill in Unchanged, got %v", report.Unchanged)
	}
}

// TestMergeWithExistingJSON_ArrayContainer_DeterministicOrderAndExtras pins the
// ORDER contract of the array container after bead agent-marketplace-5exj.26:
// every surviving existing entry keeps its original index, and entries that are
// genuinely new are appended after them sorted by name. The previous contract
// ("all managed entries sorted first, user entries pushed to the back")
// re-sorted the destination on every sync, which both destroyed the user's
// chosen order and — because jsonc associates a comment with the element that
// follows it — handed the user's comments to our generated entries.
func TestMergeWithExistingJSON_ArrayContainer_DeterministicOrderAndExtras(t *testing.T) {
	// Existing has an unkeyable (no "name") entry that must be preserved, plus
	// a managed entry that is still desired and must not move.
	existing := []byte(`{
  "droids": [
    {"description": "no name - user blob"},
    {"name": "zeta", "_managed_by": "agent-marketplace"}
  ]
}`)
	desired := []byte(`{
  "droids": [
    {"name": "zeta"},
    {"name": "alpha"}
  ]
}`)

	result, _, err := MergeWithExistingJSON(existing, desired, "_managed_by", "agent-marketplace")
	if err != nil {
		t.Fatalf("MergeWithExistingJSON: %v", err)
	}
	// Determinism: re-running with identical inputs yields identical bytes.
	result2, _, err := MergeWithExistingJSON(existing, desired, "_managed_by", "agent-marketplace")
	if err != nil {
		t.Fatalf("MergeWithExistingJSON (2nd): %v", err)
	}
	if string(result) != string(result2) {
		t.Fatalf("non-deterministic output:\n%s\nvs\n%s", result, result2)
	}

	var merged map[string]any
	if err := json.Unmarshal(result, &merged); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	arr := merged["droids"].([]any)
	if len(arr) != 3 {
		t.Fatalf("droids = %v, want three entries", arr)
	}
	// The unkeyable user blob keeps index 0, exactly where the user put it.
	if _, hasName := arr[0].(map[string]any)["name"]; hasName {
		t.Errorf("arr[0] should be the unkeyable user blob, got %v", arr[0])
	}
	// zeta already existed and is still desired: it keeps index 1.
	if got := arr[1].(map[string]any)["name"]; got != "zeta" {
		t.Errorf("arr[1] name = %v, want zeta (an existing entry must not move)", got)
	}
	// alpha is new, so it is appended after everything that was already there
	// even though it sorts before zeta.
	if got := arr[2].(map[string]any)["name"]; got != "alpha" {
		t.Errorf("arr[2] name = %v, want alpha (new entries are appended)", got)
	}
}

// TestMergeWithExistingJSON_ArrayContainer_NewEntriesSortedOnFirstWrite proves
// the appended tail is sorted, which is what makes a first-ever write (no
// existing entries at all) deterministic and identical to the pre-.26 ordering.
func TestMergeWithExistingJSON_ArrayContainer_NewEntriesSortedOnFirstWrite(t *testing.T) {
	desired := []byte(`{"droids":[{"name":"zeta"},{"name":"alpha"},{"name":"mid"}]}`)
	result, _, err := MergeWithExistingJSON(nil, desired, "_managed_by", "agent-marketplace")
	if err != nil {
		t.Fatalf("MergeWithExistingJSON: %v", err)
	}
	var merged map[string]any
	if err := json.Unmarshal(result, &merged); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	var names []string
	for _, el := range merged["droids"].([]any) {
		entry, ok := el.(map[string]any)
		if !ok {
			t.Fatalf("droid entry is not an object: %T", el)
		}
		name, ok := entry["name"].(string)
		if !ok {
			t.Fatalf("droid entry has no string name: %v", entry)
		}
		names = append(names, name)
	}
	if got := strings.Join(names, ","); got != "alpha,mid,zeta" {
		t.Fatalf("droids order = %s, want alpha,mid,zeta", got)
	}
}

func TestMergeWithExistingJSON_ArrayContainer_PreservesUserDuplicate(t *testing.T) {
	// A user-authored entry and a stale managed entry share the same name.
	// Regardless of their order, the user entry must survive untouched. This
	// guards against the keyed map collapsing duplicates and letting the stale
	// managed entry overwrite or delete the user entry.
	cases := []struct {
		name     string
		existing string
	}{
		{
			name:     "user-first",
			existing: `{"skills":[{"name":"x","description":"user"},{"name":"x","description":"old","_managed_by":"agent-marketplace"}]}`,
		},
		{
			name:     "managed-first",
			existing: `{"skills":[{"name":"x","description":"old","_managed_by":"agent-marketplace"},{"name":"x","description":"user"}]}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Desired no longer wants "x": the stale managed copy must go, the
			// user copy must remain.
			desired := []byte(`{"skills":[{"name":"y"}]}`)

			result, _, err := MergeWithExistingJSON([]byte(tc.existing), desired, "_managed_by", "agent-marketplace")
			if err != nil {
				t.Fatalf("MergeWithExistingJSON: %v", err)
			}

			var merged map[string]any
			if err := json.Unmarshal(result, &merged); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			skills, ok := merged["skills"].([]any)
			if !ok {
				t.Fatalf("skills is not an array: %T", merged["skills"])
			}

			var userX, managedX int
			var sawY bool
			for _, el := range skills {
				m, ok := el.(map[string]any)
				if !ok {
					t.Fatalf("skill entry is not an object: %T", el)
				}
				switch m["name"] {
				case "x":
					if m["_managed_by"] == "agent-marketplace" {
						managedX++
					} else {
						userX++
						if m["description"] != "user" {
							t.Errorf("user x description = %v, want user", m["description"])
						}
					}
				case "y":
					sawY = true
				}
			}
			if userX != 1 {
				t.Errorf("expected exactly 1 surviving user x entry, got %d (skills=%s)", userX, result)
			}
			if managedX != 0 {
				t.Errorf("expected stale managed x entry to be removed, got %d (skills=%s)", managedX, result)
			}
			if !sawY {
				t.Errorf("expected new managed y entry, got %s", result)
			}
		})
	}
}

func TestMergeWithExistingJSON_UpdatesExistingManagedEntry(t *testing.T) {
	existing := []byte(`{
  "mcpServers": {
    "my-server": {"command": "v1", "_managed_by": "agent-marketplace"}
  }
}`)
	desired := []byte(`{
  "mcpServers": {
    "my-server": {"command": "v2"}
  }
}`)

	result, report, err := MergeWithExistingJSON(existing, desired, "_managed_by", "agent-marketplace")
	if err != nil {
		t.Fatalf("MergeWithExistingJSON: %v", err)
	}

	var merged map[string]any
	if err := json.Unmarshal(result, &merged); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	servers := merged["mcpServers"].(map[string]any)

	// Existing managed entry should be updated to v2.
	entry := servers["my-server"].(map[string]any)
	if entry["command"] != "v2" {
		t.Errorf("my-server command = %v, want v2", entry["command"])
	}
	if entry["_managed_by"] != "agent-marketplace" {
		t.Error("my-server should retain _managed_by marker")
	}
	if !slices.Contains(report.Added, "my-server") {
		t.Errorf("expected my-server in Added (updated), got %v", report.Added)
	}
}
