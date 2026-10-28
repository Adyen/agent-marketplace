package tui

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/glamour/v2"
	"charm.land/lipgloss/v2/compat"
	"github.com/Adyen/agent-marketplace/internal/session"
	"github.com/charmbracelet/x/ansi"
)

func TestDetailSourceAndRenderedOutputAreBounded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "large.md")
	if err := os.WriteFile(path, []byte(strings.Repeat("x", session.MaxDetailSourceBytes+1024)), 0o644); err != nil {
		t.Fatal(err)
	}
	content, truncated, err := session.ReadDetailSourceLimit(path, session.MaxDetailSourceBytes)
	if err != nil {
		t.Fatal(err)
	}
	if !truncated || len(content) != session.MaxDetailSourceBytes {
		t.Fatalf("bounded read = %d bytes, truncated=%v", len(content), truncated)
	}

	rendered := boundRenderedDetail(strings.Repeat("line\n", maxDetailRenderedLines+10))
	if lines := strings.Count(rendered, "\n") + 1; lines > maxDetailRenderedLines+1 {
		t.Fatalf("bounded render has %d lines", lines)
	}
	if !strings.Contains(ansi.Strip(rendered), detailRenderLimitMarker) {
		t.Fatal("bounded render omitted truncation marker")
	}
}

func TestLongDetailLinesSplitAtRuneBoundaries(t *testing.T) {
	content := strings.Repeat("界", maxDetailLineChars+20)
	split := splitLongDetailLines(content, maxDetailLineChars)
	parts := strings.Split(split, "\n")
	if len(parts) != 2 {
		t.Fatalf("split produced %d lines, want 2", len(parts))
	}
	for _, part := range parts {
		if len([]rune(part)) > maxDetailLineChars {
			t.Fatalf("line has %d runes", len([]rune(part)))
		}
	}
}

func TestDetailResizeRendersAtCurrentWidth(t *testing.T) {
	path := filepath.Join(t.TempDir(), "detail.md")
	if err := os.WriteFile(path, []byte("# Detail\n\n"+strings.Repeat("content ", 30)), 0o644); err != nil {
		t.Fatal(err)
	}
	r := mkRow("detail", withProject)
	r.Artifact.Path = path
	m := modelWithSelectedRow(t, r)
	m.width = 80
	m.height = 24
	generation := m.beginRequest(requestDetail)
	m.enterDetailPane()
	initial := loadDetailContent(newTheme(true), r, generation, 80, false)().(detailContentMsg)
	updated, _ := m.Update(initial)
	m = updated.(Model)
	if m.detailRenderWidth != 80 {
		t.Fatalf("initial render width = %d", m.detailRenderWidth)
	}

	updated, cmd := m.Update(tea.WindowSizeMsg{Width: 50, Height: 12})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("resize did not schedule a detail rerender")
	}
	resized := cmd().(detailContentMsg)
	if resized.width != 50 || !resized.keepScroll {
		t.Fatalf("resize message = %+v", resized)
	}
	updated, _ = m.Update(resized)
	m = updated.(Model)
	if m.detailRenderWidth != 50 {
		t.Fatalf("accepted render width = %d, want 50", m.detailRenderWidth)
	}
	assertTerminalBounds(t, m.View().Content, 50, 12)
}

func TestWidthMismatchWithoutSourceAppliesDeliveredContent(t *testing.T) {
	r := mkRow("missing-source", withProject)
	r.Artifact.Path = ""
	m := modelWithSelectedRow(t, r)
	m.width = 80
	m.height = 24
	generation := m.beginRequest(requestDetail)
	m.enterDetailPane()
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 50, Height: 12})
	m = updated.(Model)
	updated, cmd := m.Update(detailContentMsg{
		generation: generation,
		content:    "No content available",
		width:      80,
	})
	m = updated.(Model)
	if cmd != nil {
		t.Fatal("source-less detail scheduled an impossible rerender")
	}
	if view := m.detailViewport.View(); !strings.Contains(view, "No content available") {
		t.Fatalf("delivered content was not applied: %q", view)
	}
}

func TestMarkdownRendererCacheIsBoundedAndConcurrentSafe(t *testing.T) {
	cache := &markdownRendererCache{renderers: make(map[rendererKey]*glamour.TermRenderer)}
	cache.init()
	var wait sync.WaitGroup
	for width := 20; width < 40; width++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if _, err := cache.render("# title\n\nbody", true, width); err != nil {
				t.Errorf("render width %d: %v", width, err)
			}
		}()
	}
	wait.Wait()
	if got := len(cache.renderers); got > maxCachedRenderers {
		t.Fatalf("renderer cache size = %d, max %d", got, maxCachedRenderers)
	}
}

func TestMarkdownRendererCacheUsesCachedNativeThemeForEveryWidth(t *testing.T) {
	previous := compat.HasDarkBackground
	t.Cleanup(func() { compat.HasDarkBackground = previous })

	outputs := make(map[bool]string)
	for _, dark := range []bool{false, true} {
		compat.HasDarkBackground = dark
		cache := &markdownRendererCache{renderers: make(map[rendererKey]*glamour.TermRenderer)}
		cache.init()
		for _, width := range []int{0, 40} {
			rendered, err := cache.render("# title", dark, width)
			if err != nil {
				t.Fatalf("render dark=%t width=%d: %v", dark, width, err)
			}
			if ansi.Strip(rendered) == rendered {
				t.Fatalf("render dark=%t width=%d did not use the cached native terminal theme", dark, width)
			}
			outputs[dark] += rendered
		}
	}
	if outputs[false] == outputs[true] {
		t.Fatal("light and dark native terminal themes rendered identically")
	}
}
