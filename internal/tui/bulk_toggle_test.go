package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/session"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
)

// helperFlatCursorOnSection returns a model with the cursor on the named
// top-level section header.
func helperFlatCursorOnSection(t *testing.T, rows []row, pathPrefix string) Model {
	t.Helper()
	m := newViewModel(t, rows)
	for i, it := range m.flatItems {
		if it.Kind == flatSection && strings.HasPrefix(it.SectionPath, pathPrefix) {
			m.cursor = i
			return m
		}
	}
	t.Fatalf("no section %q in %v", pathPrefix, sectionPaths(m.flatItems))
	return m
}

// spec: tui-interactions/filtered-folded-descendants-and-skipped-rows
func TestSpaceOnSectionHeaderOpensBulkModal(t *testing.T) {
	m := helperFlatCursorOnSection(t, groupingTestRows(), "status:")

	mi, _ := m.Update(keyNamed("space"))
	m = mi.(Model)
	if m.pane != paneBulkToggle {
		t.Fatalf("space on a section header should open the bulk modal, pane=%v", m.pane)
	}
	if m.bulkModal.sectionPath == "" {
		t.Fatalf("bulk modal must capture the section path")
	}
}

// spec: tui-interactions/filtered-folded-descendants-and-skipped-rows
func TestBulkModalCountsEachScopeIndependently(t *testing.T) {
	// A row enabled at all three layers must count in every scope; off is
	// derived per scope from membership, not an exclusive bucket.
	onAll := mkRow("all", withProject, withRegistry("sec.git", "security"))
	onAll.EnabledGlobal = true
	onAll.EnabledLocal = true
	onGlobalOnly := mkRow("gonly", withRegistry("sec.git", "security"))
	onGlobalOnly.EnabledGlobal = true
	off := mkRow("off", withRegistry("sec.git", "security"))
	m := helperFlatCursorOnSection(t, []row{onAll, onGlobalOnly, off}, "status:enabled")
	// The modal must disclose an active filter and the skipped count.
	m.filter = "all"
	mi, _ := m.Update(keyNamed("space"))
	m = mi.(Model)
	body := ansi.Strip(renderBulkToggleModal(m))
	if !strings.Contains(body, "g2/1 p1/2 l1/2") {
		t.Fatalf("per-scope on/off counts must be independent, got:\n%s", body)
	}
	if !strings.Contains(body, "filter: all") || !strings.Contains(body, "skipped") {
		t.Fatalf("modal must disclose the active filter and skips, got:\n%s", body)
	}
}

// spec: tui-interactions/filtered-folded-descendants-and-skipped-rows
func TestBulkMembersRespectSearchFilter(t *testing.T) {
	rows := groupingTestRows()
	m := newViewModel(t, rows)
	m.applyViewSettings(viewSettings{GroupBy: axisSource, ThenBy: axisNone})
	m.filter = "alpha"
	m.recomputeView()
	var secPath string
	for _, it := range m.flatItems {
		if it.Kind == flatSection && it.SectionDepth == 0 && it.SectionPath != "attention" {
			secPath = it.SectionPath
		}
	}
	members := m.sectionMembers(secPath)
	if len(members) != 1 || members[0].Artifact.Name != "alpha-skill" {
		names := []string{}
		for _, r := range members {
			names = append(names, r.Artifact.Name)
		}
		t.Fatalf("filtered members = %v, want [alpha-skill]", names)
	}
}

// spec: tui-interactions/filtered-folded-descendants-and-skipped-rows
func TestBulkEligibilityMatchesSingleRowGuards(t *testing.T) {
	discoveryInstallable := mkRow("disc", withKind(ir.KindSkill))
	discoveryInstallable.MarketplaceDiscovery = &marketplaceDiscovery{
		Item: mpapi.ResourceListItem{Kind: "skill", Name: "disc", Availability: mpapi.AvailabilityAvailable},
	}
	discoveryBlocked := mkRow("disc-b", withKind(ir.KindSkill))
	discoveryBlocked.MarketplaceDiscovery = &marketplaceDiscovery{
		Item: mpapi.ResourceListItem{Kind: "skill", Name: "disc-b", Availability: mpapi.AvailabilityBlocked},
	}
	collectionOnly := mkRow("coll", withKind(ir.KindSkill))
	collectionOnly.Artifact.IsMarketplace = true
	collectionOnly.Artifact.Source = config.RepoCommitted
	collectionOnly.MarketplaceCollectionOnly = true
	pendingVerification := mkRow("pend", withProject)
	pendingVerification.PendingVerification = true
	broken := mkRow("b", withProject)
	broken.UnavailableReason = "gone"
	// Drift-broken rows carry no UnavailableReason: the batch must still skip
	// them even though the single-row toggle may act.
	driftBroken := mkRow("db", withProject, withDrift(materialize.DriftBroken))
	cases := []struct {
		name      string
		r         row
		rowDenial toggleDenial
		reason    string
	}{
		{"eligible", mkRow("e", withProject), toggleEligible, ""},
		{"required", mkRow("r", withRequired), toggleRequired, "required"},
		{"malformed", mkRow("m", withMalformed("x")), toggleMalformed, "malformed"},
		{"pending verification", pendingVerification, togglePendingVerification, "pending verification"},
		{"discovery blocked", discoveryBlocked, toggleMarketplaceBlocked, "not installable"},
		{"collection only", collectionOnly, toggleCollectionOnly, "collection-selected"},
		// Batch-only guards: row-level toggles may still act on these, but a
		// config-only batch cannot repair a broken row or acquire content.
		{"unavailable", broken, toggleEligible, "unavailable"},
		{"drift broken", driftBroken, toggleEligible, "broken"},
		{"pending sync", mkRow("pd", withProject, withDrift(materialize.DriftMissing)), toggleEligible, ""},
		{"discovery needs acquisition", discoveryInstallable, toggleEligible, "requires acquisition"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newViewModel(t, groupingTestRows())
			if got := m.rowToggleEligibility(tc.r); got != tc.rowDenial {
				t.Fatalf("rowToggleEligibility = %v, want %v", got, tc.rowDenial)
			}
			got := m.bulkSkipReason(tc.r)
			if tc.reason == "" && got != "" {
				t.Fatalf("expected eligible, skipped: %q", got)
			}
			if tc.reason != "" && !strings.HasPrefix(got, tc.reason) {
				t.Fatalf("skip reason = %q, want prefix %q", got, tc.reason)
			}
		})
	}
}

