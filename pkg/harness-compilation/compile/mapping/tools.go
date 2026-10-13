package mapping

// toolEntry maps a Claude tool ID to per-target tool ID(s).
type toolEntry struct {
	Claude  string // Source Claude tool ID
	Targets map[Target][]string
}

// toolTable is the curated translation table for Claude tool IDs.
//
// Some Claude tools map to multiple tools in certain targets (e.g. Write →
// [Edit, Create] for Factory). Unknown tools return ok=false and the caller
// should emit a W_TOOL_UNKNOWN diagnostic.
//
// Devin provenance for every Devin cell below: the accepted allowed-tools
// vocabulary is exactly read/edit/grep/glob/exec. Originally pinned against
// Devin CLI 3000.2.17 and restated by the bundled 3000.3 documentation in
// docs/devin-cli-harness-spec-3000.3.md, it is now RE-VERIFIED on 3000.3.27 by a
// committed probe: internal/devinprobe/probe_test.go's
// TestProbeAllowedToolVocabulary offers Devin a 22-name candidate superset and
// logs `PROBE PASS tools: accepted allowed-tools set is exactly
// [edit exec glob grep read]; every other candidate is silently dropped` — 17 of
// the 22 candidates were dropped without any error. Because that vocabulary is
// closed, known, and silently filtered rather than rejected, a nil Devin cell
// means "Devin has no such tool" (unsupported), never "not yet mapped", and the
// compiler must fail a source tool it cannot map rather than pass it through.
//
// Codex provenance for every Codex cell below: verified 2026-08-19 against
// https://developers.openai.com/codex/subagents. The documented custom-agent
// schema has no per-agent tool allow-list equivalent to the IR Tools field, so
// every source tool is explicitly unsupported. A nil cell is intentional and
// prevents translation from silently widening a source restriction.
var toolTable = []toolEntry{
	{Claude: "Create", Targets: map[Target][]string{
		TargetClaude: nil, TargetFactory: {"Create"}, TargetCursor: nil,
		TargetWindsurf: nil, TargetGemini: nil, TargetDevin: nil, TargetCodex: nil, TargetOpenCode: nil,
	}},
	// Last verified: 2026-04-23 against Claude Code v1.0
	{Claude: "Read", Targets: map[Target][]string{
		TargetClaude:   {"Read"},
		TargetFactory:  {"Read"},
		TargetCursor:   {"Read"},
		TargetWindsurf: {"Read"},
		TargetGemini:   {"Read"},
		TargetDevin:    {"read"}, // Devin: documented allowed-tools value.
		TargetCodex:    nil,
		TargetOpenCode: {"read"},
	}},

	// Last verified: 2026-04-23 against Claude Code v1.0
	{Claude: "Write", Targets: map[Target][]string{
		TargetClaude:   {"Write"},
		TargetFactory:  {"Edit", "Create"},
		TargetCursor:   {"Write"},
		TargetWindsurf: {"Write"},
		TargetGemini:   {"Write"},
		TargetDevin:    nil, // unsupported: not in Devin's closed read/edit/grep/glob/exec vocabulary.
		TargetCodex:    nil,
		TargetOpenCode: {"write"},
	}},

	// Last verified: 2026-04-23 against Claude Code v1.0
	{Claude: "Edit", Targets: map[Target][]string{
		TargetClaude:   {"Edit"},
		TargetFactory:  {"Edit"},
		TargetCursor:   {"Edit"},
		TargetWindsurf: {"Edit"},
		TargetGemini:   {"Edit"},
		TargetDevin:    {"edit"}, // Devin: documented allowed-tools value.
		TargetCodex:    nil,
		TargetOpenCode: {"edit"},
	}},

	// Last verified: 2026-04-23 against Claude Code v1.0
	{Claude: "Bash", Targets: map[Target][]string{
		TargetClaude:   {"Bash"},
		TargetFactory:  {"Bash"},
		TargetCursor:   {"Bash"},
		TargetWindsurf: {"Bash"},
		TargetGemini:   {"Bash"},
		TargetDevin:    {"exec"}, // Devin: documented allowed-tools value.
		TargetCodex:    nil,
		TargetOpenCode: {"bash"},
	}},

	// Last verified: 2026-04-23 against Claude Code v1.0
	{Claude: "Glob", Targets: map[Target][]string{
		TargetClaude:   {"Glob"},
		TargetFactory:  {"Glob"},
		TargetCursor:   {"Glob"},
		TargetWindsurf: {"Glob"},
		TargetGemini:   {"Glob"},
		TargetDevin:    {"glob"}, // Devin: documented allowed-tools value.
		TargetCodex:    nil,
		TargetOpenCode: {"glob"},
	}},

	// Last verified: 2026-04-23 against Claude Code v1.0
	{Claude: "Grep", Targets: map[Target][]string{
		TargetClaude:   {"Grep"},
		TargetFactory:  {"Grep"},
		TargetCursor:   {"Grep"},
		TargetWindsurf: {"Grep"},
		TargetGemini:   {"Grep"},
		TargetDevin:    {"grep"}, // Devin: documented allowed-tools value.
		TargetCodex:    nil,
		TargetOpenCode: {"grep"},
	}},

	// Last verified: 2026-04-23 against Claude Code v1.0
	{Claude: "MultiEdit", Targets: map[Target][]string{
		TargetClaude:   {"MultiEdit"},
		TargetFactory:  {"MultiEdit"},
		TargetCursor:   {"MultiEdit"},
		TargetWindsurf: {"MultiEdit"},
		TargetGemini:   {"MultiEdit"},
		TargetDevin:    nil, // unsupported: not in Devin's closed read/edit/grep/glob/exec vocabulary.
		TargetCodex:    nil,
		TargetOpenCode: nil,
	}},

	// Last verified: 2026-04-23 against Claude Code v1.0
	{Claude: "WebFetch", Targets: map[Target][]string{
		TargetClaude:   {"WebFetch"},
		TargetFactory:  {"WebFetch"},
		TargetCursor:   {"WebFetch"},
		TargetWindsurf: {"WebFetch"},
		TargetGemini:   {"WebFetch"},
		TargetDevin:    nil, // unsupported: not in Devin's closed read/edit/grep/glob/exec vocabulary.
		TargetCodex:    nil,
		TargetOpenCode: {"webfetch"},
	}},

	// Last verified: 2026-04-23 against Claude Code v1.0
	{Claude: "WebSearch", Targets: map[Target][]string{
		TargetClaude:   {"WebSearch"},
		TargetFactory:  {"WebSearch"},
		TargetCursor:   {"WebSearch"},
		TargetWindsurf: {"WebSearch"},
		TargetGemini:   {"WebSearch"},
		TargetDevin:    nil, // unsupported: not in Devin's closed read/edit/grep/glob/exec vocabulary.
		TargetCodex:    nil,
		TargetOpenCode: {"websearch"},
	}},

	// OpenCode provenance for every OpenCode-only row below (Task, TodoWrite,
	// Skill): the native spellings are the fixed command-hook adapter's
	// runtime canonicalization table, executed against the pinned OpenCode
	// 1.18.34 (see internal/opencodehook/adapter.js and the compatibility
	// note).
	//
	// Every other target carries an explicit nil cell, per the table-wide
	// coverage contract. That is deliberately the same resolution as before
	// these rows existed (unknown → passthrough plus W_TOOL_UNKNOWN, or
	// fail-closed where the target already fails): the curated lookup returns
	// (nil, false) for a nil cell exactly as it does for a missing row, so no
	// existing target's behavior changes, and the nil cells prevent a future
	// edit from silently widening an explicitly unsupported tool.
	{Claude: "Task", Targets: map[Target][]string{
		TargetClaude: nil, TargetFactory: nil, TargetCursor: nil, TargetWindsurf: nil,
		TargetGemini: nil, TargetDevin: nil, TargetCodex: nil, TargetOpenCode: {"task"},
	}},
	{Claude: "TodoWrite", Targets: map[Target][]string{
		TargetClaude: nil, TargetFactory: nil, TargetCursor: nil, TargetWindsurf: nil,
		TargetGemini: nil, TargetDevin: nil, TargetCodex: nil, TargetOpenCode: {"todowrite"},
	}},
	{Claude: "Skill", Targets: map[Target][]string{
		TargetClaude: nil, TargetFactory: nil, TargetCursor: nil, TargetWindsurf: nil,
		TargetGemini: nil, TargetDevin: nil, TargetCodex: nil, TargetOpenCode: {"skill"},
	}},
}

