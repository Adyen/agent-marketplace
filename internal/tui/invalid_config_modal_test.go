package tui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	tea "github.com/charmbracelet/bubbletea"
)

func seedMarketplaceConsent(t *testing.T, m *Model) {
	t.Helper()
	operation, ok := m.beginOperation(operationMutation)
	if !ok {
		t.Fatal("could not begin marketplace install operation")
	}
	m.marketplaceInstallOperation = operation
	m.marketplacePreparedInstall = &pipeline.PreparedMarketplaceInstall{}
	m.setMarketplaceConsentPreview("verified preview\n" + strings.Repeat("detail line\n", 50))
	m.marketplaceConsentViewport.GotoBottom()
	m.pane = paneMarketplaceConsent
}

func assertConsentCleared(t *testing.T, m Model) {
	t.Helper()
	if m.marketplaceInstallOperation != 0 {
		t.Fatalf("marketplace install operation = %d, want 0", m.marketplaceInstallOperation)
	}
	if m.operations.active != nil {
		t.Fatalf("operation still active: %+v", m.operations.active)
	}
	if m.marketplacePreparedInstall != nil {
		t.Fatal("prepared install survived dismissal")
	}
	if m.marketplaceInstallPreview != "" {
		t.Fatalf("install preview = %q, want empty", m.marketplaceInstallPreview)
	}
	if view := m.marketplaceConsentViewport.View(); strings.TrimSpace(view) != "" {
		t.Fatalf("consent viewport retained content: %q", view)
	}
	if m.marketplaceConsentViewport.YOffset != 0 {
		t.Fatalf("consent viewport offset = %d, want reset to top", m.marketplaceConsentViewport.YOffset)
	}
}

func invalidConfigSnapshot(m Model) snapshotLoadedMsg {
	return snapshotLoadedMsg{
		generation:      m.requests.active(requestSnapshot),
		driftAssessment: driftFailed,
		mergedCfg:       &config.Config{},
		diagnostics: []tuiDiagnostic{
			snapshotDiagnostic(diagnosticError, diagnosticConfig, errors.New("config parse failed")),
		},
	}
}

func TestInvalidConfigSnapshotDismissesStaleMutationPanes(t *testing.T) {
	cases := []struct {
		name    string
		pane    pane
		consent bool
	}{
		{name: "toggle", pane: paneToggleModal},
		{name: "harness", pane: paneHarnessModal},
		{name: "marketplace consent", pane: paneMarketplaceConsent, consent: true},
		{name: "migration", pane: paneMigrationModal},
		{name: "init", pane: paneInitModal},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := modelWithSelectedRow(t, mkRow("unsafe", withProject))
			m.authoritativeRows = m.rows
			m.pane = tc.pane
			if tc.consent {
				seedMarketplaceConsent(t, &m)
			}

			updated, _ := m.Update(invalidConfigSnapshot(m))
			m = updated.(Model)
			if !m.configInvalid {
				t.Fatal("config-error snapshot did not mark the model config-invalid")
			}
			if m.pane != paneList {
				t.Fatalf("stale mutation pane survived the invalid-config snapshot: pane %v", m.pane)
			}
			if tc.consent {
				assertConsentCleared(t, m)
			}
		})
	}
}

func TestInvalidConfigKeyDismissesStaleMutationPanes(t *testing.T) {
	cases := []struct {
		name    string
		pane    pane
		key     tea.KeyMsg
		consent bool
	}{
		{name: "toggle", pane: paneToggleModal, key: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}}},
		{name: "harness", pane: paneHarnessModal, key: tea.KeyMsg{Type: tea.KeyEnter}},
		{name: "marketplace consent", pane: paneMarketplaceConsent, key: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}}, consent: true},
		{name: "migration", pane: paneMigrationModal, key: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}}},
		{name: "init", pane: paneInitModal, key: tea.KeyMsg{Type: tea.KeyEnter}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := modelWithSelectedRow(t, mkRow("unsafe", withProject))
			m.authoritativeRows = m.rows
			m.configInvalid = true
			m.pane = tc.pane
			if tc.consent {
				seedMarketplaceConsent(t, &m)
			}

			updated, cmd := m.handleKey(tc.key)
			m = updated.(Model)
			if m.pane != paneList {
				t.Fatalf("write key did not dismiss the stale mutation pane: pane %v", m.pane)
			}
			if cmd == nil {
				t.Fatal("dismissed pane returned no editor guidance")
			}
			if msg, ok := cmd().(toastMsg); !ok || !strings.Contains(msg.text, "edit config") {
				t.Fatalf("dismissal guidance = %#v, want an edit-config toast", cmd())
			}
			if m.operations.active != nil {
				t.Fatalf("write key began or retained an operation: %+v", m.operations.active)
			}
			if tc.consent {
				assertConsentCleared(t, m)
			}
		})
	}
}

func TestInvalidConfigPreservesNonMutationPanes(t *testing.T) {
	m := modelWithSelectedRow(t, mkRow("unsafe", withProject))
	m.configInvalid = true
	m.pane = paneEditConfigModal
	updated, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(Model)
	if m.pane != paneEditConfigModal {
		t.Fatalf("configInvalid closed the recovery editor: pane %v", m.pane)
	}
	if cmd != nil {
		t.Fatal("navigation key in the editor emitted a command")
	}

	verified := mkRow("unsafe", withProject)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("verified body"), 0o644); err != nil {
		t.Fatal(err)
	}
	verified.Artifact.Path = dir
	m = modelWithSelectedRow(t, verified)
	m.width = 120
	m.height = 30
	m.configInvalid = true
	m.enterDetailPane()
	if m.pane != paneDetail {
		t.Fatalf("verified row did not open the detail pane: pane %v", m.pane)
	}
	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(Model)
	if m.pane != paneDetail {
		t.Fatalf("configInvalid closed the detail pane: pane %v", m.pane)
	}
}
