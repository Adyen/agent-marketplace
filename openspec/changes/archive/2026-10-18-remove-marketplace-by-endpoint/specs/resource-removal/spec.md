## ADDED Requirements

### Requirement: Marketplace selections are removable by endpoint

Named removal with `--from` MUST accept the URL of a configured Marketplace
endpoint and remove only that endpoint's matching direct pin or collection,
updating configuration and the acquisition lock together. Without `--scope`,
it MUST target repository scope like other `--from` removals; an explicit scope
MUST edit only that layer. The ambiguity remediation for a Marketplace
selection MUST name its endpoint with `--from` and MUST NOT include `--ref` or
`--path`. Git and directory `--from` removal MUST remain unchanged.

#### Scenario: Remove a Marketplace pin by its endpoint

- **GIVEN** a direct Marketplace pin and another explicit selection share a kind and name in one scope
- **WHEN** removal refuses for source ambiguity and the user runs the remediation suggested for the pin
- **THEN** only that pin MUST be removed from configuration and the acquisition lock
- **AND** the other selection MUST remain, whether or not its resource is discoverable
- **AND** endpoint removal without `--scope` MUST NOT edit another layer

#### Scenario: Refuse ambiguous or unsupported Marketplace endpoint removal

- **WHEN** `--from` names a configured Marketplace endpoint together with `--ref` or `--path`, names no matching direct selection of the requested kind, or also matches a configured Git or directory source
- **THEN** removal MUST refuse without changing configuration or acquisition locks
