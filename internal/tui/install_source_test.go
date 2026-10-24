package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/skill"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/charmbracelet/x/ansi"
)

// spec: tui-interactions/install-from-source-is-refused-while-mutations-are-unavailable
func TestInstallSourceKeyAndMutationGuard(t *testing.T) {
	if !key.Matches(keyPress('i'), defaultKeyMap.InstallSource) {
		t.Fatal("i does not match the InstallSource binding")
	}
	for _, tc := range []struct {
		name   string
		mutate func(*Model)
	}{
		{name: "active operation", mutate: func(m *Model) { m.operations.begin(operationMutation) }},
		{name: "invalid configuration", mutate: func(m *Model) { m.configInvalid = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := NewModel(t.TempDir(), t.TempDir())
			tc.mutate(&m)
			next, cmd := m.Update(keyPress('i'))
			got := next.(Model)
			if got.pane == paneInstallSource {
				t.Fatal("install source modal opened while mutations were unavailable")
			}
			if cmd == nil {
				t.Fatal("refusal did not explain why install source was unavailable")
			}
			msg := cmd()
			if _, ok := msg.(toastMsg); !ok {
				t.Fatalf("refusal command returned %T, want toastMsg", msg)
			}
		})
	}
}

func TestInstallSourceModalCancelsAndDropsSupersededPreview(t *testing.T) {
	m := NewModel(t.TempDir(), t.TempDir())
	next, _ := m.Update(keyPress('i'))
	m = next.(Model)
	if m.pane != paneInstallSource || m.installSource.step != installSourceStepSource {
		t.Fatalf("after i: pane=%v step=%v, want source modal", m.pane, m.installSource.step)
	}

	old := m.beginRequest(requestInstallPreview)
	current := m.beginRequest(requestInstallPreview)
	next, _ = m.Update(installSourcePreviewLoadedMsg{
		generation: old,
		preview:    &pipeline.SourcePreview{URL: "stale"},
	})
	m = next.(Model)
	if m.installSource.preview != nil || m.requests.active(requestInstallPreview) != current {
		t.Fatalf("superseded preview changed modal: preview=%#v active=%d", m.installSource.preview, m.requests.active(requestInstallPreview))
	}
	next, _ = m.Update(keyNamed("esc"))
	m = next.(Model)
	if m.pane != paneList || m.requests.active(requestInstallPreview) != 0 {
		t.Fatalf("Esc did not cancel modal/request: pane=%v active=%d", m.pane, m.requests.active(requestInstallPreview))
	}
}

func TestInstallSourceURLFieldAcceptsTypedAddress(t *testing.T) {
	m := NewModel(t.TempDir(), t.TempDir())
	next, _ := m.Update(keyPress('i'))
	m = next.(Model)
	const source = "https://example.invalid/quick-source.git"
	for _, char := range source {
		next, _ = m.Update(keyPress(char))
		m = next.(Model)
	}
	if m.pane != paneInstallSource || m.installSource.values.url != source {
		t.Fatalf("typing q canceled or changed URL: pane=%v URL=%q, want %q", m.pane, m.installSource.values.url, source)
	}
}

func TestInstallSourceFilterTreatsQAsInputAndEscLeavesFilter(t *testing.T) {
	m := NewModel(t.TempDir(), t.TempDir())
	next, _ := m.Update(keyPress('i'))
	m = next.(Model)
	loaded := catalog.LoadedCatalog{Entries: []catalog.Entry{{Kind: ir.KindSkill, Name: "query"}}}
	generation := m.beginRequest(requestInstallPreview)
	next, _ = m.Update(installSourcePreviewLoadedMsg{
		generation: generation,
		preview: &pipeline.SourcePreview{
			URL:      "https://example.invalid/source.git",
			Catalogs: map[string]catalog.LoadedCatalog{"source": loaded},
		},
	})
	m = next.(Model)
	next, _ = m.Update(keyPress('/'))
	m = next.(Model)
	next, _ = m.Update(keyPress('q'))
	m = next.(Model)
	if m.pane != paneInstallSource || !m.installSource.filtering || m.installSource.filterInput.Value() != "q" {
		t.Fatalf("q did not remain filter input: pane=%v filtering=%t value=%q", m.pane, m.installSource.filtering, m.installSource.filterInput.Value())
	}
	next, _ = m.Update(keyNamed("esc"))
	m = next.(Model)
	if m.pane != paneInstallSource || m.installSource.filtering || m.installSource.filter != "" {
		t.Fatalf("Esc did not exit filtering without closing modal: pane=%v filtering=%t filter=%q", m.pane, m.installSource.filtering, m.installSource.filter)
	}
}

