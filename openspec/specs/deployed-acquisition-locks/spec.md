# deployed-acquisition-locks Specification

## Purpose
Defines which deployed acquisition locks can authorize exact remote resources and how an intentionally absent organization lock differs from invalid deployed evidence.

## Requirements

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

### Requirement: Writable layers retain exact acquisition boundaries

An online operation MAY acquire remote evidence for a missing writable acquisition lock, but a read of deployed locks MUST NOT treat missing writable evidence as valid.

#### Scenario: Missing writable acquisition lock

- **WHEN** a read-only deployed-lock inspection requires a missing writable acquisition lock
- **THEN** it MUST report the missing evidence rather than acquire it

#### Scenario: Check reports changes to missing locks

- **WHEN** `lock --check` finds a missing writable Git acquisition lock and a separate v1 lock needing upgrade
- **THEN** it MUST report the missing lock as a required change without writing either lock

#### Scenario: Missing lock preserves independent evidence

- **WHEN** one writable layer lacks a required lock and another layer has verified Marketplace evidence
- **THEN** a strict read MUST report the missing lock while retaining the other layer's bound resources

### Requirement: Writable Marketplace locks extend existing Git evidence

A writable Git-only acquisition lock SHALL gain verified Marketplace evidence when a Marketplace resource is selected, while preserving its exact Git records.

#### Scenario: Git-only lock gains Marketplace evidence

- **WHEN** sync selects a Marketplace resource in a layer with an existing Git-only lock
- **THEN** the reconciled lock MUST retain the Git records and add verified Marketplace records

### Requirement: Transient organization acquisition reuses unchanged inputs

An online consumer MAY reuse verified in-memory organization Marketplace evidence across selection-only configuration edits, but MUST reacquire it when its target harnesses or acquisition allowlists change.

#### Scenario: Selection edit reuses organization evidence

- **WHEN** user or repository selection configuration changes without changing the effective acquisition inputs
- **THEN** another resolution in the same invocation MUST reuse the verified organization Marketplace evidence without a new request

#### Scenario: Harness change reacquires organization evidence

- **WHEN** the effective target harnesses change between two resolutions in the same invocation
- **THEN** the second resolution MUST reacquire organization Marketplace evidence for the new target harnesses

### Requirement: Organization acquisition errors preserve independent diagnostics

An organization acquisition failure SHALL replace the stale organization missing-evidence diagnostic without discarding errors from independent configuration layers.

#### Scenario: Independent layer errors survive organization acquisition failure

- **WHEN** organization acquisition fails and another configuration layer has an independent error
- **THEN** both the actual organization acquisition failure and the independent error SHALL remain available to the consumer

### Requirement: Writable locks record resource-free Marketplace registrations

A v4 source that registers a Marketplace endpoint without selecting resources SHALL be readable without acquisition evidence. A writing `lock` or `sync` for a selected user, repository, or personal-local layer SHALL record that endpoint's well-known-verified instance identity in the layer's acquisition lock, creating the lock when absent or appending only the missing identity record to an existing lock while its other Marketplace and Git records remain exact. Read-only dependency modes MUST NOT fetch the identity: a check MUST report the missing record as a required change, and a locked or offline operation MUST refuse it as stale lock evidence naming the layer scope, lock path, and endpoint. When required configuration and lock validation succeeds, `lock --check` SHALL report every selected writable scope with unrecorded resource-free Marketplace endpoint registrations in layer order, rather than only the first scope. Organization locks MUST NOT be written implicitly.

#### Scenario: Writing commands record a registration identity

- **WHEN** `lock` or `sync` runs for a writable layer whose v4 source registers a Marketplace endpoint without resources and no layer lock exists
- **THEN** the layer lock MUST be created with the endpoint's verified instance identity and no resource records
- **AND** the authored configuration MUST remain unchanged

#### Scenario: Existing lock gains only the missing registration identity

- **WHEN** a writable layer lock already records one Marketplace endpoint and the configuration adds a resource-free registration for another endpoint
- **THEN** `lock` MUST append only the new endpoint's identity record while existing records remain unchanged
- **AND** a repeated `lock` or `sync` MUST NOT rewrite the lock

#### Scenario: Read-only modes refuse an unrecorded registration

- **WHEN** a writable layer lock exists but lacks the identity record for a resource-free registration
- **THEN** `sync --check` and `lock --check` MUST report a required lock change, and `sync --locked` and `sync --offline` MUST refuse with stale lock evidence naming the scope, lock path, and endpoint
- **AND** they MUST NOT contact a Marketplace endpoint or write configuration or locks

#### Scenario: Check reports every selected unrecorded registration scope

- **WHEN** valid user, repository, and personal-local layers register resource-free Marketplace endpoints not recorded in their absent or existing acquisition locks
- **THEN** `lock --check` MUST fail with one `lock.change-required` diagnostic per affected selected scope, in user, repository, and personal-local order, naming each lock path and its unrecorded endpoints
- **AND** an explicit `--scope` MUST omit unselected scopes from these diagnostics
- **AND** the check MUST NOT contact Marketplace endpoints or create or change configuration, locks, caches, or durable state
- **AND** the same check MUST succeed without writes or acquisition when every selected registration is recorded
