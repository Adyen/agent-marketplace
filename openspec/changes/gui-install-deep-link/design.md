## Context

See proposal.md for motivation. This layer depends on the user-scope GUI and local API. Deep-link inputs are untrusted; all resolution and confirmation must use the same Marketplace and mutation policies as the interactive client.

## Goals / Non-Goals

**Goals:** Deliver an explicit, reviewable install path from a hosted Marketplace resource to the local GUI without weakening revision verification, security policy, or the user-scope boundary.

**Non-Goals:** Silent installation, persisted pending links while a mutation is busy, auto-updates, arbitrary configuration editing, or bypassing Marketplace identity and allowlist checks.

## Decisions

- **URL registration:** Register `agent-marketplace` with macOS `Info.plist` `CFBundleURLTypes` and Linux `.desktop` `MimeType=x-scheme-handler/agent-marketplace;`. Route new and already-running launches through the same handler.
- **Single-instance handoff:** Use Wails v2 `SingleInstanceLock` to focus the existing window and deliver the full launch request. If first run is incomplete, complete it before showing the retained pending install confirmation.
- **Parsing:** Parse only the specified `agent-marketplace://install` form. Treat every parameter as untrusted; reject unknown keys, duplicates, malformed values, and values beyond explicit input-size limits with a visible reason before any write. A supplied digest selects an exact Resource revision; no digest resolves the latest installable revision.
- **Plan and confirmation:** Resolve the Resource and full install plan using the CLI/TUI dependency reconciliation path. Show Marketplace, Resource coordinate, Resource revision, user scope, Harness targets, all dependencies and collection members, and the command notice for hooks and MCP servers before confirmation. Confirmation is an explicit affirmative action; cancellation remains write-free.
- **Policy:** Verify exact revisions against the Marketplace immediately before installation. Reuse effective `allowed_marketplaces` evaluation and the Marketplace UI's concise refusal reason for Revoked revisions and security-policy blocks. For an unregistered endpoint, refuse if the allowlist blocks it; otherwise verify endpoint identity, warn, and offer registration in the user-personal root before install confirmation.
- **Busy behavior:** Reject a link received during sync, update, install, or remove with a busy notice and discard it; do not queue it for later execution.
- **Hosted UI:** In `marketplace-ui`, place “Open in app” beside the command for installable resources and retain both actions regardless of app detection. Keep copy behavior unchanged. Unavailable resources retain the concise notice and no “Open in app” action.

## Risks / Trade-offs

- [A scheme URL can contain ambiguous or oversized data] → Reject unknown, repeated, malformed, and oversized fields before network access or writes; test encoded and delimiter edge cases.
- [Endpoint registration or a confirmation race changes the plan] → Recheck endpoint identity, allowlist, current revision, operation state, and complete plan immediately before the guarded mutation.
- [Platform launch delivery drops or duplicates a request] → Exercise cold launch, second-instance handoff, and first-run continuation on both package platforms; make each request single-consumption.
