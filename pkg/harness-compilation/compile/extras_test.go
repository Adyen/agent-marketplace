package compile

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

func TestHookExtraKeysReservesUnicodeAliasesDeterministically(t *testing.T) {
	extra := map[string]any{"COMMAND": "one", "hook\u017f": "blocked", "Foo": "one", "foo": "two"}
	canonical := CanonicalFields("command")
	before := map[string]any{}
	for key, value := range extra {
		before[key] = value
	}
	for i := 0; i < 10; i++ {
		keys, diagnostics := HookExtraKeys(extra, canonical, TargetClaude, "probe", diag.SourceRef{})
		if !reflect.DeepEqual(keys, []string{"Foo", "foo"}) || !diag.HasCode(diagnostics, diag.CodeCanonicalFieldOverridesExtra) {
			t.Fatalf("iteration %d: keys=%#v diagnostics=%#v", i, keys, diagnostics)
		}
	}
	if !reflect.DeepEqual(extra, before) || !reflect.DeepEqual(canonical, CanonicalFields("command")) {
		t.Fatalf("inputs mutated: extra=%#v canonical=%#v", extra, canonical)
	}
}

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
