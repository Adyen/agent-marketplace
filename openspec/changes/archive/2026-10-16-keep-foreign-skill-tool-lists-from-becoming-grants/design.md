## Context

`ir.Skill.ToolSource` records the harness whose native tool names `AllowedTools` carries. Devin, Factory and Gemini ingestion set it; other ingesters leave it empty, which means canonical Claude vocabulary.

Documented semantics of skill `allowed-tools`:
- Claude: pre-approval for the invoking turn; it does not restrict availability.
- Agent Skills (Cursor, Gemini): pre-approved tools.
- Devin: auto-approval; it does not restrict availability. Only a subagent profile's `allowed-tools` restricts.
- Factory: metadata for review, packaging and UI; it grants nothing.

## Decisions

1. **Provenance decides.** A list whose `ToolSource` is set and differs from the target is foreign and is omitted on Claude, Cursor and Gemini. Translating Devin's list would reproduce the author's auto-approvals, but the maintainer chose never to turn a foreign list into a grant (decision on #637).
2. **Canonical lists stay.** An empty `ToolSource` is Claude vocabulary with Claude's pre-approval meaning, which matches the Agent Skills meaning on Cursor and Gemini.
3. **One shared warning.** `skill.allowed-tools-omitted` names the target; the skill itself is still emitted because omission only removes pre-approvals.

## Non-goals

Devin, Factory, Codex, Windsurf and OpenCode skill targets; commands; subagent `tools`; ingesters that do not record skill tool provenance (Cursor, Windsurf, Codex, OpenCode).

## Verification

Public `Convert` regressions fail before the change: Devin and Factory skill lists reach Claude, Cursor and Gemini verbatim. Positive controls keep Claude→Claude and Gemini→Gemini lists unchanged.
