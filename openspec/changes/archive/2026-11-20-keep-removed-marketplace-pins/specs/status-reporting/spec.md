## ADDED Requirements

### Requirement: Status notes Marketplace resources removed upstream

When `status` queries a Marketplace endpoint and a selected pin's exact revision
lookup succeeds but its latest-revision lookup returns HTTP 404, `status` SHALL
note the removal on the pin and report a warning with the remedy. The note
SHALL NOT change the pin's STATE, its update availability, or the exit status of
`status`. Cached-only inspection, which makes no endpoint request, SHALL NOT add
the note.

#### Scenario: Removed pin keeps its state and gains a note

- **WHEN** a synced Marketplace pin's resource is removed upstream so that its latest-revision lookup returns 404 while its exact revision is still served, and `status` runs online
- **THEN** the pin's STATE MUST stay `current` and its DETAIL MUST contain `removed from the Marketplace; pinned revision kept`
- **AND** `status` MUST report a warning naming the pin's resource coordinate and the `agent-marketplace remove <installed name>` remedy
- **AND** `status` MUST exit 0 and leave configuration and lock files byte-identical

#### Scenario: Required removed pin names its required entry in status

- **WHEN** the removed pin is required
- **THEN** the warning MUST name the configuration entry that marks it required and MUST NOT suggest `agent-marketplace remove`

#### Scenario: Cached-only status adds no removal note

- **WHEN** the same pin is inspected in a mode that makes no Marketplace request
- **THEN** its DETAIL MUST NOT contain the removal note
