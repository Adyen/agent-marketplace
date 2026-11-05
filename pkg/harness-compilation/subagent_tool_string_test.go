package harnesscompile

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

// Claude documents a subagent's tools and disallowedTools as a YAML list or a
// comma-separated string, so the string spelling must restrict exactly as the
// list spelling does on every target.

func readerSubagent(fields string) fstest.MapFS {
	return fstest.MapFS{"agents/reader.md": {Data: []byte("---\nname: reader\ndescription: Reader\n" + fields + "\n---\nRead carefully.\n")}}
}

// spec: harness-compilation/comma-separated-subagent-tool-strings-become-separate-tools
func TestCommaSeparatedSubagentToolStringsBecomeSeparateTools(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		target Target
		want   []string
	}{
		{TargetClaude, []string{"Read", "Grep"}},
		{TargetFactory, []string{"Read", "Grep"}},
		{TargetGemini, []string{"read_file", "read_many_files", "grep_search"}},
	} {
		t.Run(string(tc.target), func(t *testing.T) {
			t.Parallel()
			result, err := Convert(readerSubagent("tools: Read, Grep"), HarnessClaude, tc.target)
			if err != nil {
				t.Fatal(err)
			}
			requireNativeSubagentTools(t, result, "tools", tc.want)
		})
	}

	t.Run("disallowedTools", func(t *testing.T) {
		t.Parallel()
		result, err := Convert(readerSubagent("disallowedTools: Bash, Write"), HarnessClaude, TargetClaude)
		if err != nil {
			t.Fatal(err)
		}
		requireNativeSubagentTools(t, result, "disallowedTools", []string{"Bash", "Write"})
	})
}

// spec: harness-compilation/cursor-source-subagent-tool-strings-become-separate-tools
func TestCursorSourceSubagentToolStringsBecomeSeparateTools(t *testing.T) {
	t.Parallel()
	src := fstest.MapFS{".cursor/agents/reader.md": {Data: []byte("---\nname: reader\ndescription: Reader\ntools: Read, Grep\n---\nRead carefully.\n")}}
	result, err := Convert(src, HarnessCursor, TargetClaude)
	if err != nil {
		t.Fatal(err)
	}
	if foreignToolHasDiagnostic(result, diag.CodeSubagentClaudeToolUnknown) {
		t.Fatalf("Cursor tool string suppressed on Claude: %+v", result.Diagnostics)
	}
	requireNativeSubagentTools(t, result, "tools", []string{"Read", "Grep"})
}

// spec: harness-compilation/parentheses-keep-a-scoped-subagent-tool-in-one-piece
func TestParenthesesKeepAScopedSubagentToolInOnePiece(t *testing.T) {
	t.Parallel()
	for name, fields := range map[string]string{
		"string": "tools: Agent(worker, researcher), Read",
		"list":   "tools:\n  - Agent(worker, researcher)\n  - Read",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			result, err := Convert(readerSubagent(fields), HarnessClaude, TargetClaude)
			if err != nil {
				t.Fatal(err)
			}
			requireNativeSubagentTools(t, result, "tools", []string{"Agent(worker, researcher)", "Read"})
		})
	}
}

// spec: harness-compilation/a-backslash-makes-the-next-character-literal
func TestBackslashMakesTheNextCharacterLiteral(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		fields string
		want   []string
	}{
		{`tools: Bash(echo \)), Read`, []string{`Bash(echo \))`, "Read"}},
		{`tools: Read, Bash(a\), b)`, []string{"Read", `Bash(a\), b)`}},
		{`tools: Read\, Grep`, []string{`Read\, Grep`}},
	} {
		t.Run(tc.fields, func(t *testing.T) {
			t.Parallel()
			result, err := Convert(readerSubagent(tc.fields), HarnessClaude, TargetClaude)
			if err != nil {
				t.Fatal(err)
			}
			requireNativeSubagentTools(t, result, "tools", tc.want)
		})
	}
}

