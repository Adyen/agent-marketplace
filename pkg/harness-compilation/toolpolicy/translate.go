package toolpolicy

// Concept-set translation (design D5): a source harness's tool names are
// decoded into the shared concept vocabulary and re-emitted with exact or
// subset coverage on the target, never widened.

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

// Matcher syntaxes, mirroring the values hook catalogs declare. They are
// restated here so toolpolicy does not import hookpolicy.
const (
	MatcherExactOrRegex = "exact-or-regex"
	MatcherRegex        = "regex"
	MatcherNone         = "none"
)

// MatcherSpec describes how one harness reads or writes a hook tool matcher.
// Source side only ExactLists is consulted; the rest shapes target output.
type MatcherSpec struct {
	Syntax     string // MatcherExactOrRegex, MatcherRegex, or MatcherNone
	ExactLists bool   // `|`/`,` lists of plain names are accepted/emitted
	Anchor     bool   // matchers are unanchored; output is always anchored
}

// ErrUntranslatable is returned when a matcher or tool list cannot be
// represented on the target without widening or narrowing the covered set.
var ErrUntranslatable = errors.New("toolpolicy: untranslatable")

// UnknownToolError reports a plain tool name that the source harness's
// catalog does not declare. It wraps ErrUntranslatable. KnownIn names the
// first other harness whose shipped catalog declares the name; it is empty
// when no catalog does, in which case callers must not quote Name.
type UnknownToolError struct {
	Name    string
	Source  string
	KnownIn string
}

func (e *UnknownToolError) Error() string {
	return fmt.Sprintf("%v: %q is not a known %s tool", ErrUntranslatable, e.Name, e.Source)
}

func (e *UnknownToolError) Unwrap() error { return ErrUntranslatable }

// ErrNoSurface reports that the target harness has no such tool surface, so a
// non-empty source list cannot be restricted at all.
var ErrNoSurface = errors.New("toolpolicy: target has no such tool surface")

// legacyAliases maps frozen old IR spellings onto Claude catalog keys, for
// canonical input that carries no source provenance. Mirrors the frozen
// legacy table modelpolicy keeps for old model spellings.
var legacyAliases = map[string]string{
	"Create": "Write",
}

func catalogFor(h target.Target) (catalog, error) {
	r, err := shipped()
	if err != nil {
		return catalog{}, err
	}
	c, ok := r.catalogs[h]
	if !ok {
		return catalog{}, fmt.Errorf("%w: no tool catalog for %s", ErrUntranslatable, h)
	}
	return c, nil
}

// sourceCatalog resolves the catalog used to decode input spelling. Input
// with no provenance (source == "") decodes through the Claude catalog plus
// the frozen legacy alias table.
func sourceCatalog(source target.Target) (catalog, error) {
	if source == "" {
		return catalogFor(target.TargetClaude)
	}
	return catalogFor(source)
}

// conceptsFor resolves one accepted source spelling to its concept set.
// Unknown spellings return ok=false so the caller can report them as
// uncovered rather than guess.
func (c *catalog) conceptsFor(name string, allowLegacy bool) ([]string, bool) {
	key := name
	if allowLegacy {
		if legacy, ok := legacyAliases[name]; ok {
			key = legacy
		}
	}
	canonical, ok := c.native[key]
	if !ok {
		return nil, false
	}
	return c.Tools[canonical].Concepts, true
}

func setContains(set map[string]bool, concepts []string) bool {
	for _, concept := range concepts {
		if !set[concept] {
			return false
		}
	}
	return true
}

func setOverlaps(set map[string]bool, concepts []string) bool {
	for _, concept := range concepts {
		if set[concept] {
			return true
		}
	}
	return false
}

// surfaceTools lists the target tools visible on a surface, in declaration
// order. A tool with no explicit narrowing is visible on every surface the
// catalog declares.
func (c *catalog) surfaceTools(surface string) []string {
	var names []string
	for _, name := range c.order {
		t := c.Tools[name]
		if len(t.Surfaces) == 0 || slices.Contains(t.Surfaces, surface) {
			names = append(names, name)
		}
	}
	return names
}

