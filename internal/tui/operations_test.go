package tui

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/skill"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// beginToggleOperation drives the toggle modal onto the given row and presses
// the project-scope key, returning the model with the started mutation
// operation and the operation command.
func beginToggleOperation(t *testing.T, m Model, r row) (Model, tea.Cmd) {
	t.Helper()
	m.rows = []row{r}
	m.filteredRows = m.rows
	m.rebuildFlatItems()
	for i := range m.flatItems {
		if m.flatItems[i].Kind == flatArtifact {
			m.cursor = i
			break
		}
	}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = updated.(Model)
	if cmd != nil || m.pane != paneToggleModal {
		t.Fatalf("opening toggle modal: pane=%v cmd=%v", m.pane, cmd)
	}
	updated, cmd = m.Update(keyPress('p'))
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("project toggle did not emit an operation command")
	}
	return m, cmd
}

func TestOperationIDsAllocatedAtStartAndCarriedInResult(t *testing.T) {
	t.Run("resource toggle", func(t *testing.T) {
		repoRoot := t.TempDir()
		m := NewModel(repoRoot, t.TempDir())
		seedTestTerminalSize(&m)
		m.loading = false
		r := mkRow("audit", withAvailable, withRegistry("git@example.com:sec.git", "sec"))

		m, cmd := beginToggleOperation(t, m, r)
		active := m.operations.active
		if active == nil || active.id == 0 || active.kind != operationMutation {
			t.Fatalf("toggle did not start a mutation operation: %#v", active)
		}
		msg, ok := cmd().(configWrittenMsg)
		if !ok {
			t.Fatalf("toggle result = %T, want configWrittenMsg", cmd())
		}
		if msg.operation != active.id {
			t.Fatalf("result operation = %d, want started operation %d", msg.operation, active.id)
		}
		if msg.err != nil {
			t.Fatalf("toggle write failed: %v", msg.err)
		}
	})

	t.Run("harness save", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		m := NewModel(t.TempDir(), home)
		seedTestTerminalSize(&m)
		m.userHarnesses = []string{"factory"}
		m.targetHarnesses = []string{"factory"}
		m.targetHarnessSource = config.UserPersonal
		m.openHarnessModal()

		updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m = updated.(Model)
		if cmd == nil {
			t.Fatal("harness save did not emit an operation command")
		}
		active := m.operations.active
		if active == nil || active.id == 0 || active.kind != operationTargets {
			t.Fatalf("harness save did not start a targets operation: %#v", active)
		}
		msg, ok := cmd().(harnessWrittenMsg)
		if !ok {
			t.Fatalf("harness save result = %T, want harnessWrittenMsg", cmd())
		}
		if msg.operation != active.id {
			t.Fatalf("result operation = %d, want started operation %d", msg.operation, active.id)
		}
		if msg.err != nil {
			t.Fatalf("harness save failed: %v", msg.err)
		}
	})

	t.Run("migration", func(t *testing.T) {
		m := NewModel(t.TempDir(), t.TempDir())
		seedTestTerminalSize(&m)
		m.pane = paneMigrationModal
		m.migrationPrompt = migrationPrompt{planErr: errors.New("plan unavailable in test")}

		updated, cmd := m.Update(keyPress('m'))
		m = updated.(Model)
		if cmd == nil {
			t.Fatal("migration run did not emit an operation command")
		}
		active := m.operations.active
		if active == nil || active.id == 0 || active.kind != operationMigration {
			t.Fatalf("migration did not start a migration operation: %#v", active)
		}
		msg, ok := cmd().(migrationDoneMsg)
		if !ok {
			t.Fatalf("migration result = %T, want migrationDoneMsg", cmd())
		}
		if msg.operation != active.id {
			t.Fatalf("result operation = %d, want started operation %d", msg.operation, active.id)
		}
	})

	t.Run("editor", func(t *testing.T) {
		repoRoot := t.TempDir()
		m := NewModel(repoRoot, t.TempDir())
		seedTestTerminalSize(&m)
		m.pane = paneEditConfigModal

		updated, cmd := m.Update(keyPress('l'))
		m = updated.(Model)
		if cmd == nil {
			t.Fatal("editor open did not emit an operation command")
		}
		active := m.operations.active
		if active == nil || active.id == 0 || active.kind != operationEditor {
			t.Fatalf("editor did not start an editor operation: %#v", active)
		}
		// Drive the produced command to completion through a failing config
		// directory so no real editor process is needed.
		t.Setenv("EDITOR", "cat")
		blocker := filepath.Join(repoRoot, ".agents")
		if err := os.WriteFile(blocker, []byte("not a directory"), 0o644); err != nil {
			t.Fatal(err)
		}
		msg, ok := cmd().(editConfigPathMsg)
		if !ok {
			t.Fatalf("editor result = %T, want editConfigPathMsg", cmd())
		}
		if msg.operation != active.id {
			t.Fatalf("result operation = %d, want started operation %d", msg.operation, active.id)
		}
	})
}

