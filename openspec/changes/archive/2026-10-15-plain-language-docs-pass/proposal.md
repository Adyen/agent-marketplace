## Why

The fact audit checked the public documentation against source and specifications, and it fixed what was wrong. It left the prose as it was. Across roughly 72,000 words the net change was about 155 lines removed, and most pages changed by one to four lines. Many sentences still add nothing: throat-clearing openings, restated tables, and hedges. Some phrases read as stock or machine-written.

## What Changes

- Cut prose on every page under `site/docs/` to plain English. Remove throat-clearing, repetition, filler hedges, and stock phrases. Use short words, active voice, and a clear actor.
- Keep every fact: numbers, flags, paths, commands, exit codes, environment variables, error strings, conditions, and caveats. A sentence that carries a fact is shortened, not dropped.
- Keep fenced code blocks, inline code, command output, and structured examples byte-identical.
- Apply the same pass area by area: reference, explanation, consumer how-to, author how-to, operator how-to, tutorials, and top-level pages.
- Keep the `openspec/config.yaml` glossary terms as they are.

## Capabilities

### New Capabilities

None. This change edits prose only.

### Modified Capabilities

None. Runtime behavior and normative requirements remain unchanged.

## Impact

Markdown pages under `site/docs/`, excluding `README.md` and `site/docs/how-to/convert-a-harness-layout.md`, which other changes own. No page moves, renames, or deletions, and no link or sidebar changes except where a link text is edited. No runtime, test, or normative-spec changes.
