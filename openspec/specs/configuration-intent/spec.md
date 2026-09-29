# configuration-intent Specification

## Purpose
Defines validated reads of authored layered configuration independently of exact remote acquisition, while preserving file provenance and effective selection precedence.

## Requirements

### Requirement: Authored configuration is readable before acquisition

The CLI SHALL parse and validate authored source and resource selections without requiring acquisition locks or remote fetches. Operations requiring exact remote resources MUST still verify acquisition evidence before using those resources.

#### Scenario: Reading authored selections without acquisition

- **WHEN** a configuration contains remote source declarations without available acquisition evidence
- **THEN** its authored selections MUST remain readable without fetching that source

### Requirement: Configuration reads keep their parsed identity

Configuration files and includes SHALL retain the content and source identity captured during validation even if the filesystem changes before binding.

#### Scenario: Captured configuration provenance

- **WHEN** a validated v4 configuration file is removed or its symlink retargeted after its bytes are read
- **THEN** the captured read MUST retain its original bytes and canonical source identity rather than silently adopting changed content

### Requirement: Marketplace aliases identify one instance

Consumer configuration MUST reject a Marketplace alias shared by endpoint registrations with different Marketplace instance IDs, even when their URLs normalize to the same endpoint. An identical instance ID at the same endpoint retains its existing layered alias behavior.

#### Scenario: Same endpoint alias with different instance IDs

- **WHEN** two Marketplace endpoint registrations in distinct layers use the same alias and normalized URL but one declares the instance ID `bootstrap` and the other declares a different instance ID
- **THEN** validation MUST reject their duplicate alias
