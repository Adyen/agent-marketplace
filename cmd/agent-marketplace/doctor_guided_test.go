package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/devincli"
)

// spec: doctor-repair-planning/configuration-errors-show-safe-source-context
func TestDoctorRepairGuidedDiagnostics(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, tc := range []struct {
		name, root, fragment, source, code string
		line                               int
		escapes                            []string
	}{
		{name: "unknown field", root: "apiVersion: agent-marketplace/v4\nunknown_field: true\n", source: "unknown_field: true", code: "v4.unknown-field", line: 2},
		{name: "wrong field type", root: "apiVersion: agent-marketplace/v4\nsources: []\n", source: "sources: []", code: "v4.type", line: 2},
		{name: "inapplicable source field", root: "apiVersion: agent-marketplace/v4\nsources:\n  team: {directory: ./team, ref: main}\n", source: "  team: {directory: ./team, ref: main}", code: "v4.source-field", line: 3},
		{name: "directory path", root: "apiVersion: agent-marketplace/v4\nsources:\n  team: {directory: ../outside}\n", source: "  team: {directory: ../outside}", code: "v4.directory-path", line: 3},
		{name: "empty include", root: "apiVersion: agent-marketplace/v4\ninclude: ''\n", source: "include: ''", code: "v4.include", line: 2},
		{name: "malformed YAML", root: "apiVersion: agent-marketplace/v4\nsources: [\n", source: "sources: [", code: "v4.parse", line: 2},
		{name: "duplicate key", root: "apiVersion: agent-marketplace/v4\nsources:\n  team: {directory: /one}\n  team: {directory: /two}\n", source: "  team: {directory: /two}", code: "v4.duplicate-key", line: 4},
		{name: "included fragment", root: "apiVersion: agent-marketplace/v4\ninclude: fragment.yaml\n", fragment: "sources:\n  team: {directory: /one, unknown: true}\n", source: "  team: {directory: /one, unknown: true}", code: "v4.unknown-field", line: 2},
		{name: "control characters", root: "apiVersion: agent-marketplace/v4\x01\u0085\u202e\r\n", source: "apiVersion: agent-marketplace/v4", code: "v4.parse", line: 1, escapes: []string{"\\x01", "\\x85", "\\u202E", "\\x0D"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repoRoot, _ := devinDoctorEnv(t)
			rootPath := filepath.Join(repoRoot, ".agents", "config.yaml")
			writeFile(t, rootPath, tc.root)
			sourcePath := rootPath
			if tc.fragment != "" {
				sourcePath = filepath.Join(repoRoot, ".agents", "fragment.yaml")
				writeFile(t, sourcePath, tc.fragment)
			}
			before, err := os.ReadFile(sourcePath)
			if err != nil {
				t.Fatal(err)
			}

			var out bytes.Buffer
			err = runDoctor(doctorOptions{
				out:    &out,
				repair: true,
				probe:  staticProbe(devincli.Probe{Status: devincli.StatusNotInstalled}),
			})
			if err == nil {
				t.Fatal("repair succeeded with invalid configuration")
			}
			text := out.String()
			displayPath, pathErr := filepath.EvalSymlinks(sourcePath)
			if pathErr != nil {
				t.Fatal(pathErr)
			}
			if !strings.Contains(text, "[guided]") || !strings.Contains(text, tc.code) ||
				!strings.Contains(text, fmt.Sprintf("%s:%d", displayPath, tc.line)) ||
				!strings.Contains(text, "source: "+tc.source) {
				t.Fatalf("guided source diagnostic is incomplete:\n%s", text)
			}
			for _, escaped := range tc.escapes {
				if !strings.Contains(text, escaped) {
					t.Errorf("terminal output lacks escaped control %q:\n%s", escaped, text)
				}
			}
			if strings.ContainsRune(text, '\u202e') || strings.ContainsRune(text, '\x01') ||
				strings.ContainsRune(text, '\u0085') || strings.ContainsRune(text, '\r') {
				t.Fatalf("terminal output contains raw control characters: %q", text)
			}
			after, err := os.ReadFile(sourcePath)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("preview changed config bytes: %v\n%s", err, after)
			}
		})
	}
}

