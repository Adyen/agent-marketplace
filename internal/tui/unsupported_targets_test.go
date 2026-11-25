package tui

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Adyen/agent-marketplace/internal/config"
)

const work45UnsupportedTarget = "work45-unsupported"

type work45Fixture struct {
	home      string
	repo      string
	userRoot  string
	protected []string
}

type work45PathState struct {
	exists    bool
	mode      os.FileMode
	directory bool
	contents  []byte
	entries   []string
}

func newWork45Fixture(t *testing.T, rootPresent bool, organizationConfig []byte) work45Fixture {
	t.Helper()
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })
	home, repo := t.TempDir(), t.TempDir()
	xdgConfig, xdgState, xdgCache, codexHome := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", xdgConfig)
	t.Setenv("XDG_STATE_HOME", xdgState)
	t.Setenv("XDG_CACHE_HOME", xdgCache)
	t.Setenv("CODEX_HOME", codexHome)
	t.Chdir(repo)

	orgDir := t.TempDir()
	orgPath := filepath.Join(orgDir, "missing-org.yaml")
	if organizationConfig != nil {
		orgPath = filepath.Join(orgDir, "config.yaml")
		writeWork45File(t, orgPath, organizationConfig)
	}
	config.SetTestOrganizationConfigPath(t, orgPath)

	source := t.TempDir()
	writeWork45File(t, filepath.Join(source, "solo", "SKILL.md"), []byte("---\nname: solo\ndescription: TUI refusal fixture\n---\n# Solo\n"))
	userRoot := filepath.Join(home, ".agents", "config.yaml")
	if rootPresent {
		writeWork45File(t, userRoot, []byte(fmt.Sprintf("apiVersion: agent-marketplace/v4\nharnesses: [devin]\ntelemetry:\n  level: off\nsources:\n  local:\n    directory: %q\nresources:\n  local: {all: true}\n", source)))
	}
	repoLocal := filepath.Join(repo, ".agents", "config.local.yaml")
	writeWork45File(t, repoLocal, []byte("apiVersion: agent-marketplace/v4\nharnesses: [codex]\nsources: {}\nresources: {}\n"))
	sentinel := filepath.Join(home, ".config", "devin", "skills", "unrelated", "SKILL.md")
	writeWork45File(t, sentinel, []byte("# Unrelated native sentinel\n"))

	homeNative := filepath.Join(home, ".config", "devin")
	homeSkills := filepath.Join(homeNative, "skills")
	xdgNative := filepath.Join(xdgConfig, "devin")
	xdgSkills := filepath.Join(xdgNative, "skills")
	repoNative := filepath.Join(repo, ".devin")
	repoSkills := filepath.Join(repoNative, "skills")
	protected := []string{
		home, filepath.Join(home, ".agents"), userRoot, config.UserPersonal.LockPath(userRoot),
		filepath.Join(home, ".agents", "config.local.yaml"), filepath.Join(home, ".agents", "lock.local.yaml"),
		repo, filepath.Join(repo, ".agents"), filepath.Join(repo, ".agents", "config.yaml"), repoLocal,
		config.RepoPersonal.LockPath(repoLocal), config.RepoCommitted.LockPath(filepath.Join(repo, ".agents", "config.yaml")),
		orgDir, orgPath, config.Organization.LockPath(orgPath), source, filepath.Join(source, "solo"), filepath.Join(source, "solo", "SKILL.md"),
		homeNative, homeSkills, filepath.Join(homeSkills, work45UnsupportedTarget, "SKILL.md"), filepath.Join(homeSkills, "unrelated"), sentinel,
		xdgConfig, xdgNative, xdgSkills, filepath.Join(xdgSkills, work45UnsupportedTarget, "SKILL.md"),
		repoNative, repoSkills, filepath.Join(repoSkills, work45UnsupportedTarget, "SKILL.md"), filepath.Join(home, ".work45-unsupported"), codexHome,
	}
	return work45Fixture{home: home, repo: repo, userRoot: userRoot, protected: protected}
}

func writeWork45File(t *testing.T, path string, contents []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal("create private fixture directory")
	}
	if err := os.WriteFile(path, contents, 0o644); err != nil {
		t.Fatal("write private fixture file")
	}
}

func captureWork45Paths(t *testing.T, paths []string) []work45PathState {
	t.Helper()
	states := make([]work45PathState, len(paths))
	for i, path := range paths {
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatal("inspect protected TUI fixture path")
		}
		state := work45PathState{exists: true, mode: info.Mode(), directory: info.IsDir()}
		if info.IsDir() {
			entries, err := os.ReadDir(path)
			if err != nil {
				t.Fatal("read protected TUI fixture directory")
			}
			for _, entry := range entries {
				state.entries = append(state.entries, entry.Name())
			}
		} else {
			state.contents, err = os.ReadFile(path)
			if err != nil {
				t.Fatal("read protected TUI fixture file")
			}
		}
		states[i] = state
	}
	return states
}

func assertWork45PathsUnchanged(t *testing.T, paths []string, before []work45PathState) {
	t.Helper()
	after := captureWork45Paths(t, paths)
	for i, prior := range before {
		current := after[i]
		if prior.exists != current.exists || prior.mode != current.mode || prior.directory != current.directory ||
			!bytes.Equal(prior.contents, current.contents) || !reflect.DeepEqual(prior.entries, current.entries) {
			t.Fatal("TUI refusal changed protected host state")
		}
	}
}

