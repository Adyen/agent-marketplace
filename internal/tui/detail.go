package tui

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"

	"github.com/Adyen/agent-marketplace/internal/skill"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	tea "github.com/charmbracelet/bubbletea"
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
	if discovery := r.MarketplaceDiscovery; discovery != nil {
		writeLine(label("Marketplace: ") + value(safe(marketplaceEndpointDisplay(discovery.Endpoint))))
		writeLine(label("Registry:    ") + value(safe(discovery.Item.RegistrySlug+" ("+discovery.Item.RegistryID+")")))
		writeLine(label("Revision:    ") + value(safe(discovery.Item.Revision)))
		writeLine(label("Digest:      ") + value(safe(discovery.Item.ContentDigest)))
		writeLine(label("Metadata:    ") + value("discovery only; verified during install"))
	}

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
	maxDetailSourceBytes    = 256 << 10 // per entry
	maxDetailCaptureBytes   = 2 << 20   // across one snapshot refresh
	maxDetailCaptureEntries = 64        // across one snapshot refresh
	maxDetailLineChars      = 1024
	maxDetailRenderedLines  = 5000

	detailRenderLimitMarker    = "[truncated: rendered output exceeds the line limit]"
	detailCaptureOmittedMarker = "[content omitted: snapshot detail capture budget exhausted]"
)

var (
	detailTruncationMarker       = fmt.Sprintf("[truncated: source exceeds %d KiB; showing the first part only]", maxDetailSourceBytes/1024)
	detailBudgetTruncationMarker = fmt.Sprintf("[truncated: snapshot detail capture exceeds %d KiB or %d entries]", maxDetailCaptureBytes/1024, maxDetailCaptureEntries)
)

type detailSourceSnapshot struct {
	content          string
	truncationMarker string
	json             bool
	omitted          bool
	err              error
}

type detailCaptureBudget struct {
	bytes   int
	entries int
}

func captureDetailSource(path string) *detailSourceSnapshot {
	return captureDetailSourceLimit(path, maxDetailSourceBytes, detailTruncationMarker)
}

func (b *detailCaptureBudget) capture(path string) *detailSourceSnapshot {
	if b.entries >= maxDetailCaptureEntries || b.bytes >= maxDetailCaptureBytes {
		return &detailSourceSnapshot{json: isJSONPath(path), omitted: true}
	}
	remaining := maxDetailCaptureBytes - b.bytes
	limit := min(maxDetailSourceBytes, remaining)
	marker := detailTruncationMarker
	if limit < maxDetailSourceBytes {
		marker = detailBudgetTruncationMarker
	}
	snapshot := captureDetailSourceLimit(path, limit, marker)
	b.entries++
	b.bytes += len(snapshot.content)
	return snapshot
}

func captureDetailSourceLimit(path string, limit int, marker string) *detailSourceSnapshot {
	content, truncated, err := readDetailSourceLimit(path, limit)
	if !truncated {
		marker = ""
	}
	return &detailSourceSnapshot{content: content, truncationMarker: marker, json: isJSONPath(path), err: err}
}

func attachDetailSources(rows []row, byPath map[string]*detailSourceSnapshot) {
	for i := range rows {
		rows[i].DetailSource = byPath[rows[i].Artifact.Path]
	}
}

