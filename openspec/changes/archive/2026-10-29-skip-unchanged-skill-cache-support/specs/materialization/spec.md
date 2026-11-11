## ADDED Requirements

### Requirement: Unchanged skill cache preparation is a no-op

During a real sync, Agent Marketplace MUST leave a cache-backed skill's compiled cache untouched when that cache already holds exactly the tree preparation would publish: the same compiled artifacts, support bytes and modes, and directories, and no other entries. Both trees MUST be read through captured handles. A difference, a missing or unreadable cache, a refused support source or a changed cache namespace MUST fall back to full preparation, keeping its existing staging, pruning, diagnostics and refusal behavior. This requirement does not add a cross-invocation cache, a trust marker or a new limit.

#### Scenario: Unchanged skill cache support repeats without mutation

- **WHEN** a real sync repeats for a cache-backed skill whose source support files and compiled cache are unchanged
- **THEN** it MUST create no support stage and MUST preserve the identity, mode and modification time of every cached entry and of the installed link

#### Scenario: Skill cache preparation reconciles support drift

- **WHEN** source support bytes, modes or membership change, or the cache gains, loses or alters an entry or has one replaced by a link
- **THEN** preparation MUST stage support again and leave the cache exactly matching the desired tree
