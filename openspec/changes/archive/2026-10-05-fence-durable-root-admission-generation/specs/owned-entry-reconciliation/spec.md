## ADDED Requirements

### Requirement: Durable aggregate reads retain existing root admission identity
Agent Marketplace ownership Store admission MUST retain the object identity of an existing canonical target root without filesystem mutation. Before importing durable aggregate payload bytes, its shared reader MUST compare the captured root with that admitted identity and refuse ordinary directory replacement or disappearance with a generation conflict. Directory metadata changes within the same root object MUST remain valid. A genuinely absent root at admission MUST retain optional absence and legitimate later-creation semantics. This authority is invocation-local, adds no persisted inode ownership or processing budget, and does not exclude uncooperative writers after verification.

#### Scenario: Existing admitted root replaced with an ordinary directory
- **GIVEN** an ownership Store admitted an existing canonical target root
- **WHEN** that root is replaced with another ordinary directory containing a copied aggregate before a durable payload read
- **THEN** the reader MUST refuse with generation-conflict identity before returning payload bytes and preserve both directory generations

#### Scenario: Missing admitted root may be created legitimately
- **GIVEN** the canonical target root is genuinely absent at Store admission
- **WHEN** a legitimate directory and regular aggregate are later created below the authorized path
- **THEN** the reader MUST return the actual aggregate bytes, while the initial optional read MUST report absence without creating anything
