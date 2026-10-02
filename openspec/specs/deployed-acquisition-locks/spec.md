# deployed-acquisition-locks Specification

## Purpose
Defines which deployed acquisition locks can authorize exact remote resources and how an intentionally absent organization lock differs from invalid deployed evidence.

## Requirements

### Requirement: Deployed evidence is validated before use

The CLI MUST reject malformed or stale deployed organization acquisition locks before using their remote resources, while permitting organization deployments without a lock where online acquisition is allowed.

#### Scenario: Absent organization lock

- **WHEN** an organization configuration has no deployed acquisition lock
- **THEN** inspection of deployed locks MUST leave that layer without evidence rather than inventing a deployed lock

#### Scenario: Stale organization lock

- **WHEN** a deployed organization acquisition lock is stale relative to its configuration
- **THEN** the CLI MUST reject that lock rather than silently acquiring replacement authority

#### Scenario: Malformed organization lock

- **WHEN** a deployed organization acquisition lock is malformed
- **THEN** the CLI MUST reject it rather than use or replace its contents

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
