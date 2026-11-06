package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Adyen/agent-marketplace/internal/config"
)

// spec: consumer-config-format/consumers-do-not-offer-in-process-conversion
// spec: consumer-config-format/writable-layer-version-refusals-name-the-older-migrator
// spec: tui-interactions/guarded-submission
// spec: session-startup/invalid-configuration-delivers-one-fail-closed-final-stage
func TestUnsupportedConsumerConfigRefusesWithoutConversion(t *testing.T) {
	for _, tc := range []struct {
		name, root, fragment, code string
	}{
		{name: "v1", root: "registries: []\n", code: "v4.api-version-required"},
		{name: "v2", root: "apiVersion: agent-marketplace/v2\nregistries: []\n", code: "v4.api-version"},
		{name: "v3", root: "apiVersion: agent-marketplace/v3\nregistries: []\n", code: "v4.api-version"},
		{name: "include", root: "apiVersion: agent-marketplace/v4\ninclude: [fragment.yaml]\n", fragment: "apiVersion: agent-marketplace/v2\nregistries: []\n", code: "v4.api-version"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, home := t.TempDir(), t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
			t.Chdir(repo)
			configPath := filepath.Join(home, ".agents", "config.yaml")
			if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
				t.Fatal(err)
			}
			mustWriteFile(t, configPath, []byte(tc.root))
			badPath := configPath
			if tc.fragment != "" {
				badPath = filepath.Join(home, ".agents", "fragment.yaml")
				mustWriteFile(t, badPath, []byte(tc.fragment))
			}
			before, err := os.Stat(badPath)
			if err != nil {
				t.Fatal(err)
			}
			orgPath := filepath.Join(t.TempDir(), "config.yaml")
			mustWriteFile(t, orgPath, []byte("apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  guarded: {git: https://example.invalid/guarded.git, ref: main}\nresources:\n  guarded: {all: true, required: true}\n"))
			config.SetTestOrganizationConfigPath(t, orgPath)
			defer swapLoadOrgConfig(config.LoadOrganizationConfig)()
			acquisitions := 0
			restoreClone := swapCloneOrPull(func(string, string) (string, error) {
				acquisitions++
				return "", errors.New("unexpected acquisition")
			})
			defer restoreClone()

			m := NewModel(repo, home)
			snapshot := readSnapshot(context.Background(), repo, home, catalogLoadCached)
			var validation *config.V4ValidationError
			if !errors.As(errors.Join(snapshot.errs...), &validation) || validation.Code != tc.code {
				t.Fatalf("snapshot errors = %v, want %s", snapshot.errs, tc.code)
			}
			if !strings.Contains(errors.Join(snapshot.errs...).Error(), badPath) || !strings.Contains(errors.Join(snapshot.errs...).Error(), "apiVersion") {
				t.Fatalf("format diagnostic lacks offending path/field: %v", snapshot.errs)
			}
			snapshot.generation = m.requests.Active(requestSnapshot)
			stages := collectSnapshotStages(readSnapshotStages(context.Background(), repo, home, catalogLoadCached, snapshot.generation))
			if len(stages) != 1 || stages[0].stage != snapshotStageFinal || stages[0].mergedCfg != nil || stages[0].provisionalCfg != nil || len(stages[0].rows) != 0 {
				t.Fatalf("unsupported startup published a usable config/inventory stage: %+v", stages)
			}
			updated, _ := m.Update(snapshot)
			m = updated.(Model)
			if !m.configInvalid || m.pane != paneList || m.mergedCfg != nil || m.operations.Active != nil {
				t.Fatalf("unsupported config entered operation: invalid=%t pane=%v cfg=%v operation=%v", m.configInvalid, m.pane, m.mergedCfg, m.operations.Active)
			}
			for _, key := range []rune{'s', 'u', 'H', ' ', 'm'} {
				updated, _ = m.Update(keyPress(key))
				m = updated.(Model)
				if m.operations.Active != nil || m.syncing || m.pane != paneList {
					t.Fatalf("%q bypassed refusal: pane=%v operation=%v syncing=%t", key, m.pane, m.operations.Active, m.syncing)
				}
			}
			updated, _ = m.Update(keyPress('e'))
			m = updated.(Model)
			view := ansi.Strip(m.viewContent())
			searchableView := strings.ReplaceAll(strings.Join(strings.Fields(view), " "), "- ", "-")
			if !strings.Contains(searchableView, tc.code) || !strings.Contains(searchableView, "apiVersion") {
				t.Fatalf("error view lacks format diagnostic:\n%s", view)
			}
			if !strings.Contains(searchableView, "agent-marketplace v4.1.4") ||
				!strings.Contains(searchableView, "agent-marketplace migrate --to v4 --scope user --dry-run") ||
				!strings.Contains(searchableView, "https://github.com/Adyen/agent-marketplace/blob/main/site/docs/how-to/migrate-configuration.md") ||
				!strings.Contains(searchableView, "include trees or unrepresentable input") ||
				strings.Contains(searchableView, "Fix the file and field named above") {
				t.Fatalf("error view lacks remediation or retains generic recovery copy:\n%s", view)
			}
			for _, unwanted := range []string{"Configuration migration required", "migrate to v4", "leave unchanged", "remain readable"} {
				if strings.Contains(view, unwanted) {
					t.Fatalf("TUI offered conversion %q:\n%s", unwanted, view)
				}
			}
			if acquisitions != 0 {
				t.Fatalf("unsupported config acquired %d sources", acquisitions)
			}
			after, err := os.Stat(badPath)
			if err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
				t.Fatalf("refusal changed config identity/metadata: %v", err)
			}
			expectedFiles := map[string]string{configPath: tc.root}
			if tc.fragment != "" {
				expectedFiles[badPath] = tc.fragment
			}
			for path, expected := range expectedFiles {
				data, err := os.ReadFile(path)
				if err != nil || string(data) != expected {
					t.Fatalf("refusal changed %s: %q, %v", path, data, err)
				}
			}
			for _, path := range []string{config.ProjectConfigPath(repo), config.LocalConfigPath(repo), filepath.Join(home, ".agents", "lock.yaml")} {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatalf("refusal created %s: %v", path, err)
				}
			}
			updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
			m = updated.(Model)
			_, cmd := m.Update(keyPress('q'))
			if !cmdProducesQuitMsg(cmd) {
				t.Fatal("format refusal prevented quit")
			}
			// Non-vacuity control: without the unsupported root the valid
			// organization source must reach the same acquisition sentinel.
			mustWriteFile(t, configPath, []byte("apiVersion: agent-marketplace/v4\n"))
			readSnapshot(context.Background(), repo, home, catalogLoadCached)
			if acquisitions == 0 {
				t.Fatal("valid control did not reach the guarded acquisition")
			}
		})
	}
}

