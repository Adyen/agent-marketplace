package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/x/ansi"
)

// writeDetailSource writes a markdown source file with lines that exceed the
// narrow test widths so word wrapping is observable.
func writeDetailSource(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "resource.md")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func detailRowWithPath(path string) row {
	r := mkRow("detail-resource", withProject)
	r.Artifact.Path = path
	return r
}

func maxStrippedLineWidth(t *testing.T, rendered string) int {
	t.Helper()
	maxWidth := 0
	for _, line := range strings.Split(ansi.Strip(rendered), "\n") {
		// Viewports and glamour pad lines to the block width with spaces;
		// measure the content, not the padding.
		line = strings.TrimRight(line, " ")
		if w := ansi.StringWidth(line); w > maxWidth {
			maxWidth = w
		}
	}
	return maxWidth
}

// viewportBody returns the stripped viewport content without the trailing
// blank padding the viewport adds to fill its height, so content equality
// does not depend on the viewport's dimensions.
func viewportBody(m Model) string {
	return strings.TrimRight(ansi.Strip(m.detailViewport.View()), "\n ")
}

func TestRenderDetailFullContentWrapsAtActualWidth(t *testing.T) {
	paragraph := "This paragraph is deliberately long enough that it must be word wrapped by the renderer at the requested viewport width rather than clipped by the frame."
	path := writeDetailSource(t, "# Title\n\n"+paragraph+"\n\n"+paragraph+"\n")
	r := detailRowWithPath(path)

	narrow := renderDetailFullContent(r, 40)
	if got := maxStrippedLineWidth(t, narrow); got > 40 {
		t.Fatalf("render at width 40 produced a %d-column line:\n%s", got, ansi.Strip(narrow))
	}

	wide := renderDetailFullContent(r, 100)
	if got := maxStrippedLineWidth(t, wide); got > 100 {
		t.Fatalf("render at width 100 produced a %d-column line:\n%s", got, ansi.Strip(wide))
	}
	// The wider render must actually use the extra width (rewrap), not just
	// reuse the narrow layout.
	if got := maxStrippedLineWidth(t, wide); got <= 40 {
		t.Fatalf("render at width 100 did not use the wider viewport (max line %d):\n%s", got, ansi.Strip(wide))
	}
}

func TestDetailPaneRerendersAtNewWidthOnResize(t *testing.T) {
	// Tall enough to scroll at both 80x24 and 120x40.
	paragraph := strings.Repeat("wrappable detail body text ", 200)
	path := writeDetailSource(t, "# Resize\n\n"+paragraph+"\n")

	m := NewModel(t.TempDir(), t.TempDir())
	seedTestTerminalSize(&m) // 80x24
	m.loading = false
	r := detailRowWithPath(path)
	m.rows = []row{r}
	m.filteredRows = m.rows
	m.rebuildFlatItems()
	for i := range m.flatItems {
		if m.flatItems[i].Kind == flatArtifact {
			m.cursor = i
			break
		}
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if cmd == nil || m.pane != paneDetail {
		t.Fatalf("enter did not open the detail pane: pane=%v", m.pane)
	}
	msg, ok := cmd().(detailContentMsg)
	if !ok {
		t.Fatalf("detail load produced %T, want detailContentMsg", cmd())
	}
	if msg.width != 80 {
		t.Fatalf("initial detail render width = %d, want 80", msg.width)
	}
	if msg.keepScroll {
		t.Fatal("initial detail load must not keep a scroll anchor")
	}
	updated, _ = m.Update(msg)
	m = updated.(Model)
	initialContent := viewportBody(m)

	// Scroll down so the anchor is observable, then resize wider: the pane
	// must schedule a re-render at the new width instead of displaying
	// 80-column-wrapped content in a 120-column viewport.
	for i := 0; i < 5; i++ {
		updated, _ = m.Update(keyPress('j'))
		m = updated.(Model)
	}
	offsetBefore := m.detailViewport.YOffset
	if offsetBefore == 0 {
		t.Fatal("precondition: detail viewport did not scroll")
	}

	updated, cmd = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("resize with the detail pane open did not schedule a width re-render")
	}
	resizeMsg, ok := cmd().(detailContentMsg)
	if !ok {
		t.Fatalf("resize re-render produced %T, want detailContentMsg", cmd())
	}
	if resizeMsg.width != 120 {
		t.Fatalf("resize re-render width = %d, want 120", resizeMsg.width)
	}
	if !resizeMsg.keepScroll {
		t.Fatal("resize re-render must keep the scroll anchor")
	}
	updated, _ = m.Update(resizeMsg)
	m = updated.(Model)

	// Sensible scroll anchor: the pre-resize line offset is kept (clamped to
	// the new content) instead of resetting to the top.
	if m.detailViewport.YOffset != offsetBefore {
		t.Fatalf("resize re-render reset the scroll anchor %d -> %d", offsetBefore, m.detailViewport.YOffset)
	}
	if viewportBody(m) == initialContent {
		t.Fatal("detail content was not re-rendered at the new width")
	}
	if got := maxStrippedLineWidth(t, m.detailViewport.View()); got > 120 {
		t.Fatalf("re-rendered detail exceeds the new width: %d", got)
	}
}

