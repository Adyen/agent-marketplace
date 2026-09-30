package tui

import (
	"errors"
	"strings"
	"testing"

	"charm.land/glamour/v2"
	"github.com/charmbracelet/x/ansi"
)

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
