# Environment variables reference

This page lists the environment variables that `agent-marketplace` reads and what each one changes. You only need them to override a setting for one run, in CI, or on a machine where you cannot edit the config file. To set one for a single command, put it in front of the command, for example `AGENT_MARKETPLACE_LOG_LEVEL=debug agent-marketplace sync`.

## Logging

| Variable | Effect |
|---|---|
| `AGENT_MARKETPLACE_LOG_ENABLED` | Turns file logging on or off. Accepts `true`/`false`, `1`/`0`, `yes`/`no`, or `on`/`off`. Other values are ignored. |
| `AGENT_MARKETPLACE_LOG_LEVEL` | Sets the log level: `debug`, `info`, `warn` (or `warning`), or `error`. Any other value means `info`. |
| `AGENT_MARKETPLACE_LOG_FILE` | Sets the log file path. `~` and `$VAR` are expanded. |
| `AGENT_MARKETPLACE_LOG_MAX_SIZE_MB` | Rotates the file when it grows past this many megabytes. Must be a positive integer; other values are ignored. |
| `AGENT_MARKETPLACE_LOG_MAX_BACKUPS` | Number of rotated files to keep. `0` keeps all of them. Negative or non-integer values are ignored. |
| `AGENT_MARKETPLACE_LOG_MAX_AGE_DAYS` | Deletes rotated files older than this many days. `0` keeps all of them. Negative or non-integer values are ignored. |
| `AGENT_MARKETPLACE_LOG_COMPRESS` | Compresses rotated files. Accepts the same values as `AGENT_MARKETPLACE_LOG_ENABLED`. |
| `AGENT_MARKETPLACE_DEBUG` | When true, forces the `debug` level and mirrors logs to standard error, like `--debug`. Accepts the same values as `AGENT_MARKETPLACE_LOG_ENABLED`. |

See the [logging reference](./logging.md) for defaults and the config file equivalents.

## Telemetry