func TestDetailRerenderRejectsStaleWidthGenerations(t *testing.T) {
	paragraph := strings.Repeat("stale generation body text ", 200)
	path := writeDetailSource(t, "# Stale\n\n"+paragraph+"\n")

	m := NewModel(t.TempDir(), t.TempDir())
	seedTestTerminalSize(&m)
	m.loading = false
	r := detailRowWithPath(path)
	m.rows = []row{r}
	m.filteredRows = m.rows
	m.rebuildFlatItems()
	for i := range m.flatItems {
		if m.flatItems[i].Kind == flatArtifact {
			m.cursor = i
			break
		}
	}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	updated, _ = m.Update(cmd())
	m = updated.(Model)
	if got := maxStrippedLineWidth(t, m.detailViewport.View()); got > 80 {
		t.Fatalf("precondition: initial render exceeds its width 80: %d", got)
	}

	// Two rapid resizes: the first re-render completes after the second was
	// requested and must be rejected as stale. Applied renders are observed
	// through the widest rendered line, which is height-independent.
	updated, cmdA := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = updated.(Model)
	updated, cmdB := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = updated.(Model)
	if cmdA == nil || cmdB == nil {
		t.Fatal("each width change must schedule its own re-render")
	}
	msgA := cmdA().(detailContentMsg)
	msgB := cmdB().(detailContentMsg)
	if got := maxStrippedLineWidth(t, msgA.content); got <= 80 {
		t.Fatalf("precondition: width-120 render should exceed 80 columns, got %d", got)
	}

	updated, _ = m.Update(msgA)
	m = updated.(Model)
	if m.detailRenderWidth != 80 {
		t.Fatalf("stale width-120 generation moved the accepted render width to %d", m.detailRenderWidth)
	}
	if got := maxStrippedLineWidth(t, m.detailViewport.View()); got > 80 {
		t.Fatalf("stale width-120 generation replaced the detail content (widest line %d)", got)
	}
	updated, _ = m.Update(msgB)
	m = updated.(Model)
	if m.detailRenderWidth != 100 {
		t.Fatalf("current width-100 generation not accepted: render width %d", m.detailRenderWidth)
	}
	if got := maxStrippedLineWidth(t, m.detailViewport.View()); got <= 80 || got > 100 {
		t.Fatalf("current width-100 generation not applied (widest line %d)", got)
	}
}

