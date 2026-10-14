package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func requiringResource(name string, requires ...any) ir.Resource {
	return ir.NewSkill(name, name+" description", "", nil,
		map[string]any{"metadata": map[string]any{"requires": requires}}, ir.SourceRef{})
}

func requiringRow(name string, requires ...any) row {
	r := mkRow(name)
	r.Artifact.Resource = requiringResource(name, requires...)
	return r
}

func resolvedArtifact(name string) resolver.ResolvedArtifact {
	return resolver.ResolvedArtifact{Entry: catalog.Entry{Kind: ir.KindSkill, Name: name}}
}

func detailText(t *testing.T, r row) string {
	t.Helper()
	m := modelWithSelectedRow(t, r)
	m.width = 80
	out := renderDetail(m)
	if out == "" {
		t.Fatal("renderDetail produced no output")
	}
	return ansi.Strip(out)
}

func requireLine(t *testing.T, out, want string) {
	t.Helper()
	if !strings.Contains(out, want) {
		t.Fatalf("detail pane missing %q:\n%s", want, out)
	}
}

// spec: tui-rendering/requires-section-marks-each-prerequisite-state
func TestRequiresSectionMarksEachPrerequisiteState(t *testing.T) {
	parent := requiringRow("parent",
		"skill:met-dep", "skill:implied-dep", "skill:preview-dep", "skill:excluded-dep", "skill:unmet-dep")
	resolved := []resolver.ResolvedArtifact{
		parent.Artifact,
		resolvedArtifact("met-dep"),
		{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "implied-dep"}, Implied: true},
	}
	preview := []resolver.ResolvedArtifact{
		{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "preview-dep"}, Implied: true},
	}
	errs := []error{
		&resolver.UnmetRequirementWarning{ResourceKind: ir.KindSkill, ResourceName: "parent",
			Requirement: catalog.Requirement{Kind: ir.KindSkill, Name: "excluded-dep"}, Reason: resolver.UnmetReasonExcluded},
		&resolver.UnmetRequirementWarning{ResourceKind: ir.KindSkill, ResourceName: "parent",
			Requirement: catalog.Requirement{Kind: ir.KindSkill, Name: "unmet-dep"}, Reason: resolver.UnmetReasonNotFound},
	}
	rows := []row{parent}
	annotatePrerequisiteRows(rows, resolved, preview, errs)

	out := detailText(t, rows[0])
	requireLine(t, out, "Requires:")
	markers := []string{
		`met skill "met-dep"`,
		`implied skill "implied-dep"`,
		`preview skill "preview-dep"`,
		`excluded skill "excluded-dep"`,
		`unmet skill "unmet-dep"`,
	}
	last := -1
	for _, marker := range markers {
		at := strings.Index(out, marker)
		if at < 0 {
			t.Fatalf("detail pane missing marker %q:\n%s", marker, out)
		}
		if at < last {
			t.Fatalf("marker %q out of declaration order:\n%s", marker, out)
		}
		last = at
	}
}

// spec: tui-rendering/required-by-section-uses-prerequisite-provenance
func TestRequiredBySectionUsesPrerequisiteProvenance(t *testing.T) {
	member := mkRow("member")
	member.Artifact.Via = "collection:tools@1.0.0"
	member.Artifact.RequiredBy = []resolver.ResourceRef{{Kind: ir.KindSkill, Name: "parent"}}

	out := detailText(t, member)
	requireLine(t, out, "Required by:")
	requireLine(t, out, `skill "parent"`)
	if strings.Contains(out, `collection "tools"`) {
		t.Fatalf("collection provenance must not appear under Required by:\n%s", out)
	}
}

// spec: tui-rendering/required-by-section-uses-prerequisite-provenance
func TestImpliedRowShowsRequiredBySection(t *testing.T) {
	implied := mkRow("implied-dep")
	implied.Artifact.Implied = true
	implied.Artifact.RequiredBy = []resolver.ResourceRef{{Kind: ir.KindSkill, Name: "parent"}}

	out := detailText(t, implied)
	requireLine(t, out, "Required by:")
	requireLine(t, out, `skill "parent"`)
}

