## ADDED Requirements

### Requirement: Install defaults to user scope

Without `--scope`, the install command SHALL write resource selections to the user configuration layer and materialize them at the machine destination for named, collection, `--tag`, `--all`, `--from`, and Marketplace locator installs. Marketplace endpoint registration MUST continue to default to user scope. Explicit `--scope repo` and `--scope local` MUST retain their existing behavior. The default MUST NOT relax the user-scope boundary, and `remove` MUST retain its existing scope inference.

#### Scenario: Install without scope writes user configuration

- **WHEN** a user installs a configured Git resource without `--scope` from inside a repository
- **THEN** the user configuration MUST select the resource and the resource MUST materialize at the machine destination
- **AND** the repository and personal-local configuration MUST remain unchanged, and an absent repository root MUST remain absent

#### Scenario: Relative directory source without scope is refused

- **WHEN** a user runs `install --from` with a repository-relative directory source and no `--scope`
- **THEN** the command MUST refuse with the existing scope-boundary error
- **AND** existing configuration, acquisition locks, and materialized resources MUST remain unchanged and an absent user root MUST remain absent
