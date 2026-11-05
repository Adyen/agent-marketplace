# How to configure telemetry

Turn telemetry on or off, send it to your own collector, and preview what the CLI would send. For what each level collects, see [telemetry and privacy](../explanation/telemetry-and-privacy.md) and the [telemetry reference](../reference/telemetry.md).

## Prerequisites

- `agent-marketplace` on your `PATH`.
- A user [configuration file](../glossary.md#configuration-file) at `~/.agents/config.yaml`. `telemetry enable` refuses to run without one; create it with `agent-marketplace init`.
- For a personal installation, an OTLP/HTTP endpoint you control. Release binaries have no built-in endpoint, so nothing is sent until you or your organization set one.

## 1. Check the current state

```bash
agent-marketplace telemetry status
```

With nothing configured:

```text
Telemetry status
  Effective level:       off (user)
  Primary endpoint:      none
  Primary headers:       none
  Retention policy:      none
  Community reporting:   false
  Managed installation:  false
  Installation ID:       not set
  Pending records:       0 (oldest none, 0 B)
  Diagnostic payloads:   0
  Last delivery failure: none
```

The word in parentheses shows where a value came from: `user` (your config or environment) or `org` (an organization policy). On a managed installation, `Managed installation` is `true` and a `Locked by organization:` line lists the fields the policy controls.

## 2. Enable telemetry with your own endpoint

```bash
agent-marketplace telemetry enable \
  --level detailed \
  --endpoint https://otel.example.com
```

The command prints `Telemetry enabled at "detailed" with endpoint "https://otel.example.com"` and saves both values to `~/.agents/config.yaml`. The level is `basic` or `detailed`.

Give a base URL without a path, and the CLI sends metrics to `/v1/metrics` and traces to `/v1/traces`. A URL with a path is used unchanged for both. A user endpoint must use HTTPS or a loopback address such as `http://localhost:4318`. If the endpoint is rejected, the new level is already saved; check with `agent-marketplace telemetry status`.

To change only the level and keep the configured endpoint, omit `--endpoint`:

```bash
agent-marketplace telemetry enable --level basic
```

It prints `Telemetry enabled at "basic"`.

## 3. Disable telemetry

```bash
agent-marketplace telemetry disable
```

This sets your level to `off` and prints `Telemetry disabled for user preference.` If an organization policy sets a level, telemetry stays on at that level and the command also prints `Organization policy keeps telemetry active at "<level>".`

## 4. Override settings for one command

Environment variables apply to one run and are not saved:

```bash
AGENT_MARKETPLACE_TELEMETRY_LEVEL=basic \
AGENT_MARKETPLACE_TELEMETRY_ENDPOINT=https://otel.example.com \
  agent-marketplace sync
```

The run prints the usual `Installed ... Removed ...` summary and exits `0`. `AGENT_MARKETPLACE_TELEMETRY_COMMUNITY_REPORTING=true` turns on community reporting for the run. It sends nothing unless the binary was built with a project endpoint and the level is not `off`, so it does nothing on release binaries.

The CLI ignores the standard `OTEL_*` variables and logs a warning for each one it finds.

## 5. Preview a payload

```bash
agent-marketplace telemetry preview --level detailed
```

This writes a sample JSON payload under the state directory (default `~/.local/state/agent-marketplace/telemetry/preview/`) and prints its path with the endpoint host, retention, and headers that would apply. Nothing is sent:

```text
Preview payload written to /home/you/.local/state/agent-marketplace/telemetry/preview/<file>.json
  level:         detailed
  endpoint host: otel.example.com
  retention:     none
  headers:       none
```

The level is `basic`, `detailed`, or `diagnostic`. A `diagnostic` preview may contain sensitive data, so the command asks for confirmation. In a non-interactive shell the command is refused, so run it from a terminal.

## 6. Clear local telemetry data

- `agent-marketplace telemetry purge` deletes undelivered records and retained diagnostic payloads, and prints `Purged <n> queued record(s) and <n> diagnostic payload(s).`
- `agent-marketplace telemetry reset-id` creates a new installation ID, purges pending telemetry, and prints `Installation ID reset. New ID: <id>`

## Organization policy

A policy in `/etc/agent-marketplace/config.yaml` overrides your settings:

- A policy level is always the effective level. `telemetry enable` refuses a lower level with `organization policy requires telemetry level "<level>" or higher`.
- A policy endpoint makes `telemetry enable --endpoint` fail with `endpoint is locked by organization policy`.
- Any policy `telemetry` block makes the CLI ignore `AGENT_MARKETPLACE_TELEMETRY_LEVEL`. `AGENT_MARKETPLACE_TELEMETRY_ENDPOINT` and `AGENT_MARKETPLACE_TELEMETRY_COMMUNITY_REPORTING` are ignored only when the policy sets the matching field.

## Troubleshooting

- `user config not found at "..."; run 'agent-marketplace init' first`: run `agent-marketplace init`, then `telemetry enable` again.
- `telemetry status` shows `Locked by organization:`: the policy controls those fields. Ask the owner of `/etc/agent-marketplace/config.yaml`.
- Nothing is sent: check that `telemetry status` shows a level other than `off` and an endpoint other than `none`. Check the endpoint path (step 2) and any `ca_file` in your telemetry config. `Last delivery failure` shows the latest error; `--debug` prints telemetry setup logs.