// spec: tui-interactions/filtered-folded-descendants-and-skipped-rows
func TestBulkFoldedSectionMemberSetIsExact(t *testing.T) {
	rows := groupingTestRows()
	m := newViewModel(t, rows)
	m.applyViewSettings(viewSettings{GroupBy: axisSource, ThenBy: axisKind})
	m.collapseAllSections()
	m.rebuildFlatItems()

	// The sec.git source section contains gamma-mcp, epsilon-unknown and
	// required-one (alpha/beta share the repo.git identity); folded or not,
	// membership must be exactly that set.
	var secPath string
	for _, it := range m.flatItems {
		if it.Kind == flatSection && it.SectionDepth == 0 && strings.HasPrefix(it.SectionTitle, "security") {
			secPath = it.SectionPath
		}
	}
	if secPath == "" {
		t.Fatalf("no security source section")
	}
	got := map[string]bool{}
	for _, r := range m.sectionMembers(secPath) {
		got[r.Artifact.Name] = true
	}
	want := map[string]bool{"gamma-mcp": true, "epsilon-unknown": true, "required-one": true}
	if len(got) != len(want) {
		t.Fatalf("folded section members = %v, want %v", got, want)
	}
	for name := range want {
		if !got[name] {
			t.Fatalf("folded section missing member %q; got %v", name, got)
		}
	}
}

// spec: tui-interactions/filtered-folded-descendants-and-skipped-rows
func TestBulkModalOpensOnFullyIneligibleSection(t *testing.T) {
	rows := []row{mkRow("req", withRequired), mkRow("req2", withRequired)}
	m := helperFlatCursorOnSection(t, rows, "status:required")
	mi, _ := m.Update(keyNamed("space"))
	m = mi.(Model)
	if m.pane != paneBulkToggle {
		t.Fatalf("a wholly ineligible section should still open the modal")
	}
	body := ansi.Strip(renderBulkToggleModal(m))
	if !strings.Contains(strings.ToLower(body), "skipped") || !strings.Contains(strings.ToLower(body), "required") {
		t.Fatalf("modal should explain skips, got:\n%s", body)
	}
}

// spec: tui-interactions/scope-direction-confirmation-and-cancellation
func TestBulkModalEscAtEveryStageWritesNothing(t *testing.T) {
	for _, keys := range [][]string{
		{"esc"},           // cancel at scope stage
		{"p", "esc"},      // cancel at direction stage
		{"p", "-", "esc"}, // cancel at confirmation stage
	} {
		f := newBulkFixture(t)
		before := mustReadFile(t, f.projectPath)
		mutations := 0
		m := helperFlatCursorOnSection(t, []row{
			mkRow("alpha", withProject, withRegistry("local", "local")),
			mkRow("beta", withProject, withRegistry("local", "local")),
		}, "status:enabled")
		m.workspace = f.workspace
		m.dependencyLifecycle.Mutate = func(context.Context, pipeline.Workspace, func(pipeline.ConfigPathMapper) error) error {
			mutations++
			return nil
		}
		mi, _ := m.Update(keyNamed("space"))
		m = mi.(Model)
		for _, k := range keys {
			mi, _ := m.Update(keyNamed(k))
			m = mi.(Model)
		}
		if m.pane != paneList {
			t.Fatalf("esc at %v should close the bulk modal, pane=%v", keys, m.pane)
		}
		if mutations != 0 {
			t.Fatalf("esc at %v started %d mutations", keys, mutations)
		}
		if after := mustReadFile(t, f.projectPath); string(after) != string(before) {
			t.Fatalf("esc at %v changed config bytes", keys)
		}
	}
}

// spec: tui-interactions/scope-direction-confirmation-and-cancellation
func TestBulkModalRequiresConfirmationBeforeWrite(t *testing.T) {
	m := helperFlatCursorOnSection(t, groupingTestRows(), "status:enabled")
	mutations := 0
	m.dependencyLifecycle.Mutate = func(context.Context, pipeline.Workspace, func(pipeline.ConfigPathMapper) error) error {
		mutations++
		return nil
	}
	mi, _ := m.Update(keyNamed("space"))
	m = mi.(Model)
	mi, _ = m.Update(keyPress('p')) // project scope
	m = mi.(Model)
	mi, _ = m.Update(keyPress('-')) // disable direction
	m = mi.(Model)
	if m.bulkModal.stage != bulkStageConfirm {
		t.Fatalf("expected confirm stage, got %v", m.bulkModal.stage)
	}
	if mutations != 0 {
		t.Fatalf("reaching the confirmation stage must not start a mutation")
	}
	body := ansi.Strip(renderBulkToggleModal(m))
	if !strings.Contains(strings.ToLower(body), "project") {
		t.Fatalf("confirmation should name the target layer, got:\n%s", body)
	}
	if !strings.Contains(body, "disable") {
		t.Fatalf("confirmation should name the direction, got:\n%s", body)
	}
}

// spec: tui-interactions/scope-direction-confirmation-and-cancellation
func TestBulkModalRejectsUnavailableScope(t *testing.T) {
	m := helperFlatCursorOnSection(t, groupingTestRows(), "status:enabled")
	m.workspace = pipeline.Workspace{} // no repo root → project scope unavailable
	mutations := 0
	m.dependencyLifecycle.Mutate = func(context.Context, pipeline.Workspace, func(pipeline.ConfigPathMapper) error) error {
		mutations++
		return nil
	}
	mi, _ := m.Update(keyNamed("space"))
	m = mi.(Model)
	mi, _ = m.Update(keyPress('p'))
	m = mi.(Model)
	if m.bulkModal.stage != bulkStageScope {
		t.Fatalf("unavailable scope should stay on the scope stage, got %v", m.bulkModal.stage)
	}
	if mutations != 0 {
		t.Fatalf("unavailable scope must not submit")
	}
}