// spec: tui-rendering/resources-without-relationships-show-no-prerequisite-sections
func TestResourcesWithoutRelationshipsShowNoPrerequisiteSections(t *testing.T) {
	plain := mkRow("plain")
	rows := []row{plain}
	annotatePrerequisiteRows(rows, []resolver.ResolvedArtifact{plain.Artifact}, nil, nil)
	out := detailText(t, rows[0])
	if strings.Contains(out, "Requires:") || strings.Contains(out, "Required by:") {
		t.Fatalf("unrelated resource must not render prerequisite sections:\n%s", out)
	}

	// Malformed declarations are dropped by the catalog parser, keep their
	// existing diagnostic elsewhere, and produce no Requires section.
	bad := requiringRow("bad", 123, map[string]any{"kind": "bogus", "name": "x"})
	rows = []row{bad}
	annotatePrerequisiteRows(rows, []resolver.ResolvedArtifact{bad.Artifact}, nil, nil)
	out = detailText(t, rows[0])
	if strings.Contains(out, "Requires:") || strings.Contains(out, "Required by:") {
		t.Fatalf("malformed declarations must not render prerequisite sections:\n%s", out)
	}
}

// spec: tui-rendering/prerequisite-text-is-escaped-and-redacted
func TestPrerequisiteTextEscapedAndRedacted(t *testing.T) {
	evil := requiringRow("evil",
		map[string]any{"kind": "skill", "name": "dep\x1b[31mX\x07", "registry": "https://user:secret@example.com/r.git"})
	rows := []row{evil}
	annotatePrerequisiteRows(rows, []resolver.ResolvedArtifact{evil.Artifact}, nil, nil)

	raw := renderDetail(func() Model {
		m := modelWithSelectedRow(t, rows[0])
		m.width = 80
		return m
	}())
	if strings.Contains(raw, "\x1b[31m") || strings.Contains(raw, "\x07") {
		t.Fatalf("terminal control sequences must be escaped:\n%q", raw)
	}
	out := ansi.Strip(raw)
	if strings.Contains(out, "secret") {
		t.Fatalf("credential-bearing hint must be redacted:\n%s", out)
	}
	if !strings.Contains(out, "***@example.com/r.git") {
		t.Fatalf("redacted hint expected in detail pane:\n%s", out)
	}
}

// spec: tui-rendering/prerequisite-text-is-escaped-and-redacted
func TestRequirementHintShowsRegistryAndPath(t *testing.T) {
	hint := requirementHint(catalog.Requirement{
		Kind:     ir.KindSkill,
		Name:     "dep",
		Registry: "https://user:secret@example.com/r.git",
		Path:     "libs/dep",
	})
	if !strings.Contains(hint, "***@example.com/r.git") {
		t.Fatalf("redacted registry hint expected, got %q", hint)
	}
	if !strings.Contains(hint, "libs/dep") {
		t.Fatalf("path hint expected alongside the registry hint, got %q", hint)
	}
}

func flatRowText(r row) string {
	return formatFlatItem(flatItem{Kind: flatArtifact, Row: r, Depth: 1}, 80, false)
}

// spec: tui-rendering/implied-and-unmet-rows-are-marked
func TestImpliedAndUnmetRowsAreMarked(t *testing.T) {
	implied := mkRow("implied-dep")
	implied.Artifact.Implied = true
	if out := flatRowText(implied); !strings.Contains(out, "req") {
		t.Fatalf("implied row must carry a req badge:\n%s", out)
	}

	requirer := requiringRow("requirer", "skill:missing-dep")
	rows := []row{requirer}
	annotatePrerequisiteRows(rows, []resolver.ResolvedArtifact{requirer.Artifact}, nil, []error{
		&resolver.UnmetRequirementWarning{ResourceKind: ir.KindSkill, ResourceName: "requirer",
			Requirement: catalog.Requirement{Kind: ir.KindSkill, Name: "missing-dep"}, Reason: resolver.UnmetReasonNotFound},
	})
	if out := flatRowText(rows[0]); !strings.Contains(out, "⚠") {
		t.Fatalf("requiring row must carry a warning marker:\n%s", out)
	}
}