// A value that is not a well-formed list keeps the single entry earlier versions
// produced, so the subagent still loads and a lock that lists it still verifies.
// spec: harness-compilation/a-subagent-tool-string-that-is-not-well-formed-is-kept-as-a-single-entry
func TestMalformedSubagentToolStringIsKeptAsASingleEntry(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		fields, key string
		want        []string
	}{
		{`tools: ""`, "tools", []string{""}},
		{`tools: " , "`, "tools", []string{" , "}},
		{"tools: Read, Bash(git diff", "tools", []string{"Read, Bash(git diff"}},
		{"tools: Read), Grep", "tools", []string{"Read), Grep"}},
		{`tools: Read, Grep\`, "tools", []string{`Read, Grep\`}},
		{"disallowedTools: Bash(rm", "disallowedTools", []string{"Bash(rm"}},
	} {
		src := readerSubagent(tc.fields)
		src["agents/sibling.md"] = &fstest.MapFile{Data: []byte("---\nname: sibling\ndescription: Sibling\ntools: Read, Grep\n---\nSibling.\n")}
		t.Run(tc.fields+"/claude", func(t *testing.T) {
			t.Parallel()
			result, err := Convert(src, HarnessClaude, TargetClaude)
			if err != nil {
				t.Fatal(err)
			}
			requireNativeSubagentTools(t, result, tc.key, tc.want)
			if foreignToolHasDiagnostic(result, diag.CodeIngestClaudeLoadSubagentFailed) {
				t.Fatalf("subagent refused at ingestion: %+v", result.Diagnostics)
			}
			var sibling bool
			for _, output := range result.Artifacts {
				sibling = sibling || strings.HasSuffix(output.Path, "/sibling.md")
			}
			if !sibling {
				t.Fatalf("valid sibling not emitted: %+v", result.Artifacts)
			}
		})
		if tc.key != "tools" {
			continue
		}
		// Factory and Gemini keep refusing the combined name at compile time.
		for target, code := range map[Target]string{TargetFactory: diag.CodeSubagentFactoryToolUnknown, TargetGemini: diag.CodeSubagentGeminiToolUnknown} {
			t.Run(tc.fields+"/"+string(target), func(t *testing.T) {
				t.Parallel()
				result, err := Convert(src, HarnessClaude, target)
				if err != nil {
					t.Fatal(err)
				}
				if !foreignToolHasDiagnostic(result, code) || foreignToolHasDiagnostic(result, diag.CodeIngestClaudeLoadSubagentFailed) {
					t.Fatalf("want %s without an ingestion failure: %+v", code, result.Diagnostics)
				}
			})
		}
	}

	// An empty string is not an inert empty list on Cursor, which has no tool
	// list and refuses any explicit restriction it cannot enforce.
	t.Run(`cursor/tools: ""`, func(t *testing.T) {
		t.Parallel()
		result, err := Convert(readerSubagent(`tools: ""`), HarnessClaude, TargetCursor)
		if err != nil {
			t.Fatal(err)
		}
		if !foreignToolHasDiagnostic(result, diag.CodeSubagentCursorToolUnknown) {
			t.Fatalf("want %s: %+v", diag.CodeSubagentCursorToolUnknown, result.Diagnostics)
		}
	})

	// The key Devin ignores must not cost a profile its enforced allowed-tools.
	t.Run("devin/allowed-tools", func(t *testing.T) {
		t.Parallel()
		src := fstest.MapFS{".devin/agents/reader.md": {Data: []byte("---\nname: reader\ndescription: Reader\nallowed-tools: [read]\ntools: Read, Bash(git diff\n---\nRead carefully.\n")}}
		result, err := Convert(src, HarnessDevin, TargetDevin)
		if err != nil {
			t.Fatal(err)
		}
		requireNativeSubagentTools(t, result, "allowed-tools", []string{"read"})
	})
}