// TestSecondOperationStartBlockedWhileActive pins the serialization half of
// the mutation-correlation contract: a resource mutation and a harness save
// cannot overlap in either order.
func TestSecondOperationStartBlockedWhileActive(t *testing.T) {
	newHarnessModalModel := func(t *testing.T) Model {
		t.Helper()
		m := NewModel(t.TempDir(), t.TempDir())
		seedTestTerminalSize(&m)
		m.userHarnesses = []string{"factory"}
		m.targetHarnesses = []string{"factory"}
		m.targetHarnessSource = config.UserPersonal
		return m
	}
	assertBlocked := func(t *testing.T, m Model, msg tea.KeyMsg, wantPane pane, wantID operationID) Model {
		t.Helper()
		updated, cmd := m.Update(msg)
		m = updated.(Model)
		if cmd == nil {
			t.Fatal("blocked operation did not surface a toast command")
		}
		toast, ok := cmd().(toastMsg)
		if !ok {
			t.Fatalf("blocked operation produced %T, want toastMsg", cmd())
		}
		if !strings.Contains(toast.text, "in progress") {
			t.Fatalf("blocked-operation toast = %q", toast.text)
		}
		if m.pane != wantPane {
			t.Fatalf("blocked operation left pane=%v, want %v (modal stays open)", m.pane, wantPane)
		}
		if m.operations.active == nil || m.operations.active.id != wantID {
			t.Fatalf("blocked start replaced active operation: %#v", m.operations.active)
		}
		return m
	}

	t.Run("harness save blocked during resource mutation", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		m := newHarnessModalModel(t)
		m.loading = false
		r := mkRow("audit", withAvailable, withRegistry("git@example.com:sec.git", "sec"))
		m, _ = beginToggleOperation(t, m, r)
		mutationID := m.operations.active.id

		updated, cmd := m.Update(keyPress('H'))
		m = updated.(Model)
		if cmd != nil || m.pane != paneHarnessModal {
			t.Fatalf("harness modal should open during a mutation: pane=%v cmd=%v", m.pane, cmd)
		}
		m = assertBlocked(t, m, tea.KeyMsg{Type: tea.KeyEnter}, paneHarnessModal, mutationID)

		// The blocked save must not have written the user-personal config.
		if _, err := os.Stat(filepath.Join(home, ".agents", "config.yaml")); !os.IsNotExist(err) {
			t.Fatalf("blocked harness save wrote config: %v", err)
		}

		// Once the mutation completes, the harness save proceeds.
		updated, _ = m.Update(configWrittenMsg{operation: mutationID, skillName: "audit", enabled: true})
		m = updated.(Model)
		if m.operations.active != nil {
			t.Fatalf("completed mutation still active: %#v", m.operations.active)
		}
		updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m = updated.(Model)
		msg, ok := cmd().(harnessWrittenMsg)
		if !ok || msg.err != nil {
			t.Fatalf("harness save after mutation completed = %#v", msg)
		}
		if msg.operation == mutationID {
			t.Fatalf("harness save reused mutation operation id %d", mutationID)
		}
	})

	t.Run("resource mutation blocked during harness save", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		m := newHarnessModalModel(t)
		m.loading = false
		m.openHarnessModal()
		updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m = updated.(Model)
		if cmd == nil {
			t.Fatal("harness save did not start")
		}
		targetsID := m.operations.active.id

		r := mkRow("audit", withAvailable, withRegistry("git@example.com:sec.git", "sec"))
		m.rows = []row{r}
		m.filteredRows = m.rows
		m.rebuildFlatItems()
		for i := range m.flatItems {
			if m.flatItems[i].Kind == flatArtifact {
				m.cursor = i
				break
			}
		}
		updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeySpace})
		m = updated.(Model)
		if cmd != nil || m.pane != paneToggleModal {
			t.Fatalf("toggle modal should open during a harness save: pane=%v cmd=%v", m.pane, cmd)
		}
		m = assertBlocked(t, m, keyPress('p'), paneToggleModal, targetsID)

		// The blocked toggle must not have written the project config.
		if _, err := os.Stat(filepath.Join(m.repoRoot, ".agents", "config.yaml")); !os.IsNotExist(err) {
			t.Fatalf("blocked toggle wrote config: %v", err)
		}
	})
}

