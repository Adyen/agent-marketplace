## ADDED Requirements

### Requirement: Ordinary retention deletes only admitted archive generations

Agent Marketplace ordinary archive retention MUST enumerate target/ordinary archive names beneath captured directories, retain the existing two-axis oldest-first policy and preflight selected removals before mutation. Selected archives MUST be regular, singly linked and nonblocking through opened-file validation; unsafe shapes MUST refuse. Each removal MUST revalidate the observed full archive state and named parent before descriptor-relative unlink. Member descriptors MUST close per inspection rather than accumulate across selected archives. Valid regular retention and genuine absent backup-root behavior MUST remain supported. Codex set retention remains separate; no new budget, directory fsync, batch transaction or uncooperative-writer exclusion is introduced.

#### Scenario: Ordinary retention refuses hardlinked archive

- **WHEN** public retention selects a multiply linked old ordinary archive
- **THEN** it MUST refuse with typed unsafe-file identity and preserve old/newest/foreign bytes, while removal of the extra link MUST permit real deletion of the old archive and preserve the newest archive
