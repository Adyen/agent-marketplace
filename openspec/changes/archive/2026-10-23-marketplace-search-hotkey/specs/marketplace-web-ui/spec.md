## ADDED Requirements

### Requirement: Catalog search keyboard shortcut

The Marketplace catalog SHALL focus its search input when the user presses `/` while focus is not in a text field, without inserting `/` into the query. The search input SHALL show a visible `/` keyboard hint and SHALL expose the shortcut to assistive technology. Pressing `/` while a text field, including the search input itself, has focus SHALL type the character normally.

#### Scenario: Slash focuses catalog search

- **WHEN** a user presses `/` on the Marketplace catalog while focus is outside any text field
- **THEN** the search input receives focus and its query remains unchanged
- **AND** the search input shows a `/` keyboard hint and declares `/` as its keyboard shortcut
- **AND** pressing `/` while the search input has focus types `/` into the query