// spec: tui-interactions/changed-eligibility-busy-operation-and-stale-completion
func TestBulkSubmitRefusesWhileOperationBusy(t *testing.T) {
	m := helperFlatCursorOnSection(t, groupingTestRows(), "status:enabled")
	mutations := 0
	m.dependencyLifecycle.Mutate = func(context.Context, pipeline.Workspace, func(pipeline.ConfigPathMapper) error) error {
		mutations++
		return nil
	}
	mi, _ := m.Update(keyNamed("space"))
	m = mi.(Model)
	mi, _ = m.Update(keyPress('p'))
	m = mi.(Model)
	mi, _ = m.Update(keyPress('-'))
	m = mi.(Model)
	// Simulate another in-flight operation.
	op, ok := m.beginOperation(operationMutation)
	if !ok {
		t.Fatalf("could not start a blocking operation")
	}
	mi, cmd := m.Update(keyNamed("enter"))
	m = mi.(Model)
	if m.operations.Active == nil || m.operations.Active.ID != op {
		t.Fatalf("busy submit replaced the active operation")
	}
	if mutations != 0 {
		t.Fatalf("busy submit started a second mutation")
	}
	if cmd == nil {
		t.Fatalf("busy submit should surface blocked-operation feedback")
	} else if msg := cmd(); func() bool { _, ok := msg.(configWrittenMsg); return ok }() {
		t.Fatalf("busy submit must not produce a config write result")
	}
	m.finishOperation(op)
}

// spec: tui-interactions/changed-eligibility-busy-operation-and-stale-completion
func TestStaleBulkCompletionRejected(t *testing.T) {
	m := helperFlatCursorOnSection(t, groupingTestRows(), "status:enabled")
	stale, ok := m.beginOperation(operationMutation)
	if !ok {
		t.Fatalf("begin stale operation")
	}
	m.finishOperation(stale)
	// A newer operation is now active; the stale completion must not touch it.
	newer, ok := m.beginOperation(operationMutation)
	if !ok {
		t.Fatalf("begin newer operation")
	}
	m.recordOperationFailure(operationTargets, diagnosticMutation, fmt.Errorf("sentinel diagnostic"))
	beforeLen := len(m.flatItems)
	beforeDiags := len(m.diagnostics.All())
	mi, cmd := m.Update(configWrittenMsg{operation: stale, skillName: "x", enabled: false})
	m = mi.(Model)
	if m.operations.Active == nil || m.operations.Active.ID != newer {
		t.Fatalf("stale completion disturbed the active operation")
	}
	if m.pendingToggleReload != 0 {
		t.Fatalf("stale completion triggered a reload")
	}
	if len(m.flatItems) != beforeLen {
		t.Fatalf("stale completion changed the view")
	}
	if len(m.diagnostics.All()) != beforeDiags {
		t.Fatalf("stale completion changed diagnostics")
	}
	if cmd != nil {
		t.Fatalf("stale completion returned a command")
	}
	m.finishOperation(newer)
}

// bulkFixture wires a real workspace with a v4 project config and counters
// on the serialized mutation and reconcile seams.
type bulkFixture struct {
	repo         string
	home         string
	stateDir     string
	projectPath  string
	workspace    pipeline.Workspace
	snap         snapshotLoadedMsg
	mutations    int
	reconciles   int
	commitOpts   []dependencytxn.Option // forwarded to dependencytxn.Commit
	beforeCommit func()                 // runs inside the reconciler, racing Commit
}

