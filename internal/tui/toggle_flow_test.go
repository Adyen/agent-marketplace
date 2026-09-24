package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/skill"
)

const v2Header = `apiVersion: agent-marketplace/v2
harnesses:
  - factory
`

const repoV2Header = `apiVersion: agent-marketplace/v2
`

// Test that after a simulated snapshotLoadedMsg, both the row data
// AND the rendered output reflect the updated badge and glyph.
func TestToggleLocalUpdatesBadgeAndGlyph(t *testing.T) {
	r := mkRow("audit", withAvailable, withRegistry("sec.git", "security"))

	m := NewModel("/repo", "/home")
	m.loading = false
	m.width = 80
	m.height = 24
	m.rows = []row{r}
	m.filteredRows = m.rows
	m.rebuildFlatItems()

	beforeOut := renderList(m, 24)
	if !contains(beforeOut, "[ ]") {
		t.Fatalf("before toggle: expected [ ] glyph, got:\n%s", beforeOut)
	}

	// Simulate the snapshot after local toggle: skill is now locally enabled.
	updatedRow := row{
		Artifact:     resolver.ResolvedArtifact{Entry: catalog.Entry{Name: "audit", Path: "/cache/audit"}, RegistryURL: "sec.git"},
		EnabledLocal: true,
		RegistryURL:  "sec.git",
		RegistryName: "security",
	}

	snapMsg := snapshotLoadedMsg{
		generation: 1,
		rows:       []row{updatedRow},
		mergedCfg:  &config.Config{},
		resolved:   []resolver.ResolvedArtifact{},
	}

	mi, _ := m.Update(snapMsg)
	m = mi.(Model)

	updated := m.rows[0]
	if updated.IsAvailable {
		t.Fatalf("expected IsAvailable=false after toggle")
	}
	if !updated.EnabledLocal {
		t.Fatalf("expected EnabledLocal=true after toggle")
	}
	if updated.Glyph() != "[x]" {
		t.Fatalf("expected glyph [x], got %s", updated.Glyph())
	}
	if originBadge(updated) != "L" {
		t.Fatalf("expected badge L, got %q", originBadge(updated))
	}

	afterOut := renderList(m, 24)
	if !contains(afterOut, "[x]") {
		t.Fatalf("after toggle: expected [x] glyph, got:\n%s", afterOut)
	}
	if !contains(afterOut, "L") {
		t.Fatalf("after toggle: expected L badge, got:\n%s", afterOut)
	}
}

// Test that toggling a skill from project-enabled to project-disabled
// updates the badge correctly (P -> blank, glyph [x] -> [ ]).
func TestToggleProjectOffUpdatesBadgeAndGlyph(t *testing.T) {
	r := mkRow("audit", withProject, withRegistry("sec.git", "security"))

	m := NewModel("/repo", "/home")
	m.loading = false
	m.width = 80
	m.height = 24
	m.rows = []row{r}
	m.filteredRows = m.rows
	m.rebuildFlatItems()

	beforeOut := renderList(m, 24)
	if !contains(beforeOut, "P") {
		t.Fatalf("before toggle off: expected P badge, got:\n%s", beforeOut)
	}

	updatedRow := mkRow("audit", withAvailable, withRegistry("sec.git", "security"))
	snapMsg := snapshotLoadedMsg{
		generation: 1,
		rows:       []row{updatedRow},
		mergedCfg:  &config.Config{},
		resolved:   []resolver.ResolvedArtifact{},
	}

	mi, _ := m.Update(snapMsg)
	m = mi.(Model)

	updated := m.rows[0]
	if updated.Glyph() != "[ ]" {
		t.Fatalf("expected glyph [ ] after toggle off, got %s", updated.Glyph())
	}
	if originBadge(updated) != "" {
		t.Fatalf("expected empty badge after toggle off, got %q", originBadge(updated))
	}
}

