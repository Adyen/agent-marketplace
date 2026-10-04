## ADDED Requirements

### Requirement: Recovery propagates state filesystem admission refusal

Agent Marketplace recovery MUST distinguish actual state metadata read/admission/generation/cleanup refusal from ordinary malformed JSON or genuine missing state. Filesystem refusal MUST preserve original cause identity/public diagnostic and stop before phase decisions or staged/journal cleanup. Genuine optional absence MUST normalize before deferred cleanup; existing missing/malformed-state recovery behavior remains. Original action-generation leases remain separate, with no new budget, recovery transaction or uncooperative-writer exclusion.

#### Scenario: Recovery state refusal preserves prepared generation

- **WHEN** actual Apply commits generation1, prepares generation2 then crashes after staging and its state is replaced by a foreign alias
- **THEN** Recover MUST refuse without changing native/foreign/original state or prepared stage/journal, and restoring original state MUST let genuine rollback remove stage/journal
