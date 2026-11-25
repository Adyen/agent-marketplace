## Context

Machine-scope Factory Droid hooks already materialize into `~/.factory/settings.local.json#hooks` and retire owned `~/.factory/hooks.json` entries atomically, while released clients carrying the repository forward-compatibility reader already recognize `.factory/settings.local.json#hooks` without duplicating `.factory/hooks.json`.

## Goals / Non-Goals

**Goals:**
- Switch `TargetFactory` repository-scope hook output to `.factory/settings.local.json` under `hooks`, reusing the existing data-driven `RetiredNative` ownership migration and `LegacyMemberSet` top-level `hooks/hooks.json` migration.
- Preserve coexistence in `.factory/settings.local.json` with handwritten settings, handwritten hooks, and Factory plugin registrations (`enabledPlugins`, `extraKnownMarketplaces`).

**Non-Goals:**
- Changing consumer configuration, acquisition lock formats, or TUI behavior.
- Pinning an unreleased client version in `released-client-gate`; pin only after the reader release is published.

## Migration Plan

1. Merge and release the prerequisite reader PR as a stable GitHub release (e.g. `v5.2.0`).
2. Update `releasedClientVersion`, `releasedClientArch`, and `.github/workflows/ci.yml` to pin that real release's verified checksums and verify `make released-client-gate` passes.
3. Archive this OpenSpec change (`openspec archive factory-hooks-settings-local-repo --yes`) and mark the PR ready for review.
