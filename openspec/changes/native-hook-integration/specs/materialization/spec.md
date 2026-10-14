## MODIFIED Requirements

### Requirement: Machine-scope hook commands are location-independent

Agent Marketplace SHALL resolve a Factory hook's plugin-root token to its
installed per-hook support directory through the library declarations.
Machine-scope commands SHALL use absolute support paths below the machine
harness target root and SHALL NOT depend on whether sync runs from the user's
home or a repository. Repository-scope commands SHALL use Factory's declared
project-directory variable followed by a clean repository-relative path below
`.factory/hook-support/`. Support script bytes SHALL remain unchanged.

#### Scenario: Machine-scope hook command uses the absolute machine root

- **WHEN** a user-selected Factory hook command contains a plugin-root token
  and sync runs from the user's home or from a repository
- **THEN** the machine-scope hook file MUST contain the same command with an
  absolute path to the installed support file below the machine Factory root
- **AND** `sync --check` MUST succeed from either location after synchronization

#### Scenario: Repository-scope hook command stays repository-relative

- **WHEN** a repository-selected Factory hook command contains a plugin-root token
- **THEN** the repository-scope hook file MUST contain Factory's declared
  project-directory variable followed by a clean relative path to the installed
  support file below `.factory/hook-support/`
- **AND** no plugin-only variable or compiled-cache command path MUST be emitted
