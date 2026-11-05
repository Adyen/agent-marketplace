## Context

The audit compares fetched model inventories with the catalogs in `pkg/harness-compilation/modelpolicy/catalogs/`. A catalog must cite a source URL before the audit compares it. Existing sources fail closed on unparseable pages with `source.parse-failed`; the Gemini API docs page, the Devin CLI snapshot, and the Cursor page need inventory-level handling.

## Decisions

- **Gemini API docs source.** A new source with Kind `gemini-docs` under Harness `gemini`. The parser reads inline-code model identifiers from list items and rejects a page with none. `ai.google.dev` joins `trustedURL`. `gemini.yaml` cites the page in `evidence.refs` so the audit accepts it; no `gemini-cli` model entry is added because `gemini-cli` does not list `gemini-4-argon`.
- **Inventory-unparsed.** For inventory kinds, a page that yields no model list reports `source.inventory-unparsed` instead of `source.parse-failed`. Other fetch and parse failures keep their existing codes.
- **Family pins.** A family member matches when its identifier contains the family name followed by a version segment. Versions are compared as numeric component lists, with missing trailing components treated as zero, so `claude-haiku-5-5` is newer than `claude-haiku-4-5-20251001`. Only the observed inventory of the same source is compared.
- **Devin snapshot.** `testdata/devin-models.json` is the raw output of `devin models list --format json`, captured locally by the developer on this host. The coverage test reads `devin.yaml` model and reasoning entries. Fusion pairings stay as they are.
- **Devin Haiku.** Devin's Haiku resolves to the same IR identity as Claude's `haiku`, through the `anthropic/haiku` family record, which is kept unchanged. `anthropic/claude-haiku-5-5` is added as a concrete Devin entry with the CLI reasoning levels and no `haiku` alias, because the family selector already owns `haiku`; the loader rejects a duplicate encoding. Haiku 5.5 is Devin's only Haiku, so this is the same model. This supersedes the earlier plan to replace the family with an alias, and it is a stated deviation from the AC6 alias wording, approved by the user.
- **Fusion exclusion.** Fusion pairings are excluded from the Devin coverage requirement, as NG6 states. Coverage applies to non-Fusion snapshot UIDs only; no wildcard matching is introduced.
- **Baselines.** The new Gemini API source needs a baseline entry to pass `validateBaselines`. It is recorded from the reviewed fixture and not a refresh of any existing baseline.

## Open items

- Cursor model inventory: the captured Cursor page has display names only. The `cursor-agent` route is blocked (the local binary has no model-list command, and the harness CI image at `ghcr.io/adyen/agent-marketplace/harness-cursor:ci` cannot be pulled because `ghcr.io` is restricted on this host). Tracked as a blocked task.
