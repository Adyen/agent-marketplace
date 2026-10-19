# configuration-intent Specification

## Purpose
Defines validated reads of authored layered configuration independently of exact remote acquisition, while preserving file provenance and effective selection precedence.

## Requirements

### Requirement: Authored configuration is readable before acquisition

The CLI SHALL parse and validate authored v4 `sources` and source-bound `resources` without requiring acquisition locks or remote fetches. All required roots and include trees MUST first satisfy `consumer-config-format`; acquisition-independent reads MUST NOT accept legacy or versionless roots. Operations requiring exact remote resources MUST still verify acquisition evidence before using those resources.

#### Scenario: Reading authored selections without acquisition

- **WHEN** valid v4 configuration contains remote source declarations without available acquisition evidence
- **THEN** its authored selections MUST remain readable without fetching that source

### Requirement: Configuration reads keep their parsed identity

Configuration files and includes SHALL retain the content and source identity captured during validation even if the filesystem changes before binding.

#### Scenario: Captured configuration provenance

- **WHEN** a validated v4 configuration file is removed or its symlink retargeted after its bytes are read
- **THEN** the captured read MUST retain its original bytes and canonical source identity rather than silently adopting changed content

### Requirement: Marketplace aliases identify one instance

Consumer configuration MUST reject a Marketplace alias shared by endpoint registrations with different bound Marketplace instance IDs, even when their URLs normalize to the same endpoint. Authored v4 Marketplace sources declare endpoint URLs, not instance IDs; exact instance identity comes from verified acquisition evidence. An identical instance ID at the same endpoint retains its existing layered alias behavior.

#### Scenario: Same endpoint alias with different instance IDs

- **WHEN** Marketplace endpoint registrations in distinct layers share an alias and normalized URL but their bound acquisition evidence identifies different Marketplace instances
- **THEN** validation MUST reject their duplicate alias
