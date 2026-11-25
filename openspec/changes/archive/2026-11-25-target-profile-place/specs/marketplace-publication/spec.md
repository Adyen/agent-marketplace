## ADDED Requirements

### Requirement: Place publication uses an explicit assessed profile

A scoped package SHALL select an exact profile identity explicitly.
Ingestion MUST recompute the payload and mapping digests and profile
binding from admitted input, not trust sidecar digest claims or accept
Marketplace assessment authority from Git or directory sources.
Publication MUST verify agreement between the revision, assessment and
artifact manifest before changing visible metadata.

#### Scenario: Unbound or inconsistent place publication preserves the prior publication

- **WHEN** a scoped package publication lacks an explicit profile selection,
  the exact selected profile is unavailable, or the assessment, revision
  and verified artifact disagree
- **THEN** the server MUST refuse publication and leave the prior visible
  publication unchanged
