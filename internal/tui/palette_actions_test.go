package tui

import "testing"

// TestPaletteActionsExposeStableUniqueIdentities is intentionally
// stdlib-only: copying this file alone onto a tree without the palette
// implementation must fail on the missing paletteActionID/paletteActions
// symbols rather than on any missing module or helper.
func TestPaletteActionsExposeStableUniqueIdentities(t *testing.T) {
	actions := paletteActions()
	if len(actions) == 0 {
		t.Fatalf("palette must expose a non-empty action set")
	}
	seen := map[paletteActionID]bool{}
	for _, a := range actions {
		if a.id == "" {
			t.Fatalf("palette action %q has an empty identity", a.label)
		}
		if a.label == "" {
			t.Fatalf("palette action %q has an empty label", a.id)
		}
		if seen[a.id] {
			t.Fatalf("duplicate palette action identity %q", a.id)
		}
		seen[a.id] = true
	}
}

// TestPaletteExcludesModalOnlyActions pins the exposed action set: list
// actions and prefix-fold identities only — never modal scope or
// confirmation keys.
func TestPaletteExcludesModalOnlyActions(t *testing.T) {
	want := map[string]bool{
		"search": true, "detail": true, "toggle": true,
		"reload": true, "sync": true, "update": true,
		"harness": true, "edit-config": true,
		"help": true, "diagnostics": true,
		"tab-scope": true, "tab-registry": true, "tab-status": true,
		"fold-toggle": true, "fold-all": true, "expand-all": true,
		"quit": true,
	}
	for _, a := range paletteActions() {
		if !want[string(a.id)] {
			t.Fatalf("palette exposes unexpected action identity %q", a.id)
		}
		delete(want, string(a.id))
	}
	for missing := range want {
		t.Fatalf("palette is missing action identity %q", missing)
	}
}
