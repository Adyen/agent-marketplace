## Context

The Cursor CLI inventory is read from `agent models` output captured in the scheduled audit job. The parser currently accepts every `<id> - <display name>`-shaped line, and the audit discards a valid CLI inventory when the Cursor documentation fails to parse.

## Decisions

- Cursor CLI rows count only after an `Available models` header (case-insensitive, optional trailing colon). The header is the boundary that separates model rows from CLI diagnostics.
- When the documentation parse fails but a non-empty CLI override exists, the audit reports `source.parse-failed` (status `unknown`), uses the CLI inventory as the observed models, and computes the evidence digest from that inventory because no documentation evidence exists.
- Gemini API documentation continuation lines count only when they directly follow a bullet and contain nothing but backtick code spans; a blank or other line ends the bullet.
- `gemini-api-models` baseline digest is updated to the digest of the extracted inventory, because the observed set changed by one model (`gemini-3.5-transcribe-live`) from the same captured page.
