## MODIFIED Requirements

### Requirement: Source form input validity

The shared source form SHALL disable its submit button until a directory location is non-blank, a Git location is accepted by the existing GUI add-source API/config location admission rules, or a Marketplace location is an absolute HTTP(S) URL. A Git ref is optional. Client checks SHALL NOT reject locations accepted by that admission boundary, and SHALL NOT loosen existing server safety rules. Directory existence SHALL remain a server check. Field hints SHALL appear only for a non-blank invalid value, after blur or an attempted submit; a blank field SHALL NOT show a hint.

#### Scenario: Invalid source input disables addition

- **WHEN** a directory location is blank or whitespace, a Git location is rejected by the existing GUI add-source API/config location admission rules, or a Marketplace location is not an absolute HTTP(S) URL
- **THEN** the submit button MUST remain disabled
- **AND** an untouched field MUST NOT show a validation hint while its first characters are typed
- **AND** a non-blank invalid field MUST show its hint after blur or an attempted submit
- **AND** a blank field MUST NOT show a hint after blur

#### Scenario: Valid source input enables addition

- **WHEN** the user supplies valid input for a source kind and the form is otherwise editable
- **THEN** the submit button MUST be enabled
- **AND** a Git source with a blank ref MUST be accepted
- **AND** the client MUST accept Git locations accepted by the existing GUI add-source API/config location admission rules
- **AND** directory existence MUST NOT be checked by the client

## ADDED Requirements

### Requirement: Git source default branch

The GUI add-source action SHALL accept a Git source without a ref. The server SHALL resolve the repository's default branch and save it as the source's ref, as `install --from` does.

#### Scenario: Git source without a ref uses the default branch

- **WHEN** a Git source is added without a ref
- **THEN** the user-personal root MUST record the source with the repository's default branch as its ref

### Requirement: Source preview

The local API SHALL preview a Git or directory source that is not configured yet. The preview SHALL apply the add-source admission rules, report the ref and commit that were read and the resources found, and SHALL NOT write configuration, locks, or materialized files. A source that cannot be loaded SHALL be reported as a failure in the preview result. Marketplace sources SHALL NOT be previewed.

#### Scenario: Source preview lists resources without writing

- **WHEN** a Git source is previewed without a ref
- **THEN** the result MUST name the default branch, the commit it resolved to, and each resource found
- **AND** previewing a ref that does not exist MUST report a failure
- **AND** previewing a Marketplace source MUST be refused
- **AND** the user-personal root MUST remain unchanged

### Requirement: Installing resources while adding a source

The GUI add-source action SHALL accept an optional list of resources, each a kind and a name, to install from a Git or directory source as it is added, the way the terminal UI's install-from-source does. With no resources the source is added alone. A commit given with the list SHALL be the commit the install expects the ref to resolve to. The list SHALL be refused for a Marketplace source, for any other action, and when an entry has an unknown kind or a blank name.

#### Scenario: Adding a source installs the picked resources

- **WHEN** a Git source is added with one of its skills listed to install
- **THEN** the user-personal root MUST record the source and select that skill from it
- **AND** a list with an unknown kind or a blank name, a list for a Marketplace source, or a list on another action MUST be refused
