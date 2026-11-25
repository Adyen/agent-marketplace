## MODIFIED Requirements

### Requirement: Sidebar sources open their Settings entry

A named Manage source action for each sidebar source SHALL open Settings on the Sources tab with that source scrolled into view, focused, and visibly highlighted. When the source is inside a folded group in Settings, the group SHALL unfold; when an active source search hides it, the search SHALL clear. The sidebar add control SHALL open the Add source modal and SHALL NOT open Settings.

#### Scenario: Selecting a sidebar source reveals it in Settings

- **WHEN** the user activates Manage source for a sidebar source
- **THEN** Settings MUST open on the Sources tab with that source's entry in view, focused, and highlighted

#### Scenario: Revealing a source unfolds its folded group

- **WHEN** the source's group is folded in Settings and the user activates Manage source for that source
- **THEN** the group MUST unfold and the source's entry MUST be in view and highlighted

#### Scenario: Sidebar add control focuses the Add source form

- **WHEN** the user activates the sidebar add control next to Sources
- **THEN** the Add source modal MUST open with focus in its Source location field
- **AND** Settings MUST NOT open

## ADDED Requirements

### Requirement: Add source modal

Adding a source SHALL happen in an "Add source" modal that reuses the shared source form with the Directory registry, Git registry, and Marketplace endpoint kinds. The modal SHALL open from the sidebar add control, from the empty-workspace "Add a source" action, and from the Add source button in Settings → Sources. When opened from Settings, Settings SHALL stay open beneath the modal and SHALL regain focus when the modal closes. Settings → Sources SHALL NOT contain an inline add form.

While the add operation is pending, the modal SHALL stay open, its primary button SHALL read "Adding…" and be disabled, and the modal SHALL NOT be dismissible. The modal SHALL close only when the operation settles without an add-source mutation failure. When the operation settles with an add-source mutation failure, the modal SHALL stay open, show that failure's diagnostics inline below the form, and keep the submitted draft.

Settings SHALL NOT repeat a mutation error that belongs to the add operation shown in the modal. Mutation errors from other operations SHALL keep their current place at the top of Settings.

#### Scenario: Add source modal opens from the sidebar plus

- **WHEN** the user activates the sidebar "+" next to Sources
- **THEN** the Add source modal MUST open offering the Directory registry, Git registry, and Marketplace endpoint kinds
- **AND** Settings MUST NOT open

#### Scenario: Add source modal stays open while the add is pending

- **WHEN** the user submits a valid source and the add operation has been accepted but has not settled
- **THEN** the modal MUST stay open with its primary button reading "Adding…" and disabled
- **AND** the modal MUST NOT close on Escape or the close control

#### Scenario: Add source modal closes when the add settles successfully

- **WHEN** the add operation settles without an add-source mutation failure
- **THEN** the modal MUST close
- **AND** the new source MUST appear in the user source list

#### Scenario: Add source modal stays open on a failed add

- **WHEN** the add operation settles with an add-source mutation failure
- **THEN** the modal MUST stay open and show that failure's diagnostics once below the form
- **AND** the submitted kind and location MUST remain in the form

#### Scenario: Settings does not repeat an add failure shown in the modal

- **WHEN** an add-source failure is shown in the Add source modal opened from Settings
- **THEN** Settings MUST NOT show that failure at its top

#### Scenario: Settings keeps other operations' errors at its top

- **WHEN** a mutation error from an operation other than the open add is in the diagnostics while the modal is open
- **THEN** Settings MUST show that error at its top, as before
