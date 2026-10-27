## Context

`plugin-native-registration` publishes native generations into an Agent
Marketplace-owned catalog under the state root and writes owned
`extraKnownMarketplaces` and `enabledPlugins` entries into the Claude
partition's `settings.json`. Its catalog lists plugins with
`{"source": "directory", "path": <absolute generation>}` and no `owner`.
Claude Code's marketplace reference requires `name`, `owner`, and `plugins`,
and its plugin sources do not include `directory`; a plugin listed by a path
relative to the marketplace root loads from the marketplace in place once
`extraKnownMarketplaces` applies. Claude honors repository
`extraKnownMarketplaces` in `.claude/settings.json` or
`.claude/settings.local.json` only in a trusted workspace. The pinned
released v4 client refuses any plugin configuration, so released readers only
ever see a repository's committed state.

## Goals / Non-Goals

**Goals:** a Claude registration the real CLI loads, proven by an isolated
probe; repository registration kept out of shared committed settings; a
one-sync move from the earlier file; the shipped Claude catalog flipped to
verified; the released-reader witness.

**Non-Goals:** Factory Droid and Codex native delivery, git-sourced native
catalog entries, user documentation, Claude trust, policy, or plugin caches,
and the delivery-resolution rules.

## Decisions

### D1: Claude catalog layout

The catalog root stays `<state>/native-plugins/claude/<am-catalog>/`.
`.claude-plugin/marketplace.json` carries `name`, `owner: {name: "Agent
Marketplace"}`, and one entry per plugin `{name, source: "./plugins/<name>"}`.
`plugins/<name>` is a symbolic link to the plugin's immutable generation
directory, replaced atomically (temporary link, then rename) under the
packages lock before the manifest commit, like generation publishing; links
no entry names are removed after the commit. A crash leaves at most an
unreferenced link, removed by the next sync. If the probe shows Claude does
not load a linked plugin directory, the fallback is copying the generation
into `plugins/<name>`; that is an execution-plan change, not a contract
change.

### D2: Registration file per scope

The Claude registration member declares a project file and a user file, like
native aggregates: `settings.local.json` for repository scope and
`settings.json` for user scope. The Codex member keeps `config.toml` for
both. Owned-entry reconciliation plans the registration file together with
hooks and MCP entries from one read, unchanged.

### D3: Move from the earlier file

Ownership state records each owned registration entry with its file. When
the desired file for an owned entry differs from the recorded one, the same
sync writes it to the new file and retires the recorded entry from the old
file, in one dependency transaction. Entries without ownership state stay
where they are and are reported as collisions.

### D4: Probe

`tools/harness-compat` defines `claude-native-plugin`. The Claude harness
compatibility driver installs a `claude-plugin` fixture with
`delivery: native` at user scope and at repository scope (the repository
workspace trusted by the driver's existing disposable trust fixture). Before
sync, `claude plugin list` and the initialize inventory must not show the
plugin. Claude loads plugins at session start only from its own installed
records and cache and fetches settings-declared marketplaces in the
background after a session starts, so after sync the driver starts one
priming session and waits for it to exit, then requires a second session's
`claude plugin list` to report `<plugin>@<am-catalog>` enabled and its
initialize inventory to list the plugin's namespaced component. The probe
records `claude plugin list` output in its report. Agent Marketplace writes
none of the records or cache Claude creates while priming. Because a new
generation reaches a running session only after a restart, the Claude
catalog declares `refresh: restart-required`. `claude plugin validate` on the
catalog is recorded as supplementary evidence when it runs offline; it is not
a gate.

### D5: Released-reader witness

The released-client gate adds a required scenario: teammate A's candidate
installs a native Claude plugin from an untracked personal-local
configuration in a repository whose committed configuration has locked
resources and owned hooks; teammate B's pinned release runs `sync --locked`
and `sync --check` after each candidate step and passes, committed bytes stay
identical, and A's registration and catalog stay unchanged.

## Risks / Trade-offs

- The probe runs only in CI images; local runs cannot certify it. A probe
  that cannot pass stops the change and escalates the release decision.
- `.claude/settings.local.json` is machine-local; Agent Marketplace does not
  edit `.gitignore`.
