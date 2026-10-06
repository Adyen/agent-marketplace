package tui

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
)

// spec: tui-source-toggle/unverified-source-inventory-cannot-mutate
func TestSourceMenuBlocksUnverifiedInventory(t *testing.T) {
	for _, onSection := range []bool{false, true} {
		target := mkRow("alpha", withProject, withRegistry("https://example.test/tools.git", "tools"))
		target.PendingVerification = true
		m := sourceToggleTestModel([]row{target})
		if onSection {
			m.cursor = 0
		} else {
			cursorOnRow(t, &m, "alpha")
		}
		updated, cmd := m.Update(keyPress('x'))
		m = updated.(Model)
		if m.pane != paneList || m.operations.active != nil || cmd == nil {
			t.Fatalf("unverified source reached mutation UI: pane=%v cmd=%v", m.pane, cmd)
		}
		if msg := cmd().(toastMsg); msg.text != "selection is still being verified" {
			t.Fatalf("refusal = %q", msg.text)
		}
		target.PendingVerification = false
		m = sourceToggleTestModel([]row{target})
		cursorOnRow(t, &m, "alpha")
		updated, cmd = m.Update(keyPress('x'))
		if updated.(Model).pane != paneGroupToggleModal || cmd != nil {
			t.Fatal("verified control could not open source menu")
		}
	}
}

// spec: tui-source-toggle/unverified-source-inventory-cannot-mutate
func TestSourceMenuRechecksVerificationOnSubmission(t *testing.T) {
	target := mkRow("alpha", withProject, withRegistry("https://example.test/tools.git", "tools"))
	m := sourceToggleTestModel([]row{target})
	m.pane, m.modalSource = paneGroupToggleModal, target
	m.rows[0].PendingVerification = true
	for _, key := range []rune{'g', 'p', 'l', 'e'} {
		updated, cmd := m.Update(keyPress(key))
		after := updated.(Model)
		if after.operations.active != nil || after.pane != paneGroupToggleModal || cmd == nil {
			t.Fatalf("pending source accepted %q: pane=%v active=%v", key, after.pane, after.operations.active)
		}
		if msg := cmd().(toastMsg); msg.text != "selection is still being verified" {
			t.Fatalf("refusal = %q", msg.text)
		}
	}
}

// spec: tui-source-toggle/resource-or-single-source-section-opens-the-menu
func TestSourceTogglePaletteUsesSharedGuards(t *testing.T) {
	target := mkRow("alpha", withProject, withRegistry("https://example.test/tools.git", "tools"))
	m := sourceToggleTestModel([]row{target})
	cursorOnRow(t, &m, "alpha")
	if !m.actionEligible(actionSourceToggle) {
		t.Fatal("verified source missing from palette")
	}
	m = typePaletteQuery(t, openPaletteFor(t, m), "whole source")
	updated, cmd := m.Update(keyNamed("enter"))
	if updated.(Model).pane != paneGroupToggleModal || cmd != nil {
		t.Fatal("palette could not open source menu")
	}
	m = sourceToggleTestModel([]row{target})
	cursorOnRow(t, &m, "alpha")
	m.rows[0].PendingVerification = true
	m.filteredRows = m.rows
	m.rebuildFlatItems()
	if m.actionEligible(actionSourceToggle) {
		t.Fatal("palette offered mutation of unverified source")
	}
}

// spec: tui-source-toggle/nested-workspace-uses-its-local-root
func TestWholeSourceMutationsUseNestedLocalRoot(t *testing.T) {
	repo := t.TempDir()
	local := filepath.Join(repo, "nested")
	if err := os.MkdirAll(local, 0o755); err != nil {
		t.Fatal(err)
	}
	ws, err := pipeline.NewWorkspace(repo, local)
	if err != nil {
		t.Fatal(err)
	}
	entry := config.RegistryEntry{URL: "https://example.test/tools.git", Ref: "main"}
	group := groupToggle{entry: entry, label: "tools", scope: scopeLocal, enable: true, resources: 2}
	msg := runDependencyGroupToggle(context.Background(), ws, group, nil)().(configWrittenMsg)
	if msg.err != nil {
		t.Fatal(msg.err)
	}
	path := config.LocalConfigPath(local)
	data := string(mustReadFile(t, path))
	if !strings.Contains(data, "all: true") || config.PeekAPIVersion([]byte(data)) != config.APIVersionV4 {
		t.Fatalf("nested local root not selected as v4:\n%s", data)
	}
	toggle := sourceToggle{entry: entry, label: "tools", exclude: true, resources: 2, edits: []config.SourceExclusionEdit{config.SourceExcludeAll}}
	msg = runDependencySourceToggle(context.Background(), ws, toggle, nil)().(configWrittenMsg)
	if msg.err != nil {
		t.Fatal(msg.err)
	}
	data = string(mustReadFile(t, path))
	if !strings.Contains(data, "'*'") && !strings.Contains(data, `"*"`) {
		t.Fatalf("nested exclusion missing:\n%s", data)
	}
	if _, err := os.Stat(config.LocalConfigPath(repo)); !os.IsNotExist(err) {
		t.Fatalf("mutation touched the repository local root: %v", err)
	}
}

