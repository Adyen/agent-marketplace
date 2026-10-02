## ADDED Requirements

### Requirement: Disk primary payload ingestion stays below captured source authority

Agent Marketplace MUST read disk-backed catalog primary payloads and bundle detection payloads through a captured source root. Payloads MUST be regular singly-linked opened snapshots with stable identity/size and captured-size streaming/growth checks, not path-based reopen after an earlier metadata check. Foreign/unsafe aliases and special files MUST refuse before their bytes enter resource parsing; FIFO opens MUST not block. Direct native catalog loading MUST retain valid sibling listing and its coded refusal diagnostics, while publication discovery MUST propagate typed source refusals rather than downgrade them to syntax warnings and prune the installed generation. Both MUST preserve original/external bytes, identity, permissions and modification time. Contained relative primary file aliases MUST remain supported. No new absolute byte/tree/work budget or whole-mutable-tree atomic snapshot is introduced.

#### Scenario: Primary ingestion refuses foreign payload aliases

- **WHEN** a native disk catalog primary resource aliases a payload outside its captured source root
- **THEN** ingestion MUST not import that payload, MUST retain its diagnostic and valid sibling listing, and MUST preserve external data and metadata

#### Scenario: Contained primary file aliases retain authored content

- **WHEN** a primary file aliases regular authored content within the captured source root
- **THEN** native ingestion MUST retain its authored name/description without modifying the source, while a multiply linked payload MUST refuse
