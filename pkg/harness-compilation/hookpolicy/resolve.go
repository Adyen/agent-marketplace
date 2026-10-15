package hookpolicy

// Pure hook resolver (design D4/D6): decodes a hook's source-native event,
// matcher, timeout, handler type, and command through the shipped catalogs
// and encodes them for a target, never widening a restriction and never
// quoting a payload in a diagnostic.

import (
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/toolpolicy"
)

// Scope is the installation scope a hook is resolved for. Repository scope
// uses the target's documented repository base; machine scope uses absolute
// paths.
type Scope string

const (
	ScopeRepository Scope = "repository"
	ScopeMachine    Scope = "machine"
)

// Resolved is the target-native view of one hook. When a diagnostic with
// error severity is present the hook is suppressed and the scalar fields are
// meaningless.
type Resolved struct {
	Event       string            // native event name on the target
	Matcher     string            // native matcher text (may be empty)
	Timeout     *int              // timeout in the target's native unit
	Type        string            // handler type (unchanged)
	Command     string            // rewritten command
	Diagnostics []diag.Diagnostic // value-free losses and refusals
}

// Failed reports whether the resolution suppressed the hook.
func (r Resolved) Failed() bool { return diag.HasErrors(r.Diagnostics) }

// Per-target error codes the spec says existing targets keep; other targets
// use the shared generic code.
var eventUnmappableCodes = map[target.Target]string{
	target.TargetGemini:   diag.CodeHookGeminiEventUnmappable,
	target.TargetWindsurf: diag.CodeHookWindsurfEventUnmappable,
	target.TargetDevin:    diag.CodeHookDevinEventUnmappable,
	target.TargetCodex:    diag.CodeHookCodexUnsupported,
}

var typeUnsupportedCodes = map[target.Target]string{
	target.TargetFactory:  diag.CodeHookFactoryNonCommandTypeDropped,
	target.TargetCursor:   diag.CodeHookCursorUnsupportedType,
	target.TargetGemini:   diag.CodeHookGeminiUnsupportedType,
	target.TargetWindsurf: diag.CodeHookWindsurfUnsupportedType,
	target.TargetDevin:    diag.CodeHookDevinUnsupportedType,
	target.TargetCodex:    diag.CodeHookCodexUnsupported,
}

func eventUnmappableCode(dst target.Target) string {
	if code, ok := eventUnmappableCodes[dst]; ok {
		return code
	}
	return diag.CodeHookEventUnmappable
}

func typeUnsupportedCode(dst target.Target, hookType string) string {
	if hookType == "prompt" {
		return diag.CodeHookPromptUnsupported
	}
	if code, ok := typeUnsupportedCodes[dst]; ok {
		return code
	}
	return diag.CodeHookTypeUnsupported
}

func timeoutUnsupportedCode(dst target.Target) string {
	if dst == target.TargetWindsurf {
		return diag.CodeHookWindsurfTimeoutUnsupported
	}
	return diag.CodeHookTimeoutUnsupported
}

func timeoutCappedCode(dst target.Target) string {
	if dst == target.TargetCodex {
		return diag.CodeCodexFieldDropped
	}
	return diag.CodeHookTimeoutCapped
}

func matcherSpec(c catalog) toolpolicy.MatcherSpec {
	return toolpolicy.MatcherSpec{Syntax: c.Matcher.Syntax, Anchor: c.Matcher.Anchor, ExactLists: c.Matcher.ExactLists}
}

// IsDeclaredEvent reports whether the shipped hook catalog for h declares the
// native event name. Ingesters use it to flag events outside the documented
// vocabulary while preserving their native spelling.
func IsDeclaredEvent(h target.Target, event string) bool {
	r, err := shipped()
	if err != nil {
		return false
	}
	c, ok := r.catalogs[h]
	if !ok {
		return false
	}
	_, ok = c.Events[event]
	return ok
}

