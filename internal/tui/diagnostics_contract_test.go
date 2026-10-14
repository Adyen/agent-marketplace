package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/skill"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/charmbracelet/x/ansi"
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
}

// spec: inspection-outcomes/materialization-dry-run-failure
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

// spec: inspection-outcomes/malformed-resource-warning
func TestReadSnapshotClassifiesMalformedResourceAsWarning(t *testing.T) {
	repoRoot := t.TempDir()
	homeDir := t.TempDir()
	registryRoot := t.TempDir()
	globalCfg := &config.Config{
		APIVersion: config.APIVersionV2,
		Harnesses:  []string{"factory"},
		Registries: []config.RegistryEntry{
			{URL: registryRoot, Skills: config.KindSelector{Include: []string{"healthy"}}},
		},
	}
	restoreOrg := swapLoadOrgConfig(func() (*config.Config, error) { return nil, nil })
	t.Cleanup(restoreOrg)
	restoreGlobal := swapLoadGlobalConfig(func() (*config.Config, error) { return globalCfg, nil })
	t.Cleanup(restoreGlobal)
	restoreRepo := swapLoadRepoConfig(func() (*config.Config, error) { return nil, nil })
	t.Cleanup(restoreRepo)
	restoreLocal := swapLoadLocalConfig(func() (*config.LocalConfig, error) { return nil, nil })
	t.Cleanup(restoreLocal)
	previousDiscover := discoverCatalog
	discoverCatalog = func(dir, sourceHarness string) (catalogInfo, error) {
		if dir != registryRoot {
			return catalogInfo{}, errors.New("unexpected discovery for " + dir)
		}
		return catalogInfo{
			Entries: []catalog.Entry{{Name: "healthy", Path: filepath.Join(dir, "healthy")}},
			Malformed: []skill.MalformedSkill{{
				DirName: "broken-skill",
				Path:    filepath.Join("skills", "broken-skill", "SKILL.md"),
				Kind:    ir.KindSkill.String(),
				Error:   errors.New("yaml: parse failure"),
			}},
		}, nil
	}
	t.Cleanup(func() { discoverCatalog = previousDiscover })

	snapshot := readSnapshot(context.Background(), repoRoot, homeDir, catalogLoadCached)
	if errs := diagnosticErrors(snapshot.diagnostics); len(errs) != 0 {
		t.Fatalf("malformed unselected resource produced error diagnostics: %v", errs)
	}
	var malformedWarning error
	for _, d := range snapshot.diagnostics {
		if d.severity == diagnosticWarning && strings.Contains(d.err.Error(), "broken-skill") {
			malformedWarning = d.err
		}
	}
	if malformedWarning == nil {
		t.Fatalf("malformed resource missing from warning diagnostics: %+v", snapshot.diagnostics)
	}
	if strings.Contains(malformedWarning.Error(), filepath.Join("SKILL.md", "SKILL.md")) {
		t.Fatalf("warning appended a second SKILL.md to a file path: %v", malformedWarning)
	}
	if snapshot.driftAssessment != driftReady {
		t.Fatalf("warning-only malformed resource marked drift failed: %v", snapshot.driftAssessment)
	}
}

