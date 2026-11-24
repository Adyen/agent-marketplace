## ADDED Requirements

### Requirement: Supplied Marketplace deployments run SkillSpector in isolation

Supplied Marketplace Compose and Helm deployments MUST enable an isolated, non-root SkillSpector runner by default for indexing skills. It MUST not have outbound network access, Marketplace credentials, repository mounts, or a container runtime socket. It MUST analyze only bounded skill input without executing skill code or contacting an LLM provider.

#### Scenario: Default deployment scans with an isolated runner

- **GIVEN** a supplied deployment is configured with its required image references
- **WHEN** the default services are deployed
- **THEN** Marketplace indexing MUST be able to submit one skill bundle to the isolated SkillSpector runner
- **AND** the runner MUST have no public endpoint and MUST prevent outbound connections

#### Scenario: Consumer and standalone server do not require the runner

- **WHEN** a consumer installs an index-only skill or a standalone Marketplace server indexes a skill without a runner configured
- **THEN** the operation MUST NOT invoke or require SkillSpector
- **AND** standalone publication MUST retain the existing built-in outcome
