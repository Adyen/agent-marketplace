## Context

`plugin-native-registration` publishes native generations into an Agent
Marketplace-owned catalog under the state root and registers it through
owned entries. The Codex registration member writes `config.toml` (repository
`.codex/config.toml`, user Codex home), and the Codex catalog writes
`.agents/plugins/marketplace.json`. Codex's catalog declares native delivery
`unverified`.

Codex documents a configured marketplace as
`marketplaces.<name>.source_type` (`git` or `local`) and
`marketplaces.<name>.source` (an absolute local marketplace root containing
`.agents/plugins/marketplace.json`), and a plugin's enabled state as
`plugins."<plugin>@<marketplace>".enabled`. Marketplace entries point
`source.path` at the plugin with a `./`-prefixed path relative to the
marketplace root and carry `policy.installation`, `policy.authentication`,
and `category`. Codex installs a plugin into
`~/.codex/plugins/cache/<marketplace>/<plugin>/<version>/` and loads it
from there. Codex reads a project's `.codex/config.toml` only for trusted
projects.

A local probe with Codex CLI 0.146.1, in an isolated `HOME` and
`CODEX_HOME` without credentials or a model request, observed:

- `source = "local"` with `path` fails to load ("marketplace root does not
  contain a supported manifest").
- Entries with absolute `{"source": "directory"}` sources are not listed;
  relative `local` sources are, including through a symlinked plugin
  directory.
- With the documented shape, `codex plugin list` shows the plugin as
  `not installed`, app-server `skills/list` omits its skill, and
  `INSTALLED_BY_DEFAULT` changes neither. `codex plugin marketplace upgrade`
  applies only to Git marketplaces. Only `codex plugin add` populates the
  cache, after which the skill is listed.

## Goals / Non-Goals

**Goals:** a Codex catalog and registration Codex accepts; a CI probe that
guards that shape; native delivery kept unverified until a follow-up finds a
Codex-owned headless installation path; the released-reader witness.

**Non-Goals:** running `codex plugin add`, Codex's plugin cache, installed
state, or trust; Claude Code and Factory Droid changes; moving the
repository-scope registration out of `.codex/config.toml`; user
documentation; git-sourced native catalog entries.

## Decisions

### D1: Codex catalog entries

Each entry in `.agents/plugins/marketplace.json` is `{name, source:
{source: "local", path: "./plugins/<name>"}, policy: {installation:
"AVAILABLE", authentication: "ON_INSTALL"}, category: "Productivity"}`.
`plugins/<name>` links to the plugin's immutable generation and is maintained
exactly like the Claude and Factory catalog links. `AVAILABLE` matches the
documented local-marketplace example; `INSTALLED_BY_DEFAULT` was observed to
install nothing headlessly and is not used.

### D2: Codex marketplace registration

The owned marketplace entry is `[marketplaces.<am-catalog>]` with
`source_type = "local"` and `source = <catalog root>`, owned by the scope's
catalog; the plugin entry is unchanged (`[plugins."<plugin>@<am-catalog>"]`,
`enabled = true`, owned by the plugin instance). The value of an owned entry
recorded in the earlier shape differs from the desired value, so ordinary
owned-entry reconciliation rewrites it in place in one sync.

### D3: Shape probe

The Codex harness-compatibility job adds a probe in an isolated `HOME` and
`CODEX_HOME`. Before sync, `codex plugin list` must not list the plugin;
after a native sync through an explicitly verified test catalog, it must
list `<plugin>@<am-catalog>` from the registered marketplace. The probe runs
no `codex plugin add`, no model request, and asserts that no trust entry or
`plugins/cache` directory exists afterwards. It proves the catalog shape, not
installation, so Codex native delivery stays `unverified`.

### D4: Released-reader witness

The released-client gate adds a required Codex scenario like the Claude and
Factory ones.

## Risks / Trade-offs

- Registration does not install the plugin headlessly; Codex native delivery
  remains unverified until a follow-up establishes a supported installation
  path.
- Repository-scope registration lives in `.codex/config.toml`, which Codex
  reads only for trusted projects and which may be committed. This is inert
  while native delivery is unverified and is left to the follow-up.
