package tui

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	inspectiontest "github.com/Adyen/agent-marketplace/internal/testutil/marketplace/inspection"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/inspection"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

type inspectionFixture = inspectiontest.Fixture

func newInspectionFixture(t *testing.T, mutate inspectiontest.Mutate) *inspectionFixture {
	return inspectiontest.NewFixture(t, mutate)
}

func newInspectionFixtureContent(t *testing.T, primary []byte, support map[string][]byte, mutate inspectiontest.Mutate) *inspectionFixture {
	return inspectiontest.NewFixtureContent(t, primary, support, mutate)
}

func inspectionFixtureRow(f *inspectionFixture) row {
	endpoint := f.Endpoint()
	item := f.Item()
	return row{
		Artifact: resolver.ResolvedArtifact{
			Entry:                 catalog.Entry{Kind: ir.KindSkill, Name: item.Name, Description: item.Description},
			IsMarketplace:         true,
			MarketplaceURL:        endpoint.URL,
			MarketplaceInstanceID: endpoint.InstanceID,
			MarketplaceRegistryID: item.RegistryID,
		},
		IsAvailable:          true,
		MarketplaceDiscovery: &marketplaceDiscovery{Endpoint: endpoint, Item: item},
	}
}

func inspectionModel(t *testing.T, r row) Model {
	t.Helper()
	m := modelWithSelectedRow(t, r)
	m.width = 100
	m.height = 30
	return m
}

func inspectionModelDirs(t *testing.T, r row, repoRoot, homeDir string, opts ...Option) Model {
	t.Helper()
	m := NewModel(repoRoot, homeDir, opts...)
	m.loading = false
	m.rows = []row{r}
	m.filteredRows = m.rows
	m.rebuildFlatItems()
	for i := range m.flatItems {
		if m.flatItems[i].Kind == flatArtifact {
			m.cursor = i
			break
		}
	}
	m.width = 100
	m.height = 30
	return m
}

