## MODIFIED Requirements

### Requirement: Repair refuses incomplete organization selection

When organization resource acquisition fails, repair SHALL report that failure and block every selection-dependent repair step rather than interpret unavailable resources as no longer desired. Repair steps that do not depend on the selection SHALL remain runnable.

#### Scenario: Organization acquisition failure refuses repair

- **WHEN** the organization Marketplace cannot resolve its configured resources and no organization acquisition lock is deployed
- **THEN** repair SHALL report the acquisition failure, list every selection-dependent step as blocked by it, exit nonzero, and leave shared aggregate entries unchanged
