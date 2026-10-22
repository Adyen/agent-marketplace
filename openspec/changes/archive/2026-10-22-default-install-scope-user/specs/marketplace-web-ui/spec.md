## REMOVED Requirements

### Requirement: Landing scope choices remain comparable

**Reason**: The install default moved from repository to user scope.

**Migration**: Replaced by "Landing scope choices identify the user default".

## ADDED Requirements

### Requirement: Landing scope choices identify the user default
Landing SHALL present user, repository, and personal-local installation scopes
as one informational comparison panel with every scope's CLI value,
configuration path, reach, and version-control outcome visible. The comparison
MUST NOT expose a selection model. User scope SHALL be identified as the
default, and Landing SHALL show a valid user-scope example command.

#### Scenario: Scope comparison identifies the user default
- **WHEN** a user reaches the Landing scope comparison
- **THEN** all scope outcomes and corresponding CLI values are visible without a scope selector
- **AND** user scope is identified as the default
- **AND** the example command uses user scope without performing an installation