// renderDetailFullContent renders the bounded source copy captured while the
// exact snapshot lease was held at the given word-wrap width. Bundle-backed
// skills retain their split multi-file view; other resources use the captured
// source copy. Production code should call loadDetailContent instead of
// blocking the UI thread.
//
// For JSON files (hooks, MCP servers), the content is pretty-printed and
// wrapped in a markdown code block before being rendered through glamour,
// so it displays with syntax highlighting instead of a raw minified blob.
func renderDetailFullContent(r row, width int) string {
	if r.Artifact.IsMarketplace {
		// Marketplace rows never read a registry-relative source path.
		// Discovery-only rows render bounded index metadata; installed rows
		// render their verified canonical artifact archive.
		if isMarketplaceDetailOnly(r) {
			return renderMarketplaceDetailContent(r)
		}
		if isMarketplaceArtifactPath(r.Artifact.Path) {
			tree, err := newMarketplaceArtifactTree(r.Artifact.Path)
			if err != nil {
				return "Error reading resource: " + sanitizeTerminalData(err.Error(), terminalSingleLine)
			}
			return renderBundleEntry(tree, tree.selectedPath(), width)
		}
	}
	if r.Artifact.Kind == ir.KindSkill && r.Artifact.Path != "" && r.DetailSource == nil {
		sources, err := detailSourcePaths(r)
		if err != nil {
			return "Error reading resource: " + sanitizeTerminalData(err.Error(), terminalSingleLine)
		}
		if len(sources) == 0 {
			return "No content available"
		}

		var bundle strings.Builder
		for i, source := range sources {
			content, truncated, readErr := readDetailSource(source.path)
			if i > 0 {
				bundle.WriteString("\n\n---\n\n")
			}
			if len(sources) > 1 {
				bundle.WriteString("## `")
				bundle.WriteString(detailFileName(source.name))
				bundle.WriteString("`\n\n")
			}
			if readErr != nil {
				bundle.WriteString("Error reading file: ")
				bundle.WriteString(sanitizeTerminalData(readErr.Error(), terminalSingleLine))
				continue
			}
			if truncated {
				content += "\n\n> " + detailTruncationMarker + "\n"
				slog.Info("tui detail source truncated", "limit_bytes", maxDetailSourceBytes)
			}
			if len(sources) > 1 {
				bundle.WriteString(prepareBundleSource(source.name, content))
			} else {
				bundle.WriteString(prepareDetailSource(source.name, content))
			}
		}

		content := bundle.String()
		rendered, err := renderMarkdownAtWidth(content, width)
		if err != nil {
			if len(sources) == 1 && isJSONPath(sources[0].name) {
				// If glamour fails on the code block, return the pretty-printed
				// JSON directly — it's still more readable than the minified form.
				return boundRenderedDetail(strings.TrimPrefix(strings.TrimSuffix(content, "```"), "```json\n"))
			}
			return boundRenderedDetail(content)
		}
		return boundRenderedDetail(rendered)
	}

	if r.DetailSource == nil {
		return "No content available"
	}
	content := r.DetailSource.content
	if r.DetailSource.omitted {
		return detailCaptureOmittedMarker
	}
	if r.DetailSource.err != nil {
		return "Error reading resource: " + sanitizeTerminalData(r.DetailSource.err.Error(), terminalSingleLine)
	}
	if r.DetailSource.truncationMarker != "" {
		content += "\n\n> " + r.DetailSource.truncationMarker + "\n"
		slog.Info("tui detail source truncated", "captured_bytes", len(r.DetailSource.content))
	}

	if r.DetailSource.json {
		formatted, fmtErr := formatJSONContent(content)
		if fmtErr == nil {
			rendered, renderErr := renderMarkdownAtWidth(formatted, width)
			if renderErr == nil {
				return boundRenderedDetail(rendered)
			}
			return boundRenderedDetail(strings.TrimPrefix(strings.TrimSuffix(formatted, "```"), "```json\n"))
		}
	}

	prepared := prepareDetailSource(filepath.Base(r.Artifact.Path), content)
	rendered, err := renderMarkdownAtWidth(prepared, width)
	if err != nil {
		return boundRenderedDetail(prepared)
	}
	return boundRenderedDetail(rendered)
}

type detailSourcePath struct {
	path string
	name string
}

func detailSourcePaths(r row) ([]detailSourcePath, error) {
	if r.Artifact.Kind != ir.KindSkill {
		return []detailSourcePath{{path: r.Artifact.Path, name: filepath.Base(r.Artifact.Path)}}, nil
	}

	info, err := os.Stat(r.Artifact.Path)
	if err != nil {
		return nil, err
	}
	root := r.Artifact.Path
	if !info.IsDir() {
		root = filepath.Dir(root)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}

	var sources []detailSourcePath
	err = filepath.WalkDir(root, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		entryInfo, infoErr := entry.Info()
		if infoErr != nil {
			return infoErr
		}
		if !entryInfo.Mode().IsRegular() {
			return nil
		}
		relative, relErr := filepath.Rel(root, current)
		if relErr != nil {
			return relErr
		}
		if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return fmt.Errorf("file %q escapes resource directory", current)
		}
		sources = append(sources, detailSourcePath{path: current, name: filepath.ToSlash(relative)})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(sources, func(i, j int) bool { return sources[i].name < sources[j].name })
	if len(sources) == 0 {
		return []detailSourcePath{{path: r.Artifact.Path, name: filepath.Base(r.Artifact.Path)}}, nil
	}
	return sources, nil
}