// spec: session-navigation/inspect-an-exact-skill-before-installation-without-writes
func TestMarketplaceSkillInspectionLoadsExactRevision(t *testing.T) {
	f := newInspectionFixture(t, nil)
	repoRoot := t.TempDir()
	homeDir := t.TempDir()
	configBytes := []byte("apiVersion: v4\n")
	lockBytes := []byte("apiVersion: v4\nresources: []\n")
	controlled := map[string][]byte{
		filepath.Join(repoRoot, ".agents", "config.yaml"): configBytes,
		filepath.Join(repoRoot, ".agents", "lock.yaml"):   lockBytes,
		filepath.Join(homeDir, ".agents", "config.yaml"):  append([]byte(nil), configBytes...),
		filepath.Join(homeDir, ".agents", "lock.yaml"):    append([]byte(nil), lockBytes...),
	}
	for path, data := range controlled {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var mutations []string
	lifecycle := DependencyLifecycle{
		Sync: func(context.Context, pipeline.Workspace) ([]error, error) {
			mutations = append(mutations, "sync")
			return nil, errors.New("sync must not run")
		},
		Update: func(context.Context, pipeline.Workspace) ([]error, error) {
			mutations = append(mutations, "update")
			return nil, errors.New("update must not run")
		},
		Mutate: func(context.Context, pipeline.Workspace, func(pipeline.ConfigPathMapper) error) error {
			mutations = append(mutations, "mutate")
			return errors.New("mutate must not run")
		},
	}
	m := inspectionModelDirs(t, inspectionFixtureRow(f), repoRoot, homeDir, WithDependencyLifecycle(lifecycle))

	m, cmd := enterDetail(t, m)
	if cmd == nil {
		t.Fatal("enter on a discovery-only skill did not schedule inspection")
	}
	if m.bundle != nil {
		t.Fatal("inspection produced a bundle before the response arrived")
	}
	if out := ansi.Strip(m.detailViewport.View()); !contains(out, "Loading") {
		t.Fatalf("pending inspection did not show a loading state:\n%s", out)
	}
	if got := f.Log(); len(got) != 0 {
		t.Fatalf("inspection performed HTTP before the command ran: %v", got)
	}

	msg, ok := cmd().(marketplaceDetailMsg)
	if !ok {
		t.Fatalf("inspection command returned %T, want marketplaceDetailMsg", msg)
	}
	updated, _ := m.Update(msg)
	m = updated.(Model)
	if m.bundle == nil {
		t.Fatalf("accepted inspection left no bundle; viewport:\n%s", ansi.Strip(m.detailViewport.View()))
	}
	if m.detailSelectedPath != inspection.PrimarySkillPath {
		t.Fatalf("initial inspection selection = %q, want SKILL.md", m.detailSelectedPath)
	}
	var names []string
	for _, entry := range m.bundle.entries() {
		names = append(names, entry.RelPath)
	}
	for _, want := range []string{inspection.PrimarySkillPath, "support/notes.md", "support/logo.png"} {
		found := false
		for _, got := range names {
			if got == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("inspection tree missing %q in %v", want, names)
		}
	}
	out := ansi.Strip(m.detailViewport.View())
	if !contains(out, "Primary inspection.") {
		t.Fatalf("inspection primary preview missing body:\n%s", out)
	}
	for _, path := range f.Log() {
		if strings.Contains(path, "/inspection/content/") {
			t.Fatalf("support file %q was fetched eagerly; requests: %v", path, f.Log())
		}
	}

	next, cmd := m.moveBundleCursor(-1)
	m = next.(Model)
	if m.detailSelectedPath != "support/notes.md" {
		t.Fatalf("move selected %q, want support/notes.md", m.detailSelectedPath)
	}
	if cmd == nil {
		t.Fatal("selecting a support file did not schedule a preview")
	}
	updated, _ = m.Update(cmd().(detailContentMsg))
	m = updated.(Model)
	out = ansi.Strip(m.detailViewport.View())
	if !contains(out, "Supporting inspection.") {
		t.Fatalf("support file preview missing body:\n%s", out)
	}
	next, cmd = m.moveBundleCursor(-1)
	m = next.(Model)
	if m.detailSelectedPath != "support/logo.png" {
		t.Fatalf("move selected %q, want support/logo.png", m.detailSelectedPath)
	}
	updated, _ = m.Update(cmd().(detailContentMsg))
	m = updated.(Model)
	out = ansi.Strip(m.detailViewport.View())
	if contains(out, "PNG\r\n") || !contains(out, "logo.png") {
		t.Fatalf("image file preview leaked bytes or lost identity:\n%s", out)
	}
	var fetched []string
	for _, path := range f.Log() {
		if strings.Contains(path, "/inspection/content/") {
			fetched = append(fetched, path)
		}
	}
	notesDigest := ""
	for _, file := range f.Manifest.Files {
		if file.Path == "support/notes.md" {
			notesDigest = file.ContentDigest
		}
	}
	if len(fetched) != 1 || !strings.HasSuffix(fetched[0], notesDigest) {
		t.Fatalf("lazy file requests = %v, want exactly the notes.md digest %q", fetched, notesDigest)
	}
	for _, path := range f.Log() {
		if strings.Contains(path, "/artifacts") || strings.Contains(path, "/install") || strings.HasSuffix(path, "/sync") {
			t.Fatalf("inspection triggered a mutating request %q", path)
		}
	}
	if len(mutations) != 0 {
		t.Fatalf("inspection invoked mutating callbacks: %v", mutations)
	}
	for path, want := range controlled {
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("inspection mutated %s (err=%v)", path, err)
		}
	}
}

