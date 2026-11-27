## ADDED Requirements

### Requirement: Unified scope toggles for organization defaults and local overrides

In the toggle modal, `[g]` on an optional organization Marketplace resource SHALL toggle its user exclusion transparently without a separate `[e]` action. On a non-required, non-machine-scope resource enabled in project config, `[l]` SHALL toggle its personal-local exclusion transparently; otherwise `[l]` SHALL retain personal-local selection toggling. Required resources and machine-scope local boundaries SHALL stay enforced.

#### Scenario: Global key disables and restores an optional organization default

- **WHEN** a user opens the toggle modal on an optional organization Marketplace default and presses `g`
- **THEN** the TUI MUST write a user exclusion to `~/.agents/config.yaml` and show the resource as excluded
- **AND** opening the toggle modal on that excluded resource and pressing `g` again MUST remove the user exclusion and restore the organization default

#### Scenario: Local key disables and restores a project-committed resource locally

- **WHEN** a user opens the toggle modal on a non-required resource enabled in project config and presses `l`
- **THEN** the TUI MUST write a personal-local exclude to `.agents/config.local.yaml`
- **AND** pressing `l` again MUST clear that personal-local exclude while preserving the repository-committed config

#### Scenario: Required resources and machine-scope local boundaries remain enforced

- **WHEN** a resource is marked required or is a non-overridable machine-scope resource
- **THEN** required resources MUST remain blocked from toggling, and personal-local toggles MUST NOT write local excludes for machine-scope resources
