package skill

import (
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestRuleKindParsingLabelsAndConfigKeys(t *testing.T) {
	for _, input := range []string{"rule", "rules", " RULE "} {
		kind, ok := ParseKind(input)
		if !ok || kind != ir.KindRule {
			t.Fatalf("ParseKind(%q) = %v, %v; want KindRule, true", input, kind, ok)
		}
	}
	if got := KindLabel(ir.KindRule); got != "rule" {
		t.Fatalf("KindLabel(rule) = %q, want rule", got)
	}
	if got := ConfigKeyForKind(ir.KindRule); got != "rules" {
		t.Fatalf("ConfigKeyForKind(rule) = %q, want rules", got)
	}
	if got := ResourceKey(ir.KindRule, "style"); got != "rule\x00style" {
		t.Fatalf("ResourceKey(rule, style) = %q, want kind-aware rule key", got)
	}
}