func detailFileName(name string) string {
	name = sanitizeTerminalData(filepath.ToSlash(name), terminalSingleLine)
	return strings.ReplaceAll(name, "`", "'")
}

func prepareDetailSource(name, content string) string {
	content = sanitizeMarkdownForTerminal(content)
	content = splitLongDetailLines(content, maxDetailLineChars)
	if !isJSONPath(name) {
		return content
	}
	formatted, err := formatJSONContent(content)
	if err != nil {
		return content
	}
	return formatted
}

func prepareBundleSource(name, content string) string {
	prepared := prepareDetailSource(name, content)
	if name == "SKILL.md" || strings.EqualFold(filepath.Ext(name), ".md") || isJSONPath(name) {
		return prepared
	}
	return "```text\n" + prepared + "\n```"
}

// readDetailSource reads a resource source file (SKILL.md inside a directory,
// or the file itself for non-skill kinds) with a hard byte cap. truncated is
// true when the file exceeds maxDetailSourceBytes; the returned content is
// cut at the cap so a hostile or corrupted multi-gigabyte source cannot
// exhaust TUI memory or render time.
func readDetailSource(path string) (content string, truncated bool, err error) {
	return readDetailSourceLimit(path, maxDetailSourceBytes)
}

func readDetailSourceLimit(path string, limit int) (content string, truncated bool, err error) {
	target := path
	if info, statErr := os.Lstat(path); statErr == nil && info.IsDir() {
		target = filepath.Join(path, "SKILL.md")
	}
	f, err := os.OpenFile(target, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return "", false, err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return "", false, err
	}
	if !info.Mode().IsRegular() {
		return "", false, fmt.Errorf("source is not a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if err != nil {
		return "", false, err
	}
	if len(data) > limit {
		return string(data[:limit]), true, nil
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

// renderMarketplaceDetailContent renders the bounded metadata view for
// Marketplace rows that have no browsable local content: discovery-only
// results and installed pins whose verified content is unavailable. It shows
// index metadata only — never fabricated source content and no network or
// filesystem lookup — and ends with an actionable next step.
func renderMarketplaceDetailContent(r row) string {
	safe := func(value string) string { return sanitizeTerminalData(value, terminalSingleLine) }
	label := styleDetailLabel.Render
	value := styleDetailValue.Render
	var b strings.Builder
	writeField := func(l, v string) {
		b.WriteString(label(l) + value(v) + "\n")
	}
	writeField("Kind:     ", skill.KindLabel(r.Artifact.Kind))
	writeField("Name:     ", safe(r.Artifact.Name))
	writeField("Status:   ", safe(r.StatusText()))
	if r.Artifact.RegistryURL != "" {
		writeField("Registry: ", safe(r.Artifact.RegistryURL))
	}
	if r.Artifact.Ref != "" {
		writeField("Ref:      ", safe(r.Artifact.Ref))
	}
	if d := r.MarketplaceDiscovery; d != nil {
		writeField("Marketplace: ", safe(marketplaceEndpointDisplay(d.Endpoint)))
		writeField("Registry:    ", safe(d.Item.RegistrySlug+" ("+d.Item.RegistryID+")"))
		if d.Item.Revision != "" {
			writeField("Revision:    ", safe(d.Item.Revision))
		}
		if d.Item.ContentDigest != "" {
			writeField("Digest:      ", safe(d.Item.ContentDigest))
		}
	}
	if r.Artifact.Description != "" {
		b.WriteString("\n" + value(safe(r.Artifact.Description)) + "\n")
	}
	b.WriteString("\n")
	switch {
	case r.MarketplaceDiscovery == nil:
		b.WriteString(value("Verified content is not available locally; run sync to restore it."))
	case r.MarketplaceDiscovery.Item.Availability == mpapi.AvailabilityAvailable:
		b.WriteString(value("Discovery metadata only — content is verified during install.\nPress space to install this exact revision."))
	case r.MarketplaceDiscovery.Item.Availability == mpapi.AvailabilityOperatorExposed:
		b.WriteString(value("Operator-exposed rejection — the assessment remains rejected, but Marketplace policy permits installation.\nPress space to install this exact revision."))
	default:
		b.WriteString(value("This Marketplace resource is not currently installable."))
	}
	return b.String()
}

// renderBundleEntry renders one selected bundle member. Archive-backed trees
// serve bounded verified in-memory content; filesystem trees keep the
// descriptor-based no-follow preview path.
func renderBundleEntry(t *bundleTree, relative string, width int) string {
	if t == nil {
		return "Error reading file: no bundle content available"
	}
	if t.files != nil {
		data, ok := t.files[relative]
		if !ok {
			return "Error reading file: archive member is not available"
		}
		return renderBundleContent(relative, data, len(data) > maxDetailSourceBytes, width)
	}
	return renderBundleFile(t.root, relative, width)
}

// renderBundleFile reads exactly one selected bundle file. The relative path
// is validated before joining and the final descriptor is opened without
// following symlinks, keeping an enumeration/read race inside the resource
// root and the regular-file-only preview contract.
func renderBundleFile(root, relative string, width int) string {
	path, err := safeBundlePath(root, relative)
	if err != nil {
		return "Error reading file: " + sanitizeTerminalData(err.Error(), terminalSingleLine)
	}
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return "Error reading file: " + sanitizeTerminalData(err.Error(), terminalSingleLine)
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return "Error reading file: " + sanitizeTerminalData(err.Error(), terminalSingleLine)
	}
	if !info.Mode().IsRegular() {
		return "Error reading file: source is not a regular file"
	}
	data, err := io.ReadAll(io.LimitReader(f, maxDetailSourceBytes+1))
	if err != nil {
		return "Error reading file: " + sanitizeTerminalData(err.Error(), terminalSingleLine)
	}
	return renderBundleContent(relative, data, len(data) > maxDetailSourceBytes, width)
}

// renderBundleContent applies the shared sanitization, per-file truncation
// marker, and bounded glamour rendering for one bundle member's bytes.
func renderBundleContent(relative string, data []byte, truncated bool, width int) string {
	if truncated {
		data = data[:maxDetailSourceBytes]
	}
	content := prepareDetailSource(relative, string(data))
	if truncated {
		content += "\n\n> " + detailTruncationMarker + "\n"
		slog.Info("tui bundle preview source truncated", "path", relative, "limit_bytes", maxDetailSourceBytes)
	}
	rendered, err := renderMarkdownAtWidth(content, width)
	if err != nil {
		return boundRenderedDetail(content)
	}
	return boundRenderedDetail(rendered)
}

func safeBundlePath(root, relative string) (string, error) {
	if root == "" || relative == "" || filepath.IsAbs(filepath.FromSlash(relative)) {
		return "", fmt.Errorf("invalid bundle path")
	}
	clean := filepath.Clean(filepath.FromSlash(relative))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("bundle path escapes resource root")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve resource root: %w", err)
	}
	candidate := filepath.Join(root, clean)
	rel, err := filepath.Rel(root, candidate)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("bundle path escapes resource root")
	}
	current := root
	parts := strings.Split(clean, string(filepath.Separator))
	for i, part := range parts {
		current = filepath.Join(current, part)
		info, statErr := os.Lstat(current)
		if statErr != nil {
			return "", statErr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("bundle path contains a symlink")
		}
		if i < len(parts)-1 && !info.IsDir() {
			return "", fmt.Errorf("bundle path traverses a non-directory")
		}
	}
	return candidate, nil
}

// loadBundleDetailContent is the asynchronous selected-file preview path.
func loadBundleDetailContent(bundle *bundleTree, relative string, generation requestGeneration, width int, keepScroll bool) tea.Cmd {
	return func() tea.Msg {
		return detailContentMsg{
			generation: generation,
			content:    renderBundleEntry(bundle, relative, width),
			width:      width,
			keepScroll: keepScroll,
			path:       relative,
		}
	}
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
