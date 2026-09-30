# materialization Specification

## Purpose
Defines how selected resources appear in harness targets, including stable
commands in shared hook files across repository and machine install scopes.
## Requirements
### Requirement: Machine-scope hook commands are location-independent

Agent Marketplace SHALL expand `${DROID_PLUGIN_ROOT}` in a machine-scope
Factory hook command to the absolute machine harness target root. Its
materialized command bytes MUST NOT depend on whether sync runs from the
user's home or a repository. Repository-scope Factory hook commands SHALL use
the relative repository harness target root.

#### Scenario: Machine-scope hook command uses the absolute machine root

- **WHEN** a user-selected Factory hook command contains `${DROID_PLUGIN_ROOT}` and sync runs from the user's home or from a repository
- **THEN** the machine-scope hook file MUST contain the same command with the absolute machine root in both cases
- **AND** `sync --check` MUST succeed from either location after synchronization

#### Scenario: Repository-scope hook command stays repository-relative

- **WHEN** a repository-selected Factory hook command contains `${DROID_PLUGIN_ROOT}`
- **THEN** the repository-scope hook file MUST contain a command rooted at the relative `.factory` path