func TestReadSnapshotReturnsBeforeMaterializeOnResolveFailure(t *testing.T) {
	materializeRan := false
	previousMaterialize := runMaterialize
	runMaterialize = func(context.Context, string, []resolver.ResolvedArtifact, ...materialize.RunOption) (*materialize.Report, error) {
		materializeRan = true
		return nil, nil
	}
	t.Cleanup(func() { runMaterialize = previousMaterialize })

	// An empty repository root yields the zero Workspace, so Resolve fails
	// with a generic ErrInvalidWorkspace before any loader runs.
	snapshot := readSnapshot(context.Background(), "", t.TempDir(), catalogLoadCached)
	if materializeRan {
		t.Fatal("runMaterialize ran after Resolve returned a generic error")
	}
	if errs := diagnosticErrors(snapshot.diagnostics); !errors.Is(errors.Join(errs...), pipeline.ErrInvalidWorkspace) {
		t.Fatalf("resolve failure not exposed as diagnostics: %v", errs)
	}
	if len(snapshot.errs) == 0 {
		t.Fatal("resolve failure did not populate snapshot errs")
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
	// The failed sync ends when the authoritative reload it scheduled is
	// accepted; only then is its own error recorded as the verdict.
	generation := m.requests.active(requestSnapshot)
	updated, _ = m.Update(snapshotLoadedMsg{
		generation:      generation,
		driftAssessment: driftReady,
		mergedCfg:       &config.Config{},
	})
	m = updated.(Model)
	if len(m.errs) != 1 || !errors.Is(m.errs[0], syncFailure) {
		t.Fatalf("sync failure not retained: %v", m.errs)
	}
	if len(m.warnings) != 0 {
		t.Fatalf("replacement snapshot retained stale warning: %v", m.warnings)
	}

	updated, _ = m.Update(syncDoneMsg{})
	m = updated.(Model)
	generation = m.requests.active(requestSnapshot)
	updated, _ = m.Update(snapshotLoadedMsg{
		generation:      generation,
		driftAssessment: driftReady,
		mergedCfg:       &config.Config{},
	})
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
	m.pane = paneList

	updated, cmd := m.handleKey(tea.KeyPressMsg{Code: tea.KeySpace})
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
	failedURL := "https://example.com/failed-registry.git"
	failedRef := "v1.2.3"
	induced := errors.New("induced catalog failure")

	restoreOrg := swapLoadOrgConfig(func() (*config.Config, error) { return nil, nil })
	t.Cleanup(restoreOrg)
	globalCfg := &config.Config{
		APIVersion: config.APIVersionV2,
		Harnesses:  []string{"factory"},
		Registries: []config.RegistryEntry{
			{URL: healthyRoot, Skills: config.KindSelector{Include: []string{"healthy"}}},
			{URL: failedURL, Ref: failedRef, Skills: config.KindSelector{Include: []string{"missing"}}},
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
	previousClone := cloneOrPull
	cloneOrPull = func(url, ref string) (string, error) {
		if url == failedURL {
			return "", induced
		}
		return "", errors.New("unexpected fetch for " + url)
	}
	t.Cleanup(func() { cloneOrPull = previousClone })
	previousDiscover := discoverCatalog
	discoverCatalog = func(dir, sourceHarness string) (catalogInfo, error) {
		if dir != healthyRoot {
			return catalogInfo{}, errors.New("unexpected discovery for " + dir)
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
	if !missing.EnabledGlobal {
		t.Fatal("missing row lost its global enablement")
	}
	if missing.EnabledProject || missing.EnabledLocal {
		t.Fatalf("missing row scope flags = project:%v local:%v, want global only", missing.EnabledProject, missing.EnabledLocal)
	}
	if missing.Artifact.Kind != ir.KindSkill {
		t.Fatalf("missing row kind = %v, want skill", missing.Artifact.Kind)
	}
	if missing.RegistryURL != failedURL || missing.RegistryPath != "" || missing.RegistryRef != failedRef {
		t.Fatalf("missing row registry identity = %q/%q/%q, want %q//%q",
			missing.RegistryURL, missing.RegistryPath, missing.RegistryRef, failedURL, failedRef)
	}
	if missing.Artifact.Source != config.UserPersonal || missing.Artifact.V4Kind != "" {
		t.Fatalf("missing row lost source/v4 identity: source=%v v4kind=%v", missing.Artifact.Source, missing.Artifact.V4Kind)
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
	detail := renderDetailFullContent(newTheme(true), *missing, 80)
	if !strings.Contains(detail, "No verified content is available") {
		t.Fatalf("missing row detail = %q", detail)
	}
	if !strings.Contains(detail, "Press space") {
		t.Fatalf("missing row detail omits the toggle-modal recovery action: %q", detail)
	}

	m := modelWithSelectedRow(t, *missing)
	m.width = 120
	m.height = 30
	m.enterDetailPane()
	if m.pane != paneDetail {
		t.Fatalf("unavailable row did not open the detail pane: pane %v", m.pane)
	}
	if m.bundle != nil {
		t.Fatal("unavailable row opened a filesystem bundle")
	}
	view := ansi.Strip(m.detailViewport.View())
	if !strings.Contains(view, "No verified content is available") {
		t.Fatalf("unavailable row detail pane = %q", view)
	}
	if !strings.Contains(view, "Press space") {
		t.Fatalf("unavailable row detail pane omits the toggle-modal recovery action: %q", view)
	}
}

func TestAppendUnavailableSelectionRows(t *testing.T) {
	loadedCatalogs := map[string]catalogInfo{"k1": {}}
	cfg := &config.Config{Registries: []config.RegistryEntry{{
		URL:        "/reg",
		Source:     config.UserPersonal,
		CatalogKey: "k1",
		Skills:     config.KindSelector{Include: []string{"missing"}},
	}}}
	via := &resolver.SelectedNotFoundError{Kind: ir.KindSkill, Name: "missing", URL: "/reg", Via: "collection:tools"}
	if rows := appendUnavailableSelectionRows(nil, cfg, nil, []error{via}, loadedCatalogs); len(rows) != 0 {
		t.Fatalf("collection-derived error synthesized %d rows", len(rows))
	}
	unmatched := &resolver.SelectedNotFoundError{Kind: ir.KindSkill, Name: "unselected", URL: "/reg"}
	direct := &resolver.SelectedNotFoundError{Kind: ir.KindSkill, Name: "missing", URL: "/reg"}
	rows := appendUnavailableSelectionRows(nil, cfg, nil, []error{direct, direct, unmatched}, loadedCatalogs)
	if len(rows) != 1 {
		t.Fatalf("synthesized %d rows, want exactly one for the duplicate direct error", len(rows))
	}
	if rows[0].UnavailableReason == "" {
		t.Fatal("synthesized row carries no unavailable reason")
	}

	sameIdentity := &config.Config{Registries: []config.RegistryEntry{
		{URL: "/reg", Source: config.UserPersonal, CatalogKey: "k1"},
		{URL: "/reg", Source: config.RepoCommitted, Required: true, CatalogKey: "k1", Skills: config.KindSelector{Include: []string{"missing"}}},
	}}
	rows = appendUnavailableSelectionRows(nil, sameIdentity, nil, []error{direct}, loadedCatalogs)
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

	slashed := &config.Config{Registries: []config.RegistryEntry{
		{URL: "/reg", Path: `a\b`, Source: config.UserPersonal, CatalogKey: "k1", Skills: config.KindSelector{Include: []string{"mixed"}}},
		{URL: "/reg", Path: "a/b", Source: config.RepoCommitted, CatalogKey: "k1", Skills: config.KindSelector{Include: []string{"mixed"}}},
	}}
	slashErr := &resolver.SelectedNotFoundError{Kind: ir.KindSkill, Name: "mixed", URL: "/reg", Path: "a/b"}
	slashRows := appendUnavailableSelectionRows(nil, slashed, nil, []error{slashErr}, loadedCatalogs)
	if len(slashRows) != 1 {
		t.Fatalf("synthesized %d rows for separator-equivalent paths, want 1", len(slashRows))
	}
	if !slashRows[0].EnabledGlobal || !slashRows[0].EnabledProject {
		t.Fatalf("separator-equivalent scope flags = global:%v project:%v, want both true", slashRows[0].EnabledGlobal, slashRows[0].EnabledProject)
	}

	exclCfg := &config.Config{Registries: []config.RegistryEntry{{
		URL:        "/reg",
		Source:     config.UserPersonal,
		CatalogKey: "k1",
		Skills:     config.KindSelector{Include: []string{"top-excluded", "scoped-excluded", "kept"}},
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
	exclRows := appendUnavailableSelectionRows(nil, exclCfg, localCfg, exclErrs, loadedCatalogs)
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

func TestReadSnapshotOptionalUnavailableSelectionKeepsAssessment(t *testing.T) {
	repoRoot := t.TempDir()
	homeDir := t.TempDir()
	registryRoot := t.TempDir()

	restoreOrg := swapLoadOrgConfig(func() (*config.Config, error) { return nil, nil })
	t.Cleanup(restoreOrg)
	globalCfg := &config.Config{
		APIVersion: config.APIVersionV2,
		Harnesses:  []string{"factory"},
		Registries: []config.RegistryEntry{
			{URL: registryRoot, Skills: config.KindSelector{Include: []string{"missing"}}},
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
		if dir != registryRoot {
			return catalogInfo{}, errors.New("unexpected discovery for " + dir)
		}
		return catalogInfo{Entries: []catalog.Entry{{Name: "other", Path: filepath.Join(dir, "other")}}}, nil
	}
	t.Cleanup(func() { discoverCatalog = previousDiscover })

	snapshot := readSnapshot(context.Background(), repoRoot, homeDir, catalogLoadCached)
	if snapshot.driftAssessment != driftReady {
		t.Fatalf("optional unavailable selection drift assessment = %v, want ready", snapshot.driftAssessment)
	}
	var missing *row
	unavailable := 0
	for i := range snapshot.rows {
		if snapshot.rows[i].UnavailableReason != "" {
			unavailable++
			if snapshot.rows[i].Artifact.Name == "missing" {
				missing = &snapshot.rows[i]
			}
		}
	}
	if unavailable != 1 || missing == nil {
		t.Fatalf("expected exactly one unavailable row for the missing selection, got %d unavailable in %+v", unavailable, snapshot.rows)
	}

	m := NewModel(repoRoot, homeDir)
	m.loading = false
	m.width = 120
	m.height = 30
	snapshot.generation = m.requests.active(requestSnapshot)
	updated, _ := m.Update(snapshot)
	m = updated.(Model)
	footer := ansi.Strip(renderFooter(m))
	if strings.Contains(footer, "drift unavailable") {
		t.Fatalf("optional unavailable selection blocked the drift footer: %q", footer)
	}
	for i := range m.flatItems {
		if m.flatItems[i].Kind == flatArtifact && m.flatItems[i].Row.UnavailableReason != "" {
			m.cursor = i
			break
		}
	}
	updated, _ = m.handleKey(tea.KeyPressMsg{Code: tea.KeySpace})
	m = updated.(Model)
	if m.pane != paneToggleModal {
		t.Fatalf("unavailable row did not open the scope modal: pane %v", m.pane)
	}

	hard := *missing
	hard.UnavailableSoft = false
	synthetic := modelWithSelectedRow(t, hard)
	synthetic.driftAssessment = driftReady
	syntheticFooter := ansi.Strip(renderFooter(synthetic))
	if strings.Contains(syntheticFooter, "in sync") {
		t.Fatalf("artificially clean model with an unavailable row rendered in sync: %q", syntheticFooter)
	}
	if !synthetic.failuresOrDrift() {
		t.Fatal("artificially clean model with an unavailable row did not count as drift/failure")
	}
}

func TestUnavailableSnapshotReplacesOpenVerifiedDetail(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("verified body"), 0o644); err != nil {
		t.Fatal(err)
	}
	verified := mkRow("gone", withKind(ir.KindSkill), withRegistry("https://example.com/reg.git", "reg"), withRegistryMeta("v1", "sub"))
	verified.Artifact.Path = dir
	replacement := mkRow("gone", withKind(ir.KindSkill), withRegistry("https://example.com/reg.git", "reg"), withRegistryMeta("v1", "sub"))
	replacement.UnavailableReason = unavailableCatalogReason

	m := modelWithSelectedRow(t, verified)
	m.width = 120
	m.height = 30
	m.enterDetailPane()
	if m.pane != paneDetail || m.bundle == nil {
		t.Fatalf("verified row did not open a detail bundle: pane %v", m.pane)
	}
	m.detailViewport.SetContent("VERIFIED STALE CONTENT")

	updated, _ := m.Update(snapshotLoadedMsg{
		generation:      m.requests.active(requestSnapshot),
		driftAssessment: driftFailed,
		mergedCfg:       &config.Config{},
		rows:            []row{replacement},
	})
	m = updated.(Model)
	if m.pane != paneDetail {
		t.Fatalf("unavailable snapshot closed the detail pane: pane %v", m.pane)
	}
	if m.bundle != nil {
		t.Fatal("unavailable replacement retained the verified bundle")
	}
	if m.detailKey != rowKey(replacement) {
		t.Fatalf("detail key = %q, want unavailable row key %q", m.detailKey, rowKey(replacement))
	}
	view := ansi.Strip(m.detailViewport.View())
	if strings.Contains(view, "VERIFIED STALE CONTENT") {
		t.Fatalf("stale verified content survived the snapshot: %q", view)
	}
	if !strings.Contains(view, "No verified content is available") {
		t.Fatalf("unavailable guidance missing from detail pane: %q", view)
	}

	updated, _ = m.Update(snapshotLoadedMsg{
		generation:      m.beginRequest(requestSnapshot),
		driftAssessment: driftFailed,
		mergedCfg:       &config.Config{},
		rows:            []row{replacement},
	})
	m = updated.(Model)
	view = ansi.Strip(m.detailViewport.View())
	if !strings.Contains(view, "No verified content is available") {
		t.Fatalf("second unavailable snapshot lost the explicit guidance: %q", view)
	}
	if strings.Contains(view, "resource has no source path") {
		t.Fatalf("second unavailable snapshot rendered a generic source error: %q", view)
	}
}

func TestInvalidConfigSnapshotKeepsEditorOnlyRecovery(t *testing.T) {
	r := mkRow("unsafe", withProject)
	m := modelWithSelectedRow(t, r)
	m.authoritativeRows = m.rows
	m.pane = paneList

	updated, _ := m.Update(snapshotLoadedMsg{
		generation:      m.requests.active(requestSnapshot),
		driftAssessment: driftFailed,
		diagnostics: []tuiDiagnostic{
			snapshotDiagnostic(diagnosticError, diagnosticConfig, errors.New("config parse failed")),
		},
	})
	m = updated.(Model)
	if !m.configInvalid {
		t.Fatal("config-error snapshot did not mark the model config-invalid")
	}
	if m.pane != paneList {
		t.Fatalf("config-error snapshot moved the pane: %v", m.pane)
	}

	for i := range m.flatItems {
		if m.flatItems[i].Kind == flatSection {
			m.cursor = i
			break
		}
	}
	sectionPath, ok := m.selectedSectionPath()
	if !ok {
		t.Fatal("no section header available under invalid config")
	}
	wasFolded := m.currentFoldState()[sectionPath]
	updated, cmd := m.handleKey(tea.KeyPressMsg{Code: tea.KeySpace})
	m = updated.(Model)
	if m.pane != paneList {
		t.Fatalf("Space on a section header moved the pane under invalid config: pane %v", m.pane)
	}
	if cmd == nil {
		t.Fatal("Space on a section header returned no editor guidance under invalid config")
	}
	if msg, ok := cmd().(toastMsg); !ok || !strings.Contains(msg.text, "edit config") {
		t.Fatalf("Space on a section header guidance = %#v, want an edit-config toast", cmd())
	}
	if m.currentFoldState()[sectionPath] != wasFolded {
		t.Fatal("Space on a section header changed the fold under invalid config")
	}
	for i := range m.flatItems {
		if m.flatItems[i].Kind == flatArtifact {
			m.cursor = i
			break
		}
	}
	if _, ok := m.selectedRow(); !ok {
		t.Fatal("artifact selection was not restored under invalid config")
	}

	updated, cmd = m.handleKey(tea.KeyPressMsg{Code: tea.KeySpace})
	m = updated.(Model)
	if m.pane != paneList {
		t.Fatalf("Space opened a structured mutation modal under invalid config: pane %v", m.pane)
	}
	if cmd == nil {
		t.Fatal("Space under invalid config returned no editor guidance")
	}
	if msg, ok := cmd().(toastMsg); !ok || !strings.Contains(msg.text, "edit config") {
		t.Fatalf("Space guidance = %#v, want an edit-config toast", cmd())
	}

	updated, cmd = m.handleKey(tea.KeyPressMsg{Code: 'H', Text: string('H')})
	m = updated.(Model)
	if m.pane != paneList {
		t.Fatalf("Harness opened a structured mutation modal under invalid config: pane %v", m.pane)
	}
	if cmd == nil {
		t.Fatal("Harness under invalid config returned no editor guidance")
	}
	if msg, ok := cmd().(toastMsg); !ok || !strings.Contains(msg.text, "edit config") {
		t.Fatalf("Harness guidance = %#v, want an edit-config toast", cmd())
	}

	updated, _ = m.handleKey(tea.KeyPressMsg{Code: 'E', Text: string('E')})
	m = updated.(Model)
	if m.pane != paneEditConfigModal {
		t.Fatalf("invalid config did not keep the editor reachable: pane %v", m.pane)
	}
	m.pane = paneList

	updated, _ = m.Update(snapshotLoadedMsg{
		generation:      m.beginRequest(requestSnapshot),
		driftAssessment: driftFailed,
		mergedCfg:       &config.Config{},
		rows:            []row{r},
		diagnostics: []tuiDiagnostic{
			snapshotDiagnostic(diagnosticError, diagnosticCatalog, errors.New("catalog fetch failed")),
		},
	})
	m = updated.(Model)
	if m.configInvalid {
		t.Fatal("hard catalog error with a valid merged config marked the model config-invalid")
	}
	for i := range m.flatItems {
		if m.flatItems[i].Kind == flatArtifact {
			m.cursor = i
			break
		}
	}
	updated, _ = m.handleKey(tea.KeyPressMsg{Code: tea.KeySpace})
	m = updated.(Model)
	if m.pane != paneToggleModal {
		t.Fatalf("catalog-error snapshot blocked the scope modal: pane %v", m.pane)
	}
}