// Tool returns the target-specific tool ID(s) for a given Claude tool ID.
// If the tool is found, the mapped tool IDs are returned with ok=true.
// If the tool is not found, ok=false and the caller should emit a
// W_TOOL_UNKNOWN diagnostic.
func Tool(claudeToolID string, target Target) (mapped []string, ok bool) {
	return ToolWithOverride(claudeToolID, target, nil)
}

// ToolWithOverride applies an invocation-specific source-to-target mapping
// before consulting the curated table. Tool overrides map one canonical tool
// to one target tool; curated mappings may still expand to multiple tools.
func ToolWithOverride(claudeToolID string, target Target, overrides map[string]string) (mapped []string, ok bool) {
	if override, exists := overrides[claudeToolID]; exists {
		return []string{override}, true
	}
	for _, entry := range toolTable {
		if entry.Claude == claudeToolID {
			m, found := entry.Targets[target]
			if !found || len(m) == 0 {
				return nil, false
			}
			return m, true
		}
	}
	return nil, false
}

// ReverseTool maps a target-specific single tool ID back to its canonical
// Claude tool ID. Only one-to-one target mappings participate in reverse
// lookup; expanded mappings like Factory Write -> [Edit, Create] intentionally
// do not reverse from individual elements.
func ReverseTool(targetToolID string, target Target) (claudeToolID string, ok bool) {
	for _, entry := range toolTable {
		mapped, found := entry.Targets[target]
		if !found || len(mapped) != 1 {
			continue
		}
		if mapped[0] == targetToolID {
			return entry.Claude, true
		}
	}
	return targetToolID, false
}
