## ADDED Requirements

### Requirement: Ownership lock mutation retains selected namespace authority

Agent Marketplace MUST record the canonical selected lock namespace at Store.Open without changing read-only failure timing, preserve initial selected state/shared-directory aliases, and prepare/open persistent lock leaves under strict captured directory authority. Revalidate selected root/named opened identity before admission, after contention and before metadata writes. Retargeted directories/replaced leaves MUST NOT authorize foreign writes or stale sessions. New directories MUST be private; existing modes and kernel timeout/cancel/reentrant/fallback/exclusion semantics remain unchanged. A concurrent creation refusal MAY use one noncreating descriptor-relative open of the existing leaf but MUST NOT delete/recreate lock inodes. Late diagnostic failure stays best effort. No new budget, overall transaction or uncooperative-writer exclusion.

#### Scenario: Lock directory retarget preserves foreign metadata

- **WHEN** a public Store's admitted lock directory is retargeted to an inert foreign directory before Lock
- **THEN** it MUST refuse without changing foreign diagnostic/original namespace and restoring the original MUST allow real kernel acquisition/diagnostic publication/release