// TestReadSnapshotReflectsLocalToggle exercises the real file I/O path:
// write a config, toggle a skill locally, reload via readSnapshot, and
// verify the rows reflect the updated local enablement.
func TestReadSnapshotReflectsLocalToggle(t *testing.T) {
	repoRoot := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	// Set up project config with one registry that has a skill.
	agentsDir := filepath.Join(repoRoot, ".agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatalf("mkdir agents: %v", err)
	}

	projectConfig := repoV2Header + `registries:
  - url: ./local-skills
    path: skills
skills:
  - audit
`
	if err := os.WriteFile(filepath.Join(agentsDir, "config.yaml"), []byte(projectConfig), 0o644); err != nil {
		t.Fatalf("write project config: %v", err)
	}

	// Create the local registry with one skill.
	skillDir := filepath.Join(repoRoot, "local-skills", "skills", "audit")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# Audit\n"), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}

	// Also create a second skill that's NOT enabled.
	extraDir := filepath.Join(repoRoot, "local-skills", "skills", "deploy")
	if err := os.MkdirAll(extraDir, 0o755); err != nil {
		t.Fatalf("mkdir extra skill dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(extraDir, "SKILL.md"), []byte("# Deploy\n"), 0o644); err != nil {
		t.Fatalf("write extra SKILL.md: %v", err)
	}

	// Create an empty global config.
	homeAgentsDir := filepath.Join(homeDir, ".agents")
	if err := os.MkdirAll(homeAgentsDir, 0o755); err != nil {
		t.Fatalf("mkdir home agents: %v", err)
	}
	if err := os.WriteFile(filepath.Join(homeAgentsDir, "config.yaml"), []byte(v2Header), 0o644); err != nil {
		t.Fatalf("write global config: %v", err)
	}

	// Mock cloneOrPull and discoverSkills since we're using a local registry.
	restoreClone := swapCloneOrPull(func(url, ref string) (string, error) {
		return url, nil
	})
	defer restoreClone()

	restoreDiscover := swapDiscoverSkills(func(dir string) ([]catalog.Entry, []skill.MalformedSkill, error) {
		return []catalog.Entry{
			{Name: "audit", Path: filepath.Join(dir, "audit")},
			{Name: "deploy", Path: filepath.Join(dir, "deploy")},
		}, nil, nil
	})
	defer restoreDiscover()

	// Mock config loaders to read from the temp directory instead of
	// using FindRepoRoot() which resolves from CWD.
	restoreOrg := swapLoadOrgConfig(func() (*config.Config, error) {
		return nil, nil
	})
	defer restoreOrg()

	restoreGlobal := swapLoadGlobalConfig(func() (*config.Config, error) {
		return newMockGlobalConfig()
	})
	defer restoreGlobal()

	restoreRepo := swapLoadRepoConfig(func() (*config.Config, error) {
		return config.LoadConfigFromPath(filepath.Join(repoRoot, ".agents", "config.yaml"))
	})
	defer restoreRepo()

	restoreLocal := swapLoadLocalConfig(func() (*config.LocalConfig, error) {
		return config.LoadLocalConfigFromPath(filepath.Join(repoRoot, ".agents", "config.local.yaml"))
	})
	defer restoreLocal()

	// Load snapshot BEFORE toggle.
	snap1 := readSnapshot(context.Background(), repoRoot, homeDir, catalogLoadCached)
	if len(snap1.errs) > 0 {
		t.Fatalf("initial snapshot errors: %v", snap1.errs)
	}

	// Find the "deploy" skill - it should be available (not enabled).
	var deployBefore *row
	for i := range snap1.rows {
		if snap1.rows[i].Artifact.Name == "deploy" {
			deployBefore = &snap1.rows[i]
			break
		}
	}
	if deployBefore == nil {
		t.Fatalf("deploy skill not found in initial snapshot")
	}
	if !deployBefore.IsAvailable {
		t.Fatalf("deploy should be available before toggle, got IsAvailable=%v", deployBefore.IsAvailable)
	}
	if deployBefore.EnabledLocal {
		t.Fatalf("deploy should NOT be locally enabled before toggle")
	}

	// Toggle: add "deploy" to the local config.
	if err := config.AddSkillToLocalConfig(repoRoot, "deploy"); err != nil {
		t.Fatalf("AddSkillToLocalConfig: %v", err)
	}

	// Load snapshot AFTER toggle.
	snap2 := readSnapshot(context.Background(), repoRoot, homeDir, catalogLoadCached)
	if len(snap2.errs) > 0 {
		t.Fatalf("post-toggle snapshot errors: %v", snap2.errs)
	}

	// Find the "deploy" skill - it should now be locally enabled.
	var deployAfter *row
	for i := range snap2.rows {
		if snap2.rows[i].Artifact.Name == "deploy" {
			deployAfter = &snap2.rows[i]
			break
		}
	}
	if deployAfter == nil {
		t.Fatalf("deploy skill not found in post-toggle snapshot")
	}
	if deployAfter.IsAvailable {
		t.Fatalf("deploy should NOT be available after local toggle, got IsAvailable=%v", deployAfter.IsAvailable)
	}
	if !deployAfter.EnabledLocal {
		t.Fatalf("deploy should be locally enabled after toggle, got EnabledLocal=%v, badge=%q, glyph=%s",
			deployAfter.EnabledLocal, originBadge(*deployAfter), deployAfter.Glyph())
	}
	if originBadge(*deployAfter) != "L" {
		t.Fatalf("expected badge L after local toggle, got %q", originBadge(*deployAfter))
	}
	// Glyph may be [*] (pending sync) since symlink hasn't been created yet;
	// the important thing is it's NOT [ ] (available) anymore.
	if deployAfter.Glyph() == "[ ]" {
		t.Fatalf("deploy glyph should NOT be [ ] after local toggle, got %s", deployAfter.Glyph())
	}
}

// TestReadSnapshotReflectsProjectToggle exercises the project-scope toggle:
// add a skill to the project config's registry entry skills list and verify
// the rows reflect the project enablement.
func TestReadSnapshotReflectsProjectToggle(t *testing.T) {
	repoRoot := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	agentsDir := filepath.Join(repoRoot, ".agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatalf("mkdir agents: %v", err)
	}

	// Project config: local registry with audit selected via entry-level skills.
	projectConfig := repoV2Header + `registries:
  - url: ./local-skills
    path: skills
    skills:
      - audit
`
	if err := os.WriteFile(filepath.Join(agentsDir, "config.yaml"), []byte(projectConfig), 0o644); err != nil {
		t.Fatalf("write project config: %v", err)
	}

	skillDir := filepath.Join(repoRoot, "local-skills", "skills", "audit")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# Audit\n"), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}

	extraDir := filepath.Join(repoRoot, "local-skills", "skills", "deploy")
	if err := os.MkdirAll(extraDir, 0o755); err != nil {
		t.Fatalf("mkdir extra dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(extraDir, "SKILL.md"), []byte("# Deploy\n"), 0o644); err != nil {
		t.Fatalf("write extra SKILL.md: %v", err)
	}

	homeAgentsDir := filepath.Join(homeDir, ".agents")
	if err := os.MkdirAll(homeAgentsDir, 0o755); err != nil {
		t.Fatalf("mkdir home agents: %v", err)
	}
	if err := os.WriteFile(filepath.Join(homeAgentsDir, "config.yaml"), []byte(v2Header), 0o644); err != nil {
		t.Fatalf("write global config: %v", err)
	}

	restoreClone := swapCloneOrPull(func(url, ref string) (string, error) {
		return url, nil
	})
	defer restoreClone()

	restoreDiscover := swapDiscoverSkills(func(dir string) ([]catalog.Entry, []skill.MalformedSkill, error) {
		return []catalog.Entry{
			{Name: "audit", Path: filepath.Join(dir, "audit")},
			{Name: "deploy", Path: filepath.Join(dir, "deploy")},
		}, nil, nil
	})
	defer restoreDiscover()

	restoreOrg := swapLoadOrgConfig(func() (*config.Config, error) {
		return nil, nil
	})
	defer restoreOrg()

	restoreGlobal := swapLoadGlobalConfig(func() (*config.Config, error) {
		return newMockGlobalConfig()
	})
	defer restoreGlobal()

	restoreRepo := swapLoadRepoConfig(func() (*config.Config, error) {
		return config.LoadConfigFromPath(filepath.Join(repoRoot, ".agents", "config.yaml"))
	})
	defer restoreRepo()

	restoreLocal := swapLoadLocalConfig(func() (*config.LocalConfig, error) {
		return config.LoadLocalConfigFromPath(filepath.Join(repoRoot, ".agents", "config.local.yaml"))
	})
	defer restoreLocal()

	// Before toggle: deploy is available.
	snap1 := readSnapshot(context.Background(), repoRoot, homeDir, catalogLoadCached)
	if len(snap1.errs) > 0 {
		t.Fatalf("initial snapshot errors: %v", snap1.errs)
	}
	var deployBefore *row
	for i := range snap1.rows {
		if snap1.rows[i].Artifact.Name == "deploy" {
			deployBefore = &snap1.rows[i]
			break
		}
	}
	if deployBefore == nil {
		t.Fatalf("deploy not found in initial snapshot")
	}
	if !deployBefore.IsAvailable {
		t.Fatalf("deploy should be available before toggle")
	}

	// Toggle: add "deploy" to the project config's registry entry skills.
	if err := config.AddSkillToRegistryInConfig(config.ProjectConfigPath(repoRoot), "./local-skills", "", "deploy"); err != nil {
		t.Fatalf("AddSkillToRegistryInConfig: %v", err)
	}

	// After toggle: deploy should be project-enabled.
	snap2 := readSnapshot(context.Background(), repoRoot, homeDir, catalogLoadCached)
	if len(snap2.errs) > 0 {
		t.Fatalf("post-toggle snapshot errors: %v", snap2.errs)
	}
	var deployAfter *row
	for i := range snap2.rows {
		if snap2.rows[i].Artifact.Name == "deploy" {
			deployAfter = &snap2.rows[i]
			break
		}
	}
	if deployAfter == nil {
		t.Fatalf("deploy not found in post-toggle snapshot")
	}
	if !deployAfter.EnabledProject {
		t.Fatalf("deploy should be project-enabled after toggle, got EnabledProject=%v, badge=%q, glyph=%s, IsAvailable=%v",
			deployAfter.EnabledProject, originBadge(*deployAfter), deployAfter.Glyph(), deployAfter.IsAvailable)
	}
	if originBadge(*deployAfter) != "P" {
		t.Fatalf("expected badge P after project toggle, got %q", originBadge(*deployAfter))
	}
}

func newMockGlobalConfig() (*config.Config, error) {
	return &config.Config{
		APIVersion: config.APIVersionV2,
		Harnesses:  []string{"factory"},
	}, nil
}

func swapLoadGlobalConfig(fn func() (*config.Config, error)) func() {
	prev := loadGlobalConfig
	loadGlobalConfig = fn
	return func() {
		loadGlobalConfig = prev
	}
}

func swapLoadRepoConfig(fn func() (*config.Config, error)) func() {
	prev := loadRepoConfig
	loadRepoConfig = fn
	return func() {
		loadRepoConfig = prev
	}
}

func swapLoadLocalConfig(fn func() (*config.LocalConfig, error)) func() {
	prev := loadLocalConfig
	loadLocalConfig = fn
	return func() {
		loadLocalConfig = prev
	}
}

func swapLoadOrgConfig(fn func() (*config.Config, error)) func() {
	prev := loadOrgConfig
	loadOrgConfig = fn
	return func() {
		loadOrgConfig = prev
	}
}

// TestReadSnapshotProjectToggleWithPerEntrySkills verifies that the fix
// for per-registry skills targeting works: when a project registry entry
// has its own skills list, the toggle now writes to that entry's skills
// list (not the top-level), so the resolver picks up the change.
func TestReadSnapshotProjectToggleWithPerEntrySkills(t *testing.T) {
	repoRoot := t.TempDir()
	homeDir := t.TempDir()

	agentsDir := filepath.Join(repoRoot, ".agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatalf("mkdir agents: %v", err)
	}

	// Project config: registry entry HAS its own skills list.
	// The toggle should now write to the per-entry skills list.
	projectConfig := repoV2Header + `registries:
  - url: ./local-skills
    path: skills
    skills:
      - audit
skills:
  - audit
`
	if err := os.WriteFile(filepath.Join(agentsDir, "config.yaml"), []byte(projectConfig), 0o644); err != nil {
		t.Fatalf("write project config: %v", err)
	}

	skillDir := filepath.Join(repoRoot, "local-skills", "skills", "audit")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# Audit\n"), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}

	extraDir := filepath.Join(repoRoot, "local-skills", "skills", "deploy")
	if err := os.MkdirAll(extraDir, 0o755); err != nil {
		t.Fatalf("mkdir extra dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(extraDir, "SKILL.md"), []byte("# Deploy\n"), 0o644); err != nil {
		t.Fatalf("write extra SKILL.md: %v", err)
	}

	restoreClone := swapCloneOrPull(func(url, ref string) (string, error) {
		return url, nil
	})
	defer restoreClone()

	restoreDiscover := swapDiscoverSkills(func(dir string) ([]catalog.Entry, []skill.MalformedSkill, error) {
		return []catalog.Entry{
			{Name: "audit", Path: filepath.Join(dir, "audit")},
			{Name: "deploy", Path: filepath.Join(dir, "deploy")},
		}, nil, nil
	})
	defer restoreDiscover()

	restoreOrg := swapLoadOrgConfig(func() (*config.Config, error) {
		return nil, nil
	})
	defer restoreOrg()

	restoreGlobal := swapLoadGlobalConfig(func() (*config.Config, error) {
		return newMockGlobalConfig()
	})
	defer restoreGlobal()

	restoreRepo := swapLoadRepoConfig(func() (*config.Config, error) {
		return config.LoadConfigFromPath(filepath.Join(repoRoot, ".agents", "config.yaml"))
	})
	defer restoreRepo()

	restoreLocal := swapLoadLocalConfig(func() (*config.LocalConfig, error) {
		return config.LoadLocalConfigFromPath(filepath.Join(repoRoot, ".agents", "config.local.yaml"))
	})
	defer restoreLocal()

	// Toggle: add "deploy" to the per-registry skills list (not top-level).
	registryURL := "./local-skills"
	if err := config.AddSkillToRegistryInConfig(config.ProjectConfigPath(repoRoot), registryURL, "", "deploy"); err != nil {
		t.Fatalf("AddSkillToRegistryInConfig: %v", err)
	}

	// After toggle: deploy should now be project-enabled because the
	// skill was added to the specific registry entry's skills list.
	snap := readSnapshot(context.Background(), repoRoot, homeDir, catalogLoadCached)
	var deployRow *row
	for i := range snap.rows {
		if snap.rows[i].Artifact.Name == "deploy" {
			deployRow = &snap.rows[i]
			break
		}
	}
	if deployRow == nil {
		t.Fatalf("deploy not found in snapshot")
	}
	if !deployRow.EnabledProject {
		t.Fatalf("deploy should be EnabledProject after per-registry toggle, "+
			"got EnabledProject=%v, badge=%q, glyph=%s, IsAvailable=%v",
			deployRow.EnabledProject, originBadge(*deployRow), deployRow.Glyph(), deployRow.IsAvailable)
	}
	if deployRow.IsAvailable {
		t.Fatalf("deploy should NOT be available after toggle")
	}
}

// Test that when a skill moves from Available to Local section after
// toggle, the cursor still finds the skill and the list shows the
// updated badge. This exercises the full recomputeView path that
// rebuilds the tree structure.
func TestToggleMovesSkillBetweenSections(t *testing.T) {
	// Set up with multiple skills across sections so the tree is
	// non-trivial. A globally-enabled skill and an available skill.
	globalRow := mkRow("audit", withGlobal, withRegistry("sec.git", "security"))
	availRow := mkRow("deploy", withAvailable, withRegistry("team.git", "team"))

	m := NewModel("/repo", "/home")
	m.loading = false
	m.width = 80
	m.height = 24
	m.rows = []row{globalRow, availRow}
	m.filteredRows = m.rows
	m.rebuildFlatItems()

	// Find and cursor on the available skill.
	cursorOnAvail := false
	for i, fi := range m.flatItems {
		if fi.Kind == flatArtifact && fi.Row.Artifact.Name == "deploy" {
			m.cursor = i
			cursorOnAvail = true
			break
		}
	}
	if !cursorOnAvail {
		t.Fatalf("could not find 'deploy' skill in flatItems")
	}

	// Before: deploy is available.
	availRowBefore := m.rows[1]
	if availRowBefore.Glyph() != "[ ]" {
		t.Fatalf("expected [ ] before toggle, got %s", availRowBefore.Glyph())
	}

	// Simulate snapshot after local toggle: deploy is now locally enabled.
	updatedDeploy := row{
		Artifact:     resolver.ResolvedArtifact{Entry: catalog.Entry{Name: "deploy", Path: "/cache/deploy"}, RegistryURL: "team.git"},
		EnabledLocal: true,
		RegistryURL:  "team.git",
		RegistryName: "team",
	}

	snapMsg := snapshotLoadedMsg{
		generation: 1,
		rows:       []row{globalRow, updatedDeploy},
		mergedCfg:  &config.Config{},
		resolved:   []resolver.ResolvedArtifact{},
	}

	mi, _ := m.Update(snapMsg)
	m = mi.(Model)

	// After: deploy row should have updated flags.
	var deployRow row
	for _, r := range m.rows {
		if r.Artifact.Name == "deploy" {
			deployRow = r
			break
		}
	}
	if deployRow.IsAvailable {
		t.Fatalf("expected deploy IsAvailable=false after toggle")
	}
	if !deployRow.EnabledLocal {
		t.Fatalf("expected deploy EnabledLocal=true after toggle")
	}
	if deployRow.Glyph() != "[x]" {
		t.Fatalf("expected deploy glyph [x] after toggle, got %s", deployRow.Glyph())
	}
	if originBadge(deployRow) != "L" {
		t.Fatalf("expected deploy badge L after toggle, got %q", originBadge(deployRow))
	}

	// The rendered list must contain the updated glyph and badge.
	out := renderList(m, 24)
	if !contains(out, "[x]") {
		t.Fatalf("rendered list should contain [x] after toggle, got:\n%s", out)
	}
	if !contains(out, "L") {
		t.Fatalf("rendered list should contain L badge after toggle, got:\n%s", out)
	}
}

func TestDisableSelectsAdjacentResourceAfterSnapshot(t *testing.T) {
	for _, tc := range []struct {
		name          string
		disabled      string
		disabledIndex int
		want          string
	}{
		{name: "next resource", disabled: "bravo", disabledIndex: 1, want: "charlie"},
		{name: "previous resource at end", disabled: "charlie", disabledIndex: 2, want: "bravo"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			alpha := mkRow("alpha", withProject)
			bravo := mkRow("bravo", withProject)
			charlie := mkRow("charlie", withProject)
			m := NewModel("/repo", "/home")
			m.loading = false
			m.rows = []row{alpha, bravo, charlie}
			m.filteredRows = m.rows
			m.rebuildFlatItems()
			for i, item := range m.flatItems {
				if item.Kind == flatArtifact && item.Row.Artifact.Name == tc.disabled {
					m.cursor = i
					break
				}
			}

			operation, ok := m.beginOperation(operationMutation)
			if !ok {
				t.Fatal("mutation operation was not accepted")
			}
			updated, _ := m.Update(configWrittenMsg{operation: operation, skillName: tc.disabled, selectNext: true})
			m = updated.(Model)
			generation := m.requests.active(requestSnapshot)
			after := []row{alpha, bravo, charlie}
			after[tc.disabledIndex] = mkRow(tc.disabled, withAvailable)
			updated, _ = m.Update(snapshotLoadedMsg{generation: generation, rows: after})
			m = updated.(Model)

			selected, ok := m.selectedRow()
			if !ok || selected.Artifact.Name != tc.want {
				t.Fatalf("selection after disabling %s = %#v, ok=%v; want %s", tc.disabled, selected, ok, tc.want)
			}
		})
	}
}

// A disabled row moves to a different Status bucket, so the snapshot rebuild
// must not scroll to its new position before selecting the neighbouring row.
func TestDisableInStatusTabKeepsListScrollPosition(t *testing.T) {
	names := make([]string, 30)
	enabled := make([]row, len(names))
	for i := range names {
		names[i] = fmt.Sprintf("skill-%02d", i)
		enabled[i] = mkRow(names[i], withProject)
	}
	m := NewModel("/repo", "/home")
	m.loading = false
	m.width, m.height = 120, 20
	m.rows = enabled
	m.authoritativeRows = enabled
	m.switchTab(tabStatus)
	if m.listVisibleHeight() >= len(m.flatItems) {
		t.Fatalf("fixture must overflow the list: visible=%d items=%d", m.listVisibleHeight(), len(m.flatItems))
	}
	disabled := names[1]
	for i, item := range m.flatItems {
		if item.Kind == flatArtifact && item.Row.Artifact.Name == disabled {
			m.cursor = i
			break
		}
	}
	m.listOffset = 0
	m.clampListOffset()
	if m.listOffset != 0 {
		t.Fatalf("fixture listOffset = %d, want 0", m.listOffset)
	}

	operation, ok := m.beginOperation(operationMutation)
	if !ok {
		t.Fatal("mutation operation was not accepted")
	}
	updated, _ := m.Update(configWrittenMsg{operation: operation, skillName: disabled, selectNext: true})
	m = updated.(Model)
	after := append([]row(nil), enabled...)
	after[1] = mkRow(disabled, withAvailable)
	updated, _ = m.Update(snapshotLoadedMsg{generation: m.requests.active(requestSnapshot), rows: after})
	m = updated.(Model)

	selected, ok := m.selectedRow()
	if !ok || selected.Artifact.Name != names[2] {
		t.Fatalf("selection after disabling %s = %q, ok=%v; want %s", disabled, selected.Artifact.Name, ok, names[2])
	}
	if m.listOffset != 0 {
		t.Fatalf("listOffset after disable = %d, want 0 so the section headers stay visible", m.listOffset)
	}
}

func TestToggleRemainsSerializedUntilAuthoritativeReload(t *testing.T) {
	m := NewModel(t.TempDir(), t.TempDir())
	m.loading = false
	m.width, m.height = 80, 24
	m.rows = []row{mkRow("review", withProject)}
	m.filteredRows = m.rows
	m.rebuildFlatItems()
	first, ok := m.beginOperation(operationMutation)
	if !ok {
		t.Fatal("initial toggle was blocked")
	}
	updated, cmd := m.Update(configWrittenMsg{operation: first, skillName: "review", enabled: true})
	m = updated.(Model)
	generation := m.requests.active(requestSnapshot)
	if cmd == nil || generation == 0 {
		t.Fatal("config write did not schedule an authoritative reload")
	}
	if _, ok := m.beginOperation(operationMutation); ok {
		t.Fatal("second toggle started before authoritative reload")
	}
	updated, _ = m.Update(snapshotLoadedMsg{generation: generation - 1, rows: []row{mkRow("stale", withProject)}})
	m = updated.(Model)
	if _, ok := m.beginOperation(operationMutation); ok {
		t.Fatal("stale snapshot unblocked a second toggle")
	}
	updated, _ = m.Update(snapshotLoadedMsg{generation: generation, rows: []row{mkRow("review", withProject)}, mergedCfg: &config.Config{}, driftAssessment: driftReady})
	m = updated.(Model)
	if m.operations.active != nil {
		t.Fatal("accepted reload left the toggle blocked")
	}
	if _, ok := m.beginOperation(operationMutation); !ok {
		t.Fatal("toggle was not available after authoritative reload")
	}
}

func TestToggleSupersededReloadReportsAuthoritativeFailure(t *testing.T) {
	m := NewModel(t.TempDir(), t.TempDir())
	m.loading = false
	first, ok := m.beginOperation(operationMutation)
	if !ok {
		t.Fatal("initial toggle was blocked")
	}
	updated, _ := m.Update(configWrittenMsg{operation: first, skillName: "review", enabled: true})
	m = updated.(Model)
	original := m.requests.active(requestSnapshot)
	superseding := m.beginRequest(requestSnapshot)
	updated, _ = m.Update(snapshotLoadedMsg{generation: original, rows: []row{mkRow("stale", withProject)}})
	m = updated.(Model)
	if m.operations.active == nil || m.pendingToggleReload != original {
		t.Fatal("stale snapshot completed a pending toggle")
	}
	failure := fmt.Errorf("catalog unavailable")
	updated, _ = m.Update(snapshotLoadedMsg{generation: superseding, mergedCfg: &config.Config{}, driftAssessment: driftFailed, errs: []error{failure}})
	m = updated.(Model)
	if m.operations.active != nil || m.pendingToggleReload != 0 {
		t.Fatal("accepted failed reload left the toggle operation active")
	}
	if !m.rowsStale || !contains(m.toast, "catalog unavailable") {
		t.Fatalf("failed reload reported stale or misleading status: stale=%t toast=%q", m.rowsStale, m.toast)
	}
}

// TestReadSnapshotExpandsCollections verifies that the TUI's readSnapshot
// expands collection references into synthetic registry entries before
// resolving skills. Without this step, skills selected via collections
// would never appear in the TUI (they'd be missing from resolved output).
func TestReadSnapshotExpandsCollections(t *testing.T) {
	repoRoot := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	// Set up project config with a local registry and a collection reference.
	agentsDir := filepath.Join(repoRoot, ".agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatalf("mkdir agents: %v", err)
	}

	// The registry has skills, but none are selected via the entry's
	// skills/tags/required/default fields. Skills are only selected via
	// the collection reference.
	projectConfig := repoV2Header + `registries:
  - url: ./local-skills
    path: skills
collections:
  include:
    - my-tools
`
	if err := os.WriteFile(filepath.Join(agentsDir, "config.yaml"), []byte(projectConfig), 0o644); err != nil {
		t.Fatalf("write project config: %v", err)
	}

	// Create the local registry with two skills.
	for _, name := range []string{"audit", "deploy"} {
		skillDir := filepath.Join(repoRoot, "local-skills", "skills", name)
		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			t.Fatalf("mkdir skill dir %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# "+name+"\n"), 0o644); err != nil {
			t.Fatalf("write SKILL.md for %s: %v", name, err)
		}
	}

	// Create a collection that selects the "audit" skill.
	collsDir := filepath.Join(repoRoot, "local-skills", "skills", "collections", "my-tools")
	if err := os.MkdirAll(collsDir, 0o755); err != nil {
		t.Fatalf("mkdir collections dir: %v", err)
	}
	collectionYAML := `name: my-tools
skills:
  - name: audit
`
	if err := os.WriteFile(filepath.Join(collsDir, "COLLECTION.yaml"), []byte(collectionYAML), 0o644); err != nil {
		t.Fatalf("write COLLECTION.yaml: %v", err)
	}

	// Empty global config.
	homeAgentsDir := filepath.Join(homeDir, ".agents")
	if err := os.MkdirAll(homeAgentsDir, 0o755); err != nil {
		t.Fatalf("mkdir home agents: %v", err)
	}
	if err := os.WriteFile(filepath.Join(homeAgentsDir, "config.yaml"), []byte(v2Header), 0o644); err != nil {
		t.Fatalf("write global config: %v", err)
	}

	restoreClone := swapCloneOrPull(func(url, ref string) (string, error) {
		return url, nil
	})
	defer restoreClone()

	restoreDiscover := swapDiscoverSkills(func(dir string) ([]catalog.Entry, []skill.MalformedSkill, error) {
		return []catalog.Entry{
			{Name: "audit", Path: filepath.Join(dir, "audit")},
			{Name: "deploy", Path: filepath.Join(dir, "deploy")},
		}, nil, nil
	})
	defer restoreDiscover()

	restoreOrg := swapLoadOrgConfig(func() (*config.Config, error) {
		return nil, nil
	})
	defer restoreOrg()

	restoreGlobal := swapLoadGlobalConfig(func() (*config.Config, error) {
		return newMockGlobalConfig()
	})
	defer restoreGlobal()

	restoreRepo := swapLoadRepoConfig(func() (*config.Config, error) {
		return config.LoadConfigFromPath(filepath.Join(repoRoot, ".agents", "config.yaml"))
	})
	defer restoreRepo()

	restoreLocal := swapLoadLocalConfig(func() (*config.LocalConfig, error) {
		return config.LoadLocalConfigFromPath(filepath.Join(repoRoot, ".agents", "config.local.yaml"))
	})
	defer restoreLocal()

	snap := readSnapshot(context.Background(), repoRoot, homeDir, catalogLoadCached)
	if len(snap.errs) > 0 {
		t.Fatalf("snapshot errors: %v", snap.errs)
	}

	byName := make(map[string]row, len(snap.rows))
	for _, r := range snap.rows {
		byName[r.Artifact.Name] = r
	}

	// audit should be resolved (enabled via collection), not just available.
	audit, ok := byName["audit"]
	if !ok {
		t.Fatalf("audit skill not found in snapshot rows")
	}
	if audit.IsAvailable {
		t.Fatalf("audit should be enabled (via collection), not available; got IsAvailable=%v EnabledProject=%v EnabledGlobal=%v",
			audit.IsAvailable, audit.EnabledProject, audit.EnabledGlobal)
	}
	// The skill is from a project-scope registry (local, RepoCommitted source),
	// so it should show as project-enabled.
	if !audit.EnabledProject {
		t.Fatalf("audit from project registry should be EnabledProject=true, got EnabledProject=%v", audit.EnabledProject)
	}

	// deploy is NOT in the collection, so it should be available.
	deploy, ok := byName["deploy"]
	if !ok {
		t.Fatalf("deploy skill not found in snapshot rows")
	}
	if !deploy.IsAvailable {
		t.Fatalf("deploy should be available (not in collection), got IsAvailable=%v", deploy.IsAvailable)
	}
}

// TestReadSnapshotProjectToggleWithGlobalRegistry verifies that enabling
// a skill at project scope works even when the skill's source registry
// only exists in the global config (not in the project config). This
// exercises the EnsureRegistryWithPathsInConfig fallback added in the fix.
func TestReadSnapshotProjectToggleWithGlobalRegistry(t *testing.T) {
	repoRoot := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	agentsDir := filepath.Join(repoRoot, ".agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatalf("mkdir agents: %v", err)
	}

	// Project config has NO git registries — only a local one.
	projectConfig := repoV2Header + `registries:
  - url: ./local-skills
    path: skills
`
	if err := os.WriteFile(filepath.Join(agentsDir, "config.yaml"), []byte(projectConfig), 0o644); err != nil {
		t.Fatalf("write project config: %v", err)
	}

	// Create the local registry with one skill.
	skillDir := filepath.Join(repoRoot, "local-skills", "skills", "local-tool")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# local-tool\n"), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}

	// Global config has a local registry with one skill selected. This test
	// exercises scope toggling; exact remote refresh behavior is covered
	// separately.
	globalRegistry := filepath.Join(repoRoot, "global-skills")
	if err := os.MkdirAll(filepath.Join(globalRegistry, "skills"), 0o755); err != nil {
		t.Fatalf("mkdir global registry: %v", err)
	}
	homeAgentsDir := filepath.Join(homeDir, ".agents")
	if err := os.MkdirAll(homeAgentsDir, 0o755); err != nil {
		t.Fatalf("mkdir home agents: %v", err)
	}
	globalConfig := v2Header + `registries:
  - url: ` + globalRegistry + `
    path: skills
    skills:
      - global-tool
`
	if err := os.WriteFile(filepath.Join(homeAgentsDir, "config.yaml"), []byte(globalConfig), 0o644); err != nil {
		t.Fatalf("write global config: %v", err)
	}

	restoreClone := swapCloneOrPull(func(url, ref string) (string, error) {
		return t.TempDir(), nil
	})
	defer restoreClone()

	restoreDiscover := swapDiscoverSkills(func(dir string) ([]catalog.Entry, []skill.MalformedSkill, error) {
		// Return skills for both the local and global registries.
		if filepath.Base(dir) == "skills" {
			return []catalog.Entry{
				{Name: "local-tool", Path: filepath.Join(dir, "local-tool")},
				{Name: "global-tool", Path: filepath.Join(dir, "global-tool")},
			}, nil, nil
		}
		return nil, nil, nil
	})
	defer restoreDiscover()

	restoreOrg := swapLoadOrgConfig(func() (*config.Config, error) {
		return nil, nil
	})
	defer restoreOrg()

	restoreGlobal := swapLoadGlobalConfig(func() (*config.Config, error) {
		return config.LoadConfigFromPath(filepath.Join(homeDir, ".agents", "config.yaml"))
	})
	defer restoreGlobal()

	restoreRepo := swapLoadRepoConfig(func() (*config.Config, error) {
		return config.LoadConfigFromPath(filepath.Join(repoRoot, ".agents", "config.yaml"))
	})
	defer restoreRepo()

	restoreLocal := swapLoadLocalConfig(func() (*config.LocalConfig, error) {
		return config.LoadLocalConfigFromPath(filepath.Join(repoRoot, ".agents", "config.local.yaml"))
	})
	defer restoreLocal()

	snap := readSnapshot(context.Background(), repoRoot, homeDir, catalogLoadRefresh)
	if len(snap.errs) > 0 {
		t.Fatalf("initial snapshot errors: %v", snap.errs)
	}

	// Find global-tool — it should be globally enabled.
	var globalTool *row
	for i := range snap.rows {
		if snap.rows[i].Artifact.Name == "global-tool" {
			globalTool = &snap.rows[i]
			break
		}
	}
	if globalTool == nil {
		t.Fatalf("global-tool not found in snapshot")
	}
	if !globalTool.EnabledGlobal {
		t.Fatalf("global-tool should be globally enabled, got EnabledGlobal=%v", globalTool.EnabledGlobal)
	}
	if globalTool.EnabledProject {
		t.Fatalf("global-tool should NOT be project-enabled initially, got EnabledProject=%v", globalTool.EnabledProject)
	}

	// Now enable global-tool at project scope. The registry URL
	// (git@example.com:global-skills.git) does NOT exist in the
	// project config, so EnsureRegistryWithPathsInConfig must add it first.
	projectPath := config.ProjectConfigPath(repoRoot)
	if err := config.EnsureRegistryWithPathsInConfig(projectPath, globalTool.RegistryURL, globalTool.RegistryRef, globalTool.RegistryPath); err != nil {
		t.Fatalf("EnsureRegistryWithPathsInConfig: %v", err)
	}
	if err := config.AddSkillToRegistryInConfig(projectPath, globalTool.RegistryURL, "", "global-tool"); err != nil {
		t.Fatalf("AddSkillToRegistryInConfig: %v", err)
	}

	// Reload snapshot and verify project enablement.
	snap2 := readSnapshot(context.Background(), repoRoot, homeDir, catalogLoadRefresh)
	// Overlap warnings are acceptable (global and project entries can overlap).
	for _, err := range snap2.errs {
		if !catalog.IsOverlapWarning(err) {
			t.Fatalf("post-toggle snapshot errors: %v", snap2.errs)
		}
	}

	var globalTool2 *row
	for i := range snap2.rows {
		if snap2.rows[i].Artifact.Name == "global-tool" {
			globalTool2 = &snap2.rows[i]
			break
		}
	}
	if globalTool2 == nil {
		t.Fatalf("global-tool not found in post-toggle snapshot")
	}
	if !globalTool2.EnabledProject {
		t.Fatalf("global-tool should be project-enabled after toggle, got EnabledProject=%v", globalTool2.EnabledProject)
	}
	if !globalTool2.EnabledGlobal {
		t.Fatalf("global-tool should still be globally enabled after toggle, got EnabledGlobal=%v", globalTool2.EnabledGlobal)
	}
}

// TestToggleFlowEnableAfterExcludeRoundTripGlobal verifies that toggling a
// skill OFF at global scope (adding exclude) and then back ON (adding
// include) correctly cleans up the exclude. This exercises the FIX-001
// enable-cleans-exclude logic at registry scope.
func TestToggleFlowEnableAfterExcludeRoundTripGlobal(t *testing.T) {
	repoRoot := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	agentsDir := filepath.Join(repoRoot, ".agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatalf("mkdir agents: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentsDir, "config.yaml"), []byte(repoV2Header), 0o644); err != nil {
		t.Fatalf("write project config: %v", err)
	}

	// Global config: default:true registry with smith in the catalog.
	// Use an absolute local path (not a repo-relative one) so the catalog
	// loader can stat the directory; repo-relative URLs are meaningless in
	// the user-personal (global) config.
	globalSkillsDir := filepath.Join(t.TempDir(), "global-skills")
	if err := os.MkdirAll(filepath.Join(globalSkillsDir, "skills"), 0o755); err != nil {
		t.Fatalf("mkdir global skills: %v", err)
	}
	homeAgentsDir := filepath.Join(homeDir, ".agents")
	if err := os.MkdirAll(homeAgentsDir, 0o755); err != nil {
		t.Fatalf("mkdir home agents: %v", err)
	}
	globalConfig := v2Header + fmt.Sprintf(`registries:
  - url: %s
    path: skills
    default: true
`, globalSkillsDir)
	if err := os.WriteFile(filepath.Join(homeAgentsDir, "config.yaml"), []byte(globalConfig), 0o644); err != nil {
		t.Fatalf("write global config: %v", err)
	}

	restoreClone := swapCloneOrPull(func(url, ref string) (string, error) {
		return url, nil
	})
	defer restoreClone()

	restoreDiscover := swapDiscoverSkills(func(dir string) ([]catalog.Entry, []skill.MalformedSkill, error) {
		return []catalog.Entry{
			{Name: "smith", Path: filepath.Join(dir, "smith")},
		}, nil, nil
	})
	defer restoreDiscover()

	restoreOrg := swapLoadOrgConfig(func() (*config.Config, error) {
		return nil, nil
	})
	defer restoreOrg()

	restoreGlobal := swapLoadGlobalConfig(func() (*config.Config, error) {
		return config.LoadConfigFromPath(filepath.Join(homeDir, ".agents", "config.yaml"))
	})
	defer restoreGlobal()
	restoreRepo := swapLoadRepoConfig(func() (*config.Config, error) {
		return config.LoadConfigFromPath(filepath.Join(repoRoot, ".agents", "config.yaml"))
	})
	defer restoreRepo()
	restoreLocal := swapLoadLocalConfig(func() (*config.LocalConfig, error) {
		return config.LoadLocalConfigFromPath(filepath.Join(repoRoot, ".agents", "config.local.yaml"))
	})
	defer restoreLocal()

	// Step 1: Disable smith at global scope — adds to exclude.
	globalPath := filepath.Join(homeDir, ".agents", "config.yaml")
	if err := config.DisableSkillInRegistryInConfig(globalPath, globalSkillsDir, "", "smith"); err != nil {
		t.Fatalf("disable smith: %v", err)
	}

	snap2 := readSnapshot(context.Background(), repoRoot, homeDir, catalogLoadCached)
	if len(snap2.errs) > 0 {
		t.Fatalf("after disable snapshot errors: %v", snap2.errs)
	}
	var smithAfterDisable *row
	for i := range snap2.rows {
		if snap2.rows[i].Artifact.Name == "smith" {
			smithAfterDisable = &snap2.rows[i]
			break
		}
	}
	if smithAfterDisable == nil {
		t.Fatalf("smith not found after disable")
	}
	if smithAfterDisable.EnabledGlobal {
		t.Fatalf("smith should NOT be globally enabled after disable")
	}

	// Step 2: Re-enable smith at global scope — adds to include, must clean up exclude.
	if err := config.AddSkillToRegistryInConfig(globalPath, globalSkillsDir, "", "smith"); err != nil {
		t.Fatalf("re-enable smith: %v", err)
	}

	snap3 := readSnapshot(context.Background(), repoRoot, homeDir, catalogLoadCached)
	if len(snap3.errs) > 0 {
		t.Fatalf("after re-enable snapshot errors: %v", snap3.errs)
	}
	var smithAfterEnable *row
	for i := range snap3.rows {
		if snap3.rows[i].Artifact.Name == "smith" {
			smithAfterEnable = &snap3.rows[i]
			break
		}
	}
	if smithAfterEnable == nil {
		t.Fatalf("smith not found after re-enable")
	}
	if !smithAfterEnable.EnabledGlobal {
		t.Fatalf("smith should be globally enabled after re-enable")
	}

	// Verify the config no longer has smith in exclude.
	cfg, err := config.LoadConfigFromPath(globalPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	for _, e := range cfg.Registries {
		if e.URL == globalSkillsDir {
			for _, ex := range e.Skills.Exclude {
				if ex == "smith" {
					t.Fatalf("smith should NOT be in exclude after re-enable, got Exclude=%v", e.Skills.Exclude)
				}
			}
		}
	}
}

// TestToggleFlowEnableAfterLocalExcludeCleansUp verifies that enabling
// a skill locally after excluding it locally cleans up the exclude list.
// This exercises the FIX-002 local-scope enable-cleans-exclude logic.
func TestToggleFlowEnableAfterLocalExcludeCleansUp(t *testing.T) {
	repoRoot := t.TempDir()

	// Step 1: Exclude smith locally.
	if err := config.AddToLocalExclude(repoRoot, "smith"); err != nil {
		t.Fatalf("add local exclude: %v", err)
	}

	cfg1, err := config.LoadLocalConfigFromPath(config.LocalConfigPath(repoRoot))
	if err != nil {
		t.Fatalf("load local config after exclude: %v", err)
	}
	if !containsString(cfg1.Skills.Exclude, "smith") {
		t.Fatalf("expected smith in Skills.Exclude, got %v", cfg1.Skills.Exclude)
	}

	// Step 2: Enable smith locally — must clean up the exclude.
	if err := config.AddSkillToLocalConfig(repoRoot, "smith"); err != nil {
		t.Fatalf("add local include: %v", err)
	}

	cfg2, err := config.LoadLocalConfigFromPath(config.LocalConfigPath(repoRoot))
	if err != nil {
		t.Fatalf("load local config after enable: %v", err)
	}
	if !containsString(cfg2.Skills.Include, "smith") {
		t.Fatalf("expected smith in Skills.Include, got %v", cfg2.Skills.Include)
	}
	if containsString(cfg2.Skills.Exclude, "smith") {
		t.Fatalf("expected smith NOT in Skills.Exclude after enable, got %v", cfg2.Skills.Exclude)
	}
}

// TestToggleFlowSinglePressDisableWithDefault verifies that disabling a
// skill in a default:true registry removes from include AND adds to exclude
// in a single call. This exercises the FIX-003 logic.
func TestToggleFlowSinglePressDisableWithDefault(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), ".agents", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	mustWriteFile(t, configPath, []byte(`apiVersion: agent-marketplace/v2
registries:
  - url: git@example.com:infra.git
    ref: main
    default: true
    skills:
      include:
        - smith
`))

	if err := config.DisableSkillInRegistryInConfig(configPath, "git@example.com:infra.git", "", "smith"); err != nil {
		t.Fatalf("DisableSkillInRegistryInConfig: %v", err)
	}

	cfg, err := config.LoadConfigFromPath(configPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	entry := cfg.Registries[0]
	if containsString(entry.Skills.Include, "smith") {
		t.Fatalf("expected smith removed from include, got %v", entry.Skills.Include)
	}
	if !containsString(entry.Skills.Exclude, "smith") {
		t.Fatalf("expected smith in exclude (single-press disable with default:true), got %v", entry.Skills.Exclude)
	}
}

func containsString(slice []string, val string) bool {
	for _, s := range slice {
		if s == val {
			return true
		}
	}
	return false
}

func mustWriteFile(t *testing.T, path string, contents []byte) {
	t.Helper()
	if err := os.WriteFile(path, contents, 0o644); err != nil {
		t.Fatalf("write file %q: %v", path, err)
	}
}
