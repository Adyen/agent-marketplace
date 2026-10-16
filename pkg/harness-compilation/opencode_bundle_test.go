package harnesscompile

import (
	"maps"
	"os"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/internal/opencodehook"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// openCodeHookSource builds a Claude source layout whose PreToolUse event
// carries the requested number of independently named command hooks, plus one
// command that must never be implicated by hook-level bundle decisions.
func openCodeHookSource(t *testing.T, hooks int) fstest.MapFS {
	t.Helper()
	var groups []string
	for range hooks {
		groups = append(groups, `{"matcher":"Bash","hooks":[{"type":"command","command":"printf 1"}]}`)
	}
	return fstest.MapFS{
		"hooks/hooks.json":  &fstest.MapFile{Data: []byte(`{"PreToolUse":[` + strings.Join(groups, ",") + `]}`)},
		"commands/check.md": &fstest.MapFile{Data: []byte("---\ndescription: Check\n---\nCheck")},
	}
}

// openCodeHookGeneration counts the accepted hook declarations and adapter
// copies in a compiled OpenCode bundle result.
func openCodeHookGeneration(t *testing.T, result *Result) (declarations, adapters int) {
	t.Helper()
	for _, a := range result.Artifacts {
		switch {
		case a.Path == opencodehook.AdapterPath:
			adapters++
		case strings.HasPrefix(a.Path, opencodehook.DeclarationsDir+"/") && strings.HasSuffix(a.Path, ".json"):
			declarations++
		}
	}
	return declarations, adapters
}

// spec: opencode-hooks/two-accepted-hooks-share-one-adapter
func TestOpenCodeBundleSharesOneAdapter(t *testing.T) {
	result, err := CompileBundle(openCodeHookSource(t, 2), TargetOpenCode)
	if err != nil {
		t.Fatalf("CompileBundle: %v", err)
	}
	if result.HasErrors() {
		t.Fatalf("two accepted hooks must not conflict: %#v", result.Diagnostics)
	}
	declarations, adapters := openCodeHookGeneration(t, result)
	if declarations != 2 || adapters != 1 {
		t.Fatalf("declarations=%d adapters=%d, want 2 declarations sharing exactly one adapter", declarations, adapters)
	}
	for _, a := range result.Artifacts {
		if a.Path == opencodehook.AdapterPath && string(a.Bytes) != string(opencodehook.Adapter) {
			t.Fatal("published adapter bytes differ from the reviewed fixed adapter")
		}
	}
	// The generation stays data-only: ingesting it back must recover both
	// hook declarations without executing any source code.
	output := fstest.MapFS{}
	for _, a := range result.Artifacts {
		output[a.Path] = &fstest.MapFile{Data: a.Bytes}
	}
	reloaded, err := IngestBundle(output, HarnessOpenCode)
	if err != nil {
		t.Fatalf("re-ingest compiled output: %v", err)
	}
	hooks := 0
	for _, resource := range reloaded.Resources {
		if resource.Kind() == ir.KindHook {
			hooks++
		}
	}
	if hooks != 2 || len(reloaded.Diagnostics) != 0 {
		t.Fatalf("re-ingested generation: hooks=%d diagnostics=%#v, want 2 hooks without diagnostics", hooks, reloaded.Diagnostics)
	}
}

// spec: opencode-hooks/excess-declarations-are-rejected-before-publication
func TestOpenCodeBundleRejectsExcessHookDeclarations(t *testing.T) {
	result, err := CompileBundle(openCodeHookSource(t, int(opencodehook.MaxDeclarationsPerToolEvent)+1), TargetOpenCode)
	if err != nil {
		t.Fatalf("CompileBundle: %v", err)
	}
	declarations, adapters := openCodeHookGeneration(t, result)
	if declarations != 0 || adapters != 0 {
		t.Fatalf("excess declarations published a generation: declarations=%d adapters=%d", declarations, adapters)
	}
	if !result.HasErrors() || !diag.HasCode(result.Diagnostics, diag.CodeHookOpenCodeUnsupported) {
		t.Fatalf("missing %s error: %#v", diag.CodeHookOpenCodeUnsupported, result.Diagnostics)
	}
	for _, d := range result.Diagnostics {
		if d.Code != diag.CodeHookOpenCodeUnsupported {
			continue
		}
		if d.Severity != SeverityError || d.Target != string(TargetOpenCode) {
			t.Fatalf("budget diagnostic: %#v", d)
		}
	}
	if !sliceContains(artifactPaths(result.Artifacts), "commands/check.md") {
		t.Fatalf("unrelated resource was suppressed: %v", artifactPaths(result.Artifacts))
	}
	if len(result.Skipped) != int(opencodehook.MaxDeclarationsPerToolEvent)+1 {
		t.Fatalf("skipped=%d, want one skip per accepted hook declaration", len(result.Skipped))
	}
	for _, skipped := range result.Skipped {
		if skipped.Kind != ir.KindHook || !sliceContains(skipped.Codes, diag.CodeHookOpenCodeUnsupported) {
			t.Fatalf("skip entry does not name the budget refusal: %+v", skipped)
		}
	}

	// The exact budget boundary stays representable.
	boundary, err := CompileBundle(openCodeHookSource(t, int(opencodehook.MaxDeclarationsPerToolEvent)), TargetOpenCode)
	if err != nil {
		t.Fatalf("CompileBundle(boundary): %v", err)
	}
	declarations, adapters = openCodeHookGeneration(t, boundary)
	if boundary.HasErrors() || declarations != int(opencodehook.MaxDeclarationsPerToolEvent) || adapters != 1 {
		t.Fatalf("boundary control: declarations=%d adapters=%d diagnostics=%#v", declarations, adapters, boundary.Diagnostics)
	}
}

// spec: harness-opencode/opencode-selection-reaches-consumer-workflows
// spec: opencode-native-configuration/creation-and-conversion-retain-private-modes
func TestOpenCodeLibraryConversionBothDirections(t *testing.T) {
	source := fstest.MapFS{
		".claude-plugin/plugin.json": &fstest.MapFile{Data: []byte(`{"name":"demo"}`)},
		"skills/demo/SKILL.md":       &fstest.MapFile{Data: []byte("---\nname: demo\ndescription: Demo\n---\nDemo body\n")},
		"commands/check.md":          &fstest.MapFile{Data: []byte("---\ndescription: Check changes\n---\nCheck $ARGUMENTS and $1.\n")},
		"agents/reviewer.md":         &fstest.MapFile{Data: []byte("---\nname: reviewer\ndescription: Review changes\nmodel: sonnet\n---\nReview carefully.\n")},
		"hooks/hooks.json":           &fstest.MapFile{Data: []byte(`{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"printf '{}'"}]}]}`)},
		".mcp.json":                  &fstest.MapFile{Data: []byte(`{"mcpServers":{"docs":{"command":"docs-server"}}}`)},
	}
	result, err := Convert(source, HarnessClaude, HarnessOpenCode)
	if err != nil || diag.HasErrors(result.Diagnostics) {
		t.Fatalf("convert to opencode: %v %v", err, result)
	}
	byPath := map[string][]byte{}
	for _, a := range result.Artifacts {
		byPath[a.Path] = a.Bytes
		if a.Path == "opencode.json" && a.Mode.Perm() != 0o600 {
			t.Fatalf("MCP artifact mode = %v, want private", a.Mode.Perm())
		}
	}
	for _, rel := range []string{"skills/demo/SKILL.md", "commands/check.md", "agents/reviewer.md", opencodehook.AdapterPath, "opencode.json"} {
		if _, ok := byPath[rel]; !ok {
			t.Fatalf("converted artifacts lack %s: %v", rel, byPath)
		}
	}
	if !strings.Contains(string(byPath["commands/check.md"]), "$ARGUMENTS and $1") {
		t.Fatal("command placeholders changed")
	}

	// The shared configuration needs external reconciliation, so Materialize
	// publishes the per-resource files and leaves opencode.json to sync.
	out := t.TempDir()
	if _, err := Materialize(result, out); err == nil {
		t.Fatal("Materialize published the externally reconciled opencode.json")
	}
	resources := &Result{Diagnostics: result.Diagnostics}
	for _, a := range result.Artifacts {
		if a.Path != "opencode.json" {
			resources.Artifacts = append(resources.Artifacts, a)
		}
	}
	if _, err := Materialize(resources, out); err != nil {
		t.Fatalf("materialize resources: %v", err)
	}

	back, err := Convert(os.DirFS(out), HarnessOpenCode, HarnessClaude)
	if err != nil || diag.HasErrors(back.Diagnostics) {
		t.Fatalf("convert back to claude: %v %v", err, back)
	}
	reversed := map[string]string{}
	for _, a := range back.Artifacts {
		reversed[a.Path] = string(a.Bytes)
	}
	for rel, want := range map[string]string{
		"skills/demo/SKILL.md": "Demo body",
		"commands/check.md":    "$ARGUMENTS and $1",
		"agents/reviewer.md":   "Review carefully.",
		"hooks/hooks.json":     `"matcher": "Bash"`,
	} {
		if !strings.Contains(reversed[rel], want) {
			t.Fatalf("reverse conversion lost %s (want %q): %v", rel, want, slices.Sorted(maps.Keys(reversed)))
		}
	}
}
