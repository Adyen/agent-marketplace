package mapping

import (
	"strings"
)

// hookEventEntry maps a Claude hook event to per-target event names.
// For Windsurf, PreToolUse is further split by tool matcher into
// specific events like pre_read_code, pre_write_code, etc.
type hookEventEntry struct {
	Claude  string            // Source Claude event name (e.g. "PreToolUse")
	Targets map[Target]string // Per-target event name
}

// hookEventTable is the curated translation table for Claude hook events.
//
// Some events map directly across targets (e.g. UserPromptSubmit → pre_user_prompt).
// PreToolUse is special: Windsurf splits it into per-tool events, which are
// resolved by HookEvent() based on the matcher field.
//
// Events that have no equivalent in a target map to "" (unmappable), and
// the caller should emit a hook.<target>.event-unmappable diagnostic.
//
// Codex provenance for every Codex cell below: verified 2026-08-19 against
// https://developers.openai.com/codex/hooks. Cells use only event names the
// official event reference documents; Notification and Devin's differently
// timed PostCompaction event remain explicitly unmappable.
var hookEventTable = []hookEventEntry{
	// Last verified: 2026-04-24 against Claude Code v1.0, Windsurf Cascade docs
	{Claude: "PreToolUse", Targets: map[Target]string{
		TargetClaude:   "PreToolUse",
		TargetFactory:  "PreToolUse",
		TargetCursor:   "PreToolUse",
		TargetWindsurf: "", // resolved by matcher via windsurfPreToolEvent()
		TargetGemini:   "BeforeTool",
		TargetDevin:    "PreToolUse", // Devin: bundled 3000.3 docs; 3000.3.27 probe pending.
		TargetCodex:    "PreToolUse",
		TargetOpenCode: "tool.execute.before",
	}},

	// Last verified: 2026-04-24 against Claude Code v1.0
	{Claude: "PostToolUse", Targets: map[Target]string{
		TargetClaude:   "PostToolUse",
		TargetFactory:  "PostToolUse",
		TargetCursor:   "PostToolUse",
		TargetWindsurf: "", // unmappable: Windsurf has no post-tool hooks
		TargetGemini:   "AfterTool",
		TargetDevin:    "PostToolUse", // Devin: bundled 3000.3 docs; 3000.3.27 probe pending.
		TargetCodex:    "PostToolUse",
		TargetOpenCode: "",
	}},

	// Last verified: 2026-04-24 against Claude Code v1.0, Windsurf Cascade docs
	{Claude: "UserPromptSubmit", Targets: map[Target]string{
		TargetClaude:   "UserPromptSubmit",
		TargetFactory:  "UserPromptSubmit",
		TargetCursor:   "UserPromptSubmit",
		TargetWindsurf: "pre_user_prompt",
		TargetGemini:   "BeforeAgent",
		TargetDevin:    "UserPromptSubmit", // Devin: bundled 3000.3 docs; 3000.3.27 probe pending.
		TargetCodex:    "UserPromptSubmit",
		TargetOpenCode: "",
	}},

	// Last verified: 2026-04-24 against Claude Code v1.0, Windsurf Cascade docs
	{Claude: "Stop", Targets: map[Target]string{
		TargetClaude:   "Stop",
		TargetFactory:  "Stop",
		TargetCursor:   "Stop",
		TargetWindsurf: "post_cascade_response",
		TargetGemini:   "",     // unmappable: no Gemini equivalent in v0
		TargetDevin:    "Stop", // Devin: bundled 3000.3 docs; 3000.3.27 probe pending.
		TargetCodex:    "Stop",
		TargetOpenCode: "",
	}},

	// Last verified: 2026-04-24 against Claude Code v1.0, Windsurf Cascade docs
	{Claude: "SubagentStop", Targets: map[Target]string{
		TargetClaude:   "SubagentStop",
		TargetFactory:  "SubagentStop",
		TargetCursor:   "SubagentStop",
		TargetWindsurf: "",
		TargetGemini:   "", // unmappable: no Gemini equivalent in v0
		TargetDevin:    "", // unmappable: Devin documents no subagent-stop event
		TargetCodex:    "", // same name, but no direct Claude-to-Codex lifecycle equivalent
		TargetOpenCode: "",
	}},

	// Codex-only event; other targets have no documented equivalent.
	{Claude: "SubagentStart", Targets: map[Target]string{
		TargetClaude:   "",
		TargetFactory:  "",
		TargetCursor:   "",
		TargetWindsurf: "",
		TargetGemini:   "",
		TargetDevin:    "",
		TargetCodex:    "", // native Codex only; no cross-harness equivalent
		TargetOpenCode: "",
	}},

	// Last verified: 2026-04-24 against Claude Code v1.0
	{Claude: "Notification", Targets: map[Target]string{
		TargetClaude:   "Notification",
		TargetFactory:  "Notification",
		TargetCursor:   "Notification",
		TargetWindsurf: "", // unmappable: no Windsurf equivalent
		TargetGemini:   "", // unmappable: no Gemini equivalent
		TargetDevin:    "", // unmappable: Devin documents no notification event
		TargetCodex:    "", // unmappable: Codex documents no notification event
		TargetOpenCode: "",
	}},

	// Last verified: 2026-04-24 against Claude Code v1.0
	{Claude: "PreCompact", Targets: map[Target]string{
		TargetClaude:   "PreCompact",
		TargetFactory:  "PreCompact",
		TargetCursor:   "PreCompact",
		TargetWindsurf: "", // unmappable: no Windsurf equivalent
		TargetGemini:   "PreCompress",
		// unmappable: Devin documents PostCompaction (after), which is a
		// semantic inversion of PreCompact (before); never map them together.
		TargetDevin:    "",
		TargetCodex:    "", // same name, but no direct Claude-to-Codex lifecycle equivalent
		TargetOpenCode: "",
	}},

	// PostCompact is a Codex event that runs after compaction. It is distinct
	// from both Claude's PreCompact and Devin's PostCompaction spellings.
	{Claude: "PostCompact", Targets: map[Target]string{
		TargetClaude:   "",
		TargetFactory:  "",
		TargetCursor:   "",
		TargetWindsurf: "",
		TargetGemini:   "",
		TargetDevin:    "",
		TargetCodex:    "", // native Codex only; no cross-harness equivalent
		TargetOpenCode: "",
	}},

	// Devin-documented events. Devin cells: bundled 3000.3 documentation
	// (docs/devin-cli-harness-spec-3000.3.md, "Hooks"); the 3000.3.27 runtime
	// probe of the event schemas is still pending. No other target has
	// in-repo vendor evidence for these events, so their cells are "".
	{Claude: "PermissionRequest", Targets: map[Target]string{
		TargetClaude:   "", // unmappable: no in-repo Claude Code evidence
		TargetFactory:  "", // unmappable: no in-repo Factory Droid evidence
		TargetCursor:   "", // unmappable: no in-repo Cursor evidence
		TargetWindsurf: "", // unmappable: no Windsurf equivalent
		TargetGemini:   "", // unmappable: no Gemini equivalent
		TargetDevin:    "PermissionRequest",
		TargetCodex:    "", // same name, but no direct Devin-to-Codex lifecycle equivalent
		TargetOpenCode: "",
	}},

	{Claude: "SessionStart", Targets: map[Target]string{
		TargetClaude:   "",             // unmappable: no in-repo Claude Code evidence
		TargetFactory:  "SessionStart", // Factory documents session-start lifecycle hooks
		TargetCursor:   "",             // unmappable: no in-repo Cursor evidence
		TargetWindsurf: "",             // unmappable: no Windsurf equivalent
		TargetGemini:   "",             // unmappable: no Gemini equivalent
		TargetDevin:    "SessionStart",
		TargetCodex:    "SessionStart",
		TargetOpenCode: "",
	}},

	{Claude: "SessionEnd", Targets: map[Target]string{
		TargetClaude:   "",           // unmappable: no in-repo Claude Code evidence
		TargetFactory:  "SessionEnd", // Factory documents session-end lifecycle hooks
		TargetCursor:   "",           // unmappable: no in-repo Cursor evidence
		TargetWindsurf: "",           // unmappable: no Windsurf equivalent
		TargetGemini:   "",           // unmappable: no Gemini equivalent
		TargetDevin:    "SessionEnd",
		TargetCodex:    "", // same name, but no direct Devin-to-Codex lifecycle equivalent
		TargetOpenCode: "",
	}},

	// PostCompaction is a Devin event that runs after compaction. It is not the
	// same as Claude Code's PreCompact (which runs before), so the two are
	// never mapped onto each other in either direction.
	{Claude: "PostCompaction", Targets: map[Target]string{
		TargetClaude:   "", // unmappable: Claude Code has PreCompact, not PostCompaction
		TargetFactory:  "", // unmappable: no in-repo Factory Droid evidence
		TargetCursor:   "", // unmappable: no in-repo Cursor evidence
		TargetWindsurf: "", // unmappable: no Windsurf equivalent
		TargetGemini:   "", // unmappable: no Gemini equivalent
		TargetDevin:    "PostCompaction",
		TargetCodex:    "", // unmappable: Codex's event is PostCompact, not PostCompaction
		TargetOpenCode: "",
	}},
}

