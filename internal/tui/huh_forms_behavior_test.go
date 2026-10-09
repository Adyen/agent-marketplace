package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/localapi"
	"github.com/Adyen/agent-marketplace/internal/session"
)

// AC4: scope switching and Space selection inside the harness Huh
// multiselect never write config; only Enter submits a non-empty
// selection through the guarded save.

// spec: session-interactions/toggling-harness-targets-does-not-write
// spec: tui-interactions/scope-and-selection-without-write
func TestHarnessFormSpaceSelectsWithoutWriting(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	configPath := filepath.Join(tmpHome, ".agents", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	original := []byte("apiVersion: agent-marketplace/v4\nharnesses:\n  - factory\nsources: {}\nresources: {}\n")
	if err := os.WriteFile(configPath, original, 0o644); err != nil {
		t.Fatal(err)
	}

	m := NewModel("/repo", tmpHome)
	m.globalHarnesses = []string{"factory"}
	m.openHarnessModal()
	if m.harnessModal.form == nil {
		t.Fatal("harness modal must own a Huh form")
	}

	mi, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = mi.(Model)
	if cmd != nil {
		t.Fatal("cursor movement must not emit a write command")
	}
	mi, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	m = mi.(Model)
	if cmd != nil {
		t.Fatal("space selection must not emit a write command")
	}
	if !m.harnessModal.selected["claude"] {
		t.Fatalf("space should select claude in the form, got %#v", m.harnessModal.selected)
	}

	mi, cmd = m.Update(tea.KeyPressMsg{Code: 'p', Text: "p"})
	m = mi.(Model)
	if cmd != nil {
		t.Fatal("scope switch must not emit a write command")
	}
	if m.harnessModal.scope != harnessScopeProject {
		t.Fatal("'p' should switch the form to project scope")
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(original) {
		t.Fatalf("config written during form interaction:\n%s", data)
	}
}

// spec: session-interactions/save-selected-harnesses
func TestHarnessFormPreservesUnknownIDs(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	configPath := filepath.Join(tmpHome, ".agents", "config.yaml")
	original := []byte("apiVersion: agent-marketplace/v4\nharnesses:\n  - factory\n  - mystery-harness\nsources:\n  agents:\n    git: https://example.test/agents.git\n    ref: main\nresources:\n  agents:\n    include:\n      - skill/review\n")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, original, 0o644); err != nil {
		t.Fatal(err)
	}

	m := NewModel("/repo", tmpHome)
	m.globalHarnesses = []string{"factory", "mystery-harness"}
	m.openHarnessModal()
	if m.harnessModal.form == nil {
		t.Fatal("harness modal must own a Huh form")
	}

	// Toggle factory off through the form; the unknown ID must survive.
	mi, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	m = mi.(Model)
	if cmd != nil {
		t.Fatal("space deselect must not emit a write command")
	}
	mi, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = mi.(Model)
	if cmd == nil {
		t.Fatal("enter on a non-empty selection must emit the save command")
	}
	msg := cmd()
	hw, ok := msg.(harnessWrittenMsg)
	if !ok {
		t.Fatalf("expected harnessWrittenMsg, got %T", msg)
	}
	if hw.err != nil {
		t.Fatalf("harness save rejected an already-present identifier: %v", hw.err)
	}

	parityPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(parityPath, original, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := config.SetHarnessesInConfig(parityPath, []string{"mystery-harness"}); err != nil {
		t.Fatalf("shared writer rejected the same selection: %v", err)
	}
	want, err := os.ReadFile(parityPath)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("TUI save diverged from the shared writer:\nTUI:\n%s\nshared:\n%s", got, want)
	}
	cfg, err := config.LoadConfigFromPath(configPath)
	if err != nil {
		t.Fatalf("reload after save: %v", err)
	}
	if !slices.Equal(cfg.Harnesses, []string{"mystery-harness"}) {
		t.Fatalf("harnesses after save = %v, want [mystery-harness]", cfg.Harnesses)
	}
}

// spec: gui-configuration/harness-target-edit-matches-the-cli-mutation
// spec: session-interactions/save-selected-harnesses
func TestHarnessFormSaveMatchesGUI(t *testing.T) {
	shared := t.TempDir()
	t.Setenv("TMPDIR", t.TempDir())
	keepDir := filepath.Join(shared, "keep-source")
	fragDir := filepath.Join(shared, "frag-source")
	for _, dir := range []struct{ root, skill string }{
		{keepDir, "solo"},
		{keepDir, "extra"},
		{fragDir, "frag"},
	} {
		skillDir := filepath.Join(dir.root, dir.skill)
		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: "+dir.skill+"\ndescription: Fixture skill\n---\n# "+dir.skill+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	userRoot := "apiVersion: agent-marketplace/v4\ninclude: fragment.yaml\nharnesses: [factory, future-harness]\n" +
		"logging: {level: debug}\nsources:\n  keep: {directory: " + keepDir + "}\n" +
		"resources:\n  keep: {include: [skill/solo, skill/extra], exclude: [skill/retired], harnesses: [factory]}\n"
	fragment := "sources:\n  frag: {directory: " + fragDir + "}\nresources:\n  frag: {all: true, harnesses: [claude]}\n"
	orgPath := filepath.Join(t.TempDir(), "org.yaml")
	orgBytes := []byte("apiVersion: agent-marketplace/v4\nharnesses: [claude]\ntelemetry: {level: off}\n")
	if err := os.WriteFile(orgPath, orgBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	config.SetTestOrganizationConfigPath(t, orgPath)

	setHome := func(home string) {
		t.Setenv("HOME", home)
		t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".xdg", "config"))
		t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".xdg", "state"))
		t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".xdg", "cache"))
	}
	writeHome := func(home string) (configPath, fragmentPath string) {
		t.Helper()
		configPath = filepath.Join(home, ".agents", "config.yaml")
		fragmentPath = filepath.Join(home, ".agents", "fragment.yaml")
		if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(configPath, []byte(userRoot), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fragmentPath, []byte(fragment), 0o644); err != nil {
			t.Fatal(err)
		}
		return configPath, fragmentPath
	}

	repoRoot := t.TempDir()
	repoConfigPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	repoLocalPath := filepath.Join(repoRoot, ".agents", "config.local.yaml")
	repoBefore := []byte("apiVersion: agent-marketplace/v4\nharnesses: [claude]\n")
	localBefore := []byte("apiVersion: agent-marketplace/v4\n")
	if err := os.MkdirAll(filepath.Dir(repoConfigPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(repoConfigPath, repoBefore, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(repoLocalPath, localBefore, 0o644); err != nil {
		t.Fatal(err)
	}

	tuiHome := t.TempDir()
	setHome(tuiHome)
	tuiConfigPath, tuiFragmentPath := writeHome(tuiHome)
	m := NewModel(repoRoot, tuiHome)
	startup := loadSnapshot(context.Background(), m.workspace, m.homeDir, m.requests.Active(requestSnapshot))
	mi, cmd := m.Update(startup())
	m = mi.(Model)
	_ = cmd
	if m.loading {
		t.Fatal("startup snapshot did not finish loading")
	}
	if !slices.Equal(m.globalHarnesses, []string{"factory", "future-harness"}) {
		t.Fatalf("startup globalHarnesses = %v, want [factory future-harness]", m.globalHarnesses)
	}

	mi, cmd = m.Update(tea.KeyPressMsg{Code: 'H', Text: "H"})
	m = mi.(Model)
	if cmd != nil || m.pane != paneHarnessModal {
		t.Fatalf("'H' should open the harness modal without a command: pane=%v", m.pane)
	}
	mi, cmd = m.Update(tea.KeyPressMsg{Code: 'g', Text: "g"})
	m = mi.(Model)
	if cmd != nil || m.harnessModal.scope != harnessScopeGlobal {
		t.Fatalf("'g' should select global scope without a command: scope=%v", m.harnessModal.scope)
	}

	moveTo := func(id string) {
		t.Helper()
		target := slices.Index(m.harnessModal.optionIDs, id)
		if target < 0 {
			t.Fatalf("harness option %q absent from %v", id, m.harnessModal.optionIDs)
		}
		for step := 0; m.harnessModal.cursor != target; step++ {
			if step >= len(m.harnessModal.optionIDs) {
				t.Fatalf("cursor stuck at %d moving toward %q in %v", m.harnessModal.cursor, id, m.harnessModal.optionIDs)
			}
			code := tea.KeyDown
			if m.harnessModal.cursor > target {
				code = tea.KeyUp
			}
			before := m.harnessModal.cursor
			updated, cmd := m.Update(tea.KeyPressMsg{Code: code})
			m = updated.(Model)
			if cmd != nil {
				t.Fatal("option navigation emitted a write command")
			}
			if m.harnessModal.cursor == before {
				t.Fatalf("keypress did not move the cursor off %d toward %q", before, id)
			}
		}
	}
	moveTo("factory")
	mi, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	m = mi.(Model)
	if cmd != nil {
		t.Fatal("toggling factory off emitted a write command")
	}
	moveTo("claude")
	mi, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	m = mi.(Model)
	if cmd != nil {
		t.Fatal("toggling claude on emitted a write command")
	}
	if before, err := os.ReadFile(tuiConfigPath); err != nil || string(before) != userRoot {
		t.Fatalf("form interaction wrote the root (err=%v):\n%s", err, before)
	}

	mi, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = mi.(Model)
	if cmd == nil {
		t.Fatal("enter on a non-empty selection must emit the save command")
	}
	saveMsg := cmd()
	hw, ok := saveMsg.(harnessWrittenMsg)
	if !ok {
		t.Fatalf("expected harnessWrittenMsg, got %T", saveMsg)
	}
	if hw.err != nil {
		t.Fatalf("harness save failed: %v", hw.err)
	}
	if m.operations.Active == nil || hw.operation != m.operations.Active.ID {
		t.Fatalf("save completion is not correlated to the active operation: %#v", hw.operation)
	}
	mi, cmd = m.Update(hw)
	m = mi.(Model)
	if m.operations.Active != nil {
		t.Fatal("targets operation did not complete after its written message")
	}
	if cmd == nil {
		t.Fatal("written targets did not schedule the authoritative snapshot reload")
	}
	emitted := cmd()
	batch, ok := emitted.(tea.BatchMsg)
	if !ok {
		t.Fatalf("written targets emitted %T, want tea.BatchMsg with the reload", emitted)
	}
	reloaded := false
	for _, child := range batch {
		if child == nil {
			continue
		}
		if msg, ok := child().(snapshotLoadedMsg); ok {
			reloaded = true
			mi, _ = m.Update(msg)
			m = mi.(Model)
		}
	}
	if !reloaded {
		t.Fatal("emitted batch contained no loadSnapshot command")
	}
	if !slices.Equal(m.globalHarnesses, []string{"claude", "future-harness"}) {
		t.Fatalf("reloaded globalHarnesses = %v, want [claude future-harness]", m.globalHarnesses)
	}
	if m.driftAssessment == driftFailed {
		t.Fatalf("authoritative reload reported failed drift: %+v", m.errs)
	}
	if errs := diagnosticErrors(m.diagnostics.All()); len(errs) > 0 {
		t.Fatalf("authoritative reload surfaced error diagnostics: %v", errs)
	}
	tuiBytes, err := os.ReadFile(tuiConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadConfigFromPath(tuiConfigPath)
	if err != nil {
		t.Fatalf("reload after save: %v", err)
	}
	if !slices.Equal(cfg.Harnesses, []string{"claude", "future-harness"}) {
		t.Fatalf("harnesses after save = %v, want [claude future-harness]", cfg.Harnesses)
	}

	guiHome := t.TempDir()
	setHome(guiHome)
	guiConfigPath, guiFragmentPath := writeHome(guiHome)
	guiLocalPath := filepath.Join(guiHome, ".agents", "config.local.yaml")
	if err := os.WriteFile(guiLocalPath, localBefore, 0o644); err != nil {
		t.Fatal(err)
	}
	api, err := localapi.NewUser(context.Background(), guiHome, session.RuntimeLifecycle(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer api.Close()
	server := httptest.NewServer(api.Handler())
	defer server.Close()

	post := func(path, body string) (int, map[string]any) {
		t.Helper()
		req, err := http.NewRequest(http.MethodPost, server.URL+path, bytes.NewReader([]byte(body)))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("X-Agent-Marketplace-Token", api.Credential())
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		var decoded map[string]any
		if len(data) > 0 {
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("%s body is not JSON: %q", path, data)
			}
		}
		return resp.StatusCode, decoded
	}

	deadline := time.Now().Add(60 * time.Second)
	for {
		status, snapshot := post("/api/snapshot", "")
		if status == http.StatusOK && snapshot["stage"] == "final" && snapshot["operation"] == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("GUI startup snapshot did not settle: %v", snapshot)
		}
		time.Sleep(10 * time.Millisecond)
	}
	status, accepted := post("/api/configuration/actions", `{"action":"save-targets","targets":["claude","future-harness"]}`)
	if status != http.StatusAccepted {
		t.Fatalf("GUI save-targets status = %d, body = %v", status, accepted)
	}
	operation, ok := accepted["operation"].(map[string]any)
	if !ok || operation["kind"] != "targets" {
		t.Fatalf("GUI accepted operation = %v, want targets", accepted["operation"])
	}
	deadline = time.Now().Add(60 * time.Second)
	for {
		status, snapshot := post("/api/snapshot", "")
		if status != http.StatusOK {
			t.Fatalf("GUI completion snapshot status = %d", status)
		}
		settled, _ := snapshot["settledOperation"].(map[string]any)
		if snapshot["stage"] == "final" && snapshot["operation"] == nil &&
			settled != nil && settled["id"] == operation["id"] && settled["kind"] == "targets" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("GUI targets operation did not settle: %v", snapshot)
		}
		time.Sleep(10 * time.Millisecond)
	}
	guiBytes, err := os.ReadFile(guiConfigPath)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(guiBytes, tuiBytes) {
		t.Fatalf("GUI and TUI user roots differ:\nGUI:\n%s\nTUI:\n%s", guiBytes, tuiBytes)
	}
	for _, literal := range []string{
		"harnesses: [claude, future-harness]",
		"include: fragment.yaml",
		"logging: {level: debug}",
		"keep: {include: [skill/solo, skill/extra], exclude: [skill/retired], harnesses: [factory]}",
	} {
		if !bytes.Contains(guiBytes, []byte(literal)) {
			t.Fatalf("saved root lost %q:\n%s", literal, guiBytes)
		}
	}
	for _, check := range []struct {
		path   string
		before []byte
	}{
		{tuiFragmentPath, []byte(fragment)},
		{guiFragmentPath, []byte(fragment)},
		{orgPath, orgBytes},
		{repoConfigPath, repoBefore},
		{repoLocalPath, localBefore},
		{guiLocalPath, localBefore},
	} {
		if after, err := os.ReadFile(check.path); err != nil || !bytes.Equal(after, check.before) {
			t.Fatalf("%s changed (err=%v)", check.path, err)
		}
	}
	for _, home := range []string{tuiHome, guiHome} {
		if _, err := os.Lstat(filepath.Join(home, ".agents", "lock.yaml")); !os.IsNotExist(err) {
			t.Fatalf("targets save created a lock under %s (err=%v)", home, err)
		}
		for _, native := range []string{".codex", ".xdg"} {
			if _, err := os.Lstat(filepath.Join(home, native)); !os.IsNotExist(err) {
				t.Fatalf("targets save wrote %s under %s (err=%v)", native, home, err)
			}
		}
	}
}

// spec: session-interactions/cancel-or-stale-completion
// spec: tui-interactions/cancel-or-stale-completion
func TestHarnessFormEscCancelsWithoutWrite(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.globalHarnesses = []string{"factory"}
	m.openHarnessModal()
	mi, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = mi.(Model)
	if m.pane != paneList {
		t.Fatal("esc should cancel the harness form")
	}
	if cmd != nil {
		t.Fatal("cancel must not emit a write command")
	}
}

func TestToggleFormEnterDispatchesHighlightedAction(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.pane = paneToggleModal
	m.modalSkill = mkRow("audit")
	m.ensureToggleForm()
	mi, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = mi.(Model)
	if cmd == nil {
		t.Fatal("enter on the toggle form should dispatch the highlighted action")
	}
	if m.pane != paneList {
		t.Fatalf("toggle action should close the modal, got pane=%v", m.pane)
	}
}

func TestEditConfigFormEnterDispatchesProjectEditor(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.pane = paneEditConfigModal
	m.ensureEditConfigForm()
	// Move highlight to project, then submit through the form.
	mi, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = mi.(Model)
	mi, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = mi.(Model)
	if cmd == nil {
		t.Fatal("enter on the edit-config form should dispatch the editor")
	}
	if m.pane != paneList {
		t.Fatalf("editor dispatch should close the modal, got pane=%v", m.pane)
	}
}

func TestSyncQuitFormConfirmAndDecline(t *testing.T) {
	m := NewModel("/repo", "/home")
	m.pane = paneConfirm
	m.confirmMsg = "1 pending changes not synced. Sync+quit? y/n/esc"
	m.ensureSyncQuitForm()
	mi, _ := m.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	m = mi.(Model)
	if m.pane != paneList {
		t.Fatal("'n' through the confirm form should close the pane")
	}
	if m.quitAfterSync {
		t.Fatal("declined sync+quit must not arm quitAfterSync")
	}
}

// spec: session-interactions/cancel-or-stale-completion
// spec: tui-interactions/cancel-or-stale-completion
func TestSyncQuitFormResetsAfterCompletion(t *testing.T) {
	origRunSyncCmd := runSyncCmd
	runSyncCmd = noopSyncCmd
	defer func() { runSyncCmd = origRunSyncCmd }()

	m := newTestModel()
	m.pane = paneConfirm
	mi, _ := m.Update(keyPress('y'))
	m = mi.(Model)
	if !m.quitAfterSync {
		t.Fatal("'y' through the confirm form should arm sync+quit")
	}

	m.pane = paneConfirm
	m.syncing = false
	m.quitAfterSync = false
	m.confirmMsg = "2 pending changes not synced. Sync+quit? y/n/esc"
	mi, cmd := m.Update(keyPress('n'))
	m = mi.(Model)
	if m.syncing || m.quitAfterSync {
		t.Fatalf("'n' on a re-presented confirm must not trigger sync+quit: syncing=%t quitAfterSync=%t", m.syncing, m.quitAfterSync)
	}
	if !cmdProducesQuitMsg(cmd) {
		t.Fatal("'n' on a re-presented confirm should quit without syncing")
	}
}

const fixedChoiceSlashChildEnv = "AGENT_MARKETPLACE_TUI_FIXED_CHOICE_SLASH_CHILD"

// spec: tui-interactions/slash-leaves-fixed-choice-dialogs-unchanged
func TestFixedChoiceSlashIsIgnoredAtModelBoundary(t *testing.T) {
	if os.Getenv(fixedChoiceSlashChildEnv) == "1" {
		runFixedChoiceSlashScenarios(t)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	proc := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestFixedChoiceSlashIsIgnoredAtModelBoundary$")
	proc.Env = append(os.Environ(), fixedChoiceSlashChildEnv+"=1")
	output, err := proc.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("fixed-choice slash handling exceeded its bound: %v\n%s", ctx.Err(), output)
	}
	if err != nil {
		t.Fatalf("fixed-choice child failed: %v\n%s", err, output)
	}
}

func runFixedChoiceSlashScenarios(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)

	toggle := NewModel(t.TempDir(), home)
	toggle.loading = false
	toggle.pane = paneToggleModal
	toggle.modalSkill = mkRow("review", withProject)
	toggle.ensureToggleForm()
	if toggle.toggleForm == nil || toggle.toggleChoice == nil {
		t.Fatal("toggle modal did not initialize its fixed-choice form")
	}
	toggleChoice := *toggle.toggleChoice
	toggleView := toggle.toggleForm.View()
	updated, cmd := toggle.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	toggle = updated.(Model)
	if cmd != nil || toggle.pane != paneToggleModal || *toggle.toggleChoice != toggleChoice || toggle.toggleForm.View() != toggleView {
		t.Fatalf("slash changed toggle choice state or form: pane=%v choice=%q", toggle.pane, *toggle.toggleChoice)
	}
	updated, cmd = toggle.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	toggle = updated.(Model)
	if cmd != nil || toggle.pane != paneToggleModal {
		t.Fatalf("toggle arrow navigation after slash = pane %v command %v", toggle.pane, cmd)
	}
	updated, cmd = toggle.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	toggle = updated.(Model)
	if cmd != nil || toggle.pane != paneList {
		t.Fatalf("toggle Escape after slash = pane %v command %v", toggle.pane, cmd)
	}

	picker := NewModel(t.TempDir(), home)
	picker.loading = false
	picker.pane = paneEditConfigModal
	picker.ensureEditConfigForm()
	if picker.editConfigForm == nil || picker.editConfigChoice == nil {
		t.Fatal("config-editor modal did not initialize its fixed-choice form")
	}
	pickerChoice := *picker.editConfigChoice
	pickerView := picker.editConfigForm.View()
	updated, cmd = picker.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	picker = updated.(Model)
	if cmd != nil || picker.pane != paneEditConfigModal || *picker.editConfigChoice != pickerChoice || picker.editConfigForm.View() != pickerView {
		t.Fatalf("slash changed config-editor choice state or form: pane=%v choice=%q", picker.pane, *picker.editConfigChoice)
	}
	updated, cmd = picker.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	picker = updated.(Model)
	if cmd != nil || picker.pane != paneEditConfigModal {
		t.Fatalf("config-editor arrow navigation after slash = pane %v command %v", picker.pane, cmd)
	}
	updated, cmd = picker.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	picker = updated.(Model)
	if cmd != nil || picker.pane != paneList || picker.operations.Active != nil {
		t.Fatalf("config-editor Escape after slash = pane %v command %v operation %v", picker.pane, cmd, picker.operations.Active)
	}
}