// NativeEventMatcherKind reports the catalog matcher kind for the target's
// declared native event (MatcherKindTool, MatcherKindLifecycle,
// MatcherKindCommandText, or MatcherKindNone). It returns "" when the event
// is undeclared.
func NativeEventMatcherKind(h target.Target, event string) string {
	r, err := shipped()
	if err != nil {
		return ""
	}
	c, ok := r.catalogs[h]
	if !ok {
		return ""
	}
	ev, ok := c.Events[event]
	if !ok {
		return ""
	}
	return ev.Matcher
}

// Resolve translates hook h for the target harness. supportBase is the
// opaque installed support-directory path the caller materialized (repository
// relative at repository scope, absolute at machine scope); plugin-root
// references in the command resolve against it.
func Resolve(h *ir.Hook, dst target.Target, scope Scope, supportBase string) Resolved {
	res := Resolved{Type: h.Type, Command: h.Command, Timeout: h.Timeout}
	fail := func(code, message, hint string) bool {
		res.Diagnostics = append(res.Diagnostics, diag.Diagnostic{
			Code:     code,
			Severity: diag.SeverityError,
			Target:   string(dst),
			Resource: h.Name(),
			Message:  message,
			Hint:     hint,
			Source:   diag.SourceRef(h.Source),
		})
		return false
	}
	warn := func(code, message, hint string) {
		res.Diagnostics = append(res.Diagnostics, diag.Diagnostic{
			Code:     code,
			Severity: diag.SeverityWarning,
			Target:   string(dst),
			Resource: h.Name(),
			Message:  message,
			Hint:     hint,
			Source:   diag.SourceRef(h.Source),
		})
	}

	r, err := shipped()
	if err != nil {
		fail(diag.CodeHookEventUnmappable, "Hook catalogs failed validation; hook suppressed", "")
		return res
	}
	src := target.Target(h.HookSource)
	if src == "" {
		src = target.TargetClaude
	}
	srcCat, ok := r.catalogs[src]
	if !ok {
		fail(eventUnmappableCode(dst), "Hook source harness has no shipped hook catalog; hook suppressed", "")
		return res
	}
	dstCat, ok := r.catalogs[dst]
	if !ok {
		fail(eventUnmappableCode(dst), "Target has no shipped hook catalog; hook suppressed", "")
		return res
	}

	if h.HookSource != "" && src == dst {
		// Same-harness output keeps the native spelling subject to the
		// handler-type, plugin-root, and timeout-refusal rules.
		res.Event = h.Event
		res.Matcher = h.Matcher
		dstEvent, declared := dstCat.Events[h.Event]
		if !declared {
			fail(eventUnmappableCode(dst), "The event has no exact catalog equivalent on the target; hook suppressed", "Use an event the target documents")
			return res
		}
		if !slices.Contains(dstEvent.Types, h.Type) {
			fail(typeUnsupportedCode(dst, h.Type), "The target catalog does not declare this handler type for the event; hook suppressed", "Use a documented handler type")
			return res
		}
		if !resolveTimeout(h, dst, srcCat.Timeout, dstCat.Timeout, dstEvent, true, &res, warn, fail) {
			return res
		}
		rewriteCommand(h, srcCat, dstCat, res.Event, scope, supportBase, true, r.pluginNames, &res, fail)
		return res
	}

	// Event decoding and target event selection. Event-less hooks
	// (resolver-level env fixtures) skip event, matcher, and type handling;
	// Compile-level resources always carry an event.
	var dstEvent event
	if h.Event != "" {
		srcEvent, declared := srcCat.Events[h.Event]
		if !declared || srcEvent.Canonical == "" {
			fail(eventUnmappableCode(dst), "The event has no exact catalog equivalent on the target; hook suppressed", "Use an event the target documents")
			return res
		}
		if h.Matcher != "" && (srcEvent.Matcher == "" || srcEvent.Matcher == MatcherKindNone) {
			fail(diag.CodeHookMatcherUntranslatable, "The source event has no documented matcher; hook suppressed", "Remove the matcher or use a source event that supports it")
			return res
		}
		name, ev, found := selectEvent(dstCat, srcEvent.Canonical, h, srcCat, srcEvent)
		if !found {
			fail(eventUnmappableCode(dst), "The event has no exact catalog equivalent on the target; hook suppressed", "Use an event the target documents")
			return res
		}
		res.Event = name
		dstEvent = ev

		if len(dstEvent.Tools) != 0 || dstEvent.MCP {
			// The event name encodes the tool selection (Windsurf); the
			// matcher was consumed by event selection and emits nothing.
			res.Matcher = ""
		} else if !resolveMatcher(h, dst, srcCat, dstCat, srcEvent, dstEvent, &res, fail) {
			return res
		}
		if !slices.Contains(dstEvent.Types, h.Type) {
			fail(typeUnsupportedCode(dst, h.Type), "The target catalog does not declare this handler type for the event; hook suppressed", "Use a documented handler type")
			return res
		}
	}

	if !resolveTimeout(h, dst, srcUnit(srcCat), dstCat.Timeout, dstEvent, false, &res, warn, fail) {
		return res
	}
	rewriteCommand(h, srcCat, dstCat, res.Event, scope, supportBase, false, r.pluginNames, &res, fail)
	return res
}