// spec: doctor-repair-planning/an-editor-fix-is-diagnosed-again
func TestDoctorRepairGuidedEditorSelectionAndRediagnosis(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, tc := range []struct {
		name, selected string
		visualSet      bool
	}{
		{name: "VISUAL precedes EDITOR", selected: "visual", visualSet: true},
		{name: "EDITOR fallback", selected: "editor"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repoRoot, _ := devinDoctorEnv(t)
			configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
			writeFile(t, configPath, "apiVersion: agent-marketplace/v4\nunknown_field: true\n")
			mcpPath, selection := prepareDevinRepairFixture(t, repoRoot)
			editorLog := filepath.Join(t.TempDir(), "editor.log")
			editor := func(name string) string {
				path := filepath.Join(t.TempDir(), name+" editor.sh")
				writeFile(t, path, "#!/bin/sh\nprintf '"+name+":%s\\n' \"$1\" >> \"$EDITOR_LOG\"\ncat > \"$1\" <<'EOF'\napiVersion: agent-marketplace/v4\nharnesses: [devin]\nsources: {}\nEOF\n")
				return "sh " + fmt.Sprintf("%q", path)
			}
			visual := editor("visual")
			fallback := editor("editor")
			t.Setenv("EDITOR_LOG", editorLog)
			t.Setenv("EDITOR", fallback)
			if tc.visualSet {
				t.Setenv("VISUAL", visual)
			} else {
				t.Setenv("VISUAL", "  ")
			}

			var out bytes.Buffer
			err := runDoctor(doctorOptions{
				out:     &out,
				in:      strings.NewReader("y\ny\n"),
				isTTY:   true,
				repair:  true,
				probe:   staticProbe(devincli.Probe{Status: devincli.StatusNotInstalled}),
				resolve: staticResolve(selection),
			})
			if err != nil {
				t.Fatalf("guided editor repair: %v\n%s", err, out.String())
			}
			log, err := os.ReadFile(editorLog)
			if err != nil {
				t.Fatalf("editor was not launched: %v\n%s", err, out.String())
			}
			if !strings.Contains(string(log), tc.selected+":"+doctorTestPath(t, configPath)) || strings.Contains(string(log), "visual:") != tc.visualSet {
				t.Fatalf("editor selection or config argument = %q, want %q:\n%s", log, tc.selected+":"+doctorTestPath(t, configPath), out.String())
			}
			if !strings.Contains(out.String(), "adopt exact shared-aggregate matches") {
				t.Fatalf("re-diagnosis did not make the selection-dependent repair runnable:\n%s", out.String())
			}
			if data, err := os.ReadFile(configPath); err != nil || strings.Contains(string(data), "unknown_field") {
				t.Fatalf("editor did not fix the selected config: %v\n%s", err, data)
			}
			if _, err := os.Stat(mcpPath); err != nil {
				t.Fatalf("guided repair removed the aggregate: %v", err)
			}
		})
	}
}

