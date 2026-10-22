package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"
	"charm.land/glamour/v2"
	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/compat"
	ltable "charm.land/lipgloss/v2/table"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
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
// rendererKey identifies one cached renderer by the background it renders
// for and its word-wrap width, so models reporting different backgrounds
// keep independent entries in the shared cache.
type rendererKey struct {
	dark  bool
	width int
}

type markdownRendererCache struct {
	mu          sync.Mutex
	initialized bool
	renderers   map[rendererKey]*glamour.TermRenderer
	lru         []rendererKey // keys in oldest → most-recently-used order
}

// cachedRenderers is the production renderer cache, initialised at startup
// (before bubbletea enters alt-screen) by initRenderer.
var cachedRenderers = &markdownRendererCache{renderers: make(map[rendererKey]*glamour.TermRenderer)}

// initRenderer seeds the process-level renderer cache exactly once. Bubble
// Tea initializes and caches Lip Gloss's terminal detection before acquiring
// stdin. Capture that native result as an explicit Glamour style so lazily
// created width-specific renderers never query the terminal after startup.
// Safe to call concurrently; only the first call acts.
func initRenderer() {
	cachedRenderers.init()
}

func (c *markdownRendererCache) init() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.initialized {
		return
	}
	c.initialized = true
	style := "light"
	if compat.HasDarkBackground {
		style = "dark"
	}
	r, err := glamour.NewTermRenderer(glamour.WithStandardStyle(style))
	if err != nil {
		// Fall back to dark style; better than crashing.
		r, err = glamour.NewTermRenderer(glamour.WithStandardStyle("dark"))
		if err != nil {
			return
		}
	}
	// Width 0 is the default-wrap renderer (glamour's built-in 80 columns),
	// kept for ad-hoc callers that render without a viewport width.
	key := rendererKey{dark: compat.HasDarkBackground, width: 0}
	c.renderers[key] = r
	c.lru = append(c.lru, key)
}

// render formats content for the given background at the given word-wrap
// width, creating and caching the per-(background, width) renderer on first
// use. The lock is held across Render so a cached renderer is never used
// concurrently. The key is marked most recently used; creating a renderer
// past maxCachedRenderers evicts the least recently used one.
func (c *markdownRendererCache) render(content string, dark bool, width int) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := rendererKey{dark: dark, width: width}
	r, ok := c.renderers[key]
	if !ok {
		style := "light"
		if dark {
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
		c.renderers[key] = r
	}
	c.touch(key)
	return r.Render(content)
}

// touch records key as most recently used, evicting the least recently used
// renderer when the cache exceeds maxCachedRenderers. Caller holds the lock.
func (c *markdownRendererCache) touch(key rendererKey) {
	kept := c.lru[:0]
	for _, k := range c.lru {
		if k != key {
			kept = append(kept, k)
		}
	}
	c.lru = append(kept, key)
	for len(c.lru) > maxCachedRenderers {
		evict := c.lru[0]
		c.lru = c.lru[1:]
		delete(c.renderers, evict)
	}
}

// keyValueTable renders aligned label/value detail fields through the
// native Lip Gloss table: labels keep the accent style, values the text
// style, and the table owns column alignment.
func keyValueTable(t theme, pairs [][2]string) string {
	rows := make([][]string, 0, len(pairs))
	for _, pair := range pairs {
		rows = append(rows, []string{pair[0], pair[1]})
	}
	// Cells are single-line sanitized values in an unconstrained table, so
	// wrapping can never produce extra lines; disabling it skips the
	// per-cell wrap writer while rendering identical output.
	valueStyle := textStyle(t)
	return ltable.New().
		Border(lipgloss.Border{}).
		BorderTop(false).BorderBottom(false).BorderLeft(false).BorderRight(false).
		BorderHeader(false).BorderColumn(false).BorderRow(false).
		Wrap(false).
		StyleFunc(func(row, col int) lipgloss.Style {
			if col == 0 {
				// The gap keeps the longest label from touching its value.
				return styleDetailLabel.PaddingRight(1)
			}
			return valueStyle
		}).
		Rows(rows...).
		String()
}

