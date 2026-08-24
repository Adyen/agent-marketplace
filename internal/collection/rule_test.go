package collection

import (
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestParseKindAcceptsRules(t *testing.T) {
	for _, value := range []string{"rule", "rules", " RULE "} {
		kind, err := parseKind(value)
		if err != nil || kind != ir.KindRule {
			t.Fatalf("parseKind(%q) = %v, %v; want KindRule, nil", value, kind, err)
		}
	}
}
