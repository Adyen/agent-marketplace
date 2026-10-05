## Why

PR #497's durable aggregate reader records a canonical root pathname at Store admission but does not retain an existing root's directory identity. Replacing that root with an ordinary directory after lock acquisition can import a copied aggregate and report a successful no-op instead of refusing the changed generation.

## What Changes

- Retain the identity of an existing canonical target root at ownership Store admission without creating files or keeping an unbounded lifetime descriptor.
- Check that identity against the captured root before durable aggregate payload reads, refusing replacement or disappearance as a generation conflict.
- Preserve optional missing-root and legitimate later-creation behavior, authorized root resolution, and ordinary updates within the same directory.
- Prove refusal through native Gemini Run and retain positive controls.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `owned-entry-reconciliation`: durable reads retain existing root admission identity.
- `materialization`: native Gemini sync refuses an ordinary replacement root after lock acquisition.

## Impact

Ownership Store admission and scoped aggregate reads, native aggregate tests, and focused benchmarks. No ownership-state schema, disk format, dependency, permission policy, or all-output transaction changes.
