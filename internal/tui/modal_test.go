package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/skill"
)

// helperFlatCursorOnSkill returns a model with cursor on a known skill row.
func helperFlatCursorOnSkill(t *testing.T, r row) Model {
	t.Helper()
	m := NewModel("/repo", "/home")
	seedTestTerminalSize(&m)
	m.rows = []row{r}
	m.filteredRows = m.rows
	m.rebuildFlatItems()
	for i, it := range m.flatItems {
		if it.Kind == flatArtifact {
			m.cursor = i
			return m
		}
	}
	t.Fatalf("no skill flatItem found")
	return m
}

func TestSpaceOnSkillRowOpensToggleModal(t *testing.T) {
	r := mkRow("code-review", withProject, withRegistry("t.git", "t"))
	m := helperFlatCursorOnSkill(t, r)

	mi, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = mi.(Model)

	if m.pane != paneToggleModal {
		t.Fatalf("expected pane=paneToggleModal, got %v", m.pane)
	}
	if m.modalSkill.Artifact.Name != "code-review" {
		t.Fatalf("expected modalSkill=code-review, got %q", m.modalSkill.Artifact.Name)
	}
}

func TestSpaceOnRequiredSkillShowsToastNotModal(t *testing.T) {
	r := mkRow("req-skill", withRequired, withProject)
	m := helperFlatCursorOnSkill(t, r)

	mi, cmd := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = mi.(Model)
	if m.pane == paneToggleModal {
		t.Fatalf("required skill must bypass modal, got pane=paneToggleModal")
	}
	// Cmd should emit a toast message.
	if cmd == nil {
		t.Fatalf("expected a toast cmd for required skill")
	}
	msg := cmd()
	toast, ok := msg.(toastMsg)
	if !ok {
		t.Fatalf("expected toastMsg, got %T", msg)
	}
	if toast.text != "required items cannot be toggled" {
		t.Fatalf("unexpected toast text: %q", toast.text)
	}
}

func TestRenderToggleModalShowsPerScopeState(t *testing.T) {
	r := mkRow("audit", withGlobal, withProject, withExcluded, withRegistry("s.git", "s"))
	m := NewModel("/repo", "/home")
	seedTestTerminalSize(&m)
	m.pane = paneToggleModal
	m.modalSkill = r

	out := renderToggleModal(m)
	for _, want := range []string{
		"audit",
		"Global",
		"Project",
		"Local",
		"Local exclude",
		"[g]",
		"[p]",
		"[l]",
		"[e]",
		"[esc]",
	} {
		if !contains(out, want) {
			t.Fatalf("modal missing %q in output:\n%s", want, out)
		}
	}
}

func TestModalGkeyTogglesGlobalByCurrentState(t *testing.T) {
	// Not enabled globally: g should schedule an Add.
	m := NewModel("/repo", "/home")
	seedTestTerminalSize(&m)
	m.pane = paneToggleModal
	m.modalSkill = mkRow("audit", withProject)

	mi, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	m = mi.(Model)
	if cmd == nil {
		t.Fatalf("expected a cmd for modal 'g'")
	}
	if m.pane != paneList {
		t.Fatalf("modal should close on action, got pane=%v", m.pane)
	}
}

func TestModalPkeyTogglesProject(t *testing.T) {
	m := NewModel("/repo", "/home")
	seedTestTerminalSize(&m)
	m.pane = paneToggleModal
	m.modalSkill = mkRow("audit")

	mi, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	m = mi.(Model)
	if cmd == nil {
		t.Fatalf("expected a cmd for modal 'p'")
	}
	if m.pane != paneList {
		t.Fatalf("modal should close on action")
	}
}

func TestModalLkeyTogglesLocal(t *testing.T) {
	m := NewModel("/repo", "/home")
	seedTestTerminalSize(&m)
	m.pane = paneToggleModal
	m.modalSkill = mkRow("audit")

	mi, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	m = mi.(Model)
	if cmd == nil {
		t.Fatalf("expected a cmd for modal 'l'")
	}
	if m.pane != paneList {
		t.Fatalf("modal should close on action")
	}
}

func TestModalEkeyTogglesLocalExclude(t *testing.T) {
	m := NewModel("/repo", "/home")
	seedTestTerminalSize(&m)
	m.pane = paneToggleModal
	m.modalSkill = mkRow("audit")

	mi, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	m = mi.(Model)
	if cmd == nil {
		t.Fatalf("expected a cmd for modal 'e'")
	}
	if m.pane != paneList {
		t.Fatalf("modal should close on action")
	}
}