// DecodedMatcher is a source hook tool matcher reduced to what it selects:
// either every tool, an MCP pattern, or a concept set of native tools.
type DecodedMatcher struct {
	Empty    bool            // no matcher text; selects nothing
	All      bool            // `*`; selects every tool, including MCP
	MCPAll   bool            // all MCP tools on all servers
	Server   string          // MCP server scope when Server-scoped
	Tool     string          // MCP tool name, or "" for all tools on Server
	Concepts map[string]bool // native-tool concept set
}

func plainToolName(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '_' {
			return false
		}
	}
	return true
}

// DecodeMatcher parses a hook tool matcher using the source harness's
// catalog. Anything outside the translatable forms — general regexes,
// unknown names, malformed MCP patterns — returns ErrUntranslatable.
func DecodeMatcher(source target.Target, matcher string, src MatcherSpec) (DecodedMatcher, error) {
	matcher = strings.TrimSpace(matcher)
	if matcher == "" {
		return DecodedMatcher{Empty: true}, nil
	}
	if matcher == "*" || (matcher == ".*" && (src.Syntax == MatcherRegex || src.Syntax == MatcherExactOrRegex)) {
		return DecodedMatcher{All: true}, nil
	}
	c, err := sourceCatalog(source)
	if err != nil {
		return DecodedMatcher{}, err
	}
	legacy := source == ""
	if matcher == c.MCP.All {
		return DecodedMatcher{MCPAll: true}, nil
	}
	if server, tool, ok := parseMCPPattern(c.MCP.Pattern, matcher); ok {
		if tool == "" || tool == ".*" {
			return DecodedMatcher{Server: server}, nil
		}
		if !plainToolName(tool) || (server != "" && !plainToolName(server)) {
			return DecodedMatcher{}, ErrUntranslatable
		}
		return DecodedMatcher{Server: server, Tool: tool}, nil
	}
	inner := matcher
	if strings.HasPrefix(inner, "^") && strings.HasSuffix(inner, "$") {
		inner = inner[1 : len(inner)-1]
		inner = strings.TrimPrefix(strings.TrimSuffix(inner, ")"), "(")
	}
	var names []string
	if strings.Contains(inner, "|") {
		names = strings.Split(inner, "|")
	} else if src.ExactLists && strings.Contains(inner, ",") {
		names = strings.Split(inner, ",")
	} else {
		names = []string{inner}
	}
	set := map[string]bool{}
	for _, name := range names {
		name = strings.TrimSpace(name)
		if !plainToolName(name) {
			return DecodedMatcher{}, ErrUntranslatable
		}
		concepts, ok := c.conceptsFor(name, legacy)
		if !ok {
			return DecodedMatcher{}, &UnknownToolError{Name: name, Source: sourceName(source), KnownIn: knownIn(source, name)}
		}
		for _, concept := range concepts {
			set[concept] = true
		}
	}
	return DecodedMatcher{Concepts: set}, nil
}

// knownIn returns the first harness other than source whose catalog declares
// name, or "" when none does.
func knownIn(source target.Target, name string) string {
	for _, h := range target.Targets() {
		if h == source || (source == "" && h == target.TargetClaude) {
			continue
		}
		if _, ok := ToolConcepts(h, name); ok {
			return string(h)
		}
	}
	return ""
}

func sourceName(source target.Target) string {
	if source == "" {
		return string(target.TargetClaude)
	}
	return string(source)
}

// parseMCPPattern matches value against a catalog's MCP naming template,
// which may contain {server} and {tool} placeholders. It returns the raw
// server and tool captures; tool may itself be a wildcard such as `.*`.
func parseMCPPattern(pattern, value string) (server, tool string, ok bool) {
	if pattern == "" {
		return "", "", false
	}
	serverScoped := strings.Contains(pattern, "{server}")
	var prefix, mid, suffix string
	if serverScoped {
		i := strings.Index(pattern, "{server}")
		prefix = pattern[:i]
		rest := pattern[i+len("{server}"):]
		j := strings.Index(rest, "{tool}")
		mid = rest[:j]
		suffix = rest[j+len("{tool}"):]
	} else {
		i := strings.Index(pattern, "{tool}")
		prefix = pattern[:i]
		suffix = pattern[i+len("{tool}"):]
	}
	if !strings.HasPrefix(value, prefix) || !strings.HasSuffix(value, suffix) {
		return "", "", false
	}
	body := value[len(prefix) : len(value)-len(suffix)]
	if body == "" {
		return "", "", false
	}
	if !serverScoped {
		return "", body, true
	}
	i := strings.Index(body, mid)
	if i <= 0 || i+len(mid) >= len(body) {
		return "", "", false
	}
	return body[:i], body[i+len(mid):], true
}

