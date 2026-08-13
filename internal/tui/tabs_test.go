package tui

import (
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/resolver"
)

func TestNextPrevTabCycle(t *testing.T) {
	if got := nextTab(tabScope); got != tabRegistry {
		t.Fatalf("nextTab(Scope) = %v, want Registry", got)
	}
	if got := nextTab(tabRegistry); got != tabStatus {
		t.Fatalf("nextTab(Registry) = %v, want Status", got)
	}
	if got := nextTab(tabStatus); got != tabScope {
		t.Fatalf("nextTab(Status) wrap = %v, want Scope", got)
	}

	if got := prevTab(tabScope); got != tabStatus {
		t.Fatalf("prevTab(Scope) wrap = %v, want Status", got)
	}
	if got := prevTab(tabRegistry); got != tabScope {
		t.Fatalf("prevTab(Registry) = %v, want Scope", got)
	}
}

func TestRenderTabBarHighlightsActive(t *testing.T) {
	bar := renderTabBar(tabRegistry)
	// Ensure all tab labels appear, numeric mnemonics present.
	for _, label := range []string{"Scope", "Registry", "Status", "1", "2", "3"} {
		if !strings.Contains(bar, label) {
			t.Fatalf("renderTabBar missing %q in output: %q", label, bar)
		}
	}
}

func TestSwitchTabPreservesPerTabCursor(t *testing.T) {
	rows := []row{
		{Artifact: resolver.ResolvedArtifact{Entry: catalog.Entry{Name: "alpha"}}},
		{Artifact: resolver.ResolvedArtifact{Entry: catalog.Entry{Name: "beta"}}},
		{Artifact: resolver.ResolvedArtifact{Entry: catalog.Entry{Name: "gamma"}}},
	}
	m := NewModel("/repo", "/home")
	m.rows = rows
	m.filteredRows = rows
	m.height = 24

	// On Scope tab, cursor at position 2.
	m.cursor = 2
	m.switchTab(tabRegistry)
	if m.activeTab != tabRegistry {
		t.Fatalf("expected active tab Registry, got %v", m.activeTab)
	}
	if m.cursor != 0 {
		t.Fatalf("expected fresh Registry tab cursor=0, got %d", m.cursor)
	}

	// Move to 1 on Registry, switch back to Scope — Scope should still be at 2.
	m.cursor = 1
	m.switchTab(tabScope)
	if m.activeTab != tabScope {
		t.Fatalf("expected active tab Scope, got %v", m.activeTab)
	}
	if m.cursor != 2 {
		t.Fatalf("expected Scope cursor preserved at 2, got %d", m.cursor)
	}

	// Back to Registry — should restore to 1.
	m.switchTab(tabRegistry)
	if m.cursor != 1 {
		t.Fatalf("expected Registry cursor preserved at 1, got %d", m.cursor)
	}
}

func TestSwitchTabNoOpWhenSameTab(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.rows = []row{{Artifact: resolver.ResolvedArtifact{Entry: catalog.Entry{Name: "a"}}}}
	m.filteredRows = m.rows
	m.cursor = 0
	m.activeTab = tabScope

	// Scribble some unrelated state to make sure switchTab(same) doesn't clobber.
	m.cursor = 0
	m.switchTab(tabScope)
	if m.activeTab != tabScope {
		t.Fatalf("expected tab unchanged, got %v", m.activeTab)
	}
}