// selectEvent picks the target event for a canonical event name. Targets that
// encode the tool set in the event name (Windsurf) choose the split entry
// whose declared tool set equals the decoded matcher concept set.
func selectEvent(dstCat catalog, canonical string, h *ir.Hook, srcCat catalog, srcEvent event) (string, event, bool) {
	type candidate struct {
		name string
		ev   event
	}
	var plain *candidate
	var splits []candidate
	for _, name := range slices.Sorted(maps.Keys(dstCat.Events)) {
		ev := dstCat.Events[name]
		if ev.Canonical != canonical {
			continue
		}
		if len(ev.Tools) == 0 && !ev.MCP {
			c := candidate{name, ev}
			plain = &c
			continue
		}
		splits = append(splits, candidate{name, ev})
	}
	if plain != nil {
		return plain.name, plain.ev, true
	}
	if len(splits) == 0 || h.Matcher == "" {
		return "", event{}, false
	}
	decoded, err := decodeSourceMatcher(h, srcCat, srcEvent)
	if err != nil {
		return "", event{}, false
	}
	for _, c := range splits {
		switch {
		case c.ev.MCP && (decoded.MCPAll || decoded.Server != "" || decoded.Tool != ""):
			return c.name, c.ev, true
		case len(c.ev.Tools) != 0 && decoded.Concepts != nil && conceptSetEqual(c.ev.Tools, decoded.Concepts):
			return c.name, c.ev, true
		}
	}
	return "", event{}, false
}

func conceptSetEqual(tools []string, set map[string]bool) bool {
	if len(tools) != len(set) {
		return false
	}
	for _, concept := range tools {
		if !set[concept] {
			return false
		}
	}
	return true
}

// decodeSourceMatcher decodes a hook matcher through its source harness's
// catalog. The matcher is the source harness's native spelling, so a matcher
// the source catalog cannot decode fails rather than being reinterpreted
// through another harness's vocabulary. A hook whose event itself declares
// the tool set — the Windsurf split events — carries no matcher member; the
// decoded set then comes from the source event's declared tools so
// cross-harness targets still produce an exact matcher.
func decodeSourceMatcher(h *ir.Hook, srcCat catalog, srcEvent event) (toolpolicy.DecodedMatcher, error) {
	decoded, err := toolpolicy.DecodeMatcher(target.Target(h.HookSource), h.Matcher, matcherSpec(srcCat))
	if err != nil {
		return toolpolicy.DecodedMatcher{}, err
	}
	if decoded.Empty {
		switch {
		case len(srcEvent.Tools) != 0:
			set := make(map[string]bool, len(srcEvent.Tools))
			for _, concept := range srcEvent.Tools {
				set[concept] = true
			}
			return toolpolicy.DecodedMatcher{Concepts: set}, nil
		case srcEvent.MCP:
			return toolpolicy.DecodedMatcher{MCPAll: true}, nil
		}
	}
	return decoded, nil
}

