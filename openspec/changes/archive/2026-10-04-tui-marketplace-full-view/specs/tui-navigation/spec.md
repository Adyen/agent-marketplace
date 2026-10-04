## ADDED Requirements

### Requirement: Complete installed Marketplace skill browsing

The TUI SHALL show and initially preview `SKILL.md` alongside verified supporting files when opening an installed Marketplace skill, including skills whose supporting files are staged separately from their canonical definition. Missing or invalid verified content SHALL produce a visible error without exposing cache implementation files.

#### Scenario: Installed skill definition and supporting files remain browsable

- **WHEN** a user opens an installed Marketplace skill whose verified supporting files use a staged cache directory, or whose verified content is missing or invalid
- **THEN** valid content MUST show and initially preview `SKILL.md` with the supporting files still browsable, while missing or invalid content MUST fail visibly without exposing cache implementation files

### Requirement: Read-only exact Marketplace skill inspection

Opening an uninstalled Marketplace skill SHALL asynchronously load its exact resource revision's verified inspection projection and primary file. Browsing SHALL NOT install the skill, edit configuration or acquisition locks, or synchronize materialized files. Unavailable, corrupt, or unsupported inspection content SHALL produce an actionable state. A superseded inspection response SHALL NOT replace another resource's detail.

#### Scenario: Inspect an exact skill before installation without writes

- **WHEN** a user opens an uninstalled Marketplace skill and browses its verified inspection files, or inspection content is unavailable, corrupt, unsupported, or superseded by another resource
- **THEN** available inspection content MUST show the exact resource revision's tree and primary file without installation, configuration, acquisition lock, or materialization writes, failures MUST be actionable, and superseded responses MUST NOT replace the current resource
