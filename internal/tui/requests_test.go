package tui

import (
	"errors"
	"strings"
	"testing"
)

func TestRequestStateAllocatesOnlyWhenWorkBegins(t *testing.T) {
	var state requestState
	first := state.begin(requestDetail)
	if first != 1 || state.active(requestDetail) != first {
		t.Fatalf("first detail request = %d, active = %d", first, state.active(requestDetail))
	}
	if cancelled := state.cancel(requestDetail); cancelled != first {
		t.Fatalf("cancelled generation = %d, want %d", cancelled, first)
	}
	if state.next != first {
		t.Fatalf("cancel consumed a generation: next = %d, want %d", state.next, first)
	}
	second := state.begin(requestSnapshot)
	if second != 2 || state.active(requestSnapshot) != second {
		t.Fatalf("snapshot request = %d, active = %d", second, state.active(requestSnapshot))
	}
}

func TestAcceptedRequestCompletionCannotReplay(t *testing.T) {
	m := Model{}
	generation := m.beginRequest(requestSnapshot)
	if !m.acceptsRequest(requestSnapshot, generation) {
		t.Fatal("current completion was rejected")
	}
	if active := m.requests.active(requestSnapshot); active != 0 {
		t.Fatalf("accepted generation remained active: %d", active)
	}
	if m.acceptsRequest(requestSnapshot, generation) {
		t.Fatal("duplicate completion was accepted")
	}
}

func TestDelayedDetailCannotOverwriteNewSelection(t *testing.T) {
	m := modelWithSelectedRow(t, mkRow("alpha", withProject))
	m.width = 80
	m.height = 24
	m.requests = requestState{}

	alphaGeneration := m.beginRequest(requestDetail)
	m.enterDetailPane()
	m.pane = paneList
	m.detailKey = ""
	m.cancelRequest(requestDetail)

	m.rows = []row{mkRow("beta", withProject)}
	m.filteredRows = m.rows
	m.rebuildFlatItems()
	for i := range m.flatItems {
		if m.flatItems[i].Kind == flatArtifact {
			m.cursor = i
			break
		}
	}
	betaGeneration := m.beginRequest(requestDetail)
	m.enterDetailPane()
	m.detailViewport.SetContent("current beta")

	updated, _ := m.Update(detailContentMsg{generation: alphaGeneration, content: "stale alpha"})
	m = updated.(Model)
	if view := m.detailViewport.View(); !strings.Contains(view, "current beta") || strings.Contains(view, "stale alpha") {
		t.Fatalf("stale detail changed current content: %q", view)
	}

	updated, _ = m.Update(detailContentMsg{generation: betaGeneration, content: "fresh beta"})
	m = updated.(Model)
	if view := m.detailViewport.View(); !strings.Contains(view, "fresh beta") {
		t.Fatalf("current detail was not accepted: %q", view)
	}
}

func TestSnapshotGenerationRejectsStaleRowsAndPreservesSelection(t *testing.T) {
	alpha := mkRow("alpha", withProject)
	beta := mkRow("beta", withProject)
	m := modelWithSelectedRow(t, alpha)
	m.rows = []row{alpha, beta}
	m.filteredRows = m.rows
	m.rebuildFlatItems()
	for i, item := range m.flatItems {
		if item.Kind == flatArtifact && item.Row.Artifact.Name == "beta" {
			m.cursor = i
			break
		}
	}
	m.requests = requestState{next: 2, snapshot: 2}

	stale, _ := m.Update(snapshotLoadedMsg{
		generation: 1,
		rows:       []row{mkRow("stale", withProject)},
		errs:       []error{errors.New("stale error")},
	})
	m = stale.(Model)
	if selected, ok := m.selectedRow(); !ok || selected.Artifact.Name != "beta" {
		t.Fatalf("stale snapshot changed selection: %#v, ok=%v", selected, ok)
	}
	if len(m.errs) != 0 {
		t.Fatalf("stale snapshot changed errors: %v", m.errs)
	}

	accepted, _ := m.Update(snapshotLoadedMsg{
		generation: 2,
		rows: []row{
			mkRow("inserted", withProject),
			alpha,
			beta,
		},
	})
	m = accepted.(Model)
	if selected, ok := m.selectedRow(); !ok || selected.Artifact.Name != "beta" {
		t.Fatalf("accepted rebuild lost stable selection: %#v, ok=%v", selected, ok)
	}
}

func TestStaleUpdateSchedulesAuthoritativeSnapshot(t *testing.T) {
	m := NewModel(t.TempDir(), t.TempDir())
	m.requests = requestState{next: 2, snapshot: 2}
	m.syncing = true
	m.errs = []error{errors.New("current error")}

	updated, cmd := m.Update(updateDoneMsg{generation: 1, err: errors.New("stale update")})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("stale update did not schedule an authoritative snapshot")
	}
	if got := m.requests.active(requestSnapshot); got != 3 {
		t.Fatalf("active snapshot generation = %d, want 3", got)
	}
	if len(m.errs) != 1 || m.errs[0].Error() != "current error" {
		t.Fatalf("stale update changed current errors: %v", m.errs)
	}
}
