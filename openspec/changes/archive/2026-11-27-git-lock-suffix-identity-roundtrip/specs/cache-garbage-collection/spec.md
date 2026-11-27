## ADDED Requirements

### Requirement: Repeated-suffix lock identities preserve exact cache reachability

Cache garbage collection MUST derive Git object and snapshot reachability from the normalized acquisition-lock identity and MUST NOT strip another terminal `.git` from a canonical URL whose path still ends in `.git`.

#### Scenario: Repeated-suffix lock retains its exact Git cache identity

- **WHEN** a v1 or v2 lock records a repeated-suffix Git origin and exact Git objects or snapshots exist for its normalized identity
- **THEN** collection MUST retain those reachable objects and snapshots without treating the shortened repository identity as equivalent
