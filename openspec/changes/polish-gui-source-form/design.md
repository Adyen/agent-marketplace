## Context

SourceForm is shared by Settings and first-run setup. Settings owns asynchronous mutation diagnostics, while the controller translates the server's unavailable-directory error. The CLI uses internal/giturl safety predicates, which accept local and scp-style Git locations as well as supported URL schemes.

## Goals / Non-Goals

Implement the approved Add source form contract without changing server safety rules. Do not add a folder picker or check directory existence in the client.

## Decisions

- Keep validation in a reusable source-input module called by SourceForm. Mirror the canonical Git location predicate rather than using the browser URL parser, which would reject supported local and scp-style inputs.
- Use a shared table of inputs in Go and TypeScript tests to compare client acceptance with the real CLI predicate. Keep this test fixture separate from production code.
- Pass structured field errors into SourceForm and emit a field-edit event so Settings can clear the corresponding server error. Keep non-field diagnostics in Settings below the form and preserve existing asynchronous diagnostic ownership.
- Keep section headings unchanged. Apply one shared sans field-label class to the legend and input labels. Align the action to the right without a fixed width.
- Verify placement, computed label styles, accessible associations, and content-width action layout in a real browser at 1024px and the existing Settings minimum width. Retain light and dark before/after captures outside the repository.

## Risks / Trade-offs

Go and JavaScript string and URL handling differ. The parity table must include local paths, scp-style remotes, supported schemes, option-like inputs, traversal, whitespace, and unsafe transport helpers. Server-only checks remain authoritative and must not become stricter client checks by accident.