// resolveMatcher emits the target-native matcher. Events without a tool
// matcher reject a non-empty source matcher rather than dropping it.
func resolveMatcher(h *ir.Hook, dst target.Target, srcCat, dstCat catalog, srcEvent, dstEvent event, res *Resolved, fail func(string, string, string) bool) bool {
	switch dstEvent.Matcher {
	case "", MatcherKindNone:
		if h.Matcher != "" {
			return fail(diag.CodeHookMatcherUntranslatable, "The target event has no tool matcher; hook suppressed", "Remove the matcher or use a tool event")
		}
		res.Matcher = ""
	case MatcherKindTool:
		decoded, err := decodeSourceMatcher(h, srcCat, srcEvent)
		if err != nil {
			return fail(diag.CodeHookMatcherUntranslatable, "The source matcher cannot be decoded for cross-harness translation; hook suppressed", "Use exact source tool names or a supported MCP pattern, or author a target-specific hook")
		}
		rendered, err := toolpolicy.RenderMatcher(dst, decoded, matcherSpec(dstCat))
		if err != nil {
			return fail(diag.CodeHookMatcherUntranslatable, "The target tool catalog cannot exactly cover the source matcher; hook suppressed", "Author a target-specific hook or explicitly narrow the selected harnesses; preview cleanup with agent-marketplace sync --check")
		}
		res.Matcher = rendered
	default:
		// Lifecycle and command-text matchers are native strings, not tool
		// concept sets; they pass through as authored.
		res.Matcher = h.Matcher
	}
	return true
}

// srcUnit is the source catalog's timeout policy; canonical input
// (no provenance) measures timeouts in seconds.
func srcUnit(c catalog) timeoutPolicy { return c.Timeout }

// resolveTimeout converts the timeout into the target's unit, applies
// documented per-event bounds, and refuses values the target cannot
// represent. sameHarness skips unit conversion but keeps the refusal rules.
func resolveTimeout(h *ir.Hook, dst target.Target, src, dstUnit timeoutPolicy, dstEvent event, sameHarness bool, res *Resolved, warn func(string, string, string), fail func(string, string, string) bool) bool {
	if h.Timeout == nil {
		res.Timeout = nil
		return true
	}
	if *h.Timeout < 0 {
		return fail(timeoutUnsupportedCode(dst), "Negative hook timeouts are refused; hook suppressed", "Use a positive timeout")
	}
	if dstUnit.Unit == TimeoutNone {
		return fail(timeoutUnsupportedCode(dst), "The target cannot represent a hook timeout; hook suppressed", "Remove the timeout")
	}
	value := *h.Timeout
	if !sameHarness {
		switch {
		case src.Unit == TimeoutMilliseconds && dstUnit.Unit == TimeoutSeconds:
			if value%1000 != 0 {
				warn(diag.CodeHookTimeoutRounded, "Timeout converted to seconds rounded up to the next whole second", "")
			}
			value = (value + 999) / 1000
		case src.Unit == TimeoutSeconds && dstUnit.Unit == TimeoutMilliseconds:
			value *= 1000
		}
	}
	if dstEvent.Timeout != nil && value > dstEvent.Timeout.Max {
		value = dstEvent.Timeout.Max
		warn(timeoutCappedCode(dst), "Timeout above the documented per-event limit; lowered to the limit", "")
	}
	res.Timeout = &value
	return true
}

