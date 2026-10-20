## ADDED Requirements

### Requirement: Landing scope choices remain comparable
Landing SHALL present user, repository, and personal-local installation scopes
as one native radio group with every scope's configuration path, reach, and
version-control outcome visible for comparison. Repository scope SHALL remain
selected by default. Selecting another scope SHALL update the example install
command to the corresponding `--scope` value without changing any real
configuration.

#### Scenario: Scope selection updates the example command
- **WHEN** a user selects user, repository, or personal-local scope on Landing
- **THEN** exactly one native radio choice is selected
- **AND** all scope outcomes remain visible
- **AND** the example command shows the selected scope without performing an installation

### Requirement: Landing harness order leads with its default
Landing SHALL keep Claude Code as the initially selected harness depiction and
place it first in the visible harness tab order. Reordering the depiction MUST
NOT change the supported harness set or capability data.

#### Scenario: Default harness leads the tab order
- **WHEN** a user reaches the Landing harness depiction
- **THEN** Claude Code is the first and selected harness tab
- **AND** every previously listed harness and its capability data remains present