// TestDetailContentRejectsWidthMismatchOnActiveGeneration pins the width
// acceptance guard: a render whose generation is still active but whose width
// no longer matches the terminal (the intervening resize began no superseding
// generation — e.g. it dipped below the minimum size) must NOT be applied.
// Accepting it would wrap content at a width the viewport does not have and
// move detailRenderWidth off the on-screen width, suppressing the next
// resize's re-render. The model rejects it and re-renders at the current
// width instead.
func TestDetailContentRejectsWidthMismatchOnActiveGeneration(t *testing.T) {
	paragraph := strings.Repeat("width guard body text ", 200)
	path := writeDetailSource(t, "# Guard\n\n"+paragraph+"\n")

	m := NewModel(t.TempDir(), t.TempDir())
	seedTestTerminalSize(&m) // 80x24
	m.loading = false
	r := detailRowWithPath(path)
	m.rows = []row{r}
	m.filteredRows = m.rows
	m.rebuildFlatItems()
	for i := range m.flatItems {
		if m.flatItems[i].Kind == flatArtifact {
			m.cursor = i
			break
		}
	}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if cmd == nil || m.pane != paneDetail {
		t.Fatalf("enter did not open the detail pane: pane=%v", m.pane)
	}

	// Inject a completion on the ACTIVE generation rendered at a width the
	// terminal no longer has (as if a below-minimum dip happened mid-flight).
	active := m.requests.active(requestDetail)
	mismatched := detailContentMsg{generation: active, content: renderDetailFullContent(r, 120), width: 120, keepScroll: true}
	updated, rerender := m.Update(mismatched)
	m = updated.(Model)

	if m.detailRenderWidth != 80 {
		t.Fatalf("width-120 render on the 80-column terminal moved the accepted width to %d", m.detailRenderWidth)
	}
	if got := maxStrippedLineWidth(t, m.detailViewport.View()); got > 80 {
		t.Fatalf("width-120 render was applied to the 80-column viewport (widest line %d)", got)
	}
	if rerender == nil {
		t.Fatal("the rejected render must be re-requested at the current width")
	}
	rerenderMsg, ok := rerender().(detailContentMsg)
	if !ok {
		t.Fatalf("re-render produced %T, want detailContentMsg", rerender())
	}
	if rerenderMsg.width != 80 {
		t.Fatalf("re-render width = %d, want the current terminal width 80", rerenderMsg.width)
	}

	// The re-render at the current width is accepted normally. keepScroll is
	// set on the re-render and the placeholder viewport was at its bottom, so
	// the anchor lands at the bottom of the long content — assert on the
	// repeated body text and the wrap width rather than the top-of-file H1.
	updated, _ = m.Update(rerenderMsg)
	m = updated.(Model)
	if m.detailRenderWidth != 80 {
		t.Fatalf("accepted render width = %d, want 80", m.detailRenderWidth)
	}
	view := ansi.Strip(m.detailViewport.View())
	if !strings.Contains(view, "width guard body text") {
		t.Fatalf("re-rendered content never reached the viewport:\n%s", view)
	}
	if got := maxStrippedLineWidth(t, m.detailViewport.View()); got > 80 {
		t.Fatalf("accepted re-render exceeds the terminal width: %d", got)
	}
}

// TestProductionRendererCacheIsBounded pins the eviction bound on the
// width-keyed glamour renderer cache: terminal resize storms must not
// accumulate one renderer per observed width forever.
func TestProductionRendererCacheIsBounded(t *testing.T) {
	cache := &markdownRendererCache{renderers: make(map[int]*glamour.TermRenderer)}
	cache.init("dark")

	content := "# Bounded\n\nBody **text**.\n"
	// Fill beyond the bound with distinct widths.
	for width := 40; width < 40+maxCachedRenderers+4; width++ {
		if _, err := cache.render(content, width); err != nil {
			t.Fatalf("render at width %d: %v", width, err)
		}
	}
	cache.mu.Lock()
	got := len(cache.renderers)
	cache.mu.Unlock()
	if got > maxCachedRenderers {
		t.Fatalf("renderer cache holds %d renderers, want <= %d", got, maxCachedRenderers)
	}
}

// TestProductionRendererCacheEvictsLeastRecentlyUsed pins eviction order: a
// re-used width survives while the oldest untouched width is evicted.
func TestProductionRendererCacheEvictsLeastRecentlyUsed(t *testing.T) {
	cache := &markdownRendererCache{renderers: make(map[int]*glamour.TermRenderer)}
	cache.init("dark") // seeds width 0 as the oldest entry

	content := "# LRU\n\nBody text.\n"
	// Fill to the bound: widths 0 (init), 40, 41, ...
	for width := 40; width < 40+maxCachedRenderers-1; width++ {
		if _, err := cache.render(content, width); err != nil {
			t.Fatalf("fill render at width %d: %v", width, err)
		}
	}
	// Touch width 40 so it is no longer the oldest.
	if _, err := cache.render(content, 40); err != nil {
		t.Fatalf("touch render: %v", err)
	}
	// One more width overflows the bound and must evict width 0 (oldest),
	// not the just-used 40.
	if _, err := cache.render(content, 999); err != nil {
		t.Fatalf("overflow render: %v", err)
	}
	cache.mu.Lock()
	_, kept40 := cache.renderers[40]
	_, kept0 := cache.renderers[0]
	size := len(cache.renderers)
	cache.mu.Unlock()
	if size > maxCachedRenderers {
		t.Fatalf("renderer cache holds %d renderers, want <= %d", size, maxCachedRenderers)
	}
	if !kept40 {
		t.Fatal("recently used width 40 was evicted")
	}
	if kept0 {
		t.Fatal("least recently used width 0 was not evicted")
	}
	// Evicted widths are transparently re-created on next use.
	if _, err := cache.render(content, 0); err != nil {
		t.Fatalf("re-render at evicted width: %v", err)
	}
}

