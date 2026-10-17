## MODIFIED Requirements

### Requirement: Diagnostic sessions require review and explicit export

A diagnostic session SHALL operate only at effective level `detailed`, buffer evidence locally, redact URL credentials, and write a review payload under the telemetry diagnostic directory. Export SHALL require interactive consent or explicit noninteractive sensitive-data acknowledgement. Successful diagnostic completion SHALL require both endpoint delivery and review-payload deletion; declined or failed delivery SHALL retain the payload. A payload deletion failure after delivery SHALL report its filesystem cause and retained payload path without claiming successful completion. Diagnostic failure SHALL NOT change the investigated command's outcome. Diagnostic payloads SHALL NOT enter the normal pending telemetry queue.

#### Scenario: Diagnostics require detailed telemetry

- **WHEN** a diagnostic session is requested at off, basic, or detailed level
- **THEN** capture MUST occur only at detailed level

#### Scenario: Diagnostic failure retains redacted review evidence

- **WHEN** diagnostic export fails
- **THEN** the retained payload MUST identify the investigated trace and installation while redacting URL credentials

#### Scenario: Noninteractive diagnostics require acknowledgement

- **WHEN** diagnostic export runs without a terminal or explicit acknowledgement
- **THEN** the payload MUST remain local and the CLI MUST explain the acknowledgement requirement

#### Scenario: Successful diagnostic export removes review evidence

- **WHEN** explicitly accepted diagnostic export succeeds
- **THEN** the local review payload MUST be deleted

#### Scenario: Failed diagnostic export is never queued

- **WHEN** an accepted diagnostic export reaches an endpoint that refuses it
- **THEN** the payload MUST remain reviewable and MUST NOT create a pending telemetry record

#### Scenario: Diagnostic payload cleanup failure is reported

- **WHEN** accepted diagnostic delivery succeeds but removing the local review payload fails
- **THEN** diagnostic completion MUST report the filesystem error and retained path without reporting success
- **AND** the retained payload bytes MUST remain unchanged without staging residue

### Requirement: Local telemetry state is inspectable and resettable

Telemetry state SHALL live under `$XDG_STATE_HOME/agent-marketplace/telemetry`, defaulting to `~/.local/state/agent-marketplace/telemetry`. Installation IDs SHALL persist privately and be resettable. Pending telemetry records SHALL have a default limit of 5 MiB or seven days, evicting oldest first, and SHALL be retried in order. Status SHALL expose effective policy, route, identity, pending records, and failures. Preview SHALL write local sample evidence without delivery. Purge SHALL remove queued records and retained diagnostics; reset SHALL additionally replace installation identity only after cleanup succeeds. Reset cleanup failure SHALL propagate its filesystem cause without claiming success or creating a replacement ID, and MAY leave the old ID and queued records already removed.

#### Scenario: Installation identity persists privately and resets

- **WHEN** installation identity is loaded repeatedly and then reset
- **THEN** it MUST remain stable with owner-only access until reset, which MUST clear queued and retained diagnostic evidence and permit a new identity

#### Scenario: Pending records are bounded and retried in order

- **WHEN** pending records exceed size or age limits or become deliverable
- **THEN** default retention MUST be 5 MiB and seven days, oldest records MUST be evicted first, and delivery MUST preserve queue order

#### Scenario: Status and preview expose local evidence

- **WHEN** users inspect telemetry status or request a preview
- **THEN** status MUST expose local delivery evidence and preview MUST write a sample payload without sending it

#### Scenario: Purge removes queued and diagnostic evidence

- **WHEN** telemetry purge runs with queued records and retained diagnostics
- **THEN** it MUST remove both and report their counts

#### Scenario: Installation ID reset cleanup failure is reported

- **WHEN** installation-ID reset cannot remove retained diagnostic evidence after removing the old ID and queued records
- **THEN** the reset command MUST fail with the filesystem cause, preserve remaining diagnostic bytes and metadata, and neither claim success nor create a replacement ID
- **AND** a retry after restoring directory permissions MUST finish cleanup and create a new installation ID