// envReference matches bare and braced variable references. Group 1 is the
// braced name, group 2 the bare name.
var envReference = regexp.MustCompile(`\$(?:\{([A-Za-z_][A-Za-z0-9_]*)\}|([A-Za-z_][A-Za-z0-9_]*))`)

// rewriteCommand applies design D6: $NAME/${NAME} references are resolved
// through the source catalog. References no shipped catalog recognizes are left
// unchanged. sameHarness keeps every non-plugin reference as authored.
func rewriteCommand(h *ir.Hook, srcCat, dstCat catalog, event string, scope Scope, supportBase string, sameHarness bool, pluginNames map[string]bool, res *Resolved, fail func(string, string, string) bool) bool {
	command := h.Command
	matches := envReference.FindAllStringSubmatchIndex(command, -1)
	if len(matches) == 0 {
		res.Command = command
		return true
	}
	var out strings.Builder
	pos := 0
	scanPos := 0
	var quote byte
	for _, m := range matches {
		for scanPos < m[0] {
			ch := command[scanPos]
			if ch == '\\' && quote != '\'' {
				scanPos++
				if scanPos < m[0] {
					scanPos++
				}
				continue
			}
			if ch == '\'' || ch == '"' {
				switch quote {
				case 0:
					quote = ch
				case ch:
					quote = 0
				}
			}
			scanPos++
		}
		scanPos = m[1]
		name := ""
		if m[2] >= 0 {
			name = command[m[2]:m[3]]
		} else {
			name = command[m[4]:m[5]]
		}
		concept, plugin, declared := lookupEnv(srcCat, name)
		if !declared {
			if pluginNames[name] {
				return fail(diag.CodeHookEnvUnrepresentable, "A plugin-only variable is not declared by the hook source; hook suppressed", "Use a variable declared by the source harness")
			}
			continue
		}
		// Other plugin-only variables never resolve in a native hook file.
		if plugin && concept != "plugin_root" {
			return fail(diag.CodeHookEnvUnrepresentable, "A plugin-only variable has no native equivalent on the target; hook suppressed", "Replace the reference with a documented variable")
		}
		if plugin && quote == '\'' {
			return fail(diag.CodeHookEnvUnrepresentable, "A plugin-root reference inside a single-quoted shell literal cannot be translated; hook suppressed", "Move the reference outside the single-quoted literal")
		}
		preserveProjectVariable := false
		repositoryBaseReference := !sameHarness && !plugin && srcCat.RepositoryBase != nil && srcCat.RepositoryBase.Env == concept
		repositoryBase, repositoryBaseKind, hasRepositoryBase := "", 0, false
		if repositoryBaseReference {
			if variable, exists := dstCat.Env[concept]; exists && slices.Contains(variable.Names, name) {
				// The target accepts the same spelling; keep it (Cursor and
				// Gemini accept CLAUDE_PROJECT_DIR).
				preserveProjectVariable = true
			} else {
				repositoryBase, repositoryBaseKind, hasRepositoryBase = repositoryBaseReplacement(dstCat)
				if !hasRepositoryBase {
					return fail(diag.CodeHookEnvUnrepresentable, "The target declares no repository base for a project-directory reference; hook suppressed", "Use a documented variable")
				}
			}
		}
		isolatedDoubleQuoted := quote == '"' && m[0] > pos && command[m[0]-1] == '"' &&
			m[1]+1 < len(command) && command[m[1]] == '"' && command[m[1]+1] == '/' &&
			(plugin || repositoryBaseReference && !preserveProjectVariable && repositoryBaseKind == repoBaseCwd)
		prefixEnd := m[0]
		if isolatedDoubleQuoted {
			prefixEnd--
		}
		out.WriteString(command[pos:prefixEnd])
		pos = m[1]
		replacementQuote := quote
		if isolatedDoubleQuoted {
			pos++
			replacementQuote = 0
		}
		if plugin {
			ref, refusal, ok := supportReference(dstCat, scope, supportBase, replacementQuote)
			if !ok {
				return fail(refusal.Code, refusal.Message, refusal.Hint)
			}
			out.WriteString(ref)
			continue
		}
		if sameHarness || preserveProjectVariable {
			out.WriteString(command[m[0]:m[1]])
			continue
		}
		if repositoryBaseReference {
			switch repositoryBaseKind {
			case repoBaseCwd:
				// {cwd: repository}: remove the reference and one following
				// slash so the path becomes repository-relative. A reference
				// that is the whole shell word, or whose remaining path would
				// start at the filesystem root, names the repository itself
				// as "."; text joined to it has no relative form.
				slash := pos < len(command) && command[pos] == '/'
				next := pos
				if slash {
					next++
				}
				switch {
				case projectDirectoryWordEnds(command, next, replacementQuote) || slash && next < len(command) && command[next] == '/':
					out.WriteByte('.')
				case !slash:
					return fail(diag.CodeHookEnvUnrepresentable, "A project-directory reference joined to other text has no repository-relative form on the target; hook suppressed", "Separate the reference from following text with / or whitespace")
				}
				pos = next
			case repoBaseLiteral:
				if replacementQuote == '"' {
					repositoryBase = withoutOuterDoubleQuotes(repositoryBase)
				}
				out.WriteString(repositoryBase)
			default:
				out.WriteString(nameReplacement(m, repositoryBase))
			}
			continue
		}
		targetVar, ok := dstCat.Env[concept]
		if !ok || len(targetVar.Events) != 0 && !slices.Contains(targetVar.Events, event) {
			return fail(diag.CodeHookEnvUnrepresentable, "The referenced variable has no equivalent on the target for this event; hook suppressed", "Use a variable the target documents")
		}
		if slices.Contains(targetVar.Names, name) {
			// The target accepts the same spelling; keep it.
			out.WriteString(command[m[0]:m[1]])
			continue
		}
		out.WriteString(nameReplacement(m, targetVar.Names[0]))
	}
	out.WriteString(command[pos:])
	result := out.String()
	if result == "" {
		result = "."
	}
	res.Command = result
	return true
}