func TestDetailRerenderKeepsBottomAnchorWhenScrolledToBottom(t *testing.T) {
	paragraph := strings.Repeat("bottom anchored body text ", 200)
	path := writeDetailSource(t, "# Bottom\n\n"+paragraph+"\n")

	m := NewModel(t.TempDir(), t.TempDir())
	seedTestTerminalSize(&m)
	m.loading = false
	r := detailRowWithPath(path)
	m.rows = []row{r}
	m.filteredRows = m.rows
	m.rebuildFlatItems()
	for i := range m.flatItems {
		if m.flatItems[i].Kind == flatArtifact {
			m.cursor = i
			break
		}
	}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	updated, _ = m.Update(cmd())
	m = updated.(Model)

	updated, _ = m.Update(keyPress('G'))
	m = updated.(Model)
	if !m.detailViewport.AtBottom() {
		t.Fatal("precondition: detail viewport not at bottom")
	}

	updated, cmd = m.Update(tea.WindowSizeMsg{Width: 50, Height: 12})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("resize did not schedule a width re-render")
	}
	updated, _ = m.Update(cmd())
	m = updated.(Model)
	if !m.detailViewport.AtBottom() {
		t.Fatal("bottom-anchored detail scroll lost across width re-render")
	}
}

// TestProductionRendererCacheConcurrentUse hammers the process-level glamour
// renderer cache from many goroutines and widths. Run under -race it proves
// the production cache is synchronized; it also proves cached renders are
// deterministic per width.
func TestProductionRendererCacheConcurrentUse(t *testing.T) {
	initRenderer()
	content := "# Concurrent\n\nConcurrent **rendering** of the shared cache with `code` and lists.\n\n- a\n- b\n"
	constants := make([]string, 8)
	var mu sync.Mutex
	var wg sync.WaitGroup
	errs := make(chan error, 256)
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			width := 40 + g*10
			for i := 0; i < 25; i++ {
				out, err := renderMarkdownAtWidth(content, width)
				if err != nil {
					errs <- err
					return
				}
				mu.Lock()
				if constants[g] == "" {
					constants[g] = out
				} else if constants[g] != out {
					errs <- fmt.Errorf("cached render for width %d changed between calls", width)
				}
				mu.Unlock()
			}
		}(g)
	}
	// Interleave a concurrent initRenderer call: it must be idempotent and
	// race-free with renders.
	wg.Add(1)
	go func() {
		defer wg.Done()
		initRenderer()
	}()
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	for g, rendered := range constants {
		if !strings.Contains(ansi.Strip(rendered), "Concurrent") {
			t.Fatalf("goroutine %d render lost content:\n%s", g, ansi.Strip(rendered))
		}
	}
}

// TestRenderDetailFullContentUsesProductionCacheWidth pins the wiring: the
// detail path renders through the width-keyed production cache, so two
// different widths produce differently wrapped output.
func TestRenderDetailFullContentUsesProductionCacheWidth(t *testing.T) {
	paragraph := strings.Repeat("cache keyed wrap probe ", 30)
	path := writeDetailSource(t, paragraph+"\n")
	r := detailRowWithPath(path)

	narrow := renderDetailFullContent(r, 44)
	wide := renderDetailFullContent(r, 120)
	if narrow == wide {
		t.Fatal("detail renders at widths 44 and 120 are identical: width not keyed")
	}
	if got := maxStrippedLineWidth(t, narrow); got > 44 {
		t.Fatalf("narrow detail render exceeds width: %d", got)
	}
}

// --- Bounded reads, bounded expansion, hostile fixtures ---

func TestReadDetailSourceBoundsLargeFiles(t *testing.T) {
	path := writeDetailSource(t, strings.Repeat("a", maxDetailSourceBytes+4096))
	content, truncated, err := readDetailSource(path)
	if err != nil {
		t.Fatal(err)
	}
	if !truncated {
		t.Fatal("oversized source was not reported as truncated")
	}
	if len(content) != maxDetailSourceBytes {
		t.Fatalf("bounded read returned %d bytes, want cap %d", len(content), maxDetailSourceBytes)
	}

	// The marker must survive into the final render.
	r := detailRowWithPath(path)
	out := ansi.Strip(renderDetailFullContent(r, 80))
	if !strings.Contains(out, "truncated: source exceeds") {
		t.Fatalf("oversized source render lacks the truncation marker:\n%.200s", out)
	}

	// A small file is read completely and untruncated.
	small := writeDetailSource(t, "# small\n")
	content, truncated, err = readDetailSource(small)
	if err != nil {
		t.Fatal(err)
	}
	if truncated || content != "# small\n" {
		t.Fatalf("small source = %q truncated=%t, want full untruncated read", content, truncated)
	}
}

