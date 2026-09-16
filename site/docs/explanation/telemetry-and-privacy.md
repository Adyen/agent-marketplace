# Telemetry and privacy

`agent-marketplace` telemetry is opt-in for unmanaged installations. Organization-managed installations can lock the level and endpoint.

## What is collected

Telemetry exports are grouped by level:

- `basic`: command invocation counts and durations.
- `detailed`: basic metrics plus configuration inventory gauges and lifecycle traces.
- `diagnostic`: a local review payload of metrics, traces, and events from one invocation.

## What is never collected

The CLI does not send:

- Command arguments.
- Registry credentials or local paths.
- Resource contents, descriptions, or tags.
- File paths other than the diagnostic payload path.
- User names, hostnames unrelated to a remote registry, prompts, terminal contents, or search text.

URL credentials are stripped from every string attribute before a diagnostic payload is written or exported.

## Installation identity

The `installation.id` is a random, resettable pseudonym. It links reports over time but is not tied to a user account.

## Organization lock

An organization policy can set a minimum level, endpoint, headers, and CA file. These fields cannot be overridden by user configuration or environment variables.

## Diagnostic review

A `--diagnostic` capture writes a JSON payload to `$XDG_STATE_HOME/agent-marketplace/diagnostic/` (default `~/.local/state/agent-marketplace/diagnostic/`). The CLI retains the payload until a successful export; a failed export leaves it in place for inspection. See [How to configure telemetry](../how-to/configure-telemetry.md).

## Community reporting

Enabling `community_reporting` sends only the `basic` schema to the project endpoint. Detailed and diagnostic data do not flow there.

## Related documentation

- [Telemetry reference](../reference/telemetry.md)
- [How to configure telemetry](../how-to/configure-telemetry.md)
