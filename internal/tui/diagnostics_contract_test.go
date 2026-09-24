package tui

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
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
	if strings.Contains(footer, "in sync") || !strings.Contains(footer, "! 1 errors") {
		t.Fatalf("hard failure did not prioritize the persistent error: %q", footer)
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

func TestLatestFailureReplacesPreviousFailureForSameOperation(t *testing.T) {
	first := errors.New("first failure")
	latest := errors.New("latest failure")
	var diagnostics diagnosticStore
	diagnostics.recordOperationFailure(operationSync, diagnosticSync, first)
	diagnostics.recordOperationFailure(operationSync, diagnosticSync, latest)
	if len(diagnostics.operations) != 1 || !errors.Is(diagnostics.operations[0].err, latest) {
		t.Fatalf("retained operation diagnostics = %+v", diagnostics.operations)
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

func TestBrokenSnapshotRowsCanOpenMutationModal(t *testing.T) {
	r := mkRow("unsafe", withProject)
	m := modelWithSelectedRow(t, r)
	m.rowsStale = true
	m.pane = paneList

	updated, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeySpace})
	m = updated.(Model)
	if m.pane != paneToggleModal {
		t.Fatalf("rows under hard snapshot errors did not open the toggle modal: pane %v", m.pane)
	}
	if m.modalSkill.Artifact.Name != r.Artifact.Name {
		t.Fatalf("modal captured %q, want %q", m.modalSkill.Artifact.Name, r.Artifact.Name)
	}
	if cmd != nil {
		t.Fatal("opening the toggle modal must not emit a command")
	}
	out := renderToggleModal(m)
	for _, action := range []string{"toggle global", "toggle project", "toggle local", "toggle skill exclude"} {
		if !strings.Contains(out, action) {
			t.Fatalf("toggle modal omits %q: %q", action, out)
		}
	}
}

func TestReadSnapshotRetainsPartialRowsAfterCatalogFailure(t *testing.T) {
	repoRoot := t.TempDir()
	homeDir := t.TempDir()
	healthyRoot := t.TempDir()
	failedRoot := t.TempDir()
	induced := errors.New("induced catalog failure")

	restoreOrg := swapLoadOrgConfig(func() (*config.Config, error) { return nil, nil })
	t.Cleanup(restoreOrg)
	globalCfg := &config.Config{
		APIVersion: config.APIVersionV2,
		Harnesses:  []string{"factory"},
		Registries: []config.RegistryEntry{
			{URL: healthyRoot, Skills: config.KindSelector{Include: []string{"healthy"}}},
			{URL: failedRoot, Skills: config.KindSelector{Include: []string{"missing"}}},
		},
	}
	restoreGlobal := swapLoadGlobalConfig(func() (*config.Config, error) { return globalCfg, nil })
	t.Cleanup(restoreGlobal)
	restoreRepo := swapLoadRepoConfig(func() (*config.Config, error) { return nil, nil })
	t.Cleanup(restoreRepo)
	restoreLocal := swapLoadLocalConfig(func() (*config.LocalConfig, error) { return nil, nil })
	t.Cleanup(restoreLocal)
	previousMaterialize := runMaterialize
	runMaterialize = func(context.Context, string, []resolver.ResolvedArtifact, ...materialize.RunOption) (*materialize.Report, error) {
		return &materialize.Report{}, nil
	}
	t.Cleanup(func() { runMaterialize = previousMaterialize })
	previousDiscover := discoverCatalog
	discoverCatalog = func(dir, sourceHarness string) (catalogInfo, error) {
		if dir == failedRoot {
			return catalogInfo{}, induced
		}
		return catalogInfo{Entries: []catalog.Entry{{Name: "healthy", Path: filepath.Join(dir, "healthy")}}}, nil
	}
	t.Cleanup(func() { discoverCatalog = previousDiscover })

	snapshot := readSnapshot(context.Background(), repoRoot, homeDir, catalogLoadRefresh)
	if snapshot.driftAssessment != driftFailed {
		t.Fatalf("drift assessment = %v, want failed", snapshot.driftAssessment)
	}
	if errs := diagnosticErrors(snapshot.diagnostics); !slices.ContainsFunc(errs, func(err error) bool { return errors.Is(err, induced) }) {
		t.Fatalf("hard catalog failure not retained in diagnostics: %v", errs)
	}
	if len(snapshot.rows) != 2 {
		t.Fatalf("partial snapshot retained %d rows, want healthy+missing", len(snapshot.rows))
	}
	var healthy, missing *row
	for i := range snapshot.rows {
		switch snapshot.rows[i].Artifact.Name {
		case "healthy":
			healthy = &snapshot.rows[i]
		case "missing":
			missing = &snapshot.rows[i]
		}
	}
	if healthy == nil || missing == nil {
		t.Fatalf("expected healthy and missing rows, got %+v", snapshot.rows)
	}
	if !healthy.EnabledGlobal {
		t.Fatal("healthy row lost its global enablement")
	}
	var failedEntry *config.RegistryEntry
	for i := range snapshot.mergedCfg.Registries {
		if snapshot.mergedCfg.Registries[i].URL == failedRoot {
			failedEntry = &snapshot.mergedCfg.Registries[i]
			break
		}
	}
	if failedEntry == nil {
		t.Fatal("loaded config lost the failed registry entry")
	}
	if !missing.EnabledGlobal {
		t.Fatal("missing row lost its global enablement")
	}
	if missing.Artifact.Kind != ir.KindSkill {
		t.Fatalf("missing row kind = %v, want skill", missing.Artifact.Kind)
	}
	if missing.RegistryURL != failedEntry.URL || missing.RegistryPath != failedEntry.Path || missing.RegistryRef != failedEntry.Ref {
		t.Fatalf("missing row registry identity = %q/%q/%q, want %q/%q/%q",
			missing.RegistryURL, missing.RegistryPath, missing.RegistryRef, failedEntry.URL, failedEntry.Path, failedEntry.Ref)
	}
	if missing.Artifact.Source != failedEntry.Source || missing.Artifact.V4Kind != failedEntry.V4Kind || missing.Artifact.CatalogKey != failedEntry.CatalogKey {
		t.Fatalf("missing row lost source/v4 identity: %+v vs %+v", missing.Artifact, *failedEntry)
	}
	if missing.UnavailableReason != unavailableCatalogReason {
		t.Fatalf("missing row unavailable reason = %q, want %q", missing.UnavailableReason, unavailableCatalogReason)
	}
	if missing.Glyph() != "[!]" {
		t.Fatalf("missing row glyph = %q, want [!]", missing.Glyph())
	}
	if !strings.Contains(missing.StatusText(), "unavailable") {
		t.Fatalf("missing row status = %q", missing.StatusText())
	}
	if missing.DetailSource != nil {
		t.Fatal("missing row captured a detail source")
	}
	detail := renderDetailFullContent(*missing, 80)
	if !strings.Contains(detail, "No verified content is available") {
		t.Fatalf("missing row detail = %q", detail)
	}
	if !strings.Contains(detail, "Press space") {
		t.Fatalf("missing row detail omits the toggle-modal recovery action: %q", detail)
	}
}

func TestAppendUnavailableSelectionRows(t *testing.T) {
	cfg := &config.Config{Registries: []config.RegistryEntry{{
		URL:    "/reg",
		Source: config.UserPersonal,
		Skills: config.KindSelector{Include: []string{"missing"}},
	}}}
	via := &resolver.SelectedNotFoundError{Kind: ir.KindSkill, Name: "missing", URL: "/reg", Via: "collection:tools"}
	if rows := appendUnavailableSelectionRows(nil, cfg, nil, []error{via}, nil); len(rows) != 0 {
		t.Fatalf("collection-derived error synthesized %d rows", len(rows))
	}
	unmatched := &resolver.SelectedNotFoundError{Kind: ir.KindSkill, Name: "unselected", URL: "/reg"}
	direct := &resolver.SelectedNotFoundError{Kind: ir.KindSkill, Name: "missing", URL: "/reg"}
	rows := appendUnavailableSelectionRows(nil, cfg, nil, []error{direct, direct, unmatched}, nil)
	if len(rows) != 1 {
		t.Fatalf("synthesized %d rows, want exactly one for the duplicate direct error", len(rows))
	}
	if rows[0].UnavailableReason == "" {
		t.Fatal("synthesized row carries no unavailable reason")
	}

	sameIdentity := &config.Config{Registries: []config.RegistryEntry{
		{URL: "/reg", Source: config.UserPersonal},
		{URL: "/reg", Source: config.RepoCommitted, Required: true, Skills: config.KindSelector{Include: []string{"missing"}}},
	}}
	rows = appendUnavailableSelectionRows(nil, sameIdentity, nil, []error{direct}, nil)
	if len(rows) != 1 {
		t.Fatalf("synthesized %d rows for same-identity entries, want 1", len(rows))
	}
	if rows[0].Artifact.Source != config.RepoCommitted {
		t.Fatalf("synthesized row source = %v, want the directly selecting RepoCommitted entry", rows[0].Artifact.Source)
	}
	if !rows[0].EnabledProject || rows[0].EnabledGlobal {
		t.Fatalf("synthesized row scope flags = global:%v project:%v, want project only", rows[0].EnabledGlobal, rows[0].EnabledProject)
	}
	if !rows[0].Artifact.Required {
		t.Fatal("synthesized row lost the selecting entry's required bit")
	}

	exclCfg := &config.Config{Registries: []config.RegistryEntry{{
		URL:    "/reg",
		Source: config.UserPersonal,
		Skills: config.KindSelector{Include: []string{"top-excluded", "scoped-excluded", "kept"}},
	}}}
	localCfg := &config.LocalConfig{
		Skills: config.KindSelector{Exclude: []string{"top-excluded"}},
		Registries: []config.RegistryEntry{{
			URL:    "/reg",
			Skills: config.KindSelector{Exclude: []string{"scoped-excluded"}},
		}},
	}
	exclErrs := []error{
		&resolver.SelectedNotFoundError{Kind: ir.KindSkill, Name: "top-excluded", URL: "/reg"},
		&resolver.SelectedNotFoundError{Kind: ir.KindSkill, Name: "scoped-excluded", URL: "/reg"},
		&resolver.SelectedNotFoundError{Kind: ir.KindSkill, Name: "kept", URL: "/reg"},
	}
	exclRows := appendUnavailableSelectionRows(nil, exclCfg, localCfg, exclErrs, nil)
	if len(exclRows) != 3 {
		t.Fatalf("synthesized %d rows, want 3", len(exclRows))
	}
	excludedByName := make(map[string]row, len(exclRows))
	for _, r := range exclRows {
		excludedByName[r.Artifact.Name] = r
	}
	for _, name := range []string{"top-excluded", "scoped-excluded"} {
		r, ok := excludedByName[name]
		if !ok {
			t.Fatalf("missing synthesized row for %q", name)
		}
		if !r.IsExcluded || !r.ExcludedLocally {
			t.Fatalf("%q row exclusion flags = IsExcluded:%v ExcludedLocally:%v, want both true", name, r.IsExcluded, r.ExcludedLocally)
		}
	}
	kept, ok := excludedByName["kept"]
	if !ok {
		t.Fatal("missing synthesized row for kept")
	}
	if kept.IsExcluded || kept.ExcludedLocally {
		t.Fatalf("kept row exclusion flags = IsExcluded:%v ExcludedLocally:%v, want both false", kept.IsExcluded, kept.ExcludedLocally)
	}
}