// TestOperationCompletionRejectedOutOfOrder pins reversed A/B completion: a
// result that does not carry the active operation's ID changes nothing, even
// when it arrives after a newer operation has started.
func TestOperationCompletionRejectedOutOfOrder(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repoRoot := t.TempDir()
	m := NewModel(repoRoot, home)
	seedTestTerminalSize(&m)
	m.loading = false
	m.userHarnesses = []string{"factory"}
	m.targetHarnesses = []string{"factory"}
	m.targetHarnessSource = config.UserPersonal

	// Operation A: resource mutation.
	r := mkRow("audit", withAvailable, withRegistry("git@example.com:sec.git", "sec"))
	m, cmdA := beginToggleOperation(t, m, r)
	idA := m.operations.active.id
	resultA, ok := cmdA().(configWrittenMsg)
	if !ok || resultA.err != nil {
		t.Fatalf("operation A result = %#v", resultA)
	}

	// While A is active a second operation cannot start (harness save).
	m.openHarnessModal()
	blocked, blockedCmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = blocked.(Model)
	if _, ok := blockedCmd().(toastMsg); !ok {
		t.Fatal("harness save was not start-blocked during operation A")
	}
	if m.operations.active == nil || m.operations.active.id != idA {
		t.Fatalf("active operation changed while blocked: %#v", m.operations.active)
	}
	m.pane = paneList

	// A's result arrives late but carries the active ID: accepted.
	updated, cmd := m.Update(resultA)
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("accepted mutation result did not schedule a reload")
	}
	if m.operations.active != nil {
		t.Fatalf("operation A still active after its result: %#v", m.operations.active)
	}
	toastAfterA := m.toast
	if !strings.Contains(toastAfterA, "Enabled") {
		t.Fatalf("operation A success toast = %q", toastAfterA)
	}

	// Operation B starts after A completed.
	m.openHarnessModal()
	updated, cmdB := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	idB := m.operations.active.id
	resultB, ok := cmdB().(harnessWrittenMsg)
	if !ok || resultB.err != nil {
		t.Fatalf("operation B result = %#v", resultB)
	}

	// Reversed completion: A's duplicated result arrives while B is active.
	// It must be rejected without touching toast, diagnostics, or the active
	// operation.
	updated, cmd = m.Update(resultA)
	m = updated.(Model)
	if cmd != nil {
		t.Fatal("stale A result scheduled work")
	}
	if m.toast != toastAfterA {
		t.Fatalf("stale A result changed toast %q -> %q", toastAfterA, m.toast)
	}
	if got := m.diagnostics.count(diagnosticError); got != 0 {
		t.Fatalf("stale A result recorded %d diagnostics", got)
	}
	if m.operations.active == nil || m.operations.active.id != idB {
		t.Fatalf("stale A result disturbed active operation B: %#v", m.operations.active)
	}

	// B's own result is accepted.
	updated, cmd = m.Update(resultB)
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("accepted harness result did not schedule a reload")
	}
	if m.operations.active != nil {
		t.Fatalf("operation B still active after its result: %#v", m.operations.active)
	}
	if !strings.Contains(m.toast, "Saved") {
		t.Fatalf("operation B success toast = %q", m.toast)
	}
}

