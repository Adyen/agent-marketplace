# authored-configuration-reads Specification

## Purpose
Defines the configuration-only reads used to guard ad-hoc registry browsing and display authored selection labels without acquiring unrelated remote sources.

## Requirements

### Requirement: Ad-hoc browsing honors authored allowlists

The CLI MUST apply the highest-precedence authored allowlist before fetching an ad-hoc registry, even when the layer supplying that allowlist cannot bind a remote resource.

#### Scenario: Allowlist from unbindable layer

- **WHEN** a higher-precedence layer permits an ad-hoc registry URL but lacks unrelated remote acquisition evidence
- **THEN** `list --from` MUST use that allowlist rather than fail on the unrelated evidence or apply a lower-precedence denial

### Requirement: Informational labels do not acquire remote sources

Prune-stale SHALL obtain authored selection names without fetching unrelated Marketplace resources or requiring their acquisition locks.

#### Scenario: Selection labels with missing acquisition evidence

- **WHEN** prune-stale prints configured selection labels while a Marketplace acquisition lock is absent
- **THEN** it MUST show authored selection names without acquiring that Marketplace resource
