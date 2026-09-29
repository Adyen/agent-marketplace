## Context

See proposal.md for the why and the spec delta for the required behavior. The parent PR archives `specify-resource-prerequisites` first. Its existing tests prove the three old behaviors and give the child a red check when rewritten for the fix. Catalog extraction reads `metadata.requires` from each resource's extra fields. `LoadFSWithOptions` builds catalog entries and retains ingestion diagnostics. Ad-hoc `list --from` reads those catalogs; `doctor` gets catalogs through `pipeline.Resolve`. Ordinary `sync` and `sync --check` have separate warning and exit paths.

## Goals / Non-Goals

**Goals:**

- Diagnose malformed prerequisites at the catalog boundary without dropping the declaring resource or leaking untrusted values.
- Apply strict policy to unmet prerequisites in both sync paths without affecting non-strict behavior.
- Keep the parent archive immutable as an accurate history of the previous behavior.

**Non-Goals:**

- Generalizing every resource metadata parser or changing the common `--kind` flag's interpretation of `all`.
- Promoting malformed advisory declarations to a hard load failure.
- Changing the `status` table or hosted Marketplace API/UI.

## Decisions

### Reuse catalog diagnostics, not logging as a UI transport

Keep the parsing policy in `internal/catalog`. A validation pass at catalog load creates diagnostics with one stable code, `catalog.requires.invalid`, a source file and resource identity, plus a zero-based item index for a list entry. Messages contain only fixed explanations of the failed field and index, never the raw value. The existing parser still returns valid items to resolution. `list --from` and `doctor` display resource-attached warnings from loaded catalogs, sorted and deduplicated when a catalog is exposed through multiple configured entries. Avoid converting warning diagnostics to malformed-resource entries: the resource is valid and remains discoverable.

- *Alternative:* print from `slog.Warn` or from resolution. Neither reaches `list --from` for unselected resources, and logging does not provide stable structured diagnostics. Avoid duplicating parsing policy between load and resolver; factor out a shared parse routine returning requirements plus issues if needed.

### Narrow `all` rejection to prerequisites

Reject `all` in the prerequisite kind parser before passing the other kind spellings through the existing kind parser. Do not change global `ParseKind` because `--kind all` is valid CLI behavior. Cover case-insensitive structured and shorthand forms with valid siblings.

### Change strict verdict without losing the warning

The normal sync path classifies unmet requirements as warnings. When strict, it must additionally return a failure without suppressing the printed warning or duplicating it as a fatal error. The check path currently ignores `strict` and already reports requirement warnings; pass strict policy explicitly to `runSyncCheck` while preserving its direct callers. Return nonzero for unmet warnings even when the output itself is in sync; do not write during `--check`. Non-strict warning behavior stays as it was.

- *Alternative:* add a new flag. The user selected existing `--strict` so existing CI commands gain enforcement without configuration changes.

### Specs and docs

The pinned OpenSpec validator refuses a MODIFIED requirement that omits or renames an old scenario. Remove the three old requirement blocks and add replacements with accurate titles instead. Keep unaffected scenario names and update annotations for the changed `all` and strict scenarios. Do not edit the parent's archived delta or rewrite its commit. Update the new human reference/how-to, CLI strict help and affected doc links on the child branch to describe the new behavior. Keep diagnostics and exit conditions factual rather than promising that the CLI detects runtime availability of MCP servers.

## Risks / Trade-offs

- [A wrong warning severity could mark an otherwise valid resource as malformed.] → Assert that valid and invalid siblings remain discoverable, that `list --from` and `doctor` print warnings, and that inspection exits successfully.
- [Untrusted metadata may contain URL credentials or terminal escapes.] → Use fixed messages, source path and index only; pass displayed fields through the existing terminal-safe output path, and test a secret-looking invalid value.
- [Strict failure may be hidden by a clean `--check` summary or by warning deduplication.] → E2E tests cover both sync paths against an already synchronized resource, and a satisfied control remains exit 0.
- [Parent baseline assertions contradict child behavior.] → Update the live tests and annotations for changed scenarios on the child; the archived parent document stays a historical record.
