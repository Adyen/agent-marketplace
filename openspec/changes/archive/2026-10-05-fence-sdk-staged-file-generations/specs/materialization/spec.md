## ADDED Requirements

### Requirement: SDK staged-file publication and cleanup retain writer generations

SDK Materialize MUST retain original staged payload state from opened writer descriptors and revalidate full state before publication. Tracked staged-file replacement or state change MUST refuse generation conflict before rename. Attempt cleanup MUST validate remaining tracked staged files before removing its private directory, preserve changed/replacement payloads and propagate refusal. A replacement-bearing attempt stage MUST remain intact rather than be reported as successfully cleaned. Healthy actual source-mode publication MUST clean only its fresh stage while preserving prior replacement evidence. Existing staged-all-artifacts, modes/no-op/dry/partial contracts remain compatible; existing fully-owned staging-directory treatment of untracked members is unchanged.

#### Scenario: SDK staged file replacement preserves payload and refuses publication

- **WHEN** public SDK Materialize moves its completed staged payload aside and writes a foreign replacement below the same admitted staging directory
- **THEN** it MUST refuse before consuming/deleting replacement bytes/inode/mode/mtime, publish no destination, preserve original retained file and replacement-bearing output membership, and healthy retry MUST really publish source640 payload while preserving prior replacement evidence
