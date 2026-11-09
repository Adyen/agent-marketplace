## Why

Review of the Cursor model inventory found three gaps in the model catalog audit. The Cursor CLI parser accepts any `<id> - <display name>`-shaped line, so CLI diagnostics such as `error - Authentication required` are read as models. A Cursor documentation parse failure discards a valid CLI inventory instead of comparing it with `cursor.yaml`. The Gemini API documentation parser misses model IDs listed on continuation lines of a bullet, such as `gemini-3.5-transcribe-live`.

## What Changes

- Parse Cursor CLI output only from `<id> - <display name>` lines that follow an `Available models` header; output without the header or rows reports `source.inventory-unparsed`.
- When the Cursor documentation cannot be parsed but the CLI inventory parses, report `source.parse-failed` as `unknown` and still compare the CLI identifiers with `cursor.yaml`.
- Extract Gemini API documentation model IDs from backtick-only continuation lines that belong to a bullet item.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `model-catalog-audit`: tightens Cursor CLI inventory parsing and adds a Cursor documentation-failure scenario.

## Impact

- `tools/model-catalog-audit/` (extraction, audit, tests, `baselines.json` for `gemini-api-models`).
- No CLI, API, or harness output behavior changes; the audit report schema stays at version 1.

## Release note

none — the audit is a maintainer-facing CI tool and no user-facing harness output changes.
