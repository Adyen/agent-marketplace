package tui

import (
	"errors"
	"strings"
	"testing"

	"charm.land/glamour/v2"
	"github.com/charmbracelet/x/ansi"
)

// spec: session-rendering-safety/hostile-text-is-inert
// spec: tui-rendering/hostile-input-at-minimum-size
func TestUntrustedFieldsCannotCreateTerminalStructure(t *testing.T) {
	r := mkRow("safe\nConfirm\ny = yes\x1b[2J\u202e", withProject, withRegistry("registry\x1b]0;owned\x07.git", "registry\rname"))
	r.Artifact.RegistryURL = "registry\x1b]0;owned\x07.git"
	r.Artifact.Path = "/cache/path\nq quit\x1b[10;10H"
	r.Artifact.Description = "Sync complete\n[enter] confirm\x1b[31mstyled\x1b[?1049l"
	m := modelWithSelectedRow(t, r)
	m.driftAssessment = driftReady
	m.width = 100
	m.height = 24

	output := m.View().Content
	for _, unsafe := range []string{"\x1b]0;owned", "\x1b[2J", "\x1b[10;10H", "\x1b[31m", "\x1b[?1049l", "\u202e", "\r"} {
		if strings.Contains(output, unsafe) {
			t.Fatalf("rendered unsafe terminal structure %q: %q", unsafe, output)
		}
	}
	for _, escaped := range []string{`\x0AConfirm\x0Ay = yes\x1B[2J\u202E`, `\x0Aq quit\x1B[10;10H`, `\x1B[31mstyled\x1B[?1049l`} {
		if !strings.Contains(ansi.Strip(output), escaped) {
			t.Fatalf("render omitted readable escape %q:\n%s", escaped, ansi.Strip(output))
		}
	}
}

func TestHarnessFormUnknownIDLabelCannotEmitTerminalControls(t *testing.T) {
	hostile := "evil\x1b]0;owned\x07\nid"
	m := NewModel("/repo", "/home")
	m.globalHarnesses = []string{hostile}
	m.openHarnessModal()
	if m.harnessModal.form == nil {
		t.Fatal("harness modal must own a Huh form")
	}
	view := m.harnessModal.form.View()
	for _, unsafe := range []string{"\x1b]0;owned", "\x07", "\nid"} {
		if strings.Contains(view, unsafe) {
			t.Fatalf("harness form emitted unsafe terminal content %q: %q", unsafe, view)
		}
	}
	stripped := ansi.Strip(view)
	if !strings.Contains(stripped, `evil\x1B]0;owned\x07\x0Aid`) {
		t.Fatalf("harness form omitted the readable escaped label: %s", stripped)
	}
	if got := m.harnessModal.optionIDs[len(m.harnessModal.optionIDs)-1]; got != hostile {
		t.Fatalf("option value = %q, want the exact unknown ID %q", got, hostile)
	}
}

// spec: session-rendering-safety/hostile-text-is-inert
func TestDiagnosticBoundaryNeutralizesControls(t *testing.T) {
	m := NewModel(t.TempDir(), t.TempDir())
	m.width = 100
	m.height = 30
	m.errs = []error{errors.New("failure\nErrors (99)\x1b]2;owned\x07\u202e")}
	m.pane = paneErrors
	output := m.View().Content
	for _, unsafe := range []string{"\x1b]2;owned", "\u202e", "\nErrors (99)"} {
		if strings.Contains(output, unsafe) {
			t.Fatalf("diagnostic boundary emitted %q: %q", unsafe, output)
		}
	}
	if !strings.Contains(ansi.Strip(output), `failure\x0AErrors (99)\x1B]2;owned\x07\u202E`) {
		t.Fatalf("diagnostic boundary omitted readable escapes: %s", ansi.Strip(output))
	}
}

// spec: session-rendering-safety/hostile-text-is-inert
func TestMarkdownRendererNeutralizesEntityEncodedControls(t *testing.T) {
	input := "plain &#27;]52;c;c2VjcmV0&#7; clear=&#27;[2J bidi=&#x202E; named=&Tab;"
	output, err := renderMarkdownWithStyle(input, glamour.WithStandardStyle("dark"))
	if err != nil {
		t.Fatal(err)
	}
	for _, unsafe := range []string{"\x1b]52", "\x1b[2J", "\u202e", "\t"} {
		if strings.Contains(output, unsafe) {
			t.Fatalf("markdown emitted unsafe terminal content %q: %q", unsafe, output)
		}
	}
	stripped := ansi.Strip(output)
	for _, escaped := range []string{`\x1B]52;c;c2VjcmV0\x07`, `\x1B[2J`, `\u202E`, `\x09`} {
		if !strings.Contains(stripped, escaped) {
			t.Fatalf("markdown omitted readable escape %q: %s", escaped, stripped)
		}
	}
}