func TestRenderToggleModalDisablesLocalExcludeForMachineScopeSkill(t *testing.T) {
	r := mkRow("audit", withGlobal)
	r.Artifact.Source = config.UserPersonal

	m := NewModel("/repo", "/home")
	seedTestTerminalSize(&m)
	m.pane = paneToggleModal
	m.modalSkill = r

	out := renderToggleModal(m)
	if !contains(out, "toggle skill exclude (use [g] to toggle at user level)") {
		t.Fatalf("expected disabled local exclude hint in modal output, got:\n%s", out)
	}
}

func TestModalEkeyShowsToastForMachineScopeSkill(t *testing.T) {
	m := NewModel("/repo", "/home")
	seedTestTerminalSize(&m)
	m.pane = paneToggleModal
	m.modalSkill = mkRow("audit", withGlobal)
	m.modalSkill.Artifact.Source = config.UserPersonal

	mi, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	m = mi.(Model)
	if cmd == nil {
		t.Fatalf("expected toast cmd for blocked local exclude")
	}
	if m.pane != paneToggleModal {
		t.Fatalf("modal should stay open when local exclude is blocked")
	}
	msg := cmd()
	toast, ok := msg.(toastMsg)
	if !ok {
		t.Fatalf("expected toastMsg, got %T", msg)
	}
	if toast.text != "Cannot exclude org/user skill at local level — use [g] instead" {
		t.Fatalf("unexpected toast text: %q", toast.text)
	}
	if toast.duration != 2*time.Second {
		t.Fatalf("unexpected toast duration: %v", toast.duration)
	}
}

func TestModalEscCancelsWithoutWrite(t *testing.T) {
	m := NewModel("/repo", "/home")
	seedTestTerminalSize(&m)
	m.pane = paneToggleModal
	m.modalSkill = mkRow("audit")

	mi, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = mi.(Model)
	if m.pane != paneList {
		t.Fatalf("esc should close modal")
	}
	if cmd != nil {
		t.Fatalf("esc must not emit a write cmd")
	}
}

func TestScopedToggleAddsWhenDisabledRemovesWhenEnabled(t *testing.T) {
	// Global disabled → action = enable
	r1 := mkRow("x", withProject)
	act := scopedToggleAction(r1, scopeGlobal)
	if !act.enable {
		t.Fatalf("scopeGlobal on project-only skill should enable")
	}

	r2 := mkRow("x", withGlobal, withProject)
	act = scopedToggleAction(r2, scopeGlobal)
	if act.enable {
		t.Fatalf("scopeGlobal on globally-enabled skill should disable")
	}

	// Exclude toggle: if not excluded → add; if excluded → remove.
	r3 := mkRow("x", withProject)
	act = scopedToggleAction(r3, scopeLocalExclude)
	if !act.enable {
		t.Fatalf("scopeLocalExclude on non-excluded skill should add exclude")
	}

	r4 := mkRow("x", withProject, withExcluded)
	act = scopedToggleAction(r4, scopeLocalExclude)
	if act.enable {
		t.Fatalf("scopeLocalExclude on excluded skill should remove exclude")
	}
}

func TestScopedToggleActionPopulatesRegistryMetadata(t *testing.T) {
	r := mkRow("audit", withAvailable,
		withRegistry("git@example.com:sec.git", "sec"),
		withRegistryMeta("main", "skills"),
	)

	act := scopedToggleAction(r, scopeProject)
	if act.registryURL != "git@example.com:sec.git" {
		t.Fatalf("expected registryURL=git@example.com:sec.git, got %q", act.registryURL)
	}
	if act.registryRef != "main" {
		t.Fatalf("expected registryRef=main, got %q", act.registryRef)
	}
	if act.registryPath != "skills" {
		t.Fatalf("expected registryPath=skills, got %q", act.registryPath)
	}
}

