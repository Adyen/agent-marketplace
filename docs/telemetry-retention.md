# Telemetry Retention and Access Policy

This document describes how the agent-marketplace CLI and the community telemetry receiver collect, retain, and delete telemetry data.

## Data categories

The community telemetry endpoint accepts only the schema-v1 basic metrics defined by the CLI:

- `agent_marketplace.command.invocations` — a counter of command executions.
- `agent_marketplace.command.duration_ms` — a histogram of command durations.

Each request carries the following resource attributes:

- `telemetry.schema.version` — always `"1"` for the current schema.
- `installation.id` — a random, resettable, per-installation identifier.
- `cli.version` — the CLI version string.
- `os.name` and `arch` — the operating system and architecture.

Metric data-point attributes include only:

- `command.name` — the command path (for example, `agent-marketplace sync`).
- `harnesses` — the comma-separated list of configured harness targets.
- `managed` — whether the installation is under organization policy.
- `outcome.code` — `success` or `error`.

The following data is **never** collected at the basic level:

- command arguments, file paths, repository names, or registry URLs;
- user names, hostnames, or network addresses;
- prompts, terminal contents, or resource contents.

## Detailed and diagnostic data

Detailed telemetry adds lifecycle spans for commands, the TUI, configuration
snapshots, and pipeline stages. Configuration snapshots include configured
remote registry locations and exact enabled resource names grouped by kind,
registry, and configuration scope. URL user information, query strings, and
fragments are removed, local registry paths are represented only as `local`,
and large resource sets are split into bounded chunks. Search text, resource
contents, and other user content are not attached.

Diagnostic telemetry is a per-invocation capture (`--diagnostic`) that buffers
metrics, traces, and selected events in memory and writes a review payload
locally before export. The CLI redacts URL credentials from every string value
in the payload. The review payload is stored under the user's state directory
with owner-only permissions (`0o600`) and is deleted after a successful export.
On export failure the payload is retained for manual review.

## Retention period

The community receiver stores accepted requests as JSON-lines, one file per calendar day. Data is retained for **30 days** from the date it was received. After 30 days the daily files are eligible for deletion by the operator.

Queued telemetry payloads on the CLI are retained for **7 days** or until the 5 MiB queue cap is reached, whichever comes first.

## Who can access the data

Access to the raw JSON-lines storage is limited to the operating-system user that owns the receiver process and the storage directory. Files are created with owner-only permissions (`0o600`) and directories with owner-only access (`0o700`).

Dashboard summaries (`/v1/dashboard.json`) are intentionally low-cardinality and may be exposed to authorized operators for operational monitoring.

## Deletion procedure

To remove data before the automatic retention period:

1. Stop the receiver process.
2. Remove the relevant files from the storage directory. Daily files are named `YYYY-MM-DD.jsonl` and the cardinality state is stored in `cardinality.json`.
3. Restart the receiver. It will rebuild its in-memory adoption counters from the remaining files but will not reconstruct data points that have been deleted.

To reset an individual installation ID, a user can run `agent-marketplace telemetry reset-id`, which deletes the local installation ID file and purges any queued payloads.

## Policy updates

When the retention policy changes, the new policy is published at the URL configured as `DefaultProjectRetentionURL` at release build time. The CLI shows the effective retention policy URL in `agent-marketplace telemetry status`.