// spec: tui-rendering/preview-only-and-unrelated-rows-are-not-marked
func TestPreviewOnlyAndUnrelatedRowsAreNotMarked(t *testing.T) {
	preview := mkRow("preview-dep")
	preview.Artifact.Implied = true
	preview.Artifact.PreviewOnly = true
	if out := flatRowText(preview); strings.Contains(out, "req") {
		t.Fatalf("preview-only row must not carry a req badge:\n%s", out)
	}

	plain := mkRow("plain")
	rows := []row{plain}
	annotatePrerequisiteRows(rows, []resolver.ResolvedArtifact{plain.Artifact}, nil, nil)
	got := flatRowText(rows[0])
	want := flatRowText(plain)
	if got != want {
		t.Fatalf("unrelated row must render unchanged:\n got %q\nwant %q", got, want)
	}
}

// requiresFixture builds a directory-source registry whose "parent" skill
// requires "child", plus a v4 project config. expand controls requires.expand;
// resources is the v4 resources block for the source (e.g. "{include: [skill/parent]}").
// extra maps additional or replacement skill names to their required skill
// names (nil for none), written after the defaults.
func newRequiresFixture(t *testing.T, expand bool, resources string, extra ...map[string][]string) *bulkFixture {
	t.Helper()
	home := t.TempDir()
	repo := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".state"))
	t.Setenv("TMPDIR", t.TempDir())
	source := filepath.Join(t.TempDir(), "source")
	write := func(rel, content string) {
		path := filepath.Join(source, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeSkill := func(name string, requires []string) {
		body := fmt.Sprintf("---\nname: %s\ndescription: %s skill\n", name, name)
		if len(requires) > 0 {
			body += "metadata:\n  requires:\n"
			for _, req := range requires {
				body += "    - skill:" + req + "\n"
			}
		}
		body += "---\n\n# " + name + "\n"
		write(filepath.Join("skills", name, "SKILL.md"), body)
	}
	writeSkill("parent", []string{"child"})
	writeSkill("child", nil)
	writeSkill("other", nil)
	for _, skills := range extra {
		for name, requires := range skills {
			writeSkill(name, requires)
		}
	}

	projectPath := filepath.Join(repo, ".agents", "config.yaml")
	project := fmt.Sprintf("apiVersion: agent-marketplace/v4\nharnesses: [devin]\nrequires: {expand: %v}\nsources:\n  local:\n    directory: %s\nresources:\n  local: %s\n", expand, source, resources)
	if err := os.MkdirAll(filepath.Dir(projectPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(projectPath, []byte(project), 0o640); err != nil {
		t.Fatal(err)
	}
	originalOrg, originalGlobal := loadOrgConfig, loadGlobalConfig
	originalRepo, originalLocal := loadRepoConfig, loadLocalConfig
	t.Cleanup(func() {
		loadOrgConfig, loadGlobalConfig = originalOrg, originalGlobal
		loadRepoConfig, loadLocalConfig = originalRepo, originalLocal
	})
	loadOrgConfig = func() (*config.Config, error) { return nil, nil }
	loadGlobalConfig = func() (*config.Config, error) { return nil, nil }
	loadRepoConfig = func() (*config.Config, error) { return config.LoadConfigFromPath(projectPath) }
	loadLocalConfig = func(string) (*config.LocalConfig, error) { return nil, nil }

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

func runToggle(t *testing.T, f *bulkFixture, act toggleAction) configWrittenMsg {
	t.Helper()
	msg, ok := runDependencyScopedToggle(context.Background(), f.workspace, act, f.mutate())().(configWrittenMsg)
	if !ok {
		t.Fatalf("expected configWrittenMsg")
	}
	if msg.err != nil {
		t.Fatalf("toggle failed: %v", msg.err)
	}
	return msg
}

func unmetWarnings(errs []error) []*resolver.UnmetRequirementWarning {
	var out []*resolver.UnmetRequirementWarning
	for _, err := range errs {
		var warning *resolver.UnmetRequirementWarning
		if errors.As(err, &warning) {
			out = append(out, warning)
		}
	}
	return out
}

// spec: tui-interactions/enabled-toggle-off-excludes-a-still-required-resource
func TestEnabledToggleOffExcludesStillRequiredResource(t *testing.T) {
	f := newRequiresFixture(t, true, "{include: [skill/parent]}")

	// Expansion selected child as an implied prerequisite of parent.
	child := findV4ToggleRow(t, f.snap.rows, "child")
	if !child.Artifact.Implied {
		t.Fatalf("child should be an implied prerequisite row, got %+v", child.Artifact)
	}
	if len(child.Artifact.RequiredBy) != 1 || child.Artifact.RequiredBy[0].Name != "parent" {
		t.Fatalf("child RequiredBy = %v, want [parent]", child.Artifact.RequiredBy)
	}

	// The implied row's owning-scope toggle writes the effective exclusion
	// (a repo-scope implied resource turns off through the project layer).
	act := scopedToggleAction(child, scopeProject)
	if !act.disablesResource() {
		t.Fatalf("toggling an implied row at its owning scope must disable, act=%+v", act)
	}
	runToggle(t, f, act)

	updated := string(mustReadFile(t, f.projectPath))
	if !strings.Contains(updated, "exclude") || !strings.Contains(updated, "skill/child") {
		t.Fatalf("toggle off an implied resource must write the effective exclusion:\n%s", updated)
	}
	if !strings.Contains(updated, "skill/parent") {
		t.Fatalf("the requiring resource must remain selected:\n%s", updated)
	}
	assertNoConfigStaging(t)

	// The reload reports parent's unmet prerequisite and the toggle feedback
	// names it for the footer.
	reloaded := readSnapshot(context.Background(), f.repo, f.home, catalogLoadRefresh)
	unmet := unmetWarnings(append(append([]error(nil), reloaded.warnings...), reloaded.errs...))
	if len(unmet) == 0 {
		t.Fatalf("expected an unmet prerequisite warning after exclusion, warnings=%v errs=%v", reloaded.warnings, reloaded.errs)
	}
	var found bool
	for _, warning := range unmet {
		if warning.ResourceName == "parent" && warning.Requirement.Name == "child" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected parent's unmet prerequisite on child, got %v", unmet)
	}
	notes := prerequisiteToggleNotes(&act, reloaded, nil)
	if len(notes) == 0 || !strings.Contains(notes[0], `"parent"`) || !strings.Contains(notes[0], `"child"`) {
		t.Fatalf("footer must name each dependent's unmet prerequisite, notes=%v", notes)
	}
}

// spec: tui-interactions/enabled-toggle-off-excludes-a-still-required-resource
func TestEnabledToggleOffDirectlySelectedRequiredWritesExclusion(t *testing.T) {
	f := newRequiresFixture(t, true, "{include: [skill/parent, skill/child]}")

	child := findV4ToggleRow(t, f.snap.rows, "child")
	if child.Artifact.Implied || len(child.Artifact.RequiredBy) == 0 {
		t.Fatalf("directly selected child must carry prerequisite provenance, got %+v", child.Artifact)
	}
	act := scopedToggleAction(child, scopeProject)
	runToggle(t, f, act)

	updated := string(mustReadFile(t, f.projectPath))
	if !strings.Contains(updated, "exclude") || !strings.Contains(updated, "skill/child") {
		t.Fatalf("toggle off must remove the direct selection and write the exclusion:\n%s", updated)
	}
	reloaded := readSnapshot(context.Background(), f.repo, f.home, catalogLoadRefresh)
	notes := prerequisiteToggleNotes(&act, reloaded, nil)
	if len(notes) == 0 {
		t.Fatalf("footer must show the dependent's resulting unmet warning")
	}
	assertNoConfigStaging(t)
}

// spec: tui-interactions/advisory-toggle-off-writes-no-prerequisite-exclusion
func TestAdvisoryToggleOffWritesNoPrerequisiteExclusion(t *testing.T) {
	f := newRequiresFixture(t, false, "{include: [skill/parent, skill/child]}")

	child := findV4ToggleRow(t, f.snap.rows, "child")
	act := scopedToggleAction(child, scopeProject)
	runToggle(t, f, act)
	childDiff := string(mustReadFile(t, f.projectPath))

	// Reload before the control fixture rebinds the global config loaders.
	reloaded := readSnapshot(context.Background(), f.repo, f.home, catalogLoadRefresh)
	notes := prerequisiteToggleNotes(&act, reloaded, nil)

	// A control disabling an unrelated resource proves the written bytes are
	// the ordinary toggle-off exclusion, not a prerequisite-specific one.
	control := newRequiresFixture(t, false, "{include: [skill/other, skill/child]}")
	controlAct := scopedToggleAction(findV4ToggleRow(t, control.snap.rows, "other"), scopeProject)
	runToggle(t, control, controlAct)
	controlDiff := string(mustReadFile(t, control.projectPath))

	if !strings.Contains(childDiff, "exclude") || !strings.Contains(controlDiff, "exclude") {
		t.Fatalf("ordinary toggle-off exclusion expected:\n%s\n---\n%s", childDiff, controlDiff)
	}
	if exclude := childDiff[strings.Index(childDiff, "exclude"):]; strings.Contains(exclude, "parent") {
		t.Fatalf("must not exclude the requiring resource:\n%s", childDiff)
	}
	if !strings.Contains(childDiff, "skill/parent") {
		t.Fatalf("the requiring resource must remain selected:\n%s", childDiff)
	}
	if _, err := os.Stat(filepath.Join(f.repo, ".agents", "config.local.yaml")); !os.IsNotExist(err) {
		t.Fatalf("advisory toggle-off must not write another scope's file")
	}
	if len(notes) == 0 {
		t.Fatalf("footer still shows the dependent's resulting unmet warning")
	}
	assertNoConfigStaging(t)
}

// spec: tui-interactions/toggle-off-keeps-policy-and-scope-authorization
func TestToggleOffKeepsPolicyAndScopeAuthorization(t *testing.T) {
	f := newRequiresFixture(t, true, "{include: [skill/parent, skill/child], required: true}")

	child := findV4ToggleRow(t, f.snap.rows, "child")
	if !child.Artifact.Required {
		t.Fatalf("required fixture must resolve child as required")
	}
	m := newViewModel(t, f.snap.rows)
	if denial := m.rowToggleEligibility(child); denial != toggleRequired {
		t.Fatalf("required-selection policy must not be bypassed, denial=%v", denial)
	}
	before := mustReadFile(t, f.projectPath)
	if after := mustReadFile(t, f.projectPath); string(after) != string(before) {
		t.Fatalf("a denied toggle must not write")
	}
}

// spec: tui-interactions/enabled-toggle-on-reports-also-selected-prerequisites
func TestEnabledToggleOnReportsAlsoSelectedPrerequisites(t *testing.T) {
	f := newRequiresFixture(t, true, "{include: [skill/parent]}")
	act := toggleAction{kind: ir.KindSkill, skillName: "parent", scope: scopeProject, enable: true}
	notes := prerequisiteToggleNotes(&act, f.snap, nil)
	if len(notes) != 1 {
		t.Fatalf("expected one also-selected note, got %v", notes)
	}
	want := `also selected skill "child" (required by skill "parent")`
	if notes[0] != want {
		t.Fatalf("note = %q, want %q", notes[0], want)
	}
}

// spec: tui-interactions/advisory-toggle-on-reports-would-also-select-prerequisites
func TestAdvisoryToggleOnReportsWouldAlsoSelectPrerequisites(t *testing.T) {
	f := newRequiresFixture(t, false, "{include: [skill/parent]}")
	act := toggleAction{kind: ir.KindSkill, skillName: "parent", scope: scopeProject, enable: true}
	notes := prerequisiteToggleNotes(&act, f.snap, nil)
	if len(notes) != 1 {
		t.Fatalf("expected one would-also-select note, got %v", notes)
	}
	want := `would also select skill "child" (required by skill "parent")`
	if notes[0] != want {
		t.Fatalf("note = %q, want %q", notes[0], want)
	}
}

// spec: tui-interactions/enabled-toggle-on-reports-also-selected-prerequisites
func TestEnabledToggleOnReportsTransitivePrerequisites(t *testing.T) {
	f := newRequiresFixture(t, true, "{include: [skill/parent]}",
		map[string][]string{"child": {"grandchild"}, "grandchild": nil})
	act := toggleAction{kind: ir.KindSkill, skillName: "parent", scope: scopeProject, enable: true}
	notes := prerequisiteToggleNotes(&act, f.snap, nil)
	want := []string{
		`also selected skill "child" (required by skill "parent")`,
		`also selected skill "grandchild" (required by skill "child")`,
	}
	if len(notes) != len(want) {
		t.Fatalf("expected %d also-selected notes, got %v", len(want), notes)
	}
	for i, note := range notes {
		if note != want[i] {
			t.Fatalf("note[%d] = %q, want %q (all notes %v)", i, note, want[i], notes)
		}
	}
}

// spec: tui-interactions/advisory-toggle-on-reports-would-also-select-prerequisites
func TestAdvisoryToggleOnReportsTransitivePrerequisites(t *testing.T) {
	f := newRequiresFixture(t, false, "{include: [skill/parent]}",
		map[string][]string{"child": {"grandchild"}, "grandchild": nil})
	act := toggleAction{kind: ir.KindSkill, skillName: "parent", scope: scopeProject, enable: true}
	notes := prerequisiteToggleNotes(&act, f.snap, nil)
	want := []string{
		`would also select skill "child" (required by skill "parent")`,
		`would also select skill "grandchild" (required by skill "child")`,
	}
	if len(notes) != len(want) {
		t.Fatalf("expected %d would-also-select notes, got %v", len(want), notes)
	}
	for i, note := range notes {
		if note != want[i] {
			t.Fatalf("note[%d] = %q, want %q (all notes %v)", i, note, want[i], notes)
		}
	}
}

// spec: tui-interactions/enabled-toggle-on-reports-also-selected-prerequisites
func TestToggleOnSkipsPrerequisitesSelectedBeforehand(t *testing.T) {
	f := newRequiresFixture(t, true, "{include: [skill/parent]}",
		map[string][]string{"parent2": {"child"}})
	// child is already implied through parent before parent2 is toggled on.
	preexisting := preexistingImpliedKeys(f.snap.resolved, f.snap.impliedPreview)
	if len(preexisting) == 0 {
		t.Fatalf("fixture must already imply prerequisites before the toggle")
	}

	act := scopedToggleAction(findV4ToggleRow(t, f.snap.rows, "parent2"), scopeProject)
	runToggle(t, f, act)
	reloaded := readSnapshot(context.Background(), f.repo, f.home, catalogLoadRefresh)
	if notes := prerequisiteToggleNotes(&act, reloaded, preexisting); len(notes) != 0 {
		t.Fatalf("a prerequisite selected before the toggle did not come along with it, notes=%v", notes)
	}
}

// spec: tui-interactions/toggle-on-without-prerequisites-adds-no-note
func TestToggleOnWithoutPrerequisitesAddsNoNote(t *testing.T) {
	f := newRequiresFixture(t, true, "{include: [skill/parent]}")
	act := toggleAction{kind: ir.KindSkill, skillName: "other", scope: scopeProject, enable: true}
	if notes := prerequisiteToggleNotes(&act, f.snap, nil); len(notes) != 0 {
		t.Fatalf("no prerequisite came along, notes=%v", notes)
	}
}

// spec: tui-interactions/enabled-toggle-on-reports-also-selected-prerequisites
func TestToggleFeedbackAppearsInFooter(t *testing.T) {
	f := newRequiresFixture(t, true, "{include: [skill/parent]}")
	act := toggleAction{kind: ir.KindSkill, skillName: "parent", scope: scopeProject, enable: true}
	m := newViewModel(t, nil)
	m.pendingToggle = &act
	m.pendingToggleReload = 1
	m.applySnapshotStatus(snapshotLoadedMsg{
		generation:      1,
		stage:           snapshotStageFinal,
		resolved:        f.snap.resolved,
		mergedCfg:       f.snap.mergedCfg,
		warnings:        f.snap.warnings,
		impliedPreview:  f.snap.impliedPreview,
		driftAssessment: driftReady,
	})
	toasts := m.visibleToasts()
	if len(toasts) == 0 {
		t.Fatalf("footer must note the prerequisite that came along")
	}
	var found bool
	for _, toast := range toasts {
		if strings.Contains(toast.text, `also selected skill "child"`) {
			found = true
		}
	}
	if !found {
		t.Fatalf("footer toasts = %v, want the also-selected note", toasts)
	}
}
