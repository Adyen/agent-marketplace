## MODIFIED Requirements

### Requirement: Resource installation presentation
The Marketplace SHALL present compact revision metadata and actionable installation controls for installable resources. The header metadata row SHALL show revision identity and SHALL NOT show the scan date, which remains available in Security details. The revision copy action SHALL copy the full digest, and command copying SHALL exclude the decorative prompt.

#### Scenario: Installable resource exposes compact actions
- **WHEN** a user views an installable resource
- **THEN** revision identity, setup documentation, command, and copy actions are available, and the scan date is available in Security details rather than the header metadata row
- **AND** copy actions use the full revision and command values

## ADDED Requirements

### Requirement: Resource source origin link
When a resource revision reports source provenance, the detail page header SHALL show the source repository and resource path as its first metadata item, linked to the original resource in a new tab. For GitHub, GitLab, and bitbucket.org hosts the link SHALL address the resource file at the exact occurrence commit; for other hosts it SHALL address the repository. When no source provenance is reported, the header SHALL omit the source item.

#### Scenario: Forge-hosted source links to the pinned resource file
- **WHEN** a user views a resource whose source repository is hosted on GitHub, GitLab, or bitbucket.org
- **THEN** the first header metadata item names the repository and path and opens the resource file at the occurrence commit in a new tab

#### Scenario: Other-host source links to the repository
- **WHEN** a user views a resource whose source repository is hosted elsewhere
- **THEN** the source item links to the repository URL without a `.git` suffix and still shows the resource path

#### Scenario: Resource without source provenance omits the origin
- **WHEN** a user views a resource that reports no source provenance
- **THEN** the header shows no source item