// windsurfToolMatcher maps Claude tool names to Windsurf split-per-tool event names
// for PreToolUse hooks. When a PreToolUse hook has a matcher, this table determines
// which Windsurf event it maps to.
var windsurfToolMatcher = map[string]string{
	// Last verified: 2026-04-24 against Windsurf Cascade docs
	"Read":       "pre_read_code",
	"Write":      "pre_write_code",
	"Edit":       "pre_write_code",
	"Write|Edit": "pre_write_code",
	"MultiEdit":  "pre_write_code",
	"Bash":       "pre_run_command",
	"Glob":       "pre_read_code",
	"Grep":       "pre_read_code",
	"WebFetch":   "pre_read_code",
	"WebSearch":  "pre_read_code",
}

// windsurfPreToolEvent resolves a PreToolUse + matcher to a Windsurf event.
// Returns the Windsurf event name and ok=true if mappable, or "", false if not.
func windsurfPreToolEvent(matcher string) (string, bool) {
	// MCP tool matchers: any matcher starting with "mcp__"
	if strings.HasPrefix(matcher, "mcp__") {
		return "pre_mcp_tool_use", true
	}

	// Known tool matchers
	if event, ok := windsurfToolMatcher[matcher]; ok {
		return event, true
	}

	// Unknown matcher or empty → unmappable
	return "", false
}

