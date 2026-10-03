## Why

Captured skill support snapshots currently reject contained directory aliases that previously installed successfully. Native installation and read-only checking must agree without permitting an alias to import bytes outside the captured source root.

## What Changes

- Accept contained support directory aliases and alias chains in both materialization modes and read-only checking.
- Preserve regular singly-linked payload snapshots, executable permissions, VCS exclusions, and file-mode no-ops.
- Refuse escaping, dangling, cyclic, and unsafe-payload aliases without replacing the installed generation.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `materialization`: clarify contained support directory alias compatibility and refusal behavior.

## Impact

Skill support copying and portable virtual source hashing; shared captured-directory authority gains a strict containment entry point without changing existing destination policy.
