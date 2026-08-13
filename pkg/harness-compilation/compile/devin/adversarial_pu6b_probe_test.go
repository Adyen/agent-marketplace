package devin

import (
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// The three outcomes an allowed-tools restriction can have on Devin. Since bead
// agent-marketplace-pu6b they are no longer equally safe: outcomeOmitted now
// means "installed with Devin's DEFAULT tool set", i.e. the widest possible
// result, where it used to mean "not installed at all".
const (
	outcomeOmitted = "<key omitted: Devin default tool set>"
	outcomeEmpty   = "allowed-tools: []"
)

// TestAllowedToolsBoundaryMap is the whole decision table for every shape that
// can reach resolveAllowedTools, in one place, asserting the RENDERED result
// rather than a diagnostic count.
//
// It exists because the dangerous transitions in this table are invisible from
// any single test: `[""]` and `""` are the same restriction, and `[" Read "]`
// and `" Read "` are the same restriction, but one member of each pair used to
// render "no tools" or "read" while the other rendered "every default tool".
// Anything that moves a row from outcomeEmpty or a mapped list to
// outcomeOmitted is a privilege widening and must fail here.
func TestAllowedToolsBoundaryMap(t *testing.T) {
	canonical := []struct {
		name  string
		tools []string
		want  string
	}{
		{"absent", nil, outcomeOmitted},
		{"explicit empty list", []string{}, outcomeEmpty},
		{"only an empty entry", []string{""}, outcomeEmpty},
		{"only blank entries", []string{"", " ", "\t"}, outcomeEmpty},
		{"only a comma", []string{","}, outcomeEmpty},
		{"one mappable", []string{"Read"}, "allowed-tools:\\n  - read"},
		{"padded mappable", []string{" Read "}, "allowed-tools:\\n  - read"},
		{"duplicates collapse", []string{"Read", "Read"}, "allowed-tools:\\n  - read"},
		{"mappable plus blank", []string{"Read", ""}, "allowed-tools:\\n  - read"},
		{"case variant lower", []string{"read"}, outcomeOmitted},
		{"case variant upper", []string{"READ"}, outcomeOmitted},
		{"unicode look-alike", []string{"Re\u0430d"}, outcomeOmitted},
		{"mixed mappable and not", []string{"Read", "WebSearch"}, outcomeOmitted},
		{"scoped specifier", []string{"Bash(openspec:*)"}, outcomeOmitted},
		{"lone parenthesis", []string{"Bash("}, outcomeOmitted},
	}
	for _, tc := range canonical {
		t.Run("canonical/"+tc.name, func(t *testing.T) {
			skill := ir.NewSkillWithOptions("demo", "Demo skill", "Body\n", nil, nil, testSource,
				ir.WithSkillAllowedTools(tc.tools))
			result, err := compile.Compile(skill, compile.TargetDevin)
			if err != nil {
				t.Fatalf("Compile: %v", err)
			}
			if got := renderedTools(result.Artifacts); got != tc.want {
				t.Errorf("rendered = %q, want %q", got, tc.want)
			}
		})
	}

	extra := []struct {
		name  string
		value any
		want  string
	}{
		{"empty string", "", outcomeEmpty},
		{"whitespace string", "  ", outcomeEmpty},
		{"comma string", ",", outcomeEmpty},
		{"empty list", []any{}, outcomeEmpty},
		{"list of one empty string", []any{""}, outcomeEmpty},
		{"list of blanks", []any{"", " "}, outcomeEmpty},
		{"list with a comma entry", []any{","}, outcomeEmpty},
		{"mappable string", "Read", "allowed-tools:\\n  - read"},
		{"padded string", " Read ", "allowed-tools:\\n  - read"},
		{"padded list entry", []any{" Read "}, "allowed-tools:\\n  - read"},
		{"repeated string", "Read,Read", "allowed-tools:\\n  - read"},
		{"map shape", map[string]any{"read": true}, outcomeOmitted},
		{"number shape", 7, outcomeOmitted},
		{"bool shape", true, outcomeOmitted},
		{"nested list shape", []any{[]any{"Read"}}, outcomeOmitted},
		{"null entry", []any{nil}, outcomeOmitted},
		{"comma-bearing specifier", "Bash(git add, git commit), Read", outcomeOmitted},
	}
	for _, tc := range extra {
		t.Run("extra/"+tc.name, func(t *testing.T) {
			cmd := ir.NewCommand("demo", "Demo command", "Body\n",
				map[string]any{allowedToolsKey: tc.value}, testSource)
			result, err := compile.Compile(cmd, compile.TargetDevin)
			if err != nil {
				t.Fatalf("Compile: %v", err)
			}
			if got := renderedTools(result.Artifacts); got != tc.want {
				t.Errorf("rendered = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestVeryLongRestrictionIsHandledWhole checks that a large list does not
// truncate, reorder, or partially emit: the whole key still goes, and a large
// fully mappable list still renders every mapped tool once.
func TestVeryLongRestrictionIsHandledWhole(t *testing.T) {
	long := make([]string, 0, 2001)
	for i := 0; i < 1000; i++ {
		long = append(long, "Read", "Grep")
	}
	skill := ir.NewSkillWithOptions("demo", "Demo skill", "Body\n", nil, nil, testSource,
		ir.WithSkillAllowedTools(long))
	result, err := compile.Compile(skill, compile.TargetDevin)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if got, want := renderedTools(result.Artifacts), "allowed-tools:\\n  - read\\n  - grep"; got != want {
		t.Errorf("rendered = %q, want %q", got, want)
	}

	withOneBadEntry := append(append([]string{}, long...), "WebSearch")
	skill = ir.NewSkillWithOptions("demo", "Demo skill", "Body\n", nil, nil, testSource,
		ir.WithSkillAllowedTools(withOneBadEntry))
	result, err = compile.Compile(skill, compile.TargetDevin)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if got := renderedTools(result.Artifacts); got != outcomeOmitted {
		t.Errorf("rendered = %q, want %q: one bad entry in a long list must drop the key whole", got, outcomeOmitted)
	}
}

// renderedTools extracts the allowed-tools block from a compiled artifact as a
// single line, so a decision table can assert it directly.
func renderedTools(arts []artifact.Artifact) string {
	if len(arts) == 0 {
		return "<no artifact>"
	}
	body := string(arts[0].Bytes)
	idx := strings.Index(body, allowedToolsKey)
	if idx < 0 {
		return outcomeOmitted
	}
	rest := body[idx:]
	if end := strings.Index(rest, "\n---"); end >= 0 {
		rest = rest[:end]
	}
	return strings.ReplaceAll(rest, "\n", "\\n")
}
