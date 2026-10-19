## Why

When two selected MCP resources under different names compile to the same Codex `config.toml` key and the file already holds an unmanaged entry with that key, `doctor --repair` offered to replace it. The offer named the last of the two resources, but confirming wrote the first one's value, and the confirmation prompt showed only the key. Ordinary sync refuses the same duplicate key with `materialize.codex.duplicate-identity` and writes neither definition.

## What Changes

- Codex repair reports `materialize.codex.duplicate-identity` for a key that two selected resources produce, naming the key and both resources, whether or not `config.toml` exists, and offers neither adoption nor replacement for it. The key stays desired, so no orphan removal is offered for it. Other entries in the file remain repairable.
- An offered adoption or replacement names the resource whose generated value it writes, and that value's digest is the one the conflict reports.
- The interactive replacement confirmation names that resource and the generated value digest.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `owned-entry-reconciliation`: repair refuses duplicate desired Codex MCP identities and names what a replacement writes.

## Impact

`doctor --repair` preview and confirmation output for shared aggregate files. The confirmation prompt text changes. No configuration, lock or ownership-state format changes.
