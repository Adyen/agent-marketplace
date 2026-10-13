package mapping

// modelEntry maps a Claude model family or specific ID to per-target model IDs.
type modelEntry struct {
	Claude  string // Source Claude model value
	Targets map[Target]string
}

// Sentinel target cells used by modelTable.
const (
	// modelInherit means the target inherits the original Claude model ID
	// unchanged. Model() returns (claudeValue, false) so the caller can emit a
	// passthrough diagnostic while still writing a value.
	modelInherit = "inherit"
	// modelUnmappable means the target has no stable identifier for the source
	// model and the value must not be guessed or passed through. Model()
	// returns ("", false) so the caller omits the field and emits a warning.
	modelUnmappable = "unmappable"
)

// modelTable is the curated translation table for Claude model families.
//
// Each entry maps a Claude model family name or specific model ID to the
// equivalent model ID for each target harness. When a target maps a model
// to modelInherit, the target inherits the original Claude model ID unchanged;
// when it maps to modelUnmappable, the model is omitted entirely.
//
// Reading the provenance stamps. A `// Last verified: <date> against <tool>`
// stamp above a row covers the SOURCE-side entry it names (the Claude row
// against Claude Code, the Gemini rows against the Gemini CLI); it does not
// date the other cells. Any cell carrying its own date — every Devin cell whose
// value rests on an observation does, 2026-08-05 from an authenticated
// `devin models list` — is dated by that comment, not by the stamp above the
// row. So a 2026-04-24 stamp above a row
// whose Devin cell says 2026-08-05 is not stale provenance: they are two
// independent captures of two different halves of the same row.
//
// Reading the Devin cells. Devin cells deliberately target ALIASES (`sonnet`,
// `opus`, `haiku`, `gemini`) and deliberately do not pin a versioned family,
// because a pinned Devin family name is not a verified stable identifier. The
// consequence is intended and must not be read as an inconsistency: an alias
// floats across model generations, so Devin's alias may serve a NEWER family
// than the pinned Claude/Factory/Cursor IDs sitting in the same row.
//
// Codex provenance for every Codex cell below: verified 2026-08-19 against
// https://developers.openai.com/codex/subagents. The official examples name
// Codex-native models but define no Claude/Gemini equivalence. Every existing
// cross-vendor source row is therefore explicitly unmappable; guessing a Codex
// model would violate the table's honest-loss contract.
var modelTable = []modelEntry{
	// --- Families (aliases) ---

	// Last verified: 2026-04-24 against Claude Code v1.0
	{Claude: "sonnet", Targets: map[Target]string{
		TargetClaude:   "claude-sonnet-4-5-20250929",
		TargetFactory:  "claude-sonnet-4-5-20250929",
		TargetCursor:   "claude-sonnet-4-5-20250929",
		TargetWindsurf: modelInherit,
		TargetGemini:   "gemini-3-preview",
		// Devin: the `sonnet` alias resolved to Claude Sonnet 5 in an
		// AUTHENTICATED `devin models list` on 3000.3.27, 2026-08-05. Deliberate
		// cross-generation float: this cell targets the ALIAS and does not pin a
		// versioned family, so Devin's `sonnet` may serve a newer family (here
		// Sonnet 5) than the Claude/Factory/Cursor IDs pinned in this same row
		// (Sonnet 4.5) — intended, not a mismatch. That check
		// needs a logged-in account, so the unauthenticated probe suite cannot
		// reproduce it; it is a recorded capture, not a local assertion. Evidence:
		// docs/devin-cli-harness-spec-3000.3.md and internal/devinprobe/manual.go
		// entry model-alias-list.
		TargetDevin:    "sonnet",
		TargetCodex:    modelUnmappable,
		TargetOpenCode: "anthropic/claude-sonnet-4-5-20250929",
	}},

	// Last verified: 2026-04-24 against Claude Code v1.0
	{Claude: "opus", Targets: map[Target]string{
		TargetClaude:   "claude-opus-4-20250514",
		TargetFactory:  "claude-opus-4-20250514",
		TargetCursor:   "claude-opus-4-20250514",
		TargetWindsurf: modelInherit,
		TargetGemini:   "gemini-3-preview",
		// Devin: the `opus` alias resolved to Claude Opus 5 in an AUTHENTICATED
		// `devin models list` on 3000.3.27, 2026-08-05. Authenticated-only, so the
		// unauthenticated probe suite cannot reproduce it. Evidence:
		// docs/devin-cli-harness-spec-3000.3.md and internal/devinprobe/manual.go
		// entry model-alias-list.
		TargetDevin:    "opus",
		TargetCodex:    modelUnmappable,
		TargetOpenCode: "anthropic/claude-opus-4-20250514",
	}},

	// Last verified: 2026-04-24 against Claude Code v1.0
	{Claude: "haiku", Targets: map[Target]string{
		TargetClaude:   "claude-haiku-3-5-20241022",
		TargetFactory:  "claude-haiku-3-5-20241022",
		TargetCursor:   "claude-haiku-3-5-20241022",
		TargetWindsurf: modelInherit,
		TargetGemini:   "gemini-3-flash-preview",
		// Devin: the `haiku` alias resolved to Claude Haiku 4.5 in an AUTHENTICATED
		// `devin models list` on 3000.3.27, 2026-08-05. Deliberate
		// cross-generation float: this cell targets the ALIAS and does not pin a
		// versioned family, so Devin's `haiku` may serve a newer family (here
		// Haiku 4.5) than the Claude/Factory/Cursor IDs pinned in this same row
		// (Haiku 3.5) — intended, not a mismatch. Authenticated-only, so the
		// unauthenticated probe suite cannot reproduce it. Evidence:
		// docs/devin-cli-harness-spec-3000.3.md and internal/devinprobe/manual.go
		// entry model-alias-list.
		TargetDevin: "haiku",
		TargetCodex: modelUnmappable,
		// OpenCode: the previously emitted dated identifier was never verified
		// against the pinned OpenCode 1.18.34 model catalog, and an unverified
		// identifier is not a supported native model. The plan forbids both
		// guessing another model and passing an unverified one through, so the
		// alias is unmappable: translation omits the model and reports the loss.
		TargetOpenCode: modelUnmappable,
	}},

	// --- Specific model IDs ---

	// Last verified: 2026-04-24 against Claude Code v1.0
	{Claude: "claude-sonnet-4-5-20250929", Targets: map[Target]string{
		TargetClaude:   "claude-sonnet-4-5-20250929",
		TargetFactory:  "claude-sonnet-4-5-20250929",
		TargetCursor:   "claude-sonnet-4-5-20250929",
		TargetWindsurf: modelInherit,
		TargetGemini:   "gemini-3-preview",
		// Devin: no stable alias for a pinned/versioned model family. The plan
		// forbids guessing versioned IDs, so this is unmappable (omit + warn),
		// not an inherit-style passthrough.
		TargetDevin:    modelUnmappable,
		TargetCodex:    modelUnmappable,
		TargetOpenCode: "anthropic/claude-sonnet-4-5-20250929",
	}},

	// Last verified: 2026-04-24 against Claude Code v1.0
	{Claude: "claude-opus-4-20250514", Targets: map[Target]string{
		TargetClaude:   "claude-opus-4-20250514",
		TargetFactory:  "claude-opus-4-20250514",
		TargetCursor:   "claude-opus-4-20250514",
		TargetWindsurf: modelInherit,
		TargetGemini:   "gemini-3-preview",
		// Devin: no stable alias for a pinned/versioned model family. The plan
		// forbids guessing versioned IDs, so this is unmappable (omit + warn),
		// not an inherit-style passthrough.
		TargetDevin:    modelUnmappable,
		TargetCodex:    modelUnmappable,
		TargetOpenCode: "anthropic/claude-opus-4-20250514",
	}},

	// Last verified: 2026-04-24 against Claude Code v1.0
	{Claude: "claude-haiku-3-5-20241022", Targets: map[Target]string{
		TargetClaude:   "claude-haiku-3-5-20241022",
		TargetFactory:  "claude-haiku-3-5-20241022",
		TargetCursor:   "claude-haiku-3-5-20241022",
		TargetWindsurf: modelInherit,
		TargetGemini:   "gemini-3-flash-preview",
		// Devin: no stable alias for a pinned/versioned model family. The plan
		// forbids guessing versioned IDs, so this is unmappable (omit + warn),
		// not an inherit-style passthrough.
		TargetDevin: modelUnmappable,
		TargetCodex: modelUnmappable,
		// OpenCode: unmappable for the same reason as the haiku alias above —
		// the dated identifier's acceptance by the pinned native was never
		// verified, and no verified OpenCode identifier exists for it.
		TargetOpenCode: modelUnmappable,
	}},

	// --- Gemini-native model families (for Gemini→other compilation) ---

	// Last verified: 2026-04-24 against Gemini CLI
	{Claude: "gemini-3-preview", Targets: map[Target]string{
		TargetClaude:   "sonnet",
		TargetFactory:  "claude-sonnet-4-5-20250929",
		TargetCursor:   "claude-sonnet-4-5-20250929",
		TargetWindsurf: modelInherit,
		TargetGemini:   "gemini-3-preview",
		// Devin: unmappable for a positive reason, not for lack of looking, but
		// state the reason exactly. An AUTHENTICATED `devin models list` on
		// 3000.3.27, 2026-08-05 showed a `gemini-3.1-pro` family that exists but
		// carries NO alias, while `gemini` resolves to Flash (see the
		// gemini-3-flash-preview row). Whether a bare family name such as
		// `gemini-3.1-pro` is ACCEPTED as a `model:` value was NOT tested — the
		// capture read the catalogue, it did not exercise the field — so "no
		// alias" must not be restated as "no usable identifier". What settles
		// this cell is the plan: it forbids depending on an identifier whose
		// acceptance has not been verified, and no verified Pro-class identifier
		// exists. Hence unmappable (omit + warn) rather than an inherit-style
		// passthrough. Authenticated-only, so the unauthenticated
		// probe suite cannot reproduce it. Evidence:
		// docs/devin-cli-harness-spec-3000.3.md and internal/devinprobe/manual.go
		// entry model-alias-list.
		TargetDevin: modelUnmappable,
		TargetCodex: modelUnmappable,
		// OpenCode: unmappable — the gemini-3-preview identifier's acceptance by
		// the pinned OpenCode 1.18.34 catalog was never verified, and
		// substituting an unrelated Google model is forbidden.
		TargetOpenCode: modelUnmappable,
	}},

	// Last verified: 2026-04-24 against Gemini CLI
	{Claude: "gemini-3-flash-preview", Targets: map[Target]string{
		TargetClaude:   "haiku",
		TargetFactory:  "claude-haiku-3-5-20241022",
		TargetCursor:   "claude-haiku-3-5-20241022",
		TargetWindsurf: modelInherit,
		TargetGemini:   "gemini-3-flash-preview",
		// Devin: the `gemini` alias resolved to Gemini 3.5 Flash in an
		// AUTHENTICATED `devin models list` on 3000.3.27, 2026-08-05 — a Flash
		// family, matching this source family. Deliberate cross-generation float:
		// this cell targets the ALIAS and does not pin a versioned family, so
		// Devin's `gemini` may serve a newer Flash generation (here 3.5) than the
		// pinned Gemini/Claude/Factory/Cursor IDs in this same row — intended, and
		// the class (Flash) is what the mapping preserves. Authenticated-only, so the
		// unauthenticated probe suite cannot reproduce it. In the same capture a
		// `gemini-3.1-pro` family existed with NO alias, which is why the
		// gemini-3-preview (Pro-class) row above stays unmappable. Evidence:
		// docs/devin-cli-harness-spec-3000.3.md and internal/devinprobe/manual.go
		// entry model-alias-list.
		TargetDevin: "gemini",
		TargetCodex: modelUnmappable,
		// OpenCode: unmappable — like gemini-3-preview, the flash identifier's
		// acceptance by the pinned native was never verified. Unlike Devin's
		// floating `gemini` alias, an OpenCode catalog ID is pinned, so an
		// unverified ID is unmappable rather than inherit-style passthrough.
		TargetOpenCode: modelUnmappable,
	}},
}