// TestCollisionEnableCompletesAsOneResult drives a colliding enable with TWO
// competing registries: the primary include and both competing excludes are
// written inside one operation and complete as one result.
func TestCollisionEnableCompletesAsOneResult(t *testing.T) {
	repoRoot := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	registriesRoot := t.TempDir()
	var registryURLs []string
	for _, name := range []string{"registry-a", "registry-b", "registry-c"} {
		registry := filepath.Join(registriesRoot, name)
		registryURLs = append(registryURLs, registry)
		skillDir := filepath.Join(registry, "smith")
		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: smith\ndescription: Smith\n---\n# Smith\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	registryA, registryB, registryC := registryURLs[0], registryURLs[1], registryURLs[2]

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
  - url: %q
    skills: [smith]
`, registryA, registryB, registryC)
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

	m := NewModel(repoRoot, homeDir)
	seedTestTerminalSize(&m)
	snapshot := readSnapshot(repoRoot, homeDir, catalogLoadCached)
	snapshot.generation = m.requests.active(requestSnapshot)
	updated, _ := m.Update(snapshot)
	m = updated.(Model)

	// The collision warning from diagnostics is what drives both the modal
	// warning and the operation-owned follow-up.
	if !hasCollisionError(m.diagnostics.values(), ir.KindSkill, "smith") {
		t.Fatalf("accepted snapshot lost the collision warning: %#v", m.diagnostics.all())
	}

	// Enable the registry-B runner-up at project scope.
	foundRow := false
	for i := range m.flatItems {
		item := m.flatItems[i]
		if item.Kind == flatArtifact && item.Row.Artifact.Name == "smith" && item.Row.RegistryURL == registryB {
			m.cursor = i
			foundRow = true
			break
		}
	}
	if !foundRow {
		t.Fatalf("snapshot did not produce registry-b smith row: %#v", m.rows)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = updated.(Model)
	if !contains(renderToggleModal(m), "Name collision") {
		t.Fatalf("collision warning missing from modal:\n%s", renderToggleModal(m))
	}

	updated, cmd := m.Update(keyPress('p'))
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("collision enable did not emit an operation command")
	}
	active := m.operations.active
	if active == nil || active.kind != operationMutation {
		t.Fatalf("collision enable did not start a mutation operation: %#v", active)
	}
	if active.collision == nil {
		t.Fatal("collision state is not owned by the operation")
	}
	if len(active.collision.competing) != 2 {
		t.Fatalf("collision competing set = %#v, want registries A and C", active.collision.competing)
	}

	// One command, one result: primary include + both competing excludes.
	result, ok := cmd().(configWrittenMsg)
	if !ok {
		t.Fatalf("collision enable result = %T, want configWrittenMsg", cmd())
	}
	if result.err != nil || !result.enabled {
		t.Fatalf("collision enable result = %#v", result)
	}
	if result.operation != active.id {
		t.Fatalf("result operation = %d, want %d", result.operation, active.id)
	}

	cfg, err := config.LoadConfigFromPath(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	entries := map[string]*config.RegistryEntry{}
	for i := range cfg.Registries {
		entries[cfg.Registries[i].URL] = &cfg.Registries[i]
	}
	for _, url := range []string{registryA, registryC} {
		entry := entries[url]
		if entry == nil || !containsString(entry.Skills.Exclude, "smith") {
			t.Fatalf("competing registry %q was not excluded in the same result: %#v", url, entry)
		}
	}
	selected := entries[registryB]
	if selected == nil || !containsString(selected.Skills.Include, "smith") || containsString(selected.Skills.Exclude, "smith") {
		t.Fatalf("selected registry-b was not preserved as the source: %#v", selected)
	}

	// Delivering the single result schedules a reload and nothing that could
	// produce a second configWrittenMsg (the old pendingExcludeAction flow).
	updated, followUp := m.Update(result)
	m = updated.(Model)
	if m.operations.active != nil {
		t.Fatalf("operation still active after its single result: %#v", m.operations.active)
	}
	batch := batchCmds(followUp)
	if len(batch) != 2 {
		t.Fatalf("accepted collision result = %d commands, want exactly reload + toast expiry", len(batch))
	}
	if _, ok := batch[0]().(snapshotLoadedMsg); !ok {
		t.Fatalf("accepted collision result batch[0] did not reload the snapshot")
	}
	if msg := batch[1](); msg != (toastExpiredMsg{}) {
		t.Fatalf("accepted collision result batch[1] = %T, want toast expiry (no second write)", msg)
	}
}

// TestFailedCollisionFollowUpSchedulesReloadAndRetainsDiagnostic pins the
// failure half: when the competing-exclude step fails, the operation
// completes as one failed result, an authoritative reload is scheduled, and
// only that operation's diagnostic is retained.
func TestFailedCollisionFollowUpSchedulesReloadAndRetainsDiagnostic(t *testing.T) {
	repoRoot := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	agentsDir := filepath.Join(repoRoot, ".agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	projectPath := filepath.Join(agentsDir, "config.yaml")
	if err := os.WriteFile(projectPath, []byte("apiVersion: agent-marketplace/v3\nregistries: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := NewModel(repoRoot, t.TempDir())
	seedTestTerminalSize(&m)
	m.loading = false

	// A retained failure from an earlier attempt of the same operation kind
	// must be replaced by the new operation's diagnostic.
	earlierErr := errors.New("earlier mutation failure")
	m.diagnostics.recordOperationAttemptFailure(operationMutation, diagnosticMutation, 999, earlierErr)

	act := toggleAction{
		skillName:   "smith",
		kind:        ir.KindSkill,
		scope:       scopeProject,
		enable:      true,
		registryURL: "git@example.com:selected.git",
	}
	// An empty competing URL cannot be produced by newCollisionFollowUp (it
	// filters empty URLs); it is used here to fail the follow-up write
	// deterministically after the primary include succeeds.
	collision := &collisionFollowUp{competing: []competingRegistry{{url: ""}}}
	id, ok := m.beginOperation(operationMutation, collision)
	if !ok {
		t.Fatal("mutation operation did not start")
	}
	result, ok := runScopedToggle(repoRoot, act, id, collision)().(configWrittenMsg)
	if !ok {
		t.Fatal("runScopedToggle did not return configWrittenMsg")
	}
	if result.err == nil {
		t.Fatal("expected the competing-exclude step to fail")
	}
	if result.operation != id {
		t.Fatalf("result operation = %d, want %d", result.operation, id)
	}

	updated, cmd := m.Update(result)
	m = updated.(Model)
	if m.operations.active != nil {
		t.Fatalf("failed operation still active: %#v", m.operations.active)
	}
	if !strings.Contains(m.toast, "error:") {
		t.Fatalf("failure toast = %q", m.toast)
	}

	// Failure schedules an authoritative reload.
	batch := batchCmds(cmd)
	if batch == nil {
		t.Fatalf("failure result = %T, want a batch with reload", cmd)
	}
	if _, ok := batch[0]().(snapshotLoadedMsg); !ok {
		t.Fatal("failure batch[0] did not schedule an authoritative snapshot reload")
	}

	// Only this operation's diagnostic is retained for the kind.
	operationDiagnostics := m.diagnostics.operations
	if len(operationDiagnostics) != 1 {
		t.Fatalf("retained %d operation diagnostics, want 1: %#v", len(operationDiagnostics), operationDiagnostics)
	}
	retained := operationDiagnostics[0]
	if errors.Is(retained.err, earlierErr) {
		t.Fatalf("earlier attempt diagnostic was retained alongside the new failure: %#v", retained)
	}
	if retained.operation != operationMutation || retained.attempt != uint64(id) {
		t.Fatalf("retained diagnostic = operation %q attempt %d, want mutation attempt %d", retained.operation, retained.attempt, id)
	}

	// A later successful mutation clears the retained failure.
	nextID, ok := m.beginOperation(operationMutation, nil)
	if !ok {
		t.Fatal("follow-up mutation did not start")
	}
	updated, _ = m.Update(configWrittenMsg{operation: nextID, skillName: "smith", enabled: false})
	m = updated.(Model)
	if got := m.diagnostics.count(diagnosticError); got != 0 {
		t.Fatalf("successful retry left %d operation failures", got)
	}
}

// TestStaleOperationResultsAreRejected pins stale result rejection for every
// ID-correlated result type: nothing about the model may change.
func TestStaleOperationResultsAreRejected(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&output, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	raw := "raw-untrusted-failure-marker"
	cases := []struct {
		name string
		kind operationKind
		msg  func(id operationID) tea.Msg
	}{
		{"mutation", operationMutation, func(id operationID) tea.Msg {
			return configWrittenMsg{operation: id, skillName: "audit", enabled: true, err: errors.New(raw)}
		}},
		{"targets", operationTargets, func(id operationID) tea.Msg {
			return harnessWrittenMsg{operation: id, scopeLabel: "user-personal", err: errors.New(raw)}
		}},
		{"migration", operationMigration, func(id operationID) tea.Msg {
			return migrationDoneMsg{operation: id, count: 1, err: errors.New(raw)}
		}},
		{"editor", operationEditor, func(id operationID) tea.Msg {
			return editConfigPathMsg{operation: id, path: "/repo/.agents/config.yaml", err: errors.New(raw)}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name+"/never started", func(t *testing.T) {
			m := NewModel(t.TempDir(), t.TempDir())
			seedTestTerminalSize(&m)
			updated, cmd := m.Update(tc.msg(0))
			m = updated.(Model)
			if cmd != nil {
				t.Fatal("result for operation 0 scheduled work")
			}
			if m.toast != "" || m.diagnostics.count(diagnosticError) != 0 {
				t.Fatalf("result for operation 0 changed state: toast=%q diagnostics=%v", m.toast, m.diagnostics.all())
			}
		})
		t.Run(tc.name+"/not active", func(t *testing.T) {
			m := NewModel(t.TempDir(), t.TempDir())
			seedTestTerminalSize(&m)
			id, ok := m.beginOperation(tc.kind, nil)
			if !ok {
				t.Fatal("operation did not start")
			}
			updated, cmd := m.Update(tc.msg(id + 99))
			m = updated.(Model)
			if cmd != nil {
				t.Fatal("stale result scheduled work")
			}
			if m.toast != "" || m.diagnostics.count(diagnosticError) != 0 {
				t.Fatalf("stale result changed state: toast=%q diagnostics=%v", m.toast, m.diagnostics.all())
			}
			if m.operations.active == nil || m.operations.active.id != id {
				t.Fatalf("stale result disturbed the active operation: %#v", m.operations.active)
			}
			// Control: the operation's own result is accepted.
			updated, cmd = m.Update(tc.msg(id))
			m = updated.(Model)
			if cmd == nil {
				t.Fatal("the operation's own result was rejected")
			}
		})
	}

	logs := output.String()
	if !strings.Contains(logs, "stale operation completion rejected") {
		t.Fatalf("stale rejections were not logged:\n%s", logs)
	}
	if strings.Contains(logs, raw) {
		t.Fatalf("stale rejection logs leaked untrusted diagnostic content:\n%s", logs)
	}
}

// TestOperationAttemptDiagnosticsRetention pins the store-level lifecycle:
// a failure retains only that operation's diagnostic for the kind, and a
// success clears only failures from the same or an earlier attempt.
func TestOperationAttemptDiagnosticsRetention(t *testing.T) {
	var store diagnosticStore
	store.recordOperationAttemptFailure(operationMutation, diagnosticMutation, 5, errors.New("attempt 5"))
	if got := store.count(diagnosticError); got != 1 {
		t.Fatalf("attempt 5 failure recorded %d diagnostics", got)
	}

	// A stale success must not clear a newer attempt's retained failure.
	store.recordOperationAttemptSuccess(operationMutation, 3)
	if got := store.count(diagnosticError); got != 1 {
		t.Fatalf("stale success cleared attempt 5 failure")
	}

	// Same-attempt success clears.
	store.recordOperationAttemptSuccess(operationMutation, 5)
	if got := store.count(diagnosticError); got != 0 {
		t.Fatalf("same-attempt success did not clear attempt 5 failure")
	}

	// A new failure retains only its own diagnostic for the kind.
	store.recordOperationAttemptFailure(operationMutation, diagnosticMutation, 6, errors.New("attempt 6"))
	store.recordOperationAttemptFailure(operationMutation, diagnosticMutation, 8, errors.New("attempt 8"))
	if got := store.count(diagnosticError); got != 1 {
		t.Fatalf("failure replacement retained %d diagnostics, want 1", got)
	}
	if store.operations[0].attempt != 8 {
		t.Fatalf("retained attempt = %d, want 8", store.operations[0].attempt)
	}

	// A later success supersedes the retained failure.
	store.recordOperationAttemptSuccess(operationMutation, 9)
	if got := store.count(diagnosticError); got != 0 {
		t.Fatalf("later success did not clear the retained failure")
	}
}

// TestSyncStartDoesNotAllocateSnapshotGeneration pins the phantom-generation
// removal: pressing 's' schedules sync work, not snapshot work, so the
// snapshot generation must stay put. The syncDoneMsg handler allocates the
// authoritative post-sync reload generation itself.
func TestSyncStartDoesNotAllocateSnapshotGeneration(t *testing.T) {
	origRunSyncCmd := runSyncCmd
	runSyncCmd = noopSyncCmd
	t.Cleanup(func() { runSyncCmd = origRunSyncCmd })

	m := newTestModel()
	m.loading = false
	m.driftAssessment = driftReady
	m.driftReport = materialize.DriftReport{Missing: []materialize.DriftItem{{Name: "a", Reason: materialize.DriftMissing}}}
	before := m.requests.active(requestSnapshot)

	updated, cmd := m.Update(keyPress('s'))
	m = updated.(Model)
	if cmd == nil || !m.syncing {
		t.Fatalf("sync did not start: syncing=%t cmd=%v", m.syncing, cmd)
	}
	if got := m.requests.active(requestSnapshot); got != before {
		t.Fatalf("sync start allocated phantom snapshot generation %d, want unchanged %d", got, before)
	}
}

// TestNewCollisionFollowUpCapturesRegistryMetadataAtStart pins that the
// collision follow-up is computed from the rows at operation start, carrying
// each competing registry's ref/path identity.
func TestNewCollisionFollowUpCapturesRegistryMetadataAtStart(t *testing.T) {
	rows := []row{
		{Artifact: resolver.ResolvedArtifact{Entry: catalog.Entry{Name: "smith", Kind: ir.KindSkill}}, RegistryURL: "git@example.com:selected.git"},
		{Artifact: resolver.ResolvedArtifact{Entry: catalog.Entry{Name: "smith", Kind: ir.KindSkill}}, RegistryURL: "git@example.com:competing.git", RegistryRef: "main", RegistryPath: "skills"},
		{Artifact: resolver.ResolvedArtifact{Entry: catalog.Entry{Name: "smith", Kind: ir.KindSkill}}, RegistryURL: "git@example.com:competing.git", RegistryRef: "main", RegistryPath: "skills"},
		{Artifact: resolver.ResolvedArtifact{Entry: catalog.Entry{Name: "smith", Kind: ir.KindCommand}}, RegistryURL: "git@example.com:other-kind.git"},
	}
	act := toggleAction{skillName: "smith", kind: ir.KindSkill, scope: scopeProject, enable: true, registryURL: "git@example.com:selected.git"}

	followUp := newCollisionFollowUp(act, rows)
	if followUp == nil || len(followUp.competing) != 1 {
		t.Fatalf("collision follow-up = %#v, want one competing registry", followUp)
	}
	competing := followUp.competing[0]
	if competing.url != "git@example.com:competing.git" || competing.ref != "main" || competing.path != "skills" {
		t.Fatalf("competing registry identity = %#v", competing)
	}

	if followUp := newCollisionFollowUp(act, nil); followUp != nil {
		t.Fatalf("no competing rows should produce no collision state, got %#v", followUp)
	}
}
