## Why

Installing a Marketplace resource from the TUI materialized the workspace's Git sources from their mutable ref instead of the commit pinned in the acquisition lock (#823). Unreviewed upstream content reached the harness folders, the lock still pinned the old commit, and every install left a spurious drift that the next sync reverted. The CLI install of the same resource kept every Git source at its locked commit. No spec described TUI Marketplace installs, and no real-terminal journey exercised them.

## What Changes

- The TUI passes the same dependency reconciler as the CLI to Marketplace installs from discovery, so the post-install sync uses exact locked snapshots.
- `tui-interactions` specifies Marketplace installs from discovery: the exact pin at the chosen scope, consent only for plans with security findings, nothing written when declined, and other locked sources kept at their locked revision.
- Two TUI QA journeys install a discovered Marketplace skill: one while the Git upstream has moved past the lock, and one whose findings require consent.

## Capabilities

### New Capabilities

### Modified Capabilities
- `tui-interactions`: Marketplace installs from discovery.

## Impact

`internal/tui` discovery installs and the TUI's production dependency wiring in `cmd/agent-marketplace`. The TUI QA Marketplace stub can answer a search for its installable skill and assess it with a finding. CLI installs are unchanged.
