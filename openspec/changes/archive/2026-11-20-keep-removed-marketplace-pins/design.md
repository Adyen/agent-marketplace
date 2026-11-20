## Context

`update-output` currently requires `update` to exit 1 when a Marketplace pin's latest-revision lookup returns 404 (archived change `2026-10-16-continue-update-past-missing-marketplace-resource`). A local reproduction on 2026-11-18 showed that after a resource is removed from a Marketplace registry, the latest lookup returns 404 while the exact pinned revision and its archive still return 200. `sync`, `sync --check`, `sync --locked`, and a fresh clone with an empty cache all keep working; only `update` fails, and it fails on every run.

## Goals / Non-Goals

**Goals:** let a user keep a removed resource with a clean `update`, keep reminding them on every `update`, and make the removal visible in `status` and the TUI update result.

**Non-Goals:** marking the TUI resource row at startup (it would need a latest-revision lookup per pin during startup); an acknowledgement setting that silences the warning; changing Git registry refusal; changing revoked-revision handling; changing lock creation when no lock exists (separate change `recover-missing-marketplace-selection-without-lock`).

## Decisions

- **Usable pinned revision decides warning versus error.** The pin is usable when the exact revision lookup succeeds and passes the existing assessment and revocation checks, or when the verified local cache holds its revision metadata and artifact. Otherwise the existing missing-resource error and exit 1 remain. Rationale: the user is only blocked when the resource genuinely cannot be installed.
- **The warning repeats on every update.** Human decision (2026-11-18): repeat rather than add an acknowledgement field to configuration.
- **Required pins warn too, for now.** Human decision (2026-11-18): a required pin removed upstream with a usable pinned revision warns and names the configuration entry that marks it required. This may change with the resource-prerequisites PR stack (#626 → #643 → #661 → #671 → #672).
- **Status keeps STATE.** Human decision (2026-11-18): STATE stays as computed (for example `current`); DETAIL gains `removed from the Marketplace; pinned revision kept`. No new STATE value enters the output format.
- **TUI shows the warning, not a row marker.** Human decision (2026-11-18).

## Risks / Trade-offs

- Scripts that relied on exit 1 to detect a removed resource now see exit 0 with a warning. The warning text names the resource so it remains detectable; the how-to documents the change.
