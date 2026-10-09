## ADDED Requirements

### Requirement: Previewable view settings

The resource list SHALL open View settings with `v` or the palette. Primary and secondary choices SHALL preview live behind the modal and SHALL enforce distinct valid axes. Enter SHALL retain the preview; Esc SHALL restore the prior choices without writing preferences or configuration. Modal cancellation and hard quit SHALL retain existing routing, and changes SHALL NOT add a permanent tab or indicator line.

#### Scenario: Accept and discard live grouping preview

- **WHEN** a user opens View settings through either entry point, previews a different grouping, and accepts with Enter or cancels with Esc
- **THEN** the list SHALL preview valid axes, Enter SHALL retain the changed view, Esc SHALL restore the original view without writes, and field navigation SHALL remain usable

### Requirement: Explicit filtered section bulk toggle

Space on a section header and the palette's Toggle all in section action SHALL open a bulk toggle modal; Space on a resource SHALL retain its existing behavior. Enter, za, h, and l SHALL retain folding. The target set SHALL include the section's descendants even when folded, limited to the current search matches. Required, malformed, unverified, unavailable or broken, noninstallable cached discovery, collection-only, and otherwise scope-ineligible rows SHALL be skipped under the existing single-resource guards. Already-target-state resources SHALL be skipped. A wholly ineligible section SHALL still open the modal and explain skips. The modal SHALL show section and match counts, active filter, scope state counts, and skip reasons. It SHALL require an available global, project, local, or exclude scope, explicit enable or disable direction, then confirmation of affected count and target layer. Future resources SHALL NOT be included in a past action. Esc at any step SHALL close without writes. Opening and submitting SHALL honor existing busy/config validity guards.

#### Scenario: Filtered folded descendants and skipped rows

- **WHEN** a user invokes bulk toggle on a section with folded descendants, a search filter, mixed states, and guarded rows, or on a section with no eligible rows
- **THEN** only matching eligible descendants needing the chosen direction SHALL be proposed, counts and skip reasons SHALL describe the target set, no-eligible sections SHALL remain inspectable without writes, and resource Space and other fold keys SHALL retain their behaviors

#### Scenario: Scope direction confirmation and cancellation

- **WHEN** a user chooses an available scope and explicit direction, attempts an unavailable scope, or cancels at any stage before confirmation
- **THEN** no write SHALL occur before affirmative confirmation of count and layer, unavailable scopes SHALL be refused, and cancellation SHALL preserve all config bytes

### Requirement: Atomic guarded section mutation

A confirmed bulk toggle SHALL apply the existing per-resource selection semantics to one chosen config layer as a single staged transaction. Submission SHALL re-evaluate current targets, eligibility, scope, and operation state rather than trusting modal-open state. A failed action or publication SHALL leave configuration bytes unchanged, with visible failure feedback. A successful batch SHALL produce one authoritative reload and pending-sync outcome rather than a reload per resource. Stale completions SHALL NOT change the active operation or newer view. Bulk toggling SHALL NOT change resolution, ownership, materialization, or selection policy.

#### Scenario: Atomic batch success and failure

- **WHEN** a confirmed batch spans multiple resources or sources, succeeds, or fails on an action after earlier actions were staged or during publication
- **THEN** success SHALL publish all intended edits to the chosen layer and request one authoritative reload, while failure SHALL publish none, preserve original bytes, and report the failure

#### Scenario: Changed eligibility busy operation and stale completion

- **WHEN** rows or eligibility change after opening, another operation is active at submit, or a superseded completion arrives
- **THEN** submit SHALL use current guarded targets or refuse the write, busy submission SHALL NOT start a second mutation, and stale completion SHALL NOT overwrite newer state or claim success