| Variable | Effect |
|---|---|
| `AGENT_MARKETPLACE_TELEMETRY_LEVEL` | Sets the telemetry level: `off`, `basic`, or `detailed`. `diagnostic` is rejected because it is a per-run flag (`--diagnostic`). Ignored when the organization config has a `telemetry` block. |
| `AGENT_MARKETPLACE_TELEMETRY_ENDPOINT` | Sets the OTLP/HTTP endpoint. Ignored when organization policy sets an endpoint. An endpoint that differs from the one in your user config drops the user config's `headers`. |
| `AGENT_MARKETPLACE_TELEMETRY_COMMUNITY_REPORTING` | Sends the basic schema to the project endpoint (`true` or `false`, or another value Go's `strconv.ParseBool` accepts, such as `1` or `0`). Ignored when organization policy sets `community_reporting`. |

An invalid value makes telemetry resolution fail; `telemetry` subcommands then exit with an error, and other commands log a warning and run without telemetry. Standard `OTEL_*` variables are ignored; the CLI logs a warning when it sees one. See the [telemetry reference](./telemetry.md).

## Runtime

| Variable | Effect |
|---|---|
| `AGENT_MARKETPLACE_NO_TUI` | Any non-empty value stops a bare `agent-marketplace` from starting the TUI; it prints help instead. |
| `AGENT_MARKETPLACE_OWNERSHIP_LOCK_TIMEOUT` | How long a command waits for another process that is updating the same Devin or Codex shared configuration files. Takes a Go duration such as `60s` or `500ms`; the default is `30s`. `0` fails immediately when another process holds the lock. A negative or unparseable value is an error. |
| `EDITOR` | Editor the TUI `E` key opens. It may include arguments, for example `code --wait`. Defaults to `vi` when unset. |
| `NO_COLOR`, `CLICOLOR=0`, `TERM=dumb` | Turn off colored `sync` and `install` output. `NO_COLOR` works when set to any value, even an empty one. |

## Paths

| Variable | Effect |
|---|---|
| `HOME` | Base for `~/.agents` (user config and lock), `~/.cache/agent-marketplace` (cache), `~/.agent-marketplace` (logs, backups, and materialization manifest), and every harness machine root. |
| `XDG_STATE_HOME` | Base for durable state under `$XDG_STATE_HOME/agent-marketplace/`: ownership records and safety backups, dependency-transaction records (`dependencies/`), and telemetry state (`telemetry/`, which holds the installation ID, queue, and diagnostic payloads). Defaults to `~/.local/state/agent-marketplace/`. Set it to an absolute path. Ownership and telemetry state ignore a relative value and use the default, but dependency transactions refuse it with the error `XDG_STATE_HOME must be absolute`. |
| `CODEX_HOME` | Moves only the Codex `.codex` machine folder (subagents, hooks, and MCP servers). Must be absolute; a relative value is an error. Codex skills stay under `~/.agents`. |

The cache is always `~/.cache/agent-marketplace/`; `XDG_CACHE_HOME` is not used. The user config is always `~/.agents/config.yaml` and the organization config is always `/etc/agent-marketplace/config.yaml`; no variable moves them. See the [filesystem layout reference](./filesystem-layout.md).

## Git

Registry acquisition runs the `git` program with a reduced environment:

- Of the `GIT_*` variables, only `GIT_SSH`, `GIT_SSH_COMMAND`, `GIT_SSL_CERT`, `GIT_SSL_KEY`, `GIT_SSL_CAINFO`, `GIT_SSL_CAPATH`, `GIT_ASKPASS`, `GIT_HTTP_PROXY`, `GIT_HTTPS_PROXY`, and `GIT_NO_PROXY` are passed through. All other `GIT_*` variables are removed. Variables that do not start with `GIT_`, such as `SSH_AUTH_SOCK` and `HTTPS_PROXY`, pass through unchanged.
- The CLI sets `GIT_TERMINAL_PROMPT=0`, so Git never stops to ask for a password. It sets `GIT_ASKPASS=echo` when you have not set `GIT_ASKPASS`.
- It sets `GIT_CONFIG_GLOBAL=/dev/null` and `GIT_CONFIG_NOSYSTEM=1`, so your Git config files are not applied as a whole.
- For HTTP(S) remotes, it copies only the authentication and redirect settings (`credential.helper`, `credential.username`, `credential.useHttpPath`, `http.sslCAInfo`, `http.sslCAPath`, `http.sslCert`, `http.sslKey`, `http.sslCertPasswordProtected`, `http.extraHeader`, and `http.followRedirects`, including URL-specific forms) from your system and global Git config and their ordinary `[include]` chains. The global config is the file named by `GIT_CONFIG_GLOBAL`, or `$XDG_CONFIG_HOME/git/config` (default `~/.config/git/config`) and `~/.gitconfig`. A true `GIT_CONFIG_NOSYSTEM` skips the system config. Every opened file must be regular, singly linked, owned by you, root, or the account that owns the `git` executable on `PATH` (found through an absolute `PATH` entry, followed through symlinks, and only when that executable is not writable by group or others), and not writable by group or others; trusted symlink targets are supported. Relative include paths resolve beside the declaring file and `~/` uses `HOME`. Conditional `[includeIf]`, unsafe files, malformed configuration, cycles, more than ten include levels or 128 expanded file visits, files over 4 MiB, and expanded content over 16 MiB cause `git.auth-config-untrusted` before transport runs. Missing optional files and `/dev/null` includes contribute no settings.

Credentials are never read from `agent-marketplace` configuration.

## Precedence

Environment variables override the matching values in `~/.agents/config.yaml`. Logging reads only the user config (see the [logging reference](./logging.md)). For telemetry, organization policy wins over both the user config and these variables, as described in the table above.

## Related documentation

- [Logging reference](./logging.md)
- [Telemetry reference](./telemetry.md)
- [Filesystem layout reference](./filesystem-layout.md)