func newBulkFixture(t *testing.T) *bulkFixture {
	t.Helper()
	home := t.TempDir()
	repo := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".state"))
	t.Setenv("TMPDIR", t.TempDir())
	source := filepath.Join(t.TempDir(), "registry.git")
	for _, name := range []string{"alpha", "beta"} {
		path := filepath.Join(source, "skills", name, "SKILL.md")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("# "+name+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	projectPath := filepath.Join(repo, ".agents", "config.yaml")
	project := "apiVersion: agent-marketplace/v4\nharnesses: [devin]\nsources:\n  local:\n    directory: " + source + "\n"
	if err := os.MkdirAll(filepath.Dir(projectPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(projectPath, []byte(project), 0o640); err != nil {
		t.Fatal(err)
	}
	originalOrg, originalGlobal := session.LoadOrgConfig, session.LoadGlobalConfig
	originalRepo, originalLocal := session.LoadRepoConfig, session.LoadLocalConfig
	t.Cleanup(func() {
		session.LoadOrgConfig, session.LoadGlobalConfig = originalOrg, originalGlobal
		session.LoadRepoConfig, session.LoadLocalConfig = originalRepo, originalLocal
	})
	session.LoadOrgConfig = func() (*config.Config, error) { return nil, nil }
	session.LoadGlobalConfig = func() (*config.Config, error) { return nil, nil }
	session.LoadRepoConfig = func() (*config.Config, error) { return config.LoadConfigFromPath(projectPath) }
	session.LoadLocalConfig = func(string) (*config.LocalConfig, error) { return nil, nil }

	workspace, err := pipeline.NewWorkspace(repo, "")
	if err != nil {
		t.Fatal(err)
	}
	snap := readSnapshot(context.Background(), repo, home, catalogLoadRefresh)
	if len(snap.errs) > 0 {
		t.Fatalf("snapshot errors: %v", snap.errs)
	}
	stateBase, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return &bulkFixture{repo: repo, home: home, stateDir: filepath.Join(stateBase, "txn-state"), projectPath: projectPath, workspace: workspace, snap: snap}
}

func (f *bulkFixture) projectEnableActions(t *testing.T) []toggleAction {
	t.Helper()
	var acts []toggleAction
	for _, r := range f.snap.rows {
		if r.IsAvailable && r.Artifact.Kind == ir.KindSkill {
			a := scopedToggleAction(r, scopeProject)
			a.enable = true
			acts = append(acts, a)
		}
	}
	if len(acts) != 2 {
		t.Fatalf("expected 2 eligible project actions, got %d", len(acts))
	}
	return acts
}

// mutate routes staged edits through the real dependencytxn.Commit seam.
func (f *bulkFixture) mutate() DependencyMutationFunc {
	return func(ctx context.Context, ws pipeline.Workspace, mutation func(pipeline.ConfigPathMapper) error) error {
		f.mutations++
		return pipeline.MutateConfigWithDependencies(ctx, ws.RepoRoot(), ws.LocalConfigDir(), mutation,
			func(_ context.Context, _ string, _ string, _ bool, edits []pipeline.StagedConfigEdit) (pipeline.DependencyLockView, error) {
				f.reconciles++
				if f.beforeCommit != nil {
					f.beforeCommit()
				}
				desired := make([]dependencytxn.DesiredFile, 0, len(edits))
				for _, edit := range edits {
					file := dependencytxn.DesiredFile{
						Path:     edit.Path,
						Expected: edit.Expected,
						Bytes:    edit.Bytes,
						Mode:     edit.Mode,
					}
					if edit.Expected.Exists {
						file.ExpectedMode = edit.Mode
					}
					desired = append(desired, file)
				}
				_, err := dependencytxn.Commit(ctx, f.stateDir, desired, f.commitOpts...)
				return pipeline.DependencyLockView{}, err
			})
	}
}

// spec: tui-interactions/atomic-batch-success-and-failure
func TestBulkToggleAtomicAcrossResources(t *testing.T) {
	f := newBulkFixture(t)
	acts := f.projectEnableActions(t)
	msg, ok := runBulkDependencyToggle(context.Background(), f.workspace, acts, f.mutate())().(configWrittenMsg)
	if !ok || msg.err != nil {
		t.Fatalf("bulk toggle message = %#v", msg)
	}
	if f.mutations != 1 || f.reconciles != 1 {
		t.Fatalf("bulk batch must run one mutation and one reconcile, got %d/%d", f.mutations, f.reconciles)
	}
	updated := string(mustReadFile(t, f.projectPath))
	if !strings.Contains(updated, "skill/alpha") || !strings.Contains(updated, "skill/beta") {
		t.Fatalf("bulk enable should write both selections in one layer:\n%s", updated)
	}
	assertNoConfigStaging(t)
}

// spec: tui-interactions/atomic-batch-success-and-failure
func TestBulkToggleMissingMutationSeamFailsClosed(t *testing.T) {
	f := newBulkFixture(t)
	acts := f.projectEnableActions(t)
	before := mustReadFile(t, f.projectPath)
	msg, ok := runBulkDependencyToggle(context.Background(), f.workspace, acts, nil)().(configWrittenMsg)
	if !ok {
		t.Fatalf("expected configWrittenMsg")
	}
	if msg.err == nil {
		t.Fatalf("a missing mutation seam must fail closed")
	}
	if after := mustReadFile(t, f.projectPath); string(after) != string(before) {
		t.Fatalf("missing seam must publish nothing:\nbefore=%s\nafter=%s", before, after)
	}
	if f.mutations != 0 || f.reconciles != 0 {
		t.Fatalf("missing seam must not mutate or reconcile")
	}
	assertNoConfigStaging(t)
}

// spec: tui-interactions/atomic-batch-success-and-failure
func TestBulkToggleMixedLayersRefuse(t *testing.T) {
	f := newBulkFixture(t)
	acts := f.projectEnableActions(t)
	acts[1].scope = scopeLocal // resolves to the local layer, not project
	before := mustReadFile(t, f.projectPath)
	msg, ok := runBulkDependencyToggle(context.Background(), f.workspace, acts, f.mutate())().(configWrittenMsg)
	if !ok || msg.err == nil {
		t.Fatalf("mixed destinations must refuse the batch, msg=%#v", msg)
	}
	if f.reconciles != 0 {
		t.Fatalf("a refused batch must never reach reconcile")
	}
	if after := mustReadFile(t, f.projectPath); string(after) != string(before) {
		t.Fatalf("refused batch changed bytes")
	}
	assertNoConfigStaging(t)
}

// spec: tui-interactions/atomic-batch-success-and-failure
func TestBulkToggleFailureAfterFirstStagedEdit(t *testing.T) {
	f := newBulkFixture(t)
	acts := f.projectEnableActions(t)
	original := mustReadFile(t, f.projectPath)
	// Positive control: the first action alone must publish.
	control, ok := runBulkDependencyToggle(context.Background(), f.workspace, acts[:1], f.mutate())().(configWrittenMsg)
	if !ok || control.err != nil {
		t.Fatalf("first-action control batch failed: %#v", control)
	}
	if got := string(mustReadFile(t, f.projectPath)); !strings.Contains(got, "skill/alpha") {
		t.Fatalf("control batch did not write alpha:\n%s", got)
	}
	if err := os.WriteFile(f.projectPath, original, 0o640); err != nil {
		t.Fatal(err)
	}
	// The second action still has a valid locator, but its staged layer edit
	// fails after the first action already applied inside the same batch.
	acts[1].isMarketplace = true
	acts[1].hasMarketplaceSelection = false
	before := mustReadFile(t, f.projectPath)
	msg, ok := runBulkDependencyToggle(context.Background(), f.workspace, acts, f.mutate())().(configWrittenMsg)
	if !ok {
		t.Fatalf("expected configWrittenMsg")
	}
	wantErr := fmt.Sprintf("marketplace skill %q has no direct installation pin", acts[1].skillName)
	if msg.err == nil || msg.err.Error() != wantErr {
		t.Fatalf("expected exact staged-edit failure %q, got %v", wantErr, msg.err)
	}
	if f.reconciles != 1 {
		t.Fatalf("the failed batch must never reach reconcile (reconciles=%d, want the control's 1)", f.reconciles)
	}
	after := mustReadFile(t, f.projectPath)
	if string(after) != string(before) {
		t.Fatalf("failed batch must publish nothing:\nbefore=%s\nafter=%s", before, after)
	}
	assertNoConfigStaging(t)
}

// spec: tui-interactions/atomic-batch-success-and-failure
func TestBulkTogglePublicationFailurePreservesBytes(t *testing.T) {
	f := newBulkFixture(t)
	acts := f.projectEnableActions(t)
	sentinel := errors.New("sentinel publish failure")
	f.commitOpts = []dependencytxn.Option{dependencytxn.WithFaultHook(func(p dependencytxn.FaultPoint) error {
		if p.Stage == dependencytxn.FaultBeforePublish {
			return sentinel
		}
		return nil
	})}
	before := mustReadFile(t, f.projectPath)
	beforeInfo, err := os.Stat(f.projectPath)
	if err != nil {
		t.Fatal(err)
	}
	msg, ok := runBulkDependencyToggle(context.Background(), f.workspace, acts, f.mutate())().(configWrittenMsg)
	if !ok || msg.err == nil {
		t.Fatalf("a failing publication must fail the batch, msg=%#v", msg)
	}
	if !errors.Is(msg.err, sentinel) {
		t.Fatalf("publication error must be the injected sentinel, got %v", msg.err)
	}
	if f.mutations != 1 || f.reconciles != 1 {
		t.Fatalf("expected one mutation and one reconcile attempt, got %d/%d", f.mutations, f.reconciles)
	}
	after := mustReadFile(t, f.projectPath)
	if string(after) != string(before) {
		t.Fatalf("publication failure changed bytes")
	}
	afterInfo, err := os.Stat(f.projectPath)
	if err != nil {
		t.Fatal(err)
	}
	if afterInfo.Mode() != beforeInfo.Mode() {
		t.Fatalf("publication failure changed the mode: %v -> %v", beforeInfo.Mode(), afterInfo.Mode())
	}
	assertNoConfigStaging(t)
}

// spec: tui-interactions/atomic-batch-success-and-failure
func TestBulkToggleConcurrentEditConflictPreservesOtherWriter(t *testing.T) {
	f := newBulkFixture(t)
	acts := f.projectEnableActions(t)
	// An independent writer races the staged generation snapshot; the real
	// compare-and-swap must refuse to publish.
	concurrent := []byte("apiVersion: agent-marketplace/v4\nharnesses: [devin]\n# concurrent edit\nsources:\n  local:\n    directory: /tmp/concurrent-source\n")
	f.beforeCommit = func() {
		if err := os.WriteFile(f.projectPath, concurrent, 0o640); err != nil {
			t.Errorf("concurrent edit: %v", err)
		}
	}
	msg, ok := runBulkDependencyToggle(context.Background(), f.workspace, acts, f.mutate())().(configWrittenMsg)
	if !ok || msg.err == nil {
		t.Fatalf("concurrent edit must fail the batch, msg=%#v", msg)
	}
	if !errors.Is(msg.err, dependencytxn.ErrGenerationConflict) {
		t.Fatalf("expected ErrGenerationConflict, got %v", msg.err)
	}
	if after := mustReadFile(t, f.projectPath); string(after) != string(concurrent) {
		t.Fatalf("the transaction must preserve the concurrent writer's bytes:\n%s", after)
	}
	assertNoConfigStaging(t)
}

// spec: tui-interactions/atomic-batch-success-and-failure
func TestBulkToggleMarketplacePinsPreserveCollectionSelectors(t *testing.T) {
	f := newBulkFixture(t)
	digest := "sha256:" + strings.Repeat("a", 64)
	entry := config.MarketplaceEntry{
		URL: "https://market.example", InstanceID: "market-id", Source: config.RepoCommitted,
		Resources: []config.MarketplaceResourcePin{
			{RegistryID: "source-id", RegistrySlug: "source-id", Kind: "skill", Name: "review", Revision: digest, Digest: digest,
				Acquisition: config.MarketplaceAcquisitionMirror, Direct: true},
			{RegistryID: "source-id", RegistrySlug: "source-id", Kind: "skill", Name: "bundle-review", Revision: digest, Digest: digest,
				Acquisition: config.MarketplaceAcquisitionMirror,
				SelectedBy:  config.MarketplaceSelectionsFrom([]config.MarketplaceSelection{{RegistryID: "collections", Name: "bundle", Revision: digest}})},
		},
	}
	mkMarketplaceRow := func(name string, pin config.MarketplaceResourcePin) row {
		r := mkRow(name, withProject, withKind(ir.KindSkill))
		r.Artifact.IsMarketplace = true
		r.Artifact.Source = config.RepoCommitted
		r.MarketplaceSelections = map[config.Source]marketplaceSelection{
			config.RepoCommitted: {Entry: entry, Pin: pin},
		}
		return r
	}
	direct := mkMarketplaceRow("review", entry.Resources[0])
	viaCollection := mkMarketplaceRow("bundle-review", entry.Resources[1])
	// Direct request plus the collection request selecting bundle-review.
	project := "apiVersion: agent-marketplace/v4\nharnesses: [devin]\n" +
		"sources: {mkt: {marketplace: https://market.example}}\n" +
		"resources:\n  mkt:\n    include:\n" +
		"      - source-id/skill/review\n" +
		"      - collections/collection/bundle\n"
	if err := os.WriteFile(f.projectPath, []byte(project), 0o640); err != nil {
		t.Fatal(err)
	}
	m := newViewModel(t, []row{direct, viaCollection})
	m.workspace = f.workspace
	m.bulkModal = bulkModalState{sectionPath: "status:enabled", scope: scopeProject, scopeSet: true, direction: bulkDirDisable}
	acts, reasons, err := m.evaluateBulkActions(m.bulkModal)
	if err != nil {
		t.Fatalf("evaluateBulkActions: %v", err)
	}
	if len(acts) != 1 || acts[0].skillName != "review" {
		t.Fatalf("only the direct pin is toggleable in bulk, acts=%v reasons=%v", acts, reasons)
	}
	if reasons["collection-selected"] != 1 {
		t.Fatalf("collection-selected row must be skipped with its reason, got %v", reasons)
	}
	msg, ok := runBulkDependencyToggle(context.Background(), f.workspace, acts, f.mutate())().(configWrittenMsg)
	if !ok || msg.err != nil {
		t.Fatalf("bulk marketplace disable failed: %#v err=%v", msg, msg.err)
	}
	if msg.enabled {
		t.Fatalf("removing a pin disables the resource, enabled=%v", msg.enabled)
	}
	updated := string(mustReadFile(t, f.projectPath))
	if strings.Contains(updated, "source-id/skill/review") {
		t.Fatalf("direct pin must be removed:\n%s", updated)
	}
	if !strings.Contains(updated, "collections/collection/bundle") {
		t.Fatalf("the collection request selecting bundle-review must survive:\n%s", updated)
	}
	assertNoConfigStaging(t)
}

// spec: tui-interactions/scope-direction-confirmation-and-cancellation
func TestBulkExcludeMixedDestinationsRefuse(t *testing.T) {
	// 'e' resolves per row: the overridable organization Marketplace row
	// takes the user exclusion, the ordinary row the local — two layers.
	orgRow := mkRow("org-mp", withProject)
	orgRow.Artifact.IsMarketplace = true
	orgRow.Artifact.Source = config.Organization
	orgRow.MarketplaceSelections = map[config.Source]marketplaceSelection{
		config.Organization: {Entry: config.MarketplaceEntry{URL: "https://market.example", InstanceID: "id"}, Pin: config.MarketplaceResourcePin{RegistryID: "src", RegistrySlug: "src", Kind: "skill", Name: "org-mp"}},
	}
	rows := []row{orgRow, mkRow("plain", withProject, withRegistry("r.git", "r"))}
	m := newViewModel(t, rows)
	m.bulkModal = bulkModalState{sectionPath: "status:enabled", scope: scopeLocalExclude, scopeSet: true, direction: bulkDirDisable}
	_, _, err := m.evaluateBulkActions(m.bulkModal)
	if err == nil || !strings.Contains(err.Error(), "multiple config layers") {
		t.Fatalf("mixed user/local exclusion targets must refuse, err=%v", err)
	}
}

// spec: tui-interactions/scope-direction-confirmation-and-cancellation
func TestBulkExcludeResolvesUserScopeInConfirmation(t *testing.T) {
	orgRow := mkRow("org-mp", withProject)
	orgRow.Artifact.IsMarketplace = true
	orgRow.Artifact.Source = config.Organization
	orgRow.MarketplaceSelections = map[config.Source]marketplaceSelection{
		config.Organization: {Entry: config.MarketplaceEntry{URL: "https://market.example", InstanceID: "id"}, Pin: config.MarketplaceResourcePin{RegistryID: "src", RegistrySlug: "src", Kind: "skill", Name: "org-mp"}},
	}
	m := helperFlatCursorOnSection(t, []row{orgRow}, "status:enabled")
	mi, _ := m.Update(keyNamed("space"))
	m = mi.(Model)
	mi, _ = m.Update(keyPress('e'))
	m = mi.(Model)
	mi, _ = m.Update(keyPress('-'))
	m = mi.(Model)
	acts, reasons, err := m.evaluateBulkActions(m.bulkModal)
	if err != nil {
		t.Fatalf("evaluateBulkActions: %v", err)
	}
	if len(acts) != 1 || acts[0].scope != scopeUserExclude {
		t.Fatalf("e on an overridable org row must resolve to user_exclude, acts=%+v reasons=%v", acts, reasons)
	}
	body := ansi.Strip(renderBulkToggleModal(m))
	if !strings.Contains(body, "user_exclude") {
		t.Fatalf("confirmation must name the resolved user exclusion layer, got:\n%s", body)
	}
	// Excluding disables the resource: the completion flag follows the
	// resource outcome, not the raw add.
	msg, ok := runBulkDependencyToggle(context.Background(), m.workspace, acts, func(context.Context, pipeline.Workspace, func(pipeline.ConfigPathMapper) error) error {
		return nil
	})().(configWrittenMsg)
	if !ok {
		t.Fatalf("expected configWrittenMsg")
	}
	if msg.enabled {
		t.Fatalf("adding an exclusion must report enabled=false")
	}
}

// spec: tui-interactions/scope-direction-confirmation-and-cancellation
func TestBulkDirectionMatchesCurrentRowState(t *testing.T) {
	cases := []struct {
		name       string
		r          row
		scopeKey   rune
		dirKey     rune
		wantSkip   bool
		wantEnable bool
	}{
		{"global off enable", mkRow("x", withRegistry("r.git", "r")), 'g', '+', false, true},
		{"global off disable", mkRow("x", withRegistry("r.git", "r")), 'g', '-', true, false},
		{"global on enable", mkRow("x", withGlobal, withRegistry("r.git", "r")), 'g', '+', true, false},
		{"global on disable", mkRow("x", withGlobal, withRegistry("r.git", "r")), 'g', '-', false, false},
		{"project off enable", mkRow("x", withRegistry("r.git", "r")), 'p', '+', false, true},
		{"project off disable", mkRow("x", withRegistry("r.git", "r")), 'p', '-', true, false},
		{"project on enable", mkRow("x", withProject, withRegistry("r.git", "r")), 'p', '+', true, false},
		{"project on disable", mkRow("x", withProject, withRegistry("r.git", "r")), 'p', '-', false, false},
		{"local off enable", mkRow("x", withRegistry("r.git", "r")), 'l', '+', false, true},
		{"local off disable", mkRow("x", withRegistry("r.git", "r")), 'l', '-', true, false},
		{"local on enable", mkRow("x", withLocal, withRegistry("r.git", "r")), 'l', '+', true, false},
		{"local on disable", mkRow("x", withLocal, withRegistry("r.git", "r")), 'l', '-', false, false},
		{"exclude notexcluded enable", mkRow("x", withRegistry("r.git", "r")), 'e', '+', true, false},
		{"exclude notexcluded disable", mkRow("x", withRegistry("r.git", "r")), 'e', '-', false, true},
		{"exclude excluded enable", mkRow("x", withExcluded, withRegistry("r.git", "r")), 'e', '+', false, false},
		{"exclude excluded disable", mkRow("x", withExcluded, withRegistry("r.git", "r")), 'e', '-', true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newViewModel(t, []row{tc.r})
			m.cursor = 0 // sole section header
			mi, _ := m.Update(keyNamed("space"))
			m = mi.(Model)
			if m.pane != paneBulkToggle {
				t.Fatalf("space did not open the bulk modal, pane=%v", m.pane)
			}
			mi, _ = m.Update(keyPress(tc.scopeKey))
			m = mi.(Model)
			mi, _ = m.Update(keyPress(tc.dirKey))
			m = mi.(Model)
			if m.bulkModal.stage != bulkStageConfirm {
				t.Fatalf("expected confirm stage, got %v", m.bulkModal.stage)
			}
			acts, targeted, _, err := m.bulkActions(m.bulkModal)
			if err != nil {
				t.Fatalf("bulkActions: %v", err)
			}
			if tc.wantSkip {
				if targeted != 0 || len(acts) != 0 {
					t.Fatalf("already-in-state row must be skipped, got %d acts %+v", targeted, acts)
				}
				return
			}
			if targeted != 1 || len(acts) != 1 {
				t.Fatalf("expected one targeted action, got %d", targeted)
			}
			if acts[0].enable != tc.wantEnable {
				t.Fatalf("act.enable = %v, want %v", acts[0].enable, tc.wantEnable)
			}
		})
	}
}

// spec: tui-feedback/bulk-action-emits-one-bounded-span
func TestBulkToggleEmitsOnePrivacySafeSpan(t *testing.T) {
	e := newTestSpanExporter(t)
	ctx, tel := setupTuiTelemetry(t, e)
	defer func() { _ = tel.Shutdown(context.Background()) }()

	m := helperFlatCursorOnSection(t, groupingTestRows(), "status:enabled")
	m.ctx = ctx
	mi, _ := m.Update(keyNamed("space"))
	m = mi.(Model)
	mi, _ = m.Update(keyPress('p'))
	m = mi.(Model)
	mi, _ = m.Update(keyPress('-'))
	m = mi.(Model)
	// The confirm stage holds the evaluated batch the span must report.
	wantTargeted := strconv.Itoa(len(m.bulkModal.confirmed))
	wantSkipped := strconv.Itoa(m.bulkModal.confirmedSkipped)
	mi, _ = m.Update(keyNamed("enter"))
	m = mi.(Model)

	s := e.waitForSpan("agent_marketplace.tui.action", 5*time.Second)
	if s == nil {
		t.Fatalf("expected one bulk toggle action span")
	}
	if got := attrValue(s.Attributes, "action"); got != "bulk-toggle" {
		t.Fatalf("action attribute = %q", got)
	}
	bulkSpans := 0
	for _, sp := range e.spansNamed("agent_marketplace.tui.action") {
		if attrValue(sp.Attributes, "action") == "bulk-toggle" {
			bulkSpans++
		}
	}
	if bulkSpans != 1 {
		t.Fatalf("expected exactly one bulk-toggle span, got %d", bulkSpans)
	}
	for key, want := range map[string]string{
		"grouping.primary":   "status",
		"grouping.secondary": "none",
		"scope":              "project",
		"direction":          "disable",
		"count.targeted":     wantTargeted,
		"count.skipped":      wantSkipped,
	} {
		if got := attrValue(s.Attributes, key); got != want {
			t.Fatalf("span %s = %q, want %q", key, got, want)
		}
	}
	for _, a := range s.Attributes {
		if a.Value == nil {
			continue
		}
		v := a.Value.GetStringValue()
		for _, forbidden := range []string{"sec.git", "security", "alpha-skill", "repo.git", "frontend", "t.git"} {
			if strings.Contains(v, forbidden) || strings.Contains(a.Key, "name") || strings.Contains(a.Key, "tab") {
				t.Fatalf("span leaks identifying data %q=%q", a.Key, v)
			}
		}
	}
}

// spec: tui-interactions/scope-direction-confirmation-and-cancellation
func TestBulkConfirmationRefreshesOnChangedTargets(t *testing.T) {
	rows := []row{
		mkRow("alpha", withProject, withRegistry("https://sec.example/repo.git", "security"), withRegistryMeta("main", "")),
		mkRow("beta", withProject, withRegistry("https://sec.example/repo.git", "security"), withRegistryMeta("main", "")),
	}
	m := helperFlatCursorOnSection(t, rows, "status:enabled")
	mutations := 0
	m.dependencyLifecycle.Mutate = func(_ context.Context, _ pipeline.Workspace, edit func(pipeline.ConfigPathMapper) error) error {
		mutations++
		dir := t.TempDir()
		n := 0
		return edit(func(string) (string, error) {
			n++
			return filepath.Join(dir, fmt.Sprintf("staged-%d.yaml", n)), nil
		})
	}
	mi, _ := m.Update(keyNamed("space"))
	m = mi.(Model)
	mi, _ = m.Update(keyPress('p'))
	m = mi.(Model)
	mi, _ = m.Update(keyPress('-'))
	m = mi.(Model)
	if len(m.bulkModal.confirmed) != 2 {
		t.Fatalf("confirmation should capture 2 targets, got %d", len(m.bulkModal.confirmed))
	}
	// Membership changes between the displayed confirmation and Enter.
	m.rows = rows[:1]
	m.filteredRows = m.rows
	m.recomputeView()
	mi, cmd := m.Update(keyNamed("enter"))
	m = mi.(Model)
	if cmd != nil {
		if _, isWrite := cmd().(configWrittenMsg); isWrite {
			t.Fatalf("first Enter after a membership change must not write")
		}
	}
	if mutations != 0 {
		t.Fatalf("changed confirmation must not start a mutation")
	}
	if m.pane != paneBulkToggle || len(m.bulkModal.confirmed) != 1 {
		t.Fatalf("first Enter must refresh the confirmation to the revised set, got pane=%v confirmed=%d", m.pane, len(m.bulkModal.confirmed))
	}
	body := ansi.Strip(renderBulkToggleModal(m))
	if !strings.Contains(body, "disable 1 resources") {
		t.Fatalf("revised confirmation must show the new count, got:\n%s", body)
	}
	mi, cmd = m.Update(keyNamed("enter"))
	m = mi.(Model)
	if m.pane == paneBulkToggle {
		t.Fatalf("second Enter must submit the revised set")
	}
	if cmd == nil {
		t.Fatalf("submit must return the operation command")
	}
	msg, isWrite := cmd().(configWrittenMsg)
	if !isWrite {
		t.Fatalf("submit command must produce a config write result, got %T", msg)
	}
	if msg.err != nil {
		t.Fatalf("revised set must publish successfully, got %v", msg.err)
	}
	if mutations != 1 {
		t.Fatalf("revised set must commit through exactly one mutation, got %d", mutations)
	}
}

// spec: tui-interactions/atomic-batch-success-and-failure
func TestBulkSubmitThroughModelUpdateRunsOneMutationAndOneReload(t *testing.T) {
	f := newBulkFixture(t)
	m := helperFlatCursorOnSection(t, f.snap.rows, "status:available")
	m.workspace = f.workspace
	m.dependencyLifecycle.Mutate = f.mutate()
	genBefore := m.requests.Active(requestSnapshot)

	mi, _ := m.Update(keyNamed("space"))
	m = mi.(Model)
	mi, _ = m.Update(keyPress('p'))
	m = mi.(Model)
	mi, _ = m.Update(keyPress('+'))
	m = mi.(Model)
	mi, cmd := m.Update(keyNamed("enter"))
	m = mi.(Model)
	if cmd == nil {
		t.Fatalf("confirmation Enter must submit the batch")
	}
	msg, ok := cmd().(configWrittenMsg)
	if !ok {
		t.Fatalf("submit command must produce one config write result, got %T", cmd())
	}
	if msg.err != nil {
		t.Fatalf("batch failed: %v", msg.err)
	}
	if f.mutations != 1 || f.reconciles != 1 {
		t.Fatalf("expected exactly one mutation and one reconcile, got %d/%d", f.mutations, f.reconciles)
	}
	// The write result must allocate exactly one new snapshot generation for
	// the authoritative reload.
	mi, reload := m.Update(msg)
	m = mi.(Model)
	if reload == nil {
		t.Fatalf("completion must schedule the authoritative reload")
	}
	genAfter := m.requests.Active(requestSnapshot)
	if genAfter != genBefore+1 {
		t.Fatalf("expected exactly one new snapshot generation, %d -> %d", genBefore, genAfter)
	}
	if m.pendingToggleReload != genAfter {
		t.Fatalf("pending reload = %d, want the new generation %d", m.pendingToggleReload, genAfter)
	}
	// A stale pre-batch completion is rejected; the reload generation lands.
	mi, _ = m.Update(snapshotLoadedMsg{generation: genBefore, mergedCfg: &config.Config{}, driftAssessment: driftReady})
	m = mi.(Model)
	if m.pendingToggleReload == 0 {
		t.Fatalf("stale generation must not complete the pending reload")
	}
	mi, _ = m.Update(snapshotLoadedMsg{generation: genAfter, mergedCfg: &config.Config{}, driftAssessment: driftReady})
	m = mi.(Model)
	if m.pendingToggleReload != 0 {
		t.Fatalf("reload generation %d did not complete the pending reload", genAfter)
	}
	assertNoConfigStaging(t)
}

// spec: tui-rendering/groupings-and-modals-remain-safe-at-supported-sizes
func TestModalsRenderBoundedAcrossSupportedSizes(t *testing.T) {
	assertBounded := func(t *testing.T, label, frame string, w, h int, controls ...string) {
		t.Helper()
		lines := strings.Split(frame, "\n")
		if len(lines) > h {
			t.Fatalf("%s at %dx%d: %d lines > height %d", label, w, h, len(lines), h)
		}
		for i, l := range lines {
			if n := ansi.StringWidth(l); n > w {
				t.Fatalf("%s at %dx%d: line %d overflows: %d > %d", label, w, h, i, n, w)
			}
		}
		plain := ansi.Strip(frame)
		for _, c := range controls {
			if !strings.Contains(plain, c) {
				t.Fatalf("%s at %dx%d: missing control %q in:\n%s", label, w, h, c, plain)
			}
		}
	}
	assertQuits := func(t *testing.T, label string, m Model) {
		t.Helper()
		_, cmd := m.Update(ctrlCKey())
		if cmd == nil {
			t.Fatalf("%s: ctrl+c must stay reachable", label)
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Fatalf("%s: ctrl+c must quit", label)
		}
	}
	for _, sz := range []struct{ w, h int }{{50, 12}, {80, 24}, {120, 34}} {
		label := fmt.Sprintf("%dx%d", sz.w, sz.h)
		// The esc hint fits the footer from 80 columns up; at 50 it is still
		// functionally reachable via the checks below.
		esc := []string{}
		if sz.w > 50 {
			esc = []string{"esc"}
		}
		rows := append(groupingTestRows(),
			mkRow("βeta—日本語", withProject, withRegistry("uni.example/r.git", "únïcode 路径")))
		m := helperFlatCursorOnSection(t, rows, "status:enabled")
		m.width, m.height = sz.w, sz.h
		// Hostile multi-line filter string; members stay populated.
		m.filter = "β\n hostile→\x1b[7m"

		// View settings modal.
		mi, _ := m.Update(keyPress('v'))
		m = mi.(Model)
		assertBounded(t, label+" view-settings", renderViewSettingsModal(m), sz.w, sz.h, append([]string{"tab", "enter", "Status", "Source", "Kind"}, esc...)...)
		assertBounded(t, label+" view-settings composed", m.View().Content, sz.w, sz.h)
		assertQuits(t, label+" view-settings", m)
		mi, _ = m.Update(keyNamed("esc"))
		m = mi.(Model)

		// Bulk modal at each stage.
		mi, _ = m.Update(keyNamed("space"))
		m = mi.(Model)
		if m.pane != paneBulkToggle {
			t.Fatalf("%s: space must open the bulk modal", label)
		}
		assertBounded(t, label+" bulk-scope", renderBulkToggleModal(m), sz.w, sz.h, append([]string{"[g]", "[p]", "[l]", "[e]"}, esc...)...)
		assertBounded(t, label+" bulk-scope composed", m.View().Content, sz.w, sz.h)
		assertQuits(t, label+" bulk-scope", m)
		mi, _ = m.Update(keyPress('p'))
		m = mi.(Model)
		assertBounded(t, label+" bulk-direction", renderBulkToggleModal(m), sz.w, sz.h, append([]string{"[+]", "[-]"}, esc...)...)
		assertQuits(t, label+" bulk-direction", m)
		mi, _ = m.Update(keyPress('-'))
		m = mi.(Model)
		frame := renderBulkToggleModal(m)
		assertBounded(t, label+" bulk-confirm", frame, sz.w, sz.h, append([]string{"enter"}, esc...)...)
		assertBounded(t, label+" bulk-confirm composed", m.View().Content, sz.w, sz.h)
		assertQuits(t, label+" bulk-confirm", m)
		// The confirmation Enter hint appears exactly once; stage accelerator
		// keys live in the body and are not duplicated in the footer.
		if n := strings.Count(ansi.Strip(frame), "enter"); n != 1 {
			t.Fatalf("%s bulk-confirm: %d enter hints, want 1", label, n)
		}
		mi, _ = m.Update(keyNamed("esc"))
		m = mi.(Model)
		if m.pane != paneList {
			t.Fatalf("%s: esc must cancel the bulk modal", label)
		}
	}
}
