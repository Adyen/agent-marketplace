## ADDED Requirements

### Requirement: Recoverable source addition failure

Adding a directory registry source whose path does not exist SHALL show an error inside the Add source section, SHALL NOT write configuration, and SHALL leave the workspace usable.

#### Scenario: Nonexistent directory source is refused in the form

- **WHEN** a user adds a directory registry source whose path does not exist
- **THEN** an error MUST show inside the Add source section, the user-personal root MUST remain unchanged, and resource rows, the toolbar, and Settings MUST still respond
