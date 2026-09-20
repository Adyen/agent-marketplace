package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// longRegistryRow builds an available Marketplace-style row whose registry
// display label is deliberately longer than the narrow list pane.
func longRegistryRow(name, registry string) row {
	r := mkRow(name, withAvailable)
	r.RegistryName = registry
	return r
}

func sectionHeaderLines(t *testing.T, m Model, height int) []string {
	t.Helper()
	var headers []string
	for _, line := range strings.Split(ansi.Strip(renderList(m, height)), "\n") {
		if strings.Contains(line, "▼") || strings.Contains(line, "▶") {
			headers = append(headers, line)
		}
	}
	return headers
}

func TestLongEndpointSectionHeaderKeepsCountOnOneLine(t *testing.T) {
	for _, width := range []int{50, 120} {
		m := NewModel(t.TempDir(), t.TempDir())
		m.loading = false
		m.width = width
		m.height = 24
		endpoint := "https://marketplace.example.com/api/v1/registries/" + strings.Repeat("long-endpoint-", 6)
		m.rows = []row{
			longRegistryRow("alpha", endpoint),
			longRegistryRow("beta", endpoint),
			longRegistryRow("gamma", endpoint),
		}
		m.filteredRows = m.rows
		m.activeTab = tabRegistry
		m.rebuildFlatItems()

		listWidth, _ := paneWidths(width)
		headers := sectionHeaderLines(t, m, 24)
		if len(headers) == 0 {
			t.Fatalf("width %d: no section headers rendered", width)
		}
		for _, header := range headers {
			if ansi.StringWidth(header) > listWidth {
				t.Fatalf("width %d: header exceeds list width %d: %q", width, listWidth, header)
			}
			if !strings.Contains(header, "(3)") {
				t.Fatalf("width %d: section count wrapped onto a stray line: %q", width, header)
			}
		}
	}
}

func TestLongResourceNameWrapsWithoutObscuring(t *testing.T) {
	m := NewModel(t.TempDir(), t.TempDir())
	m.loading = false
	m.width = 50
	m.height = 24
	name := "very-long-marketplace-resource-name-that-wraps-without-loss"
	m.rows = []row{longRegistryRow(name, "endpoint")}
	m.filteredRows = m.rows
	m.rebuildFlatItems()

	out := ansi.Strip(renderList(m, 24))
	listWidth, _ := paneWidths(50)
	for _, line := range strings.Split(out, "\n") {
		if ansi.StringWidth(line) > listWidth {
			t.Fatalf("row line exceeds list width %d: %q", listWidth, line)
		}
	}
	if !strings.Contains(strings.ReplaceAll(out, "\n", ""), name) {
		t.Fatalf("wrapped row obscured the resource name %q:\n%s", name, out)
	}
}

func TestErrorsOverlayPreservesCompleteLongMessages(t *testing.T) {
	long := "registry https://example.com/" + strings.Repeat("nested/path/", 12) + " failed: verification rejected the revision"
	m := NewModel(t.TempDir(), t.TempDir())
	m.width = 50
	m.height = 12
	m.pane = paneErrors
	m.errs = []error{errors.New(long)}

	output := m.View()
	assertTerminalBounds(t, output, 50, 12)

	content := ansi.Strip(renderErrors(m))
	compact := func(s string) string {
		return strings.NewReplacer(" ", "", "\n", "").Replace(s)
	}
	if !strings.Contains(compact(content), compact(long)) {
		t.Fatalf("errors overlay dropped part of the diagnostic:\n%s", content)
	}
	for _, line := range strings.Split(content, "\n") {
		if ansi.StringWidth(line) > 50 {
			t.Fatalf("errors overlay line exceeds width: %q", line)
		}
	}
}

func TestFooterActionsDropWholeActionsNotPartialWords(t *testing.T) {
	actions := "k1:aaaaaaaaaa · k2:bbbbbbbbbb · q:quit"

	if got := ansi.Strip(fitFooterGroups(actions, " · ", 40)); got != actions {
		t.Fatalf("fitting footer changed actions that already fit: %q", got)
	}
	got := ansi.Strip(fitFooterGroups(actions, " · ", 20))
	if got != "q:quit" {
		t.Fatalf("expected only whole actions that fit, got %q", got)
	}
	got = ansi.Strip(fitFooterGroups(actions, " · ", 25))
	if got != "k2:bbbbbbbbbb · q:quit" {
		t.Fatalf("expected whole-action fitting, got %q", got)
	}
	for _, fragment := range []string{"k2:bbbb", "k1:aaaa", "· q"} {
		if strings.Contains(ansi.Strip(fitFooterGroups(actions, " · ", 20)), fragment) {
			t.Fatalf("footer emitted a partial action containing %q", fragment)
		}
	}
}

func TestFooterStatusDropsWholeGroups(t *testing.T) {
	left := "in sync  Marketplace: 2 failed  ! 3 errors"
	got := ansi.Strip(fitFooterGroups(left, "  ", 25))
	if got != "in sync  ! 3 errors" {
		t.Fatalf("expected whole status groups that fit, got %q", got)
	}
	for _, group := range strings.Split(got, "  ") {
		if !strings.Contains(left, group) {
			t.Fatalf("footer emitted a partial status group %q: %q", group, got)
		}
	}
}

func TestCollectionSectionHeadingCapitalization(t *testing.T) {
	items := buildScopeTree([]row{mkRow("tools", withKind(ir.KindCollection), withProject)}, map[string]bool{})
	var titles []string
	for _, it := range items {
		if it.Kind == flatSection {
			titles = append(titles, it.SectionTitle)
		}
	}
	joined := strings.Join(titles, "|")
	if !strings.Contains(joined, "Collections") {
		t.Fatalf("collection section heading not capitalized consistently: %q", joined)
	}
}
