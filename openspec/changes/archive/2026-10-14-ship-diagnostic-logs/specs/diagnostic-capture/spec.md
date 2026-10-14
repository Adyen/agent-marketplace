## ADDED Requirements

### Requirement: Diagnostic capture records invocation logs at debug level

While diagnostic capture is active, the CLI MUST capture every log record the invocation emits from logging initialization until the diagnostic payload is built, at debug level and above, regardless of the configured log level. Capture MUST continue while a TUI terminal session owns the screen. The log file and the stderr mirror MUST keep their configured levels and contents. When diagnostic capture is not active, including when `--diagnostic` is given but the effective telemetry level is not `detailed`, the CLI MUST NOT write or send captured log records.

#### Scenario: Debug records are captured when the log level is info

- **WHEN** a command runs with `--diagnostic` at effective telemetry level `detailed` while the configured log level is `info`
- **THEN** the review payload MUST list the invocation's debug, info, warn, and error records under `logs`
- **AND** the log file MUST NOT contain the debug records

#### Scenario: Records logged during a TUI session are captured

- **WHEN** a record is logged while a terminal session is active during a diagnostic invocation
- **THEN** the review payload MUST list that record under `logs`

#### Scenario: Runs without diagnostic capture send no logs

- **WHEN** a command runs at effective level `detailed` without `--diagnostic`, or with `--diagnostic` at effective level `basic`
- **THEN** the CLI MUST NOT send any request to `/v1/logs`

### Requirement: Captured log records are correlated with the invocation trace

Every captured record MUST carry the trace ID of the invocation's root command span. Its span ID MUST be that of the span carried by the record's context when it is a valid span of the invocation, and the root command span's ID otherwise, including for records logged before the root span started.

#### Scenario: A record logged with a span context carries that span

- **WHEN** a record is logged with a context that carries a child span of the invocation
- **THEN** the exported record MUST carry the invocation's trace ID and that child span's ID

#### Scenario: A record logged without a span carries the root span

- **WHEN** a record is logged without a context span, or before the root span started
- **THEN** the exported record MUST carry the invocation's trace ID and the root command span's ID

### Requirement: Captured log records are bounded and redacted

The CLI MUST keep at most 2,000 captured records per invocation, whose bounded messages, attribute keys, and attribute values total at most 4 MiB, and report the number of further records as `logs_dropped`. A message longer than 8 KiB MUST be truncated to at most 8 KiB, ending in `…[truncated]`. Attributes in groups MUST be flattened to dotted keys and their values converted to strings. A value longer than 2 KiB MUST be truncated to at most 2 KiB the same way. Attributes whose key does not match `^[a-z][a-z0-9_.]{0,63}$` MUST be dropped, and a record MUST keep at most its first 32 remaining attributes. URL credentials MUST be removed from the message and every attribute value before the review payload is written or the records are exported.

#### Scenario: Excess and oversized records are bounded

- **WHEN** a diagnostic invocation logs more than 2,000 records, a message over 8 KiB, and a record with more than 32 valid attributes
- **THEN** the payload MUST list 2,000 records and report the remainder as `logs_dropped`
- **AND** the long message MUST be truncated to at most 8 KiB ending in `…[truncated]`
- **AND** the record MUST keep only its first 32 attributes

#### Scenario: Captured records stay within the total size budget

- **WHEN** a diagnostic invocation logs fewer than 2,000 records whose bounded sizes together exceed 4 MiB
- **THEN** the payload MUST list only the records that fit within 4 MiB, in emission order
- **AND** report every later record in `logs_dropped`

#### Scenario: URL credentials are removed from log records

- **WHEN** a captured message or attribute value contains a URL with credentials
- **THEN** neither the review payload nor the exported record MUST contain those credentials

### Requirement: Diagnostic log records are reviewed and exported with the other signals

The review payload MUST list each captured record under `logs` with its timestamp, severity, message, trace ID, span ID, and attributes, as exported. On consent, the CLI MUST send the records as OTLP/HTTP logs to the primary endpoint's `/v1/logs` with the redacted resource and instrumentation scope `agent_marketplace`, alongside the metrics and traces requests. A non-success response or transport failure for the logs request MUST fail the diagnostic export and keep the payload, as for metrics and traces. Log records MUST NOT be queued for later delivery.

#### Scenario: Consented export sends logs next to traces

- **WHEN** a diagnostic export is accepted and the endpoint accepts every signal
- **THEN** the endpoint MUST receive a `/v1/logs` request whose records match the payload's `logs` entries
- **AND** those records MUST carry the trace ID of the exported traces

#### Scenario: A rejected logs request fails the export

- **WHEN** the endpoint accepts metrics and traces but rejects `/v1/logs`
- **THEN** the CLI MUST report the diagnostic export as failed
- **AND** keep the review payload
- **AND** the command's exit status MUST be unchanged