func TestInstallSourceBackspaceDuringPreviewDropsStalePreview(t *testing.T) {
	const url = "https://example.invalid/slow.git"
	m := NewModel(t.TempDir(), t.TempDir())
	next, _ := m.Update(keyPress('i'))
	m = next.(Model)
	m.installSource.values.url = url
	next, _ = m.Update(keyNamed("enter"))
	m = next.(Model)
	generation := m.requests.active(requestInstallPreview)
	if generation == 0 || !m.installSource.loading {
		t.Fatalf("preview did not start: generation=%d loading=%t", generation, m.installSource.loading)
	}
	next, _ = m.Update(keyNamed("backspace"))
	m = next.(Model)
	if m.installSource.step != installSourceStepSource {
		t.Fatalf("backspace step = %v, want Source", m.installSource.step)
	}
	next, _ = m.Update(installSourcePreviewLoadedMsg{
		generation: generation,
		preview:    &pipeline.SourcePreview{URL: "https://example.invalid/other.git", Ref: "main"},
	})
	m = next.(Model)
	if m.installSource.step != installSourceStepSource || m.installSource.values.url != url || m.installSource.error != "" {
		t.Fatalf("stale preview changed modal: step=%v url=%q error=%q", m.installSource.step, m.installSource.values.url, m.installSource.error)
	}
}