// lookupEnv resolves a variable name to its catalog concept through the
// source catalog, preferring regular env over plugin_env.
func lookupEnv(c catalog, name string) (concept string, plugin, declared bool) {
	for key, v := range c.Env {
		if slices.Contains(v.Names, name) {
			return key, false, true
		}
	}
	for key, v := range c.PluginEnv {
		if slices.Contains(v.Names, name) {
			return key, true, true
		}
	}
	return "", false, false
}

// projectDirectoryWordEnds reports whether the shell word containing a
// project-directory reference ends at pos. Inside double quotes only the
// closing quote ends the reference; the word then ends at an unquoted shell
// separator or the end of the command.
func projectDirectoryWordEnds(command string, pos int, quote byte) bool {
	if quote == '"' {
		if pos >= len(command) || command[pos] != '"' {
			return false
		}
		pos++
	}
	return pos >= len(command) || strings.IndexByte(" \t\n;&|()<>", command[pos]) >= 0
}

// Repository-base replacement kinds for a project-directory reference.
const (
	repoBaseName    = iota // substitute the bare variable name in place
	repoBaseLiteral        // replace the whole quoted reference with a literal
	repoBaseCwd            // drop the reference; the path becomes relative
)

// repositoryBaseReplacement renders the target's repository base.
func repositoryBaseReplacement(c catalog) (replacement string, kind int, ok bool) {
	rb := c.RepositoryBase
	if rb == nil {
		return "", 0, false
	}
	switch {
	case rb.Cwd != "":
		return "", repoBaseCwd, true
	case rb.Shell != "":
		return rb.Shell, repoBaseLiteral, true
	case rb.Env != "":
		v, exists := c.Env[rb.Env]
		if !exists || len(v.Names) == 0 {
			return "", 0, false
		}
		return v.Names[0], repoBaseName, true
	}
	return "", 0, false
}