// HookEvent returns the target-specific event name for a given Claude hook event
// and optional tool matcher. The matcher is used to resolve PreToolUse into
// Windsurf's split-per-tool events (e.g. pre_read_code, pre_write_code).
//
// If the event is mappable, the target event name is returned with ok=true.
// If the event is unmappable for the target, "", false is returned and the caller
// should emit a hook.<target>.event-unmappable diagnostic.
func HookEvent(claudeEvent string, matcher string, target Target) (mapped string, ok bool) {
	// Special handling for PreToolUse → Windsurf split-per-tool events
	if claudeEvent == "PreToolUse" && target == TargetWindsurf {
		return windsurfPreToolEvent(matcher)
	}

	for _, entry := range hookEventTable {
		if entry.Claude == claudeEvent {
			m, found := entry.Targets[target]
			if !found || m == "" {
				return "", false
			}
			return m, true
		}
	}
	return "", false
}

// reverseHookEntry maps a target-specific event name back to a Claude event + matcher.
type reverseHookEntry struct {
	TargetEvent string
	ClaudeEvent string
	Matcher     string // optional matcher for PreToolUse events
}

// devinReverseTable maps Devin hook event names back to canonical event names.
//
// It is written out explicitly instead of being derived from hookEventTable by
// a forward lookup. The deleted D3 arm did exactly that — it called
// HookEvent(devinEvent, "", TargetDevin), i.e. it fed a DEVIN event name into
// the CLAUDE column of the forward table — which only appeared to work because
// every Devin cell happens to be spelled like its canonical row key. That is an
// accident of the current vocabulary, not a property of the table: the moment
// one Devin cell differs from its canonical row name (as every Windsurf and
// Gemini cell already does) the forward lookup either silently returns the
// wrong event or claims a canonical-only event is a valid Devin event.
//
// Two rows are deliberately ABSENT even though a name-based reversal would
// produce them:
//
//   - PreCompact is not listed. Devin documents PostCompaction, which runs
//     AFTER compaction; PreCompact runs before. The forward table already
//     refuses to map them onto each other, and the reverse direction must
//     refuse just as hard, or a Devin PostCompaction hook would come back as a
//     canonical PreCompact hook and then compile to a Claude PreCompact hook
//     that fires at the opposite moment.
//   - SubagentStop and Notification are not listed because Devin documents no
//     such events; a file that contains them is not canonical Devin input.
//
// Evidence for every row: docs/devin-cli-harness-spec-3000.3.md, "Hooks" —
// "Documented events include: PreToolUse, PostToolUse, PermissionRequest,
// UserPromptSubmit, Stop, SessionStart, SessionEnd, PostCompaction". The
// canonical (Claude-column) spelling of each of those eight events is
// identical, so every row reverses to itself; TestReverseHookEventDevin pins
// the agreement with the forward table so the two can never drift.
//
// The Matcher column is empty for every row: unlike Windsurf, Devin does not
// encode a tool matcher in the event name. A Devin matcher is a separate
// `matcher` member of the rule object and is carried by the ingester directly.
var devinReverseTable = []reverseHookEntry{
	{"PreToolUse", "PreToolUse", ""},
	{"PostToolUse", "PostToolUse", ""},
	{"PermissionRequest", "PermissionRequest", ""},
	{"UserPromptSubmit", "UserPromptSubmit", ""},
	{"Stop", "Stop", ""},
	{"SessionStart", "SessionStart", ""},
	{"SessionEnd", "SessionEnd", ""},
	{"PostCompaction", "PostCompaction", ""},
}

