# Proposal

## Why

Agent Marketplace can deliver resources only into agent harnesses. Non-agent applications such as the Herdr app read user-root files (mods, layouts, themes) that teams want to share with the same review, lock, ownership and recovery guarantees. That needs a generic way to place reviewed package files into an approved application root, plus profile selection, consent and verification. A failed or interrupted write must never adopt, overwrite or delete files the engine cannot prove it owns.

## What Changes

- Target profiles describe a place target: its ID, macOS/Linux user-root location, package mapping and an optional initial-load check. They are selected and locked by exact identity. A package assessment binds the package identity, the mapping and the full profile digest, both online and offline.
- `target enable <id>` and `target disable <id>` add or remove only the target ID for a selected, known profile that organization policy admits. Disabling never deselects the profile or retires its installation.
- An interactive approval receipt binds the exact OS, target, profile digest, canonical target root, state store, command roles and, when commands exist, the canonical executable and its digest. Organization preapproval matches exact canonical roots only. Approval never authorizes deleting files under an old root; retirement is an explicit operation on a recorded installation ID.
- Selection outcomes:
  - Directly selecting a disabled, unavailable, unapproved or policy-blocked target refuses before acquisition, config or lock changes.
  - Collections keep such members and report them as skipped.
  - Headless `sync` skips targets that are still waiting for consent and continues independent work.
  - `sync --check` stays read-only.
- Place delivery:
  - The engine plans the whole package set and refuses unowned, new/new and installed/new conflicts without adopting files.
  - It stages files in agent-marketplace state, refuses cross-filesystem publication and publishes file by file in dependency order.
  - It commits ownership after publication and then prunes, all under one dependency journal.
  - Recovery follows an explicit state machine.
  - Edited or deleted files keep their package's projection and are reported as drift.
- After placement, sync runs the approved initial-load check and records its result. `target verify <locator> --installation <id>` re-runs only that named check.

## Capabilities

### New Capabilities
- `target-profile-place`: profile identity and selection, approval and retirement, target enable/disable/verify, place planning, publication, ownership, recovery, drift and verification records.

### Modified Capabilities

## Impact

- `internal/config`: profile selection and target enable/disable.
- `internal/marketplace` and `internal/marketplacelock`: assessment binding.
- `internal/dependencytxn`: a place authority branch, an early journal, topological publish order and a prune phase.
- `internal/packagegen`: place generations and garbage-collection retention.
- `internal/ownership`: place records, created-directory identities and verification results.
- `internal/fsboundary` use and `cmd/agent-marketplace`: the `target` commands.
- Only test profiles ship. No real host profile, Windows, aggregates, register mode, installer, import manifest or dry-run is included.

## Release note

None. This change targets the `feat/target-profiles` integration branch and is not released by itself.
