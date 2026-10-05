## ADDED Requirements

### Requirement: Single-view provisional row continuity

The single status-grouped view SHALL preserve provisional-to-verified row layout, scope letters, registry-qualified names, and applicable harness override badges. Unverified or required rows SHALL retain their mutation guards. Supported config-writing toggles SHALL continue to use the selected registry and scope and authoritative reload, and synchronization SHALL retain its existing confirmation and safety behavior.

#### Scenario: Provisional rows retain single-view position and guarded actions

- **WHEN** provisional configured rows become verified inventory in the single view, an unverified or required row is offered a toggle, and an eligible row is toggled and synchronized
- **THEN** provisional and verified section ordering and labels agree, harness override badges remain visible, guarded rows refuse mutation, and the eligible action writes only its chosen registry and scope before authoritative reload and existing synchronization handling
