## ADDED Requirements

### Requirement: Marketplace aliases identify one instance

Consumer configuration MUST reject a Marketplace alias shared by endpoint registrations with different Marketplace instance IDs, even when their URLs normalize to the same endpoint. An identical instance ID at the same endpoint retains its existing layered alias behavior.

#### Scenario: Same endpoint alias with different instance IDs

- **WHEN** two Marketplace endpoint registrations in distinct layers use the same alias and normalized URL but one declares the instance ID `bootstrap` and the other declares a different instance ID
- **THEN** validation MUST reject their duplicate alias
