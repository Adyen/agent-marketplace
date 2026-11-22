## ADDED Requirements

### Requirement: Target lists accept the targets spelling

A v4 root or resource group SHALL accept `targets:` as an alias of `harnesses:`; a non-empty `targets:` list MUST resolve exactly as the same `harnesses:` list, and its IDs MUST be validated like `harnesses:` IDs. Configurations that use only `harnesses:` MUST resolve as before.

#### Scenario: Root and group targets resolve like harnesses

- **WHEN** a user root declares `targets: [claude, factory]` and a resource group declares `targets: [claude]`
- **THEN** `sync` and `status` MUST resolve the same target lists as the equivalent `harnesses:` configuration

#### Scenario: Unknown target IDs under targets are refused

- **WHEN** a root declares `targets:` containing an unknown target ID
- **THEN** commands MUST refuse with `v4.harness` naming the ID, as for `harnesses:`

#### Scenario: Existing harnesses configurations resolve unchanged

- **WHEN** pinned fixtures with an absent user list, a user `harnesses: []`, and non-empty `harnesses:` lists in each layer are resolved through `sync` and `status`
- **THEN** the resolved target lists and their reported layers MUST match the results recorded from the reader before this change

### Requirement: A target list has one spelling

A root or resource group that declares both `targets:` and `harnesses:` MUST be refused with a diagnostic naming the file and both fields, before acquisition, lock or harness writes.

#### Scenario: Both spellings in one file are refused

- **WHEN** a root, or a resource group within it, declares both `targets:` and `harnesses:`
- **THEN** configuration-dependent commands MUST refuse with a non-zero outcome naming the file and both fields
- **AND** configuration, acquisition locks and installed output MUST remain unchanged

### Requirement: Empty targets lists belong to the user root

An empty `targets: []` MUST be accepted only in the user root. In an organization or repository root or in a resource group it MUST be refused with `v4.harness` before acquisition or writes.

#### Scenario: Empty targets outside the user root is refused

- **WHEN** an organization root, a repository root, or a resource group declares `targets: []`
- **THEN** commands MUST refuse with `v4.harness` before acquisition or writes
