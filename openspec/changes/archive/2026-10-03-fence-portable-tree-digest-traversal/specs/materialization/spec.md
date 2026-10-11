## ADDED Requirements

### Requirement: Portable tree digest traversal retains captured directory authority

Agent Marketplace portable ownership tree digests MUST enumerate and descend through captured directories, not independently reopened directory pathnames. Observed/captured directory identity and state MUST remain validated through enumeration and descendant reads, and parent-relative entry generations MUST be rechecked before accepting a digest. A replaced or retargeted directory MUST retain typed refusal and MUST NOT import foreign payload bytes. Legacy lexical ordering, relative paths, modes, regular bytes and structural symlink text MUST remain compatible; structural symlinks MUST NOT be followed for payload hashing. No new absolute processing budget, whole mutable-tree atomic snapshot or publication/removal transaction is introduced.

#### Scenario: Portable tree digest refuses replaced directory authority

- **WHEN** a directory classified for portable ownership hashing is replaced or retargeted before enumeration
- **THEN** digesting MUST retain a filesystem refusal without accepting foreign bytes or changing external generations, while valid ordered and structural-link digest controls remain compatible
