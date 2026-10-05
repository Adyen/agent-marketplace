## ADDED Requirements

### Requirement: SDK stale discovery retains original directory generations

SDK stale traversal MUST remain below captured original output and directory authority, match opened directories to original admitted metadata and validate original ancestry before/after importing descriptor-read members. A directory replacement MUST refuse generation conflict before importing its stale claims, including dry-run. Candidates MUST retain original no-follow leaf metadata/parent identities, preserve sorted paths/transaction exclusions/special-shape nondestruction and genuine absence behavior. Refusal MUST preserve replacement/original bytes/metadata/membership and create no staging. Valid restored directory discovery and actual pruning MUST remain functional. This requirement does not by itself fence subsequent deletion/reporting/empty-parent cleanup.

#### Scenario: SDK stale discovery refuses directory replacement before importing members

- **WHEN** public SDK sync or dry-run replaces an admitted nested real directory before member discovery
- **THEN** it MUST generation-refuse before importing foreign member claims, preserve foreign bytes/inode/mode/mtime/output membership and original retained payload, create no stage, and restoring original directory MUST permit actual regular prune or nonmutating dry-run candidate discovery
