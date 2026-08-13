package tui

import (
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	tea "github.com/charmbracelet/bubbletea"
)

func TestRowGlyphEnabled(t *testing.T) {
	r := row{IsAvailable: false}
	if got := r.Glyph(); got != "[x]" {
		t.Fatalf("expected [x] for enabled skill, got %s", got)
	}
}

func TestRowGlyphRequired(t *testing.T) {
	r := row{
		Artifact: resolver.ResolvedArtifact{Required: true, Entry: catalog.Entry{Name: "audit-logging"}},
	}
	if got := r.Glyph(); got != "[R]" {
		t.Fatalf("expected [R] for required skill, got %s", got)
	}
}

func TestRowGlyphAvailable(t *testing.T) {
	r := row{IsAvailable: true}
	if got := r.Glyph(); got != "[ ]" {
		t.Fatalf("expected [ ] for available skill, got %s", got)
	}
}

func TestRowGlyphMalformed(t *testing.T) {
	r := row{IsMalformed: true, MalformedMsg: "bad yaml"}
	if got := r.Glyph(); got != "[!]" {
		t.Fatalf("expected [!] for malformed skill, got %s", got)
	}
}

func TestRowGlyphPendingSync(t *testing.T) {
	r := row{DriftItem: &materialize.DriftItem{Reason: materialize.DriftMissing}}
	if got := r.Glyph(); got != "[*]" {
		t.Fatalf("expected [*] for pending sync, got %s", got)
	}
}

func TestRowGlyphOrphaned(t *testing.T) {
	r := row{DriftItem: &materialize.DriftItem{Reason: materialize.DriftOrphaned}}
	if got := r.Glyph(); got != "[-]" {
		t.Fatalf("expected [-] for orphaned, got %s", got)
	}
}

func TestRowStatusText(t *testing.T) {
	tests := []struct {
		name string
		row  row
		want string
	}{
		{"enabled", row{}, "enabled"},
		{"available", row{IsAvailable: true}, "available"},
		{"excluded", row{IsAvailable: true, IsExcluded: true}, "excluded"},
		{"required", row{Artifact: resolver.ResolvedArtifact{Required: true}}, "required (org)"},
		{"malformed", row{IsMalformed: true, MalformedMsg: "bad"}, "malformed: bad"},
		{"pending", row{DriftItem: &materialize.DriftItem{Reason: materialize.DriftMissing}}, "enabled (pending sync)"},
		{"orphaned", row{DriftItem: &materialize.DriftItem{Reason: materialize.DriftOrphaned}}, "orphaned"},
		{"broken", row{DriftItem: &materialize.DriftItem{Reason: materialize.DriftBroken}}, "broken"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.row.StatusText(); got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

func TestApplyFilter(t *testing.T) {
	rows := []row{
		{Artifact: resolver.ResolvedArtifact{Entry: catalog.Entry{Name: "vm-lifecycle", Tags: []string{"infra"}}}},
		{Artifact: resolver.ResolvedArtifact{Entry: catalog.Entry{Name: "k8s-deploy", Tags: []string{"k8s"}}}},
		{Artifact: resolver.ResolvedArtifact{Entry: catalog.Entry{Name: "firewall-rules", Tags: []string{"security"}}}},
	}

	m := Model{rows: rows}
	filtered := m.applyFilter()
	if len(filtered) != 3 {
		t.Fatalf("expected 3 rows with no filter, got %d", len(filtered))
	}

	m.filter = "k8s"
	filtered = m.applyFilter()
	if len(filtered) != 1 {
		t.Fatalf("expected 1 row matching 'k8s', got %d", len(filtered))
	}

	m.filter = "infra"
	filtered = m.applyFilter()
	if len(filtered) != 1 {
		t.Fatalf("expected 1 row matching 'infra', got %d", len(filtered))
	}
}

func TestStaleRefreshLoadedMsgIsIgnoredAfterReloadStarts(t *testing.T) {
	m := Model{
		rows:         []row{{Artifact: resolver.ResolvedArtifact{Entry: catalog.Entry{Name: "new-state"}}}},
		filteredRows: []row{{Artifact: resolver.ResolvedArtifact{Entry: catalog.Entry{Name: "new-state"}}}},
		refreshing:   true,
		refreshToken: 1,
		keys:         defaultKeyMap,
		help:         newHelpModel(),
	}

	updatedModel, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if cmd == nil {
		t.Fatalf("expected reload key to return a command")
	}

	gotModel := updatedModel.(Model)
	if gotModel.refreshToken != 0 {
		t.Fatalf("expected reload to invalidate pending refresh token, got %d", gotModel.refreshToken)
	}

	stale := refreshLoadedMsg{
		snapshotLoadedMsg: snapshotLoadedMsg{
			rows: []row{{Artifact: resolver.ResolvedArtifact{Entry: catalog.Entry{Name: "stale-state"}}}},
		},
		refreshToken: 1,
	}

	nextModel, _ := gotModel.Update(stale)
	finalModel := nextModel.(Model)

	if finalModel.rows[0].Artifact.Name != "new-state" {
		t.Fatalf("expected stale refresh to be ignored, got %q", finalModel.rows[0].Artifact.Name)
	}
}
