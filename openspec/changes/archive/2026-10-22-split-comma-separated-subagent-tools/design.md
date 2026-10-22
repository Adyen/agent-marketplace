## Context

`bundle.LoadSubagent` read every string as a one-element list. Claude, Cursor and Devin source layout ingestion share it, so the documented comma-separated `tools` form reached the compilers as one tool named `Read, Grep` (#822).

## Goals / Non-Goals

**Goals:** read the documented comma-separated form as separate tools for `tools` and `disallowedTools`, without changing which subagents load.

**Non-Goals:** skill `allowed-tools`, the Factory, Gemini and Devin readers, and any new diagnostic.

## Decisions

- **Keep a value that is not well formed as it was.** #822 asks for an unparseable value to be refused with a coded diagnostic. The v4 source-revision manifest lists every resource of a registry root, selected or not, so refusing at ingestion drops a subagent that earlier releases ingested from that inventory. A lock written by an earlier release then fails every consumer of the root with `lock.manifest-mismatch`, and a lock written after the change fails on earlier releases. The `hook-resources` requirement that legacy Windsurf hook locks keep their inventory sets the precedent that older locks keep verifying. The value keeps the single entry earlier versions produced instead. Factory and Gemini still suppress such a subagent at compile time with `subagent.factory.tool-unknown` and `subagent.gemini.tool-unknown`, so no wider permission results. Alternative: refuse at ingestion behind a new source-revision contract version. Rejected for this fix because it needs a lock schema decision.
- **A backslash makes the next character literal.** Permission rules read `\(` and `\)` as literal parentheses and scoped tool entries use the same form, so the character after a backslash counts as neither a parenthesis nor a separator. An escape that leaves the parentheses unbalanced, or a backslash with nothing after it, makes the string not well formed, so it falls back to the single entry.
- **Digests.** Source-revision digests hash the raw source bytes of file-backed resources, so they do not change. Marketplace artifacts are built from the ingested resource, so their capability entries, tool-grant texts and digests change for a re-ingested string-form subagent.

## Risks / Trade-offs

- [A malformed string still reaches Claude as one combined entry, as before] → Claude receives it as written and Factory and Gemini refuse it.

## Follow-ups

- Skill `allowed-tools` (`bundle/skill.go` line 108) keeps the one-entry reading, and Claude also documents space separation there.
- The Factory, Gemini and Devin ingesters keep private `toStringSlice` copies with the same one-entry reading of a string.
- Cursor workspace subagents are absent from the source-revision inventory (pre-existing, also in v5.0.3), so the Cursor lock check does not exercise this change.
- The Devin compiler splits each tool entry on commas again (`compile/devin/fields.go` line 309), so it does not understand the parenthesis grouping the loader now keeps.
