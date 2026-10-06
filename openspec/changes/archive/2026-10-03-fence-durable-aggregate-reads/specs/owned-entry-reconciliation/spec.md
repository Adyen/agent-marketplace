## ADDED Requirements

### Requirement: Durable aggregate reads retain canonical-root opened-file authority

Agent Marketplace durable aggregate reconciliation, repair, safety-backup, compare-and-swap and recovery live-payload readers MUST consume through captured authority below the ownership store's recorded canonical root. Relative path validation and strict ancestors MUST refuse intermediate/final aliases and traversal before payload reads. Opened files MUST remain regular, singly linked and nonblocking at captured size with state/named-object validation. Genuine absence MUST retain errors.Is and legacy os.IsNotExist; unsafe/generation or cleanup failures MUST NOT become absence. Explicit target-root aliases canonicalized at store admission MUST remain supported. Refusal MUST preserve original/foreign bytes and metadata. Publication/state/journal/archive mutation remains separate; no arbitrary new limits, transaction or exclusion of uncooperative writers is introduced.

#### Scenario: Scoped aggregate reader refuses intermediate alias

- **WHEN** a canonical-root aggregate reader encounters an intermediate alias to an external regular payload after a valid native read control
- **THEN** it MUST refuse with typed unsafe-file identity before returning bytes and preserve foreign payload bytes, identity, mode and mtime
