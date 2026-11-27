## ADDED Requirements

### Requirement: Unified scope toggles for organization defaults and local overrides

In the toggle modal, `[g]` on an optional organization Marketplace resource with an overridable pin SHALL toggle its user exclusion transparently without a separate `[e]` action. On a non-required, non-machine-scope, non-Marketplace resource enabled in project config that supports personal-local exclusion, `[l]` SHALL toggle its personal-local exclusion transparently; otherwise `[l]` SHALL retain personal-local selection toggling. Project Marketplace pins SHALL keep `[l]` as a local installation-scope control, and the modal SHALL NOT offer an ineffective local exclusion. Required resources and machine-scope local boundaries SHALL stay enforced.

#### Scenario: Global key disables and restores an optional organization default

- **WHEN** a user opens the toggle modal on an optional organization Marketplace default and presses `g`
- **THEN** the TUI MUST write a user exclusion to `~/.agents/config.yaml` and show the resource as excluded
- **AND** opening the toggle modal on that excluded resource and pressing `g` again MUST remove the user exclusion and restore the organization default

#### Scenario: Local key disables and restores a project-committed resource locally

- **WHEN** a user opens the toggle modal on a non-required, non-machine-scope, non-Marketplace resource enabled in project config that supports personal-local exclusion and presses `l`
- **THEN** the TUI MUST write a personal-local exclude to `.agents/config.local.yaml`
- **AND** pressing `l` again MUST clear that personal-local exclude while preserving the repository-committed config

#### Scenario: Project Marketplace pins keep local installation controls

- **WHEN** a user opens the toggle modal on a project Marketplace pin
- **THEN** `[l]` MUST toggle the personal-local installation scope and the modal MUST NOT offer a local exclusion

#### Scenario: Required resources and machine-scope local boundaries remain enforced

- **WHEN** a resource is marked required or is a non-overridable machine-scope resource
- **THEN** required resources MUST remain blocked from toggling, and personal-local toggles MUST NOT write local excludes for machine-scope resources

## MODIFIED Requirements

### Requirement: Explicit filtered section bulk toggle

Space on a section header and the palette's Toggle all in section action SHALL open a bulk toggle modal; Space on a resource SHALL retain its existing behavior. Enter, za, h, and l SHALL retain folding. The target set SHALL include the section's descendants even when folded, limited to the current search matches. Required, malformed, unverified, unavailable or broken, noninstallable cached discovery, collection-only, and otherwise scope-ineligible rows SHALL be skipped under the existing single-resource guards. Already-target-state resources SHALL be skipped. A wholly ineligible section SHALL still open the modal and explain skips. The modal SHALL show section and match counts, active filter, scope state counts, and skip reasons. It SHALL require an available global, project, local, or exclude scope, explicit enable or disable direction, then confirmation of affected count and target layer. Future resources SHALL NOT be included in a past action. Esc at any step SHALL close without writes. Opening and submitting SHALL honor existing busy/config validity guards. For each eligible row, `g` SHALL use the user exclusion for an optional organization Marketplace default; `l` SHALL use a personal-local exclusion only when the row supports one. Project Marketplace pins SHALL retain local installation-scope controls. The bulk `e` scope SHALL remain available for its existing eligible rows.

#### Scenario: Filtered folded descendants and skipped rows

- **WHEN** a user invokes bulk toggle on a section with folded descendants, a search filter, mixed states, and guarded rows, or on a section with no eligible rows
- **THEN** only matching eligible descendants needing the chosen direction SHALL be proposed, counts and skip reasons SHALL describe the target set, no-eligible sections SHALL remain inspectable without writes, and resource Space and other fold keys SHALL retain their behaviors

#### Scenario: Scope direction confirmation and cancellation

- **WHEN** a user chooses an available scope and explicit direction, attempts an unavailable scope, or cancels at any stage before confirmation
- **THEN** no write SHALL occur before affirmative confirmation of count and layer, unavailable scopes SHALL be refused, and cancellation SHALL preserve all config bytes

#### Scenario: Bulk global and local keys use row-eligible exclusions

- **WHEN** a confirmed bulk toggle includes optional organization Marketplace defaults, project-enabled resources that support personal-local exclusion, or project Marketplace pins
- **THEN** `g` SHALL apply user exclusions to eligible organization defaults, `l` SHALL apply personal-local exclusions only to eligible project resources, project Marketplace pins SHALL remain installation-scope actions, and all edits to a single target layer SHALL publish atomically
