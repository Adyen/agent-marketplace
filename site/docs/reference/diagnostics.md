# Diagnostics reference

A [diagnostic](../glossary.md#diagnostic) is a structured message that `agent-marketplace` reports when something about a resource, a file, or a run needs your attention. This page explains the parts of a diagnostic, what each severity means, and how to find the code you are looking at. Use it when a command prints a code such as `lock.stale` or `rule.devin.unsupported` and you want to know which area it comes from.

## Diagnostic structure

Diagnostics produced while converting and writing resources have these fields:

- `code`: a stable, dot-separated identifier, such as `subagent.windsurf.no-primitive`. Scripts and tests should match on the code, never on the message text.
- `severity`: `error`, `warning`, or `info`.
- `target`: the harness the diagnostic applies to, or empty when it applies to every harness.
- `resource`: the name of the resource that produced it.
- `message`: a readable description of the condition.
- `hint`: an optional suggestion for what to do next.
- `source`: the original file, relative to the registry or bundle root, and the line number when known.

Configuration, lock, and dependency errors also start their message with a stable code, for example `lock.stale: repo lock "<repo>/.agents/lock.yaml": lock does not match configuration`.

## Severity semantics

- `error`: the source is malformed or unsafe. The affected resource or file is not written, or the run cannot continue in that scope.
- `warning`: the run continues, but the resource was translated with some loss or was dropped.
- `info`: the translation succeeded with a change worth noting.

## Common prefixes

The first part of a code names the area that reported it.

| Prefix | Topic |
|---|---|
| `ingest.*` | Reading a native harness file into the internal resource model. |
| `compile.*` | Writing the internal resource model out for a target harness. |
| `skill.*`, `subagent.*`, `hook.*`, `mcp.*` | Translation issues for one resource kind. |
| `rule.*` | Harness rule files that a target does not support, such as `rule.devin.unsupported`. |
| `bundle.*` | Conflicts between resources in one bundle, such as two resources writing the same path. |
| `json.*` | Malformed or duplicate-key JSON in a harness file. |
| `materialize.*` | Writing generated files into harness folders. |
| `ownership.*` | Ownership records and locking for Devin and Codex shared configuration files. |
| `doctor.*` | Findings from `agent-marketplace doctor` and `doctor --repair`. |
| `config.*` | Loading and editing configuration files, such as `config.edit.include-owned`. |
| `v4.*` | Consumer configuration validation, such as `v4.api-version-required` or `v4.api-version`. |
| `lock.*` | Lockfile state, such as `lock.stale` or `lock.snapshot-unavailable`. |
| `dependency.*` | Recovery of an interrupted config and lock update, such as `dependency.recovery-required`. |
| `git.*` | Git acquisition and authentication, such as `git.auth-config-untrusted`. |
| `catalog.*`, `resolver.*` | Discovering resources in registries and choosing between them, such as `resolver.name-collision`. |
| `marketplace.*` | Hosted Marketplace security findings, policy, and ingestion, plus consumer install and remove refusals such as `marketplace.remove-not-selected`. |

## Consumer format refusals

- `v4.api-version-required`: an existing consumer root omits `apiVersion`, including a personal-local or organization root.
- `v4.api-version`: a root or explicitly versioned included fragment has an unsupported or invalid version. Only the exact string `agent-marketplace/v4` is accepted; fragments under a validated v4 root may omit the field.
- `v4.legacy-key`: a v4-validated root or included fragment still contains legacy installation keys. Changing only the version string is not conversion.

Format diagnostics identify the offending file and `apiVersion` field (or legacy key), with line context when available. `v4.api-version-required` and `v4.api-version` also print a next step: for a user, repository, or personal-local layer the message names the older released migrator (agent-marketplace v4.1.4), its `agent-marketplace migrate --to v4 --scope <user|repo|local> --dry-run` preview command, and the conversion guide; for organization configuration it says to ask the administrator and links the same guide. [Convert before upgrading](../how-to/migrate-configuration.md) with an older released migrator; the shipped CLI and TUI never offer conversion. A refused new operation does not acquire resources or change config, locks, or harness output. Existing pending-journal recovery and ordinary logs are separate: read-only modes report `dependency.recovery-required` without replay, while recovery-capable commands can finish previously admitted writes before validating fresh input.

## What to do

Every error a CLI command prints is followed by at least one `hint: ` line naming a supported command or a concrete next action. `doctor` prints the same `hint:` line under each error in its configuration and organization error sections, and the TUI errors view shows the same guidance before its own category copy. An error that carries its own remediation uses that text; otherwise the hint comes from the stable code the error carries; otherwise it names a diagnostic command suited to the command you ran (for example `agent-marketplace doctor`, `<command> --check`, or `<command> --help` for usage errors). Hints never appear in `--json` output and never change the exit status.

Where each prefix's hints send you:

| Prefix | What the hint tells you to do |
|---|---|
| `v4.*` | Fix the named configuration file and line, then run `agent-marketplace doctor`. `v4.api-version-required` and `v4.api-version` instead name the older released migrator (or your administrator for organization configuration) and link the [conversion guide](../how-to/migrate-configuration.md). `v4.marketplace-lock-required` and `v4.marketplace-lock-mismatch` name a writing `agent-marketplace sync` run while online to record the selected resource in the lock, then commit it. |
| `lock.*` | Restore or reconcile the named lock, then run `agent-marketplace sync --check`. Stale, missing, or outdated locks point at `agent-marketplace lock` or a writing `sync`; verification failures warn you not to delete the lock or bypass the check. |
| `config.edit.*` | Adjust the named selection or rerun the edit as the hint describes, then run `agent-marketplace doctor`. |
| `dependency.*` | Run a writing `agent-marketplace sync` or `lock` while online to finish an interrupted write. |
| `ownership.*` | Retry after the other process finishes, run `agent-marketplace doctor --repair`, or fix the named cause, as the hint says. |
| `organization.trust.*` | Ask your administrator. Hints for organization-owned configuration, locks, and trust never suggest editing or deleting organization files yourself. |
| `git.*` | Check credentials, your SSH agent, or move authentication settings to trusted Git configuration, then rerun the command. |
| `marketplace.*` install/remove refusals | Run `agent-marketplace list` to confirm names, use `remove --scope` or `remove --from` to disambiguate, or ask your administrator for organization-owned selections. |
| no code | A command-aware fallback: the command's own `--check` mode for sync and lock, `doctor --debug` for doctor, `doctor` (and the command's `--help` for install, remove, and update) for the rest, or `<command> --help` for usage errors. |

## Viewing diagnostics

- `agent-marketplace sync` prints errors and warnings. `-v` adds affected files and malformed-resource details.
- `agent-marketplace status` prints warnings and errors in its `WARNINGS` and `ERRORS` sections.
- `agent-marketplace doctor` prints ownership and compatibility diagnostics.
- In the TUI, `e` opens the diagnostics view.

## Complete catalog

The full list of harness-compilation diagnostic codes, with their severities and examples, is in the source tree at [`pkg/harness-compilation/diag/codes.md`](https://github.com/Adyen/agent-marketplace/blob/main/pkg/harness-compilation/diag/codes.md).

## Related documentation

- [How to troubleshoot sync drift](../how-to/troubleshoot-sync-drift.md)
- [How to repair owned shared configuration](../how-to/repair-owned-shared-configuration.md)
