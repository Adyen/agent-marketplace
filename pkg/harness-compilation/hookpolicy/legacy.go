package hookpolicy

import "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"

// legacyEventEntry is one BASE (pre-provenance) reverse hook mapping: the
// canonical event the historical ingester assigned and the matcher it implied.
type legacyEventEntry struct {
	canonical string
	matcher   string
}

// legacyEventTables replay mapping.ReverseHookEvent as it existed in the v1-v4
// source-revision era. These tables are frozen: they describe what historical
// ingesters recorded, not what current catalogs translate, and must never gain
// entries.
var legacyEventTables = map[target.Target]map[string]legacyEventEntry{
	target.TargetDevin: {
		"PreToolUse":        {"PreToolUse", ""},
		"PostToolUse":       {"PostToolUse", ""},
		"PermissionRequest": {"PermissionRequest", ""},
		"UserPromptSubmit":  {"UserPromptSubmit", ""},
		"Stop":              {"Stop", ""},
		"SessionStart":      {"SessionStart", ""},
		"SessionEnd":        {"SessionEnd", ""},
		"PostCompaction":    {"PostCompaction", ""},
	},
	target.TargetCodex: {
		"PreToolUse":        {"PreToolUse", ""},
		"PostToolUse":       {"PostToolUse", ""},
		"PermissionRequest": {"PermissionRequest", ""},
		"UserPromptSubmit":  {"UserPromptSubmit", ""},
		"Stop":              {"Stop", ""},
		"SessionStart":      {"SessionStart", ""},
		"SessionEnd":        {"SessionEnd", ""},
		"PreCompact":        {"PreCompact", ""},
		"PostCompact":       {"PostCompact", ""},
		"SubagentStart":     {"SubagentStart", ""},
		"SubagentStop":      {"SubagentStop", ""},
	},
	target.TargetWindsurf: {
		// BASE folded normalizeMatcher's pre_mcp_tool_use override into the
		// implied matcher, so the frozen matcher is "mcp__*".
		"pre_run_command":       {"PreToolUse", "Bash"},
		"post_cascade_response": {"Stop", ""},
		"pre_write_code":        {"PreToolUse", "Write|Edit"},
		"pre_user_prompt":       {"UserPromptSubmit", ""},
		"pre_read_code":         {"PreToolUse", "Read"},
		"pre_mcp_tool_use":      {"PreToolUse", "mcp__*"},
	},
	target.TargetGemini: {
		"BeforeTool":  {"PreToolUse", ""},
		"AfterTool":   {"PostToolUse", ""},
		"BeforeAgent": {"UserPromptSubmit", ""},
		"PreCompress": {"PreCompact", ""},
	},
}

// LegacyEvent returns the canonical event and implied matcher the pre-catalog
// (v1-v4 source-revision era) ingesters assigned to a native hook event on
// source. known is false when BASE found no mapping: BASE then preserved the
// native event spelling verbatim as the canonical placeholder. The
// Claude-lineage sources (claude, factory, cursor, and the empty canonical
// source) accepted every event verbatim, so they always report known with the
// authored event and matcher.
func LegacyEvent(source target.Target, event string) (canonical, matcher string, known bool) {
	if table, ok := legacyEventTables[source]; ok {
		if entry, found := table[event]; found {
			return entry.canonical, entry.matcher, true
		}
		return "", "", false
	}
	return event, "", true
}
