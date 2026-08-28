package tui

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Adyen/agent-marketplace/internal/skill"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

// maxCachedRenderers bounds the width-keyed renderer cache. Terminal resize
// storms would otherwise accumulate one glamour renderer per observed width
// for the life of the process; a small LRU keeps the hot widths and evicts
// the rest. Evicted widths are transparently re-created on next use.
const maxCachedRenderers = 8

// markdownRendererCache is the process-level cache of glamour renderers
// keyed by word-wrap width. Detail content renders at the actual viewport
// width, so one renderer per recently observed width is kept; renderers are
// created lazily and reused to avoid re-parsing the style definition per
// render, and the least recently used width is evicted past
// maxCachedRenderers. The mutex guards the map, the recency order, and the
// Render call itself: goldmark/glamour do not document a renderer as safe
// for concurrent use, and detail loads run on background tea.Cmd goroutines.
type markdownRendererCache struct {
	mu          sync.Mutex
	style       string
	initialized bool
	renderers   map[int]*glamour.TermRenderer
	lru         []int // widths in oldest → most-recently-used order
}

// cachedRenderers is the production renderer cache, initialised at startup
// (before bubbletea enters alt-screen) by initRenderer.
var cachedRenderers = &markdownRendererCache{renderers: make(map[int]*glamour.TermRenderer)}

// initRenderer seeds the process-level renderer cache exactly once. The
// style is an explicit trusted choice derived from the process environment
// only (see terminal_guard.go): glamour's WithAutoStyle actively probes the
// terminal with an OSC 11 query, whose response is swallowed once Bubble
// Tea owns input — stalling the first render for the full query timeout.
// Safe to call concurrently; only the first call acts.
func initRenderer() {
	cachedRenderers.init(glamourStyleForBackground(detectDarkBackgroundFromEnv()))
}

func (c *markdownRendererCache) init(style string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.initialized {
		return
	}
	c.initialized = true
	c.style = style
	r, err := glamour.NewTermRenderer(glamour.WithStandardStyle(style))
	if err != nil {
		// Fall back to dark style; better than crashing.
		r, err = glamour.NewTermRenderer(glamour.WithStandardStyle("dark"))
		if err != nil {
			return
		}
		c.style = "dark"
	}
	// Width 0 is the default-wrap renderer (glamour's built-in 80 columns),
	// kept for ad-hoc callers that render without a viewport width.
	c.renderers[0] = r
	c.lru = append(c.lru, 0)
}

// render formats content at the given word-wrap width, creating and caching
// the per-width renderer on first use. The lock is held across Render so a
// cached renderer is never used concurrently. The width is marked most
// recently used; creating a renderer past maxCachedRenderers evicts the least
// recently used width.
func (c *markdownRendererCache) render(content string, width int) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	r, ok := c.renderers[width]
	if !ok {
		style := c.style
		if style == "" {
			style = "dark"
		}
		options := []glamour.TermRendererOption{glamour.WithStandardStyle(style)}
		if width > 0 {
			options = append(options, glamour.WithWordWrap(width))
		}
		var err error
		r, err = glamour.NewTermRenderer(options...)
		if err != nil {
			return "", err
		}
		c.renderers[width] = r
	}
	c.touch(width)
	return r.Render(content)
}

// touch records width as most recently used, evicting the least recently used
// renderer when the cache exceeds maxCachedRenderers. Caller holds the lock.
func (c *markdownRendererCache) touch(width int) {
	kept := c.lru[:0]
	for _, w := range c.lru {
		if w != width {
			kept = append(kept, w)
		}
	}
	c.lru = append(kept, width)
	for len(c.lru) > maxCachedRenderers {
		evict := c.lru[0]
		c.lru = c.lru[1:]
		delete(c.renderers, evict)
	}
}

