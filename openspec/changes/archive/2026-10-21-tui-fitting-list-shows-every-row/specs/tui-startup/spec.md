## MODIFIED Requirements

### Requirement: Selection and detail anchoring across stages

Accepted stage rebuilds SHALL preserve the user's selection and an open
detail's resource identity as rows are inserted or reordered between stages.

#### Scenario: Verified rows inserted above a selection keep it on the same screen line

- **WHEN** a pending row is selected in a list that overflows the viewport and
  verified inventory inserts rows above it
- **THEN** the selection MUST follow the same resource
- **AND** the selected row MUST keep its line within the list viewport rather
  than shifting on screen

#### Scenario: An untouched cursor stays on top when installed rows arrive

- **WHEN** the user has not moved the cursor and discovery rows, then
  verified inventory rows, then the final stage arrive
- **THEN** the cursor and list offset MUST remain at the top through every
  stage

#### Scenario: An open detail follows its verified row

- **WHEN** a detail pane is open on a verification-pending row and verified
  inventory replaces it
- **THEN** the open detail MUST retain the same resource identity under the
  verified row's key rather than being cleared