func TestInstallSourceBadgeSkipsUnenrolledRowsAndPrefersSameSource(t *testing.T) {
	const url = "https://example.invalid/source.git"
	const other = "https://example.invalid/other.git"
	choice := installSourceChoice{kind: ir.KindSkill, name: "alpha"}
	artifact := func(registry string) resolver.ResolvedArtifact {
		return resolver.ResolvedArtifact{Kind: ir.KindSkill, Name: "alpha", RegistryURL: registry}
	}
	for _, tc := range []struct {
		name string
		rows []row
		want string
	}{
		{name: "available from same source", rows: []row{{Artifact: artifact(url), IsAvailable: true}}, want: ""},
		{name: "available from another registry", rows: []row{{Artifact: artifact(other), IsAvailable: true}}, want: ""},
		{name: "installed conflict ahead of same source", rows: []row{{Artifact: artifact(other), EnabledProject: true}, {Artifact: artifact(url), EnabledProject: true}}, want: "✓ already installed (P)"},
		{name: "installed conflict only", rows: []row{{Artifact: artifact(other), EnabledProject: true}}, want: "conflicts: installed from other"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := NewModel(t.TempDir(), t.TempDir())
			m.rows = tc.rows
			if got := installSourceBadge(m, url, choice); got != tc.want {
				t.Fatalf("badge = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestInstallSourceRefusalWhileAnotherPaneIsOpen(t *testing.T) {
	const url = "https://example.invalid/source.git"
	const expected = "0123456789abcdef0123456789abcdef01234567"
	const actual = "89abcdef0123456789abcdef0123456789abcdef"
	preview := &pipeline.SourcePreview{
		URL: url, Ref: "main", Commit: expected,
		Catalogs: map[string]catalog.LoadedCatalog{"source": {Entries: []catalog.Entry{{Kind: ir.KindSkill, Name: "alpha"}}}},
	}
	for _, tc := range []struct {
		name      string
		refusal   error
		wantStep  installSourceStep
		wantToast string
	}{
		{name: "locked", refusal: &pipeline.SourceLockedError{URL: url, Ref: "main", Scope: pipeline.ScopeLocal, Locked: actual, Expected: expected}, wantStep: installSourceStepReview, wantToast: "Local lock pins main"},
		{name: "moved", refusal: &pipeline.SourceMovedError{Ref: "main", Expected: expected, Actual: actual}, wantStep: installSourceStepPick, wantToast: "main moved"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := NewModel(t.TempDir(), t.TempDir())
			operation, ok := m.beginOperation(operationMutation)
			if !ok {
				t.Fatal("could not start the install operation fixture")
			}
			m.pane = paneHelp
			m.installSource = installSourceModal{
				step: installSourceStepReview, values: &installSourceValues{url: url, ref: "main"},
				preview: preview, picked: map[string]bool{"skill\x00alpha": true}, scope: scopeLocal,
			}
			next, cmd := m.Update(installSourceInstallMsg{
				operation: operation,
				options:   pipeline.InstallOptions{FromURL: url, FromRef: "main", ExpectedCommit: expected},
				err:       tc.refusal,
			})
			m = next.(Model)
			if m.pane != paneHelp {
				t.Fatalf("refusal changed the open pane to %v, want paneHelp", m.pane)
			}
			if m.installSource.step != tc.wantStep || m.installSource.loading || m.requests.active(requestInstallPreview) != 0 {
				t.Fatalf("refusal state = step %v loading=%t preview request=%d, want step %v without a preview",
					m.installSource.step, m.installSource.loading, m.requests.active(requestInstallPreview), tc.wantStep)
			}
			if !strings.Contains(m.toast, tc.wantToast) || cmd == nil {
				t.Fatalf("refusal toast = %q cmd=%t, want toast containing %q", m.toast, cmd != nil, tc.wantToast)
			}

			m.pane = paneList
			next, _ = m.Update(keyPress('i'))
			m = next.(Model)
			if m.pane != paneInstallSource || m.installSource.step != tc.wantStep {
				t.Fatalf("next i = pane %v step %v, want resumed refusal state at step %v", m.pane, m.installSource.step, tc.wantStep)
			}
			if tc.wantStep == installSourceStepPick {
				if !m.installSource.loading || m.requests.active(requestInstallPreview) == 0 {
					t.Fatalf("moved resume did not re-preview: loading=%t request=%d", m.installSource.loading, m.requests.active(requestInstallPreview))
				}
				if m.installSource.notice != "main moved from 01234567 to 89abcdef; preview again" {
					t.Fatalf("moved resume lost its notice: %q", m.installSource.notice)
				}
			} else if m.installSource.preview == nil {
				t.Fatal("locked resume lost its preview")
			}
		})
	}
}

func TestInstallSourcePasteFillsOpenFilter(t *testing.T) {
	m := NewModel(t.TempDir(), t.TempDir())
	next, _ := m.Update(keyPress('i'))
	m = next.(Model)
	loaded := catalog.LoadedCatalog{Entries: []catalog.Entry{{Kind: ir.KindSkill, Name: "query"}}}
	generation := m.beginRequest(requestInstallPreview)
	next, _ = m.Update(installSourcePreviewLoadedMsg{
		generation: generation,
		preview: &pipeline.SourcePreview{
			URL:      "https://example.invalid/source.git",
			Catalogs: map[string]catalog.LoadedCatalog{"source": loaded},
		},
	})
	m = next.(Model)
	next, _ = m.Update(keyPress('/'))
	m = next.(Model)
	next, _ = m.Update(tea.PasteMsg{Content: "query"})
	m = next.(Model)
	if m.installSource.filterInput.Value() != "query" || !m.installSource.filtering {
		t.Fatalf("paste did not fill the open filter: value=%q filtering=%t", m.installSource.filterInput.Value(), m.installSource.filtering)
	}
}

func TestInstallSourceMovedCommitReturnsToPickAndPreservesSelections(t *testing.T) {
	m := NewModel(t.TempDir(), t.TempDir())
	operation, ok := m.beginOperation(operationMutation)
	if !ok {
		t.Fatal("could not start the install operation fixture")
	}
	const expected = "0123456789abcdef0123456789abcdef01234567"
	const actual = "89abcdef0123456789abcdef0123456789abcdef"
	preview := &pipeline.SourcePreview{
		URL: "https://example.invalid/source.git", Ref: "main", Commit: expected,
		Catalogs: map[string]catalog.LoadedCatalog{"source": {Entries: []catalog.Entry{{Kind: ir.KindSkill, Name: "alpha"}}}},
	}
	m.installSource = installSourceModal{
		step: installSourceStepReview, values: &installSourceValues{url: preview.URL, ref: preview.Ref},
		preview: preview, picked: map[string]bool{"skill\x00alpha": true}, scope: scopeProject,
	}
	next, cmd := m.Update(installSourceInstallMsg{
		operation: operation,
		options:   pipeline.InstallOptions{FromURL: preview.URL, FromRef: preview.Ref, ExpectedCommit: expected},
		err:       &pipeline.SourceMovedError{Ref: "main", Expected: expected, Actual: actual},
	})
	m = next.(Model)
	if cmd == nil || m.pane != paneInstallSource || m.installSource.step != installSourceStepPick {
		t.Fatalf("moved commit result = pane %v step %v cmd=%t", m.pane, m.installSource.step, cmd != nil)
	}
	if m.installSource.notice != "main moved from 01234567 to 89abcdef; preview again" || !m.installSource.picked["skill\x00alpha"] {
		t.Fatalf("moved commit did not preserve notice and selection: notice=%q picked=%#v", m.installSource.notice, m.installSource.picked)
	}
}

func TestInstallSourceLockedCommitReturnsToReviewWithRemediation(t *testing.T) {
	m := NewModel(t.TempDir(), t.TempDir())
	operation, ok := m.beginOperation(operationMutation)
	if !ok {
		t.Fatal("could not start the install operation fixture")
	}
	const expected = "0123456789abcdef0123456789abcdef01234567"
	const locked = "89abcdef0123456789abcdef0123456789abcdef"
	preview := &pipeline.SourcePreview{
		URL: "https://example.invalid/source.git", Ref: "main", Commit: expected,
		Catalogs: map[string]catalog.LoadedCatalog{"source": {Entries: []catalog.Entry{{Kind: ir.KindSkill, Name: "alpha"}}}},
	}
	m.installSource = installSourceModal{
		step: installSourceStepReview, values: &installSourceValues{url: preview.URL, ref: preview.Ref},
		preview: preview, picked: map[string]bool{"skill\x00alpha": true}, scope: scopeLocal,
	}
	next, cmd := m.Update(installSourceInstallMsg{
		operation: operation,
		options:   pipeline.InstallOptions{FromURL: preview.URL, FromRef: preview.Ref, ExpectedCommit: expected},
		err:       &pipeline.SourceLockedError{URL: preview.URL, Ref: "main", Scope: pipeline.ScopeLocal, Locked: locked, Expected: expected},
	})
	m = next.(Model)
	if cmd != nil || m.pane != paneInstallSource || m.installSource.step != installSourceStepReview || m.installSource.loading {
		t.Fatalf("locked commit result = pane %v step %v loading=%t cmd=%t, want Review without a preview", m.pane, m.installSource.step, m.installSource.loading, cmd != nil)
	}
	if !m.installSource.picked["skill\x00alpha"] || m.installSource.scope != scopeLocal {
		t.Fatalf("locked commit lost picks or scope: picked=%#v scope=%v", m.installSource.picked, m.installSource.scope)
	}
	for _, want := range []string{"Local lock pins main at 89abcdef", "preview showed 01234567", "agent-marketplace update --registry " + preview.URL + " --ref main --scope local"} {
		if !strings.Contains(m.installSource.error, want) {
			t.Fatalf("locked commit error = %q, want it to contain %q", m.installSource.error, want)
		}
	}
}

// spec: tui-interactions/blocked-source-is-refused-before-network-access
func TestInstallSourceBlockedPreviewReturnsToSourceWithoutWriting(t *testing.T) {
	repo := t.TempDir()
	configPath := config.ProjectConfigPath(repo)
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	before := []byte("apiVersion: agent-marketplace/v4\nallowed_registries: []\n")
	if err := os.WriteFile(configPath, before, 0o600); err != nil {
		t.Fatal(err)
	}
	ws, err := pipeline.NewWorkspace(repo, repo)
	if err != nil {
		t.Fatal(err)
	}
	m := NewModel(repo, t.TempDir(), WithWorkspace(ws))
	next, _ := m.Update(keyPress('i'))
	m = next.(Model)
	m.installSource.values.url = "https://blocked.invalid/registry.git"
	next, cmd := m.startInstallSourcePreview(false)
	m = next.(Model)
	if cmd == nil {
		t.Fatal("source preview did not start")
	}
	next, _ = m.Update(cmd())
	m = next.(Model)
	if m.installSource.step != installSourceStepSource || !strings.Contains(m.installSource.error, "blocked by allowed_registries") {
		t.Fatalf("blocked source state = step %v error %q", m.installSource.step, m.installSource.error)
	}
	after, err := os.ReadFile(configPath)
	if err != nil || string(after) != string(before) {
		t.Fatalf("config after blocked preview = %q, %v; want unchanged bytes", after, err)
	}
	if _, err := os.Stat(config.RepoCommitted.LockPath(configPath)); !os.IsNotExist(err) {
		t.Fatalf("blocked preview created an acquisition lock: %v", err)
	}
}

// spec: tui-interactions/source-with-several-bundles-asks-for-a-bundle-path
func TestInstallSourceBundleSelectionRepreviewsChosenPath(t *testing.T) {
	repo, source := t.TempDir(), t.TempDir()
	for _, name := range []string{"alpha", "beta"} {
		writeInstallSourceTestFile(t, source, filepath.Join(name, "commands", name+".md"), "---\nname: "+name+"\ndescription: "+name+"\n---\n\n"+name+"\n")
	}
	ws, err := pipeline.NewWorkspace(repo, repo)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := pipeline.PreviewSource(context.Background(), ws, source, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.BundlePaths) != 2 {
		t.Fatalf("preview bundle paths = %#v, want alpha and beta", preview.BundlePaths)
	}
	m := NewModel(repo, t.TempDir(), WithWorkspace(ws))
	next, _ := m.Update(keyPress('i'))
	m = next.(Model)
	generation := m.beginRequest(requestInstallPreview)
	next, _ = m.Update(installSourcePreviewLoadedMsg{generation: generation, preview: &preview})
	m = next.(Model)
	if got := installSourceChoices(m.installSource.preview); len(got) != 2 || got[0].bundlePath != "alpha" {
		t.Fatalf("bundle choices = %#v", got)
	}

	next, cmd := m.Update(keyPress(' '))
	m = next.(Model)
	if cmd == nil || m.installSource.values.path != "alpha" {
		t.Fatalf("bundle selection did not request alpha preview: path=%q cmd=%t", m.installSource.values.path, cmd != nil)
	}
	next, _ = m.Update(cmd())
	m = next.(Model)
	choices := installSourceChoices(m.installSource.preview)
	if m.installSource.preview == nil || m.installSource.preview.Path != "alpha" || len(m.installSource.preview.BundlePaths) != 0 || len(choices) != 1 || choices[0].name != "alpha" {
		t.Fatalf("selected bundle preview = %#v", m.installSource.preview)
	}
}

func TestInstallSourcePickReviewAndScopeEligibility(t *testing.T) {
	m := NewModel(t.TempDir(), t.TempDir())
	next, _ := m.Update(keyPress('i'))
	m = next.(Model)
	m.installSource.values.url = "./source"
	loaded := catalog.LoadedCatalog{
		Entries:   []catalog.Entry{{Kind: ir.KindSkill, Name: "alpha", Description: "Alpha"}},
		Malformed: []skill.MalformedSkill{{Kind: "skill", DirName: "broken", Error: errors.New("invalid frontmatter")}},
	}
	generation := m.beginRequest(requestInstallPreview)
	next, _ = m.Update(installSourcePreviewLoadedMsg{
		generation: generation,
		preview:    &pipeline.SourcePreview{URL: "./source", Catalogs: map[string]catalog.LoadedCatalog{"source": loaded}},
	})
	m = next.(Model)
	if m.installSource.step != installSourceStepPick || !strings.Contains(renderInstallSourceModal(m), "alpha") {
		t.Fatalf("preview did not enter Pick: pane=%v state=%+v", m.pane, m.installSource)
	}
	next, _ = m.Update(keyNamed("enter"))
	m = next.(Model)
	if m.installSource.step != installSourceStepPick || m.installSource.error != "pick at least one resource" {
		t.Fatalf("empty pick advanced to Review: step=%v error=%q", m.installSource.step, m.installSource.error)
	}
	next, _ = m.Update(keyPress(' '))
	m = next.(Model)
	next, _ = m.Update(keyNamed("enter"))
	m = next.(Model)
	if m.installSource.step != installSourceStepReview {
		t.Fatalf("selected resource did not enter Review: step=%v", m.installSource.step)
	}
	if m.installSource.picked[installSourceChoiceKey(installSourceChoice{kind: ir.KindSkill, name: "broken"})] {
		t.Fatal("malformed resource was selected")
	}
	next, _ = m.Update(keyPress('g'))
	m = next.(Model)
	if m.installSource.scope != scopeProject || !strings.Contains(m.installSource.error, "repo-relative") {
		t.Fatalf("ineligible user scope = %v error=%q", m.installSource.scope, m.installSource.error)
	}
	next, _ = m.Update(keyPress('p'))
	m = next.(Model)
	if m.installSource.scope != scopeProject || m.installSource.error != "" {
		t.Fatalf("project scope selection = %v error=%q", m.installSource.scope, m.installSource.error)
	}
}

// spec: tui-interactions/malformed-and-empty-sources-are-not-installable
func TestInstallSourceEmptyPreviewCannotEnterReview(t *testing.T) {
	m := NewModel(t.TempDir(), t.TempDir())
	next, _ := m.Update(keyPress('i'))
	m = next.(Model)
	generation := m.beginRequest(requestInstallPreview)
	next, _ = m.Update(installSourcePreviewLoadedMsg{
		generation: generation,
		preview:    &pipeline.SourcePreview{URL: "/empty/source", Catalogs: map[string]catalog.LoadedCatalog{}},
	})
	m = next.(Model)
	next, _ = m.Update(keyNamed("enter"))
	m = next.(Model)
	if m.installSource.step != installSourceStepPick || !strings.Contains(renderInstallSourceModal(m), "Nothing discoverable under this source") {
		t.Fatalf("empty source advanced or omitted its hint: step=%v view=%s", m.installSource.step, renderInstallSourceModal(m))
	}
}

func TestInstallSourcePickWindowKeepsCursorAndSelectedCountVisible(t *testing.T) {
	m := NewModel(t.TempDir(), t.TempDir())
	m.width, m.height = 80, 24
	next, _ := m.Update(keyPress('i'))
	m = next.(Model)
	entries := make([]catalog.Entry, 60)
	for i := range entries {
		entries[i] = catalog.Entry{Kind: ir.KindSkill, Name: fmt.Sprintf("row-%02d", i)}
	}
	loaded := catalog.LoadedCatalog{Entries: entries}
	preview := &pipeline.SourcePreview{
		URL:      "https://example.invalid/source.git",
		Catalogs: map[string]catalog.LoadedCatalog{"source": loaded},
	}
	generation := m.beginRequest(requestInstallPreview)
	next, _ = m.Update(installSourcePreviewLoadedMsg{generation: generation, preview: preview})
	m = next.(Model)
	m.installSource.cursor = 45
	m.installSource.picked[installSourceChoiceKey(installSourceChoice{kind: ir.KindSkill, name: "row-00"})] = true

	view := ansi.Strip(renderInstallSourceModal(m))
	if !strings.Contains(view, "› [ ] skill") || !strings.Contains(view, "row-45") {
		t.Fatalf("cursor row is not visible in windowed Pick view:\n%s", view)
	}
	if !strings.Contains(view, "1 selected") {
		t.Fatalf("selection count is not visible in windowed Pick view:\n%s", view)
	}
}

func installSourcePickModel(t *testing.T, url string) Model {
	t.Helper()
	m := NewModel(t.TempDir(), t.TempDir())
	next, _ := m.Update(keyPress('i'))
	m = next.(Model)
	m.installSource.values.url = url
	generation := m.beginRequest(requestInstallPreview)
	loaded := catalog.LoadedCatalog{Entries: []catalog.Entry{{Kind: ir.KindSkill, Name: "alpha", Description: "Alpha"}}}
	next, _ = m.Update(installSourcePreviewLoadedMsg{generation: generation, preview: &pipeline.SourcePreview{URL: url, Catalogs: map[string]catalog.LoadedCatalog{"source": loaded}}})
	m = next.(Model)
	next, _ = m.Update(keyPress(' '))
	return next.(Model)
}

func TestInstallSourceArrowKeysWalkSteps(t *testing.T) {
	m := installSourcePickModel(t, "https://example.invalid/source.git")
	next, cmd := m.Update(keyNamed("right"))
	m = next.(Model)
	if cmd != nil || m.installSource.step != installSourceStepReview {
		t.Fatalf("right on Pick = step %v cmd=%t, want Review", m.installSource.step, cmd != nil)
	}
	next, cmd = m.Update(keyNamed("right"))
	m = next.(Model)
	if cmd != nil || m.installSource.step != installSourceStepReview {
		t.Fatalf("right on Review = step %v cmd=%t, want Review without install", m.installSource.step, cmd != nil)
	}
	next, _ = m.Update(keyNamed("left"))
	m = next.(Model)
	if m.installSource.step != installSourceStepPick {
		t.Fatalf("left on Review = step %v, want Pick", m.installSource.step)
	}
	next, _ = m.Update(keyNamed("left"))
	m = next.(Model)
	if m.installSource.step != installSourceStepSource {
		t.Fatalf("left on Pick = step %v, want Source", m.installSource.step)
	}
}

func TestInstallSourceFilterEditingKeepsArrowKeys(t *testing.T) {
	m := installSourcePickModel(t, "https://example.invalid/source.git")
	next, _ := m.Update(keyPress('/'))
	m = next.(Model)
	for _, name := range []string{"left", "right"} {
		next, _ = m.Update(keyNamed(name))
		m = next.(Model)
	}
	if m.installSource.step != installSourceStepPick || !m.installSource.filtering {
		t.Fatalf("arrows while filtering: step=%v filtering=%t, want Pick still filtering", m.installSource.step, m.installSource.filtering)
	}
}

func TestInstallSourceUnchangedSourceReusesPreview(t *testing.T) {
	m := installSourcePickModel(t, "https://example.invalid/source.git")
	preview := m.installSource.preview
	next, _ := m.Update(keyNamed("left"))
	m = next.(Model)
	next, cmd := m.Update(keyNamed("enter"))
	m = next.(Model)
	if cmd != nil || m.installSource.step != installSourceStepPick || m.installSource.preview != preview || !m.installSource.picked["skill\x00alpha"] {
		t.Fatalf("unchanged enter = step %v cmd=%t reused=%t picked=%#v", m.installSource.step, cmd != nil, m.installSource.preview == preview, m.installSource.picked)
	}

	next, _ = m.Update(keyNamed("left"))
	m = next.(Model)
	m.installSource.values.ref = "v2"
	next, cmd = m.Update(keyNamed("enter"))
	m = next.(Model)
	if cmd == nil || !m.installSource.loading {
		t.Fatalf("changed ref enter = cmd=%t loading=%t, want a new preview", cmd != nil, m.installSource.loading)
	}
}

func TestInstallSourceReviewArrowsMoveScope(t *testing.T) {
	m := installSourcePickModel(t, "./source")
	next, _ := m.Update(keyNamed("right"))
	m = next.(Model)
	next, _ = m.Update(keyNamed("up"))
	m = next.(Model)
	if m.installSource.scope != scopeProject || !strings.Contains(m.installSource.error, "repo-relative") {
		t.Fatalf("up onto refused User scope = %v error=%q, want Project and the refusal", m.installSource.scope, m.installSource.error)
	}
	next, _ = m.Update(keyNamed("down"))
	m = next.(Model)
	if m.installSource.scope != scopeLocal || m.installSource.error != "" {
		t.Fatalf("down onto Local = %v error=%q", m.installSource.scope, m.installSource.error)
	}
	next, _ = m.Update(keyNamed("down"))
	m = next.(Model)
	if m.installSource.scope != scopeLocal {
		t.Fatalf("down past Local = %v, want Local without wrapping", m.installSource.scope)
	}

	m = installSourcePickModel(t, "https://example.invalid/source.git")
	next, _ = m.Update(keyNamed("right"))
	m = next.(Model)
	for range 2 {
		next, _ = m.Update(keyNamed("up"))
		m = next.(Model)
	}
	if m.installSource.scope != scopeGlobal || m.installSource.error != "" {
		t.Fatalf("up to User = %v error=%q, want User without wrapping", m.installSource.scope, m.installSource.error)
	}
}

func installSourceTypeSequence(t *testing.T, m Model, steps ...tea.Msg) Model {
	t.Helper()
	for _, step := range steps {
		next, _ := m.Update(step)
		m = next.(Model)
	}
	return m
}

func TestInstallSourceTabMovesTypingAcrossFields(t *testing.T) {
	m := installSourceTypeSequence(t, NewModel(t.TempDir(), t.TempDir()),
		keyPress('i'), keyNamed("tab"), keyPress('r'), keyNamed("tab"), keyPress('p'), keyNamed("tab"), keyPress('u'))
	if v := m.installSource.values; v.ref != "r" || v.path != "p" || v.url != "u" {
		t.Fatalf("typing after Tab landed url=%q ref=%q path=%q, want u r p", v.url, v.ref, v.path)
	}
}

func TestInstallSourceShiftTabMovesTypingBack(t *testing.T) {
	m := installSourceTypeSequence(t, NewModel(t.TempDir(), t.TempDir()),
		keyPress('i'), keyNamed("shift+tab"), keyPress('p'), keyNamed("shift+tab"), keyPress('r'), keyNamed("shift+tab"), keyPress('u'))
	if v := m.installSource.values; v.ref != "r" || v.path != "p" || v.url != "u" {
		t.Fatalf("typing after Shift+Tab landed url=%q ref=%q path=%q, want u r p", v.url, v.ref, v.path)
	}
}

func TestInstallSourceArrowsMoveTypingAcrossFields(t *testing.T) {
	m := installSourceTypeSequence(t, NewModel(t.TempDir(), t.TempDir()),
		keyPress('i'), keyNamed("up"), keyPress('u'), keyNamed("down"), keyNamed("down"), keyPress('p'),
		keyNamed("down"), keyPress('q'), keyNamed("up"), keyPress('r'))
	if v := m.installSource.values; v.url != "u" || v.ref != "r" || v.path != "pq" {
		t.Fatalf("typing after arrows landed url=%q ref=%q path=%q, want u r pq", v.url, v.ref, v.path)
	}
	if m.installSource.form == nil || m.installSource.form.State != huh.StateNormal || m.installSource.step != installSourceStepSource {
		t.Fatalf("arrows left the Source step: step=%v form=%#v", m.installSource.step, m.installSource.form)
	}
}

func TestInstallSourceFieldsStayUsableAfterTabOnPath(t *testing.T) {
	m := installSourceTypeSequence(t, NewModel(t.TempDir(), t.TempDir()),
		keyPress('i'), keyNamed("tab"), keyNamed("tab"), keyPress('p'), keyNamed("tab"), keyPress('z'))
	if m.installSource.form == nil || m.installSource.form.State != huh.StateNormal {
		t.Fatalf("form after Tab past Path = %#v, want an active form", m.installSource.form)
	}
	if v := m.installSource.values; v.path != "p" || v.url != "z" {
		t.Fatalf("typing after Tab past Path landed url=%q path=%q, want z and p", v.url, v.path)
	}
	view := ansi.Strip(renderInstallSourceModal(m))
	for _, label := range []string{"URL", "Ref", "Path"} {
		if !strings.Contains(view, label) {
			t.Fatalf("Source form no longer renders the %s field:\n%s", label, view)
		}
	}
}

func writeInstallSourceTestFile(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
