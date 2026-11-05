package harnesscompile

// Tool-catalog-driven subagent and skill tool translation (OpenSpec change
// native-hook-integration, task 2.2). These tests exercise the public
// Compile path only.

import (
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func compileSubagentTools(t *testing.T, dst Target, tools []string, opts ...ir.SubagentOption) *Result {
	t.Helper()
	opts = append([]ir.SubagentOption{}, opts...)
	resource := ir.NewSubagentWithOptions("tool-agent", "Tool agent", "body\n", "", tools, "", nil, "", nil, ir.SourceRef{}, opts...)
	result, err := Compile(resource, dst)
	if err != nil {
		t.Fatalf("Compile(subagent, %s): %v", dst, err)
	}
	return result
}

func artifactBody(t *testing.T, r *Result) string {
	t.Helper()
	if len(r.Artifacts) != 1 {
		t.Fatalf("want exactly one artifact, got %d (diags: %+v)", len(r.Artifacts), r.Diagnostics)
	}
	return string(r.Artifacts[0].Bytes)
}

// spec: harness-tool-catalogs/factory-droids-receive-factory-tool-ids
func TestFactoryDroidsReceiveFactoryToolIDs(t *testing.T) {
	body := artifactBody(t, compileSubagentTools(t, TargetFactory, []string{"Read", "Bash"}))
	if !strings.Contains(body, "- Read") || !strings.Contains(body, "- Execute") {
		t.Fatalf("factory droid tools missing Read/Execute:\n%s", body)
	}
}

// spec: harness-tool-catalogs/write-and-edit-reach-every-factory-file-change-tool
func TestWriteAndEditReachEveryFactoryFileChangeTool(t *testing.T) {
	body := artifactBody(t, compileSubagentTools(t, TargetFactory, []string{"Write", "Edit"}))
	for _, want := range []string{"- Create", "- Edit", "- ApplyPatch"} {
		if !strings.Contains(body, want) {
			t.Fatalf("factory droid tools missing %q:\n%s", want, body)
		}
	}
}

// spec: harness-tool-catalogs/a-combined-tool-is-not-granted-for-part-of-its-scope
func TestCombinedToolNotGrantedForPartOfScope(t *testing.T) {
	body := artifactBody(t, compileSubagentTools(t, TargetFactory, []string{"Edit"}))
	if !strings.Contains(body, "- Edit") {
		t.Fatalf("factory droid tools missing Edit:\n%s", body)
	}
	if strings.Contains(body, "ApplyPatch") {
		t.Fatalf("Edit-only restriction widened to ApplyPatch:\n%s", body)
	}
}

// spec: harness-tool-catalogs/gemini-agents-receive-gemini-tool-names
func TestGeminiAgentsReceiveGeminiToolNames(t *testing.T) {
	body := artifactBody(t, compileSubagentTools(t, TargetGemini, []string{"Read", "Grep"}))
	for _, want := range []string{"read_file", "read_many_files", "grep_search"} {
		if !strings.Contains(body, want) {
			t.Fatalf("gemini agent tools missing %q:\n%s", want, body)
		}
	}
}

// spec: harness-tool-catalogs/cursor-subagents-with-a-tool-list-are-suppressed
func TestCursorSubagentsWithToolListAreSuppressed(t *testing.T) {
	result := compileSubagentTools(t, TargetCursor, []string{"Read"})
	if len(result.Artifacts) != 0 {
		t.Fatalf("cursor subagent with tool list produced artifacts: %+v", result.Artifacts)
	}
	if !diag.HasCode(result.Diagnostics, diag.CodeSubagentCursorToolUnknown) {
		t.Fatalf("want %s diagnostic, got %+v", diag.CodeSubagentCursorToolUnknown, result.Diagnostics)
	}
}

// spec: harness-tool-catalogs/devin-keeps-its-narrow-allowed-tools-vocabulary
func TestDevinKeepsNarrowAllowedToolsVocabulary(t *testing.T) {
	result := compileSubagentTools(t, TargetDevin, []string{"Read", "Write"})
	if !diag.HasCode(result.Diagnostics, diag.CodeSubagentDevinToolUnknown) {
		t.Fatalf("want %s diagnostic, got %+v", diag.CodeSubagentDevinToolUnknown, result.Diagnostics)
	}
	for _, d := range result.Diagnostics {
		if d.Code == diag.CodeSubagentDevinToolUnknown && d.Severity != diag.SeverityWarning {
			t.Fatalf("devin tool-unknown must stay a warning, got %s", d.Severity)
		}
	}
	if len(result.Artifacts) == 0 {
		t.Fatal("devin warning should not suppress the profile artifact")
	}
	if strings.Contains(string(result.Artifacts[0].Bytes), "allowed-tools") {
		t.Fatalf("unrepresentable allowed-tools emitted:\n%s", result.Artifacts[0].Bytes)
	}
}

// spec: harness-tool-catalogs/gemini-wildcards-survive-same-harness-output
func TestGeminiWildcardsSurviveSameHarnessOutput(t *testing.T) {
	body := artifactBody(t, compileSubagentTools(t, TargetGemini,
		[]string{"read_file", "mcp_*"}, ir.WithSubagentToolSource(string(TargetGemini))))
	if !strings.Contains(body, "read_file") || !strings.Contains(body, "mcp_*") {
		t.Fatalf("same-harness gemini tools not preserved verbatim:\n%s", body)
	}
}

// spec: harness-tool-catalogs/renamed-tools-are-accepted-through-aliases
func TestRenamedToolsAcceptedThroughAliases(t *testing.T) {
	// A Gemini-sourced subagent carrying the legacy spelling
	// `search_file_content` compiles to the canonical `grep_search` on
	// Gemini and to `Grep` on Claude.
	gemini := artifactBody(t, compileSubagentTools(t, TargetGemini,
		[]string{"search_file_content"}, ir.WithSubagentToolSource(string(TargetGemini))))
	if !strings.Contains(gemini, "grep_search") {
		t.Fatalf("legacy gemini alias not canonicalized:\n%s", gemini)
	}
	claudeResult := compileSubagentTools(t, TargetClaude,
		[]string{"grep_search"}, ir.WithSubagentToolSource(string(TargetGemini)))
	var claude string
	for _, art := range claudeResult.Artifacts {
		if strings.HasPrefix(art.Path, "agents/") {
			claude = string(art.Bytes)
		}
	}
	if !strings.Contains(claude, "- Grep") {
		t.Fatalf("gemini tool not translated to Claude name:\n%s", claude)
	}
}
