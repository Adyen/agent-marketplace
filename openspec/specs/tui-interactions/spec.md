# tui-interactions Specification

## Purpose
Defines modal keyboard interactions and guarded configuration writes so selection, cancellation and scope changes cannot silently mutate user data.

## Requirements

### Requirement: Explicit modal intent

q, Esc, and Ctrl+C SHALL be routed before a form may submit. Toggle and config-editor accelerators SHALL retain their documented TUI actions. The TUI SHALL NOT offer a consumer migration modal or an automatic conversion action. Unsupported consumer configuration SHALL surface the path-specific diagnostic defined by `consumer-config-format` and block configuration-dependent actions rather than enter legacy operation. Modal submission SHALL use the `session-interactions` guarded mutation contract.

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

The initial harness-selection and target-harness dialogs SHALL restore the complete supported option list when the terminal expands after a compact layout. Resizing SHALL preserve the cursor and selected supported harness target IDs.

#### Scenario: Harness options return after compact resize

- **WHEN** a user opens either harness dialog at a supported full size, selects supported harness target IDs, shrinks the terminal to a compact size, then expands it to the original size
- **THEN** all supported harness choices SHALL be visible with the same cursor and selected IDs

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

### Requirement: Multi-harness key bindings

The TUI SHALL use g and p to select the edited scope without writing, Space to toggle the focused Harness target without writing, and Enter to submit the focused multi-selection through the guarded save. Surface-neutral selection and validation rules follow `session-interactions`.

#### Scenario: Scope and selection without write

- **WHEN** a user switches scopes with g or p, toggles a Harness target with Space, and cancels
- **THEN** each scope's selections remain available and all configuration bytes stay unchanged

#### Scenario: Enter saves selected Harness targets

- **WHEN** a user has a non-empty Harness target selection in the focused form and presses Enter
- **THEN** the TUI SHALL submit that selection through the guarded write

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

### Requirement: Install from an unconfigured source

Pressing `i` in the resource list SHALL open an install-from-source modal with
three steps: Source, Pick, and Review. The Source step SHALL accept a Git URL,
a GitHub or GitLab web URL, or a directory path, plus an optional ref and
path; a web URL SHALL fill the ref and path it encodes. Before any network
access, the TUI SHALL reject a source that the effective allowlist blocks or
whose registry path is invalid, and SHALL show the rejection on the Source
step. The Pick step SHALL show the resources the source contains, grouped by
kind, with the commit the ref resolved to, counts per kind, and a status for
each resource: already installed, conflicting with an installed resource from
another source, or malformed. Malformed resources SHALL NOT be selectable. An
empty source SHALL show a hint and SHALL NOT advance to Review. A source that
expands into several bundles SHALL list the bundle paths and SHALL preview the
chosen bundle path. The Review step SHALL offer only the scopes the source may
be written to, SHALL name the configuration file that will be written, and
SHALL list the selected resources. Confirming SHALL write the source and the
selected resources to the chosen scope in one guarded mutation, with the same
configuration, acquisition lock, and harness results as `install --from` with
those names. The install SHALL refuse when the source no longer resolves to
the previewed commit. Previewing, cancelling, and every refusal SHALL leave
configuration, acquisition lock, and harness bytes unchanged. The modal SHALL
NOT open when the consumer configuration is unsupported or invalid, or while
another operation is active.

#### Scenario: Previewing a source writes nothing

- **WHEN** a user presses `i`, enters an unconfigured Git source, and the Pick
  step shows its resources with the resolved commit and counts per kind
- **THEN** configuration, acquisition lock, and harness bytes MUST be
  unchanged

#### Scenario: Picked resources install at the chosen scope

- **WHEN** a user picks a skill and a command from the previewed source,
  chooses project scope, and confirms
- **THEN** the repository configuration MUST record the source with both
  resources selected, the repository acquisition lock MUST record the source
  at the previewed commit, both resources MUST appear in the resource list,
  and a sync MUST materialize them in each target harness

#### Scenario: Blocked source is refused before network access

- **WHEN** a user enters a source that the effective allowlist blocks
- **THEN** the Source step MUST show the refusal, the source MUST NOT be
  fetched, and configuration, acquisition lock, and harness bytes MUST be
  unchanged

#### Scenario: Moved ref refuses the stale install

- **WHEN** the previewed source's ref advances to a new commit before the user
  confirms the install
- **THEN** the install MUST be refused with the previewed and current commits,
  the modal MUST return to the Pick step, and configuration, acquisition lock,
  and harness bytes MUST be unchanged

#### Scenario: Cancelling at any step preserves bytes

- **WHEN** a user cancels the modal from the Source, Pick, or Review step
- **THEN** the modal MUST close and configuration, acquisition lock, and
  harness bytes MUST be unchanged

