## ADDED Requirements

### Requirement: Durable ownership metadata requires captured read authority

Agent Marketplace ownership state and journal readers MUST stay beneath the canonical selected state-root authority recorded at Store.Open and strict captured ownership/key descriptors. Opened metadata MUST remain regular, singly linked, nonblocking and bounded at captured size with opened/named state validation; unsupported aliases/unsafe shapes MUST refuse before proof or recovery decisions. Genuine optional absence MUST normalize before deferred cleanup; cleanup/unsafe failures MUST NOT become missing state/no journal. Preserve state-corrupt coded diagnostics and filesystem cause identity, zero unmanaged proof on refusal, JSON formats, missing/uninitialized distinction, valid recovery, public selected StateRoot spelling and initial explicitly selected state-root aliases. Publication and recovery cleanup remain separate, with no new byte/work limit, transaction or durability promise.

#### Scenario: Aliased ownership state cannot grant proof

- **WHEN** public Proof encounters valid matching state through an external leaf alias
- **THEN** it MUST return coded corrupt and typed filesystem refusal with zero unmanaged proof while preserving foreign/aggregate bytes; restoring real state MUST grant actual owned proof

#### Scenario: Aliased journal cannot authorize recovery

- **WHEN** public recovery encounters a real prepared-crash journal through an external leaf alias
- **THEN** it MUST refuse with typed filesystem identity and no outcomes while preserving alias/foreign/aggregate bytes; restoring the real journal MUST perform actual rollback
