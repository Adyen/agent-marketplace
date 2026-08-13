## Requirements

### Requirement: Local harness-compilation package

`agent-marketplace` SHALL build against `pkg/harness-compilation` within the root module without requiring an external harness-compilation module dependency.

#### Scenario: Root module has no external harness-compilation dependency

- **WHEN** `go list -m all` is run from the repository root
- **THEN** the module list MUST NOT include an external harness-compilation module

#### Scenario: Root module owns harness-compilation packages

- **WHEN** `go list -f '{{.ImportPath}} -> {{.Module.Path}}' ./pkg/harness-compilation/...` is run from the repository root
- **THEN** every listed package MUST map to the root `github.com/Adyen/agent-marketplace` module

#### Scenario: No nested Go module remains

- **WHEN** the repository is inspected after migration
- **THEN** `pkg/harness-compilation/go.mod` MUST NOT exist
- **AND** `pkg/harness-compilation/go.sum` MUST NOT exist

### Requirement: Path-rewritten upstream history

The imported `harness-compilation` code SHALL retain upstream Git history as reachable commits with tracked files rewritten under `pkg/harness-compilation/`.

#### Scenario: Imported history is reachable

- **WHEN** Git history for the migration is inspected
- **THEN** the imported `harness-compilation` default-branch history MUST be reachable from the `agent-marketplace` branch history
- **AND** the imported files in that history MUST appear under `pkg/harness-compilation/`

#### Scenario: Rewritten import has no root-level HCC files

- **WHEN** the rewritten upstream branch is verified before merge
- **THEN** every tracked file from the imported upstream history MUST be located below `pkg/harness-compilation/`

### Requirement: Local import paths

All references to `harness-compilation` packages SHALL use the root-module local import path after assimilation.

#### Scenario: External imports are removed

- **WHEN** source files in `cmd`, `internal`, and `pkg/harness-compilation` are searched for imports
- **THEN** no Go import path MUST reference `example.com/harness-compilation`
- **AND** HCC imports MUST use `github.com/Adyen/agent-marketplace/pkg/harness-compilation`

### Requirement: Existing HCC-backed behavior remains compatible

The migration SHALL preserve existing agent-marketplace behavior for HCC-backed catalog loading, resolving, materialization, conversion, doctor diagnostics, and TUI flows.

#### Scenario: Existing validators pass

- **WHEN** `go test ./...`, `make ci-lint`, and `make build` are run after the migration
- **THEN** all commands MUST pass

#### Scenario: Doctor reports embedded source truthfully

- **WHEN** `agent-marketplace doctor` reports harness-compilation source information
- **THEN** it MUST report that harness-compilation is embedded or local under `pkg/harness-compilation`
- **AND** it MUST NOT report an external module version for the removed dependency
