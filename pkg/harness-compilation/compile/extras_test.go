package compile

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

func TestHookExtraKeysReservesCaseAliasesWithoutMutatingCanonical(t *testing.T) {
	canonical := CanonicalFields("command")
	before := map[string]struct{}{"command": {}}
	extra := map[string]any{
		"COMMAND": "unreviewed-extra-value",
		"Foo":     "one",
		"foo":     "two",
	}
	keys, diagnostics := HookExtraKeys(extra, canonical, TargetClaude, "probe", diag.SourceRef{})
	if !reflect.DeepEqual(canonical, before) {
		t.Fatalf("canonical = %#v, want %#v", canonical, before)
	}
	if !reflect.DeepEqual(keys, []string{"Foo", "foo"}) {
		t.Fatalf("keys = %#v, want Foo and foo", keys)
	}
	if !diag.HasCode(diagnostics, diag.CodeCanonicalFieldOverridesExtra) {
		t.Fatalf("diagnostics = %#v, want canonical-field diagnostic", diagnostics)
	}
	for _, diagnostic := range diagnostics {
		if strings.Contains(diagnostic.Message, "unreviewed-extra-value") || strings.Contains(diagnostic.Hint, "unreviewed-extra-value") {
			t.Fatalf("diagnostic leaked extra value: %#v", diagnostic)
		}
	}
}