// spec: session-navigation/inspect-an-exact-skill-before-installation-without-writes
func TestMarketplaceSkillInspectionFailsVisibly(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(f *inspectionFixture, wk *mpapi.WellKnown, rev *store.AssessedResourceRevision, manifestPayload *[]byte, bodies map[string][]byte)
		row    func(f *inspectionFixture) row
	}{
		{name: "endpoint identity mismatch", mutate: func(_ *inspectionFixture, wk *mpapi.WellKnown, _ *store.AssessedResourceRevision, _ *[]byte, _ map[string][]byte) {
			wk.InstanceID = "other-instance"
		}},
		{name: "revision provenance mismatch", mutate: func(_ *inspectionFixture, _ *mpapi.WellKnown, rev *store.AssessedResourceRevision, _ *[]byte, _ map[string][]byte) {
			rev.ContentDigest = store.RevisionDigest("sha256:" + strings.Repeat("e", 64))
		}},
		{name: "no inspection support", mutate: func(_ *inspectionFixture, _ *mpapi.WellKnown, rev *store.AssessedResourceRevision, _ *[]byte, _ map[string][]byte) {
			rev.Assessment.InspectionManifestDigest = ""
		}},
		{name: "revision endpoint returns 404", row: func(f *inspectionFixture) row {
			r := inspectionFixtureRow(f)
			r.MarketplaceDiscovery.Item.Revision = "sha256:" + strings.Repeat("d", 64)
			return r
		}},
		{name: "inspection transport failure", row: func(f *inspectionFixture) row {
			f.Server.Close()
			return inspectionFixtureRow(f)
		}},
		{name: "discovery content digest mismatch", row: func(f *inspectionFixture) row {
			r := inspectionFixtureRow(f)
			r.MarketplaceDiscovery.Item.ContentDigest = "sha256:" + strings.Repeat("f", 64)
			return r
		}},
		{name: "primary body hash mismatch", mutate: func(f *inspectionFixture, _ *mpapi.WellKnown, _ *store.AssessedResourceRevision, _ *[]byte, _ map[string][]byte) {
			f.Primary = []byte("corrupted primary body")
		}},
		{name: "manifest digest mismatch", mutate: func(_ *inspectionFixture, _ *mpapi.WellKnown, _ *store.AssessedResourceRevision, manifestPayload *[]byte, _ map[string][]byte) {
			*manifestPayload = append(append([]byte(nil), *manifestPayload...), ' ')
		}},
		{name: "incomplete discovery coordinates", row: func(f *inspectionFixture) row {
			r := inspectionFixtureRow(f)
			r.MarketplaceDiscovery.Item.Revision = ""
			return r
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newInspectionFixture(t, tc.mutate)
			r := inspectionFixtureRow(f)
			if tc.row != nil {
				r = tc.row(f)
			}
			m := inspectionModel(t, r)
			m, cmd := enterDetail(t, m)
			if cmd == nil {
				t.Fatal("enter did not schedule inspection")
			}
			msg, ok := cmd().(marketplaceDetailMsg)
			if !ok {
				t.Fatalf("inspection command returned %T", msg)
			}
			if msg.err == nil {
				t.Fatal("inspection succeeded for an invalid fixture")
			}
			updated, _ := m.Update(msg)
			m = updated.(Model)
			if m.bundle != nil || m.detailSelectedPath != "" {
				t.Fatal("failed inspection left a bundle or selection behind")
			}
			out := ansi.Strip(m.detailViewport.View())
			if !contains(out, "unavailable") {
				t.Fatalf("failed inspection is not actionable:\n%s", out)
			}
		})
	}

	t.Run("primary success control", func(t *testing.T) {
		f := newInspectionFixture(t, nil)
		m := inspectionModel(t, inspectionFixtureRow(f))
		_, cmd := enterDetail(t, m)
		msg, ok := cmd().(marketplaceDetailMsg)
		if !ok || msg.err != nil {
			t.Fatalf("control inspection failed: %v", msg.err)
		}
	})
}