func TestV4StartupRetainsHarnessesWithoutConversion(t *testing.T) {
	repo, home := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Chdir(repo)
	const valid = "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n"
	path := filepath.Join(home, ".agents", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, path, []byte(valid))
	m := NewModel(repo, home)
	snapshot := readSnapshot(context.Background(), repo, home, catalogLoadCached)
	if len(snapshot.errs) > 0 {
		t.Fatalf("valid v4 refused: %v", snapshot.errs)
	}
	snapshot.generation = m.requests.Active(requestSnapshot)
	updated, _ := m.Update(snapshot)
	m = updated.(Model)
	if m.configInvalid || m.pane != paneList || m.mergedCfg == nil || !equalStrings(m.globalHarnesses, []string{"factory"}) {
		t.Fatalf("v4 startup state: invalid=%t pane=%v cfg=%v harnesses=%v", m.configInvalid, m.pane, m.mergedCfg, m.globalHarnesses)
	}
	updated, _ = m.Update(keyPress('H'))
	m = updated.(Model)
	if m.pane != paneHarnessModal || !m.harnessModal.selected["factory"] {
		t.Fatal("valid v4 startup could not open its normal harness modal")
	}
	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = updated.(Model)
	if cmd != nil || m.pane != paneList {
		t.Fatal("normal modal cancel dispatched a write")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != valid {
		t.Fatalf("normal modal changed config: %q, %v", data, err)
	}
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