// spec: doctor-repair-planning/an-editor-fix-is-diagnosed-again
func TestDoctorRepairGuidedEditorReceivesConfigPathAsArgument(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	root := t.TempDir()
	injected := filepath.Join(root, "injected")
	if err := os.MkdirAll(filepath.Join(injected, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	repoRoot := filepath.Join(root, "repo;touch "+injected)
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	withWorkingDir(t, repoRoot)
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	writeFile(t, configPath, "apiVersion: agent-marketplace/v4\nunknown_field: true\n")
	editorLog := filepath.Join(root, "editor.log")
	script := filepath.Join(root, "editor.sh")
	writeFile(t, script, "#!/bin/sh\nprintf '%s\\n' \"$1\" > \"$EDITOR_LOG\"\ncat > \"$1\" <<'EOF'\napiVersion: agent-marketplace/v4\nsources: {}\nEOF\n")
	t.Setenv("EDITOR_LOG", editorLog)
	t.Setenv("VISUAL", "sh "+fmt.Sprintf("%q", script))

	var out bytes.Buffer
	err := runDoctor(doctorOptions{
		out:    &out,
		in:     strings.NewReader("y\n"),
		isTTY:  true,
		repair: true,
		probe:  staticProbe(devincli.Probe{Status: devincli.StatusNotInstalled}),
	})
	if err != nil {
		t.Fatalf("guided editor repair: %v\n%s", err, out.String())
	}
	got, err := os.ReadFile(editorLog)
	if err != nil || strings.TrimSpace(string(got)) != doctorTestPath(t, configPath) {
		t.Fatalf("editor path argument = %q, want %q: %v", got, doctorTestPath(t, configPath), err)
	}
	if _, err := os.Stat(filepath.Join(injected, ".agents", "config.yaml")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("config path was evaluated as shell text: %v", err)
	}
}

// spec: doctor-repair-planning/an-editor-fix-is-diagnosed-again
func TestDoctorRepairGuidedEditorLeavingFileBrokenIsDeferred(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	repoRoot, _ := devinDoctorEnv(t)
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	writeFile(t, configPath, "apiVersion: agent-marketplace/v4\nunknown_field: true\n")
	log := filepath.Join(t.TempDir(), "editor.log")
	script := filepath.Join(t.TempDir(), "keep-broken.sh")
	writeFile(t, script, "#!/bin/sh\nprintf '%s\\n' \"$1\" >> \"$EDITOR_LOG\"\n")
	t.Setenv("EDITOR_LOG", log)
	t.Setenv("VISUAL", "sh "+fmt.Sprintf("%q", script))

	var out bytes.Buffer
	err := runDoctor(doctorOptions{
		out:    &out,
		in:     strings.NewReader("y\n"),
		isTTY:  true,
		repair: true,
		probe:  staticProbe(devincli.Probe{Status: devincli.StatusNotInstalled}),
	})
	if err == nil || strings.Contains(err.Error(), "kept recurring") {
		t.Fatalf("still-broken guided step did not stop as incomplete repair: %v\n%s", err, out.String())
	}
	if strings.Count(out.String(), "[guided]") != 2 {
		t.Fatalf("still-broken guided step was not shown again after diagnosis:\n%s", out.String())
	}
	if data, readErr := os.ReadFile(configPath); readErr != nil || string(data) != "apiVersion: agent-marketplace/v4\nunknown_field: true\n" {
		t.Fatalf("editor changed the broken file: %v\n%s", readErr, data)
	}
	if data, readErr := os.ReadFile(log); readErr != nil || strings.Count(string(data), configPath) != 1 {
		t.Fatalf("editor launched %d times, want once: %v\n%s", strings.Count(string(data), configPath), readErr, data)
	}
}

// spec: doctor-repair-planning/missing-editor-leaves-manual-instructions
func TestDoctorRepairGuidedWithoutEditorPrintsInstructions(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	repoRoot, _ := devinDoctorEnv(t)
	writeFile(t, filepath.Join(repoRoot, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nunknown_field: true\n")
	t.Setenv("VISUAL", " ")
	t.Setenv("EDITOR", "")

	var out bytes.Buffer
	err := runDoctor(doctorOptions{
		out:    &out,
		in:     strings.NewReader(""),
		isTTY:  true,
		repair: true,
		probe:  staticProbe(devincli.Probe{Status: devincli.StatusNotInstalled}),
	})
	if err == nil || !strings.Contains(out.String(), "manual") || strings.Contains(out.String(), "editor failed") {
		t.Fatalf("missing editor was not deferred with manual instructions: %v\n%s", err, out.String())
	}
}

// spec: doctor-repair-planning/non-interactive-guided-steps-are-deferred
func TestDoctorRepairGuidedStepsAreDeferredWithoutConsentContext(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, tc := range []struct {
		name  string
		isTTY bool
		yes   bool
	}{
		{name: "TTY with yes", isTTY: true, yes: true},
		{name: "off TTY with yes", yes: true},
		{name: "off TTY preview"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repoRoot, _ := devinDoctorEnv(t)
			configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
			before := "apiVersion: agent-marketplace/v4\nunknown_field: true\n"
			writeFile(t, configPath, before)
			marker := filepath.Join(t.TempDir(), "editor-ran")
			script := filepath.Join(t.TempDir(), "marker.sh")
			writeFile(t, script, "#!/bin/sh\nprintf ran > \"$EDITOR_MARKER\"\n")
			t.Setenv("EDITOR_MARKER", marker)
			t.Setenv("VISUAL", "sh "+fmt.Sprintf("%q", script))

			var out bytes.Buffer
			err := runDoctor(doctorOptions{
				out:    &out,
				in:     strings.NewReader("y\n"),
				isTTY:  tc.isTTY,
				yes:    tc.yes,
				repair: true,
				probe:  staticProbe(devincli.Probe{Status: devincli.StatusNotInstalled}),
			})
			if err == nil || !strings.Contains(out.String(), "deferred") {
				t.Fatalf("guided item was not deferred: %v\n%s", err, out.String())
			}
			if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("deferred guided step launched editor: %v", err)
			}
			if data, err := os.ReadFile(configPath); err != nil || string(data) != before {
				t.Fatalf("deferred guided step changed config bytes: %v\n%s", err, data)
			}
		})
	}
}

// spec: doctor-repair-planning/version-and-organization-issues-remain-human-only
func TestDoctorRepairVersionAndOrganizationIssuesRemainHumanOnly(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, tc := range []struct {
		name, configText, wantCode, wantRemedy string
	}{
		{name: "missing version", configText: "harnesses: []\n", wantCode: "v4.api-version-required", wantRemedy: "older released migrator"},
		{name: "unsupported version", configText: "apiVersion: agent-marketplace/v3\n", wantCode: "v4.api-version", wantRemedy: "older released migrator"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repoRoot, _ := devinDoctorEnv(t)
			configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
			writeFile(t, configPath, tc.configText)
			assertDoctorHumanOnlyUnchanged(t, configPath, tc.wantCode, tc.wantRemedy)
		})
	}
	t.Run("versionless repository root included by personal-local config", func(t *testing.T) {
		repoRoot, _ := devinDoctorEnv(t)
		repoPath := filepath.Join(repoRoot, ".agents", "config.yaml")
		localPath := filepath.Join(repoRoot, ".agents", "config.local.yaml")
		writeFile(t, repoPath, "unknown_field: true\n")
		writeFile(t, localPath, "apiVersion: agent-marketplace/v4\ninclude: config.yaml\n")
		assertDoctorHumanOnlyUnchanged(t, repoPath, "v4.api-version-required", "older released migrator")
	})
	t.Run("organization trust", func(t *testing.T) {
		_, _ = devinDoctorEnv(t)
		authority := t.TempDir()
		orgPath := filepath.Join(authority, "config.yaml")
		fragmentPath := filepath.Join(authority, "fragment.yaml")
		writeFile(t, orgPath, "apiVersion: agent-marketplace/v4\ninclude: fragment.yaml\n")
		writeFile(t, fragmentPath, "sources: {}\n")
		if err := os.Chmod(fragmentPath, 0o666); err != nil {
			t.Fatal(err)
		}
		config.SetTestOrganizationConfigPath(t, orgPath)
		beforeRoot, _ := os.ReadFile(orgPath)
		beforeFragment, _ := os.ReadFile(fragmentPath)
		assertDoctorHumanOnlyUnchanged(t, orgPath, "organization.trust.include.writable", "restrict writes to the organization include")
		afterRoot, _ := os.ReadFile(orgPath)
		afterFragment, _ := os.ReadFile(fragmentPath)
		if !bytes.Equal(beforeRoot, afterRoot) || !bytes.Equal(beforeFragment, afterFragment) {
			t.Fatalf("human-only organization issue changed files:\nroot=%s\nfragment=%s", afterRoot, afterFragment)
		}
	})
}

// spec: doctor-repair-planning/version-and-organization-issues-remain-human-only
func TestDoctorRepairOrganizationIncludeAliasesRemainHumanOnly(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, alias := range []string{"symlink", "hard link"} {
		t.Run(alias, func(t *testing.T) {
			for _, scenario := range []struct {
				name, organizationCode, organizationRemedy string
				includeMode                                os.FileMode
			}{
				{name: "validation error", organizationCode: "v4.unknown-field", organizationRemedy: "Ask your administrator to correct the organization configuration", includeMode: 0o444},
				{name: "trust refusal", organizationCode: "organization.trust.include.writable", organizationRemedy: "Ask your administrator to restrict writes to the organization include", includeMode: 0o666},
			} {
				t.Run(scenario.name, func(t *testing.T) {
					repoRoot, _ := devinDoctorEnv(t)
					authority := filepath.Join(repoRoot, ".agents")
					orgPath := filepath.Join(authority, "organization.yaml")
					includePath := filepath.Join(authority, "fragment.yaml")
					writeFile(t, orgPath, "apiVersion: agent-marketplace/v4\ninclude: fragment.yaml\n")
					writeFile(t, includePath, "apiVersion: agent-marketplace/v4\nunknown_field: true\n")
					if err := os.Chmod(includePath, scenario.includeMode); err != nil {
						t.Fatal(err)
					}
					config.SetTestOrganizationConfigPath(t, orgPath)

					repoConfig := filepath.Join(authority, "config.yaml")
					if alias == "hard link" {
						if err := os.Link(includePath, repoConfig); err != nil {
							t.Fatal(err)
						}
					} else {
						target, err := filepath.Rel(filepath.Dir(repoConfig), includePath)
						if err != nil {
							t.Fatal(err)
						}
						if err := os.Symlink(target, repoConfig); err != nil {
							t.Fatal(err)
						}
					}
					beforeRoot, err := os.ReadFile(orgPath)
					if err != nil {
						t.Fatal(err)
					}
					beforeInclude, err := os.ReadFile(includePath)
					if err != nil {
						t.Fatal(err)
					}

					marker := filepath.Join(t.TempDir(), "editor-launched")
					editor := filepath.Join(t.TempDir(), "editor.sh")
					writeFile(t, editor, "#!/bin/sh\nprintf launched > \"$EDITOR_MARKER\"\n")
					t.Setenv("EDITOR_MARKER", marker)
					t.Setenv("VISUAL", "sh "+fmt.Sprintf("%q", editor))

					var out bytes.Buffer
					if err := runDoctor(doctorOptions{
						out:    &out,
						in:     strings.NewReader("y\ny\n"),
						isTTY:  true,
						repair: true,
						probe:  staticProbe(devincli.Probe{Status: devincli.StatusNotInstalled}),
					}); err == nil {
						t.Fatal("repair succeeded with a human-only organization issue")
					}
					text := out.String()
					if !strings.Contains(text, "[human-only]") || !strings.Contains(text, scenario.organizationCode) ||
						!strings.Contains(text, "v4.unknown-field") ||
						!strings.Contains(text, scenario.organizationRemedy) ||
						strings.Contains(text, "[guided]") {
						t.Fatalf("organization alias did not retain its human-only remedy:\n%s", text)
					}
					if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
						t.Fatalf("organization alias launched the editor: %v", err)
					}
					afterRoot, err := os.ReadFile(orgPath)
					if err != nil || !bytes.Equal(beforeRoot, afterRoot) {
						t.Fatalf("organization root changed: %v\n%s", err, afterRoot)
					}
					afterInclude, err := os.ReadFile(includePath)
					if err != nil || !bytes.Equal(beforeInclude, afterInclude) {
						t.Fatalf("organization include changed: %v\n%s", err, afterInclude)
					}
				})
			}
		})
	}
}

