## MODIFIED Requirements

### Requirement: Target harnesses resolve per selecting scope

Output selected by the repository or personal-local configuration MUST use the repository harness list when it is non-empty, otherwise the user list, otherwise the organization list. Output selected by the user or organization configuration MUST use the user harness list when it is non-empty, otherwise the organization list. Repository and personal-local configuration MUST NOT narrow or replace that machine list. Only when neither the user nor the organization configuration declares targets MUST their selections use the repository list. An authored user `targets: []` MUST count as a declared, empty user list: wherever the user list applies, selections MUST receive no harness target, and neither the organization list nor the repository fallback MUST replace it. An absent user list and a user `harnesses: []` MUST keep inheriting as above, and deleting the user `targets:` key MUST restore that inheritance. A collection's members MUST use the list of the layer that selected the collection. Per-source group `harnesses` or `targets` MUST keep narrowing the applicable list and MUST never add a harness. `sync` and `deinit --scope user` MUST apply this rule when computing the output they install, keep or remove. When the two lists differ, `status` MUST report both, each with the layer that supplied it.

#### Scenario: Repository harness list does not narrow machine-scope output

- **WHEN** the user and organization configurations declare `harnesses: [factory]` with selected skills, and a repository declares `harnesses: [claude]` with its own selected skill
- **THEN** `sync` inside that repository MUST keep the user and organization skills installed for Factory in machine scope
- **AND** the repository skill MUST be installed only for Claude in the repository
- **AND** `status` MUST report the repository list from the repository layer and the machine list from the user layer

#### Scenario: Machine-scope teardown preserves organization output for machine targets

- **WHEN** `deinit --scope user` runs inside a repository whose `harnesses:` list omits a target that the organization's list declares
- **THEN** organization output for that target MUST remain installed, while user-owned machine output MUST be removed

#### Scenario: Machine-scope output falls back to the repository list

- **WHEN** neither the user nor the organization configuration declares `harnesses`, and a repository declares `harnesses: [claude]`
- **THEN** `sync` inside that repository MUST install selected user skills for Claude in machine scope

#### Scenario: Explicit empty user target list selects no machine targets

- **WHEN** the organization declares `harnesses: [factory]` with a selected skill, the user root declares `targets: []`, and a repository declares `harnesses: [claude]`
- **THEN** `sync` inside that repository MUST install no machine-scope output for the organization or user selections
- **AND** `status` MUST report the empty machine list from the user layer
- **AND** after the user `targets:` key is deleted, `sync` MUST install the organization skill for Factory again

#### Scenario: Group target list cannot add a harness

- **WHEN** the user root declares `targets: [factory]` and a resource group declares `targets: [factory, claude]`
- **THEN** the group's resources MUST be installed only for Factory
