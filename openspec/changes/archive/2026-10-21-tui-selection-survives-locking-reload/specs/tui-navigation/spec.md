## ADDED Requirements

### Requirement: Reload selection follows the resource

When an accepted reload changes the catalog that loads a selected resource,
such as a write that creates the missing acquisition lock or an update that
moves the locked commit, the TUI SHALL restore the list selection and an open
detail view to that resource by its kind, name, and source URL, ref, and path.
When that identity matches more than one row, the TUI SHALL NOT guess between
them and SHALL keep the existing position-based selection.

#### Scenario: Selection survives a reload that locks its source

- **WHEN** a user enables a resource from a Git source that has no acquisition
  lock yet, and the write creates the lock so the reload loads that source from
  its locked snapshot
- **THEN** after the authoritative reload the enabled resource MUST remain
  selected in the list, and an open detail view MUST stay on it
- **AND** when another row shares that resource identity, the reload MUST NOT
  move the selection to either of them on that basis
