## ADDED Requirements

### Requirement: Consistent resource opening

For Marketplace, directory, and Git skill rows, Enter and the Open resource button SHALL both open the inspector when it can show the row's content, and neither SHALL be offered when it cannot.

#### Scenario: Enter and Open resource agree across source kinds

- **WHEN** a user selects a Marketplace, directory, or Git skill row and presses Enter or activates Open resource
- **THEN** both MUST open the inspector when it can show that row's content, and neither MUST be offered when it cannot

### Requirement: Search-independent discovery health

Diagnostics SHALL show Marketplace discovery health that does not depend on the resource search query.

#### Scenario: Discovery health ignores the resource search

- **WHEN** a user views Diagnostics with an empty resource search and again with a non-empty resource search
- **THEN** the Marketplace discovery health shown MUST be identical
