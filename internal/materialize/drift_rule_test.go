package materialize

import (
	"testing"

	"github.com/Adyen/agent-marketplace/internal/skill"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestDriftRulePathsPreserveKindIdentity(t *testing.T) {
	skillKey := skill.ResourceKey(ir.KindSkill, "shared")
	ruleKey := skill.ResourceKey(ir.KindRule, "shared")
	for _, rulePath := range []string{
		"rules/shared.md",
		".windsurf/rules/shared.md",
		".cursor/rules/shared.mdc",
		".factory/rules/shared.md",
	} {
		t.Run(rulePath, func(t *testing.T) {
			report := &Report{PerRoot: []TargetReport{
				{Written: []string{"skills/shared/SKILL.md", rulePath}},
			}}
			got := DriftReportFromReport(report, []string{skillKey, ruleKey})
			if len(got.Missing) != 2 {
				t.Fatalf("Missing = %#v, want skill and rule", got.Missing)
			}
			seen := map[string]bool{}
			for _, item := range got.Missing {
				seen[item.Name] = true
			}
			if !seen[skillKey] || !seen[ruleKey] {
				t.Fatalf("Missing identities = %#v, want %q and %q", seen, skillKey, ruleKey)
			}
		})
	}
}

func TestDriftDoesNotRebaseBareRootMarkdownAsRule(t *testing.T) {
	got := resourceKeyForPath("shared.md", map[string]string{}, map[string]bool{})
	if got != "shared.md" {
		t.Fatalf("bare-root markdown identity = %q, want unchanged path", got)
	}
}

func TestDriftDiagnosticWithAmbiguousBareNamePreservesNeutralIdentity(t *testing.T) {
	skillKey := skill.ResourceKey(ir.KindSkill, "shared")
	ruleKey := skill.ResourceKey(ir.KindRule, "shared")
	report := &Report{Diags: []diag.Diagnostic{{
		Code:     "compile.failed",
		Severity: diag.SeverityError,
		Resource: "shared",
	}}}

	got := DriftReportFromReport(report, []string{skillKey, ruleKey})
	if len(got.Broken) != 1 || got.Broken[0].Name != "shared" {
		t.Fatalf("Broken = %#v, want one neutral bare identity", got.Broken)
	}
	if len(got.Synced) != 0 {
		t.Fatalf("Synced = %#v, want ambiguous broken resources withheld from synced", got.Synced)
	}
}
