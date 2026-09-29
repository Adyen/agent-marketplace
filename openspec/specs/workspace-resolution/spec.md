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

### Requirement: Invalid roots cannot reach mutation

The CLI MUST reject a personal-local root outside its repository, including a symlink that resolves outside the repository, before preparing locks, writing configuration, or materializing resources.

#### Scenario: Escaping personal-local root

- **WHEN** a personal-local root resolves outside the repository
- **THEN** the command MUST fail without preparing locks or changing configuration or generated output