#### Scenario: Malformed and empty sources are not installable

- **WHEN** a previewed source contains a malformed resource, or contains no
  discoverable resources
- **THEN** the malformed resource MUST be shown as not selectable, and an
  empty source MUST show a hint and MUST NOT advance to Review

#### Scenario: Source with several bundles asks for a bundle path

- **WHEN** a previewed source expands into several bundles
- **THEN** the Pick step MUST list the bundle paths instead of resources, and
  choosing one MUST preview that bundle path

#### Scenario: Install from source is refused while mutations are unavailable

- **WHEN** a user presses `i` while another operation is active or the
  consumer configuration is unsupported
- **THEN** the modal MUST NOT open, the TUI MUST explain the refusal, and
  configuration, acquisition lock, and harness bytes MUST be unchanged

### Requirement: Install-from-source step navigation

In the install-from-source modal, `→` SHALL act as `enter` on the Pick step
and `←` SHALL return from Pick to Source and from Review to Pick. `→` on the
Review step SHALL NOT install. On the Source step `←` and `→` SHALL keep
moving the text cursor, and while the Pick filter is being edited they SHALL
edit the filter. On the Source step `↓` SHALL move focus from URL to Ref to
Path and `↑` SHALL move it back, without wrapping and without completing the
step; `k` and `j` SHALL remain text input. Confirming the Source step with
the same URL, ref, and path as the shown preview SHALL return to Pick with
that preview and the current picks without previewing the source again; any
changed field SHALL preview again. On the Review step `↑`/`k` and `↓`/`j`
SHALL move the scope selection through User, Project, and Local in that order
without wrapping; moving onto a scope the source cannot be written to SHALL
keep the current scope and show the reason.

#### Scenario: Arrow keys walk the steps without installing

- **WHEN** a user on the Pick step with a picked resource presses `→`, then
  presses `→` again on Review, then presses `←` twice
- **THEN** the modal MUST show Review after the first `→`, MUST stay on Review
  without installing after the second, and MUST show Pick and then Source,
  and configuration, acquisition lock, and harness bytes MUST be unchanged

#### Scenario: Arrow keys move between Source fields

- **WHEN** a user on the Source step presses `↑` on URL, types, presses `↓`
  twice and types, presses `↓` again and types, then presses `↑` and types
- **THEN** the first text MUST go to URL, the second and third to Path, the
  last to Ref, and the Source step MUST stay open without previewing

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

### Requirement: Personal-local writes are git-ignored at once

A successful TUI write to the personal-local layer, whether a single toggle, a local exclude, or a section bulk toggle, SHALL apply in the same operation the git-ignore step that sync applies, adding `.agents/config.local.yaml` and `.agents/lock.local.yaml` to `.gitignore`, so the personal-local files do not appear as untracked repository files while a sync is pending. Like sync, the step SHALL leave an unsafe `.gitignore`, such as a symbolic link, untouched and log the refusal instead of failing the write. Writes to other layers SHALL leave `.gitignore` unchanged.

#### Scenario: Personal-local writes are git-ignored at once

- **WHEN** a TUI toggle, local exclude or bulk toggle writes the personal-local layer in a repository without those entries in `.gitignore`
- **THEN** `.gitignore` MUST list `.agents/config.local.yaml` and `.agents/lock.local.yaml` before any sync runs
- **AND** a TUI write to the repository layer MUST NOT change `.gitignore`

### Requirement: Install-from-source recovers from a lock-pinned source

When the install-from-source modal's install is refused because the chosen
scope's acquisition lock pins the source at a commit other than the previewed
one, the modal SHALL return to the Review step with the picks and the chosen
scope kept, SHALL show the locked and previewed commits and the `update`
command that advances the source, and SHALL NOT preview again.

#### Scenario: Lock-pinned source returns to Review with update remediation

- **WHEN** a user picks resources from a source that is already configured
  and locked behind the remote in project scope, chooses project scope, and
  confirms
- **THEN** the modal MUST stay on the Review step showing the locked and
  previewed commits and the `update` command, MUST NOT start a preview, and
  configuration, acquisition lock, and harness bytes MUST be unchanged

### Requirement: Lock-pinned refusal offers another scope

The install-from-source modal's lock-pinned remediation SHALL offer choosing
another scope besides the `update` command. Choosing a scope whose lock does
not pin the source behind the preview and confirming SHALL install the picks
into that scope.

#### Scenario: Lock-pinned source installs into another scope

- **WHEN** an install into project scope was refused because the project lock
  pins the source behind the preview, and the user then chooses Local on the
  Review step and confirms
- **THEN** the modal MUST close, local configuration MUST record the picked
  resources, project configuration and acquisition lock bytes MUST be
  unchanged, and a sync MUST leave the list in sync