var codexReverseTable = []reverseHookEntry{
	{"PreToolUse", "PreToolUse", ""},
	{"PermissionRequest", "PermissionRequest", ""},
	{"PostToolUse", "PostToolUse", ""},
	{"PreCompact", "PreCompact", ""},
	{"PostCompact", "PostCompact", ""},
	{"UserPromptSubmit", "UserPromptSubmit", ""},
	{"SubagentStop", "SubagentStop", ""},
	{"SubagentStart", "SubagentStart", ""},
	{"Stop", "Stop", ""},
	{"SessionStart", "SessionStart", ""},
	{"SessionEnd", "SessionEnd", ""},
}

// windsurfReverseTable maps Windsurf events back to Claude events.
var windsurfReverseTable = []reverseHookEntry{
	{"pre_read_code", "PreToolUse", "Read"},
	{"pre_write_code", "PreToolUse", "Write|Edit"},
	{"pre_run_command", "PreToolUse", "Bash"},
	{"pre_mcp_tool_use", "PreToolUse", "mcp"},
	{"pre_user_prompt", "UserPromptSubmit", ""},
	{"post_cascade_response", "Stop", ""},
}

// ReverseHookEvent maps a target-specific event name back to a Claude event name
// and optional matcher. This is used by ingesters that read non-Claude hook formats.
func ReverseHookEvent(targetEvent string, target Target) (claudeEvent string, matcher string, ok bool) {
	switch target {
	case TargetClaude, TargetFactory, TargetCursor:
		// For Claude-family targets, the event name IS the Claude event name
		return targetEvent, "", true
	case TargetDevin:
		// Devin's documented event vocabulary is CLOSED (eight events), so an
		// unlisted name is not "probably fine, pass it through" — it is not
		// canonical Devin input. Returning ok=false makes the Devin ingester
		// report it instead of silently canonicalizing an event Devin never
		// fires. See devinReverseTable for why this is a real table and not a
		// forward lookup.
		for _, entry := range devinReverseTable {
			if entry.TargetEvent == targetEvent {
				return entry.ClaudeEvent, entry.Matcher, true
			}
		}
	case TargetCodex:
		for _, entry := range codexReverseTable {
			if entry.TargetEvent == targetEvent {
				return entry.ClaudeEvent, entry.Matcher, true
			}
		}
	case TargetWindsurf:
		for _, entry := range windsurfReverseTable {
			if entry.TargetEvent == targetEvent {
				return entry.ClaudeEvent, entry.Matcher, true
			}
		}
	case TargetGemini:
		// Reverse Gemini events to Claude using the same vocabulary as HookEvent.
		geminiReverse := map[string]reverseHookEntry{
			"BeforeTool":  {"BeforeTool", "PreToolUse", ""},
			"AfterTool":   {"AfterTool", "PostToolUse", ""},
			"BeforeAgent": {"BeforeAgent", "UserPromptSubmit", ""},
			"PreCompress": {"PreCompress", "PreCompact", ""},
		}
		if entry, ok := geminiReverse[targetEvent]; ok {
			return entry.ClaudeEvent, entry.Matcher, true
		}
	}
	return "", "", false
}

// NativeCodexHookEvent maps only the Codex lifecycles with a documented
// cross-harness counterpart. Equal event names are not evidence of equal
// payload or timing semantics.
func NativeCodexHookEvent(event, matcher string, target Target) (string, bool) {
	switch event {
	case "PreToolUse", "PostToolUse", "UserPromptSubmit", "Stop", "SessionStart":
		return HookEvent(event, matcher, target)
	default:
		return "", false
	}
}
