## Why

Marketplace users should be able to hand an exact or latest Resource revision from the hosted UI to the local GUI, while retaining explicit consent, organization policy, and security checks before anything is installed.

## What Changes

- Add and validate the `agent-marketplace://install` link format, including exact revision pins and strict rejection of untrusted malformed input.
- Resolve links into a complete user-scope install plan and require explicit confirmation before any write or materialization.
- Handle unregistered Marketplaces, allowlist policy, revoked or blocked revisions, first-run completion, single-instance handoff, and busy operations safely.
- Add an “Open in app” action beside the unchanged command-copy action for installable hosted Marketplace resources; keep unavailable notices free of that action.

## Capabilities

### New Capabilities
- `gui-install-deep-link`: Deep-link parsing, Marketplace resolution, install confirmation, policy refusals, launch handoff, and busy behavior.

### Modified Capabilities
- `marketplace-web-ui`: Add the “Open in app” action for installable resources while preserving command copying and the unavailable installation notice contract.

## Impact

The desktop URL-scheme entry points, single-instance handoff, install-plan resolution and confirmation flow, and hosted Marketplace resource details and tests are affected. The GUI continues to write only user-personal configuration; link content is treated as untrusted input.