func TestRunScopedToggleProjectScopeAutoEnsuresRegistry(t *testing.T) {
	repoRoot := t.TempDir()
	agentsDir := filepath.Join(repoRoot, ".agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Project config has NO registry matching the global-only URL.
	projectConfig := `apiVersion: agent-marketplace/v2
registries:
  - url: ./local
    path: skills
`
	if err := os.WriteFile(filepath.Join(agentsDir, "config.yaml"), []byte(projectConfig), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	// Toggle a skill from a global-only registry at project scope.
	act := toggleAction{
		skillName:    "audit",
		scope:        scopeProject,
		enable:       true,
		registryURL:  "git@example.com:global-skills.git",
		registryRef:  "main",
		registryPath: "skills",
	}

	cmd := runScopedToggle(repoRoot, act, 1, nil)
	msg := cmd()
	cw, ok := msg.(configWrittenMsg)
	if !ok {
		t.Fatalf("expected configWrittenMsg, got %T", msg)
	}
	if cw.err != nil {
		t.Fatalf("project-scope toggle should succeed with auto-ensure, got error: %v", cw.err)
	}
	if !cw.enabled {
		t.Fatalf("expected enabled=true")
	}
	if cw.operation != 1 {
		t.Fatalf("result did not carry the started operation id, got %d", cw.operation)
	}

	// Verify the project config now has the global registry entry.
	cfg, err := config.LoadConfigFromPath(filepath.Join(agentsDir, "config.yaml"))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	found := false
	for _, reg := range cfg.Registries {
		if reg.URL == "git@example.com:global-skills.git" {
			found = true
			if reg.Ref != "main" {
				t.Fatalf("expected ref=main, got %q", reg.Ref)
			}
			if reg.Path != "skills" {
				t.Fatalf("expected path=skills, got %q", reg.Path)
			}
			// The skill should be in the registry's skills list.
			hasAudit := false
			for _, s := range reg.Skills.Include {
				if s == "audit" {
					hasAudit = true
				}
			}
			if !hasAudit {
				t.Fatalf("expected audit in registry skills list, got %v", reg.Skills)
			}
		}
	}
	if !found {
		t.Fatalf("expected global-skills.git registry to be added to project config")
	}
}

// TestRunScopedToggleLocalScopeWritesRegistryScoped verifies that enabling a
// skill at local scope writes a registry-scoped include into config.local.yaml
// (creating the registry entry), NOT a top-level skills.include. Previously the
// local branch wrote to the top-level selector, which was inconsistent with the
// project/global scopes.
func TestRunScopedToggleLocalScopeWritesRegistryScoped(t *testing.T) {
	repoRoot := t.TempDir()
	agentsDir := filepath.Join(repoRoot, ".agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	act := toggleAction{
		skillName:    "audit",
		scope:        scopeLocal,
		enable:       true,
		registryURL:  "git@example.com:global-skills.git",
		registryRef:  "main",
		registryPath: "skills",
	}

	cmd := runScopedToggle(repoRoot, act, 1, nil)
	msg := cmd()
	cw, ok := msg.(configWrittenMsg)
	if !ok {
		t.Fatalf("expected configWrittenMsg, got %T", msg)
	}
	if cw.err != nil {
		t.Fatalf("local-scope toggle should succeed, got error: %v", cw.err)
	}

	localCfg, err := config.LoadLocalConfigFromPath(filepath.Join(agentsDir, "config.local.yaml"))
	if err != nil {
		t.Fatalf("load local config: %v", err)
	}
	// Local config must exist and must NOT use the top-level selector.
	if localCfg == nil || len(localCfg.Skills.Include) != 0 {
		t.Fatalf("expected local config written with no top-level skills.include, got %#v", localCfg)
	}
	// Must be registry-scoped under the matching entry.
	found := false
	for _, reg := range localCfg.Registries {
		if reg.URL == "git@example.com:global-skills.git" {
			found = true
			// The ensured local shadow entry must preserve the exact-entry
			// identity fields (ref + path), not just the URL-scoped include.
			if reg.Ref != "main" {
				t.Fatalf("expected ref=main on local registry entry, got %q", reg.Ref)
			}
			if reg.Path != "skills" {
				t.Fatalf("expected path=skills on local registry entry, got %q", reg.Path)
			}
			if !containsString(reg.Skills.Include, "audit") {
				t.Fatalf("expected audit in registry skills.include, got %v", reg.Skills.Include)
			}
		}
	}
	if !found {
		t.Fatalf("expected global-skills.git registry entry in local config, got %#v", localCfg.Registries)
	}
}

// TestRunScopedToggleLocalExcludeIsRegistryScoped reproduces the bug where
// toggling the local exclude ([e]) on a skill that was locally enabled wrote a
// top-level skills.exclude AND left the registry-scoped include in place,
// producing a contradictory include+exclude pair. The exclude must now be
// registry-scoped and must remove the include.
func TestRunScopedToggleLocalExcludeIsRegistryScoped(t *testing.T) {
	repoRoot := t.TempDir()
	agentsDir := filepath.Join(repoRoot, ".agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Local config already has a registry-scoped include (from a prior [l] enable).
	localPath := filepath.Join(agentsDir, "config.local.yaml")
	if err := os.WriteFile(localPath, []byte(`registries:
  - url: ./plugins/terminals
    skills:
      include:
        - adding-terminal-analytics-events
`), 0o644); err != nil {
		t.Fatalf("write local config: %v", err)
	}

	act := toggleAction{
		skillName:   "adding-terminal-analytics-events",
		kind:        ir.KindSkill,
		scope:       scopeLocalExclude,
		enable:      true,
		registryURL: "./plugins/terminals",
	}
	msg := runScopedToggle(repoRoot, act, 1, nil)()
	if cw, ok := msg.(configWrittenMsg); !ok || cw.err != nil {
		t.Fatalf("expected successful configWrittenMsg, got %#v", msg)
	}

	localCfg, err := config.LoadLocalConfigFromPath(localPath)
	if err != nil {
		t.Fatalf("load local config: %v", err)
	}
	// No top-level exclude.
	if len(localCfg.Skills.Exclude) != 0 {
		t.Fatalf("expected no top-level skills.exclude, got %v", localCfg.Skills.Exclude)
	}
	if len(localCfg.Registries) != 1 {
		t.Fatalf("expected one registry entry, got %#v", localCfg.Registries)
	}
	entry := localCfg.Registries[0]
	// Registry-scoped exclude present; the contradictory include removed.
	if !containsString(entry.Skills.Exclude, "adding-terminal-analytics-events") {
		t.Fatalf("expected registry-scoped exclude, got %#v", entry.Skills)
	}
	if containsString(entry.Skills.Include, "adding-terminal-analytics-events") {
		t.Fatalf("expected the contradictory include to be removed, got %#v", entry.Skills.Include)
	}
}

func TestHasCollisionErrorDetectsCollision(t *testing.T) {
	errs := []error{
		&resolver.NameCollisionError{Kind: ir.KindSkill, Name: "smith", Sources: []string{"global.git", "project.git"}},
	}
	if !hasCollisionError(errs, ir.KindSkill, "smith") {
		t.Fatalf("expected collision to be detected for smith")
	}
	if hasCollisionError(errs, ir.KindSkill, "other") {
		t.Fatalf("expected no collision for other")
	}
	if hasCollisionError(nil, ir.KindSkill, "smith") {
		t.Fatalf("expected no collision with nil errs")
	}
	if hasCollisionError([]error{fmt.Errorf("some other error")}, ir.KindSkill, "smith") {
		t.Fatalf("expected no collision for unrelated error")
	}
	// Different kind with same name should not match.
	if hasCollisionError(errs, ir.KindCommand, "smith") {
		t.Fatalf("expected no collision for command smith when error is about skill")
	}
}

func TestCompetingRegistryURLsFindsOtherSources(t *testing.T) {
	rows := []row{
		{Artifact: resolver.ResolvedArtifact{Entry: catalog.Entry{Name: "smith", Kind: ir.KindSkill}}, RegistryURL: "git@a.com:global.git"},
		{Artifact: resolver.ResolvedArtifact{Entry: catalog.Entry{Name: "smith", Kind: ir.KindSkill}}, RegistryURL: "git@b.com:project.git"},
		{Artifact: resolver.ResolvedArtifact{Entry: catalog.Entry{Name: "other", Kind: ir.KindSkill}}, RegistryURL: "git@a.com:global.git"},
		{Artifact: resolver.ResolvedArtifact{Entry: catalog.Entry{Name: "smith", Kind: ir.KindCommand}}, RegistryURL: "git@c.com:cmd.git"},
	}

	competing := competingRegistryURLs(rows, ir.KindSkill, "smith", "git@a.com:global.git")
	if len(competing) != 1 || competing[0] != "git@b.com:project.git" {
		t.Fatalf("expected competing=[git@b.com:project.git], got %v", competing)
	}

	// No competing sources.
	competing = competingRegistryURLs(rows, ir.KindSkill, "other", "git@a.com:global.git")
	if len(competing) != 0 {
		t.Fatalf("expected no competing URLs, got %v", competing)
	}

	// Same name but different kind should not be competing.
	competing = competingRegistryURLs(rows, ir.KindCommand, "smith", "git@c.com:cmd.git")
	if len(competing) != 0 {
		t.Fatalf("expected no competing URLs across different kinds, got %v", competing)
	}
}

func TestRenderToggleModalShowsCollisionWarning(t *testing.T) {
	r := mkRow("smith", withAvailable, withRegistry("global.git", "global"))
	m := NewModel("/repo", "/home")
	seedTestTerminalSize(&m)
	m.pane = paneToggleModal
	m.modalSkill = r
	m.diagnostics.replaceSnapshot([]tuiDiagnostic{
		snapshotDiagnostic(diagnosticWarning, diagnosticResolution, &resolver.NameCollisionError{
			Kind:    ir.KindSkill,
			Name:    "smith",
			Sources: []string{"global.git", "project.git"},
		}),
	})

	out := renderToggleModal(m)
	if !contains(out, "Name collision") {
		t.Fatalf("expected collision warning in modal output, got:\n%s", out)
	}
}

// TestHasCollisionErrorIsTyped pins typed collision detection: only resolver
// collision types for the exact kind+name qualify. An untyped error whose
// text merely contains the collision phrasing (e.g. a user-authored message
// or a promoted materialize diagnostic) must not arm the modal warning or the
// auto-exclude follow-up, and neither may a collision for a different
// resource or kind.
func TestHasCollisionErrorIsTyped(t *testing.T) {
	lookAlike := fmt.Errorf(`skill "smith" is provided by multiple registries: global.git, project.git`)
	if hasCollisionError([]error{lookAlike}, ir.KindSkill, "smith") {
		t.Fatal("untyped look-alike error must not count as a collision")
	}

	wrapped := fmt.Errorf("sync: %w", &resolver.NameCollisionError{
		Kind:    ir.KindSkill,
		Name:    "smith",
		Sources: []string{"global.git", "project.git"},
	})
	if !hasCollisionError([]error{wrapped}, ir.KindSkill, "smith") {
		t.Fatal("wrapped resolver.NameCollisionError for the same kind+name must count as a collision")
	}

	otherName := &resolver.NameCollisionError{Kind: ir.KindSkill, Name: "jones", Sources: []string{"a", "b"}}
	if hasCollisionError([]error{otherName}, ir.KindSkill, "smith") {
		t.Fatal("collision for a different resource must not count")
	}

	otherKind := &resolver.NameCollisionError{Kind: ir.KindCommand, Name: "smith", Sources: []string{"a", "b"}}
	if hasCollisionError([]error{otherKind}, ir.KindSkill, "smith") {
		t.Fatal("collision for a different kind must not count")
	}

	softWarning := &resolver.NameCollisionWarning{Kind: ir.KindSkill, Name: "smith", Winner: "global.git", RunnersUp: []string{"project.git"}}
	if !hasCollisionError([]error{softWarning}, ir.KindSkill, "smith") {
		t.Fatal("resolver.NameCollisionWarning for the same kind+name must count as a collision")
	}
}

func TestRenderToggleModalNoCollisionWarningWithoutDiagnostic(t *testing.T) {
	r := mkRow("smith", withAvailable, withRegistry("global.git", "global"))
	m := NewModel("/repo", "/home")
	seedTestTerminalSize(&m)
	m.pane = paneToggleModal
	m.modalSkill = r
	// No diagnostics — no collision warning.

	out := renderToggleModal(m)
	if contains(out, "Name collision") {
		t.Fatalf("did not expect collision warning without diagnostic, got:\n%s", out)
	}
}

func TestReadSnapshotCollisionWarningDrivesModalAndAutoExclude(t *testing.T) {
	repoRoot := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	registriesRoot := t.TempDir()
	registryA := filepath.Join(registriesRoot, "registry-a")
	registryB := filepath.Join(registriesRoot, "registry-b")
	for _, registry := range []string{registryA, registryB} {
		skillDir := filepath.Join(registry, "smith")
		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: smith\ndescription: Smith\n---\n# Smith\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	projectPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(projectPath), 0o755); err != nil {
		t.Fatal(err)
	}
	projectConfig := fmt.Sprintf(`apiVersion: agent-marketplace/v3
registries:
  - url: %q
    skills: [smith]
  - url: %q
    skills: [smith]
`, registryA, registryB)
	if err := os.WriteFile(projectPath, []byte(projectConfig), 0o644); err != nil {
		t.Fatal(err)
	}

	userPath := filepath.Join(homeDir, ".agents", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(userPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userPath, []byte("apiVersion: agent-marketplace/v3\nharnesses: [factory]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	restoreDiscover := swapDiscoverSkills(func(dir string) ([]catalog.Entry, []skill.MalformedSkill, error) {
		return []catalog.Entry{{Name: "smith", Kind: ir.KindSkill, Path: filepath.Join(dir, "smith")}}, nil, nil
	})
	defer restoreDiscover()
	restoreOrg := swapLoadOrgConfig(func() (*config.Config, error) { return nil, nil })
	defer restoreOrg()
	restoreGlobal := swapLoadGlobalConfig(func() (*config.Config, error) { return config.LoadConfigFromPath(userPath) })
	defer restoreGlobal()
	restoreRepo := swapLoadRepoConfig(func() (*config.Config, error) { return config.LoadConfigFromPath(projectPath) })
	defer restoreRepo()
	restoreLocal := swapLoadLocalConfig(func() (*config.LocalConfig, error) { return nil, nil })
	defer restoreLocal()

	snapshot := readSnapshot(repoRoot, homeDir, catalogLoadCached)
	foundCollisionWarning := false
	for _, diagnostic := range snapshot.diagnostics {
		if resolver.IsNameCollision(diagnostic.err) {
			foundCollisionWarning = true
			if diagnostic.severity != diagnosticWarning {
				t.Fatalf("readSnapshot collision severity = %v, want warning", diagnostic.severity)
			}
		}
	}
	if !foundCollisionWarning {
		t.Fatalf("readSnapshot diagnostics omitted collision warning: %#v", snapshot.diagnostics)
	}

	m := NewModel(repoRoot, homeDir)
	seedTestTerminalSize(&m)
	snapshot.generation = m.requests.active(requestSnapshot)
	updated, _ := m.Update(snapshot)
	m = updated.(Model)
	foundRow := false
	for i := range m.flatItems {
		item := m.flatItems[i]
		if item.Kind == flatArtifact && item.Row.Artifact.Name == "smith" && item.Row.RegistryURL == registryB {
			if !item.Row.IsAvailable {
				t.Fatalf("registry-b row should be the available collision runner-up: %#v", item.Row)
			}
			m.cursor = i
			foundRow = true
			break
		}
	}
	if !foundRow {
		t.Fatalf("snapshot did not produce registry-b smith row: %#v", m.rows)
	}

	opened, openCmd := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	if openCmd != nil {
		t.Fatal("opening collision modal emitted command")
	}
	m = opened.(Model)
	if !contains(renderToggleModal(m), "Name collision") {
		t.Fatalf("readSnapshot collision warning was not visible in modal:\n%s", renderToggleModal(m))
	}

	acted, operationCmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	m = acted.(Model)
	if operationCmd == nil {
		t.Fatal("collision selection did not emit the toggle operation")
	}
	active := m.operations.active
	if active == nil || active.kind != operationMutation {
		t.Fatalf("collision selection did not start a mutation operation: %#v", active)
	}
	if active.collision == nil || len(active.collision.competing) != 1 || active.collision.competing[0].url != registryA {
		t.Fatalf("operation-owned collision state = %#v, want competing registry-a", active.collision)
	}

	// Primary include and the competing exclude complete as one result.
	result, ok := operationCmd().(configWrittenMsg)
	if !ok || result.err != nil || !result.enabled {
		t.Fatalf("collision toggle result = %#v", result)
	}
	if result.operation != active.id {
		t.Fatalf("collision toggle result operation = %d, want %d", result.operation, active.id)
	}

	completed, reloadCmd := m.Update(result)
	m = completed.(Model)
	if reloadCmd == nil {
		t.Fatal("successful collision toggle omitted the authoritative reload")
	}
	if m.operations.active != nil {
		t.Fatalf("operation still active after its single result: %#v", m.operations.active)
	}

	cfg, err := config.LoadConfigFromPath(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	var competing, selected *config.RegistryEntry
	for i := range cfg.Registries {
		switch cfg.Registries[i].URL {
		case registryA:
			competing = &cfg.Registries[i]
		case registryB:
			selected = &cfg.Registries[i]
		}
	}
	if competing == nil || !containsString(competing.Skills.Exclude, "smith") {
		t.Fatalf("competing registry was not auto-excluded: %#v", competing)
	}
	if selected == nil || !containsString(selected.Skills.Include, "smith") || containsString(selected.Skills.Exclude, "smith") {
		t.Fatalf("selected registry was not preserved as the source: %#v", selected)
	}
}