// selectCovered returns the surface tools whose concept sets lie entirely
// within S, in declaration order. A target tool that only partly overlaps S
// cannot be selectively included, so the whole translation is refused.
func (c *catalog) selectCovered(surface string, set map[string]bool) ([]string, error) {
	var names []string
	covered := map[string]bool{}
	for _, name := range c.surfaceTools(surface) {
		concepts := c.Tools[name].Concepts
		if setContains(set, concepts) {
			names = append(names, name)
			for _, concept := range concepts {
				covered[concept] = true
			}
			continue
		}
		if setOverlaps(set, concepts) && surface == SurfaceHookMatcher {
			return nil, fmt.Errorf("%w: %s tool %q partly overlaps the source set", ErrUntranslatable, c.Harness, name)
		}
	}
	for concept := range set {
		if !covered[concept] {
			return nil, fmt.Errorf("%w: no %s tool covers %q", ErrUntranslatable, c.Harness, concept)
		}
	}
	return names, nil
}

func anchorName(name string) string {
	return "^" + name + "$"
}

// RenderMatcher writes a decoded matcher in the target's matcher syntax. The
// caller supplies the target's spec from its hook catalog (MatcherSpec{} for
// syntax "none" is rejected unless the decoded matcher carries no tool set).
func RenderMatcher(dst target.Target, d DecodedMatcher, spec MatcherSpec) (string, error) {
	if d.Empty {
		return "", nil
	}
	c, err := catalogFor(dst)
	if err != nil {
		return "", err
	}
	anchored := spec.Anchor || spec.Syntax == MatcherRegex
	switch {
	case d.All:
		if spec.Syntax == MatcherExactOrRegex {
			return "*", nil
		}
		return ".*", nil
	case d.MCPAll:
		if anchored {
			return "^" + c.MCP.All, nil
		}
		return c.MCP.All, nil
	case d.Server != "" || d.Tool != "":
		if !c.MCP.ServerScoped || c.MCP.Pattern == "" {
			return "", fmt.Errorf("%w: %s has no server-scoped MCP naming", ErrUntranslatable, dst)
		}
		server := d.Server
		if server == "" {
			return "", fmt.Errorf("%w: source pattern carries no server for %s naming", ErrUntranslatable, dst)
		}
		tool := d.Tool
		if tool == "" {
			tool = ".*"
		}
		rendered := strings.NewReplacer("{server}", server, "{tool}", tool).Replace(c.MCP.Pattern)
		if anchored {
			if tool == ".*" {
				return "^" + rendered, nil
			}
			return "^" + rendered + "$", nil
		}
		return rendered, nil
	}
	names, err := c.selectCovered(SurfaceHookMatcher, d.Concepts)
	if err != nil {
		return "", err
	}
	if spec.Syntax == MatcherNone {
		return "", fmt.Errorf("%w: %s has no matcher field", ErrUntranslatable, dst)
	}
	if len(names) == 1 {
		if anchored {
			return anchorName(names[0]), nil
		}
		return names[0], nil
	}
	if spec.ExactLists && !anchored {
		return strings.Join(names, "|"), nil
	}
	return "^(" + strings.Join(names, "|") + ")$", nil
}

// TranslateMatcher decodes matcher on the source and renders it on the
// target, enforcing exact coverage for hook matchers.
func TranslateMatcher(source, dst target.Target, matcher string, src, dstSpec MatcherSpec) (string, error) {
	d, err := DecodeMatcher(source, matcher, src)
	if err != nil {
		return "", err
	}
	return RenderMatcher(dst, d, dstSpec)
}

