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
	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
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
	m.installSource.generation = generation
	next, _ = m.Update(installSourcePreviewLoadedMsg{
		generation: generation,
		preview: &pipeline.SourcePreview{
			URL: "https://example.invalid/source.git", Catalog: loaded,
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
		Catalog:  catalog.LoadedCatalog{Entries: []catalog.Entry{{Kind: ir.KindSkill, Name: "alpha"}}},
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
	m.installSource.generation = generation
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
	if m.installSource.preview == nil || m.installSource.preview.Path != "alpha" || len(m.installSource.preview.BundlePaths) != 0 || len(m.installSource.preview.Catalog.Entries) != 1 || m.installSource.preview.Catalog.Entries[0].Name != "alpha" {
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
	m.installSource.generation = generation
	next, _ = m.Update(installSourcePreviewLoadedMsg{
		generation: generation,
		preview:    &pipeline.SourcePreview{URL: "./source", Catalog: loaded, Catalogs: map[string]catalog.LoadedCatalog{"source": loaded}},
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
	m.installSource.generation = generation
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
		URL: "https://example.invalid/source.git", Catalog: loaded,
		Catalogs: map[string]catalog.LoadedCatalog{"source": loaded},
	}
	generation := m.beginRequest(requestInstallPreview)
	m.installSource.generation = generation
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
