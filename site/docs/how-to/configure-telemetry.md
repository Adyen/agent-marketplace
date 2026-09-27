# How to configure telemetry

This guide shows how to turn telemetry on and off, send it to your own collector, and check what the CLI would send. It covers personal installations and installations managed by an organization policy. For what each level collects, see [telemetry and privacy](../explanation/telemetry-and-privacy.md) and the [telemetry reference](../reference/telemetry.md).

## Prerequisites

- `agent-marketplace` on your `PATH`.
- A user [configuration file](../glossary.md#configuration-file) at `~/.agents/config.yaml`. If you do not have one, run `agent-marketplace init` first. `telemetry enable` refuses to run without it.
- For a personal installation, an OTLP/HTTP endpoint that you control. Release binaries have no built-in project endpoint, so telemetry has nowhere to go until you or your organization set one.

## 1. Check the current state

```bash
agent-marketplace telemetry status
```

On a personal installation with nothing configured, the output looks like this:

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

The word in parentheses after the level and endpoint shows where the value came from: `user` for your config or environment, `org` for an organization policy. On a managed installation, `Managed installation` is `true` and an extra line, `Locked by organization:`, lists the fields that the policy controls.

## 2. Enable telemetry with your own endpoint

Set the level and the endpoint in one step:

```bash
agent-marketplace telemetry enable \
  --level detailed \
  --endpoint https://otel.example.com
```

The command prints `Telemetry enabled at "detailed" with endpoint "https://otel.example.com"` and writes both values to `~/.agents/config.yaml`. The level can be `basic` or `detailed`.

Give the endpoint as a base URL without a path. The CLI then sends metrics to `/v1/metrics` and traces to `/v1/traces` on that host. If the URL has a path, the CLI uses that path unchanged for both. A user endpoint must use HTTPS, or point at a loopback address such as `http://localhost:4318`. If the command rejects the endpoint, it has already saved the new level, so run `agent-marketplace telemetry status` to see what is in effect.

To change only the level and keep an endpoint that is already configured, leave out `--endpoint`:

```bash
agent-marketplace telemetry enable --level basic
```

## 3. Disable telemetry

```bash
agent-marketplace telemetry disable
```

This sets your user level to `off` and prints `Telemetry disabled for user preference.` If an organization policy sets a level, telemetry stays on at that level, and the command also prints `Organization policy keeps telemetry active at "<level>".`

## 4. Override settings for one command

Environment variables change the settings for a single run without editing your config:

```bash
AGENT_MARKETPLACE_TELEMETRY_LEVEL=basic \
AGENT_MARKETPLACE_TELEMETRY_ENDPOINT=https://otel.example.com \
  agent-marketplace sync
```

`AGENT_MARKETPLACE_TELEMETRY_COMMUNITY_REPORTING=true` turns on community reporting for the run. Community reporting only sends anything when the binary was built with a project endpoint and the effective level is not `off`, so it has no effect on release binaries.

The CLI ignores the standard `OTEL_*` environment variables when it works out telemetry settings, and logs a warning for each one it finds.

## 5. Preview a payload

```bash
agent-marketplace telemetry preview --level detailed
```

This writes a representative JSON payload to a file under the state directory (by default `~/.local/state/agent-marketplace/telemetry/preview/`) and prints its path, together with the endpoint host, retention policy, and headers that would apply. Nothing is sent.

The level can be `basic`, `detailed`, or `diagnostic`. A `diagnostic` preview may include sensitive data, so the command asks for confirmation. In a non-interactive shell, add `--diagnostic-accept-sensitive-data` instead.

## 6. Clear local telemetry data

Two commands remove local data:

- `agent-marketplace telemetry purge` removes queued records that have not been delivered yet and any retained diagnostic payloads.
- `agent-marketplace telemetry reset-id` creates a new installation ID and purges pending telemetry.

## Organization policy

An organization policy in `/etc/agent-marketplace/config.yaml` takes priority over your settings:

- If the policy sets a level, that level is the effective level whatever your config says. `telemetry enable` also refuses a level below it with `organization policy requires telemetry level "<level>" or higher`.
- If the policy sets an endpoint, `telemetry enable --endpoint` fails with `endpoint is locked by organization policy`.
- When the policy has a `telemetry` block at all, `AGENT_MARKETPLACE_TELEMETRY_LEVEL` is ignored. `AGENT_MARKETPLACE_TELEMETRY_ENDPOINT` and `AGENT_MARKETPLACE_TELEMETRY_COMMUNITY_REPORTING` are ignored only when the policy sets the matching field.

## Troubleshooting

- `user config not found at "..."; run 'agent-marketplace init' first`: Run `agent-marketplace init` to create the user config, then run `telemetry enable` again.
- `telemetry status` shows `Locked by organization:`: An organization policy controls the listed fields. Ask the owner of `/etc/agent-marketplace/config.yaml` to change them.
- Nothing is being sent: Check that `telemetry status` shows an effective level other than `off` and a primary endpoint other than `none`. Check the endpoint path (see step 2) and any `ca_file` in your telemetry config. `Last delivery failure` shows the most recent error, and `--debug` prints telemetry setup logs.