func renderDetail(m Model) string {
	r, ok := m.selectedRow()
	if !ok {
		// Cursor is on a section header (or out of range). The detail
		// pane has no skill to describe; show a short hint instead of
		// leaking an unrelated filteredRows entry.
		if _, onSection := m.selectedSectionPath(); onSection {
			return styleDim.Render("Select a resource to see details")
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

	value := textStyle(m.currentTheme()).Render
	// The enclosing detail box already wraps at contentWidth and pads every
	// line to the pane width, so bare lines here render identically without
	// a second wrap/pad pass.
	writeLine := func(line string) {
		b.WriteString(line)
		b.WriteString("\n")
	}

	fields := [][2]string{
		{"Kind:", r.kindLabel()},
		{"Name:", safe(r.Artifact.Name)},
	}
	if !r.IsMalformed {
		if r.Artifact.RegistryURL != "" {
			fields = append(fields, [2]string{"Registry:", safe(r.Artifact.RegistryURL)})
		}
		if r.Artifact.Ref != "" {
			fields = append(fields, [2]string{"Ref:", safe(r.Artifact.Ref)})
		}
		if len(r.Artifact.Tags) > 0 {
			fields = append(fields, [2]string{"Tags:", safe(strings.Join(r.Artifact.Tags, ", "))})
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
			fields = append(fields, [2]string{harnessLabel, safe(harnessValue)})
		}
	}
	fields = append(fields, [2]string{"Status:", safe(r.StatusText())})
	if discovery := r.MarketplaceDiscovery; discovery != nil {
		fields = append(fields,
			[2]string{"Marketplace:", safe(marketplaceEndpointDisplay(discovery.Endpoint))},
			[2]string{"Registry:", safe(discovery.Item.RegistrySlug + " (" + discovery.Item.RegistryID + ")")},
			[2]string{"Revision:", safe(discovery.Item.Revision)},
			[2]string{"Digest:", safe(discovery.Item.ContentDigest)},
			[2]string{"Metadata:", "discovery only; verified during install"},
		)
	}
	if r.Artifact.Path != "" {
		fields = append(fields, [2]string{"Source:", safe(r.Artifact.Path)})
	}
	b.WriteString(keyValueTable(m.currentTheme(), fields))
	b.WriteString("\n")

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
	hint := "enter = full view"
	if rowToggleDenial(r) == toggleEligible {
		hint = "space = toggle · enter = full view"
	}
	b.WriteString(styleDim.Render(hint))

	return b.String()
}

func renderDetailFull(m Model) string {
	r, ok := m.selectedRow()
	if !ok {
		return "No resource selected"
	}
	return renderDetailFullContent(m.currentTheme(), r, m.width)
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

func captureDetailSource(path string, readTargetWith ...previewTargetRead) *detailSourceSnapshot {
	return captureDetailSourceLimit(path, maxDetailSourceBytes, detailTruncationMarker, readTargetWith...)
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

func captureDetailSourceLimit(path string, limit int, marker string, readTargetWith ...previewTargetRead) *detailSourceSnapshot {
	content, truncated, err := readDetailSourceLimit(path, limit, readTargetWith...)
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
func renderDetailFullContent(t theme, r row, width int) string {
	if r.UnavailableReason != "" {
		return "Resource unavailable: " + sanitizeTerminalData(r.UnavailableReason, terminalSingleLine) +
			"\n\nNo verified content is available. Press space to change this selection."
	}
	if r.Artifact.IsMarketplace {
		// Marketplace rows never read a registry-relative source path.
		// Discovery-only rows render bounded index metadata; installed rows
		// render their verified canonical artifact archive.
		if isMarketplaceDetailOnly(r) {
			return renderMarketplaceDetailContent(t, r)
		}
		if isMarketplaceArtifactPath(r.Artifact.Path) {
			tree, err := newMarketplaceArtifactTree(r.Artifact.Path)
			if err != nil {
				return "Error reading resource: " + sanitizeTerminalData(err.Error(), terminalSingleLine)
			}
			return renderBundleEntry(context.Background(), t, tree, tree.selectedPath(), width)
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
		rendered, err := renderMarkdownAtWidth(content, t.dark, width)
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
			rendered, renderErr := renderMarkdownAtWidth(formatted, t.dark, width)
			if renderErr == nil {
				return boundRenderedDetail(rendered)
			}
			return boundRenderedDetail(strings.TrimPrefix(strings.TrimSuffix(formatted, "```"), "```json\n"))
		}
	}

	prepared := prepareDetailSource(filepath.Base(r.Artifact.Path), content)
	rendered, err := renderMarkdownAtWidth(prepared, t.dark, width)
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

func readDetailSourceLimit(path string, limit int, readTargetWith ...previewTargetRead) (content string, truncated bool, err error) {
	target := path
	if info, statErr := os.Lstat(path); statErr == nil && info.IsDir() {
		target = filepath.Join(path, "SKILL.md")
	}
	data, err := readPreviewSource(filepath.Dir(target), target, limit, readTargetWith...)
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
func renderMarketplaceDetailContent(t theme, r row) string {
	safe := func(value string) string { return sanitizeTerminalData(value, terminalSingleLine) }
	value := textStyle(t).Render
	var b strings.Builder
	fields := [][2]string{
		{"Kind:", r.kindLabel()},
		{"Name:", safe(r.Artifact.Name)},
		{"Status:", safe(r.StatusText())},
	}
	if r.Artifact.RegistryURL != "" {
		fields = append(fields, [2]string{"Registry:", safe(r.Artifact.RegistryURL)})
	}
	if r.Artifact.Ref != "" {
		fields = append(fields, [2]string{"Ref:", safe(r.Artifact.Ref)})
	}
	if d := r.MarketplaceDiscovery; d != nil {
		fields = append(fields,
			[2]string{"Marketplace:", safe(marketplaceEndpointDisplay(d.Endpoint))},
			[2]string{"Registry:", safe(d.Item.RegistrySlug + " (" + d.Item.RegistryID + ")")},
		)
		if d.Item.Revision != "" {
			fields = append(fields, [2]string{"Revision:", safe(d.Item.Revision)})
		}
		if d.Item.ContentDigest != "" {
			fields = append(fields, [2]string{"Digest:", safe(d.Item.ContentDigest)})
		}
	}
	b.WriteString(keyValueTable(t, fields))
	b.WriteString("\n")
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
func renderBundleEntry(ctx context.Context, t theme, tr *bundleTree, relative string, width int) string {
	if tr == nil {
		return "Error reading file: no bundle content available"
	}
	if tr.files != nil {
		data, ok := tr.files[relative]
		if !ok {
			if tr.inspection != nil {
				return tr.inspection.renderFile(ctx, t.dark, relative, width)
			}
			return "Error reading file: archive member is not available"
		}
		return renderBundleContent(t.dark, relative, data, len(data) > maxDetailSourceBytes, width)
	}
	if data, ok := tr.virtual[relative]; ok {
		return renderBundleContent(t.dark, relative, data, len(data) > maxDetailSourceBytes, width)
	}
	return renderBundleFile(t.dark, tr.root, relative, width)
}

// renderBundleFile reads exactly one selected bundle file. The relative path
// is validated before joining and the final descriptor is opened without
// following symlinks, keeping an enumeration/read race inside the resource
// root and the regular-file-only preview contract.
func renderBundleFile(dark bool, root, relative string, width int, readTargetWith ...previewTargetRead) string {
	path, err := safeBundlePath(root, relative)
	if err != nil {
		return "Error reading file: " + sanitizeTerminalData(err.Error(), terminalSingleLine)
	}
	data, err := readPreviewSource(root, path, maxDetailSourceBytes, readTargetWith...)
	if err != nil {
		return "Error reading file: " + sanitizeTerminalData(err.Error(), terminalSingleLine)
	}
	return renderBundleContent(dark, relative, data, len(data) > maxDetailSourceBytes, width)
}

// renderBundleContent applies the shared sanitization, per-file truncation
// marker, and bounded glamour rendering for one bundle member's bytes.
func renderBundleContent(dark bool, relative string, data []byte, truncated bool, width int) string {
	if truncated {
		data = data[:maxDetailSourceBytes]
	}
	content := prepareDetailSource(relative, string(data))
	if truncated {
		content += "\n\n> " + detailTruncationMarker + "\n"
		slog.Info("tui bundle preview source truncated", "path", relative, "limit_bytes", maxDetailSourceBytes)
	}
	rendered, err := renderMarkdownAtWidth(content, dark, width)
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
func loadBundleDetailContent(ctx context.Context, t theme, bundle *bundleTree, relative string, generation requestGeneration, width int, keepScroll bool) tea.Cmd {
	return func() tea.Msg {
		return detailContentMsg{
			generation: generation,
			content:    renderBundleEntry(ctx, t, bundle, relative, width),
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
	return renderMarkdownAtWidth(content, compat.HasDarkBackground, 0)
}

// renderMarkdownAtWidth renders markdown content word-wrapped at width via
// the concurrency-safe, width-keyed production renderer cache. Width <= 0
// selects the default-wrap renderer. The content is sanitized after entity
// decoding but before the renderer emits trusted ANSI.
func renderMarkdownAtWidth(content string, dark bool, width int) (string, error) {
	return cachedRenderers.render(sanitizeMarkdownForTerminal(content), dark, width)
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
