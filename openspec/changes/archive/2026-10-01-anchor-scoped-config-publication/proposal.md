## Why

The admitted R2 config publication cases allow repository-authored layer links to redirect edits into external configuration, and path-based publication can lose its captured scope after an ancestor replacement. Shared descriptor primitives do not remediate those consumer paths until config and lock readers, staging, migration and transaction publication use them.

## What Changes

- Bind the user, repository and personal-local config/lock destinations to opened scope descriptors for snapshots, staging and publication.
- Preserve current-user-owned regular user dotfile links and repository/local regular links contained in their layer root; refuse unsafe ancestors, escaping links, special files, hard links and cross-layer object aliases.
- Keep layer-owned lock locations logical even when config content is read from an approved link target.
- Preserve current LayerIntent, LayerEditor, missing-root v4 creation, read-only dangling-layer behavior and acquisition-lock schemas.
- Route live commits and recovery through the shared boundary and retain typed refusal identities and caller-owned staging behavior.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `config-mutation`: scoped publication and approved-link preservation.

## Impact

Config editors, structural/v4 migration, config/lock staging and dependency transactions, current deployed-lock consumers and purge-config. No new global size policy, dependency version, config schema or acquisition-lock wire-format changes. Other R2 destinations remain separate work.
