## MODIFIED Requirements

### Requirement: Malformed prerequisite diagnostics

The marketplace SHALL skip malformed prerequisite declarations without failing
to load the declaring resource. A malformed item SHALL NOT prevent remaining
valid items from being read or cause unrelated resources in a covered
sub-bundle to be reported as undiscovered. `list --from` and `doctor` SHALL
print a warning with the stable code `catalog.requires.invalid`, the declaring
resource and its source file, and the item index when the malformed value is a
list item. The warning MUST NOT include the malformed value or credentials from
it.

#### Scenario: Non-list requires value

- **WHEN** `metadata.requires` is a string or map instead of a list
- **THEN** the resource MUST load with no prerequisites
- **AND** `list --from` and `doctor` MUST report `catalog.requires.invalid` with its resource and source file

#### Scenario: Invalid item among valid items

- **WHEN** a list contains a valid item and an item with an unknown kind, kind `collection` or `all`, a missing or blank name, a non-string `kind`, `name`, `registry`, or `path`, or a value that is neither a string nor a map
- **THEN** the valid item MUST be retained and the invalid item MUST be dropped
- **AND** `list --from` and `doctor` MUST report `catalog.requires.invalid` for the invalid item's index without including the input value

#### Scenario: Malformed item stays silent

- **WHEN** a selected resource declares only malformed prerequisite items and ordinary sync runs
- **THEN** sync MUST materialize the resource and MUST NOT print an unmet-prerequisite warning

#### Scenario: Covered sub-bundle with malformed prerequisite

- **WHEN** a discovered resource in a covered organizational sub-bundle declares a malformed prerequisite
- **THEN** the marketplace MUST retain the resource and report `catalog.requires.invalid`
- **AND** it MUST NOT report that covered sub-bundle as ignored
