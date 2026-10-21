## ADDED Requirements

### Requirement: Landing scope choices remain comparable
Landing SHALL present user, repository, and personal-local installation scopes
as one informational comparison panel with every scope's CLI value,
configuration path, reach, and version-control outcome visible. The comparison
MUST NOT expose a selection model. Repository scope SHALL be identified as the
default, and Landing SHALL show a valid repository-scope example command.

#### Scenario: Scope comparison identifies the repository default
- **WHEN** a user reaches the Landing scope comparison
- **THEN** all scope outcomes and corresponding CLI values are visible without a scope selector
- **AND** repository scope is identified as the default
- **AND** the example command uses repository scope without performing an installation

### Requirement: Landing harness order leads with its default
Landing SHALL keep Claude Code as the initially selected harness depiction and
place it first in the visible harness tab order. Reordering the depiction MUST
NOT change the supported harness set or capability data.

#### Scenario: Default harness leads the tab order
- **WHEN** a user reaches the Landing harness depiction
- **THEN** Claude Code is the first and selected harness tab
- **AND** every previously listed harness and its capability data remains present
