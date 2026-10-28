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
out of shared committed settings; an isolated probe that decides Factory's
native status; the released-reader witness.

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

### D3: Native probe decides the status

The probe `factory-native-plugin` runs in the Factory harness-compatibility
job with an isolated home, at user and repository scope. After `sync` it
starts a metadata session and requires the plugin's skill in
`droid.list_skills` with the generation's exact content. It then changes the
generation without changing the authored version and requires the new content
after the next sync and session, or the catalog declares that refresh needs a
restart and status reports `reload-required`. It requires that no dependency
was installed implicitly, and that removing the plugin and syncing removes the
skill from the next session's inventory. The probe sends no user message and
no model request, and a failing or blocked probe is never replaced by a
file-only check. Only a passing probe flips the Factory catalog to
`native: {status: verified}` with the probe identifier; otherwise the catalog
stays `unverified` and the v5.0.0 release decision is escalated.

### D4: Released-reader witness

The released-client gate adds a required scenario like the Claude one:
teammate A's candidate installs a native Factory plugin from an untracked
personal-local configuration through an explicitly verified test catalog;
teammate B's pinned release runs `sync --locked` and `sync --check` after
each candidate step and passes, committed bytes stay identical, and A's
registration and catalog stay unchanged.

## Risks / Trade-offs

- Droid may skip re-checking a recently checked marketplace for up to six
  hours, so a changed generation may not refresh in a new session; the probe
  decides between verified refresh, `reload-required`, and staying
  unverified.
- Droid rejects plugin sources that escape the marketplace; whether it
  follows a linked generation while copying into its cache is settled by the
  probe.
- `.factory/settings.local.json` is machine-local; Agent Marketplace does not
  edit `.gitignore`.
