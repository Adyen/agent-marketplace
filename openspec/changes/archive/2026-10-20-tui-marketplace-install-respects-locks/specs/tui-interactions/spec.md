## ADDED Requirements

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