func TestRenderDetailFullContentBoundsRenderedExpansion(t *testing.T) {
	// A tight markdown list renders roughly one line per item, so 6000
	// items expand well past the rendered-line cap.
	path := writeDetailSource(t, strings.Repeat("- item\n", 6000))
	r := detailRowWithPath(path)
	out := renderDetailFullContent(r, 80)
	lines := strings.Split(out, "\n")
	if len(lines) > maxDetailRenderedLines+1 {
		t.Fatalf("rendered expansion = %d lines, want <= %d + marker", len(lines), maxDetailRenderedLines)
	}
	if !strings.Contains(ansi.Strip(out), "truncated: rendered output exceeds the line limit") {
		t.Fatal("bounded render lacks the truncation marker")
	}
}

func TestSplitLongDetailLinesBoundsUnbrokenTokens(t *testing.T) {
	content := "short\n" + strings.Repeat("x", maxDetailLineChars*3+7) + "\nshort again\n"
	split := splitLongDetailLines(content, maxDetailLineChars)
	for line := range strings.Lines(split) {
		line = strings.TrimSuffix(line, "\n")
		if n := utf8.RuneCountInString(line); n > maxDetailLineChars {
			t.Fatalf("split left a %d-char line (limit %d)", n, maxDetailLineChars)
		}
	}
	// Content is preserved modulo the inserted newlines.
	if strings.ReplaceAll(split, "\n", "") != strings.ReplaceAll(content, "\n", "") {
		t.Fatal("splitLongDetailLines changed the content bytes")
	}
	// Multibyte runes are never torn apart.
	multibyte := strings.Repeat("界", maxDetailLineChars+1)
	if got := splitLongDetailLines(multibyte, maxDetailLineChars); !utf8.ValidString(got) {
		t.Fatalf("split produced invalid UTF-8: %q", got[:64])
	}
	// Short content is returned untouched (fast path).
	short := "all\nshort\nlines\n"
	if got := splitLongDetailLines(short, maxDetailLineChars); got != short {
		t.Fatalf("short content changed: %q", got)
	}
}

func TestBoundRenderedDetailClosesStylesBeforeMarker(t *testing.T) {
	rendered := "\x1b[31m" + strings.Repeat("styled line\n", maxDetailRenderedLines+10)
	bounded := boundRenderedDetail(rendered)
	lines := strings.Split(bounded, "\n")
	if len(lines) != maxDetailRenderedLines+1 {
		t.Fatalf("bounded render = %d lines, want %d + marker", len(lines), maxDetailRenderedLines)
	}
	if !strings.HasPrefix(lines[maxDetailRenderedLines], "\x1b[0m") {
		t.Fatalf("truncation marker line does not start with an SGR reset: %q", lines[maxDetailRenderedLines])
	}
	if !strings.Contains(bounded, detailRenderLimitMarker) {
		t.Fatal("bounded render lacks the truncation marker")
	}
}

// hostileDetailFixture combines every untrusted-input class the terminal
// boundary must neutralize: OSC/CSI sequences, C0/C1 controls, tabs,
// carriage returns, structural newlines inside fields, bidi controls,
// terminal-active HTML entities, and invalid UTF-8.
const hostileDetailFixture = "# T\ttle\r\n" +
	"\n" +
	"line with OSC \x1b]0;owned\x07 and CSI \x1b[2J and cursor move \x1b[10;10H\n" +
	"\n" +
	"bidi \u202ereordered\u2066 isolate and NEL \u0085and C1 \u009b\n" +
	"\n" +
	"entities &#27;[2J &NewLine; &#x202E; &Tab;break &#129;\n" +
	"\n" +
	"invalid utf8 \xff\xfe\xc3( lone continuation \x80 end\n"