// spec: session-navigation/inspect-an-exact-skill-before-installation-without-writes
func TestMarketplaceSkillInspectionTruncatesOversizeBodies(t *testing.T) {
	oversize := func(sentinel string) []byte {
		data := make([]byte, maxDetailSourceBytes+64)
		for i := range data {
			data[i] = 'x'
		}
		copy(data, "# Big\n\n")
		copy(data[len(data)-len(sentinel):], sentinel)
		return data
	}

	t.Run("primary", func(t *testing.T) {
		f := newInspectionFixtureContent(t, oversize("TAIL-SENTINEL-P"), map[string][]byte{"notes.md": []byte("# Notes\n\nSupporting inspection.")}, nil)
		m := inspectionModel(t, inspectionFixtureRow(f))
		_, cmd := enterDetail(t, m)
		msg := cmd().(marketplaceDetailMsg)
		if msg.err != nil {
			t.Fatalf("oversize primary inspection failed: %v", msg.err)
		}
		out := ansi.Strip(msg.content)
		if !contains(out, "truncated: source exceeds") {
			t.Fatalf("oversize primary lost the truncation marker:\n%.200s", out)
		}
		if contains(out, "TAIL-SENTINEL-P") {
			t.Fatal("oversize primary leaked bytes past the cap")
		}
	})

	t.Run("lazy support file", func(t *testing.T) {
		f := newInspectionFixtureContent(t,
			[]byte("# Grill with docs\n\nPrimary inspection."),
			map[string][]byte{"notes.md": oversize("TAIL-SENTINEL-S")}, nil)
		m := inspectionModel(t, inspectionFixtureRow(f))
		m, cmd := enterDetail(t, m)
		updated, _ := m.Update(cmd().(marketplaceDetailMsg))
		m = updated.(Model)
		next, pending := m.moveBundleCursor(-1)
		m = next.(Model)
		if m.detailSelectedPath != "support/notes.md" {
			t.Fatalf("support selection = %q", m.detailSelectedPath)
		}
		preview := pending().(detailContentMsg)
		out := ansi.Strip(preview.content)
		if !contains(out, "truncated: source exceeds") {
			t.Fatalf("oversize support file lost the truncation marker:\n%.200s", out)
		}
		if contains(out, "TAIL-SENTINEL-S") {
			t.Fatal("oversize support file leaked bytes past the cap")
		}
	})

	t.Run("small support control", func(t *testing.T) {
		f := newInspectionFixture(t, nil)
		m := inspectionModel(t, inspectionFixtureRow(f))
		m, cmd := enterDetail(t, m)
		updated, _ := m.Update(cmd().(marketplaceDetailMsg))
		m = updated.(Model)
		next, pending := m.moveBundleCursor(-1)
		m = next.(Model)
		updated, _ = m.Update(pending().(detailContentMsg))
		out := ansi.Strip(updated.(Model).detailViewport.View())
		if !contains(out, "Supporting inspection.") || contains(out, "truncated") {
			t.Fatalf("small support file control failed:\n%s", out)
		}
	})
}

// spec: session-navigation/inspect-an-exact-skill-before-installation-without-writes
func TestMarketplaceSkillInspectionRejectsCorruptSupportFile(t *testing.T) {
	f := newInspectionFixture(t, func(f *inspectionFixture, _ *mpapi.WellKnown, _ *store.AssessedResourceRevision, _ *[]byte, bodies map[string][]byte) {
		bodies[inspection.Digest(f.Support["notes.md"])] = []byte("corrupted notes body")
	})
	m := inspectionModel(t, inspectionFixtureRow(f))
	m, cmd := enterDetail(t, m)
	msg := cmd().(marketplaceDetailMsg)
	if msg.err != nil {
		t.Fatalf("initial inspection failed: %v", msg.err)
	}
	updated, _ := m.Update(msg)
	m = updated.(Model)
	next, pending := m.moveBundleCursor(-1)
	m = next.(Model)
	updated, _ = m.Update(pending().(detailContentMsg))
	out := ansi.Strip(updated.(Model).detailViewport.View())
	if contains(out, "corrupted notes body") || !contains(out, "Error reading file") {
		t.Fatalf("corrupt support file did not fail visibly:\n%s", out)
	}
}

