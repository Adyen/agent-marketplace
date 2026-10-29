## ADDED Requirements

### Requirement: Install-from-source step navigation

In the install-from-source modal, `→` SHALL act as `enter` on the Pick step
and `←` SHALL return from Pick to Source and from Review to Pick. `→` on the
Review step SHALL NOT install. On the Source step `←` and `→` SHALL keep
moving the text cursor, and while the Pick filter is being edited they SHALL
edit the filter. Confirming the Source step with the same URL, ref, and path
as the shown preview SHALL return to Pick with that preview and the current
picks without previewing the source again; any changed field SHALL preview
again. On the Review step `↑`/`k` and `↓`/`j` SHALL move the scope selection
through User, Project, and Local in that order without wrapping; moving onto
a scope the source cannot be written to SHALL keep the current scope and show
the reason.

#### Scenario: Arrow keys walk the steps without installing

- **WHEN** a user on the Pick step with a picked resource presses `→`, then
  presses `→` again on Review, then presses `←` twice
- **THEN** the modal MUST show Review after the first `→`, MUST stay on Review
  without installing after the second, and MUST show Pick and then Source,
  and configuration, acquisition lock, and harness bytes MUST be unchanged

#### Scenario: Unchanged source reuses the preview

- **WHEN** a user returns from Pick to Source and confirms without changing
  the URL, ref, or path, after the source gained a resource
- **THEN** the Pick step MUST show the previous preview without the new
  resource and MUST keep the picks

#### Scenario: Changed source previews again

- **WHEN** a user returns from Pick to Source, changes the ref or path, and
  confirms
- **THEN** the source MUST be previewed again with the changed values

#### Scenario: Arrow keys choose the Review scope

- **WHEN** a user on the Review step presses `↓` and confirms
- **THEN** the selection MUST move from Project to Local and the install MUST
  write the local configuration

#### Scenario: Arrow keys skip an unwritable scope

- **WHEN** a user on the Review step presses `↑` from Project and the source
  cannot be written to user configuration
- **THEN** Project MUST stay selected and the reason MUST be shown
