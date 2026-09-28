# Devin CLI validation

[Shared validation contract](README.md)

## Native installation contract

Devin uses:

- Project root: `<repo>/.devin`
- User root: `~/.config/devin`
- Skills and converted commands: `skills/<name>/SKILL.md`
- Subagents: `agents/<name>.md`
- Project hooks: `hooks.v1.json`
- User hooks: the `hooks` member of `config.json`
- MCP: `mcp_config.json`

Each native hook event holds matcher groups, and each group has a nested
`hooks` array. Legacy flat hook input is still ingestible. Sync upgrades only
entries that durable ownership evidence supports, and preserves handwritten
groups and settings.

## Native probe in CI

The CI image installs a pinned Devin bundle after checking its SHA256
checksum, and the driver sets `auto_update: false` in the isolated
`~/.config/devin/config.json`. The probe runs:

```text
devin --version
devin skills list
devin skills show <name>
devin mcp list
devin mcp get <name>
```

The probe inspects skills and converted commands natively by name,
description, and source path, and MCP entries by name and configured fields.
The offline probe makes no claim about MCP transport.

## Configuration-only coverage

- Hooks: Devin documents `/hooks` as an in-session inventory, and there is no
  documented standalone hook-listing subcommand the credential-free driver
  could use. SDK-provided hooks, or parsing user settings, would not prove
  that Devin discovered the project hook file.
- Subagents: `run_subagent` uses custom profiles, and the inspected CLI
  reference exposes no standalone profile-definition inventory. Starting an
  agent is not a metadata-only check.
- Rules: `devin rules list/show` exists, but this compiler does not emit Devin
  rules by design. The suite tests the explicit omission diagnostic, because a
  negative-only rule check would not prove support.
- Collections: virtual Marketplace selections, not native resources.

The ACP interface was also considered. It documents credential acquisition
and a session command palette, but no credential-free hook or profile
inventory. Hooks and subagents therefore stay covered by generated-file and
compiler/ingester contracts, and they are not certified as natively invoked.

References: [Devin command reference](https://docs.devin.ai/cli/reference/commands), [hook verification](https://docs.devin.ai/cli/extensibility/hooks/overview).

## Local runtime probe suite

`make devin-probe` runs a separate, opt-in probe suite in
[`internal/devinprobe`](../../internal/devinprobe/doc.go) against a `devin`
executable on your `PATH`. The target sets `AGENT_MARKETPLACE_DEVIN_PROBES=1`.
Without that variable every probe skips, so `go test ./...` and CI never
depend on a Devin installation.

Each probe runs in a fresh temporary HOME with its XDG roots and a throwaway
Git repository inside it. It passes `devin` an allowlisted environment, and a
probe fails instead of running if its sandbox HOME is the real home or
contains the real `~/.config/devin`. It uses
only read-only commands: `--version`, `skills paths|list|show`,
`rules paths|list`, and `mcp list|get`.

The project and user skill-directory symlink probes are release blockers. A
failure prints `RELEASE BLOCKER` and exits nonzero. Set `DEVIN_PROBE_REQUIRE=1`
in a release check so that a missing Devin installation fails instead of
skipping.

Every run also prints the probes the suite cannot execute (`UnrunProbes` in
`unrun.go`): per-event hook loading, hook-comment acceptance, user hooks in
`config.json`, MCP `${env:VAR}` interpolation, and parts of subagent
frontmatter handling. All of them need an authenticated interactive session,
and none is release-gating. Observations from a manual authenticated run are
recorded in `manual.go`.

## Safety requirements

The runtime probe must use an isolated HOME, XDG roots, repository, and state
directory. It must never authenticate, invoke a model, change a real Devin
configuration, or execute an external MCP service. A malformed aggregate file
must be refused without changes and must not damage unrelated aggregate
files.
