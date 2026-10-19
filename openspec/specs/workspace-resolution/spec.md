# workspace-resolution Specification

## Purpose
Defines how Agent Marketplace locates repository and personal-local configuration and constrains where a nested workspace can write harness output.

## Requirements

### Requirement: Workspace roots remain aligned

The CLI SHALL use the discovered repository configuration and personal-local configuration roots consistently throughout a command. Harness output for a nested personal-local root MUST be placed beneath that nested root without moving the committed repository configuration.

#### Scenario: Nested personal-local synchronization

- **WHEN** sync runs from a nested directory with a personal-local configuration
- **THEN** it MUST read committed repository configuration from the repository root
- **AND** it MUST materialize harness output beneath the nested personal-local root

#### Scenario: Nested personal-local browsing and inspection

- **WHEN** list --from, search, doctor, or cache garbage collection runs from a nested directory with personal-local configuration
- **THEN** each command MUST read committed repository configuration from the repository root and personal-local configuration from the nested root

### Requirement: Invalid roots cannot reach mutation

The CLI MUST reject a personal-local root outside its repository, including a symlink that resolves outside the repository, before reading configuration, preparing locks, writing configuration, collecting cache entries, or materializing resources.

#### Scenario: Escaping personal-local root

- **WHEN** a personal-local root resolves outside the repository
- **THEN** the command MUST fail without preparing locks or changing configuration or generated output

#### Scenario: Escaping personal-local root during browsing and collection

- **WHEN** list --from, search, doctor, or cache garbage collection discovers a personal-local root outside its repository
- **THEN** the command MUST fail before reading configuration, network acquisition, collecting cache entries, or changing configuration or generated output
