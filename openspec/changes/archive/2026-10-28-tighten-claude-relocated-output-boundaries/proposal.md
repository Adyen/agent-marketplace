## Why

Adversarial review of #970 reproduced two gaps in relocated Claude Code output:
recursive marker cleanup can strip owned entries from another account or
repository beneath the selected root, and native hook support verification
rejects an otherwise admitted machine-root alias.

## What Changes

- Bound marker-based cleanup of relocated Claude Code output to the aggregate
  paths declared by the harness target, including empty component plans.
- Carry admitted parent and regular-file authority into cleanup so a later
  component retarget cannot turn an eligible path into foreign-file access.
- Reuse admitted destination boundaries for hook support directories and
  descriptor-relative support-link snapshots; retain strict standalone behavior.
- Prove both fixes in both materialization modes, including real stale-entry
  cleanup, unrelated-file bytes/metadata, hook publication and a clean check.

## Capabilities

### Modified Capabilities

- `claude-native-configuration`: explicit cleanup boundaries and complete hook
  support through admitted directory aliases.

## Impact

Internal materialization and native hook support verification. No consumer,
lock, artifact, ownership-state, or transaction format changes.

## Release note

Claude Code output relocated with `CLAUDE_CONFIG_DIR` no longer prunes unrelated
JSON files beneath that directory, and native hooks work through admitted
machine-root directory aliases.