func TestRequiredSectionIsNotLabeledExcluded(t *testing.T) {
	m := sourceToggleTestModel([]row{mkRow("alpha", withRequired)})
	if m.flatItems[0].SectionExcluded {
		t.Fatal("required section was labeled excluded")
	}
}

// spec: tui-source-toggle/source-layouts-retain-separate-menu-identities
func TestWholeSourceMenuKeepsSourceHarnessIdentity(t *testing.T) {
	target := mkRow("alpha", withProject, withRegistry("https://example.test/tools.git", "tools"))
	target.Artifact.SourceHarness = "factory"
	sibling := mkRow("beta", withLocal, withRegistry("https://example.test/tools.git", "tools"))
	sibling.Artifact.SourceHarness = "claude"
	m := sourceToggleTestModel([]row{target, sibling})
	if counts := m.sourceGroupCounts(target); counts.resources != 1 || counts.project != 1 || counts.local != 0 {
		t.Fatalf("menu mixed source layouts: %+v", counts)
	}
	group, refusal := m.groupToggleFor(target, scopeProject)
	if refusal != "" || group.resources != 1 || group.enable || group.entry.SourceHarness != "factory" {
		t.Fatalf("scope action mixed source layouts: %+v refusal=%q", group, refusal)
	}
	exclusion, refusal := m.sourceToggleFor(target)
	if refusal != "" || exclusion.resources != 1 || exclusion.entry.SourceHarness != "factory" {
		t.Fatalf("exclusion mixed source layouts: %+v refusal=%q", exclusion, refusal)
	}
	m.cursor = 0
	if _, refusal, ok := m.sourceTargetAtCursor(); ok || refusal == "" {
		t.Fatal("section with two source layouts opened a single-source menu")
	}
}

// spec: tui-source-toggle/source-layouts-retain-separate-menu-identities
func TestLocalSourceExclusionStaysWithItsLayout(t *testing.T) {
	url := "https://example.test/tools.git"
	cfg := &config.Config{Registries: []config.RegistryEntry{
		{URL: url, Ref: "main", SourceHarness: "factory", Source: config.RepoPersonal, ExcludeAll: true},
	}}
	entries := localExcludeAllEntries(cfg)
	factory := mkRow("alpha", withProject, withRegistry(url, "tools"))
	factory.Artifact.RegistryURL, factory.Artifact.Ref, factory.Artifact.SourceHarness = url, "main", "factory"
	claude := mkRow("beta", withProject, withRegistry(url, "tools"))
	claude.Artifact.RegistryURL, claude.Artifact.Ref, claude.Artifact.SourceHarness = url, "main", "claude"
	if !sourceExcludedBy(factory.Artifact, entries) || sourceExcludedBy(claude.Artifact, entries) {
		t.Fatal("local whole-source exclusion was attributed to the wrong source layout")
	}

	factory.SourceExcludedLocally = true
	m := sourceToggleTestModel([]row{factory, claude})
	toggle, refusal := m.sourceToggleFor(claude)
	if refusal != "" || !toggle.exclude || toggle.entry.SourceHarness != "claude" || !slices.Equal(toggle.edits, []config.SourceExclusionEdit{config.SourceExcludeAll}) {
		t.Fatalf("claude layout toggle = %+v refusal=%q, want a claude exclusion", toggle, refusal)
	}

	cfg.Registries[0].SourceHarness = ""
	if entries := localExcludeAllEntries(cfg); !sourceExcludedBy(factory.Artifact, entries) || !sourceExcludedBy(claude.Artifact, entries) {
		t.Fatal("exclusion without source_harness did not cover every layout")
	}
}
