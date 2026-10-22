# tui-interactions Specification

## Purpose
Defines modal keyboard interactions and guarded configuration writes so selection, cancellation and scope changes cannot silently mutate user data.

## Requirements

### Requirement: Multi-harness selection
Harness editing and initial configuration SHALL support selecting multiple harnesses. Switching global and project scopes SHALL load that scope's selection without writing; Space SHALL toggle the focused selection without writing; only explicit Enter SHALL save after non-empty validation. Saving an existing configuration SHALL preserve unknown selected harness IDs.

#### Scenario: Scope and selection without write
- **WHEN** a user switches scopes with g or p, toggles a harness with Space, and cancels
- **THEN** each scope's selections remain available and all configuration bytes stay unchanged

#### Scenario: Save selected harnesses
- **WHEN** a user explicitly submits a non-empty multi-selection that includes an unknown existing ID
- **THEN** the guarded write preserves that ID and updates only the intended scope

#### Scenario: Empty selection
- **WHEN** a user attempts to save an empty harness selection
- **THEN** the form refuses the save without modifying configuration bytes

### Requirement: Explicit modal intent

Toggle and config-editor accelerators SHALL retain their documented actions; sync-and-quit and quit modals SHALL retain affirmative, negative and cancel paths. The TUI SHALL NOT offer a consumer migration modal or an automatic conversion action. Unsupported consumer configuration SHALL surface the path-specific diagnostic defined by `consumer-config-format` and block configuration-dependent actions rather than enter legacy operation. q, Esc and Ctrl+C SHALL be routed before a form may submit. Modal submission SHALL use the existing serialized guarded operation and authoritative post-write reload; no form completion alone SHALL claim a write succeeded.

#### Scenario: Cancel or stale completion

- **WHEN** a user cancels a supported modal or a late write completion arrives for an inactive operation
- **THEN** no unintended write occurs and the active operation and diagnostics remain unchanged

#### Scenario: Guarded submission

- **WHEN** a user submits a mutation while another operation is active or prerequisites are invalid, including unsupported consumer configuration
- **THEN** the same guard used by the keyboard path blocks the new write without claiming success or offering conversion

### Requirement: Fixed-choice modal input ignores search filtering

The toggle and config-editor choice dialogs SHALL keep their fixed options when the user presses `/`; the key SHALL NOT enter text filtering, change the selected choice, or cause a configuration write. Arrow navigation and Escape cancellation SHALL remain available afterward.

#### Scenario: Slash leaves fixed-choice dialogs unchanged

- **WHEN** a user presses `/` in the toggle or config-editor choice dialog
- **THEN** the dialog SHALL retain its current choice without filtering or writing, and the user SHALL still be able to navigate or cancel it

### Requirement: Harness choices return after resizing

The initial harness-selection and target-harness dialogs SHALL restore the complete option list when the terminal expands after a compact layout. Resizing SHALL preserve the cursor and selected harness IDs, including an unknown selected ID already present in configuration.

#### Scenario: Harness options return after compact resize

- **WHEN** a user opens either harness dialog at a supported full size, selects an unknown existing harness ID, shrinks the terminal to a compact size, then expands it to the original size
- **THEN** all harness choices, including the selected unknown ID, SHALL be visible with the same cursor and selected IDs

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

### Requirement: Marketplace installs from discovery

Installing an available resource found through Marketplace discovery SHALL
write its exact pin at the chosen user, project, or local scope and install
it. When the verified plan has security findings, the TUI SHALL show the
plan's preview with each finding and SHALL install only after an explicit
confirmation; declining SHALL write no configuration, acquisition lock, or
harness output. A plan without findings SHALL install without a confirmation
prompt. The sync that follows the install SHALL materialize every other
locked source from the exact revision recorded in its acquisition lock.

#### Scenario: Discovered Marketplace skill installs at the chosen scope

- **WHEN** a user searches, selects an available Marketplace skill whose plan
  has no findings, and installs it at project scope
- **THEN** the repository config and acquisition lock MUST record its exact
  pin, the skill MUST be installed in each target harness, and the settled
  view MUST report the workspace in sync without a confirmation prompt

#### Scenario: Marketplace findings require consent before install

- **WHEN** a user installs a Marketplace skill whose verified plan has a
  security finding
- **THEN** the TUI MUST show the plan's preview with the finding and wait for
  confirmation
- **AND** declining MUST leave configuration, acquisition lock, and harness
  bytes unchanged, while confirming MUST install the skill

#### Scenario: Marketplace install keeps locked sources at their locked revision

- **WHEN** a repository acquisition lock pins a Git source, the source's ref
  advances upstream, and the user then installs a Marketplace skill from the
  TUI
- **THEN** the Git source's installed resources MUST keep their locked
  content, the lock MUST still record the same commit, and `sync --check` MUST
  report the workspace in sync

### Requirement: Retargeted resources report pending sync

After saved target harnesses change, a resource that is still selected SHALL
appear as enabled and pending sync until a sync completes, whether it is
missing from a new target, has a stale copy in a harness it no longer targets,
or both. A broken output SHALL still take precedence and show the resource as
broken.

#### Scenario: Retargeted resource is pending sync

- **WHEN** a user saves project target harnesses that replace the harness a
  selected resource is installed in
- **THEN** before syncing, that resource MUST show as enabled and pending sync
  for its new target, not as orphaned or broken
- **AND** syncing MUST install it into the new target and remove the copy from
  the former one

#### Scenario: Removed target harness is pending cleanup

- **WHEN** a selected resource is in sync in its remaining target harnesses
  and only a copy in a harness that is no longer targeted is left
- **THEN** before syncing, that resource MUST show as enabled and pending sync,
  not as orphaned or broken

### Requirement: Personal-local writes are git-ignored at once

A successful TUI write to the personal-local layer, whether a single toggle, a local exclude, or a section bulk toggle, SHALL ensure in the same operation that `.agents/config.local.yaml` and `.agents/lock.local.yaml` are git-ignored, as sync does, so the personal-local files never appear as untracked repository files while a sync is pending. Writes to other layers SHALL leave `.gitignore` unchanged.

#### Scenario: Personal-local writes are git-ignored at once

- **WHEN** a TUI toggle, local exclude or bulk toggle writes the personal-local layer in a repository without those entries in `.gitignore`
- **THEN** `.gitignore` MUST list `.agents/config.local.yaml` and `.agents/lock.local.yaml` before any sync runs
- **AND** a TUI write to the repository layer MUST NOT change `.gitignore`
