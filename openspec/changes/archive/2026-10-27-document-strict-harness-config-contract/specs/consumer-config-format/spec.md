## ADDED Requirements

### Requirement: Consumer configuration requires supported harness target IDs

The running CLI MUST understand every harness target ID named in consumer configuration, including harness selections and per-target materialization settings. Unknown IDs MUST remain configuration errors that gate configuration-dependent actions, not warnings or permission to use only the supported subset. Refusal MUST NOT silently remove or rewrite unknown entries. This validation does not require the corresponding harness application to be installed. Ordinary operational logging and recovery of a pre-existing dependency transaction retain their existing contracts.

#### Scenario: Unknown harness targets gate configuration-dependent actions

- **WHEN** otherwise valid consumer configuration names an unknown harness target ID alongside supported IDs in harness selections or per-target materialization settings
- **THEN** CLI synchronization MUST refuse with a non-zero outcome and the TUI MUST visibly refuse harness editing and synchronization instead of using only the supported subset
- **AND** the refused new operation MUST preserve authored configuration, acquisition locks and existing installed resources without installing pending resources
- **AND** after the user explicitly repairs the configuration and reloads, supported harness editing and synchronization MUST work