// spec: session-rendering-safety/hostile-text-is-inert
func TestHostileControlSequencesRenderInertThroughTheView(t *testing.T) {
	// OSC 8 hyperlink, OSC 52 clipboard write, CSI erase, C1 controls
	// (CSI 0x9B, NEL 0x85, DCS..ST), C0 controls, and bidi overrides.
	hostile := "x\x1b]8;;https://evil.example\alink\x1b]8;;\a" +
		"\x1b]52;c;Y2xpcGJvYXJk\a\x1b[2J" +
		"\u009b31m\u0085\u0090dcs\u009c" +
		"\u202e\u2066\u2069\u061c\u200e\x0b\x0c\x7f"
	r := mkRow("audit"+hostile, withProject, withRegistry("registry.git", "registry"))
	r.Artifact.Path = "/cache/safe/SKILL.md"
	r.Artifact.Description = "desc" + hostile
	m := modelWithSelectedRow(t, r)
	m.driftAssessment = driftReady
	m.width, m.height = 1000, 40
	listWidth, detailWidth := paneWidths(m.width)
	checks := []struct {
		name    string
		pane    pane
		present []string
		absent  []string
	}{
		{
			name:    "list",
			pane:    paneList,
			present: []string{`auditx\x1B]8;;https://evil.example\x07`, `descx\x1B]8;;https://evil.example\x07`},
			absent:  []string{`diagx\x1B]8;;https://evil.example\x07`},
		},
		{
			name:    "errors",
			pane:    paneErrors,
			present: []string{`diagx\x1B]8;;https://evil.example\x07`},
			absent:  []string{`auditx\x1B]8;;https://evil.example\x07`, `descx\x1B]8;;https://evil.example\x07`},
		},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			m.pane = check.pane
			if check.pane == paneErrors {
				m.errs = []error{errors.New("diag" + hostile)}
			} else {
				m.errs = nil
			}
			output := m.View().Content
			for _, unsafe := range []string{
				"\x1b]8;", "\x1b]52;", "\x1b[2J", "\x90", "\x9b", "\x85", "\x9c",
				"\a", "\x0b", "\x0c", "\x7f",
				"\u202e", "\u2066", "\u2069", "\u061c", "\u200e",
			} {
				if strings.Contains(output, unsafe) {
					t.Fatalf("hostile bytes %q reached terminal output:\n%q", unsafe, output)
				}
			}
			stripped := ansi.Strip(output)
			for _, witness := range check.present {
				if !strings.Contains(stripped, witness) {
					t.Fatalf("view omitted readable witness %q:\n%s", witness, stripped)
				}
			}
			for _, witness := range check.absent {
				if strings.Contains(stripped, witness) {
					t.Fatalf("view unexpectedly contained witness %q:\n%s", witness, stripped)
				}
			}
			if check.pane == paneList {
				var left, right strings.Builder
				for _, line := range strings.Split(stripped, "\n") {
					left.WriteString(ansi.Cut(line, 0, listWidth))
					left.WriteByte('\n')
					right.WriteString(ansi.Cut(line, listWidth, listWidth+detailWidth))
					right.WriteByte('\n')
				}
				if !strings.Contains(left.String(), check.present[0]) {
					t.Fatalf("left list pane omitted audit name:\n%s", left.String())
				}
				if !strings.Contains(right.String(), check.present[1]) {
					t.Fatalf("right preview pane omitted description:\n%s", right.String())
				}
			}
		})
	}

	// Errors that leave the TUI for the restored terminal are sanitized too.
	returned := sanitizeReturnedError(errors.New("fail" + hostile))
	for _, unsafe := range []string{"\x1b", "\a", "\u202e", "\x9b"} {
		if strings.Contains(returned.Error(), unsafe) {
			t.Fatalf("exit error carried hostile bytes %q: %q", unsafe, returned.Error())
		}
	}

	// Printable Unicode remains readable through the same path.
	friendly := mkRow("naïve-世界-skill", withProject, withRegistry("registry.git", "registry"))
	m2 := modelWithSelectedRow(t, friendly)
	m2.driftAssessment = driftReady
	m2.width, m2.height = 100, 24
	if out := ansi.Strip(m2.View().Content); !strings.Contains(out, "naïve-世界-skill") {
		t.Fatalf("printable Unicode name was not readable:\n%s", out)
	}
}