func TestDetailFinalRenderNeutralizesInvalidUTF8(t *testing.T) {
	path := writeDetailSource(t, "prefix \xff\xfe\xc3( suffix \x80\n")
	r := detailRowWithPath(path)
	out := renderDetailFullContent(r, 50)
	if !utf8.ValidString(out) {
		t.Fatalf("final render is not valid UTF-8: %q", out)
	}
	stripped := ansi.Strip(out)
	for _, want := range []string{`\xFF`, `\xFE`, `\xC3(`, `\x80`} {
		if !strings.Contains(stripped, want) {
			t.Fatalf("invalid UTF-8 bytes not visibly escaped as %q:\n%s", want, stripped)
		}
	}
	// No raw invalid bytes survive into the render.
	for _, raw := range []string{"\xff", "\xfe", "\x80", "\xc3("} {
		if strings.Contains(out, raw) {
			t.Fatalf("final render contains raw invalid byte sequence %q", raw)
		}
	}
}

func TestDetailFinalRenderRegressionFixture(t *testing.T) {
	path := writeDetailSource(t, hostileDetailFixture)
	r := detailRowWithPath(path)
	out := renderDetailFullContent(r, 50)

	if !utf8.ValidString(out) {
		t.Fatalf("final render is not valid UTF-8: %q", out)
	}
	// Hostile raw structures must never reach the terminal: no OSC, no
	// hostile CSI, no raw C1 bytes, no raw bidi controls, no tabs/CR.
	for _, raw := range []string{"\x1b]", "\x1b[2J", "\x1b[10;10H", "\x85", "\x9b", "\u202e", "\u2066", "\t", "\r", "\x7f"} {
		if strings.Contains(out, raw) {
			t.Fatalf("final render contains raw hostile structure %q:\n%q", raw, out)
		}
	}
	stripped := ansi.Strip(out)
	if !utf8.ValidString(stripped) {
		t.Fatalf("stripped final render is not valid UTF-8: %q", stripped)
	}
	// The neutralized forms stay visible and readable.
	for _, want := range []string{`\x1B]0;owned\x07`, `\x1B[2J`, `\x09`, `\x0D`, `\u202E`, `\x85`, `\xFF`} {
		if !strings.Contains(stripped, want) {
			t.Fatalf("final render omitted readable escape %q:\n%s", want, stripped)
		}
	}
	// Trusted chrome text survives; spoofed structure does not create rows.
	if strings.Contains(stripped, "\nline with OSC \n") {
		t.Fatalf("hostile content created structural rows:\n%s", stripped)
	}
}

// TestWideCombiningUnicodeReadableAtViewportWidth pins task 5.7: printable
// wide (标题) and combining (e◌́) characters stay readable, are measured by
// physical display width at the actual viewport width, and rerender safely
// when the width changes.
func TestWideCombiningUnicodeReadableAtViewportWidth(t *testing.T) {
	body := "# 标题 e◌́\n\n" + strings.Repeat("缓存 技能 界面 e◌́ a ", 30) + "\n"
	path := writeDetailSource(t, body)
	r := detailRowWithPath(path)

	for _, width := range []int{40, 80} {
		out := renderDetailFullContent(r, width)
		if !utf8.ValidString(out) {
			t.Fatalf("render at width %d is not valid UTF-8", width)
		}
		stripped := ansi.Strip(out)
		for _, want := range []string{"标题", "e◌́", "缓存"} {
			if !strings.Contains(stripped, want) {
				t.Fatalf("render at width %d lost readable Unicode %q:\n%s", width, want, stripped)
			}
		}
		if got := maxStrippedLineWidth(t, out); got > width {
			t.Fatalf("render at width %d produced a %d-column line (physical display width)", width, got)
		}
	}

	// And through the full model path: a width change rerenders safely with
	// the Unicode content intact.
	m := NewModel(t.TempDir(), t.TempDir())
	seedTestTerminalSize(&m)
	m.loading = false
	m.rows = []row{r}
	m.filteredRows = m.rows
	m.rebuildFlatItems()
	for i := range m.flatItems {
		if m.flatItems[i].Kind == flatArtifact {
			m.cursor = i
			break
		}
	}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	updated, _ = m.Update(cmd())
	m = updated.(Model)
	updated, cmd = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("width change did not schedule a Unicode detail rerender")
	}
	updated, _ = m.Update(cmd())
	m = updated.(Model)
	stripped := ansi.Strip(m.detailViewport.View())
	if !strings.Contains(stripped, "标题") || !strings.Contains(stripped, "e◌́") {
		t.Fatalf("width rerender lost wide/combining Unicode:\n%s", stripped)
	}
	if m.detailRenderWidth != 100 {
		t.Fatalf("accepted render width = %d, want 100", m.detailRenderWidth)
	}
}
