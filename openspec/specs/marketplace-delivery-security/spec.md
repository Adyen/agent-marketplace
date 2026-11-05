# marketplace-delivery-security Specification

## Purpose
Defines fail-closed secret scanning, explicit deployment image versions, and an independently selectable shell-free Marketplace web runtime.

## Requirements

### Requirement: Secret scanning fails directly

The secret-scanning workflow SHALL run its pinned TruffleHog action for pull
requests and merge groups without tolerating scanner failure at either step or
job scope. Scanner failure SHALL fail the job directly, without depending on a
later status-repair step. Requiring the resulting status for merges remains a
repository branch-protection responsibility.

#### Scenario: Secret scanner failure fails the job

- **WHEN** the configured TruffleHog action fails
- **THEN** its step and job MUST NOT tolerate that failure
- **AND** the scanner MUST NOT depend on a conditional follow-up step to fail
  the job

### Requirement: Helm image versions are explicit

The supplied Helm chart MUST NOT default an image to `latest` or infer a
version from an omitted tag. Every enabled workload SHALL require an explicit
image tag or digest after existing role inheritance is applied. A digest SHALL
take precedence over a tag. Web and documentation images SHALL have independent
versions, not silently inherit the Marketplace service's version.

#### Scenario: Helm refuses an unspecified image version

- **WHEN** the chart is rendered with an enabled workload lacking both a tag
  and digest
- **THEN** rendering MUST refuse and request an explicit tag or digest
- **AND** rendering with an explicit development tag MUST remain supported

#### Scenario: Helm digests take precedence for each enabled workload

- **WHEN** a Marketplace operator renders split API and worker roles and
  enabled frontends using explicit digests
- **THEN** each frontend MUST use its own digest
- **AND** the API and worker MUST retain role overrides and top-level
  inheritance
- **AND** supplying a tag alongside a digest MUST NOT change the selected
  digest

### Requirement: Web runtime is shell-free and independently selectable

The supplied web image SHALL build Node output on Debian and run it with
distroless Node 24 by default, independently of the builder image. Startup and
healthchecks SHALL invoke Node directly in exec form without a shell. The
runtime SHALL retain its non-root user. Make and both Compose profiles SHALL
propagate a separate generic runtime-image override for internal mirrors.
Production operator guidance SHALL require verified digest pins for deployed
images and every base-image override, including mirrors; tagged defaults and
explicit Helm tags remain development conveniences rather than immutable pins.

#### Scenario: Web runtime is independent of the builder

- **WHEN** the supplied web image is built with its defaults
- **THEN** its final stage MUST use distroless Node rather than the builder
- **AND** startup and healthchecks MUST invoke Node without a shell
- **AND** the final stage MUST run as a non-root user

### Requirement: Web healthcheck requests are bounded

The supplied web image's exec-form healthcheck SHALL configure Node fetch with
a four-second request deadline, before Docker's five-second process timeout.
Failed and aborted requests SHALL exit nonzero without requiring a shell.

#### Scenario: Web healthcheck has an explicit request deadline

- **WHEN** the supplied web image's healthcheck is configured
- **THEN** its native Node fetch MUST use a four-second AbortSignal deadline
- **AND** failed or aborted requests MUST produce a nonzero exit

### Requirement: Nested collection preparation is bounded

Marketplace `install` and `update` preparation MUST fetch, cache and traverse each content-addressed nested collection at most once per preparation. Later include paths MUST reuse its verified result. Cycle detection, revocation enforcement and every per-collection check MUST still apply on its first verification. Preparation MUST apply the lock-bootstrap work limit of 13,981 units and MUST refuse with the resource-limit error before fetching further manifests once traversal exceeds it.

#### Scenario: Collection preparation fetches each nested collection once

- **WHEN** two nested collections in one collection both include the same nested collection
- **THEN** preparation MUST request that shared collection's revision and manifest once each and MUST still verify all three nested collections

#### Scenario: Collection preparation stays within the lock bootstrap limit

- **WHEN** a collection's nested traversal exceeds 13,981 work units
- **THEN** `PrepareLocator` MUST refuse with the resource-limit error
