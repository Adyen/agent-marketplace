package tui

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/skill"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

// cachedRenderer is a process-level glamour renderer initialised once at
// startup (before bubbletea enters alt-screen). Reusing a single renderer
// avoids the ~5 s stall caused by glamour's WithAutoStyle() calling
// termenv.HasDarkBackground(), which sends an OSC 11 query and blocks until
// the terminal responds or the 5 s timeout expires. Inside bubbletea's
// alt-screen the OSC response is swallowed, so the timeout always fires.
var cachedRenderer *glamour.TermRenderer

// initRenderer creates the glamour TermRenderer exactly once. It must be
// called before bubbletea takes over the terminal (i.e. before
// tea.WithAltScreen). After that, all renderMarkdown calls reuse the
// cached renderer via renderMarkdownWithRenderer.
func initRenderer() {
	if cachedRenderer != nil {
		return
	}
	// WithAutoStyle is safe here because we are still in the foreground
	// terminal — the OSC 11 query will succeed.
	r, err := glamour.NewTermRenderer(glamour.WithAutoStyle())
	if err != nil {
		// Fall back to dark style; better than crashing.
		r, err = glamour.NewTermRenderer(glamour.WithStandardStyle("dark"))
		if err != nil {
			return
		}
	}
	cachedRenderer = r
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

	label := styleDetailLabel.Render
	value := styleDetailValue.Render
	lineStyle := lipgloss.NewStyle().Width(contentWidth)
	writeLine := func(line string) {
		b.WriteString(lineStyle.Render(line))
		b.WriteString("\n")
	}

	writeLine(label("Kind:     ") + value(skill.KindLabel(r.Artifact.Kind)))
	writeLine(label("Name:     ") + value(r.Artifact.Name))

	if !r.IsMalformed {
		if r.Artifact.RegistryURL != "" {
			writeLine(label("Registry: ") + value(r.Artifact.RegistryURL))
		}
		if r.Artifact.Ref != "" {
			writeLine(label("Ref:      ") + value(r.Artifact.Ref))
		}
		if len(r.Artifact.Tags) > 0 {
			writeLine(label("Tags:     ") + value(strings.Join(r.Artifact.Tags, ", ")))
		}
		if len(r.Harnesses) > 0 {
			harnessLabel := "Targets:"
			if r.IsAvailable {
				harnessLabel = "Would target:"
			}
			harnessValue := strings.Join(r.Harnesses, ", ")
			if r.HarnessSource != "" {
				harnessValue += " (" + r.HarnessSource + ")"
			}
			writeLine(label(fmt.Sprintf("%-9s", harnessLabel)) + value(harnessValue))
		}
	}

	writeLine(label("Status:   ") + value(r.StatusText()))

	if r.Artifact.Path != "" {
		writeLine(label("Source:   ") + value(r.Artifact.Path))
	}

	if r.Artifact.Description != "" {
		b.WriteString("\n")
		// Word-wrap the description
		writeLine(value(r.Artifact.Description))
	}

	if r.IsMalformed {
		b.WriteString("\n")
		writeLine(styleGlyphBroken.Render("Error: ") + value(r.MalformedMsg))
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
	return renderDetailFullContent(r)
}

// renderDetailFullContent reads and renders a skill's source file. Used
// internally by loadDetailContent (async tea.Cmd) and by tests. Production
// code should call loadDetailContent instead of blocking the UI thread.
//
// For JSON files (hooks, MCP servers), the content is pretty-printed and
// wrapped in a markdown code block before being rendered through glamour,
// so it displays with syntax highlighting instead of a raw minified blob.
func renderDetailFullContent(r row) string {
	if r.Artifact.Path == "" {
		return "No content available"
	}

	content, err := ReadSkillMarkdown(r.Artifact.Path)
	if err != nil {
		return fmt.Sprintf("Error reading resource: %v", err)
	}

	// If the source file is JSON, pretty-print and wrap in a code block
	// before handing off to the markdown renderer.
	if isJSONPath(r.Artifact.Path) {
		formatted, fmtErr := formatJSONContent(content)
		if fmtErr == nil {
			rendered, renderErr := renderMarkdown(formatted)
			if renderErr == nil {
				return rendered
			}
			// If glamour fails on the code block, return the pretty-printed
			// JSON directly — it's still more readable than the minified form.
			return strings.TrimPrefix(strings.TrimSuffix(formatted, "```"), "```json\n")
		}
	}

	rendered, err := renderMarkdown(content)
	if err != nil {
		return content
	}
	return rendered
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
// glamour renderer. Falls back to raw content on error.
func renderMarkdown(content string) (string, error) {
	if cachedRenderer != nil {
		return cachedRenderer.Render(content)
	}
	// Fallback: should not happen in production (initRenderer was not
	// called), but keep a working path for ad-hoc callers.
	return renderMarkdownWithStyle(content, glamour.WithStandardStyle("dark"))
}

// renderMarkdownWithStyle renders markdown with an explicit glamour style.
// Used in tests where WithAutoStyle falls back to notty (no ANSI output).
func renderMarkdownWithStyle(content string, option glamour.TermRendererOption) (string, error) {
	renderer, err := glamour.NewTermRenderer(option)
	if err != nil {
		return "", err
	}
	return renderer.Render(content)
}
