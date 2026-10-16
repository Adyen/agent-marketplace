## ADDED Requirements

### Requirement: Portable pruning revalidates admitted entry and parent authority

Agent Marketplace manifest-driven portable pruning MUST retain captured destination parents and revalidate full observed entry state, recorded ownership digest and named parent object before descriptor-relative removal. A replaced entry or retargeted parent MUST retain coded refusal and MUST preserve admitted/replacement/manifest bytes. Stale portable links MUST be revalidated as managed symlink objects without reading their targets, and refused removals MUST NOT be reported as successful. Empty-parent cleanup MUST retain strict captured ancestor authority and use kernel-enforced empty-directory-only removal, never remove a regular file or follow an intermediate link. Valid pruning, dry-run and best-effort empty cleanup MUST remain supported. No new arbitrary budget, transactional prune or uncooperative-writer exclusion is introduced.

#### Scenario: Portable pruning refuses replacement generation

- **WHEN** manifest-authorized native portable pruning encounters a replaced output after ownership classification
- **THEN** materialization MUST report refusal without removing replacement/admitted bytes or changing manifest bytes, while healthy pruning remains functional