// Model returns the target-specific model ID for a given Claude model value.
// If the model is found in the curated table, the mapped value is returned
// with ok=true. If the model maps to modelInherit for the target, the original
// claudeValue is returned (the caller should emit a W_MODEL_PASSTHROUGH diagnostic).
// If the model maps to modelUnmappable, ("", false) is returned: the target has
// no stable identifier for it, so the caller must omit the model and emit an
// unmappable-model warning instead of writing the source value.
// If the model is not found at all, ok=false means the value should be
// passed through unchanged and the caller should emit a W_MODEL_PASSTHROUGH
// diagnostic.
func Model(claudeValue string, target Target) (mapped string, ok bool) {
	return ModelWithOverride(claudeValue, target, nil)
}

// ModelWithOverride applies an invocation-specific source-to-target mapping
// before consulting the curated table. Overrides are intentionally keyed by
// the canonical source value so the same option works for every target.
func ModelWithOverride(claudeValue string, target Target, overrides map[string]string) (mapped string, ok bool) {
	if override, exists := overrides[claudeValue]; exists {
		return override, true
	}
	for _, entry := range modelTable {
		if entry.Claude == claudeValue {
			m, found := entry.Targets[target]
			if !found {
				return claudeValue, false
			}
			if m == modelUnmappable {
				return "", false
			}
			if m == modelInherit {
				return claudeValue, false
			}
			return m, true
		}
	}
	return claudeValue, false
}

// ReverseModel maps a target-specific model ID back to its canonical Claude
// value. If multiple Claude values map to the same target ID, the first entry
// in the curated table wins, which prefers family aliases over fully expanded
// IDs.
func ReverseModel(targetValue string, target Target) (claudeValue string, ok bool) {
	for _, entry := range modelTable {
		mapped, found := entry.Targets[target]
		if !found || mapped == modelInherit || mapped == modelUnmappable {
			continue
		}
		if mapped == targetValue {
			return entry.Claude, true
		}
	}
	return targetValue, false
}
