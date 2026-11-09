## MODIFIED Requirements

### Requirement: Cursor CLI output supplies the Cursor model inventory

The scheduled audit SHALL capture `agent models` output from the Cursor harness CI image and pass it to `check`. The audit SHALL use the model identifiers parsed from the `<id> - <display name>` lines that follow an `Available models` header line (matched case-insensitively, with an optional trailing colon) as the `cursor-models` inventory. Lines before that header SHALL be ignored. Missing, empty, or unparseable output, including output without the header or without any `<id> - <display name>` line after it, SHALL report `source.inventory-unparsed` for `cursor-models`.

#### Scenario: Cursor CLI output lists model identifiers

- **WHEN** `check` receives Cursor CLI output with `<id> - <display name>` lines after an `Available models` header
- **THEN** the `cursor-models` inventory SHALL contain those identifiers and the audit SHALL compare them with `cursor.yaml`

#### Scenario: Cursor CLI output is missing or unauthenticated

- **WHEN** `check` receives no Cursor CLI output, or output without any `<id> - <display name>` line after an `Available models` header
- **THEN** the audit SHALL report `source.inventory-unparsed` for `cursor-models`

#### Scenario: Cursor CLI diagnostic lines before the header are ignored

- **WHEN** `check` receives Cursor CLI output with `<id> - <display name>`-shaped diagnostic lines such as `error - Authentication required` or `1 - Getting started` before the `Available models` header
- **THEN** the `cursor-models` inventory SHALL contain only the identifiers listed after the header
- **AND** the audit SHALL NOT report those diagnostic lines as models

#### Scenario: Cursor documentation is unparseable but CLI output parses

- **WHEN** the Cursor model documentation cannot be parsed and `check` receives Cursor CLI output that yields a `cursor-models` inventory
- **THEN** the audit SHALL report `source.parse-failed` for `cursor-models` with status `unknown`
- **AND** the audit SHALL NOT report `source.inventory-unparsed` for `cursor-models`
- **AND** the audit SHALL compare the CLI identifiers with `cursor.yaml`