func renderDetail(m Model) string {
	r, ok := m.selectedRow()
	if !ok {
		// Cursor is on a section header (or out of range). The detail
		// pane has no skill to describe; show a short hint instead of
		// leaking an unrelated filteredRows entry.
		hintStyle := lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#666666", Dark: "#888888"})
		if _, onSection := m.selectedSectionPath(); onSection {
			return hintStyle.Render("Select a resource to see details")
		}
		return ""
	}

	_, detailWidth := paneWidths(m.width)
	contentWidth := detailWidth - 1
	if contentWidth < 1 {
		return ""
	}

	var b strings.Builder
	safe := func(value string) string { return sanitizeTerminalData(value, terminalSingleLine) }

	label := styleDetailLabel.Render
	value := styleDetailValue.Render
	lineStyle := lipgloss.NewStyle().Width(contentWidth)
	writeLine := func(line string) {
		b.WriteString(lineStyle.Render(line))
		b.WriteString("\n")
	}

	writeLine(label("Kind:     ") + value(skill.KindLabel(r.Artifact.Kind)))
	writeLine(label("Name:     ") + value(safe(r.Artifact.Name)))

	if !r.IsMalformed {
		if r.Artifact.RegistryURL != "" {
			writeLine(label("Registry: ") + value(safe(r.Artifact.RegistryURL)))
		}
		if r.Artifact.Ref != "" {
			writeLine(label("Ref:      ") + value(safe(r.Artifact.Ref)))
		}
		if len(r.Artifact.Tags) > 0 {
			writeLine(label("Tags:     ") + value(safe(strings.Join(r.Artifact.Tags, ", "))))
		}
		if len(r.Harnesses) > 0 {
			harnessLabel := "Targets:"
			if r.IsAvailable {
				harnessLabel = "Would target:"
			}
			harnessValue := harnessListLabel(r.Harnesses, ", ")
			if r.HarnessSource != "" {
				harnessValue += " (" + r.HarnessSource + ")"
			}
			writeLine(label(fmt.Sprintf("%-9s", harnessLabel)) + value(safe(harnessValue)))
		}
	}

	writeLine(label("Status:   ") + value(safe(r.StatusText())))

	if r.Artifact.Path != "" {
		writeLine(label("Source:   ") + value(safe(r.Artifact.Path)))
	}

	if r.Artifact.Description != "" {
		b.WriteString("\n")
		// Word-wrap the description
		writeLine(value(safe(r.Artifact.Description)))
	}

	if r.IsMalformed {
		b.WriteString("\n")
		writeLine(styleGlyphBroken.Render("Error: ") + value(safe(r.MalformedMsg)))
	}

	// Hint line
	b.WriteString("\n")
	hintStyle := lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#666666", Dark: "#888888"})
	hint := "enter = full view"
	if !r.IsAvailable && !r.IsMalformed {
		hint = "space = toggle · enter = full view"
	}
	b.WriteString(lineStyle.Render(hintStyle.Render(hint)))

	return b.String()
}

func renderDetailFull(m Model) string {
	r, ok := m.selectedRow()
	if !ok {
		return "No resource selected"
	}
	return renderDetailFullContent(r, m.width)
}

// Bounds for the full detail view, all documented in
// docs/tui-product-contract.md:
//   - maxDetailSourceBytes caps the source read; larger sources are cut and
//     carry a visible truncation marker.
//   - maxDetailLineChars hard-splits unbroken lines before rendering:
//     glamour's word wrap is superlinear in unbroken-token length (a single
//     100 KB token renders in ~20 s), so a minified/hostile file would
//     otherwise pin a renderer goroutine for minutes.
//   - maxDetailRenderedLines caps the rendered expansion with a marker.
const (
	maxDetailSourceBytes   = 256 << 10 // 256 KiB of markdown/JSON source
	maxDetailLineChars     = 1024
	maxDetailRenderedLines = 5000

	detailRenderLimitMarker = "[truncated: rendered output exceeds the line limit]"
)

var detailTruncationMarker = fmt.Sprintf("[truncated: source exceeds %d KiB; showing the first part only]", maxDetailSourceBytes/1024)

// renderDetailFullContent reads and renders a resource's source file at the
// given word-wrap width (the actual detail viewport width in production).
// Used internally by loadDetailContent (async tea.Cmd) and by tests.
// Production code should call loadDetailContent instead of blocking the UI
// thread.
//
// For JSON files (hooks, MCP servers), the content is pretty-printed and
// wrapped in a markdown code block before being rendered through glamour,
// so it displays with syntax highlighting instead of a raw minified blob.
func renderDetailFullContent(r row, width int) string {
	if r.Artifact.Path == "" {
		return "No content available"
	}

	content, truncated, err := readDetailSource(r.Artifact.Path)
	if err != nil {
		return "Error reading resource: " + sanitizeTerminalData(err.Error(), terminalSingleLine)
	}
	if truncated {
		content += "\n\n> " + detailTruncationMarker + "\n"
		slog.Info("tui detail source truncated", "limit_bytes", maxDetailSourceBytes)
	}
	content = sanitizeMarkdownForTerminal(content)
	content = splitLongDetailLines(content, maxDetailLineChars)

	// If the source file is JSON, pretty-print and wrap in a code block
	// before handing off to the markdown renderer.
	if isJSONPath(r.Artifact.Path) {
		formatted, fmtErr := formatJSONContent(content)
		if fmtErr == nil {
			rendered, renderErr := renderMarkdownAtWidth(formatted, width)
			if renderErr == nil {
				return boundRenderedDetail(rendered)
			}
			// If glamour fails on the code block, return the pretty-printed
			// JSON directly — it's still more readable than the minified form.
			return boundRenderedDetail(strings.TrimPrefix(strings.TrimSuffix(formatted, "```"), "```json\n"))
		}
	}

	rendered, err := renderMarkdownAtWidth(content, width)
	if err != nil {
		return boundRenderedDetail(content)
	}
	return boundRenderedDetail(rendered)
}

