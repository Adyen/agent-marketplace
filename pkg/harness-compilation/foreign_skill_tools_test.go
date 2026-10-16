package harnesscompile

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

func foreignToolSkillFrontmatter(t *testing.T, result *Result, name string) string {
	t.Helper()
	for _, a := range result.Artifacts {
		if strings.HasSuffix(a.Path, "/"+name+"/SKILL.md") {
			parts := strings.SplitN(string(a.Bytes), "---", 3)
			if len(parts) < 3 {
				t.Fatalf("%s has no frontmatter:\n%s", a.Path, a.Bytes)
			}
			return parts[1]
		}
	}
	t.Fatalf("skill %q not emitted: %+v", name, result.Artifacts)
	return ""
}

func foreignToolHasDiagnostic(result *Result, code string) bool {
	for _, d := range result.Diagnostics {
		if d.Code == code {
			return true
		}
	}
	return false
}

// spec: harness-compilation/devin-and-factory-skill-tool-lists-are-omitted-on-pre-approval-targets
func TestForeignSkillToolListsAreOmittedOnPreApprovalTargets(t *testing.T) {
	sources := map[Harness]fstest.MapFS{
		HarnessDevin:   {".devin/skills/review/SKILL.md": {Data: []byte("---\nname: review\ndescription: Review\nallowed-tools:\n  - read\n  - exec\n---\nInert.\n")}},
		HarnessFactory: {".factory/skills/review/SKILL.md": {Data: []byte("---\nname: review\ndescription: Review\nallowed-tools:\n  - Read\n  - Edit\n---\nInert.\n")}},
	}
	for from, src := range sources {
		for _, to := range []Target{TargetClaude, TargetCursor, TargetGemini} {
			t.Run(string(from)+"->"+string(to), func(t *testing.T) {
				result, err := Convert(src, from, to)
				if err != nil {
					t.Fatal(err)
				}
				if fm := foreignToolSkillFrontmatter(t, result, "review"); strings.Contains(fm, "allowed-tools") {
					t.Fatalf("foreign tool list emitted as a pre-approval:\n%s", fm)
				}
				if !foreignToolHasDiagnostic(result, diag.CodeSkillAllowedToolsOmitted) {
					t.Fatalf("missing %s: %+v", diag.CodeSkillAllowedToolsOmitted, result.Diagnostics)
				}
			})
		}
	}
}

// spec: harness-compilation/same-harness-and-canonical-skill-tool-lists-are-unchanged
func TestSameHarnessAndCanonicalSkillToolListsAreUnchanged(t *testing.T) {
	for _, tc := range []struct {
		from Harness
		to   Target
		src  fstest.MapFS
		want string
	}{
		{HarnessClaude, TargetClaude, fstest.MapFS{"skills/review/SKILL.md": {Data: []byte("---\nname: review\ndescription: Review\nallowed-tools:\n  - Read\n---\nInert.\n")}}, "allowed-tools:\n  - Read\n"},
		{HarnessGemini, TargetGemini, fstest.MapFS{".gemini/skills/review/SKILL.md": {Data: []byte("---\nname: review\ndescription: Review\nallowed-tools:\n  - read_file\n---\nInert.\n")}}, "allowed-tools:\n  - read_file\n"},
	} {
		t.Run(string(tc.from), func(t *testing.T) {
			result, err := Convert(tc.src, tc.from, tc.to)
			if err != nil {
				t.Fatal(err)
			}
			if fm := foreignToolSkillFrontmatter(t, result, "review"); !strings.Contains(fm, tc.want) {
				t.Fatalf("tool list changed, want %q in:\n%s", tc.want, fm)
			}
			if foreignToolHasDiagnostic(result, diag.CodeSkillAllowedToolsOmitted) {
				t.Fatalf("unexpected %s", diag.CodeSkillAllowedToolsOmitted)
			}
		})
	}
}
