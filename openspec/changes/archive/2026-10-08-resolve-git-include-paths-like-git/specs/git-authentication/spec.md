## ADDED Requirements

### Requirement: Include paths resolve to the files Git reads

The reader MUST resolve each ordinary `include.path` value to the same file Git reads. Relative and `~/` paths MUST NOT be cleaned lexically, so `..` after a symlinked directory MUST resolve through its physical parent. `%(prefix)/` paths MUST resolve against the installed Git's runtime prefix. Every resolved path remains subject to the same trust checks and limits.

#### Scenario: Include paths resolve as Git resolves them

- **WHEN** a trusted chain declares relative and `~/` includes whose `..` follows a symlinked directory, and a `%(prefix)/` include
- **THEN** the reader MUST copy authentication settings from the same files native Git reads, in declaration order
