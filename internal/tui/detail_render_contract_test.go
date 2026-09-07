package tui

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func TestBundlePreviewPreservesSourceWhitespace(t *testing.T) {
	for _, name := range []string{"script.py", "notes.txt", "config.yaml"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			content := "first  line\nsecond line\n\n\n  indented\n    nested  content\n```\n**literal**\nlast line\n"
			if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
			output := ansi.Strip(renderBundleFile(root, name, 80))
			lines := strings.Split(output, "\n")
			for i, line := range lines {
				if strings.TrimSpace(line) != "first  line" {
					continue
				}
				margin := line[:strings.Index(line, "first")]
				want := strings.Split(strings.TrimSuffix(content, "\n"), "\n")
				if len(lines)-i < len(want) {
					t.Fatalf("preview lost source lines:\n%s", output)
				}
				for j, expected := range want {
					got := strings.TrimRight(strings.TrimPrefix(lines[i+j], margin), " ")
					if got != expected {
						t.Errorf("source line %d = %q, want %q", j+1, got, expected)
					}
				}
				return
			}
			t.Fatalf("preview lost the first source line:\n%s", output)
		})
	}
}

func TestDetailSourceAndRenderedOutputAreBounded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "large.md")
	if err := os.WriteFile(path, []byte(strings.Repeat("x", maxDetailSourceBytes+1024)), 0o644); err != nil {
		t.Fatal(err)
	}
	content, truncated, err := readDetailSource(path)
	if err != nil {
		t.Fatal(err)
	}
	if !truncated || len(content) != maxDetailSourceBytes {
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
	initialWidth := 47
	initial := loadDetailContent(r, generation, initialWidth, false)().(detailContentMsg)
	updated, _ := m.Update(initial)
	m = updated.(Model)
	if m.detailRenderWidth != initialWidth {
		t.Fatalf("initial render width = %d, want %d", m.detailRenderWidth, initialWidth)
	}

	updated, cmd := m.Update(tea.WindowSizeMsg{Width: 50, Height: 12})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("resize did not schedule a detail rerender")
	}
	resized := cmd().(detailContentMsg)
	wantWidth := 29
	if resized.width != wantWidth || !resized.keepScroll {
		t.Fatalf("resize message = %+v, want width %d", resized, wantWidth)
	}
	updated, _ = m.Update(resized)
	m = updated.(Model)
	if m.detailRenderWidth != wantWidth {
		t.Fatalf("accepted render width = %d, want %d", m.detailRenderWidth, wantWidth)
	}
	assertTerminalBounds(t, m.View(), 50, 12)
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
	cache := &markdownRendererCache{renderers: make(map[int]*glamour.TermRenderer)}
	cache.init()
	var wait sync.WaitGroup
	for width := 20; width < 40; width++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if _, err := cache.render("# title\n\nbody", width); err != nil {
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
	previous := lipgloss.HasDarkBackground()
	t.Cleanup(func() { lipgloss.SetHasDarkBackground(previous) })

	outputs := make(map[bool]string)
	for _, dark := range []bool{false, true} {
		lipgloss.SetHasDarkBackground(dark)
		cache := &markdownRendererCache{renderers: make(map[int]*glamour.TermRenderer)}
		cache.init()
		for _, width := range []int{0, 40} {
			rendered, err := cache.render("# title", width)
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
