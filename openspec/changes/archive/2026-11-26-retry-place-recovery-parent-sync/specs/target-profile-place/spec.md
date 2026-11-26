## ADDED Requirements

### Requirement: Marked file mutations must be durable before recovery advances

During mutating Place recovery, a marked regular-file or ownership publication whose only matching generation is intended and whose stage is absent SHALL sync the verified effective destination parent before completion. A marked regular-file prune with an absent leaf SHALL sync its verified parent before completion. Sync failure MUST leave the operation pending without advancing progress; retry MUST finish without repeating the mutation. Genuine no-ops and read-only inspection are exempt.

#### Scenario: Marked file mutation retries parent sync

- **WHEN** a marked file publication, ownership publication, or regular-file prune has the visible result described above and the verified parent sync fails during recovery
- **THEN** recovery MUST return the sync error with the same pending operation and completed count, and a later successful sync MUST allow recovery to finish without repeating the mutation