// IsToolOnSurface reports whether name is a catalog tool (or accepted alias)
// of harness h visible on the given surface. Ingesters use it to enforce
// closed native vocabularies such as Devin's allowed-tools list.
func IsToolOnSurface(h target.Target, surface, name string) bool {
	c, err := catalogFor(h)
	if err != nil {
		return false
	}
	canonical, ok := c.native[name]
	if !ok {
		return false
	}
	t := c.Tools[canonical]
	return len(t.Surfaces) == 0 || slices.Contains(t.Surfaces, surface)
}

// ToolConcepts resolves one accepted tool spelling of harness h to the
// concepts it expresses. Input without provenance (h == "") decodes through
// the Claude catalog plus the frozen legacy alias table. Unknown spellings
// return ok=false so callers can refuse rather than guess.
func ToolConcepts(h target.Target, name string) ([]string, bool) {
	c, err := sourceCatalog(h)
	if err != nil {
		return nil, false
	}
	concepts, ok := c.conceptsFor(name, h == "")
	return slices.Clone(concepts), ok
}

// TranslateToolList translates a tool allow-list from source spellings to
// target spellings on the given surface. Coverage is subset-only: target
// tools are emitted when every concept they express lies within the source
// set. uncovered lists the source spellings no emitted target tool covers;
// the caller turns each into its existing unknown-tool diagnostic. When the
// target has no such surface, a non-empty source list returns ErrNoSurface.
// overrides maps a canonical (source-catalog key) tool to one target tool
// and is applied per invocation before catalog lookup.
func TranslateToolList(source, dst target.Target, surface string, tools []string, overrides map[string]string) (emitted, uncovered []string, err error) {
	src, err := sourceCatalog(source)
	if err != nil {
		return nil, nil, err
	}
	c, err := catalogFor(dst)
	if err != nil {
		return nil, nil, err
	}
	if source != "" && source == dst {
		// Same-harness lists keep their native spellings as authored,
		// including documented wildcards the catalog does not model as
		// named tools (design D5, "Same-harness tool lists keep native
		// names").
		seen := map[string]bool{}
		for _, name := range tools {
			// Aliased spellings are re-emitted under their canonical
			// catalog name; anything else — including documented
			// wildcards such as `mcp_*` — passes through as authored.
			if canonical, ok := c.native[name]; ok {
				name = canonical
			}
			if !seen[name] {
				seen[name] = true
				emitted = append(emitted, name)
			}
		}
		return emitted, nil, nil
	}
	if !slices.Contains(c.Surfaces, surface) {
		if len(tools) == 0 {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("%w: %s has no %s", ErrNoSurface, dst, surface)
	}
	legacy := source == ""
	emittedSet := map[string]bool{}
	emit := func(name string) {
		if !emittedSet[name] {
			emittedSet[name] = true
			emitted = append(emitted, name)
		}
	}
	total := map[string]bool{}
	for _, name := range tools {
		key := name
		if canonical, ok := src.native[name]; ok {
			key = canonical
		} else if legacy {
			if mapped, ok := legacyAliases[name]; ok {
				key = mapped
			}
		}
		if override, ok := overrides[key]; ok {
			emit(override)
			continue
		}
		concepts, ok := src.conceptsFor(name, legacy)
		if !ok {
			uncovered = append(uncovered, name)
			continue
		}
		set := map[string]bool{}
		for _, concept := range concepts {
			set[concept] = true
			total[concept] = true
		}
		var covered []string
		for _, toolName := range c.surfaceTools(surface) {
			if setContains(set, c.Tools[toolName].Concepts) {
				covered = append(covered, toolName)
			}
		}
		for _, toolName := range covered {
			emit(toolName)
		}
		if len(covered) == 0 {
			uncovered = append(uncovered, name)
		}
	}
	// Tools whose concepts span more than one source entry only qualify via
	// the combined set; append them after the per-tool emissions.
	for _, toolName := range c.surfaceTools(surface) {
		if emittedSet[toolName] {
			continue
		}
		if setContains(total, c.Tools[toolName].Concepts) {
			emit(toolName)
		}
	}
	return emitted, uncovered, nil
}
