## ADDED Requirements

### Requirement: Status keeps the configured harness set when inspection fails

When the configuration loads successfully but acquisition-lock or Marketplace
inspection fails, `status` SHALL still show the effective harness set and its
source in TARGET HARNESS SET and SHALL NOT suggest adding harnesses when any
loaded layer declares them. The inspection error and exit status SHALL be
unchanged.

#### Scenario: Lock-required error keeps the declared harnesses

- **WHEN** the user configuration declares `harnesses: [factory]`, a repository configuration selects a Marketplace resource, no repository acquisition lock exists, and `status` runs
- **THEN** `status` MUST still exit nonzero with `v4.marketplace-lock-required`
- **AND** TARGET HARNESS SET MUST show `factory` with source `USER`
- **AND** the output MUST NOT contain `add harnesses to ~/.agents/config.yaml`
