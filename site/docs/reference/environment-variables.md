# Environment variables reference

## Logging

| Variable | Effect |
|---|---|
| `AGENT_MARKETPLACE_LOG_ENABLED` | Enable or disable logging (`true`/`false`, `1`/`0`, `yes`/`no`, `on`/`off`). |
| `AGENT_MARKETPLACE_LOG_LEVEL` | Set the log level (`debug`, `info`, `warn`, `error`). |
| `AGENT_MARKETPLACE_LOG_FILE` | Override the log file path. `~` and `$VAR` are expanded. |
| `AGENT_MARKETPLACE_LOG_MAX_SIZE_MB` | Rotate when the file exceeds this size in MB. |
| `AGENT_MARKETPLACE_LOG_MAX_BACKUPS` | Number of rotated files to keep (`0` keeps all). |
| `AGENT_MARKETPLACE_LOG_MAX_AGE_DAYS` | Delete rotated files older than this (`0` keeps all). |
| `AGENT_MARKETPLACE_LOG_COMPRESS` | Compress rotated files. |
| `AGENT_MARKETPLACE_DEBUG` | Force `debug` level and mirror logs to standard error, like `--debug`. |

## Telemetry

| Variable | Effect |
|---|---|
| `AGENT_MARKETPLACE_TELEMETRY_LEVEL` | Set the telemetry level (`off`, `basic`, `detailed`). |
| `AGENT_MARKETPLACE_TELEMETRY_ENDPOINT` | Set the OTLP/HTTP endpoint. |
| `AGENT_MARKETPLACE_TELEMETRY_COMMUNITY_REPORTING` | Send the basic schema to the project endpoint (`true`/`false`). |

Fields that an organization policy locks cannot be overridden by these variables. Standard `OTEL_*` variables are ignored.

## Runtime

| Variable | Effect |
|---|---|
| `AGENT_MARKETPLACE_NO_TUI` | A non-empty value disables the TUI and prints help on a bare invocation. |
| `AGENT_MARKETPLACE_OWNERSHIP_LOCK_TIMEOUT` | Override the per-root ownership lock wait (a Go duration such as `60s`; default 30s). `0` fails immediately when another process holds the lock. |
| `EDITOR` | Editor launched by the TUI `E` key. |
| `NO_COLOR`, `CLICOLOR=0`, `TERM=dumb` | Disable colored `sync` output. |

## Paths

| Variable | Effect |
|---|---|
| `HOME` | Base for `~/.agents`, `~/.cache/agent-marketplace`, `~/.agent-marketplace`, and every harness machine root. |
| `XDG_STATE_HOME` | Base for durable ownership state, safety backups, and diagnostic payloads (`$XDG_STATE_HOME/agent-marketplace/`; default `~/.local/state/agent-marketplace/`). Must be absolute to take effect. |
| `CODEX_HOME` | Relocates only the Codex `.codex` machine partition (subagents, hooks, MCP). Must be absolute; a relative value is a diagnostic error. Skills stay under `~/.agents`. |

The cache always lives at `~/.cache/agent-marketplace/`; `XDG_CACHE_HOME` is not consulted.

## Git

Registry acquisition shells out to `git`, so the usual Git environment applies (`GIT_SSH_COMMAND`, `GIT_ASKPASS`, `GIT_CONFIG_NOSYSTEM`, `XDG_CONFIG_HOME/git`). Credentials are never read from `agent-marketplace` configuration.

## Precedence

Environment variables override values from `~/.agents/config.yaml`. Logging is read from the user layer only (see the [logging reference](./logging.md)); telemetry fields locked by organization policy win over both user configuration and environment.

## Related documentation

- [Logging reference](./logging.md)
- [Telemetry reference](./telemetry.md)
- [Filesystem layout reference](./filesystem-layout.md)
