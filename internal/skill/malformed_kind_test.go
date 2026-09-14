package skill

import (
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestMalformedKindLabelDefaultsUnknownKindsToSkill(t *testing.T) {
	for _, test := range []struct {
		kind string
		want string
	}{
		{kind: "rule", want: "rule"},
		{kind: "", want: "skill"},
		{kind: "future-kind", want: "skill"},
	} {
		if got := MalformedKindLabel(MalformedSkill{Kind: test.kind}); got != test.want {
			t.Errorf("MalformedKindLabel(%q) = %q, want %q", test.kind, got, test.want)
		}
	}
	if got := MalformedKind(MalformedSkill{Kind: "rule"}); got != ir.KindRule {
		t.Fatalf("MalformedKind(rule) = %v, want rule", got)
	}
}