// spec: session-navigation/inspect-an-exact-skill-before-installation-without-writes
func TestMarketplaceSkillInspectionStaleResponses(t *testing.T) {
	t.Run("result after escape is ignored", func(t *testing.T) {
		f := newInspectionFixture(t, nil)
		m := inspectionModel(t, inspectionFixtureRow(f))
		m, cmd := enterDetail(t, m)
		msg := cmd().(marketplaceDetailMsg)
		updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		m = updated.(Model)
		updated, _ = m.Update(msg)
		m = updated.(Model)
		if m.pane != paneList || m.bundle != nil {
			t.Fatalf("stale inspection was applied after escape: pane=%v bundle=%v", m.pane, m.bundle != nil)
		}
	})

	t.Run("result for another resource is ignored", func(t *testing.T) {
		f := newInspectionFixture(t, nil)
		m := inspectionModel(t, inspectionFixtureRow(f))
		m, cmd := enterDetail(t, m)
		msg := cmd().(marketplaceDetailMsg)
		updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		m = updated.(Model)
		other := inspectionFixtureRow(f)
		other.Artifact.Name = "other"
		other.MarketplaceDiscovery.Item.Name = "other"
		m.rows = []row{other}
		m.filteredRows = m.rows
		m.rebuildFlatItems()
		m.cursor = 0
		m, _ = enterDetail(t, m)
		updated, _ = m.Update(msg)
		m = updated.(Model)
		if m.bundle != nil {
			t.Fatal("stale inspection replaced another resource's detail")
		}
	})

	t.Run("resize while initial load pending reflows at current width", func(t *testing.T) {
		f := newInspectionFixture(t, nil)
		m := inspectionModel(t, inspectionFixtureRow(f))
		m, cmd := enterDetail(t, m)
		updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
		m = updated.(Model)
		msg := cmd().(marketplaceDetailMsg)
		updated, reflow := m.Update(msg)
		m = updated.(Model)
		if m.bundle == nil {
			t.Fatal("accepted inspection lost its tree on stale width")
		}
		if reflow == nil {
			t.Fatal("stale-width acceptance did not schedule a reflow")
		}
		contentMsg, ok := reflow().(detailContentMsg)
		if !ok {
			t.Fatalf("reflow did not produce a detailContentMsg: %v", msg)
		}
		if contentMsg.width != previewWidthFor(120) {
			t.Fatalf("reflow width = %d, want %d", contentMsg.width, previewWidthFor(120))
		}
		updated, _ = m.Update(contentMsg)
		m = updated.(Model)
		if !contains(ansi.Strip(m.detailViewport.View()), "Primary inspection.") {
			t.Fatalf("reflowed primary missing:\n%s", ansi.Strip(m.detailViewport.View()))
		}
	})

	t.Run("support response after escape and reopen is ignored", func(t *testing.T) {
		f := newInspectionFixture(t, nil)
		m := inspectionModel(t, inspectionFixtureRow(f))
		m, cmd := enterDetail(t, m)
		updated, _ := m.Update(cmd().(marketplaceDetailMsg))
		m = updated.(Model)
		next, pending := m.moveBundleCursor(-1)
		m = next.(Model)
		if m.detailSelectedPath != "support/notes.md" || pending == nil {
			t.Fatalf("support selection = %q cmd=%v", m.detailSelectedPath, pending)
		}
		updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		m = updated.(Model)
		other := inspectionFixtureRow(f)
		other.Artifact.Name = "other"
		other.MarketplaceDiscovery.Item.Name = "other"
		m.rows = []row{other}
		m.filteredRows = m.rows
		m.rebuildFlatItems()
		m.cursor = 0
		m, _ = enterDetail(t, m)
		stale := pending().(detailContentMsg)
		updated, _ = m.Update(stale)
		m = updated.(Model)
		if contains(ansi.Strip(m.detailViewport.View()), "Supporting inspection.") {
			t.Fatalf("stale support fetch overwrote another resource's detail:\n%s", ansi.Strip(m.detailViewport.View()))
		}
	})

	t.Run("resize while support fetch pending rejects the stale result", func(t *testing.T) {
		f := newInspectionFixture(t, nil)
		m := inspectionModel(t, inspectionFixtureRow(f))
		m, cmd := enterDetail(t, m)
		updated, _ := m.Update(cmd().(marketplaceDetailMsg))
		m = updated.(Model)
		next, pending := m.moveBundleCursor(-1)
		m = next.(Model)
		updated, reflow := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
		m = updated.(Model)
		if reflow == nil {
			t.Fatal("resize did not schedule a reflow for the pending support file")
		}
		before := m.detailViewport.View()
		stale := pending().(detailContentMsg)
		if stale.width == previewWidthFor(120) {
			t.Fatalf("control is vacuous: pending fetch captured current width %d", stale.width)
		}
		updated, _ = m.Update(stale)
		m = updated.(Model)
		if m.detailViewport.View() != before {
			t.Fatal("stale-width support fetch overwrote the resized detail")
		}
	})

	t.Run("same row snapshot preserves the accepted tree", func(t *testing.T) {
		f := newInspectionFixture(t, nil)
		m := inspectionModel(t, inspectionFixtureRow(f))
		m, cmd := enterDetail(t, m)
		updated, _ := m.Update(cmd().(marketplaceDetailMsg))
		m = updated.(Model)
		bundle := m.bundle
		before := ansi.Strip(m.detailViewport.View())
		m.applySnapshotData(snapshotLoadedMsg{rows: m.rows, driftAssessment: driftReady})
		if m.bundle != bundle || m.detailSelectedPath != inspection.PrimarySkillPath {
			t.Fatal("same-row snapshot discarded the accepted inspection tree")
		}
		if ansi.Strip(m.detailViewport.View()) != before || !contains(before, "Primary inspection.") {
			t.Fatalf("same-row snapshot lost the accepted preview content:\n%s", ansi.Strip(m.detailViewport.View()))
		}
	})
}
