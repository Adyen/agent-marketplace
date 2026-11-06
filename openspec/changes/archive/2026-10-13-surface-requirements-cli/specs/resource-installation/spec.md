## ADDED Requirements

### Requirement: Install reports associated implied prerequisites

After its existing summary, install SHALL report transitive implied resources reachable from the requested kind and name identities or expanded collection members as `also selected` with their kind, name and `required by <kind> "<name>"` provenance. When expansion is disabled, it SHALL instead report eligible transitive preview resources as `would also select` without selecting or writing those resources. Repeated implied identities SHALL be reported once. Unrelated selections and previews SHALL NOT appear in this summary. A direct selection with requirement provenance SHALL NOT be reported as implied. A prerequisite without an eligible candidate SHALL retain existing unmet warnings and SHALL NOT receive a selection summary. Existing install summaries, collection handling and unmet-warning filtering SHALL remain intact.

#### Scenario: Enabled install reports transitive implied prerequisites

- **WHEN** an install selects a resource whose eligible prerequisites form a transitive chain with expansion enabled
- **THEN** the existing install summary MUST be followed by each associated implied resource and its requiring resource
- **AND** configuration MUST contain only the requested selections, not implied selections

#### Scenario: Disabled install reports a transitive preview

- **WHEN** an install selects a resource with an eligible transitive prerequisite chain and expansion is disabled
- **THEN** install MUST report `would also select` for associated preview resources after its existing summary
- **AND** the preview resources MUST NOT be selected, configured or newly materialized

#### Scenario: Install summary omits direct and unrelated resources

- **WHEN** requested resources or collection members share prerequisites, a prerequisite is directly selected, and unrelated selected resources have their own prerequisites
- **THEN** install MUST report each reachable implied resource once and MUST NOT call the direct prerequisite implied
- **AND** unrelated prerequisite summaries and unmet warnings MUST be omitted

#### Scenario: Unavailable install prerequisite keeps its warning

- **WHEN** a requested resource declares an excluded, missing or unconfigured-source prerequisite
- **THEN** install MUST retain the existing remediable unmet warning and successful ordinary exit status
- **AND** it MUST NOT claim that unavailable prerequisite was selected or previewed
