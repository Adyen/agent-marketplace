# authored-configuration-reads Specification

## Purpose
Defines the configuration-only reads used to guard ad-hoc registry browsing and display authored selection labels without acquiring unrelated remote sources.

## Requirements

### Requirement: Ad-hoc browsing honors authored allowlists

The CLI MUST validate all required consumer roots and include trees under `consumer-config-format` before applying the highest-precedence authored allowlist or fetching an ad-hoc registry. A valid v4 layer supplying that allowlist need not bind an unrelated remote resource; missing acquisition evidence MUST NOT bypass its allowlist or be confused with unsupported consumer input.

#### Scenario: Allowlist from unbindable layer

- **WHEN** a higher-precedence valid v4 layer permits an ad-hoc registry URL but lacks unrelated remote acquisition evidence
- **THEN** `list --from` MUST use that allowlist rather than fail on the unrelated evidence or apply a lower-precedence denial

### Requirement: Informational labels do not acquire remote sources

Prune-stale SHALL obtain authored selection names from roots and include trees validated under `consumer-config-format` without fetching unrelated Marketplace resources or requiring their acquisition locks. Unsupported input MUST retain its format refusal rather than contribute labels or authorize pruning.

#### Scenario: Selection labels with missing acquisition evidence

- **WHEN** prune-stale prints configured v4 selection labels while a Marketplace acquisition lock is absent
- **THEN** it MUST show authored selection names without acquiring that Marketplace resource
