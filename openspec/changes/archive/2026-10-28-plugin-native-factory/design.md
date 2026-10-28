## Context

`plugin-native-registration` and `plugin-native-claude` publish native
generations into an Agent Marketplace-owned catalog under the state root and
register it through owned entries. Claude Code and Codex have registration
members; Factory Droid has none, and its catalog declares native delivery
`unverified`.

Factory's plugin documentation describes the registration Droid itself acts
on. A marketplace is a directory with `.factory-plugin/marketplace.json`
(falling back to `.claude-plugin/marketplace.json`); plugin sources inside it
are relative paths that must stay inside the marketplace. Settings register a
marketplace through `extraKnownMarketplaces.<name>.source`, where a local
directory uses `{"source": "local", "path": <absolute path>}`, and enable a
plugin through `enabledPlugins["<plugin>@<name>"]`. Droid installs enabled
plugins automatically during startup at the scope of the settings file that
declares them, copying each plugin into its own cache. `settings.local.json`
merges over `settings.json` at the same level, for both `~/.factory/` and a
project's `.factory/`. The plugin list commands do not trigger that install;
a session start does.

The Factory harness-compatibility job already starts a metadata session with
`droid exec --input-format stream-jsonrpc` and `droid.initialize_session`,
then reads `droid.list_skills`, without credentials or a model request.

## Goals / Non-Goals

**Goals:** a Factory catalog and registration shape that match Factory's
documented marketplace and settings references; repository registration kept
out of shared committed settings; native delivery kept unverified until a
follow-up proves the registration installs plugins; the released-reader
witness.

**Non-Goals:** Claude Code and Codex changes, git-sourced native catalog
entries, user documentation, Droid's cache, installed-plugin records,
organization-managed settings or `strictKnownMarketplaces`, and the
delivery-resolution rules.

## Decisions

### D1: Factory catalog layout

The catalog root is `<state>/native-plugins/factory/<am-catalog>/`.
`.factory-plugin/marketplace.json` carries `name`, `owner: {name: "Agent
Marketplace"}`, and one entry per plugin `{name, source: "./plugins/<name>"}`.
`plugins/<name>` links to the plugin's immutable generation and is maintained
exactly like the Claude catalog's links: replaced atomically under the
packages lock before the manifest commit, and links no entry names are
removed after the commit.

### D2: Registration file per scope

The Factory registration member declares `settings.local.json` for
repository scope and `settings.json` for user scope, under the Factory
partition's `.factory` root. Its entries are
`extraKnownMarketplaces.<am-catalog> = {"source": {"source": "local",
"path": <catalog root>}}`, owned by the scope's catalog, and
`enabledPlugins["<plugin>@<am-catalog>"] = true`, owned by the plugin
instance. Owned-entry reconciliation, collision reporting, and retirement are
unchanged from the other registration members.

### D3: Factory native delivery stays unverified

Factory Droid 0.251.0 recorded the owned marketplace but did not install the
enabled plugin in headless metadata sessions in CI runs 40682001059 and
40698987206. The registration groundwork therefore ships without a Factory
probe or probe identifier, and native delivery remains
`native: {status: unverified}`. `auto` continues to decompose Factory plugins,
explicit `delivery: native` stays refused, and verification moves to a
follow-up.

### D4: Released-reader witness

The released-client gate adds a required scenario like the Claude one:
teammate A's candidate installs a native Factory plugin from an untracked
personal-local configuration through an explicitly verified test catalog;
teammate B's pinned release runs `sync --locked` and `sync --check` after
each candidate step and passes, committed bytes stay identical, and A's
registration and catalog stay unchanged.

## Risks / Trade-offs

- The settings-only registration has not installed the enabled plugin in
  headless CI metadata sessions; the catalog remains unverified until a
  follow-up establishes a supported installation path.
- `.factory/settings.local.json` is machine-local; Agent Marketplace does not
  edit `.gitignore`.
