package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	tea "github.com/charmbracelet/bubbletea"
)

func TestHardSnapshotFailureNeverRendersInSync(t *testing.T) {
	m := NewModel(t.TempDir(), t.TempDir())
	m.loading = false
	m.width = 80
	m.height = 24
	updated, _ := m.Update(snapshotLoadedMsg{
		generation:      m.requests.active(requestSnapshot),
		driftAssessment: driftFailed,
		mergedCfg:       &config.Config{},
		diagnostics: []tuiDiagnostic{
			snapshotDiagnostic(diagnosticError, diagnosticMaterialization, errors.New("dry run failed")),
		},
	})
	m = updated.(Model)
	footer := renderFooter(m)
	if strings.Contains(footer, "in sync") || !strings.Contains(footer, "drift unavailable") {
		t.Fatalf("hard failure rendered a clean drift verdict: %q", footer)
	}
	if !m.rowsStale {
		t.Fatal("hard snapshot failure did not mark rows stale")
	}
}

func TestReadSnapshotPropagatesMaterializationDryRunError(t *testing.T) {
	induced := errors.New("induced dry-run failure")
	previousMaterialize := runMaterialize
	runMaterialize = func(context.Context, string, []resolver.ResolvedArtifact, ...materialize.RunOption) (*materialize.Report, error) {
		return nil, induced
	}
	t.Cleanup(func() { runMaterialize = previousMaterialize })

	restoreOrg := swapLoadOrgConfig(func() (*config.Config, error) { return nil, nil })
	t.Cleanup(restoreOrg)
	restoreGlobal := swapLoadGlobalConfig(func() (*config.Config, error) {
		return &config.Config{APIVersion: config.APIVersionV2, Harnesses: []string{"factory"}}, nil
	})
	t.Cleanup(restoreGlobal)
	restoreRepo := swapLoadRepoConfig(func() (*config.Config, error) { return nil, nil })
	t.Cleanup(restoreRepo)
	restoreLocal := swapLoadLocalConfig(func() (*config.LocalConfig, error) { return nil, nil })
	t.Cleanup(restoreLocal)

	snapshot := readSnapshot(context.Background(), t.TempDir(), t.TempDir(), catalogLoadCached)
	if snapshot.driftAssessment != driftFailed {
		t.Fatalf("drift assessment = %v, want failed", snapshot.driftAssessment)
	}
	if errs := diagnosticErrors(snapshot.diagnostics); len(errs) != 1 || !errors.Is(errs[0], induced) {
		t.Fatalf("dry-run failure not propagated: %v", errs)
	}
}

func TestOperationFailureSurvivesSnapshotUntilSameOperationRecovers(t *testing.T) {
	warning := errors.New("snapshot warning")
	syncFailure := errors.New("sync failure")
	m := NewModel(t.TempDir(), t.TempDir())
	updated, _ := m.Update(snapshotLoadedMsg{
		generation:      m.requests.active(requestSnapshot),
		driftAssessment: driftReady,
		mergedCfg:       &config.Config{},
		diagnostics: []tuiDiagnostic{
			snapshotDiagnostic(diagnosticWarning, diagnosticCatalog, warning),
		},
	})
	m = updated.(Model)

	updated, _ = m.Update(syncDoneMsg{err: syncFailure})
	m = updated.(Model)
	if len(m.errs) != 1 || !errors.Is(m.errs[0], syncFailure) {
		t.Fatalf("sync failure not retained: %v", m.errs)
	}

	generation := m.requests.active(requestSnapshot)
	updated, _ = m.Update(snapshotLoadedMsg{
		generation:      generation,
		driftAssessment: driftReady,
		mergedCfg:       &config.Config{},
	})
	m = updated.(Model)
	if len(m.errs) != 1 || !errors.Is(m.errs[0], syncFailure) {
		t.Fatalf("snapshot erased operation failure: %v", m.errs)
	}
	if len(m.warnings) != 0 {
		t.Fatalf("replacement snapshot retained stale warning: %v", m.warnings)
	}

	updated, _ = m.Update(syncDoneMsg{})
	m = updated.(Model)
	if len(m.errs) != 0 {
		t.Fatalf("successful sync did not recover its failure: %v", m.errs)
	}
}

func TestSnapshotDiagnosticsDeduplicateStableIdentity(t *testing.T) {
	duplicate := errors.New("same failure")
	m := NewModel(t.TempDir(), t.TempDir())
	updated, _ := m.Update(snapshotLoadedMsg{
		generation:      m.requests.active(requestSnapshot),
		driftAssessment: driftFailed,
		diagnostics: []tuiDiagnostic{
			snapshotDiagnostic(diagnosticError, diagnosticConfig, duplicate),
			snapshotDiagnostic(diagnosticError, diagnosticConfig, duplicate),
		},
	})
	m = updated.(Model)
	if len(m.errs) != 1 {
		t.Fatalf("duplicate diagnostics were not collapsed: %v", m.errs)
	}
}

func TestBrokenSnapshotRowsCannotOpenMutationModal(t *testing.T) {
	r := mkRow("unsafe", withProject)
	m := modelWithSelectedRow(t, r)
	m.rowsStale = true
	m.pane = paneList

	updated, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeySpace})
	m = updated.(Model)
	if m.pane != paneList {
		t.Fatalf("broken rows opened pane %v", m.pane)
	}
	if cmd == nil {
		t.Fatal("broken rows did not surface recovery guidance")
	}
	toast, ok := cmd().(toastMsg)
	if !ok || !strings.Contains(toast.text, "reload") {
		t.Fatalf("unexpected recovery guidance: %#v", toast)
	}
}