func submitWork45TargetSelection(t *testing.T, m Model, paths []string, before []work45PathState) (Model, harnessWrittenMsg) {
	t.Helper()
	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	var ok bool
	m, ok = updated.(Model)
	if !ok || cmd == nil {
		t.Fatal("Enter did not submit the live harness form")
	}
	if m.operations.Active == nil || m.operations.Active.Kind != operationTargets {
		t.Fatal("Enter did not create the active target operation")
	}
	assertWork45PathsUnchanged(t, paths, before)
	operation := m.operations.Active.ID
	result, ok := cmd().(harnessWrittenMsg)
	if !ok {
		t.Fatal("target command did not return a harness-written message")
	}
	if result.operation != operation {
		t.Fatal("target command completion did not match its active operation")
	}
	assertWork45PathsUnchanged(t, paths, before)
	return m, result
}

func requireWork45Refusal(t *testing.T, err error, expectedPhrase string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), work45UnsupportedTarget) || !strings.Contains(err.Error(), expectedPhrase) {
		t.Fatal("TUI command did not return the unsupported-target validation error")
	}
}

func consumeWork45Refusal(t *testing.T, m Model, hw harnessWrittenMsg, expectedPhrase string) {
	t.Helper()
	updated, expiry := m.Update(hw)
	var ok bool
	m, ok = updated.(Model)
	if !ok || expiry == nil {
		t.Fatal("refusal did not settle as a TUI error notification")
	}
	if m.operations.Active != nil || len(m.diagnostics.Operations) != 1 {
		t.Fatal("refusal left the operation active or omitted its diagnostic")
	}
	diagnostic := m.diagnostics.Operations[0]
	if diagnostic.Operation != operationTargets || diagnostic.Category != diagnosticMutation || diagnostic.Severity != diagnosticError {
		t.Fatal("refusal diagnostic did not identify a target mutation error")
	}
	if diagnostic.Err == nil || !strings.Contains(diagnostic.Err.Error(), work45UnsupportedTarget) ||
		!strings.Contains(diagnostic.Err.Error(), expectedPhrase) || len(m.toastQueue) != 1 {
		t.Fatal("refusal notification omitted its validation error")
	}
	toast := m.toastQueue[0].text
	view := m.View().Content
	if !strings.Contains(toast, "error:") || !strings.Contains(toast, work45UnsupportedTarget) ||
		!strings.Contains(view, "error:") || !strings.Contains(view, expectedPhrase) {
		t.Fatal("visible TUI notification did not show the refusal")
	}
}

// spec: session-interactions/newly-introduced-unsupported-harness-target-is-refused
func TestTUIRefusesNewUnsupportedTarget(t *testing.T) {
	fixture := newWork45Fixture(t, true, nil)
	before := captureWork45Paths(t, fixture.protected)
	m := NewModel(fixture.repo, fixture.home)
	m.globalHarnesses = []string{"devin"}
	m.openHarnessModal()
	m.harnessModal.selected = harnessSelection([]string{"devin", work45UnsupportedTarget})
	m.buildHarnessForm()
	if !slices.Contains(m.harnessModal.optionIDs, work45UnsupportedTarget) || m.harnessModal.multiValue == nil ||
		!slices.Contains(*m.harnessModal.multiValue, "devin") || !slices.Contains(*m.harnessModal.multiValue, work45UnsupportedTarget) {
		t.Fatal("live Huh form did not include both selected harness IDs")
	}
	m, hw := submitWork45TargetSelection(t, m, fixture.protected, before)
	requireWork45Refusal(t, hw.err, "unknown or unsupported compiler harness")
	consumeWork45Refusal(t, m, hw, "unknown or unsupported compiler harness")
	if _, err := os.Stat(fixture.userRoot); err != nil {
		t.Fatal("supported user root disappeared after refusal")
	}
	assertWork45PathsUnchanged(t, fixture.protected, before)
}

// spec: session-interactions/initial-configuration-rejects-unsupported-harness-targets
func TestTUIRefusesUnsupportedInitialization(t *testing.T) {
	org := []byte("apiVersion: agent-marketplace/v4\nharnesses: [devin]\ntelemetry:\n  level: off\n")
	fixture := newWork45Fixture(t, false, org)
	before := captureWork45Paths(t, fixture.protected)
	m := NewModel(fixture.repo, fixture.home)
	m.pane = paneInitModal
	m.initModal = initModalState{selected: harnessSelection([]string{"devin", work45UnsupportedTarget})}
	m.ensureInitForm()
	if !slices.Contains(m.initModal.optionIDs, work45UnsupportedTarget) || m.initModal.multiValue == nil ||
		!slices.Contains(*m.initModal.multiValue, "devin") || !slices.Contains(*m.initModal.multiValue, work45UnsupportedTarget) {
		t.Fatal("live initialization Huh form did not include both selected harness IDs")
	}
	m, hw := submitWork45TargetSelection(t, m, fixture.protected, before)
	requireWork45Refusal(t, hw.err, "unknown harness")
	consumeWork45Refusal(t, m, hw, "unknown harness")
	if _, err := os.Lstat(fixture.userRoot); !os.IsNotExist(err) {
		t.Fatal("unsupported initialization created the user root")
	}
	assertWork45PathsUnchanged(t, fixture.protected, before)
}
