package catalog

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func writeNestedNativeFixture(t *testing.T, withNative bool) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"valid/skills/review/SKILL.md": "---\nname: review\ndescription: Review changes\n---\nBody",
	}
	if withNative {
		files["native-only/.factory/hooks.json"] = `{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo native"}]}]}`
	}
	for rel, body := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func nestedKinds(entries []Entry) map[string]bool {
	kinds := make(map[string]bool)
	for _, entry := range entries {
		kinds[entry.Kind.String()+"/"+entry.Name] = true
	}
	return kinds
}

func TestLegacyNativeInventoryMasksNestedNativeOnlyContainers(t *testing.T) {
	legacy := IngestOptions{LegacyNativeHookInventory: true}
	control, err := DiscoverWithOptions(writeNestedNativeFixture(t, false), "", legacy)
	if err != nil {
		t.Fatalf("control discover: %v", err)
	}
	withNative, err := DiscoverWithOptions(writeNestedNativeFixture(t, true), "", legacy)
	if err != nil {
		t.Fatalf("frozen discover: %v", err)
	}
	if got, want := nestedKinds(withNative.Entries), nestedKinds(control.Entries); len(got) != len(want) || !got[ir.KindSkill.String()+"/review"] {
		t.Fatalf("frozen inventory = %v, want the control inventory %v", got, want)
	}

	runtime, err := DiscoverWithOptions(writeNestedNativeFixture(t, true), "", IngestOptions{})
	if err != nil {
		t.Fatalf("runtime discover: %v", err)
	}
	kinds := nestedKinds(runtime.Entries)
	if !kinds[ir.KindSkill.String()+"/review"] || !hasHookKind(runtime.Entries) {
		t.Fatalf("runtime inventory = %v, want the skill and the native hook", kinds)
	}
}

func hasHookKind(entries []Entry) bool {
	for _, entry := range entries {
		if entry.Kind == ir.KindHook {
			return true
		}
	}
	return false
}
