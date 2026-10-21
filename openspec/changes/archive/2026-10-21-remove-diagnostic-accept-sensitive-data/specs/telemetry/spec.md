## MODIFIED Requirements

### Requirement: Diagnostic sessions require review and explicit export

A diagnostic session SHALL operate only at effective level `detailed`, buffer evidence locally, redact URL credentials, and write a review payload under the telemetry diagnostic directory. Export SHALL require interactive consent at a terminal; the CLI SHALL offer no flag or other noninteractive acknowledgement, so a diagnostic session without a terminal SHALL keep its payload local. A diagnostic-level telemetry preview SHALL likewise require interactive consent and SHALL be refused without a terminal. Successful diagnostic completion SHALL require both endpoint delivery and review-payload deletion; declined or failed delivery SHALL retain the payload. A payload deletion failure after delivery SHALL report its filesystem cause and retained payload path without claiming successful completion. Diagnostic failure SHALL NOT change the investigated command's outcome. Diagnostic payloads SHALL NOT enter the normal pending telemetry queue.

#### Scenario: Diagnostics require detailed telemetry

- **WHEN** a diagnostic session is requested at off, basic, or detailed level
- **THEN** capture MUST occur only at detailed level

#### Scenario: Diagnostic failure retains redacted review evidence

- **WHEN** diagnostic export fails
- **THEN** the retained payload MUST identify the investigated trace and installation while redacting URL credentials

#### Scenario: Noninteractive diagnostics require acknowledgement

- **WHEN** a diagnostic session completes without a terminal
- **THEN** the payload MUST remain local without any export request, and the CLI MUST explain that export requires interactive acknowledgement at a terminal

#### Scenario: Sensitive-data acknowledgement flag is not accepted

- **WHEN** an invocation passes `--diagnostic-accept-sensitive-data`
- **THEN** the CLI MUST reject it as an unknown flag without capturing or exporting a diagnostic payload

#### Scenario: Noninteractive diagnostic preview is refused

- **WHEN** `telemetry preview --level diagnostic` runs without a terminal
- **THEN** the command MUST fail, explaining that diagnostic preview requires an interactive terminal, and MUST write no preview payload

#### Scenario: Successful diagnostic export removes review evidence

- **WHEN** interactively accepted diagnostic export succeeds
- **THEN** the local review payload MUST be deleted

#### Scenario: Failed diagnostic export is never queued

- **WHEN** an accepted diagnostic export reaches an endpoint that refuses it
- **THEN** the payload MUST remain reviewable and MUST NOT create a pending telemetry record

#### Scenario: Diagnostic payload cleanup failure is reported

- **WHEN** accepted diagnostic delivery succeeds but removing the local review payload fails
- **THEN** diagnostic completion MUST report the filesystem error and retained path without reporting success
- **AND** the retained payload bytes MUST remain unchanged without staging residue
