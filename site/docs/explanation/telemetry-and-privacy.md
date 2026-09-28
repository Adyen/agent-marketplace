# Telemetry and privacy

Telemetry is usage data the CLI can send so its maintainers, or your organization, can see how it is used and where it fails. `agent-marketplace` sends nothing unless you turn telemetry on. The level is `off` until you or your organization set one. On a machine your organization manages, the organization can set the level and the destination for you, and you cannot override them.

## What each level collects

- `basic` counts command runs and measures how long they take. Each count carries the command name, the configured harnesses, whether the installation is organization-managed, and an outcome code. Every report also names the CLI version, operating system, CPU architecture, and installation ID.
- `detailed` adds a snapshot of the configuration and traces of what each command did. The snapshot includes the location, ref, and subfolder of each remote registry, and the kind and name of each selected resource. Any registry on your disk is reported only as `local`, with no path or ref.
- `diagnostic` is not a level you can store in configuration. You request it for one command with `--diagnostic`, and it writes the metrics, traces, and events from that one command to a local file for you to review before anything is sent.

`agent-marketplace telemetry preview --level <level>` prints a representative payload for a level without sending it.

## What is never collected

The CLI does not send:

- Command arguments.
- Registry credentials or paths on your machine.
- Resource contents, descriptions, or tags.
- User names, hostnames other than a remote registry's, prompts, terminal contents, or search text.

Before a diagnostic payload is written to disk or sent, the CLI removes credentials embedded in URLs (such as `https://user:token@host/...`) from every text field.

## Installation identity

Reports carry an `installation.id`: a random value generated on your machine, not tied to any user account. It lets the receiver link reports from the same installation over time. `agent-marketplace telemetry reset-id` replaces it with a new one and deletes any telemetry still waiting to be sent.

## What an organization can enforce

An organization's policy in `/etc/agent-marketplace/config.yaml` can set the telemetry level, the endpoint that receives the data along with its headers and CA certificate file, and whether community reporting is on. Your user configuration and environment variables cannot override the values the organization sets. `agent-marketplace telemetry status` shows the effective settings and which configuration each one came from.

## Reviewing a diagnostic capture

A `--diagnostic` capture writes a JSON file to `$XDG_STATE_HOME/agent-marketplace/telemetry/diagnostic/` (by default `~/.local/state/agent-marketplace/telemetry/diagnostic/`) and prints its path, the destination hostname, and a warning that the file may contain sensitive information. Read the file before you agree to send it. In a terminal, the CLI asks for confirmation. Without a terminal, it sends nothing unless you also pass `--diagnostic-accept-sensitive-data`. The CLI deletes the file after a successful export. If you decline, or the export fails, the file stays in place, and `agent-marketplace telemetry purge` removes it. See [How to configure telemetry](../how-to/configure-telemetry.md).

## Community reporting

Community reporting is a second, separate stream. When you set `community_reporting` and telemetry is on, the CLI also sends command counts and durations (the `basic` data) to the agent-marketplace project's endpoint, even when your own level is `detailed`. Detailed and diagnostic data never travel through this stream. It has no effect in a build that has no project endpoint configured.

Where your main telemetry goes is a separate choice. The organization's endpoint wins, then your own. If neither sets one, the CLI sends to the project endpoint built into the binary. Some builds, including the published GitHub release binaries, have no built-in endpoint, and with no configured endpoint they send nothing.

## Related documentation

- [Telemetry reference](../reference/telemetry.md)
- [How to configure telemetry](../how-to/configure-telemetry.md)