func assertDoctorHumanOnlyUnchanged(t *testing.T, path, code, remedy string) {
	t.Helper()
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(t.TempDir(), "editor-ran")
	script := filepath.Join(t.TempDir(), "marker.sh")
	writeFile(t, script, "#!/bin/sh\nprintf ran > \"$EDITOR_MARKER\"\n")
	t.Setenv("EDITOR_MARKER", marker)
	t.Setenv("VISUAL", "sh "+fmt.Sprintf("%q", script))
	var out bytes.Buffer
	err = runDoctor(doctorOptions{
		out:    &out,
		in:     strings.NewReader("y\n"),
		isTTY:  true,
		repair: true,
		probe:  staticProbe(devincli.Probe{Status: devincli.StatusNotInstalled}),
	})
	if err == nil || !strings.Contains(out.String(), "[human-only]") ||
		!strings.Contains(out.String(), code) || !strings.Contains(out.String(), remedy) ||
		strings.Contains(out.String(), "[guided]") {
		t.Fatalf("human-only issue lacks its remedy: %v\n%s", err, out.String())
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("human-only issue launched editor: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("human-only issue changed %s: %v\n%s", path, err, after)
	}
}

// spec: doctor-repair-planning/a-guided-layer-does-not-block-independent-steps
func TestDoctorRepairIndependentGuidedLayerCanRunAfterDecline(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	repoRoot, home := devinDoctorEnv(t)
	repoConfig := filepath.Join(repoRoot, ".agents", "config.yaml")
	userConfig := filepath.Join(home, ".agents", "config.yaml")
	repoBefore := "apiVersion: agent-marketplace/v4\nunknown_repo: true\n"
	userBefore := "apiVersion: agent-marketplace/v4\nunknown_user: true\n"
	writeFile(t, repoConfig, repoBefore)
	writeFile(t, userConfig, userBefore)
	mcpPath, selection := prepareDevinRepairFixture(t, repoRoot)
	aggregateBefore, err := os.ReadFile(mcpPath)
	if err != nil {
		t.Fatal(err)
	}
	editorLog := filepath.Join(t.TempDir(), "editor.log")
	script := filepath.Join(t.TempDir(), "fix-one.sh")
	writeFile(t, script, "#!/bin/sh\nprintf '%s\\n' \"$1\" >> \"$EDITOR_LOG\"\ncat > \"$1\" <<'EOF'\napiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources: {}\nEOF\n")
	t.Setenv("EDITOR_LOG", editorLog)
	t.Setenv("VISUAL", "sh "+fmt.Sprintf("%q", script))

	var out bytes.Buffer
	err = runDoctor(doctorOptions{
		out:     &out,
		in:      strings.NewReader("n\ny\n"),
		isTTY:   true,
		repair:  true,
		probe:   staticProbe(devincli.Probe{Status: devincli.StatusNotInstalled}),
		resolve: staticResolve(selection),
	})
	if err == nil || !strings.Contains(out.String(), "[guided]") ||
		!strings.Contains(out.String(), "reconcile shared-aggregate ownership") ||
		!strings.Contains(out.String(), "blocked by") {
		t.Fatalf("broken layers did not retain independent guided work and block selection work: %v\n%s", err, out.String())
	}
	edited, err := os.ReadFile(editorLog)
	if err != nil || strings.Count(string(edited), "\n") != 1 {
		t.Fatalf("second guided step did not run after the first was declined: %v\n%s", err, edited)
	}
	editedPath := strings.TrimSpace(string(edited))
	if editedPath != doctorTestPath(t, repoConfig) && editedPath != doctorTestPath(t, userConfig) {
		t.Fatalf("editor received %q, want one config path", editedPath)
	}
	unchangedPath, original := doctorTestPath(t, repoConfig), repoBefore
	if editedPath == doctorTestPath(t, repoConfig) {
		unchangedPath, original = doctorTestPath(t, userConfig), userBefore
	}
	if data, readErr := os.ReadFile(editedPath); readErr != nil || !strings.Contains(string(data), "harnesses: [factory]") {
		t.Fatalf("accepted editor did not update %s: %v\n%s", editedPath, readErr, data)
	}
	if data, readErr := os.ReadFile(unchangedPath); readErr != nil || string(data) != original {
		t.Fatalf("declined editor changed %s: %v\n%s", unchangedPath, readErr, data)
	}
	aggregateAfter, err := os.ReadFile(mcpPath)
	if err != nil || !bytes.Equal(aggregateBefore, aggregateAfter) {
		t.Fatalf("selection-dependent step changed aggregate bytes: %v\n%s", err, aggregateAfter)
	}
}

// spec: doctor-repair-planning/policy-change-before-a-confirmed-step-stops-repair
// spec: doctor-repair-planning/an-editor-fix-is-diagnosed-again
func TestDoctorRepairGuidedEditKeepsPolicyGenerationChecks(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	repoRoot, home := devinDoctorEnv(t)
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	userConfig := filepath.Join(home, ".agents", "config.yaml")
	writeFile(t, configPath, "apiVersion: agent-marketplace/v4\nunknown_repo: true\n")
	writeFile(t, userConfig, "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources: {}\n")
	mcpPath, selection := prepareDevinRepairFixture(t, repoRoot)
	assertUnchanged := snapshotDoctorRepairFiles(t, repoRoot, mcpPath)
	script := filepath.Join(t.TempDir(), "change-unrelated.sh")
	writeFile(t, script, "#!/bin/sh\ncat > \"$1\" <<'EOF'\napiVersion: agent-marketplace/v4\nharnesses: [devin]\nsources: {}\nEOF\ncat > \"$UNRELATED_CONFIG\" <<'EOF'\napiVersion: agent-marketplace/v4\nharnesses: [codex]\nsources: {}\nEOF\n")
	t.Setenv("UNRELATED_CONFIG", userConfig)
	t.Setenv("VISUAL", "sh "+fmt.Sprintf("%q", script))

	var out bytes.Buffer
	err := runDoctor(doctorOptions{
		out:     &out,
		in:      strings.NewReader("y\n"),
		isTTY:   true,
		repair:  true,
		probe:   staticProbe(devincli.Probe{Status: devincli.StatusNotInstalled}),
		resolve: staticResolve(selection),
	})
	if !errors.Is(err, dependencytxn.ErrGenerationConflict) || !strings.Contains(out.String(), "policy changed") {
		t.Fatalf("unrelated editor write bypassed policy generation checks: %v\n%s", err, out.String())
	}
	assertUnchanged()
}

// spec: doctor-repair-planning/policy-change-before-a-confirmed-step-stops-repair
func TestDoctorRepairGuidedEditRevalidatesAtNextStepBoundary(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	repoRoot, _ := devinDoctorEnv(t)
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	writeFile(t, configPath, "apiVersion: agent-marketplace/v4\nunknown_field: true\n")
	mcpPath, selection := prepareDevinRepairFixture(t, repoRoot)
	assertUnchanged := snapshotDoctorRepairFiles(t, repoRoot, mcpPath)
	script := filepath.Join(t.TempDir(), "fix-config.sh")
	writeFile(t, script, "#!/bin/sh\ncat > \"$1\" <<'EOF'\napiVersion: agent-marketplace/v4\nharnesses: [devin]\nsources: {}\nEOF\n")
	t.Setenv("VISUAL", "sh "+fmt.Sprintf("%q", script))
	promptReads := 0
	input := doctorPlannerReadFunc(func(p []byte) (int, error) {
		promptReads++
		if promptReads > 2 {
			return 0, io.EOF
		}
		if promptReads == 2 {
			if err := os.WriteFile(configPath, []byte("apiVersion: agent-marketplace/v4\nharnesses: [codex]\nsources: {}\n"), 0o644); err != nil {
				return 0, err
			}
		}
		return copy(p, "y\n"), nil
	})

	var out bytes.Buffer
	err := runDoctor(doctorOptions{
		out:          &out,
		repairReader: bufio.NewReader(input),
		isTTY:        true,
		repair:       true,
		probe:        staticProbe(devincli.Probe{Status: devincli.StatusNotInstalled}),
		resolve:      staticResolve(selection),
	})
	if promptReads != 2 {
		t.Fatalf("confirmation reader was called %d times, want guided and safe-step confirmations", promptReads)
	}
	if !errors.Is(err, dependencytxn.ErrGenerationConflict) {
		t.Fatalf("edit generation was not revalidated before the next step: %v\n%s", err, out.String())
	}
	assertUnchanged()
}

// spec: doctor-repair-planning/an-editor-fix-is-diagnosed-again
func TestDoctorRepairGuidedEditorNonzeroExitRediagnoses(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, tc := range []struct {
		name       string
		fix        bool
		wantGuided int
		wantErr    bool
	}{
		{name: "leaves broken input", wantGuided: 2, wantErr: true},
		{name: "fixes input", fix: true, wantGuided: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repoRoot, _ := devinDoctorEnv(t)
			configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
			before := "apiVersion: agent-marketplace/v4\nunknown_field: true\n"
			writeFile(t, configPath, before)
			editorLog := filepath.Join(t.TempDir(), "editor.log")
			script := filepath.Join(t.TempDir(), "exit-17.sh")
			scriptBody := "#!/bin/sh\nprintf '%s\\n' \"$1\" >> \"$EDITOR_LOG\"\n"
			if tc.fix {
				scriptBody += "cat > \"$1\" <<'EOF'\napiVersion: agent-marketplace/v4\nsources: {}\nEOF\n"
			}
			scriptBody += "exit 17\n"
			writeFile(t, script, scriptBody)
			t.Setenv("EDITOR_LOG", editorLog)
			t.Setenv("VISUAL", "sh "+fmt.Sprintf("%q", script))

			var out bytes.Buffer
			err := runDoctor(doctorOptions{
				out:    &out,
				in:     strings.NewReader("y\n"),
				isTTY:  true,
				repair: true,
				probe:  staticProbe(devincli.Probe{Status: devincli.StatusNotInstalled}),
			})
			if (err != nil) != tc.wantErr {
				t.Fatalf("repair error = %v, want error %t\n%s", err, tc.wantErr, out.String())
			}
			if err != nil && strings.Contains(err.Error(), "run configuration editor") {
				t.Fatalf("finished editor exit was treated as a startup failure: %v\n%s", err, out.String())
			}
			if !strings.Contains(out.String(), "  editor exited with status 17; diagnosing configuration again\n") {
				t.Fatalf("editor exit status was not reported:\n%s", out.String())
			}
			if got := strings.Count(out.String(), "[guided]"); got != tc.wantGuided {
				t.Fatalf("guided step count = %d, want %d:\n%s", got, tc.wantGuided, out.String())
			}
			if data, readErr := os.ReadFile(editorLog); readErr != nil || strings.Count(string(data), doctorTestPath(t, configPath)) != 1 {
				t.Fatalf("editor launch log = %q, want one launch: %v", data, readErr)
			}
			after, readErr := os.ReadFile(configPath)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if tc.fix {
				if string(after) != "apiVersion: agent-marketplace/v4\nsources: {}\n" {
					t.Fatalf("editor result = %q, want valid configuration", after)
				}
			} else if string(after) != before {
				t.Fatalf("editor changed broken configuration: %q", after)
			}
		})
	}
}

func doctorTestPath(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}