// nameReplacement emits the target variable name with the source brace style.
func nameReplacement(m []int, name string) string {
	if m[2] >= 0 {
		return "${" + name + "}"
	}
	return "$" + name
}

func withoutOuterDoubleQuotes(value string) string {
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		return value[1 : len(value)-1]
	}
	return value
}

// machineSupportBaseLiteral renders an absolute machine-scope support base as
// a safe shell word: unchanged when every character is already word-safe,
// otherwise a POSIX single-quoted literal with embedded single quotes escaped.
// The machine destination root is a real HOME/CODEX_HOME path that can carry
// spaces or shell metacharacters; the emitted command must still reference
// that literal directory, never an expansion.
func machineSupportBaseLiteral(base string) string {
	for _, r := range base {
		safe := r == '/' || r == '.' || r == '_' || r == '-' ||
			('a' <= r && r <= 'z') || ('A' <= r && r <= 'Z') || ('0' <= r && r <= '9')
		if !safe {
			return "'" + strings.ReplaceAll(base, "'", `'\''`) + "'"
		}
	}
	return base
}

// supportReference renders the installed support-directory reference for a
// plugin-root token.
func supportReference(c catalog, scope Scope, supportBase string, quote byte) (string, diag.Diagnostic, bool) {
	refuse := func() (string, diag.Diagnostic, bool) {
		return "", diag.Diagnostic{Code: diag.CodeHookSupportPathUnrepresentable, Message: "The plugin-root reference has no representable support path on the target; hook suppressed", Hint: "Remove the plugin-root reference"}, false
	}
	if supportBase == "" {
		return refuse()
	}
	if scope == ScopeMachine {
		if quote == '"' {
			literal := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "$", `\$`, "`", "\\`").Replace(supportBase)
			return literal, diag.Diagnostic{}, true
		}
		return machineSupportBaseLiteral(supportBase), diag.Diagnostic{}, true
	}
	rb := c.RepositoryBase
	if rb == nil {
		return refuse()
	}
	rel := strings.Trim(supportBase, "/")
	switch {
	case rb.Cwd != "":
		return rel, diag.Diagnostic{}, true
	case rb.Shell != "":
		base := rb.Shell
		if quote == '"' {
			base = withoutOuterDoubleQuotes(base)
		}
		return base + "/" + rel, diag.Diagnostic{}, true
	case rb.Env != "":
		v, exists := c.Env[rb.Env]
		if !exists || len(v.Names) == 0 {
			return refuse()
		}
		if quote == '"' {
			return "$" + v.Names[0] + "/" + rel, diag.Diagnostic{}, true
		}
		return `"$` + v.Names[0] + `"/` + rel, diag.Diagnostic{}, true
	}
	return refuse()
}

// HasEquivalentEvent reports whether the hook's event has a catalog-declared
// equivalent on dst, sharing Resolve's event-selection logic — including
// target tool-split events matched against the hook's decoded matcher. It is
// metadata-only: it inspects the event and matcher, never the command or
// prompt payload, and does not duplicate resolution work.
func HasEquivalentEvent(h *ir.Hook, dst target.Target) bool {
	r, err := shipped()
	if err != nil {
		return false
	}
	src := target.Target(h.HookSource)
	if src == "" {
		src = target.TargetClaude
	}
	srcCat, ok := r.catalogs[src]
	if !ok {
		return false
	}
	dstCat, ok := r.catalogs[dst]
	if !ok {
		return false
	}
	srcEvent, declared := srcCat.Events[h.Event]
	if !declared {
		return false
	}
	if src == dst {
		return true
	}
	if srcEvent.Canonical == "" {
		return false
	}
	_, _, found := selectEvent(dstCat, srcEvent.Canonical, h, srcCat, srcEvent)
	return found
}