// readDetailSource reads a resource source file (SKILL.md inside a directory,
// or the file itself for non-skill kinds) with a hard byte cap. truncated is
// true when the file exceeds maxDetailSourceBytes; the returned content is
// cut at the cap so a hostile or corrupted multi-gigabyte source cannot
// exhaust TUI memory or render time.
func readDetailSource(path string) (content string, truncated bool, err error) {
	target := path
	if info, statErr := os.Stat(path); statErr == nil && info.IsDir() {
		target = filepath.Join(path, "SKILL.md")
	}
	f, err := os.Open(target)
	if err != nil {
		return "", false, err
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, maxDetailSourceBytes+1))
	if err != nil {
		return "", false, err
	}
	if len(data) > maxDetailSourceBytes {
		return string(data[:maxDetailSourceBytes]), true, nil
	}
	return string(data), false, nil
}

// splitLongDetailLines hard-splits unbroken lines longer than maxLine at
// rune boundaries, after sanitization (so the input is valid UTF-8 free of
// terminal controls). The split is visual-only: it inserts a plain newline
// into the markdown so the renderer's word wrap stays linear-cost for
// minified or hostile sources.
func splitLongDetailLines(content string, maxLine int) string {
	if maxLine < 1 {
		return content
	}
	tooLong := false
	lineLen := 0
	for _, r := range content {
		if r == '\n' {
			lineLen = 0
			continue
		}
		lineLen++
		if lineLen > maxLine {
			tooLong = true
			break
		}
	}
	if !tooLong {
		return content
	}
	var b strings.Builder
	b.Grow(len(content) + len(content)/maxLine + 1)
	lineLen = 0
	for _, r := range content {
		if r == '\n' {
			b.WriteByte('\n')
			lineLen = 0
			continue
		}
		if lineLen >= maxLine {
			b.WriteByte('\n')
			lineLen = 0
		}
		b.WriteRune(r)
		lineLen++
	}
	return b.String()
}

// boundRenderedDetail caps the rendered expansion at maxDetailRenderedLines.
// Glamour closes its style sequences per line, so cutting whole lines cannot
// strand an open SGR; an explicit reset is appended before the marker as a
// belt-and-suspenders guard for future renderer changes.
func boundRenderedDetail(rendered string) string {
	lines := strings.Split(rendered, "\n")
	if len(lines) <= maxDetailRenderedLines {
		return rendered
	}
	slog.Info("tui detail render truncated", "limit_lines", maxDetailRenderedLines)
	lines = lines[:maxDetailRenderedLines]
	lines = append(lines, "\x1b[0m"+styleDim.Render(detailRenderLimitMarker))
	return strings.Join(lines, "\n")
}

// isJSONPath returns true when the file extension suggests JSON content
// (e.g. hooks.json, mcp.json). This covers hooks and MCP server resources
// whose source files are fully JSON.
func isJSONPath(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".json")
}

// formatJSONContent parses raw JSON and returns it pretty-printed inside a
// markdown fenced code block (```json … ```). Returns an error if the
// content is not valid JSON, allowing the caller to fall back to raw
// rendering.
func formatJSONContent(raw string) (string, error) {
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return "", fmt.Errorf("format JSON: %w", err)
	}
	pretty, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", fmt.Errorf("indent JSON: %w", err)
	}
	return "```json\n" + string(pretty) + "\n```", nil
}

// renderMarkdown renders markdown content using the process-level cached
// glamour renderer at the default wrap width (glamour's built-in 80 columns).
func renderMarkdown(content string) (string, error) {
	return renderMarkdownAtWidth(content, 0)
}

// renderMarkdownAtWidth renders markdown content word-wrapped at width via
// the concurrency-safe, width-keyed production renderer cache. Width <= 0
// selects the default-wrap renderer. The content is sanitized after entity
// decoding but before the renderer emits trusted ANSI.
func renderMarkdownAtWidth(content string, width int) (string, error) {
	return cachedRenderers.render(sanitizeMarkdownForTerminal(content), width)
}

// renderMarkdownWithStyle renders markdown with an explicit glamour style.
// Used in tests where WithAutoStyle falls back to notty (no ANSI output).
func renderMarkdownWithStyle(content string, option glamour.TermRendererOption) (string, error) {
	renderer, err := glamour.NewTermRenderer(option)
	if err != nil {
		return "", err
	}
	return renderer.Render(sanitizeMarkdownForTerminal(content))
}
