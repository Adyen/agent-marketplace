## MODIFIED Requirements

### Requirement: Deployed evidence is validated before use

The CLI MUST reject malformed, wrong-scope, or stale deployed organization acquisition locks before using their remote resources, while permitting organization deployments without a lock where online acquisition is allowed. Single-file configuration reads MUST reject acquisition locks declaring a different layer scope.

#### Scenario: Absent organization lock

- **WHEN** an organization configuration has no deployed acquisition lock
- **THEN** inspection of deployed locks MUST leave that layer without evidence rather than inventing a deployed lock

#### Scenario: Stale organization lock

- **WHEN** a deployed organization acquisition lock is stale relative to its configuration
- **THEN** the CLI MUST reject that lock rather than silently acquiring replacement authority

#### Scenario: Malformed organization lock

- **WHEN** a deployed organization acquisition lock is malformed
- **THEN** the CLI MUST reject it rather than use or replace its contents

#### Scenario: Wrong scope in single-file read

- **WHEN** a single-file loader reads an otherwise valid acquisition lock declaring a different layer scope
- **THEN** it MUST reject the lock without binding its remote evidence
