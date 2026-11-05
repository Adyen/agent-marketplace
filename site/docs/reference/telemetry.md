# Telemetry reference

Telemetry is optional usage data that `agent-marketplace` can send over OTLP/HTTP (the OpenTelemetry protocol). This page lists the levels, the configuration fields, the commands that manage telemetry, what each level sends, and where local telemetry files live. For the reasoning behind these choices, see [Telemetry and privacy](../explanation/telemetry-and-privacy.md).

## Levels

| Level | What is sent |
|---|---|
| `off` | Nothing. This is the level when no configuration sets one. |
| `basic` | A count and a duration for each command, with a few low-detail attributes. |
| `detailed` | Everything in `basic`, plus a snapshot of your configuration (inventory metrics) and traces of each command's steps. |

`diagnostic` is not a level you can save in configuration. It is a one-off capture you request with the `--diagnostic` flag on a single command. It only runs when the effective level is `detailed`. At `basic` the CLI skips the capture and writes a warning to the log. At `off` it skips the capture without a warning. See [Diagnostic payloads](#diagnostic-payloads).

## Configuration schema

```yaml
telemetry:
  level: basic
  endpoint: https://otel.example.com
  community_reporting: false
  retention_policy: https://example.com/telemetry-retention
  ca_file: /home/you/.otel/ca.pem
  headers:
    Authorization:
      env: AGENT_MARKETPLACE_TELEMETRY_AUTH_TOKEN
    X-Tenant:
      file: /home/you/.otel/tenant
```

A `telemetry:` block is allowed only in the organization file and in your user-personal [configuration file](../glossary.md#configuration-file) (`~/.agents/config.yaml`). Repository and personal-local configs that contain one are rejected.

## Fields

| Field | Type | Allowed in | Description |
|---|---|---|---|
| `level` | string | user, organization | `off`, `basic`, or `detailed`. |
| `endpoint` | URL | user, organization | Where telemetry is sent (the primary endpoint). Must use HTTPS, except for loopback addresses such as `localhost`. For a base URL with no path, metrics go to `/v1/metrics` and traces to `/v1/traces`. A URL with a path sends both to that path unchanged. |
| `community_reporting` | boolean | user, organization | Also send the `basic` command metrics to the project's own endpoint. |
| `retention_policy` | URL | user, organization | An `http` or `https` link describing how long the endpoint keeps data. `telemetry status`, `telemetry preview`, and diagnostic output show it. |
| `ca_file` | path | user, organization | PEM file with the certificate authority to trust for the primary endpoint. |
| `allow_insecure` | boolean | organization | Allows plain HTTP to a private network endpoint. A hostname must resolve only to private, loopback, or link-local addresses. The CLI ignores this field in the user file. |
| `headers` | mapping | user, organization | Extra HTTP headers for the primary endpoint. Each header reads its value from exactly one source: `env` (an environment variable name) or `file` (a file path). |

A few rules apply to these fields:

- `headers`, `ca_file`, and `retention_policy` come from the same layer as the endpoint in use. For example, if the organization sets `endpoint`, your user headers are not sent to it.
- A `ca_file` or header `file` must be a regular file, not a symlink, owned by you, and not readable or writable by group or others. Its parent folder must meet the same rules.
- Header values cannot contain line breaks.

## Where telemetry is sent

- If the organization sets `endpoint`, that endpoint is used.
- Otherwise, if you set `endpoint`, yours is used.
- Otherwise, the CLI uses the project endpoint that is built into the release. A build without a project endpoint has nowhere to send data, and `community_reporting` has no effect.

With `community_reporting: true`, the `basic` command metrics also go to the project endpoint. When the primary endpoint is already the project endpoint, the two are sent once, and `telemetry status` reports `folded into primary`.

## Who decides

The organization file takes priority wherever it sets a value.

- If the organization sets `level`, that is the effective level. `telemetry enable` refuses a level below the organization's, and `telemetry disable` saves your preference but reports that the organization keeps telemetry active.
- An organization `endpoint` replaces yours, and `telemetry enable --endpoint` fails with `endpoint is locked by organization policy`.
- An organization `community_reporting` value replaces yours. If the organization has a `telemetry:` block without this field, your value still applies. If neither of you set it, it is off.

Three environment variables override your user settings:

| Variable | Overrides | Skipped when |
|---|---|---|
| `AGENT_MARKETPLACE_TELEMETRY_LEVEL` | `level` | the organization file has any `telemetry:` block |
| `AGENT_MARKETPLACE_TELEMETRY_ENDPOINT` | `endpoint` | the organization sets `endpoint` |
| `AGENT_MARKETPLACE_TELEMETRY_COMMUNITY_REPORTING` | `community_reporting` | the organization sets `community_reporting` |

When `AGENT_MARKETPLACE_TELEMETRY_ENDPOINT` points somewhere other than your configured endpoint, your configured `headers` are not sent to it. Standard `OTEL_*` variables are ignored, and the CLI logs a warning when they are set. The [environment variables reference](./environment-variables.md) covers how invalid values are reported.

## Commands

| Command | What it does |
|---|---|
| `agent-marketplace telemetry status` | Shows the effective level and where it came from, the primary endpoint host, header names, retention policy, community reporting, whether the installation is managed by an organization, the installation ID, pending records, retained diagnostic payloads, the last delivery failure, and any fields the organization locks. |
| `agent-marketplace telemetry enable --level <basic\|detailed> [--endpoint <url>]` | Saves the level (and optional endpoint) in your user config. It requires an existing user config (`agent-marketplace init` creates one). `telemetry disable` turns telemetry off. |
| `agent-marketplace telemetry disable` | Sets your user level to `off`. |
| `agent-marketplace telemetry preview --level <basic\|detailed\|diagnostic>` | Writes an example payload with sample values to a local file and prints its path. Nothing is sent. The `diagnostic` preview asks for confirmation and is refused when not run in a [terminal](../glossary.md#terminal). |
| `agent-marketplace telemetry reset-id` | Deletes the installation ID, queued records, and retained diagnostic payloads, then creates a new ID and prints it. |
| `agent-marketplace telemetry purge` | Deletes queued records and retained diagnostic payloads, and prints how many of each it removed. |

`agent-marketplace init` and the `telemetry` commands themselves never send telemetry.

`telemetry reset-id` reports cleanup errors instead of claiming a completed reset or creating a replacement ID. A failure can occur after deleting the old ID and queued records. Fix the filesystem permissions and retry to finish removing retained diagnostics and create a new ID.

## What each level sends

Every signal carries these resource attributes: `telemetry.schema.version`, `cli.version`, `os.name`, `arch`, and `installation.id`. The installation ID is a random value created on first use. It is not derived from your machine or account.

At `basic`, the command metrics are a counter (`agent_marketplace.command.invocations`) and a duration histogram in milliseconds (`agent_marketplace.command.duration_ms`). They carry only these attributes:

| Attribute | Value |
|---|---|
| `command.name` | The command path, such as `agent-marketplace sync`. Arguments and flag values are not included. |
| `harnesses` | The target harnesses. |
| `managed` | Whether an organization `telemetry:` block is active. |
| `outcome.code` | `success` or `error`. |

`detailed` adds inventory metrics that describe your effective configuration:

- `agent_marketplace.config.snapshot`: a configuration fingerprint, harnesses, harness scope, and registry and resource counts.
- `agent_marketplace.config.registry`: one point per registry, with its ID, location, path, ref, configuration scope, and whether it is required or default.
- `agent_marketplace.config.resource`: one point per selected resource, with its kind, name, registry details, configuration scope, and whether it is required.
- `agent_marketplace.config.inventory_omissions`: a count of entries left out, with the reason.

Registry locations are cleaned before they are sent. User names, passwords, query strings, and fragments are removed from Git URLs, and any local location (an absolute or relative path, `~`, or a `file:` URL) is replaced with the word `local`.

Detailed also sends traces of the steps inside a command, such as loading config, resolving resources, and materializing output.

## What is not collected

- Command arguments and flag values
- Registry credentials and local registry paths
- Resource contents, descriptions, and tags
- User names, hostnames, prompts, terminal contents, and search text

At the `detailed` level, the names of your registries' Git hosts and repositories, the paths and refs inside them, and the names of selected resources are sent, as described above.

## Diagnostic payloads

`--diagnostic` captures one run for review before export. It needs the effective level `detailed`.

1. The CLI records the run's metrics, traces, and diagnostic events in memory. URL credentials are removed.
2. When the command ends, it writes the payload to `payload-<trace-id>.json` under `$XDG_STATE_HOME/agent-marketplace/telemetry/diagnostic/` (default `~/.local/state/agent-marketplace/telemetry/diagnostic/`) and prints the path, destination host, retention policy, installation ID, and trace ID. Only you can read the file.
3. In a terminal, it asks `Proceed with export? [y/N]`. Outside a terminal it sends nothing: the file stays in place, and export is only possible interactively.
4. If you agree, it sends the payload to the primary endpoint and deletes the file. If delivery succeeds but local deletion fails, it reports the cleanup error and retained path instead of successful completion. If you decline, or delivery fails, the file stays where it is.

The payload can contain sensitive details. Review the file before you agree to send it. Diagnostic payloads are never added to the normal delivery queue. `telemetry purge` or `telemetry reset-id` deletes retained ones.

Diagnostic failures, including cleanup errors, do not change the investigated command's exit status.

## Local files

All telemetry state lives under `$XDG_STATE_HOME/agent-marketplace/telemetry/` (default `~/.local/state/agent-marketplace/telemetry/`):

| Path | Contents |
|---|---|
| `installation-id` | The installation ID. |
| `queue/` | Records that have not been delivered yet. |
| `diagnostic/` | Retained diagnostic payloads. |
| `preview/` | Files written by `telemetry preview`. |

## Related documentation

- [How to configure telemetry](../how-to/configure-telemetry.md)
- [Telemetry and privacy](../explanation/telemetry-and-privacy.md)
- [Environment variables reference](./environment-variables.md)
