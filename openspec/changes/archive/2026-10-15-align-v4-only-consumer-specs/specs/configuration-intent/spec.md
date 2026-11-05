## MODIFIED Requirements

### Requirement: Authored configuration is readable before acquisition

The CLI SHALL parse and validate authored v4 `sources` and source-bound `resources` without requiring acquisition locks or remote fetches. All required roots and include trees MUST first satisfy `consumer-config-format`; acquisition-independent reads MUST NOT accept legacy or versionless roots. Operations requiring exact remote resources MUST still verify acquisition evidence before using those resources.

#### Scenario: Reading authored selections without acquisition

- **WHEN** valid v4 configuration contains remote source declarations without available acquisition evidence
- **THEN** its authored selections MUST remain readable without fetching that source

### Requirement: Marketplace aliases identify one instance

Consumer configuration MUST reject a Marketplace alias shared by endpoint registrations with different bound Marketplace instance IDs, even when their URLs normalize to the same endpoint. Authored v4 Marketplace sources declare endpoint URLs, not instance IDs; exact instance identity comes from verified acquisition evidence. An identical instance ID at the same endpoint retains its existing layered alias behavior.

#### Scenario: Same endpoint alias with different instance IDs

- **WHEN** Marketplace endpoint registrations in distinct layers share an alias and normalized URL but their bound acquisition evidence identifies different Marketplace instances
- **THEN** validation MUST reject their duplicate alias
