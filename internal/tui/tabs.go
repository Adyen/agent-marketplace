package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// tab represents the active top-level view.
// Scope: skills grouped by enablement scope (global/project/local/available).
// Registry: skills grouped by their source registry.
// Status: skills grouped by sync status (synced / pending / stale / etc).
type tab int

const (
	tabScope tab = iota
	tabRegistry
	tabStatus
)

// allTabs is the ordered list used for rendering and cycling.
var allTabs = []tab{tabScope, tabRegistry, tabStatus}

func (t tab) String() string {
	switch t {
	case tabScope:
		return "Scope"
	case tabRegistry:
		return "Registry"
	case tabStatus:
		return "Status"
	default:
		return "?"
	}
}

// tabViewState captures per-tab navigation state so switching tabs doesn't
// lose the cursor position or scroll offset.
type tabViewState struct {
	cursor     int
	listOffset int
	filter     string
	// Fold state is keyed by section identifier ("global", "project",
	// "registry:<url>", "status:synced", ...) so each tab can fold
	// independently without clobbering other tabs.
	foldedSections map[string]bool
}

// tabStates returns the initial per-tab state map.
func newTabStates() map[tab]*tabViewState {
	states := make(map[tab]*tabViewState, len(allTabs))
	for _, t := range allTabs {
		states[t] = &tabViewState{foldedSections: make(map[string]bool)}
	}
	return states
}

// nextTab returns the next tab in allTabs, wrapping at the end.
func nextTab(current tab) tab {
	for i, t := range allTabs {
		if t == current {
			return allTabs[(i+1)%len(allTabs)]
		}
	}
	return allTabs[0]
}

// prevTab returns the previous tab in allTabs, wrapping at the start.
func prevTab(current tab) tab {
	for i, t := range allTabs {
		if t == current {
			return allTabs[(i-1+len(allTabs))%len(allTabs)]
		}
	}
	return allTabs[0]
}

var (
	styleTabActive = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.ANSIColor(15)).
			Background(paletteAccent).
			Padding(0, 2)

	styleTabInactive = lipgloss.NewStyle().
				Foreground(paletteMuted).
				Padding(0, 2)
)

// renderTabBar returns a single-line string with all tabs, highlighting the
// active tab. Numeric mnemonics are prefixed for discoverability (1/2/3).
func renderTabBar(active tab) string {
	var parts []string
	for i, t := range allTabs {
		label := t.String()
		if i < 9 {
			label = string(rune('1'+i)) + " " + label
		}
		if t == active {
			parts = append(parts, styleTabActive.Render(label))
		} else {
			parts = append(parts, styleTabInactive.Render(label))
		}
	}
	return strings.Join(parts, "")
}
