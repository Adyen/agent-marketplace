## ADDED Requirements

### Requirement: Lock-pinned refusal offers another scope

The install-from-source modal's lock-pinned remediation SHALL offer choosing
another scope besides the `update` command. Choosing a scope whose lock does
not pin the source behind the preview and confirming SHALL install the picks
into that scope.

#### Scenario: Lock-pinned source installs into another scope

- **WHEN** an install into project scope was refused because the project lock
  pins the source behind the preview, and the user then chooses Local on the
  Review step and confirms
- **THEN** the modal MUST close, local configuration MUST record the picked
  resources, project configuration and acquisition lock bytes MUST be
  unchanged, and a sync MUST leave the list in sync
